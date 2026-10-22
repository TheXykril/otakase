package cast

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// A jump moves the device within a stream that has already been written,
// without starting ffmpeg again.
//
// ffmpeg runs ahead of playback, and every segment of the current stream stays
// on disk, so a seek to anywhere it has already reached needs no new stream at
// all: the device is handed the same playlist with the segments before the
// target left off. The view is served under a directory of its own,
//
//	g2/j5-41/stream.m3u8
//
// meaning generation g2 from its segment 41, the fifth jump. The jump number
// keeps every view's URL new, because a device handed a URL it has seen may
// play it from its own cache. Nothing is written for a view: the server reads
// the generation's playlist as it grows and serves the segments from where
// ffmpeg put them.

// jumpDirPrefix starts the name of a view's directory.
const jumpDirPrefix = "j"

// JumpPath is the URL path of the view of the playlist at playlistPath (as the
// device is given it, "g2/stream.m3u8") that starts at segment from.
func JumpPath(playlistPath string, jump, from int) string {
	dir := path.Dir(playlistPath)
	view := jumpDirPrefix + strconv.Itoa(jump) + "-" + strconv.Itoa(from)
	if dir == "." || dir == "" {
		return view + "/" + path.Base(playlistPath)
	}
	return dir + "/" + view + "/" + path.Base(playlistPath)
}

// parseJumpPath reads a request for a view: the file it stands for, and the
// segment the view starts at.
func parseJumpPath(urlPath string) (real string, from int, ok bool) {
	clean := path.Clean("/" + urlPath)
	dir, file := path.Split(clean)
	dir = strings.TrimSuffix(dir, "/")
	parent, view := path.Split(dir)
	if !strings.HasPrefix(view, jumpDirPrefix) {
		return "", 0, false
	}
	jump, start, found := strings.Cut(strings.TrimPrefix(view, jumpDirPrefix), "-")
	if !found {
		return "", 0, false
	}
	if _, err := strconv.Atoi(jump); err != nil {
		return "", 0, false
	}
	from, err := strconv.Atoi(start)
	if err != nil || from < 0 {
		return "", 0, false
	}
	return path.Join(parent, file), from, true
}

// serveJump answers a request under a view's directory, reporting whether it
// was one.
func serveJump(w http.ResponseWriter, r *http.Request, root string, files http.Handler) bool {
	real, from, ok := parseJumpPath(r.URL.Path)
	if !ok {
		return false
	}
	switch path.Base(real) {
	case PlaylistName:
		serveJumpPlaylist(w, r, filepath.Join(root, filepath.FromSlash(path.Clean("/"+real))), from)
	case ProgressiveName:
		serveProgressiveFrom(w, r, root, real, from)
	default:
		// A segment: the same file the generation's own playlist names.
		r.URL.Path = real
		files.ServeHTTP(w, r)
	}
	return true
}

// serveJumpPlaylist sends the playlist at name without its first from
// segments.
func serveJumpPlaylist(w http.ResponseWriter, r *http.Request, name string, from int) {
	raw, err := os.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write([]byte(JumpPlaylist(string(raw), from)))
}

// JumpPlaylist rewrites a playlist to start at segment from.
//
// The header stays as it is, apart from the media sequence: this is a new
// playlist to the device, and it begins at zero. Each segment takes the tags
// written above it, so a dropped segment drops its #EXTINF with it.
func JumpPlaylist(playlist string, from int) string {
	var (
		out     strings.Builder
		pending []string
		index   int
		inBody  bool
	)
	for _, line := range strings.Split(playlist, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			continue
		case strings.HasPrefix(trimmed, "#EXT-X-MEDIA-SEQUENCE:"):
			if !inBody {
				out.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
				continue
			}
		case trimmed == "#EXT-X-ENDLIST":
			out.WriteString(trimmed + "\n")
			continue
		}
		if strings.HasPrefix(trimmed, "#EXTINF:") || strings.HasPrefix(trimmed, "#EXT-X-DISCONTINUITY") {
			inBody = true
		}
		if !inBody {
			out.WriteString(trimmed + "\n")
			continue
		}
		pending = append(pending, trimmed)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if index >= from {
			for _, kept := range pending {
				out.WriteString(kept + "\n")
			}
		}
		pending = pending[:0]
		index++
	}
	return out.String()
}

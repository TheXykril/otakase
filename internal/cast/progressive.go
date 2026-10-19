package cast

import (
	"bufio"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// ProgressiveName is the one continuous MPEG-TS stream served beside each
// playlist, for devices that do not play HLS.
//
// It is not a file ffmpeg writes. The playlist's segments are MPEG-TS already,
// and transport streams join end to end, so the server reads the playlist as it
// grows and sends each finished segment in order. One ffmpeg, one output, and
// every seek and burn path that already writes HLS works for it unchanged.
const ProgressiveName = "stream.ts"

var (
	// progressivePoll is how often a stream waiting on ffmpeg looks again.
	progressivePoll = 250 * time.Millisecond
	// progressiveStall ends a stream whose playlist has stopped growing
	// without being finished: ffmpeg died, or was replaced by a seek.
	progressiveStall = 60 * time.Second
)

// dlnaContentFeatures is the DLNA description of what is being served: an
// MPEG-TS stream that is being written as it plays, so not seekable by bytes
// or time (OP=00), streamed (the 0x0100000 bit and friends in FLAGS).
const dlnaContentFeatures = "DLNA.ORG_PN=MPEG_TS_HD_NA_ISO;DLNA.ORG_OP=00;DLNA.ORG_CI=0;DLNA.ORG_FLAGS=01700000000000000000000000000000"

// serveProgressive streams the segments of the playlist beside urlPath, from
// the first, until the playlist says it is finished.
func serveProgressive(w http.ResponseWriter, r *http.Request, root, urlPath string) {
	// path.Clean keeps a "../" in the request from reaching outside root; the
	// file server does the same for everything else it serves.
	dir := filepath.Join(root, filepath.FromSlash(path.Dir(path.Clean("/"+urlPath))))

	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("transferMode.dlna.org", "Streaming")
	w.Header().Set("contentFeatures.dlna.org", dlnaContentFeatures)
	// No Content-Length: the length is not known until ffmpeg finishes, and a
	// wrong one ends playback early. The server falls back to chunked.
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	sent := 0
	lastGrowth := time.Now()
	for {
		select {
		case <-r.Context().Done():
			return
		default:
		}

		segments, finished := readPlaylistSegments(filepath.Join(dir, PlaylistName))
		if sent < len(segments) {
			if !copySegment(w, filepath.Join(dir, filepath.FromSlash(segments[sent]))) {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			sent++
			lastGrowth = time.Now()
			continue
		}
		if finished || time.Since(lastGrowth) > progressiveStall {
			return
		}
		time.Sleep(progressivePoll)
	}
}

// copySegment sends one segment, reporting whether the client is still there.
func copySegment(w io.Writer, name string) bool {
	f, err := os.Open(name)
	if err != nil {
		// Listed but gone: an old generation cleared after a seek. The client
		// is being moved to the new stream anyway.
		return false
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err == nil
}

// readPlaylistSegments lists the segments of an HLS playlist in order, and
// whether it is finished. ffmpeg adds a segment to the playlist only once the
// segment is complete, so every name returned is safe to send whole.
func readPlaylistSegments(name string) (segments []string, finished bool) {
	f, err := os.Open(name)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "":
		case line == "#EXT-X-ENDLIST":
			finished = true
		case strings.HasPrefix(line, "#"):
		default:
			// Only names beside the playlist: an absolute or climbing URI is
			// nothing ffmpeg writes, and nothing to open.
			if !strings.Contains(line, "://") && !strings.Contains(line, "..") && !strings.HasPrefix(line, "/") {
				segments = append(segments, line)
			}
		}
	}
	return segments, finished
}

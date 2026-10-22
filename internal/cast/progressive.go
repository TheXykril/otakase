package cast

import (
	"bufio"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
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
	serveProgressiveFrom(w, r, root, urlPath, 0)
}

// serveProgressiveFrom is serveProgressive starting at segment skip: a jump
// within a stream already written, for a device that plays one continuous
// stream rather than a playlist.
func serveProgressiveFrom(w http.ResponseWriter, r *http.Request, root, urlPath string, skip int) {
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

	sent := skip
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
	listed, finished := ReadPlaylist(name)
	for _, segment := range listed {
		segments = append(segments, segment.Name)
	}
	return segments, finished
}

// Segment is one finished piece of a stream ffmpeg is writing.
type Segment struct {
	Name string
	// Duration is the segment's length in seconds, as its #EXTINF says.
	Duration float64
}

// ReadPlaylist lists the segments of an HLS playlist ffmpeg is writing, with
// their lengths, and whether it is finished. Every segment listed is complete:
// ffmpeg adds one to the playlist only once it has been written.
func ReadPlaylist(name string) (segments []Segment, finished bool) {
	f, err := os.Open(name)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	duration := 0.0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "":
		case line == "#EXT-X-ENDLIST":
			finished = true
		case strings.HasPrefix(line, "#EXTINF:"):
			value, _, _ := strings.Cut(strings.TrimPrefix(line, "#EXTINF:"), ",")
			duration, _ = strconv.ParseFloat(strings.TrimSpace(value), 64)
		case strings.HasPrefix(line, "#"):
		default:
			// Only names beside the playlist: an absolute or climbing URI is
			// nothing ffmpeg writes, and nothing to open.
			if !strings.Contains(line, "://") && !strings.Contains(line, "..") && !strings.HasPrefix(line, "/") {
				segments = append(segments, Segment{Name: line, Duration: duration})
			}
			duration = 0
		}
	}
	return segments, finished
}

package cast

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServeProgressiveJoinsSegmentsAsTheyArrive(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "g0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("seg00000.ts", "AAA")
	write("seg00001.ts", "BBB")
	write(PlaylistName, "#EXTM3U\n#EXTINF:4,\nseg00000.ts\n")

	oldPoll := progressivePoll
	progressivePoll = 10 * time.Millisecond
	defer func() { progressivePoll = oldPoll }()

	go func() {
		time.Sleep(50 * time.Millisecond)
		write(PlaylistName, "#EXTM3U\n#EXTINF:4,\nseg00000.ts\n#EXTINF:4,\nseg00001.ts\n#EXT-X-ENDLIST\n")
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveProgressive(w, r, root, r.URL.Path)
	}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/g0/" + ProgressiveName)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "AAABBB" {
		t.Fatalf("body = %q", body)
	}
	if resp.Header.Get("Content-Type") != "video/mp2t" || resp.Header.Get("transferMode.dlna.org") != "Streaming" {
		t.Fatalf("headers = %v", resp.Header)
	}
}

func TestReadPlaylistSegmentsIgnoresOutsidePaths(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, PlaylistName)
	os.WriteFile(name, []byte("#EXTM3U\nseg0.ts\n../secret\n/etc/passwd\nhttp://x/y.ts\n"), 0o644)
	segments, finished := readPlaylistSegments(name)
	if len(segments) != 1 || segments[0] != "seg0.ts" || finished {
		t.Fatalf("segments = %v finished = %v", segments, finished)
	}
}

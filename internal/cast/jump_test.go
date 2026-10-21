package cast

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const jumpTestPlaylist = `#EXTM3U
#EXT-X-VERSION:6
#EXT-X-TARGETDURATION:10
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-PLAYLIST-TYPE:EVENT
#EXT-X-INDEPENDENT-SEGMENTS
#EXTINF:10.416667,
seg00000.ts
#EXTINF:9.583333,
seg00001.ts
#EXTINF:4.000000,
seg00002.ts
`

func TestJumpPlaylistStartsAtTheSegment(t *testing.T) {
	got := JumpPlaylist(jumpTestPlaylist, 1)
	if strings.Contains(got, "seg00000.ts") || strings.Contains(got, "10.416667") {
		t.Fatalf("the first segment is still listed:\n%s", got)
	}
	for _, want := range []string{"#EXTM3U", "#EXT-X-PLAYLIST-TYPE:EVENT", "#EXT-X-MEDIA-SEQUENCE:0", "#EXTINF:9.583333,\nseg00001.ts", "#EXTINF:4.000000,\nseg00002.ts"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "#EXT-X-ENDLIST") {
		t.Fatalf("an unfinished playlist was finished:\n%s", got)
	}
	if finished := JumpPlaylist(jumpTestPlaylist+"#EXT-X-ENDLIST\n", 2); !strings.HasSuffix(finished, "seg00002.ts\n#EXT-X-ENDLIST\n") {
		t.Fatalf("a finished playlist lost its end:\n%s", finished)
	}
}

func TestJumpPathRoundTrips(t *testing.T) {
	jump := JumpPath("g2/"+PlaylistName, 5, 41)
	if jump != "g2/j5-41/"+PlaylistName {
		t.Fatalf("jump path %q", jump)
	}
	real, from, ok := parseJumpPath("/" + jump)
	if !ok || real != "/g2/"+PlaylistName || from != 41 {
		t.Fatalf("parsed %q %d %t", real, from, ok)
	}
	if real, _, ok := parseJumpPath("/g2/j5-41/seg00041.ts"); !ok || real != "/g2/seg00041.ts" {
		t.Fatalf("segment parsed as %q %t", real, ok)
	}
	for _, other := range []string{"/g2/" + PlaylistName, "/g2/seg00001.ts", "/g2/jx-1/" + PlaylistName, "/g2/j1/" + PlaylistName} {
		if _, _, ok := parseJumpPath(other); ok {
			t.Fatalf("%q read as a jump", other)
		}
	}
}

func TestReadPlaylistKeepsSegmentLengths(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, PlaylistName)
	if err := os.WriteFile(name, []byte(jumpTestPlaylist), 0o644); err != nil {
		t.Fatal(err)
	}
	segments, finished := ReadPlaylist(name)
	if finished || len(segments) != 3 || segments[0].Duration != 10.416667 || segments[2].Name != "seg00002.ts" {
		t.Fatalf("read %+v finished=%t", segments, finished)
	}
}

func TestAJumpIsServedFromTheGenerationsFiles(t *testing.T) {
	root := t.TempDir()
	gen := filepath.Join(root, "g0")
	if err := os.MkdirAll(gen, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gen, PlaylistName), []byte(jumpTestPlaylist+"#EXT-X-ENDLIST\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i, body := range []string{"zero", "one", "two"} {
		if err := os.WriteFile(filepath.Join(gen, "seg0000"+string(rune('0'+i))+".ts"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := http.FileServer(http.Dir(root))
	get := func(path string) string {
		t.Helper()
		rec := httptest.NewRecorder()
		if !serveJump(rec, httptest.NewRequest(http.MethodGet, path, nil), root, files) {
			t.Fatalf("%s not served as a jump", path)
		}
		return rec.Body.String()
	}

	if got := get("/g0/j1-1/" + PlaylistName); strings.Contains(got, "seg00000.ts") || !strings.Contains(got, "seg00001.ts") {
		t.Fatalf("playlist:\n%s", got)
	}
	if got := get("/g0/j1-1/seg00002.ts"); got != "two" {
		t.Fatalf("segment body %q", got)
	}
	defer func(poll time.Duration) { progressivePoll = poll }(progressivePoll)
	progressivePoll = time.Millisecond
	if got := get("/g0/j1-1/" + ProgressiveName); got != "onetwo" {
		t.Fatalf("progressive stream %q, want the segments from the jump on", got)
	}
}

// A load replaces the stream, but the receiver answers a moment longer about
// the one it was told to leave.
func TestAStatusForTheStreamBeforeIsStale(t *testing.T) {
	if !staleStatus("http://pc/g0/stream.m3u8", "http://pc/g1/stream.m3u8", time.Second) {
		t.Fatal("the old stream's status was taken for the new one")
	}
	if staleStatus("http://pc/g1/stream.m3u8", "http://pc/g1/stream.m3u8", time.Second) {
		t.Fatal("the new stream's status was taken as stale")
	}
	// Not forever: a receiver that names the stream some other way is
	// believed again.
	if staleStatus("other", "http://pc/g1/stream.m3u8", staleStatusWindow+time.Second) {
		t.Fatal("still stale long after the load")
	}
	if staleStatus("", "http://pc/g1/stream.m3u8", time.Second) {
		t.Fatal("a status naming nothing was taken as stale")
	}
}

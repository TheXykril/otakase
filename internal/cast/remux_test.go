package cast

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Casting must cost bandwidth and almost no CPU, exactly as a download does.
func TestBuildRemuxArgsCopiesStreams(t *testing.T) {
	args := BuildRemuxArgs("https://cdn.test/master.m3u8", "", "/tmp/cast")
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "-c copy") {
		t.Fatalf("expected a stream copy, got: %s", joined)
	}
	if !strings.Contains(joined, "-nostdin") {
		t.Fatalf("ffmpeg must not consume otakase's stdin, got: %s", joined)
	}
}

// Providers disguise HLS segments as images to slip past filters. Without
// these, a perfectly good stream fails with "not in allowed_segment_extensions"
// -- the same wall downloads hit before internal/download.go worked around it.
func TestBuildRemuxArgsToleratesDisguisedSegments(t *testing.T) {
	joined := strings.Join(BuildRemuxArgs("https://cdn.test/x.m3u8", "", "/tmp/cast"), " ")

	if !strings.Contains(joined, "-allowed_extensions ALL") {
		t.Errorf("expected -allowed_extensions ALL, got: %s", joined)
	}
	if !strings.Contains(joined, "-extension_picky 0") {
		t.Errorf("expected -extension_picky 0, got: %s", joined)
	}
}

// Hosts reject a request without the referrer their own player sends, and
// -headers is a per-input option: it must come before -i or it applies to
// nothing.
func TestBuildRemuxArgsPassesReferrerBeforeTheInput(t *testing.T) {
	args := BuildRemuxArgs("https://cdn.test/x.m3u8", "https://megaplay.buzz/", "/tmp/cast")

	headerIdx, inputIdx := -1, -1
	for i, arg := range args {
		if arg == "-headers" {
			headerIdx = i
		}
		if arg == "-i" && inputIdx == -1 {
			inputIdx = i
		}
	}
	if headerIdx == -1 {
		t.Fatalf("no referrer header: %v", args)
	}
	if headerIdx > inputIdx {
		t.Fatalf("-headers came after -i, so it applies to nothing: %v", args)
	}
}

// An event playlist appends and never drops segments, so the device can seek
// anywhere already written. A VOD playlist would need the whole duration known
// upfront, and a live one would offer no seek bar at all.
func TestBuildRemuxArgsWritesASeekableEventPlaylist(t *testing.T) {
	args := BuildRemuxArgs("https://cdn.test/x.m3u8", "", "/tmp/cast")
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "-hls_playlist_type event") {
		t.Errorf("expected an event playlist, got: %s", joined)
	}
	if args[len(args)-1] != filepath.Join("/tmp/cast", PlaylistName) {
		t.Errorf("playlist must be the final argument, got: %s", joined)
	}
}

// The session cannot tell the device to play a playlist that does not exist
// yet, and ffmpeg takes a moment to write the first segment.
func TestWaitForPlaylistReturnsOnceItExists(t *testing.T) {
	dir := t.TempDir()
	go func() {
		time.Sleep(50 * time.Millisecond)
		os.WriteFile(filepath.Join(dir, PlaylistName), []byte("#EXTM3U\n"), 0o644)
	}()

	if err := WaitForPlaylist(dir, 2*time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWaitForPlaylistGivesUp(t *testing.T) {
	if err := WaitForPlaylist(t.TempDir(), 100*time.Millisecond); err == nil {
		t.Error("waiting for a playlist that never appears should fail")
	}
}

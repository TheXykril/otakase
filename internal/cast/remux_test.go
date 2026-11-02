package cast

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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

// The receiver ignores SEEK on an event playlist, so a seek rebuilds the stream
// from the target instead. Where the offset goes decides whether that takes a
// moment or most of an episode.
func TestASeekingRemuxSeeksTheInputRatherThanDecodingUpToIt(t *testing.T) {
	args := BuildRemuxArgsFrom("https://host/master.m3u8", "", "/tmp/out", 754.5, nil, nil)

	ss, input := -1, -1
	for i, arg := range args {
		switch arg {
		case "-ss":
			ss = i
		case "-i":
			input = i
		}
	}
	if ss < 0 {
		t.Fatalf("no -ss in %v", args)
	}
	// After -i, ffmpeg decodes the whole episode up to the target before writing
	// a byte, which the viewer reads as a seek that hung.
	if ss > input {
		t.Fatalf("-ss at %d comes after -i at %d", ss, input)
	}
	if args[ss+1] != "754.500" {
		t.Fatalf("offset written as %q", args[ss+1])
	}
}

func TestAStreamStartingAtZeroCarriesNoOffset(t *testing.T) {
	// The ordinary first play goes through the same builder, and an -ss 0 is a
	// difference in the command line for no difference in the output.
	for _, arg := range BuildRemuxArgsFrom("https://host/master.m3u8", "", "/tmp/out", 0, nil, nil) {
		if arg == "-ss" {
			t.Fatal("a stream starting at zero was given an -ss")
		}
	}
}

func TestASeekingRemuxKeepsTheRefererAndTheDisguisedSegments(t *testing.T) {
	args := strings.Join(BuildRemuxArgsFrom("https://host/master.m3u8", "https://player.test/", "/tmp/out", 60, nil, nil), " ")

	// A seek must not quietly drop the two things that make these streams work
	// at all, or seeking would fail on exactly the providers that need them.
	for _, want := range []string{strings.Join(HLSInputArgs(), " "), "Referer: https://player.test/"} {
		if !strings.Contains(args, want) {
			t.Fatalf("seeking args lost %q:\n%s", want, args)
		}
	}
}

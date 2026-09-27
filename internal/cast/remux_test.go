package cast

import (
	"os"
	"os/exec"
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

// Critical 1: aac_adtstoasc strips ADTS headers for an MP4 container. This
// output is MPEG-TS, which needs AAC *in* ADTS -- with the filter, ffmpeg
// exits 0 and every device plays undecodable audio. download.go carries this
// filter for its own (different) container, which is exactly how it arrived
// here; this assertion is the one that would have caught it.
func TestBuildRemuxArgsDoesNotStripADTSHeaders(t *testing.T) {
	args := BuildRemuxArgs("https://cdn.test/master.m3u8", "", "/tmp/cast")
	joined := strings.Join(args, " ")

	if strings.Contains(joined, "aac_adtstoasc") {
		t.Fatalf("aac_adtstoasc strips the ADTS headers this MPEG-TS output needs, got: %s", joined)
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

// Critical 1's bug exited 0 from both the remux and a decode of its output,
// and was loud only on stderr -- every prior test in this file asserts
// argument strings, which is exactly why a broken argument vector shipped.
// This builds a real TS-HLS source with ffmpeg, remuxes it with the actual
// BuildRemuxArgs vector, decodes the result, and fails on any decoder
// complaint.
func TestRemuxedStreamDecodesCleanly(t *testing.T) {
	ffmpegBin, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}

	// A ~6 second H.264/AAC source, muxed as HLS so BuildRemuxArgs' own
	// -allowed_extensions/-extension_picky handling applies to it the same way
	// it would to a real provider's stream.
	sourceDir := t.TempDir()
	sourcePlaylist := filepath.Join(sourceDir, "source.m3u8")
	build := exec.Command(ffmpegBin,
		"-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=15:duration=6",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=6",
		"-c:v", "libx264", "-preset", "ultrafast",
		"-c:a", "aac",
		"-f", "hls", "-hls_time", "2", "-hls_playlist_type", "event",
		sourcePlaylist,
	)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("could not build the test fixture: %v\n%s", err, out)
	}

	// The real argument vector, run exactly as StartRemux would run it.
	outDir := t.TempDir()
	remux := exec.Command(ffmpegBin, BuildRemuxArgs(sourcePlaylist, "", outDir)...)
	if out, err := remux.CombinedOutput(); err != nil {
		t.Fatalf("remux failed: %v\n%s", err, out)
	}

	// The assertion that catches Critical 1: the bug produced a zero exit
	// status here too, and was loud only on stderr.
	decode := exec.Command(ffmpegBin,
		"-v", "error",
		"-allowed_extensions", "ALL",
		"-i", filepath.Join(outDir, PlaylistName),
		"-f", "null", "-",
	)
	var decodeStderr strings.Builder
	decode.Stderr = &decodeStderr
	if err := decode.Run(); err != nil {
		t.Fatalf("decoding the remuxed stream failed: %v\n%s", err, decodeStderr.String())
	}
	if stderr := decodeStderr.String(); stderr != "" {
		t.Fatalf("remuxed stream did not decode cleanly, e.g. Critical 1 (aac_adtstoasc on a TS output):\n%s", stderr)
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

// A Chromecast decodes H.264 up to Level 4.1, and encoders over-declare: the
// stream that first exercised this arrived claiming Level 5.0 for 1080p24
// content that fits inside Level 4.0. The device fetched one segment and
// dropped the media session, with no error state to read back.
func TestBuildRemuxArgsCapsTheH264Level(t *testing.T) {
	args := strings.Join(BuildRemuxArgs("https://example.test/stream.m3u8", "", "/tmp/out"), " ")
	if !strings.Contains(args, "h264_metadata=level=4.1") {
		t.Errorf("the H.264 level is not capped for the device's decoder:\n%s", args)
	}
	// Still a remux. A level is a field in the SPS, not a reason to re-encode.
	if !strings.Contains(args, "-c copy") {
		t.Errorf("the stream is no longer copied:\n%s", args)
	}
}

// The receiver ignores SEEK on an event playlist, so a seek rebuilds the stream
// from the target instead. Where the offset goes decides whether that takes a
// moment or most of an episode.
func TestASeekingRemuxSeeksTheInputRatherThanDecodingUpToIt(t *testing.T) {
	args := BuildRemuxArgsFrom("https://host/master.m3u8", "", "/tmp/out", 754.5)

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
	for _, arg := range BuildRemuxArgsFrom("https://host/master.m3u8", "", "/tmp/out", 0) {
		if arg == "-ss" {
			t.Fatal("a stream starting at zero was given an -ss")
		}
	}
}

func TestASeekingRemuxKeepsTheRefererAndTheDisguisedSegments(t *testing.T) {
	args := strings.Join(BuildRemuxArgsFrom("https://host/master.m3u8", "https://player.test/", "/tmp/out", 60), " ")

	// A seek must not quietly drop the two things that make these streams work
	// at all, or seeking would fail on exactly the providers that need them.
	for _, want := range []string{"-allowed_extensions ALL", "-extension_picky 0", "Referer: https://player.test/"} {
		if !strings.Contains(args, want) {
			t.Fatalf("seeking args lost %q:\n%s", want, args)
		}
	}
}

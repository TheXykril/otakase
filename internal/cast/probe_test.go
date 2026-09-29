package cast

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTheProbeAllowsTheDisguisedSegmentsTheRemuxAllows(t *testing.T) {
	args := strings.Join(ProbeStreamArgs("https://example.test/master.m3u8", "", nil), " ")
	// Providers serve HLS segments named .jpg. ffprobe refuses unrecognised
	// extensions exactly as ffmpeg does, so a probe without these fails on the
	// streams that need a probe most.
	for _, want := range []string{"-allowed_extensions ALL", "-extension_picky 0"} {
		if !strings.Contains(args, want) {
			t.Fatalf("probe args %q are missing %q", args, want)
		}
	}
}

func TestTheProbeSendsTheReferrerBeforeTheInput(t *testing.T) {
	args := ProbeStreamArgs("https://example.test/master.m3u8", "https://player.test/", nil)

	headers, input := -1, -1
	for i, arg := range args {
		switch arg {
		case "-headers":
			headers = i
		case "-i":
			input = i
		}
	}
	if headers < 0 {
		t.Fatalf("no -headers in %v", args)
	}
	// -headers is a per-input option: after -i it applies to nothing, and the
	// CDNs that require a referrer answer 403 without it.
	if headers > input {
		t.Fatalf("-headers at %d comes after -i at %d: %v", headers, input, args)
	}
	if got := args[headers+1]; !strings.Contains(got, "Referer: https://player.test/") {
		t.Fatalf("referrer header is %q", got)
	}
}

func TestAStreamWithNoReferrerSendsNoHeaders(t *testing.T) {
	for _, arg := range ProbeStreamArgs("https://example.test/master.m3u8", "   ", nil) {
		if arg == "-headers" {
			t.Fatal("a blank referrer still produced a -headers option")
		}
	}
}

func TestTheProbeReadsTheLengthOfTheEpisode(t *testing.T) {
	// ffprobe's output for a real anikoto episode: 24:40, where the tracker's
	// average for the series says 24:00.
	seconds, ok := ParseProbedDuration("1480.020778\n")
	if !ok {
		t.Fatal("a plain duration was not read")
	}
	if seconds != 1480.020778 {
		t.Fatalf("read %v seconds", seconds)
	}
}

func TestAStreamOfUnknownLengthReportsNoLength(t *testing.T) {
	// Both shapes ffprobe produces when it cannot work out a length, and the
	// answer for both is to keep whatever length we already had.
	for _, output := range []string{"N/A\n", "", "\n\n", "0.000000\n"} {
		if seconds, ok := ParseProbedDuration(output); ok {
			t.Fatalf("output %q was read as %v seconds", output, seconds)
		}
	}
}

func TestTheProbeUsesTheFFprobeBesideTheResolvedFFmpeg(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the executable bit and bare names differ on windows")
	}
	dir := t.TempDir()
	ffmpeg := filepath.Join(dir, "ffmpeg")
	beside := filepath.Join(dir, "ffprobe")
	for _, path := range []string{ffmpeg, beside} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got, err := FFprobePathFor(ffmpeg)
	if err != nil {
		t.Fatal(err)
	}
	// A user running a self-contained ffmpeg build has one on PATH too, and the
	// pair has to stay together: PATH would silently pick the other install.
	if got != beside {
		t.Fatalf("resolved %q, want the ffprobe beside ffmpeg at %q", got, beside)
	}
}

func TestAnFFmpegWithNoFFprobeBesideItFallsBackToPath(t *testing.T) {
	dir := t.TempDir()
	ffmpeg := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(ffmpeg, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := FFprobePathFor(ffmpeg)
	if err != nil {
		// No ffprobe on this machine at all is a legitimate outcome, and the
		// caller treats it the same as a probe that reported nothing.
		return
	}
	if filepath.Dir(got) == dir {
		t.Fatalf("resolved %q from a directory holding no ffprobe", got)
	}
}

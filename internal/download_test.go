package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"One Piece":              "One Piece",
		"Re:Zero kara Hajimeru":  "ReZero kara Hajimeru",
		"Fate/Zero":              "FateZero",
		`bad<>:"/\|?*chars`:      "badchars",
		"  spaced   out  ":       "spaced out",
		"trailing dots...":       "trailing dots",
		"":                       "episode",
		strings.Repeat("x", 300): strings.Repeat("x", 150),
	}
	for input, want := range cases {
		if got := SanitizeFilename(input); got != want {
			t.Fatalf("SanitizeFilename(%q) = %q, want %q", input, got, want)
		}
	}
}

// A path separator smuggled in via the title would write outside the download
// directory.
func TestDownloadPathStaysInsideDir(t *testing.T) {
	for _, hostile := range []string{"../../etc/passwd", "..", "/etc/shadow", `..\..\windows`} {
		path := DownloadPath("/tmp/dl", hostile, 1, "sub")
		if filepath.Dir(path) != "/tmp/dl" {
			t.Fatalf("%q escaped the download directory: %q", hostile, path)
		}
		// Separators are stripped, so the result is always a flat filename.
		if strings.ContainsAny(filepath.Base(path), `/\`) {
			t.Fatalf("%q produced a path separator: %q", hostile, path)
		}
	}
}

func TestDownloadPathFormat(t *testing.T) {
	got := DownloadPath("/tmp/dl", "One Piece", 7, "dub")
	want := filepath.Join("/tmp/dl", "One Piece - Episode 07 (dub).mp4")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	// Episode numbers are zero-padded so a directory listing sorts correctly.
	if !strings.Contains(DownloadPath("/tmp", "X", 3, "sub"), "Episode 03") {
		t.Fatal("expected zero-padded episode numbers")
	}
	if !strings.Contains(DownloadPath("/tmp", "X", 123, "sub"), "Episode 123") {
		t.Fatal("expected long episode numbers to be kept intact")
	}
}

func TestParseEpisodeRange(t *testing.T) {
	cases := []struct {
		raw      string
		fallback int
		from, to int
		wantErr  bool
	}{
		{raw: "", fallback: 4, from: 4, to: 4},
		{raw: "7", fallback: 1, from: 7, to: 7},
		{raw: "1-12", fallback: 1, from: 1, to: 12},
		{raw: " 3 - 5 ", fallback: 1, from: 3, to: 5},
		{raw: "9-4", fallback: 1, from: 4, to: 9},
		{raw: "", fallback: 0, wantErr: true},
		{raw: "abc", fallback: 1, wantErr: true},
		{raw: "0", fallback: 1, wantErr: true},
		{raw: "1-x", fallback: 1, wantErr: true},
	}

	for _, tc := range cases {
		from, to, err := ParseEpisodeRange(tc.raw, tc.fallback)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("ParseEpisodeRange(%q, %d) should have failed", tc.raw, tc.fallback)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseEpisodeRange(%q, %d): %v", tc.raw, tc.fallback, err)
		}
		if from != tc.from || to != tc.to {
			t.Fatalf("ParseEpisodeRange(%q, %d) = %d-%d, want %d-%d", tc.raw, tc.fallback, from, to, tc.from, tc.to)
		}
	}
}

func TestResolveDownloadDir(t *testing.T) {
	if got := ResolveDownloadDir(&Config{DownloadDir: "/explicit/path"}); got != "/explicit/path" {
		t.Fatalf("got %q", got)
	}
	t.Setenv("CURD_TEST_DL", "/from/env")
	if got := ResolveDownloadDir(&Config{DownloadDir: "$CURD_TEST_DL/x"}); got != "/from/env/x" {
		t.Fatalf("expected env expansion, got %q", got)
	}
	// An unset directory still resolves somewhere usable.
	if got := ResolveDownloadDir(&Config{}); got == "" {
		t.Fatal("expected a default download directory")
	}
	if got := ResolveDownloadDir(nil); got == "" {
		t.Fatal("expected a default for a nil config")
	}
}

// Streams are remuxed, never re-encoded: a download should cost bandwidth and
// almost no CPU.
func TestBuildFFmpegArgsCopiesStreams(t *testing.T) {
	args := buildFFmpegArgs("https://cdn.test/master.m3u8", "", "", "/tmp/out.mp4")
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "-c copy") {
		t.Fatalf("expected a stream copy, got: %s", joined)
	}
	// HLS audio is ADTS; MP4 needs it converted or the file will not play.
	if !strings.Contains(joined, "aac_adtstoasc") {
		t.Fatalf("expected the ADTS bitstream filter, got: %s", joined)
	}
	if !strings.Contains(joined, "-nostdin") {
		t.Fatalf("ffmpeg must not consume otakase's stdin, got: %s", joined)
	}
	if args[len(args)-1] != "/tmp/out.mp4" {
		t.Fatalf("output must be the final argument, got: %s", joined)
	}
}

// Hosts reject requests without the referrer their own player sends.
func TestBuildFFmpegArgsPassesReferrer(t *testing.T) {
	args := buildFFmpegArgs("https://cdn.test/x.m3u8", "https://megaplay.buzz/", "", "/tmp/o.mp4")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "Referer: https://megaplay.buzz/") {
		t.Fatalf("expected the referrer header, got: %s", joined)
	}

	// The header must precede -i, or ffmpeg applies it to nothing.
	headerIdx, inputIdx := -1, -1
	for i, arg := range args {
		if arg == "-headers" {
			headerIdx = i
		}
		if arg == "-i" && inputIdx == -1 {
			inputIdx = i
		}
	}
	if headerIdx == -1 || inputIdx == -1 || headerIdx > inputIdx {
		t.Fatalf("-headers must come before -i, got: %s", joined)
	}

	// With no referrer the flag is omitted entirely.
	if strings.Contains(strings.Join(buildFFmpegArgs("u", "", "", "o"), " "), "-headers") {
		t.Fatal("expected no -headers without a referrer")
	}
}

func TestBuildFFmpegArgsMuxesSubtitles(t *testing.T) {
	args := buildFFmpegArgs("https://cdn.test/x.m3u8", "https://megaplay.buzz/", "https://cdn.test/subs.vtt", "/tmp/o.mp4")
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "subs.vtt") {
		t.Fatalf("expected the subtitle input, got: %s", joined)
	}
	if !strings.Contains(joined, "mov_text") {
		t.Fatalf("MP4 subtitles need mov_text, got: %s", joined)
	}

	// A bare "-map 0" pulls in every variant of an HLS master playlist, which
	// produced a broken file and crashed ffmpeg.
	if strings.Contains(joined, "-map 0 ") {
		t.Fatalf("expected explicit stream mapping, not -map 0: %s", joined)
	}
	for _, want := range []string{"-map 0:v:0", "-map 0:a:0", "-map 1:0"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q, got: %s", want, joined)
		}
	}

	// -headers is per-input: the subtitle fetch is a separate request and gets a
	// 403 without its own referrer.
	if strings.Count(joined, "-headers") != 2 {
		t.Fatalf("expected a referrer header before each input, got: %s", joined)
	}

	// ...but the HLS demuxer options must not be repeated before the WebVTT
	// input; ffmpeg fails with "Option extension_picky not found".
	if strings.Count(joined, "-extension_picky") != 1 {
		t.Fatalf("HLS options must apply only to the stream input, got: %s", joined)
	}
	if strings.Count(joined, "-allowed_extensions") != 1 {
		t.Fatalf("HLS options must apply only to the stream input, got: %s", joined)
	}
}

func TestDownloadEpisodeRejectsBadInput(t *testing.T) {
	if _, err := DownloadEpisode(Config{}, nil, 1, t.TempDir()); err == nil {
		t.Fatal("expected an error for a nil anime")
	}
	if _, err := DownloadEpisode(Config{}, &Anime{}, 0, t.TempDir()); err == nil {
		t.Fatal("expected an error for episode 0")
	}
}

// A completed download must not be repeated on a later run.
func TestDownloadEpisodeSkipsExistingFile(t *testing.T) {
	if _, err := ffmpegPath(); err != nil {
		t.Skip("ffmpeg is not installed")
	}

	dir := t.TempDir()
	anime := &Anime{Title: AnimeTitle{English: "Demo Show"}, ProviderName: "anipub", ProviderId: "1"}
	path := DownloadPath(dir, GetAnimeName(*anime), 1, "sub")
	if err := os.WriteFile(path, []byte("already downloaded"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := DownloadEpisode(Config{SubOrDub: "sub"}, anime, 1, dir)
	if err != nil {
		t.Fatalf("expected the existing file to be reused, got %v", err)
	}
	if got != path {
		t.Fatalf("got %q, want %q", got, path)
	}
	// Untouched.
	raw, _ := os.ReadFile(path)
	if string(raw) != "already downloaded" {
		t.Fatal("the existing download was overwritten")
	}
}

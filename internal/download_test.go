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
		path := DownloadPath("/tmp/dl", hostile, 1, "sub", DownloadFormatMKV)
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
	got := DownloadPath("/tmp/dl", "One Piece", 7, "dub", DownloadFormatMKV)
	want := filepath.Join("/tmp/dl", "One Piece - Episode 07 (dub).mkv")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := DownloadPath("/tmp/dl", "One Piece", 7, "dub", DownloadFormatMP4); !strings.HasSuffix(got, "(dub).mp4") {
		t.Fatalf("expected an MP4 name, got %q", got)
	}
	// Anything unrecognised is MKV, never a path with a stray extension.
	if got := DownloadPath("/tmp/dl", "X", 1, "sub", "../evil"); !strings.HasSuffix(got, "(sub).mkv") {
		t.Fatalf("expected an MKV name, got %q", got)
	}
	// Episode numbers are zero-padded so a directory listing sorts correctly.
	if !strings.Contains(DownloadPath("/tmp", "X", 3, "sub", DownloadFormatMKV), "Episode 03") {
		t.Fatal("expected zero-padded episode numbers")
	}
	if !strings.Contains(DownloadPath("/tmp", "X", 123, "sub", DownloadFormatMKV), "Episode 123") {
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
	args := ffmpegJob{Stream: "https://cdn.test/master.m3u8", Output: "/tmp/out.mp4"}.args()
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
	args := ffmpegJob{Stream: "https://cdn.test/x.m3u8", Referrer: "https://megaplay.buzz/", Output: "/tmp/o.mp4"}.args()
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
	if strings.Contains(strings.Join(ffmpegJob{Stream: "u", Output: "o"}.args(), " "), "-headers") {
		t.Fatal("expected no -headers without a referrer")
	}
}

func TestBuildFFmpegArgsMuxesSubtitles(t *testing.T) {
	args := ffmpegJob{Stream: "https://cdn.test/x.m3u8", Referrer: "https://megaplay.buzz/", Subtitle: "https://cdn.test/subs.vtt", Output: "/tmp/o.mp4"}.args()
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
	path := DownloadPath(dir, GetAnimeName(*anime), 1, "sub", DownloadFormatMKV)
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

// An episode saved as MP4 before the default became MKV is still a finished
// download.
func TestDownloadEpisodeSkipsExistingFileInOtherFormat(t *testing.T) {
	if _, err := ffmpegPath(); err != nil {
		t.Skip("ffmpeg is not installed")
	}

	dir := t.TempDir()
	anime := &Anime{Title: AnimeTitle{English: "Demo Show"}, ProviderName: "anipub", ProviderId: "1"}
	path := DownloadPath(dir, GetAnimeName(*anime), 2, "sub", DownloadFormatMP4)
	if err := os.WriteFile(path, []byte("already downloaded"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := DownloadEpisode(Config{SubOrDub: "sub", DownloadFormat: "mkv"}, anime, 2, dir)
	if err != nil {
		t.Fatalf("expected the existing file to be reused, got %v", err)
	}
	if got != path {
		t.Fatalf("got %q, want %q", got, path)
	}
}

func TestDownloadFormat(t *testing.T) {
	cases := map[string]string{"": "mkv", "mkv": "mkv", "MP4": "mp4", " mp4 ": "mp4", "avi": "mkv"}
	for raw, want := range cases {
		if got := DownloadFormat(&Config{DownloadFormat: raw}); got != want {
			t.Fatalf("DownloadFormat(%q) = %q, want %q", raw, got, want)
		}
	}
	if got := DownloadFormat(nil); got != "mkv" {
		t.Fatalf("nil config: got %q", got)
	}
}

// MKV takes WebVTT as it is; converting it would lose nothing but gain nothing.
func TestBuildFFmpegArgsMKVKeepsSubtitles(t *testing.T) {
	args := ffmpegJob{Stream: "https://cdn.test/x.m3u8", Subtitle: "https://cdn.test/subs.vtt", Output: "/tmp/o.mkv"}.args()
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "mov_text") {
		t.Fatalf("MKV must not convert subtitles to mov_text: %s", joined)
	}
	for _, want := range []string{"-map 0:v:0", "-map 0:a:0", "-map 1:0", "-extension_picky 0", "aac_adtstoasc"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q, got: %s", want, joined)
		}
	}
}

// A torrent release is a whole Matroska file: every track and font is kept,
// and the HLS-only options and the ADTS filter -- which rejects FLAC and Opus
// -- are left out.
func TestBuildFFmpegArgsWholeFileKeepsEverything(t *testing.T) {
	args := ffmpegJob{Stream: "http://127.0.0.1:41234/abcdef", Output: "/tmp/o.mkv"}.args()
	joined := strings.Join(args, " ")
	for _, unwanted := range []string{"-allowed_extensions", "-extension_picky", "aac_adtstoasc", "mov_text"} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("unexpected %q for a whole file: %s", unwanted, joined)
		}
	}
	for _, want := range []string{"-map 0:v:0", "-map 0:a?", "-map 0:s?", "-map 0:t?"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q, got: %s", want, joined)
		}
	}

	// MP4 holds neither ASS nor fonts, so one subtitle track is converted.
	joined = strings.Join(ffmpegJob{Stream: "https://host.test/ep1.mkv", Output: "/tmp/o.mp4"}.args(), " ")
	if strings.Contains(joined, "0:t") || !strings.Contains(joined, "-map 0:s:0?") || !strings.Contains(joined, "mov_text") {
		t.Fatalf("expected one converted subtitle track for MP4: %s", joined)
	}
}

func TestFFmpegJobWholeFile(t *testing.T) {
	cases := map[string]bool{
		"http://127.0.0.1:5000/hash":                 true,
		"http://localhost:5000/hash":                 true,
		"https://cdn.test/ep.mkv":                    true,
		"https://cdn.test/ep.MP4?token=x":            true,
		"https://cdn.test/master.m3u8":               false,
		"https://cdn.test/playlist?id=3":             false,
		"https://cdn.test/seg/index.m3u8?a=b":        false,
		"http://127.0.0.1:5000/stream/1/master.m3u8": false,
	}
	for stream, want := range cases {
		if got := (ffmpegJob{Stream: stream}).wholeFile(); got != want {
			t.Fatalf("wholeFile(%q) = %v, want %v", stream, got, want)
		}
	}
}

// Some CDNs refuse segments without an Origin header; the provider's extra
// headers are sent with every input, in a stable order, and a value carrying a
// line break is dropped rather than allowed to inject a header of its own.
func TestBuildFFmpegArgsSendsProviderHeaders(t *testing.T) {
	job := ffmpegJob{
		Stream:   "https://cdn.test/x.m3u8",
		Referrer: "https://player.test/",
		Headers: map[string]string{
			"Origin":     "https://player.test",
			"Referer":    "https://ignored.test/",
			"User-Agent": "UA",
			"X-Evil":     "a\r\nCookie: stolen",
		},
		Subtitle: "https://cdn.test/subs.vtt",
		Output:   "/tmp/o.mkv",
	}
	args := job.args()
	want := "Referer: https://player.test/\r\nOrigin: https://player.test\r\nUser-Agent: UA\r\n"
	count := 0
	for i, arg := range args {
		if arg == "-headers" {
			count++
			if args[i+1] != want {
				t.Fatalf("headers = %q, want %q", args[i+1], want)
			}
		}
	}
	if count != 2 {
		t.Fatalf("expected headers before both inputs, got %d", count)
	}
}

func TestDownloadInfoRoundTrip(t *testing.T) {
	previous := downloadSkipTimes
	downloadSkipTimes = func(Config, *Anime, int, string) SkipTimes {
		return SkipTimes{Op: Skip{Start: 90, End: 180}, Ed: Skip{Start: 1300, End: 1390}}
	}
	t.Cleanup(func() { downloadSkipTimes = previous })

	video := filepath.Join(t.TempDir(), "Demo Show - Episode 03 (sub).mkv")
	anime := &Anime{Title: AnimeTitle{English: "Demo Show"}, AnilistId: 21, MalId: 20, TotalEpisodes: 12, ProviderName: "anipub", ProviderId: "77"}
	writeDownloadInfo(Config{}, anime, 3, "sub", "anipub", video)

	if DownloadInfoPath(video) != strings.TrimSuffix(video, ".mkv")+".otakase.json" {
		t.Fatalf("unexpected info path %q", DownloadInfoPath(video))
	}
	info, err := ReadDownloadInfo(video)
	if err != nil {
		t.Fatal(err)
	}
	if info.AnilistID != 21 || info.MalID != 20 || info.Episode != 3 || info.Mode != "sub" || info.TotalEpisodes != 12 {
		t.Fatalf("unexpected info %+v", info)
	}
	if info.Provider != "anipub" || info.ProviderID != "77" || info.File != filepath.Base(video) {
		t.Fatalf("unexpected source %+v", info)
	}
	if info.SkipTimes.Op.End != 180 || info.SkipTimes.Ed.Start != 1300 {
		t.Fatalf("skip times not kept: %+v", info.SkipTimes)
	}

	// The id is the show's id on its own provider, meaningless on a fallback.
	writeDownloadInfo(Config{}, anime, 3, "sub", "anizone", video)
	if info, _ := ReadDownloadInfo(video); info.ProviderID != "" {
		t.Fatalf("a fallback provider must not carry the stored id, got %q", info.ProviderID)
	}
}

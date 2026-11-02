package internal

import (
	"strings"
	"testing"

	"github.com/thexykril/otakase/internal/cast"
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

func TestResolveDownloadDir(t *testing.T) {
	if got := ResolveDownloadDir(&Config{DownloadDir: "/explicit/path"}); got != "/explicit/path" {
		t.Fatalf("got %q", got)
	}
	t.Setenv("OTAKASE_TEST_DL", "/from/env")
	if got := ResolveDownloadDir(&Config{DownloadDir: "$OTAKASE_TEST_DL/x"}); got != "/from/env/x" {
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
	// -extension_picky itself is left out for an ffmpeg too old to know it.
	if strings.Count(joined, "-extension_picky") > 1 {
		t.Fatalf("HLS options must apply only to the stream input, got: %s", joined)
	}
	if strings.Count(joined, "-allowed_extensions") != 1 {
		t.Fatalf("HLS options must apply only to the stream input, got: %s", joined)
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
	for _, want := range []string{"-map 0:v:0", "-map 0:a:0", "-map 1:0", strings.Join(cast.HLSInputArgs(), " "), "aac_adtstoasc"} {
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

package cast

import (
	"strings"
	"testing"
)

// Trimmed from what ffprobe reports for a real anizone episode: one video per
// variant, and one audio per language, Japanese marked default.
const multiAudioProbe = `{
  "streams": [
    {"index": 0, "codec_type": "audio", "disposition": {"default": 0}, "tags": {"language": "hi"}},
    {"index": 1, "codec_type": "audio", "disposition": {"default": 0}, "tags": {"language": "en"}},
    {"index": 2, "codec_type": "audio", "disposition": {"default": 1}, "tags": {"language": "ja"}},
    {"index": 3, "codec_type": "video", "width": 1280, "height": 720},
    {"index": 4, "codec_type": "video", "width": 1920, "height": 1080},
    {"index": 5, "codec_type": "video", "width": 640, "height": 360}
  ],
  "format": {"duration": "1421.500000"}
}`

func TestParseProbedStreamReadsTracksAndLength(t *testing.T) {
	info, err := ParseProbedStream([]byte(multiAudioProbe))
	if err != nil {
		t.Fatal(err)
	}
	if info.Duration != 1421.5 {
		t.Errorf("duration %v, want 1421.5", info.Duration)
	}
	if len(info.Audio) != 3 || len(info.Video) != 3 {
		t.Fatalf("got %d audio and %d video tracks, want 3 and 3", len(info.Audio), len(info.Video))
	}
	if !info.Audio[2].Default || info.Audio[2].Language != "ja" {
		t.Errorf("the default Japanese track was not read: %+v", info.Audio[2])
	}
}

// A dub asked for from a stream that carries every language gets the English
// track -- not the default Japanese one ffmpeg would pick on its own -- and the
// sharpest video, because naming one stream stops ffmpeg choosing any other.
func TestSelectTracksPicksTheRequestedLanguage(t *testing.T) {
	info, _ := ParseProbedStream([]byte(multiAudioProbe))

	dub := SelectTracks(info, "en")
	if got := strings.Join(dub.Maps, " "); got != "-map 0:4 -map 0:1" {
		t.Errorf("dub maps %q, want the 1080p video and the English audio", got)
	}
	if dub.AudioLanguage != "en" {
		t.Errorf("dub audio language %q, want en", dub.AudioLanguage)
	}

	sub := SelectTracks(info, "ja")
	if got := strings.Join(sub.Maps, " "); got != "-map 0:4 -map 0:2" {
		t.Errorf("sub maps %q, want the Japanese audio", got)
	}
}

// With no track in the language asked for, the one the host marks default
// plays, and the selection says what it is so the caller can act on it.
func TestSelectTracksFallsBackToTheDefault(t *testing.T) {
	info, _ := ParseProbedStream([]byte(multiAudioProbe))
	got := SelectTracks(info, "de")
	if got.AudioLanguage != "ja" || !strings.Contains(strings.Join(got.Maps, " "), "0:2") {
		t.Errorf("expected the default Japanese track, got %+v", got)
	}
}

// A stream with one audio track has nothing to choose, and ffmpeg's own
// selection is left alone.
func TestSelectTracksLeavesASingleAudioStreamAlone(t *testing.T) {
	info, _ := ParseProbedStream([]byte(`{"streams":[
		{"index":0,"codec_type":"video","width":1920,"height":1080},
		{"index":1,"codec_type":"audio","tags":{"language":"eng"}}]}`))
	got := SelectTracks(info, "ja")
	if len(got.Maps) != 0 {
		t.Errorf("mapped a stream with nothing to choose: %v", got.Maps)
	}
	if got.AudioLanguage != "en" {
		t.Errorf("the language of the only track was not reported, normalised: %q", got.AudioLanguage)
	}
}

func TestSelectTracksReportsNothingForAnUntaggedStream(t *testing.T) {
	info, _ := ParseProbedStream([]byte(`{"streams":[
		{"index":0,"codec_type":"video","width":1920,"height":1080},
		{"index":1,"codec_type":"audio"}]}`))
	if got := SelectTracks(info, "en"); got.AudioLanguage != "" || len(got.Maps) != 0 {
		t.Errorf("expected nothing known, got %+v", got)
	}
}

func TestTheMapsReachFFmpegAfterTheInput(t *testing.T) {
	maps := []string{"-map", "0:4", "-map", "0:1"}
	for name, args := range map[string][]string{
		"remux": BuildRemuxArgsFrom("https://host/master.m3u8", "", "/tmp/out", 0, maps, nil),
		"burn":  BuildBurnArgsFrom("https://host/master.m3u8", "", "/tmp/s.vtt", "/tmp/out", Encoder{Name: "libx264"}, 0, maps, nil),
	} {
		joined := strings.Join(args, " ")
		input := strings.Index(joined, "-i https://host/master.m3u8")
		mapped := strings.Index(joined, "-map 0:4 -map 0:1")
		if input < 0 || mapped < input {
			t.Errorf("%s: the maps are missing or precede the input:\n%s", name, joined)
		}
	}
}

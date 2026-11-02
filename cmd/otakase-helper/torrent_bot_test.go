package main

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBotTrackSelection(t *testing.T) {
	var p botProbe
	if err := json.Unmarshal([]byte(`{"format":{"duration":"12"},"streams":[{"index":0,"codec_type":"video"},{"index":1,"codec_type":"audio","tags":{"language":"eng"}},{"index":2,"codec_type":"audio","tags":{"language":"jpn"}},{"index":3,"codec_type":"subtitle","codec_name":"ass","tags":{"language":"eng","title":"Signs"},"disposition":{"default":1}},{"index":4,"codec_type":"subtitle","codec_name":"ass","tags":{"language":"eng","title":"Dialogue"}}]}`), &p); err != nil {
		t.Fatal(err)
	}
	sub, err := chooseBotMedia(p, "sub")
	if err != nil || sub.audio != 2 || sub.subtitle != 1 {
		t.Fatalf("sub: %+v %v", sub, err)
	}
	dub, err := chooseBotMedia(p, "dub")
	if err != nil || dub.audio != 1 || dub.subtitle != 0 {
		t.Fatalf("dub: %+v %v", dub, err)
	}
	p.Streams = p.Streams[:4]
	if _, err := chooseBotMedia(p, "sub"); err == nil {
		t.Fatal("signs-only subtitles accepted as dialogue")
	}
	p.Streams = p.Streams[:1]
	if _, err := chooseBotMedia(p, "dub"); err == nil {
		t.Fatal("missing audio accepted")
	}
	p.Format.Duration = "NaN"
	if _, err := chooseBotMedia(p, "sub"); err == nil {
		t.Fatal("invalid duration accepted")
	}
}

func TestBotSubtitlesPartial(t *testing.T) {
	dir := t.TempDir()
	s := &botSubtitles{path: filepath.Join(dir, "subtitles.vtt")}
	os.WriteFile(s.path, []byte("WEBVTT\n\n00:00.000 --> 00:01.000\nOne\n\n00:02.000 --> 00:03.000\nTw"), 0600)
	data, done := s.body()
	want := "WEBVTT\n\n" + botSubtitlesPartial + "\n\n00:00.000 --> 00:01.000\nOne\n\n"
	if done || string(data) != want {
		t.Fatalf("partial body %q", data)
	}
	// Nothing converted yet: still a valid, partial file.
	os.WriteFile(s.path, nil, 0600)
	if data, done = s.body(); done || string(data) != "WEBVTT\n\n"+botSubtitlesPartial+"\n\n" {
		t.Fatalf("empty body %q", data)
	}
	os.WriteFile(s.path, []byte("WEBVTT\n\n00:00.000 --> 00:01.000\nOne\n\n00:02.000 --> 00:03.000\nTw"), 0600)
	s.done = true
	if data, done = s.body(); !done || strings.Contains(string(data), botSubtitlesPartial) || !strings.HasSuffix(string(data), "Tw") {
		t.Fatalf("finished body %q", data)
	}
}

func TestBotAudioLanguageCodes(t *testing.T) {
	for _, tc := range []struct{ raw, expected string }{
		{"jpn", "ja"}, {"eng", "en"}, {"deu", "de"}, {"spa", "es"},
		{"fra", "fr"}, {"hin", "hi"}, {"ita", "it"}, {"por", "pt"},
		{"tam", "ta"}, {"tel", "te"}, {"zh-CN", "zh"},
		{"und", "track-7"}, {"", "track-7"},
	} {
		if got := botAudioLanguage(tc.raw, 7); got != tc.expected {
			t.Errorf("botAudioLanguage(%q) = %q, want %q", tc.raw, got, tc.expected)
		}
	}
}

func TestBotOutputLimit(t *testing.T) {
	output := &botLimitedOutput{limit: 4}
	if _, err := io.Copy(output, strings.NewReader("oversized")); err == nil {
		t.Fatal("output limit bypassed")
	}
	if output.buffer.Len() > 4 {
		t.Fatal("oversized output retained")
	}
}

func TestBotSegmentsJoinShortTail(t *testing.T) {
	// A 40.04 s episode once ended on a 0.04 s segment ffmpeg wrote nothing
	// for, and the player stopped with an error.
	s := newBotSegments(context.Background(), t.TempDir(), "x", botMedia{duration: 40.04}, "sub", nil)
	if s.segments() != 10 || math.Abs(s.length(9)-4.04) > 0.001 || strings.Contains(s.playlist(), "seg10.ts") {
		t.Fatalf("%d segments, last %v s:\n%s", s.segments(), s.length(9), s.playlist())
	}
	if s.duration(0) != "4" || s.duration(9) != "5.040" {
		t.Fatalf("ffmpeg lengths %s %s", s.duration(0), s.duration(9))
	}
	s.media.duration = 41.5
	if s.segments() != 11 || math.Abs(s.length(10)-1.5) > 0.001 {
		t.Fatalf("%d segments, last %v s", s.segments(), s.length(10))
	}
}

func TestBotBufferTarget(t *testing.T) {
	// A 24 minute episode of 1440 MB needs 1 MB a second.
	const mb = 1 << 20
	for _, c := range []struct {
		speed, want float64
	}{
		{-1, 90 * mb},        // speed unknown
		{2 * mb, 60 * mb},    // fast: a minute ahead
		{0.6 * mb, 720 * mb}, // slow: (1.1 - 0.6) MB/s for 1440 s
		{0, 1440 * mb},       // stalled: the whole episode at most
	} {
		if got := botBufferTarget(mb, c.speed, 1440, 1440*mb); math.Abs(got-c.want) > mb {
			t.Errorf("speed %.0f: target %.0f MB, want %.0f MB", c.speed/mb, got/mb, c.want/mb)
		}
	}
}

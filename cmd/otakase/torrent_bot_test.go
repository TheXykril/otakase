package main

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
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

func TestBotSegmentsIntegration(t *testing.T) {
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skip(name + " unavailable")
		}
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "source.mkv")
	subtitles := filepath.Join(dir, "dialogue.srt")
	os.WriteFile(subtitles, []byte("1\n00:00:08,000 --> 00:00:11,500\nLate subtitle\n"), 0600)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	args := []string{"-v", "error", "-f", "lavfi", "-i", "color=black:s=320x180:r=24:d=12", "-f", "lavfi", "-i", "sine=frequency=440:duration=12", "-f", "lavfi", "-i", "sine=frequency=880:duration=12", "-f", "lavfi", "-i", "sine=frequency=660:duration=12", "-i", subtitles, "-map", "0:v", "-map", "1:a", "-map", "2:a", "-map", "3:a", "-map", "4:s", "-c:v", "libx264", "-threads", "1", "-c:a", "aac", "-c:s", "srt", "-metadata:s:a:0", "language=eng", "-metadata:s:a:1", "language=jpn", "-metadata:s:a:2", "language=deu", "-metadata:s:s:0", "language=eng", source}
	if out, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	m, err := probeBotMedia(ctx, source, "sub", 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if m.audio != 2 || m.subtitle != 0 {
		t.Fatalf("wrong tracks: %+v", m)
	}
	s := newBotSegments(ctx, dir, source, m, "sub", nil)
	s.burnFile = "source.mkv"
	if !strings.Contains(s.playlist(), "#EXT-X-ENDLIST") || !strings.Contains(s.playlist(), "seg2.ts") {
		t.Fatal("incomplete VOD playlist")
	}
	probe := func(data []byte) (start, duration float64, streams string) {
		t.Helper()
		path := filepath.Join(dir, "segment.ts")
		os.WriteFile(path, data, 0600)
		out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=start_time,duration:stream=codec_type", "-of", "json", path).Output()
		if err != nil {
			t.Fatal(err)
		}
		var p struct {
			Streams []struct {
				Type string `json:"codec_type"`
			} `json:"streams"`
			Format struct {
				Start    string `json:"start_time"`
				Duration string `json:"duration"`
			} `json:"format"`
		}
		json.Unmarshal(out, &p)
		start, _ = strconv.ParseFloat(p.Format.Start, 64)
		duration, _ = strconv.ParseFloat(p.Format.Duration, 64)
		for _, stream := range p.Streams {
			streams += stream.Type + " "
		}
		return start, duration, strings.TrimSpace(streams)
	}
	// Request the final segment first, then seek backward.
	for _, n := range []int{2, 0, 1, 2} {
		data, err := s.video(ctx, n, m.subtitle)
		if err != nil {
			t.Fatal(err)
		}
		start, duration, streams := probe(data)
		if math.Abs(start-float64(n*4)) > 0.1 || math.Abs(duration-4) > 0.1 || streams != "video" {
			t.Fatalf("video segment %d: start %v duration %v streams %q", n, start, duration, streams)
		}
		audio, err := s.audio(ctx, n, m.audio)
		if err != nil {
			t.Fatal(err)
		}
		// Audio and video segments must line up for the player to mix them.
		if start, duration, streams = probe(audio); math.Abs(start-float64(n*4)) > 0.1 || math.Abs(duration-4) > 0.15 || streams != "audio" {
			t.Fatalf("audio segment %d: start %v duration %v streams %q", n, start, duration, streams)
		}
		if n == 2 {
			// A white subtitle must be present on the otherwise black frame after seeking.
			// probe left the audio segment behind; check the picture.
			os.WriteFile(filepath.Join(dir, "segment.ts"), data, 0600)
			pixels, err := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-i", filepath.Join(dir, "segment.ts"), "-ss", "1", "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "gray", "pipe:1").Output()
			if err != nil {
				t.Fatal(err)
			}
			bright := 0
			for _, v := range pixels {
				if v > 150 {
					bright++
				}
			}
			if bright < 20 {
				t.Fatal("embedded subtitle missing after seek")
			}
		}
	}
	dub, err := probeBotMedia(ctx, source, "dub", 20*time.Second)
	if err != nil || dub.audio != 1 {
		t.Fatalf("dub track: %+v %v", dub, err)
	}
	primary, variants, err := probeBotMediaVariants(ctx, source, "sub", 20*time.Second)
	if err != nil || primary.audio != m.audio {
		t.Fatalf("multi-audio probe: %+v %v", primary, err)
	}
	if variants["ja"].audio != 2 || variants["en"].audio != 1 || variants["de"].audio != 3 {
		t.Fatalf("missing or wrong audio languages: %+v", variants)
	}
	// Soft subtitles: the video is encoded once for every language.
	for language, track := range variants {
		track.subtitle = -1
		variants[language] = track
	}
	primary.subtitle = -1
	s = newBotSegments(ctx, dir, source, primary, "sub", variants)
	get := func(route string) *httptest.ResponseRecorder {
		res := httptest.NewRecorder()
		s.ServeHTTP(res, httptest.NewRequest(http.MethodGet, route, nil))
		return res
	}
	for _, language := range []string{"sub", "dub", "ja", "en", "de"} {
		if list := get("/" + language + "/playlist.m3u8"); list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "seg0.ts") {
			t.Fatalf("%s playlist returned %d %q", language, list.Code, list.Body.String())
		}
		res := get("/" + language + "/seg1.ts")
		if res.Code != http.StatusOK {
			t.Fatalf("%s segment returned %d", language, res.Code)
		}
		start, duration, streams := probe(res.Body.Bytes())
		if math.Abs(start-4) > 0.1 || math.Abs(duration-4) > 0.15 || streams != "video audio" {
			t.Fatalf("%s segment: start %v duration %v streams %q", language, start, duration, streams)
		}
	}
	if res := get("/playlist.m3u8"); !strings.Contains(res.Body.String(), "seg0.ts") {
		t.Fatalf("primary playlist %q", res.Body.String())
	}
	// Five language routes share one encoded picture: one video and three
	// distinct audio streams for the segment.
	videos, audios := 0, 0
	for key := range s.cache {
		if strings.HasPrefix(key, "v") {
			videos++
		} else {
			audios++
		}
	}
	if videos != 1 || audios != 3 {
		t.Fatalf("cache holds %d videos and %d audios", videos, audios)
	}
	for _, route := range []string{"/xx/playlist.m3u8", "/ja/seg100.ts", "/ja/seg01.ts", "/ja/a0.ts", "/seg-1.ts", "/subs/en.vtt"} {
		if res := get(route); res.Code != http.StatusNotFound {
			t.Fatalf("%s returned %d", route, res.Code)
		}
	}
	if _, err := s.video(ctx, 100, -1); err == nil {
		t.Fatal("out-of-range segment accepted")
	}
	if _, err := s.video(ctx, 0, 0); err == nil {
		t.Fatal("burn-in accepted without a local file")
	}
	subs := followBotSubtitles(ctx, source, dir, m.subtitle)
	if subs == nil {
		t.Fatal("subtitle conversion did not start")
	}
	s.subs = subs
	deadline := time.Now().Add(20 * time.Second)
	for {
		data, done := subs.body()
		if done {
			if !strings.Contains(string(data), "Late subtitle") || strings.Contains(string(data), botSubtitlesPartial) {
				t.Fatalf("subtitle conversion failed: %q", data)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("subtitle conversion never finished")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if res := get("/subs/en.vtt"); res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "Late subtitle") {
		t.Fatalf("subtitle route returned %d", res.Code)
	}
	if followBotSubtitles(ctx, source, dir, 5) != nil {
		t.Fatal("missing subtitle track accepted")
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

// The torrent helper streams through a loopback HTTP server; ffmpeg must
// play from it the same way it plays a local file.
func TestBotSegmentsOverHTTP(t *testing.T) {
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skip(name + " unavailable")
		}
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "source.mkv")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	args := []string{"-v", "error", "-f", "lavfi", "-i", "color=black:s=320x180:r=24:d=10", "-f", "lavfi", "-i", "sine=frequency=440:duration=10", "-map", "0:v", "-map", "1:a", "-c:v", "libx264", "-threads", "1", "-c:a", "aac", "-metadata:s:a:0", "language=jpn", source}
	if out, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	server := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer server.Close()
	url := server.URL + "/source.mkv"
	primary, variants, err := probeBotMediaVariants(ctx, url, "dub", 20*time.Second)
	if err == nil {
		t.Fatalf("dub accepted without English audio: %+v", primary)
	}
	m, err := probeBotMedia(ctx, url, "sub", 20*time.Second)
	if err == nil {
		t.Fatalf("sub accepted without subtitles: %+v %v", m, variants)
	}
	m = botMedia{duration: 10, video: 0, audio: 1, subtitle: -1}
	s := newBotSegments(ctx, dir, url, m, "sub", map[string]botMedia{"ja": m})
	if _, err := s.segment(ctx, 2, "ja"); err != nil {
		t.Fatal(err)
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

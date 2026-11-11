package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTitleArgs(t *testing.T) {
	for arg, want := range map[string][3]any{
		"tt0133093":     {"tt0133093", 0, 0},
		"tt0903747:2:5": {"tt0903747", 2, 5},
	} {
		imdb, season, episode, err := parseTitleArg(arg)
		if err != nil || imdb != want[0] || season != want[1] || episode != want[2] {
			t.Errorf("%s: %s %d %d %v", arg, imdb, season, episode, err)
		}
	}
	for _, bad := range []string{"", "0133093", "tt1:1", "tt0903747:1:0", "tt0903747; rm"} {
		if _, _, _, err := parseTitleArg(bad); err == nil {
			t.Errorf("%q was taken", bad)
		}
	}
}

func TestTitleStreamInfoInlinesSubtitles(t *testing.T) {
	srt := filepath.Join(t.TempDir(), "a.srt")
	os.WriteFile(srt, []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n"), 0o644)
	old := titleResolve
	t.Cleanup(func() { titleResolve = old })
	var asked map[string]any
	titleResolve = func(request string) string {
		json.Unmarshal([]byte(request), &asked)
		b, _ := json.Marshal(map[string]any{"version": 1, "data": map[string]any{"stream": map[string]any{
			"url": "https://cdn.example/a.m3u8", "hls": true, "referrer": "https://vidsrc.example/",
			"subtitles": []map[string]string{{"url": srt, "language": "English", "label": "a"}, {"url": "/gone.srt", "language": "English"}},
		}}})
		return string(b)
	}
	info, err := titleStreamInfo("tt0903747:1:2")
	if err != nil {
		t.Fatal(err)
	}
	movie := asked["movie"].(map[string]any)
	if movie["path"] != "tt0903747" || movie["series"] != true || asked["season"] != 1.0 || asked["episode"] != 2.0 {
		t.Errorf("asked %v", asked)
	}
	if len(info.Streams) != 1 || !info.Streams[0].HLS || len(info.Streams[0].Subtitles) != 1 ||
		!strings.HasPrefix(info.Streams[0].Subtitles[0].URL, "data:text/plain;base64,") || info.Streams[0].Subtitles[0].Language != "en" {
		t.Fatalf("%+v", info)
	}
	titleResolve = func(string) string {
		return `{"version":1,"error":{"code":"no_stream","message":"No server played this title.","retryable":true}}`
	}
	if _, err := titleStreamInfo("tt0133093"); err == nil || err.Error() != "No server played this title" {
		t.Errorf("error: %v", err)
	}
}

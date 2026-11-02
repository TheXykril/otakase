package main

import "testing"

func TestMovieRenderPercent(t *testing.T) {
	for _, c := range []struct {
		line string
		want int
		ok   bool
	}{
		{"out_time_us=50000000", 50, true},
		{"out_time_us=100000000", 99, true},
		{"out_time_us=N/A", 0, false},
		{"frame=12", 0, false},
	} {
		got, ok := movieRenderPercent(c.line, 100)
		if got != c.want || ok != c.ok {
			t.Errorf("%q: %d %v", c.line, got, ok)
		}
	}
	for _, path := range []string{"/playlist.m3u8", "/seg0.ts", "/seg25.ts"} {
		if !movieBotFile.MatchString(path) {
			t.Errorf("%s refused", path)
		}
	}
	for _, path := range []string{"/credits.ass", "/music.wav", "/seg.ts", "/../x", "/anilist_token.json"} {
		if movieBotFile.MatchString(path) {
			t.Errorf("%s served", path)
		}
	}
}

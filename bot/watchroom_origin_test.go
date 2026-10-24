package main

import "testing"

func TestWatchPlaybackHost(t *testing.T) {
	cases := []struct {
		link string
		want string
	}{
		{"https://media.example.org/path/playlist.m3u8?token=private", "media.example.org"},
		{"http://127.0.0.1:8080/playlist.m3u8?languages=sub,dub", "Local relay"},
		{"http://[::1]:8080/playlist.m3u8", "Local relay"},
		{"http://localhost:8080/video", "Local relay"},
		{"not-a-url", ""},
	}
	for _, tc := range cases {
		if got := watchPlaybackHost(tc.link); got != tc.want {
			t.Errorf("watchPlaybackHost(%q) = %q, want %q", tc.link, got, tc.want)
		}
	}
}

func TestWatchPlaybackOriginFailsClosedWithoutDiscord(t *testing.T) {
	b := &bot{}
	if b.watchCanInspectPlayback("member") || b.watchCanInspectPlayback("") {
		t.Fatal("playback provider details must stay private without a guild session")
	}
}

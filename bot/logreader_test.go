package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestDiagnoseLog(t *testing.T) {
	cases := map[string][]string{
		// Debug log lines are JSON strings, so quotes arrive escaped.
		`[LOG] 2026/10/11 12:00:01 /src/internal/player.go:551: "Player resolution failed for 'mpv': configured player \"mpv\" was not found and fallback to mpv failed: exec: \"mpv\": executable file not found in $PATH"`: {"mpv"},
		"Error: Failed to resolve media player":                                                            {"mpv"},
		`exec: "mpv.exe": executable file not found in %PATH%`:                                             {"mpv"},
		`[LOG] 2026/10/11 12:00:01 player.go:599: "Failed to connect to MPV socket after 10 attempts"`:     {"mpv-socket"},
		"\nNo providers are currently enabled.\n":                                                          {"no-providers"},
		`"anidb.app returned a Cloudflare challenge"`:                                                      {"blocked"},
		`anizone request failed with status 429: slow down`:                                                {"blocked"},
		"Playback failed to start with anipub. Trying another provider...\nNo more providers left to try.": {"no-stream"},
		`kickassanime: request failed: Get "https://kaa.to/api": dial tcp: lookup kaa.to: no such host`:    {"network"},
		`Get "https://x": dial tcp 1.2.3.4:443: i/o timeout`:                                               {"network"},
		"Failed to get user data\nYou can reset the token by running `otakase -change-token`":              {"anilist-token"},
		`"AniList token invalid (invalid token), starting browser authentication"`:                         {"anilist-token"},
		"MyAnimeList sync needs MAL application credentials.":                                              {"mal-credentials"},
		"Wrong match is common — search again and pick the right title.":                                   {"wrong-match"},
		`failed to run Rofi: exec: \"rofi\": executable file not found in $PATH`:                           {"rofi"},
		"Downloading needs ffmpeg, which was not found.":                                                   {"ffmpeg"},
		"No cast devices found.":                                                                           {"cast"},
		"Playing episode 3 (sub)\nEpisode completed! Progress updated: 3":                                  {},
		// The mpv socket going away when the player closes is normal.
		`"dial unix /tmp/otakasempvsocket: connect: connection refused"`: {},
	}
	for text, want := range cases {
		var got []string
		for _, h := range diagnoseLog(text) {
			got = append(got, h.Key)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%q: got %v, want %v", text, got, want)
		}
	}
}

func TestDiagnoseLogLimit(t *testing.T) {
	text := "No providers are currently enabled\nNo cast devices found\nDownloading needs ffmpeg\nfailed to run Rofi\nFailed to get user data"
	if got := diagnoseLog(text); len(got) != maxLogHints {
		t.Fatalf("got %d hints, want %d", len(got), maxLogHints)
	}
}

func TestLogHintsComplete(t *testing.T) {
	keys := map[string]bool{}
	for _, h := range logHints {
		if keys[h.Key] {
			t.Errorf("duplicate key %q", h.Key)
		}
		keys[h.Key] = true
		if h.Title == "" || h.Fix == "" || len(h.Fix) > 1024 {
			t.Errorf("%s: title or fix missing, or fix over Discord's field limit", h.Key)
		}
	}
}

func TestLooksLikeLog(t *testing.T) {
	yes := []string{
		"here:\n```\nError: Failed to start mpv process\n```",
		"[LOG] 2026/10/11 12:00:01 /src/internal/player.go:676: \"Failed to start mpv\"",
		"my otakase-debug.log says nothing useful",
		"error: no episode links found",
	}
	no := []string{
		"it says mpv not found when I play",
		"nothing plays, any idea? there was an error",
	}
	for _, s := range yes {
		if !looksLikeLog(s) {
			t.Errorf("%q should look like a log", s)
		}
	}
	for _, s := range no {
		if looksLikeLog(s) {
			t.Errorf("%q should not look like a log", s)
		}
	}
}

func TestLogAttachment(t *testing.T) {
	cases := []struct {
		a    discordgo.MessageAttachment
		want bool
	}{
		{discordgo.MessageAttachment{Filename: "otakase-debug.log", Size: 4000}, true},
		{discordgo.MessageAttachment{Filename: "paste.txt", Size: 10}, true},
		{discordgo.MessageAttachment{Filename: "message.txt", ContentType: "text/plain; charset=utf-8", Size: 10}, true},
		{discordgo.MessageAttachment{Filename: "screenshot.png", ContentType: "image/png", Size: 4000}, false},
		{discordgo.MessageAttachment{Filename: "huge.log", Size: 9 << 20}, false},
	}
	for _, c := range cases {
		if got := logAttachment(&c.a); got != c.want {
			t.Errorf("%s: got %v, want %v", c.a.Filename, got, c.want)
		}
	}
}

func TestFetchLogKeepsTail(t *testing.T) {
	body := strings.Repeat("x", logTail) + "\nNo cast devices found."
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()
	got := fetchLog(srv.URL)
	if len(got) != logTail || !strings.HasSuffix(got, "No cast devices found.") {
		t.Fatalf("got %d bytes, want the last %d", len(got), logTail)
	}
}

func TestLogReaderOncePerPost(t *testing.T) {
	r := newLogReader()
	hints := diagnoseLog("No cast devices found\nfailed to run Rofi")
	if got := r.fresh("post", hints); len(got) != 2 {
		t.Fatalf("first: got %d", len(got))
	}
	if got := r.fresh("post", diagnoseLog("No cast devices found")); len(got) != 0 {
		t.Fatalf("repeat: got %d", len(got))
	}
	if got := r.fresh("other", hints); len(got) != 2 {
		t.Fatalf("other post: got %d", len(got))
	}
}

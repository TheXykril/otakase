package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMentionedVersion(t *testing.T) {
	cases := map[string]string{
		"Otakase version: 26.5.0\nit crashes on play": "26.5.0",
		"I'm on v2.2.1 on arch":                       "2.2.1",
		"otakase 26.1.2 won't start":                  "26.1.2",
		"ver 26.6.0":                                  "26.6.0",
		"mpv version 0.41.0, otakase 26.7.0":          "26.7.0",
		"mpv v0.38.0 and windows 10.0.19045":          "",
		"episode 12 is broken":                        "",
		"server 1.2.3 is down":                        "",
	}
	for text, want := range cases {
		got, ok := mentionedVersion(text)
		if got != want || ok != (want != "") {
			t.Errorf("mentionedVersion(%q) = %q, %v; want %q", text, got, ok, want)
		}
	}
}

func TestOlderVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"26.5.0", "26.7.0", true},
		{"2.2.2", "26.6.0", true},
		{"26.6.9", "26.7.0", true},
		{"26.7.0", "26.7.0", false},
		{"26.7.1", "26.7.0", false},
		{"27.0.0", "26.10.3", false},
		{"26.10.0", "26.9.0", false},
	}
	for _, c := range cases {
		if got := olderVersion(c.a, c.b); got != c.want {
			t.Errorf("olderVersion(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

func TestLatestVersion(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"tag_name":"v26.6.0"}`))
	}))
	defer srv.Close()
	old := releasesLatestURL
	releasesLatestURL = srv.URL
	versionCheck.latest = ""
	defer func() { releasesLatestURL = old; versionCheck.latest = "" }()

	for k := 0; k < 2; k++ {
		v, err := latestVersion("")
		if err != nil || v != "26.6.0" {
			t.Fatalf("latestVersion = %q, %v", v, err)
		}
	}
	if calls != 1 {
		t.Errorf("GitHub asked %d times, want 1 (cached)", calls)
	}
	e := outdatedEmbed("26.5.0", "26.6.0")
	if !strings.Contains(e.Description, "26.5.0") || !strings.Contains(e.Description, "otakase -u") {
		t.Errorf("embed misses version or update command: %q", e.Description)
	}
}

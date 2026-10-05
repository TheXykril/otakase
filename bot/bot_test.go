package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAutoRules(t *testing.T) {
	cases := map[string]string{
		"it says mpv not found when I play":                 "mpv",
		"the menu icons are boxes for me":                   "icons",
		"nothing found when I search for frieren":           "nothing-found",
		"my anilist progress doesn't update after watching": "sync",
		"chromecast not showing up at all":                  "cast",
		"how do i update otakase on windows":                "install",
		"just watched the new episode, great stuff":         "",
	}
	for text, want := range cases {
		a := newAutoReplier()
		f, ok := a.match("c", text)
		if got := map[bool]string{true: f.Key, false: ""}[ok]; got != want {
			t.Errorf("%q: got %q, want %q", text, got, want)
		}
	}
}

func TestAutoReplyCooldown(t *testing.T) {
	a := newAutoReplier()
	if _, ok := a.match("c", "it says mpv not found"); !ok {
		t.Fatal("first match missing")
	}
	if _, ok := a.match("c", "still mpv not found"); ok {
		t.Fatal("second match within cooldown")
	}
	if _, ok := a.match("other", "mpv not found here too"); !ok {
		t.Fatal("other channel should still match")
	}
}

func TestRulesPointAtFAQ(t *testing.T) {
	for _, r := range autoRules {
		if _, ok := faqByKey(r.faq); !ok {
			t.Errorf("rule points at missing FAQ %q", r.faq)
		}
	}
	for _, k := range []string{"linux", "arch", "windows", "mac"} {
		if installGuides[k].Body == "" {
			t.Errorf("no install guide for %s", k)
		}
	}
}

func TestLoadEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(path, []byte("# comment\nOTK_A=\"one\"\nOTK_B = two\nbad line\n"), 0o600)
	t.Setenv("OTK_B", "kept")
	t.Setenv("OTK_A", "")
	os.Unsetenv("OTK_A")
	loadEnvFile(path)
	if got := os.Getenv("OTK_A"); got != "one" {
		t.Errorf("OTK_A = %q", got)
	}
	if got := os.Getenv("OTK_B"); got != "kept" {
		t.Errorf("OTK_B = %q, want existing value kept", got)
	}
}

package main

import (
	"testing"
	"time"

	"github.com/thexykril/otakase/internal"
)

func TestLaunchLinks(t *testing.T) {
	cases := []struct {
		name       string
		open, flag string
		args       []string
		wantOpen   string
		wantParty  string
	}{
		{name: "nothing", wantOpen: "", wantParty: ""},
		{name: "open flag", open: " otakase://play/anilist/1 ", wantOpen: "otakase://play/anilist/1"},
		{name: "bare argument", args: []string{"otakase://play/anilist/2?ep=3"}, wantOpen: "otakase://play/anilist/2?ep=3"},
		{name: "other argument", args: []string{"naruto"}},
		{name: "flag wins over argument", open: "otakase://play/anilist/1", args: []string{"otakase://play/anilist/2"}, wantOpen: "otakase://play/anilist/1"},
		{name: "party link", args: []string{"otakase://party/abc"}, wantParty: "otakase://party/abc"},
		{name: "party flag", flag: " abc ", wantParty: "abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			open, party := launchLinks(tc.open, tc.flag, tc.args)
			if open != tc.wantOpen || party != tc.wantParty {
				t.Fatalf("launchLinks = (%q, %q), want (%q, %q)", open, party, tc.wantOpen, tc.wantParty)
			}
		})
	}
}

func TestClampPercent(t *testing.T) {
	for in, want := range map[int]int{-5: 0, 0: 0, 85: 85, 100: 100, 140: 100} {
		if got := clampPercent(in); got != want {
			t.Errorf("clampPercent(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestUseRofi(t *testing.T) {
	cases := []struct {
		name       string
		configured bool
		flags      cliFlags
		goos       string
		want       bool
	}{
		{name: "config", configured: true, goos: "linux", want: true},
		{name: "flag", flags: cliFlags{rofi: true}, goos: "linux", want: true},
		{name: "no-rofi beats config", configured: true, flags: cliFlags{noRofi: true}, goos: "linux"},
		{name: "windows", configured: true, flags: cliFlags{rofi: true}, goos: "windows"},
		{name: "update stays in terminal", configured: true, flags: cliFlags{update: true}, goos: "linux"},
		{name: "edit stays in terminal", flags: cliFlags{rofi: true, editConfig: true}, goos: "linux"},
	}
	for _, tc := range cases {
		if got := useRofi(tc.configured, &tc.flags, tc.goos); got != tc.want {
			t.Errorf("%s: useRofi = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestUseImagePreview(t *testing.T) {
	if !useImagePreview(false, &cliFlags{imagePreview: true}, "linux") {
		t.Error("-image-preview should turn previews on")
	}
	if useImagePreview(true, &cliFlags{noImagePreview: true}, "linux") {
		t.Error("-no-image-preview should beat the config")
	}
	if useImagePreview(true, &cliFlags{imagePreview: true}, "windows") {
		t.Error("previews are never shown on Windows")
	}
	if !useImagePreview(true, &cliFlags{}, "darwin") {
		t.Error("the config should hold without flags")
	}
}

func TestApplyAudioFlags(t *testing.T) {
	cfg := internal.Config{SubOrDub: "dub", SubStyle: "hard"}
	applyAudioFlags(&cfg, &cliFlags{})
	if cfg.SubOrDub != "dub" || cfg.SubOrDubFlag || cfg.SubStyle != "hard" {
		t.Fatalf("no flags should leave the config alone, got %+v", cfg)
	}

	applyAudioFlags(&cfg, &cliFlags{sub: true, dub: true, softSub: true, hardSub: true})
	if cfg.SubOrDub != "sub" || !cfg.SubOrDubFlag || cfg.SubStyle != "soft" {
		t.Fatalf("-sub and -softsub should win when both of a pair are given, got %+v", cfg)
	}
}

func TestRewatchingFinishedShow(t *testing.T) {
	if rewatchingFinishedShow(nil, 12) {
		t.Error("a show not on the list is not a rewatch")
	}
	if !rewatchingFinishedShow(&internal.Entry{Progress: 12, Status: "COMPLETED"}, 12) {
		t.Error("a completed show watched to the end is a rewatch")
	}
	if rewatchingFinishedShow(&internal.Entry{Progress: 12, Status: "CURRENT"}, 12) {
		t.Error("a show still on Watching is not a rewatch")
	}
	if rewatchingFinishedShow(&internal.Entry{Progress: 5, Status: "PAUSED"}, 12) {
		t.Error("a show not watched to the end is not a rewatch")
	}
}

func TestSkippedEpisode(t *testing.T) {
	cfg := &internal.Config{SkipFiller: true}
	if !skippedEpisode(internal.Episode{IsFiller: true}, cfg) {
		t.Error("filler should be skipped with SkipFiller")
	}
	if skippedEpisode(internal.Episode{IsRecap: true}, cfg) {
		t.Error("a recap should play without SkipRecap")
	}
	cfg = &internal.Config{SkipRecap: true}
	if !skippedEpisode(internal.Episode{IsRecap: true}, cfg) {
		t.Error("a recap should be skipped with SkipRecap")
	}
	if skippedEpisode(internal.Episode{}, &internal.Config{SkipFiller: true, SkipRecap: true}) {
		t.Error("an ordinary episode is never skipped")
	}
}

func TestRoundSeconds(t *testing.T) {
	for in, want := range map[float64]int{0: 0, 1.49: 1, 1.5: 2, 1439.7: 1440} {
		if got := roundSeconds(in); got != want {
			t.Errorf("roundSeconds(%v) = %d, want %d", in, got, want)
		}
	}
}

func TestPresenceStateWaitsForDuration(t *testing.T) {
	var p presenceState
	start := time.Unix(1000, 0)

	if send, _, _ := p.observe(3, false, 0, start); send {
		t.Fatal("nothing should be sent before mpv reports a duration")
	}
	// Held back for the keep-alive interval like a real send.
	if send, _, _ := p.observe(5, false, 0, start.Add(2*time.Second)); send {
		t.Fatal("still no duration, still nothing to send")
	}

	send, seek, total := p.observe(7, false, 1440, start.Add(4*time.Second))
	if send || seek || total != 0 {
		t.Fatalf("a duration alone is not a change worth sending, got send=%v seek=%v total=%d", send, seek, total)
	}

	send, _, total = p.observe(9, true, 1440, start.Add(6*time.Second))
	if !send || total != 1440 {
		t.Fatalf("a pause should be sent with the real duration, got send=%v total=%d", send, total)
	}
}

func TestPresenceStateSendsChanges(t *testing.T) {
	var p presenceState
	now := time.Unix(1000, 0)

	send, seek, total := p.observe(10, false, 1440, now)
	if !send || seek || total != 1440 {
		t.Fatalf("the first state should be sent, got send=%v seek=%v total=%d", send, seek, total)
	}
	p.sent(false, now)

	now = now.Add(2 * time.Second)
	if send, _, _ := p.observe(12, false, 1440, now); send {
		t.Fatal("ordinary playback should not be sent")
	}

	now = now.Add(2 * time.Second)
	send, seek, _ = p.observe(300, false, 1440, now)
	if !send || !seek {
		t.Fatalf("a jump forward should be sent as a seek, got send=%v seek=%v", send, seek)
	}
	p.sent(false, now)

	now = now.Add(2 * time.Second)
	if _, seek, _ = p.observe(290, false, 1440, now); !seek {
		t.Fatal("a jump back should count as a seek")
	}
	p.sent(false, now)

	now = now.Add(31 * time.Second)
	send, seek, _ = p.observe(292, false, 1440, now)
	if !send || seek {
		t.Fatalf("the keep-alive should be sent after 30 seconds, got send=%v seek=%v", send, seek)
	}
	p.sent(false, now)

	// Known once, then lost: the bar is kept just past the position.
	now = now.Add(2 * time.Second)
	if send, _, total = p.observe(294, true, 0, now); !send || total != 295 {
		t.Fatalf("a lost duration should fall back to position+1, got send=%v total=%d", send, total)
	}
}

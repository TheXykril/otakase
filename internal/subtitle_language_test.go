package internal

import (
	"testing"
)

func TestShowPrefsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := updateShowPrefs(dir, 21, func(p *ShowPrefs) { p.SubtitleLanguage = "portuguese" }); err != nil {
		t.Fatal(err)
	}
	if err := updateShowPrefs(dir, 1535, func(p *ShowPrefs) { p.SubtitleLanguage = "spanish" }); err != nil {
		t.Fatal(err)
	}
	prefs := loadShowPrefs(dir)
	if prefs["21"].SubtitleLanguage != "portuguese" || prefs["1535"].SubtitleLanguage != "spanish" {
		t.Fatalf("prefs = %+v", prefs)
	}
	// Clearing the only field drops the show rather than leaving an empty entry.
	if err := updateShowPrefs(dir, 21, func(p *ShowPrefs) { p.SubtitleLanguage = "" }); err != nil {
		t.Fatal(err)
	}
	if _, kept := loadShowPrefs(dir)["21"]; kept {
		t.Fatal("empty entry was kept")
	}
}

func TestSubtitleLanguageForPrefersTheShow(t *testing.T) {
	dir := t.TempDir()
	config := &Config{StoragePath: dir, SubsLanguage: "eng"}
	anime := &Anime{AnilistId: 21}
	if got := subtitleLanguageFor(config, anime); got != "english" {
		t.Fatalf("without a show pref got %q", got)
	}
	if err := updateShowPrefs(dir, 21, func(p *ShowPrefs) { p.SubtitleLanguage = "spanish" }); err != nil {
		t.Fatal(err)
	}
	if got := subtitleLanguageFor(config, anime); got != "spanish" {
		t.Fatalf("with a show pref got %q", got)
	}
	if got := subtitleLanguageFor(config, &Anime{AnilistId: 99}); got != "english" {
		t.Fatalf("another show got %q", got)
	}
}

func TestPickSubtitleForHint(t *testing.T) {
	config := &Config{StoragePath: t.TempDir(), SubsLanguage: "portuguese"}
	hint := StreamPlaybackHint{
		Subtitle: "en.vtt",
		Subtitles: []SubtitleTrack{
			{URL: "en.vtt", Language: "English"},
			{URL: "pt.vtt", Language: "Portuguese"},
		},
	}
	if got := pickSubtitleForHint(config, &Anime{}, hint); got != "pt.vtt" {
		t.Fatalf("got %q", got)
	}
	// A host that lists no tracks keeps its own pick.
	if got := pickSubtitleForHint(config, &Anime{}, StreamPlaybackHint{Subtitle: "only.vtt"}); got != "only.vtt" {
		t.Fatalf("got %q", got)
	}
}

func TestMPVSubtitleLanguageArgs(t *testing.T) {
	got := mpvSubtitleLanguageArgs("english", nil)
	if len(got) != 1 || got[0] != "--slang=en,eng" {
		t.Fatalf("got %v", got)
	}
	if got := mpvSubtitleLanguageArgs("english", []string{"--slang=de"}); got != nil {
		t.Fatalf("a viewer's own --slang must win, got %v", got)
	}
}

func TestAddAlternateSubtitlesSkipsTheChosenTrack(t *testing.T) {
	var sent [][]interface{}
	send := func(_ string, command []interface{}) (interface{}, error) {
		sent = append(sent, command)
		return nil, nil
	}
	tracks := []SubtitleTrack{
		{URL: "en.vtt", Language: "English", Label: "English"},
		{URL: "pt.vtt", Language: "Portuguese - Brazilian", Label: "Portuguese - Brazilian"},
	}
	addAlternateSubtitles(send, "/tmp/sock", "en.vtt", tracks)
	if len(sent) != 1 {
		t.Fatalf("sent %v", sent)
	}
	command := sent[0]
	if command[0] != "sub-add" || command[1] != "pt.vtt" || command[2] != "auto" || command[4] != "pt" {
		t.Fatalf("command = %v", command)
	}
}

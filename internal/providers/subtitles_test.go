package providers

import "testing"

func TestCanonicalLanguage(t *testing.T) {
	cases := map[string]string{
		"en":                      "english",
		"ENG":                     "english",
		"English":                 "english",
		"English [CC]":            "english",
		"pt-BR":                   "portuguese",
		"Portuguese - Brazilian":  "portuguese",
		"Spanish (Latin America)": "spanish",
		"jpn":                     "japanese",
		"":                        "",
		"Klingon":                 "klingon",
	}
	for input, want := range cases {
		if got := CanonicalLanguage(input); got != want {
			t.Errorf("CanonicalLanguage(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPickSubtitle(t *testing.T) {
	tracks := []SubtitleTrack{
		{URL: "en-cc.vtt", Language: "English", Label: "English [CC]"},
		{URL: "en.vtt", Language: "en", Label: "English"},
		{URL: "pt.vtt", Language: "por", Label: "Portuguese - Brazilian"},
		{URL: "", Language: "fr", Label: "French"},
	}
	cases := []struct {
		wanted, want string
	}{
		{"english", "en.vtt"},
		{"Portuguese", "pt.vtt"},
		{"pt", "pt.vtt"},
		{"french", "default.vtt"}, // listed, but with no file
		{"german", "default.vtt"},
		{"", "default.vtt"},
	}
	for _, c := range cases {
		if got := PickSubtitle(tracks, c.wanted, "default.vtt"); got != c.want {
			t.Errorf("PickSubtitle(%q) = %q, want %q", c.wanted, got, c.want)
		}
	}
}

func TestPickSubtitleTakesCaptionsWhenAlone(t *testing.T) {
	tracks := []SubtitleTrack{{URL: "cc.vtt", Language: "en", Label: "English [CC]"}}
	if got := PickSubtitle(tracks, "english", ""); got != "cc.vtt" {
		t.Fatalf("got %q, want the only English track", got)
	}
}

func TestMPVLanguageCodes(t *testing.T) {
	if got := MPVLanguageCodes("English"); got != "en,eng" {
		t.Errorf("english = %q", got)
	}
	if got := MPVLanguageCodes("portuguese"); got != "pt,por" {
		t.Errorf("portuguese = %q", got)
	}
	if got := MPVLanguageCodes("klingon"); got != "klingon" {
		t.Errorf("unknown = %q", got)
	}
}

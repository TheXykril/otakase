package internal

import (
	"strings"
	"testing"

	"github.com/thexykril/otakase/internal/providers/anipub"
)

func TestFormatAnimeSearchLabelAddsKnownCount(t *testing.T) {
	cases := []struct {
		title    string
		episodes int
		want     string
	}{
		{"Frieren", 28, "Frieren (28 episodes)"},
		{"Airing Show", 0, "Airing Show"},
		{"  ", 12, ""},
	}
	for _, c := range cases {
		if got := FormatAnimeSearchLabel(c.title, c.episodes); got != c.want {
			t.Errorf("FormatAnimeSearchLabel(%q, %d) = %q, want %q", c.title, c.episodes, got, c.want)
		}
	}
}

// The tick marks the result whose count agrees with the tracker, which is
// what tells two similarly named shows apart.
func TestSearchResultsTickTheMatchingCount(t *testing.T) {
	results := []SelectionOption{
		{Key: "a", Label: "One Piece (1090 episodes) [anikoto]"},
		{Key: "b", Label: "One Pace (300 episodes) [anineko]"},
	}
	display := ProviderSearchOptionsForDisplay(results, &Entry{Media: Media{Episodes: 1090}})

	want := []string{"One Piece (1090 episodes) [anikoto] ✓", "One Pace (300 episodes) [anineko]"}
	for i := range want {
		if display[i].Label != want[i] {
			t.Errorf("result %d = %q, want %q", i, display[i].Label, want[i])
		}
	}
	if results[0].Label != "One Piece (1090 episodes) [anikoto]" {
		t.Errorf("the caller's slice was changed: %q", results[0].Label)
	}
}

func TestManualProviderSearchHintNamesWhatIsSearched(t *testing.T) {
	entry := &Entry{Media: Media{Format: "TV", Episodes: 24, Title: AnimeTitle{Romaji: "Frieren"}}}
	hint := ManualProviderSearchHint(&Config{SubOrDub: "sub"}, entry, "frieren", "sub")
	for _, want := range []string{`Searching for "Frieren"`, "TV", "24 episodes", "sub"} {
		if !strings.Contains(hint, want) {
			t.Errorf("hint %q is missing %q", hint, want)
		}
	}
}

func TestManualProviderSearchIsOffUnlessSet(t *testing.T) {
	if !ManualProviderSearchEnabled(&Config{ManualProviderSearch: true}) {
		t.Error("enabled in the config, but reported off")
	}
	if ManualProviderSearchEnabled(&Config{}) || ManualProviderSearchEnabled(nil) {
		t.Error("manual provider search should be off by default")
	}
}

// A provider that knows the count but leaves it out of the title still gets
// it shown, ahead of its tag.
func TestEpisodeCountFromSearchItemGoesBeforeTheTag(t *testing.T) {
	item := anipub.SearchItem{Name: "Frieren", Episodes: 28}
	option := SelectionOption{Key: "session", Label: "Frieren [anipub]", Title: "Frieren", ExtraData: item}
	if got := ensureEpisodeCountInLabel(option).Label; got != "Frieren (28 episodes) [anipub]" {
		t.Errorf("label = %q", got)
	}
}

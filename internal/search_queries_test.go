package internal

import (
	"strings"
	"testing"
)

// The failure this guards against: AniList hands Curd a long romaji title, anipub
// indexes the English release title, and the search reports "no results" for a show
// the provider actually carries.
func TestBuildSearchQueryVariantsFallsBackToEnglishTitle(t *testing.T) {
	romaji := "Saijo no Osewa: Takane no Hanadarake na Meimonkou de, Gakuin Ichi no Ojou-sama (Seikatsu Nouryoku Kaimu) wo Kagenagara Osewa suru Koto ni Narimashita"
	english := "Rich Girl Caretaker: I'm Secretly the Caregiver of the Most Popular Girl in This Rich Kid School"

	variants := buildSearchQueryVariants(
		&CurdConfig{AnimeNameLanguage: "english"},
		AnimeTitle{Romaji: romaji, English: english},
		romaji,
	)

	if len(variants) == 0 || variants[0] != romaji {
		t.Fatalf("expected the caller's query to lead, got %v", variants)
	}

	foundEnglish := false
	for _, variant := range variants {
		if variant == english {
			foundEnglish = true
			break
		}
	}
	if !foundEnglish {
		t.Fatalf("expected the English title among variants, got %v", variants)
	}
}

func TestBuildSearchQueryVariantsHonorsRomajiPreference(t *testing.T) {
	variants := buildSearchQueryVariants(
		&CurdConfig{AnimeNameLanguage: "romaji"},
		AnimeTitle{Romaji: "Shingeki no Kyojin", English: "Attack on Titan"},
		"",
	)

	if len(variants) < 2 {
		t.Fatalf("expected both titles, got %v", variants)
	}
	if variants[0] != "Shingeki no Kyojin" {
		t.Fatalf("expected romaji first for romaji preference, got %v", variants)
	}
}

func TestBuildSearchQueryVariantsDropsParentheticalsAndSeasons(t *testing.T) {
	variants := buildSearchQueryVariants(
		&CurdConfig{},
		AnimeTitle{English: "Some Show (TV) Season 2"},
		"",
	)

	if !containsVariant(variants, "Some Show") {
		t.Fatalf("expected a simplified %q variant, got %v", "Some Show", variants)
	}
}

func TestBuildSearchQueryVariantsSplitsOnSubtitleSeparator(t *testing.T) {
	variants := buildSearchQueryVariants(
		&CurdConfig{},
		AnimeTitle{English: "Rich Girl Caretaker: I'm Secretly the Caregiver"},
		"",
	)

	if !containsVariant(variants, "Rich Girl Caretaker") {
		t.Fatalf("expected the leading clause as a variant, got %v", variants)
	}
}

func TestBuildSearchQueryVariantsDeduplicates(t *testing.T) {
	variants := buildSearchQueryVariants(
		&CurdConfig{},
		AnimeTitle{Romaji: "One Piece", English: "One Piece"},
		"one piece",
	)

	seen := make(map[string]struct{}, len(variants))
	for _, variant := range variants {
		key := strings.ToLower(variant)
		if _, exists := seen[key]; exists {
			t.Fatalf("duplicate variant %q in %v", variant, variants)
		}
		seen[key] = struct{}{}
	}
}

// A query the user typed must not be replaced by a title guess.
func TestSetManualQueryClearsVariants(t *testing.T) {
	state := &providerMappingSearchState{
		query:         "auto title",
		queryVariants: []string{"auto title", "other title"},
	}
	state.setManualQuery("what the user typed")

	if state.query != "what the user typed" {
		t.Fatalf("query = %q", state.query)
	}
	if len(state.queryVariants) != 0 {
		t.Fatalf("expected variants cleared, got %v", state.queryVariants)
	}
}

func containsVariant(variants []string, want string) bool {
	for _, variant := range variants {
		if variant == want {
			return true
		}
	}
	return false
}

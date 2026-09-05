package internal

import (
	"errors"
	"os"
	"testing"

	"github.com/wraient/curd/internal/curdhost"
)

// TestProviderSearchResolvesRomajiOnlyTitlesLive exercises the real fix against
// live providers: AniList supplies a romaji title, the provider indexes the
// English one, and the search must still resolve the show.
//
// Run with: CURD_LIVE_SEARCH_TEST=1 go test -run Live ./internal/
func TestProviderSearchResolvesRomajiOnlyTitlesLive(t *testing.T) {
	if os.Getenv("CURD_LIVE_SEARCH_TEST") != "1" {
		t.Skip("set CURD_LIVE_SEARCH_TEST=1")
	}

	config := &CurdConfig{Provider: "stacked", AnimeNameLanguage: "english"}
	previous := GetGlobalConfig()
	SetGlobalConfig(config)
	t.Cleanup(func() { SetGlobalConfig(previous) })

	cases := []struct {
		name  string
		title AnimeTitle
	}{
		{
			name: "romaji-only title the providers index in English",
			title: AnimeTitle{
				Romaji:  "Saijo no Osewa: Takane no Hanadarake na Meimonkou de, Gakuin Ichi no Ojou-sama (Seikatsu Nouryoku Kaimu) wo Kagenagara Osewa suru Koto ni Narimashita",
				English: "Rich Girl Caretaker: I'm Secretly the Caregiver of the Most Popular Girl in This Rich Kid School",
			},
		},
		{
			name:  "long-running series",
			title: AnimeTitle{Romaji: "One Piece", English: "One Piece"},
		},
		{
			name:  "title differing between romaji and English",
			title: AnimeTitle{Romaji: "Shingeki no Kyojin", English: "Attack on Titan"},
		},
		{
			name:  "title with a season suffix",
			title: AnimeTitle{Romaji: "Sousou no Frieren", English: "Frieren: Beyond Journey's End"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Start from the romaji title, exactly as curd.go seeds the query.
			state := &providerMappingSearchState{
				query:         tc.title.Romaji,
				allProviders:  configuredProviderNames(config),
				queryVariants: buildSearchQueryVariants(config, tc.title, tc.title.Romaji),
			}

			results, err := searchAnimeForMapping(config, state, "sub")
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			if len(results) == 0 {
				t.Fatalf("no results for %q (variants: %v)", tc.title.Romaji, state.queryVariants)
			}
			t.Logf("resolved %q via query %q with %d result(s); first = %s",
				tc.title.Romaji, state.query, len(results), results[0].Label)
		})
	}
}

// TestAnipubNeedsEnglishTitleVariantLive pins the exact failure from the bug
// report: anipub indexes only the English release title, so the romaji query
// misses and the search must fall through to a variant to find the show.
func TestAnipubNeedsEnglishTitleVariantLive(t *testing.T) {
	if os.Getenv("CURD_LIVE_SEARCH_TEST") != "1" {
		t.Skip("set CURD_LIVE_SEARCH_TEST=1")
	}

	romaji := "Saijo no Osewa: Takane no Hanadarake na Meimonkou de, Gakuin Ichi no Ojou-sama (Seikatsu Nouryoku Kaimu) wo Kagenagara Osewa suru Koto ni Narimashita"
	english := "Rich Girl Caretaker: I'm Secretly the Caregiver of the Most Popular Girl in This Rich Kid School"
	title := AnimeTitle{Romaji: romaji, English: english}

	config := &CurdConfig{Provider: `["anipub"]`, AnimeNameLanguage: "english"}
	previous := GetGlobalConfig()
	SetGlobalConfig(config)
	t.Cleanup(func() { SetGlobalConfig(previous) })

	// Without variants the romaji query misses outright — the original bug.
	_, romajiErr := searchAnimeWithProviders([]string{"anipub"}, romaji, "sub")
	if romajiErr == nil {
		t.Skip("anipub now indexes the romaji title; this regression no longer reproduces")
	}
	// Being throttled proves nothing either way, and failing on it would make a
	// scheduled run cry wolf about a provider that is perfectly healthy.
	if errors.Is(romajiErr, curdhost.ErrRateLimited) {
		t.Skip("anipub is rate limiting; cannot exercise the variant fallback right now")
	}

	state := &providerMappingSearchState{
		query:         romaji,
		allProviders:  configuredProviderNames(config),
		queryVariants: buildSearchQueryVariants(config, title, romaji),
	}

	results, err := searchAnimeForMapping(config, state, "sub")
	if errors.Is(err, curdhost.ErrRateLimited) {
		t.Skip("anipub is rate limiting; cannot exercise the variant fallback right now")
	}
	if err != nil {
		t.Fatalf("expected a variant to rescue the search, got %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("no results (variants: %v)", state.queryVariants)
	}
	if state.query == romaji {
		t.Fatalf("expected the search to adopt a working variant, still on %q", state.query)
	}
	t.Logf("romaji query missed; variant %q returned %d result(s); first = %s",
		state.query, len(results), results[0].Label)
}

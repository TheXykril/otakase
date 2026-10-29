package internal

import (
	"errors"
	"slices"
	"strings"
	"testing"

	_ "github.com/thexykril/otakase/internal/loadproviders"
	"github.com/thexykril/otakase/internal/providers"
)

func TestSearchStateWalksTheStack(t *testing.T) {
	t.Run("a single provider has no next one", func(t *testing.T) {
		single := &providerMappingSearchState{allProviders: []string{"kickassanime"}}
		if next := single.nextProviderLabel(); next != "" {
			t.Fatalf("next provider %q, want none", next)
		}
		if single.advanceToNextProvider() {
			t.Fatal("advanced past the only provider")
		}
	})

	t.Run("an empty stack goes nowhere", func(t *testing.T) {
		empty := &providerMappingSearchState{sequential: true}
		if empty.advanceToNextProvider() || empty.activeProviders() != nil || empty.currentProviderLabel() != "" {
			t.Fatalf("empty stack: %+v", empty)
		}
	})

	t.Run("from the whole stack the next is its first", func(t *testing.T) {
		state := &providerMappingSearchState{allProviders: []string{"anizone", "kickassanime"}}
		if got := state.nextProviderLabel(); got != "anizone" {
			t.Fatalf("next provider %q, want anizone", got)
		}
		if got := state.currentProviderLabel(); got != "every source" {
			t.Fatalf("scope label %q", got)
		}
		if got := state.activeProviders(); !slices.Equal(got, state.allProviders) {
			t.Fatalf("searching %v, want the whole stack", got)
		}
	})

	t.Run("advancing walks the stack one at a time", func(t *testing.T) {
		state := &providerMappingSearchState{allProviders: []string{"anizone", "kickassanime", "nyaa"}}
		for i, name := range state.allProviders {
			if !state.advanceToNextProvider() {
				t.Fatalf("advance %d failed", i+1)
			}
			if !state.sequential || state.providerIndex != i {
				t.Fatalf("after advance %d: sequential=%v index=%d", i+1, state.sequential, state.providerIndex)
			}
			if got := state.activeProviders(); !slices.Equal(got, []string{name}) {
				t.Fatalf("after advance %d searching %v, want %s", i+1, got, name)
			}
			if got := state.currentProviderLabel(); got != strings.Trim(sourceTag(name), "[]") || strings.Contains(got, name) {
				t.Fatalf("after advance %d the scope reads %q", i+1, got)
			}
		}
		if next := state.nextProviderLabel(); next != "" {
			t.Fatalf("next provider %q at the end of the stack", next)
		}
		if state.advanceToNextProvider() {
			t.Fatal("advanced past the end")
		}
		if got := state.activeProviders(); got != nil {
			t.Fatalf("past the end searching %v", got)
		}

		state.resetToAllProviders()
		if state.sequential || state.nextProviderLabel() != "anizone" {
			t.Fatal("reset did not return to the whole stack")
		}
	})
}

// secondOfTwo is a search narrowed to the second provider of a two-provider
// stack, with the config that stack comes from.
func secondOfTwo() (*Config, *providerMappingSearchState) {
	stack := []string{"anizone", "kickassanime"}
	return &Config{Provider: `["anizone","kickassanime"]`},
		&providerMappingSearchState{allProviders: stack, sequential: true, providerIndex: 1}
}

func TestABarePickBelongsToTheProviderBeingSearched(t *testing.T) {
	withAllProvidersEnabledForTest(t)
	config, search := secondOfTwo()

	pick := SelectionOption{Key: "frieren-beyond-journeys-end", Title: "Frieren: Beyond Journey's End"}
	if owner := providerNameFromSelection(config, search, pick); owner != "kickassanime" {
		t.Fatalf("pick attributed to %q, want kickassanime", owner)
	}

	var mapped Anime
	applySelectedProviderMapping(config, search, &mapped, pick)
	if mapped.ProviderName != "kickassanime" || mapped.ProviderId != pick.Key {
		t.Fatalf("stored as %s/%s", mapped.ProviderName, mapped.ProviderId)
	}
}

func TestProviderNameFromSelectionReadsKeyThenLabel(t *testing.T) {
	withAllProvidersEnabledForTest(t)
	config := &Config{Provider: `["anizone","kickassanime"]`}
	state := &providerMappingSearchState{allProviders: []string{"anizone", "kickassanime"}}

	for _, tc := range []struct {
		option SelectionOption
		want   string
	}{
		{option: SelectionOption{Key: "kickassanime::frieren", Label: "Frieren [anizone]"}, want: "kickassanime"},
		{option: SelectionOption{Key: "frieren", Label: "Frieren [nyaa]"}, want: "nyaa"},
		{option: SelectionOption{Key: "frieren", Label: "Frieren [nonsense]"}, want: "anizone"},
		{option: SelectionOption{Key: "frieren", Label: "Frieren"}, want: "anizone"},
	} {
		if got := providerNameFromSelection(config, state, tc.option); got != tc.want {
			t.Errorf("%+v: got %q, want %q", tc.option, got, tc.want)
		}
	}

	var anime Anime
	applySelectedProviderMapping(config, state, &anime, SelectionOption{Key: "kickassanime::frieren"})
	if anime.ProviderName != "kickassanime" || anime.ProviderId != "frieren" {
		t.Fatalf("a qualified pick was stored as %+v", anime)
	}
}

func TestAnAutomaticMatchBelongsToTheProviderBeingSearched(t *testing.T) {
	withAllProvidersEnabledForTest(t)
	config, search := secondOfTwo()

	matched := Anime{ProviderId: "frieren-beyond-journeys-end"}
	applyMatchedProviderMapping(config, search, &matched)
	if matched.ProviderName != "kickassanime" || matched.ProviderId != "frieren-beyond-journeys-end" {
		t.Fatalf("stored as %s/%s", matched.ProviderName, matched.ProviderId)
	}

	qualified := Anime{ProviderId: "anizone::42"}
	applyMatchedProviderMapping(config, search, &qualified)
	if qualified.ProviderName != "anizone" || qualified.ProviderId != "42" {
		t.Fatalf("a qualified match was stored as %s/%s", qualified.ProviderName, qualified.ProviderId)
	}
}

func TestActiveProviderNameFallsBackToTheStackHead(t *testing.T) {
	withAllProvidersEnabledForTest(t)
	config := &Config{Provider: `["nyaa","anizone"]`}
	if got := activeProviderName(config, nil); got != "nyaa" {
		t.Fatalf("without a search: %q", got)
	}
	whole := &providerMappingSearchState{allProviders: []string{"anizone"}}
	if got := activeProviderName(config, whole); got != "nyaa" {
		t.Fatalf("searching the whole stack: %q", got)
	}
}

func TestTranslationModeOfDefaultsToSub(t *testing.T) {
	if got := translationModeOf(nil); got != "sub" {
		t.Fatalf("nil config: %q", got)
	}
	if got := translationModeOf(&Config{SubOrDub: "DUB"}); got != "dub" {
		t.Fatalf("dub config: %q", got)
	}
}

func TestEpisodeLinkFailureOptionsOfferAudioOnlyWhenAsked(t *testing.T) {
	keys := func(options []SelectionOption) []string {
		var out []string
		for _, option := range options {
			out = append(out, option.Key)
		}
		return out
	}
	if got := keys(episodeLinkFailureRecoveryOptions("sub", true)); !slices.Equal(got, []string{"remap", "audio", "episode"}) {
		t.Fatalf("with audio: %v", got)
	}
	if got := keys(episodeLinkFailureRecoveryOptions("sub", false)); !slices.Equal(got, []string{"remap", "episode"}) {
		t.Fatalf("without audio: %v", got)
	}
}

func TestProviderListingMatchSignals(t *testing.T) {
	anime := &Anime{AnilistId: 154587, MalId: 52991}
	noPictures := func() []string {
		t.Fatal("pictures fetched for a result without a MyAnimeList cover")
		return nil
	}

	for _, tc := range []struct {
		name   string
		option SelectionOption
		match  bool
	}{
		{name: "MAL id in search data", option: SelectionOption{ExtraData: providers.SearchHit{MalID: 52991}}, match: true},
		{name: "other MAL id in search data", option: SelectionOption{ExtraData: providers.SearchHit{MalID: 1}}},
		{name: "AniList cover", option: SelectionOption{Thumbnail: "https://s4.anilist.co/file/anilistcdn/media/anime/cover/large/bx154587-n1fmjRv4JQUd.jpg"}, match: true},
		{name: "another show's AniList cover", option: SelectionOption{Thumbnail: "https://s4.anilist.co/file/anilistcdn/media/anime/cover/medium/98478.jpg"}},
		{name: "MAL poster", option: SelectionOption{Thumbnail: "https://example.org/posters/52991.webp"}, match: true},
		{name: "other poster", option: SelectionOption{Thumbnail: "https://example.org/posters/1.webp"}},
		{name: "no signal", option: SelectionOption{Title: "Frieren"}},
	} {
		if got := providerListingMatch(anime, tc.option, noPictures) != ""; got != tc.match {
			t.Errorf("%s: matched=%v, want %v", tc.name, got, tc.match)
		}
	}
}

func TestProviderListingMatchLooksUpMyAnimeListCoversOnce(t *testing.T) {
	anime := &Anime{AnilistId: 21366, MalId: 31646}
	fetches := 0
	pictures := func() []string {
		fetches++
		return []string{"https://cdn.myanimelist.net/images/anime/1637/108857.jpg"}
	}

	cover := SelectionOption{Thumbnail: "https://cdn.myanimelist.net/images/anime/1637/108857.jpg"}
	if providerListingMatch(anime, cover, pictures) == "" {
		t.Fatal("a cover among the show's MAL pictures did not match")
	}
	other := SelectionOption{Thumbnail: "https://cdn.myanimelist.net/images/anime/1/2.jpg"}
	if providerListingMatch(anime, other, pictures) != "" {
		t.Fatal("a cover that is not among the show's pictures matched")
	}
	if fetches != 2 {
		t.Fatalf("pictures asked for %d times, want once per MAL cover", fetches)
	}
}

func TestAutoMatchProviderListingPrefersTheMALKey(t *testing.T) {
	anime := &Anime{AnilistId: 154587, MalId: 52991, Title: AnimeTitle{English: "Frieren"}}
	options := []SelectionOption{
		{Key: "frieren", Title: "Frieren"},
		{Key: "52991", Title: "Sousou no Frieren"},
	}
	if key, ok := autoMatchProviderListing(&Config{}, anime, options, "Frieren", nil); !ok || key != "52991" {
		t.Fatalf("got (%q, %v), want the result keyed by the MAL id", key, ok)
	}

	// Without one, a clear title match is taken.
	if key, ok := autoMatchProviderListing(&Config{}, anime, options[:1], "Frieren", nil); !ok || key != "frieren" {
		t.Fatalf("got (%q, %v), want the exact title", key, ok)
	}
	if _, ok := autoMatchProviderListing(&Config{}, anime, []SelectionOption{{Key: "x", Title: "Something else"}}, "Frieren", nil); ok {
		t.Fatal("an unrelated result was matched")
	}
}

func TestResolveAnimeProviderMappingTakesAnAutomaticMatch(t *testing.T) {
	anizoneStub := &stackStubProvider{
		name:          "anizone",
		searchResults: map[string][]SelectionOption{"sub": {{Key: "52991", Title: "Sousou no Frieren"}}},
	}
	withProviderFactories(t, anizoneStub)
	withPromptSelect(t, func(options []SelectionOption) (SelectionOption, error) {
		t.Fatalf("a menu was shown for an automatic match: %+v", options)
		return SelectionOption{}, nil
	})

	config := &Config{Provider: `["anizone"]`, SubOrDub: "sub"}
	anime := &Anime{AnilistId: 154587, MalId: 52991, Title: AnimeTitle{English: "Frieren"}}
	outcome, err := resolveAnimeProviderMapping(config, anime, "Frieren", nil, false)
	if err != nil || outcome != ProviderMappingOK {
		t.Fatalf("got (%v, %v)", outcome, err)
	}
	if anime.ProviderName != "anizone" || anime.ProviderId != "52991" {
		t.Fatalf("mapped to %s/%s", anime.ProviderName, anime.ProviderId)
	}
}

func TestResolveAnimeProviderMappingBacksOutOfTheMatchMenu(t *testing.T) {
	anizoneStub := &stackStubProvider{
		name:          "anizone",
		searchResults: map[string][]SelectionOption{"sub": {{Key: "x", Title: "Something else"}}},
	}
	withProviderFactories(t, anizoneStub)
	var menus [][]string
	withPromptSelect(t, func(options []SelectionOption) (SelectionOption, error) {
		var keys []string
		for _, option := range options {
			keys = append(keys, option.Key)
		}
		menus = append(menus, keys)
		return SelectionOption{Key: "back"}, nil
	})

	config := &Config{Provider: `["anizone"]`, SubOrDub: "sub"}
	anime := &Anime{AnilistId: 154587, MalId: 52991, Title: AnimeTitle{English: "Frieren"}}
	outcome, err := resolveAnimeProviderMapping(config, anime, "Frieren", nil, false)
	if err != nil || outcome != ProviderMappingBack {
		t.Fatalf("got (%v, %v), want back", outcome, err)
	}
	if len(menus) != 1 || !slices.Equal(menus[0], []string{"pick", "custom", "back"}) {
		t.Fatalf("menus shown: %v", menus)
	}
}

func TestResolveUntrackedProviderSearchRecoveryAnswers(t *testing.T) {
	withProviderFactories(t, &stackStubProvider{name: "anizone"}, &stackStubProvider{name: "kickassanime"})
	config := &Config{Provider: `["anizone","kickassanime"]`, SubOrDub: "sub"}

	for _, tc := range []struct {
		answers []string
		back    bool
		menus   int
	}{
		{answers: []string{"back"}, back: true, menus: 1},
		{answers: []string{"quit"}, back: false, menus: 1},
		{answers: []string{"-1"}, back: false, menus: 1},
		// Each provider on its own, then nothing is left to try. Backing out
		// of that last menu ends the search without reporting a back.
		{answers: []string{"next_provider", "next_provider", "next_provider", "back"}, back: false, menus: 4},
	} {
		menus := 0
		withPromptSelect(t, func(options []SelectionOption) (SelectionOption, error) {
			answer := tc.answers[min(menus, len(tc.answers)-1)]
			menus++
			return SelectionOption{Key: answer}, nil
		})
		id, name, _, back, err := ResolveUntrackedProviderSearch(config, "nothing")
		if err != nil || id != "" || name != "" || back != tc.back || menus != tc.menus {
			t.Errorf("%v: got id=%q name=%q back=%v err=%v after %d menus, want back=%v after %d",
				tc.answers, id, name, back, err, menus, tc.back, tc.menus)
		}
	}
}

func TestMalIDFromProviderExtraData(t *testing.T) {
	if got := malIDFromProviderExtraData(providers.SearchHit{MalID: 7}); got != 7 {
		t.Fatalf("got %d", got)
	}
	if got := malIDFromProviderExtraData("7"); got != 0 {
		t.Fatalf("got %d from a string", got)
	}
}

func TestRecoveryActionFoldsExitEntries(t *testing.T) {
	for key, want := range map[string]string{"-1": "quit", "-2": "back", "back": "back", "custom": "custom"} {
		if got := recoveryAction(SelectionOption{Key: key}); got != want {
			t.Errorf("%q: got %q, want %q", key, got, want)
		}
	}
}

func TestPersistRemappedProviderNeedsTheAnime(t *testing.T) {
	if err := persistRemappedProvider("", nil, nil, &Anime{}, "x"); err == nil {
		t.Fatal("expected an error without a tracker entry")
	}
}

// Backing out of "search under a different name" leaves the search as it was.
// The question is one keypress from a list of results, and changing your mind
// about it should not clear the name that got you there.
func TestCustomProviderQueryCancelKeepsTheCurrentQuery(t *testing.T) {
	query, cancelled, err := customProviderQueryFromAnswer("frieren", func() (string, bool, error) {
		return "", true, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cancelled {
		t.Error("cancelling the prompt should be reported as cancelled")
	}
	if query != "frieren" {
		t.Errorf("cancelling changed the query to %q", query)
	}
}

func TestCustomProviderQueryUsesTheAnswer(t *testing.T) {
	query, cancelled, err := customProviderQueryFromAnswer("frieren", func() (string, bool, error) {
		return "sousou no frieren", false, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cancelled {
		t.Error("an answered prompt should not be reported as cancelled")
	}
	if query != "sousou no frieren" {
		t.Errorf("the answer was not used, got %q", query)
	}
}

// A prompt that fails is not a prompt that was answered with nothing: the
// caller reports the failure, and the query it already had survives it.
func TestCustomProviderQueryKeepsTheQueryOnError(t *testing.T) {
	failed := errors.New("no terminal")
	query, cancelled, err := customProviderQueryFromAnswer("frieren", func() (string, bool, error) {
		return "", false, failed
	})
	if !errors.Is(err, failed) {
		t.Fatalf("expected the prompt error back, got %v", err)
	}
	if cancelled {
		t.Error("an error is not a cancellation")
	}
	if query != "frieren" {
		t.Errorf("the query was lost, got %q", query)
	}
}

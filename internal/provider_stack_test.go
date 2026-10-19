package internal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/thexykril/otakase/internal/providers"
)

var fullTestStack = []string{"anikoto", "kickassanime", "anizone", "anipub", "anineko", "nyaa", "sukebei", "anidb"}

// stackStubProvider stands in for a registered provider. Searches answer
// from searchResults by audio; episodes answer from episodeErrors, then
// episodeResults, by show id and audio. calls records each episode lookup
// as "name:mode".
type stackStubProvider struct {
	name string // the registered provider it replaces

	searchResults  map[string][]SelectionOption   // by audio
	episodeResults map[string]map[string][]string // by show id, then audio
	episodeErrors  map[string]map[string]error    // as episodeResults; wins
	calls          []string                       // "name:mode" per lookup
}

func (stub *stackStubProvider) Name() string { return stub.name }

func (stub *stackStubProvider) SearchAnime(query, mode string) ([]providers.SelectionOption, error) {
	var options []providers.SelectionOption
	for _, option := range stub.searchResults[normalizeTranslationType(mode)] {
		options = append(options, providers.SelectionOption{
			Key:       option.Key,
			Label:     option.Label,
			Title:     option.Title,
			Thumbnail: option.Thumbnail,
			ExtraData: option.ExtraData,
		})
	}
	return options, nil
}

func (stub *stackStubProvider) EpisodesList(id, mode string) ([]string, error) {
	return nil, nil // nothing here reads episode lists
}

func (stub *stackStubProvider) GetEpisodeURL(config providers.PlaybackConfig, id string, epNo int) ([]string, error) {
	return stub.GetEpisodeURLForMode(config, id, epNo, config.SubOrDub)
}

func (stub *stackStubProvider) GetEpisodeURLForMode(config providers.PlaybackConfig, id string, epNo int, mode string) ([]string, error) {
	audio := normalizeTranslationType(mode)
	stub.calls = append(stub.calls, stub.name+":"+audio)
	if err := stub.episodeErrors[id][audio]; err != nil {
		return nil, err
	}
	return stub.episodeResults[id][audio], nil
}

// withProviderFactories registers the stubs in place of the providers they
// are named after, with every provider enabled, for the rest of the test.
func withProviderFactories(t *testing.T, stubs ...*stackStubProvider) {
	t.Helper()
	withAllProvidersEnabledForTest(t)
	for _, s := range stubs {
		t.Cleanup(providers.SetFactoryForTest(s.name, func() providers.Provider { return s }))
	}
}

func TestConfiguredProviderNamesAcceptsOrderedLists(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	for _, tc := range []struct {
		name     string
		provider string
		want     []string
	}{
		{name: "empty", provider: "", want: fullTestStack},
		{name: "stacked", provider: "stacked", want: fullTestStack},
		{name: "older stack spelling", provider: " Auto ", want: fullTestStack},
		{name: "json list", provider: `["anikoto","anineko"]`, want: []string{"anikoto", "anineko"}},
		{name: "comma list", provider: "anineko,anikoto", want: []string{"anineko", "anikoto"}},
		{name: "plus list", provider: "anikoto+anineko", want: []string{"anikoto", "anineko"}},
		{name: "pipe and semicolon", provider: "anipub|nyaa;anikoto", want: []string{"anipub", "nyaa", "anikoto"}},
		{name: "repeats and aliases", provider: `["AniPub","anipub","Kick-Ass Anime"]`, want: []string{"anipub", "kickassanime"}},
		{name: "unknown names dropped", provider: `["nonsense","anineko"]`, want: []string{"anineko"}},
		{name: "nothing known", provider: `["nonsense"]`, want: []string{"anikoto"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ConfiguredProviderNames(&Config{Provider: tc.provider}); !slices.Equal(got, tc.want) {
				t.Errorf("Provider=%q gives %v, want %v", tc.provider, got, tc.want)
			}
		})
	}
	if got := ConfiguredProviderNames(nil); !slices.Equal(got, fullTestStack) {
		t.Errorf("nil config: got %v", got)
	}
}

func TestCanonicalProviderConfigValueMigratesLegacyValues(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	for _, tc := range []struct{ raw, want string }{
		{raw: "anikoto", want: `["anikoto"]`},
		{raw: "anineko", want: `["anineko"]`},
		{raw: "anineko,anikoto", want: `["anineko","anikoto"]`},
		{raw: "", want: "stacked"},
		{raw: "STACK", want: "stacked"},
		{raw: strings.Join(fullTestStack, ","), want: "stacked"},
		{raw: `["anipub","senshi"]`, want: "stacked"},
	} {
		got := canonicalProviderConfigValue(tc.raw)
		if got != tc.want {
			t.Errorf("%q: got %s, want %s", tc.raw, got, tc.want)
		}
	}
}

func TestLoadConfigMigratesLegacyProviderToList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "otakase.conf")
	if err := os.WriteFile(path, []byte("Provider=anikoto\nAddMissingOptions=true\n"), 0644); err != nil {
		t.Fatalf("seeding the config: %v", err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if loaded.Provider != `["anikoto"]` {
		t.Fatalf("got provider %s, want [\"anikoto\"]", loaded.Provider)
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the config back: %v", err)
	}
	if !strings.Contains(string(saved), `Provider=["anikoto"]`) {
		t.Fatalf("config file was not migrated to anikoto-only list:\n%s", saved)
	}
	if strings.Contains(string(saved), "anineko") {
		t.Fatalf("migration added anineko without consent:\n%s", saved)
	}
}

func TestProviderQualifiedIDsRoundTrip(t *testing.T) {
	if got := QualifyProviderID("AniKoto", "21"); got != "anikoto::21" {
		t.Fatalf("QualifyProviderID = %q", got)
	}
	if got := QualifyProviderID("anikoto", "anikoto::21"); got != "anikoto::21" {
		t.Fatalf("an id already qualified for its provider was qualified again: %q", got)
	}
	if got := QualifyProviderID("nonsense", "21"); got != "21" {
		t.Fatalf("an unknown provider qualified the id: %q", got)
	}
	if got := QualifyProviderID("anikoto", ""); got != "" {
		t.Fatalf("an empty id was qualified: %q", got)
	}

	for _, tc := range []struct {
		id, name, raw string
		ok            bool
	}{
		{id: "anipub::frieren::s2", name: "anipub", raw: "frieren::s2", ok: true},
		{id: "frieren", raw: "frieren"},
		{id: "nonsense::21", raw: "nonsense::21"},
		{id: "anipub::", raw: "anipub::"},
	} {
		name, raw, ok := ParseProviderQualifiedID(tc.id)
		if name != tc.name || raw != tc.raw || ok != tc.ok {
			t.Errorf("%q: got (%q, %q, %v)", tc.id, name, raw, ok)
		}
	}
}

func TestProviderIDForAnimePrefersTheQualifiedID(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	anime := &Anime{ProviderName: "anipub", ProviderId: "anineko::slug"}
	if name, id := AnimeProviderID(anime); name != "anineko" || id != "slug" {
		t.Fatalf("got (%q, %q), want the provider the id names", name, id)
	}
	anime = &Anime{ProviderName: "AniPub", ProviderId: "42"}
	if name, id := AnimeProviderID(anime); name != "anipub" || id != "42" {
		t.Fatalf("got (%q, %q)", name, id)
	}
}

func TestQualifySelectionOptionTagsTheLabelOnce(t *testing.T) {
	got := qualifySelectionOption("anipub", SelectionOption{Key: "7", Label: "Show"})
	if got.Key != "anipub::7" || got.Label != "Show [anipub]" {
		t.Fatalf("got %+v", got)
	}
	again := qualifySelectionOption("anipub", got)
	if again.Key != "anipub::7" || again.Label != "Show [anipub]" {
		t.Fatalf("qualifying twice changed the option: %+v", again)
	}
}

func TestSearchAnimeReturnsProviderQualifiedStackResultsInOrder(t *testing.T) {
	anikoto := &stackStubProvider{
		name:          "anikoto",
		searchResults: map[string][]SelectionOption{"sub": {{Title: "First", Key: "anikoto-id", Label: "First"}}},
	}
	anineko := &stackStubProvider{
		name: "anineko",
		searchResults: map[string][]SelectionOption{"sub": {
			{Title: "Second", Key: "pahe-id", Label: "Second"},
			{Title: "Second again", Key: "pahe-id", Label: "Duplicate"},
		}},
	}
	withProviderFactories(t, anikoto, anineko)
	withGlobalConfig(t, &Config{Provider: `["anikoto","anineko"]`})

	got, err := SearchAnime("query", "sub")
	if err != nil {
		t.Fatalf("SearchAnime: %v", err)
	}
	keys := make([]string, len(got))
	for i, option := range got {
		keys[i] = option.Key
	}
	if want := []string{"anikoto::anikoto-id", "anineko::pahe-id"}; !slices.Equal(keys, want) {
		t.Fatalf("keys %v, want %v (qualified, stack order, repeats dropped)", keys, want)
	}
	if got[1].Label != "Second [anineko]" {
		t.Fatalf("label does not name the provider: %q", got[1].Label)
	}
}

func TestResolveEpisodeURLUsesProviderListOrder(t *testing.T) {
	anikoto := &stackStubProvider{
		name:           "anikoto",
		episodeResults: map[string]map[string][]string{"anikoto-id": {"sub": {"anikoto-sub"}}},
	}
	anineko := &stackStubProvider{
		name:           "anineko",
		episodeResults: map[string]map[string][]string{"pahe-id": {"sub": {"anineko-sub"}}},
		searchResults:  map[string][]SelectionOption{"sub": {{Title: "Example", Key: "pahe-id"}}},
	}
	withProviderFactories(t, anikoto, anineko)

	cfg := Config{Provider: `["anineko","anikoto"]`, SubOrDub: "sub"}
	anime := &Anime{Title: AnimeTitle{Romaji: "Example"}, ProviderName: "anikoto", ProviderId: "anikoto-id"}

	got, err := ResolveEpisodeURL(cfg, anime, 1)
	if err != nil {
		t.Fatalf("ResolveEpisodeURL: %v", err)
	}
	if got.ProviderName != "anineko" || got.ProviderID != "pahe-id" || got.Links[0] != "anineko-sub" {
		t.Fatalf("expected anineko to win by config order, got %#v", got)
	}
	if anime.ProviderName != "anineko" || anime.ProviderId != "pahe-id" {
		t.Fatalf("the mapping did not move to the provider that served: %+v", anime)
	}
}

func TestProviderNamesForAnimeAppendsTheStoredProvider(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	cfg := &Config{Provider: `["anineko","anipub"]`}
	got := providerNamesForAnime(cfg, &Anime{ProviderName: "nyaa", ProviderId: "x"})
	if want := []string{"anineko", "anipub", "nyaa"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	got = providerNamesForAnime(cfg, &Anime{ProviderName: "anipub", ProviderId: "x"})
	if want := []string{"anineko", "anipub"}; !slices.Equal(got, want) {
		t.Fatalf("a stored provider already in the stack was added again: %v", got)
	}
}

func TestFilterExcludedProviders(t *testing.T) {
	kept := filterExcludedProviders([]string{"anineko", "anikoto", "anipub"}, []string{"anineko", "ANINEKO"})
	if want := []string{"anikoto", "anipub"}; !slices.Equal(kept, want) {
		t.Fatalf("kept %v, want %v", kept, want)
	}
}

func TestResolveEpisodeURLExcludingProvidersSkipsTheFailedOne(t *testing.T) {
	anikoto := &stackStubProvider{
		name:           "anikoto",
		episodeResults: map[string]map[string][]string{"anikoto-id": {"sub": {"anikoto-sub"}}},
		searchResults:  map[string][]SelectionOption{"sub": {{Title: "Example", Key: "anikoto-id"}}},
	}
	anipub := &stackStubProvider{
		name:           "anipub",
		episodeResults: map[string]map[string][]string{"anipub-id": {"sub": {"anipub-sub"}}},
		searchResults:  map[string][]SelectionOption{"sub": {{Title: "Example", Key: "anipub-id"}}},
	}
	withProviderFactories(t, anikoto, anipub)

	cfg := Config{Provider: `["anipub","anikoto"]`, SubOrDub: "sub"}
	anime := &Anime{Title: AnimeTitle{Romaji: "Example"}, ProviderName: "anipub", ProviderId: "anipub-id"}

	got, err := ResolveEpisodeURLExcludingProvidersMode(cfg, anime, 1, []string{"anipub"}, cfg.SubOrDub)
	if err != nil {
		t.Fatalf("ResolveEpisodeURLExcludingProvidersMode: %v", err)
	}
	if got.ProviderName != "anikoto" || got.Links[0] != "anikoto-sub" {
		t.Fatalf("expected anikoto fallback, got %#v", got)
	}
	if len(anipub.calls) != 0 {
		t.Fatalf("the excluded provider was asked: %v", anipub.calls)
	}

	if _, err := ResolveEpisodeURLExcludingProvidersMode(cfg, anime, 1, []string{"anipub", "anikoto"}, "sub"); err == nil {
		t.Fatal("expected an error with every provider excluded")
	}
}

func TestResolveEpisodeURLExcludingProvidersModeUsesExplicitMode(t *testing.T) {
	anikoto := &stackStubProvider{
		name:           "anikoto",
		episodeResults: map[string]map[string][]string{"anikoto-id": {"dub": {"anikoto-dub"}, "sub": {"anikoto-sub"}}},
	}
	withProviderFactories(t, anikoto)

	cfg := Config{Provider: `["anikoto"]`, SubOrDub: "sub"}
	anime := &Anime{Title: AnimeTitle{Romaji: "Example"}, ProviderName: "anikoto", ProviderId: "anikoto-id"}

	got, err := ResolveEpisodeURLExcludingProvidersMode(cfg, anime, 1, nil, "dub")
	if err != nil {
		t.Fatalf("ResolveEpisodeURLExcludingProvidersMode: %v", err)
	}
	if got.Mode != "dub" || got.Links[0] != "anikoto-dub" {
		t.Fatalf("the mode asked for lost to SubOrDub: %#v", got)
	}
}

func TestResolveEpisodeURLAlternateModeWithPromptRequiresApproval(t *testing.T) {
	anikoto := &stackStubProvider{
		name:           "anikoto",
		episodeResults: map[string]map[string][]string{"anikoto-id": {"dub": {"anikoto-dub"}}},
	}
	withProviderFactories(t, anikoto)

	cfg := Config{Provider: `["anikoto"]`, SubOrDub: "sub"}
	anime := &Anime{Title: AnimeTitle{Romaji: "Example"}, ProviderName: "anikoto", ProviderId: "anikoto-id"}
	answer := func(key string) {
		withPromptSelect(t, func([]SelectionOption) (SelectionOption, error) {
			return SelectionOption{Key: key}, nil
		})
	}

	answer("cancel")
	if _, err := ResolveEpisodeURLAlternateModeWithPrompt(cfg, anime, 1, nil); err == nil {
		t.Fatal("declining the other audio still resolved")
	}
	if anime.ProviderId != "anikoto-id" {
		t.Fatalf("declining moved the mapping to %q", anime.ProviderId)
	}

	answer("play")
	got, err := ResolveEpisodeURLAlternateModeWithPrompt(cfg, anime, 1, nil)
	if err != nil {
		t.Fatalf("accepting the other audio failed: %v", err)
	}
	if got.Mode != "dub" || got.Links[0] != "anikoto-dub" {
		t.Fatalf("want the dub links, got %#v", got)
	}
}

// The recovery menu stops offering the other audio on this error, so it has
// to survive wrapping and keep the text it has always had.
func TestNoStreamsErrorIsRecognisedWhenWrapped(t *testing.T) {
	err := fmt.Errorf("resolve: %w", noStreamsError{mode: "dub"})
	if !isNoStreamsError(err) {
		t.Fatal("a wrapped noStreamsError was not recognised")
	}
	if !strings.HasSuffix(err.Error(), "no dub streams available") {
		t.Fatalf("unexpected text %q", err.Error())
	}
	if isNoStreamsError(errors.New("no dub streams available")) {
		t.Fatal("a plain error with the same text is not a noStreamsError")
	}
}

func TestResolveEpisodeURLForPlaybackTriesAllPreferredProvidersBeforeAudioFallback(t *testing.T) {
	sameShow := []SelectionOption{{Title: "Example", Key: "shared-id"}}
	anikoto := &stackStubProvider{
		name:           "anikoto",
		searchResults:  map[string][]SelectionOption{"dub": sameShow, "sub": sameShow},
		episodeResults: map[string]map[string][]string{"shared-id": {"sub": {"anikoto-sub"}}},
		episodeErrors:  map[string]map[string]error{"shared-id": {"dub": errors.New("no anikoto dub")}},
	}
	anineko := &stackStubProvider{
		name:           "anineko",
		episodeResults: map[string]map[string][]string{"pahe-id": {"dub": {"anineko-dub"}}},
		searchResults:  map[string][]SelectionOption{"dub": {{Title: "Example", Key: "pahe-id"}}},
	}
	withProviderFactories(t, anikoto, anineko)
	withPromptSelect(t, func([]SelectionOption) (SelectionOption, error) {
		t.Fatal("offered the other audio while a later provider still had dub")
		return SelectionOption{}, nil
	})

	cfg := Config{Provider: `["anikoto","anineko"]`, SubOrDub: "dub"}
	anime := &Anime{Title: AnimeTitle{Romaji: "Example"}, ProviderName: "anikoto", ProviderId: "shared-id"}

	got, err := ResolveEpisodeURLForPlayback(cfg, anime, 1)
	if err != nil {
		t.Fatalf("ResolveEpisodeURLForPlayback: %v", err)
	}
	if got.ProviderName != "anineko" || got.Mode != "dub" || got.Links[0] != "anineko-dub" {
		t.Fatalf("expected anineko dub before sub fallback, got %#v", got)
	}
	if slices.Contains(anikoto.calls, "anikoto:sub") {
		t.Fatalf("anikoto sub fallback was tried before anineko dub: %#v", anikoto.calls)
	}
}

func TestProviderTotalEpisodesReadsBothAudios(t *testing.T) {
	if got := inferTotalEpisodesFromEpisodeList([]string{"1", "2", "12.5", "x", "-3", " 11 "}); got != 12 {
		t.Fatalf("inferTotalEpisodesFromEpisodeList = %d, want 12", got)
	}
	if _, err := getProviderTotalEpisodes(nil, "id", "sub"); err == nil {
		t.Fatal("expected an error without a provider")
	}
}

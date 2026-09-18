package internal

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thexykril/otakase/internal/providers"
)

type stackStubProvider struct {
	name           string
	searchResults  map[string][]SelectionOption
	episodeResults map[string]map[string][]string
	episodeErrors  map[string]map[string]error
	searchCalls    []string
	calls          []string
}

func (s *stackStubProvider) Name() string { return s.name }

func (s *stackStubProvider) SearchAnime(query, mode string) ([]SelectionOption, error) {
	mode = normalizeTranslationType(mode)
	s.searchCalls = append(s.searchCalls, s.name+":"+mode+":"+query)
	return s.searchResults[mode], nil
}

func (s *stackStubProvider) EpisodesList(showID, mode string) ([]string, error) {
	return nil, nil
}

func (s *stackStubProvider) GetEpisodeURL(config CurdConfig, id string, epNo int) ([]string, error) {
	return s.GetEpisodeURLForMode(config, id, epNo, config.SubOrDub)
}

func (s *stackStubProvider) GetEpisodeURLForMode(config CurdConfig, id string, epNo int, mode string) ([]string, error) {
	mode = normalizeTranslationType(mode)
	s.calls = append(s.calls, s.name+":"+mode)
	if byID, ok := s.episodeErrors[id]; ok {
		if err := byID[mode]; err != nil {
			return nil, err
		}
	}
	if byID, ok := s.episodeResults[id]; ok {
		return byID[mode], nil
	}
	return nil, nil
}

func withProviderFactories(t *testing.T, stubs ...*stackStubProvider) {
	t.Helper()
	withAllProvidersEnabledForTest(t)
	restores := make([]func(), 0, len(stubs))
	for _, stub := range stubs {
		stub := stub
		restores = append(restores, providers.SetFactoryForTest(stub.name, func() providers.Provider {
			return &stackStubProviderBridge{stub}
		}))
	}
	t.Cleanup(func() {
		for i := len(restores) - 1; i >= 0; i-- {
			restores[i]()
		}
	})
}

type stackStubProviderBridge struct {
	*stackStubProvider
}

func (s *stackStubProviderBridge) SearchAnime(query, mode string) ([]providers.SelectionOption, error) {
	options, err := s.stackStubProvider.SearchAnime(query, mode)
	return toProviderSelectionOptions(options), err
}

func (s *stackStubProviderBridge) EpisodesList(showID, mode string) ([]string, error) {
	return s.stackStubProvider.EpisodesList(showID, mode)
}

func (s *stackStubProviderBridge) GetEpisodeURL(config providers.PlaybackConfig, id string, epNo int) ([]string, error) {
	return s.stackStubProvider.GetEpisodeURL(CurdConfig{SubOrDub: config.SubOrDub}, id, epNo)
}

func (s *stackStubProviderBridge) GetEpisodeURLForMode(config providers.PlaybackConfig, id string, epNo int, mode string) ([]string, error) {
	return s.stackStubProvider.GetEpisodeURLForMode(CurdConfig{SubOrDub: config.SubOrDub}, id, epNo, mode)
}

func TestConfiguredProviderNamesAcceptsOrderedLists(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	cases := []struct {
		name string
		cfg  *CurdConfig
		want []string
	}{
		{name: "empty", cfg: &CurdConfig{}, want: []string{"anikoto", "kickassanime", "anipub", "anineko", "nyaa", "anidb"}},
		{name: "json list", cfg: &CurdConfig{Provider: `["anikoto","anineko"]`}, want: []string{"anikoto", "anineko"}},
		{name: "comma list", cfg: &CurdConfig{Provider: "anineko,anikoto"}, want: []string{"anineko", "anikoto"}},
		{name: "plus list", cfg: &CurdConfig{Provider: "anikoto+anineko"}, want: []string{"anikoto", "anineko"}},
		{name: "legacy alias", cfg: &CurdConfig{Provider: "stacked"}, want: []string{"anikoto", "kickassanime", "anipub", "anineko", "nyaa", "anidb"}},
	}

	for _, tc := range cases {
		got := ConfiguredProviderNames(tc.cfg)
		if len(got) != len(tc.want) {
			t.Fatalf("%s: got %v, want %v", tc.name, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%s: got %v, want %v", tc.name, got, tc.want)
			}
		}
	}
}

func TestCanonicalProviderConfigValueMigratesLegacyValues(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "legacy anikoto", raw: "anikoto", want: `["anikoto"]`},
		{name: "legacy anineko", raw: "anineko", want: `["anineko"]`},
		{name: "ordered stack", raw: "anineko,anikoto", want: `["anineko","anikoto"]`},
		{name: "empty default", raw: "", want: "stacked"},
	}

	for _, tc := range cases {
		if got := canonicalProviderConfigValue(tc.raw); got != tc.want {
			t.Fatalf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestLoadConfigMigratesLegacyProviderToList(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "curd.conf")
	if err := os.WriteFile(configPath, []byte("Provider=anikoto\nAddMissingOptions=true\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if config.Provider != `["anikoto"]` {
		t.Fatalf("got provider %s, want [\"anikoto\"]", config.Provider)
	}

	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(contents), `Provider=["anikoto"]`) {
		t.Fatalf("config file was not migrated to anikoto-only list:\n%s", string(contents))
	}
	if strings.Contains(string(contents), "anineko") {
		t.Fatalf("migration added anineko without consent:\n%s", string(contents))
	}
}

func TestSearchAnimeReturnsProviderQualifiedStackResultsInOrder(t *testing.T) {
	anikoto := &stackStubProvider{
		name: "anikoto",
		searchResults: map[string][]SelectionOption{
			"sub": {{Title: "First", Key: "anikoto-id", Label: "First"}},
		},
	}
	anineko := &stackStubProvider{
		name: "anineko",
		searchResults: map[string][]SelectionOption{
			"sub": {{Title: "Second", Key: "pahe-id", Label: "Second"}},
		},
	}
	withProviderFactories(t, anikoto, anineko)

	previousConfig := GetGlobalConfig()
	SetGlobalConfig(&CurdConfig{Provider: `["anikoto","anineko"]`})
	t.Cleanup(func() { SetGlobalConfig(previousConfig) })

	results, err := SearchAnime("query", "sub")
	if err != nil {
		t.Fatalf("SearchAnime returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2: %#v", len(results), results)
	}
	if results[0].Key != "anikoto::anikoto-id" || results[1].Key != "anineko::pahe-id" {
		t.Fatalf("results were not provider-qualified in config order: %#v", results)
	}
}

func TestResolveEpisodeURLUsesProviderListOrder(t *testing.T) {
	anikoto := &stackStubProvider{
		name: "anikoto",
		episodeResults: map[string]map[string][]string{
			"anikoto-id": {"sub": {"anikoto-sub"}},
		},
	}
	anineko := &stackStubProvider{
		name: "anineko",
		episodeResults: map[string]map[string][]string{
			"pahe-id": {"sub": {"anineko-sub"}},
		},
		searchResults: map[string][]SelectionOption{
			"sub": {{Title: "Example", Key: "pahe-id"}},
		},
	}
	withProviderFactories(t, anikoto, anineko)

	cfg := CurdConfig{Provider: `["anineko","anikoto"]`, SubOrDub: "sub"}
	anime := &Anime{
		Title:        AnimeTitle{Romaji: "Example"},
		ProviderName: "anikoto",
		ProviderId:   "anikoto-id",
	}

	result, err := ResolveEpisodeURL(cfg, anime, 1)
	if err != nil {
		t.Fatalf("ResolveEpisodeURL returned error: %v", err)
	}
	if result.ProviderName != "anineko" || result.ProviderID != "pahe-id" || result.Links[0] != "anineko-sub" {
		t.Fatalf("expected anineko to win by config order, got %#v", result)
	}
}

func TestFilterExcludedProviders(t *testing.T) {
	filtered := filterExcludedProviders([]string{"anineko", "anikoto", "anipub"}, []string{"anineko", "ANINEKO"})
	if len(filtered) != 2 || filtered[0] != "anikoto" || filtered[1] != "anipub" {
		t.Fatalf("unexpected filtered providers: %#v", filtered)
	}
}

func TestResolveEpisodeURLExcludingProviders(t *testing.T) {
	anikoto := &stackStubProvider{
		name: "anikoto",
		episodeResults: map[string]map[string][]string{
			"anikoto-id": {"sub": {"anikoto-sub"}},
		},
		searchResults: map[string][]SelectionOption{
			"sub": {{Title: "Example", Key: "anikoto-id"}},
		},
	}
	anipub := &stackStubProvider{
		name: "anipub",
		episodeResults: map[string]map[string][]string{
			"anipub-id": {"sub": {"anipub-sub"}},
		},
		searchResults: map[string][]SelectionOption{
			"sub": {{Title: "Example", Key: "anipub-id"}},
		},
	}
	withProviderFactories(t, anikoto, anipub)

	cfg := CurdConfig{Provider: `["anipub","anikoto"]`, SubOrDub: "sub"}
	anime := &Anime{
		Title:        AnimeTitle{Romaji: "Example"},
		ProviderName: "anipub",
		ProviderId:   "anipub-id",
	}

	result, err := ResolveEpisodeURLExcludingProviders(cfg, anime, 1, []string{"anipub"})
	if err != nil {
		t.Fatalf("ResolveEpisodeURLExcludingProviders returned error: %v", err)
	}
	if result.ProviderName != "anikoto" || result.Links[0] != "anikoto-sub" {
		t.Fatalf("expected anikoto fallback, got %#v", result)
	}
}

func TestResolveEpisodeURLExcludingProvidersModeUsesExplicitMode(t *testing.T) {
	anikoto := &stackStubProvider{
		name: "anikoto",
		episodeResults: map[string]map[string][]string{
			"anikoto-id": {"dub": {"anikoto-dub"}, "sub": {"anikoto-sub"}},
		},
		searchResults: map[string][]SelectionOption{
			"dub": {{Title: "Example", Key: "anikoto-id"}},
			"sub": {{Title: "Example", Key: "anikoto-id"}},
		},
	}
	withProviderFactories(t, anikoto)

	cfg := CurdConfig{Provider: `["anikoto"]`, SubOrDub: "sub"}
	anime := &Anime{
		Title:        AnimeTitle{Romaji: "Example"},
		ProviderName: "anikoto",
		ProviderId:   "anikoto-id",
	}

	result, err := ResolveEpisodeURLExcludingProvidersMode(cfg, anime, 1, nil, "dub")
	if err != nil {
		t.Fatalf("ResolveEpisodeURLExcludingProvidersMode returned error: %v", err)
	}
	if result.Mode != "dub" || result.Links[0] != "anikoto-dub" {
		t.Fatalf("expected explicit dub mode, got %#v", result)
	}
}

func TestResolveEpisodeURLAlternateModeWithPromptRequiresApproval(t *testing.T) {
	anikoto := &stackStubProvider{
		name: "anikoto",
		episodeResults: map[string]map[string][]string{
			"anikoto-id": {"dub": {"anikoto-dub"}},
		},
		searchResults: map[string][]SelectionOption{
			"dub": {{Title: "Example", Key: "anikoto-id"}},
		},
	}
	withProviderFactories(t, anikoto)

	cfg := CurdConfig{Provider: `["anikoto"]`, SubOrDub: "sub"}
	anime := &Anime{
		Title:        AnimeTitle{Romaji: "Example"},
		ProviderName: "anikoto",
		ProviderId:   "anikoto-id",
	}

	withPromptSelect(t, func(options []SelectionOption) (SelectionOption, error) {
		return SelectionOption{Key: "cancel"}, nil
	})
	if _, err := ResolveEpisodeURLAlternateModeWithPrompt(cfg, anime, 1, nil); err == nil {
		t.Fatal("expected declined alternate mode to error")
	}
	if anime.ProviderId != "anikoto-id" {
		t.Fatalf("declined prompt should not change provider mapping, got %q", anime.ProviderId)
	}

	withPromptSelect(t, func(options []SelectionOption) (SelectionOption, error) {
		return SelectionOption{Key: "play"}, nil
	})
	result, err := ResolveEpisodeURLAlternateModeWithPrompt(cfg, anime, 1, nil)
	if err != nil {
		t.Fatalf("accepted alternate mode returned error: %v", err)
	}
	if result.Mode != "dub" || result.Links[0] != "anikoto-dub" {
		t.Fatalf("expected dub fallback, got %#v", result)
	}
}

func TestResolveEpisodeURLForPlaybackTriesAllPreferredProvidersBeforeAudioFallback(t *testing.T) {
	anikoto := &stackStubProvider{
		name: "anikoto",
		searchResults: map[string][]SelectionOption{
			"dub": {{Title: "Example", Key: "shared-id"}},
			"sub": {{Title: "Example", Key: "shared-id"}},
		},
		episodeResults: map[string]map[string][]string{
			"shared-id": {"sub": {"anikoto-sub"}},
		},
		episodeErrors: map[string]map[string]error{
			"shared-id": {"dub": errors.New("no anikoto dub")},
		},
	}
	anineko := &stackStubProvider{
		name: "anineko",
		episodeResults: map[string]map[string][]string{
			"pahe-id": {"dub": {"anineko-dub"}},
		},
		searchResults: map[string][]SelectionOption{
			"dub": {{Title: "Example", Key: "pahe-id"}},
		},
	}
	withProviderFactories(t, anikoto, anineko)
	withPromptSelect(t, func(options []SelectionOption) (SelectionOption, error) {
		t.Fatalf("audio fallback prompt should not be shown while another provider has preferred audio")
		return SelectionOption{}, nil
	})

	cfg := CurdConfig{Provider: `["anikoto","anineko"]`, SubOrDub: "dub"}
	anime := &Anime{
		Title:        AnimeTitle{Romaji: "Example"},
		ProviderName: "anikoto",
		ProviderId:   "shared-id",
	}

	result, err := ResolveEpisodeURLForPlayback(cfg, anime, 1)
	if err != nil {
		t.Fatalf("ResolveEpisodeURLForPlayback returned error: %v", err)
	}
	if result.ProviderName != "anineko" || result.Mode != "dub" || result.Links[0] != "anineko-dub" {
		t.Fatalf("expected anineko dub before sub fallback, got %#v", result)
	}
	for _, call := range anikoto.calls {
		if call == "anikoto:sub" {
			t.Fatalf("anikoto sub fallback was tried before anineko dub: %#v", anikoto.calls)
		}
	}
}

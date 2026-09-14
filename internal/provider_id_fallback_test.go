package internal

import (
	"fmt"
	"strings"
	"testing"

	"github.com/thexykril/otakase/internal/providers"
)

// narrowTitleProvider answers only for the exact queries it is given, the way a
// host with a short catalogue title does.
type narrowTitleProvider struct {
	name     string
	answers  map[string]string // query -> show id
	queried  []string
	episodes []string
}

func (p *narrowTitleProvider) Name() string { return p.name }

func (p *narrowTitleProvider) SearchAnime(query, mode string) ([]SelectionOption, error) {
	p.queried = append(p.queried, query)
	id, ok := p.answers[query]
	if !ok {
		return nil, fmt.Errorf("no results for %q", query)
	}
	return []SelectionOption{{Key: id, Label: query}}, nil
}

func (p *narrowTitleProvider) EpisodesList(showID, mode string) ([]string, error) {
	return p.episodes, nil
}

func (p *narrowTitleProvider) GetEpisodeURL(config CurdConfig, id string, epNo int) ([]string, error) {
	return nil, nil
}

// Resolving a provider for playback searched the full AniList title once and
// gave up. anineko carries "Rich Girl Caretaker: I'm Secretly the Caregiver of
// the Most Popular Girl in This Rich Kid School" only as "Rich Girl Caretaker",
// so episode 10 was reported missing on the one host that actually had it,
// while anipub -- which matched the long title but stops at episode 5 -- was the
// only host consulted. The same title variants the initial search already uses
// are now tried here too.
func TestFindProviderIDFallsBackToTitleVariants(t *testing.T) {
	provider := &narrowTitleProvider{
		name:    "anineko",
		answers: map[string]string{"Rich Girl Caretaker": "rich-girl-caretaker"},
	}
	anime := &Anime{
		Title: AnimeTitle{
			English: "Rich Girl Caretaker: I'm Secretly the Caregiver of the Most Popular Girl in This Rich Kid School",
			Romaji:  "Saijo no Osewa: Takane no Hanadarake na Meimonkou de",
		},
	}
	config := &CurdConfig{AnimeNameLanguage: "english"}

	id, err := findProviderIDForAnime(provider, config, anime, "dub")
	if err != nil {
		t.Fatalf("expected the short title to resolve, got %v", err)
	}
	if id != "rich-girl-caretaker" {
		t.Fatalf("resolved the wrong id: %q", id)
	}
	if len(provider.queried) < 2 {
		t.Fatalf("expected the full title to be tried before the short one, got %q", provider.queried)
	}
	if provider.queried[0] != anime.Title.English {
		t.Fatalf("the exact title must be tried first, got %q", provider.queried[0])
	}
}

// A provider that answers the exact title must not be sent the broader
// simplified forms, which are likelier to match the wrong show.
func TestFindProviderIDStopsAtTheFirstMatch(t *testing.T) {
	provider := &narrowTitleProvider{
		name:    "anipub",
		answers: map[string]string{"Rich Girl Caretaker: I'm Secretly the Caregiver": "8433"},
	}
	anime := &Anime{Title: AnimeTitle{English: "Rich Girl Caretaker: I'm Secretly the Caregiver"}}

	id, err := findProviderIDForAnime(provider, &CurdConfig{AnimeNameLanguage: "english"}, anime, "sub")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "8433" {
		t.Fatalf("wrong id %q", id)
	}
	if len(provider.queried) != 1 {
		t.Fatalf("expected a single search, got %q", provider.queried)
	}
}

// When nothing matches, the error must still name the title the user knows.
func TestFindProviderIDReportsThePrimaryTitle(t *testing.T) {
	provider := &narrowTitleProvider{name: "anineko", answers: map[string]string{}}
	anime := &Anime{Title: AnimeTitle{English: "Some Show That Is Not Carried"}}

	_, err := findProviderIDForAnime(provider, &CurdConfig{AnimeNameLanguage: "english"}, anime, "sub")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "Some Show That Is Not Carried") {
		t.Fatalf("error should name the primary title, got %v", err)
	}
}

// A provider already mapped to this anime keeps its id without searching.
func TestFindProviderIDUsesTheExistingMapping(t *testing.T) {
	provider := &narrowTitleProvider{name: "anipub", answers: map[string]string{}}
	anime := &Anime{
		Title:        AnimeTitle{English: "Anything"},
		ProviderName: "anipub",
		ProviderId:   "8433",
	}

	id, err := findProviderIDForAnime(provider, &CurdConfig{}, anime, "sub")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "8433" {
		t.Fatalf("wrong id %q", id)
	}
	if len(provider.queried) != 0 {
		t.Fatalf("expected no search at all, got %q", provider.queried)
	}
}

// staleMappingProvider serves episodes only for its own real id, and rejects
// anything else the way anipub rejects a slug from another host.
type staleMappingProvider struct {
	name     string
	realID   string
	title    string
	searched int
}

func (p *staleMappingProvider) Name() string { return p.name }

func (p *staleMappingProvider) SearchAnime(query, mode string) ([]providers.SelectionOption, error) {
	p.searched++
	if query != p.title {
		return nil, fmt.Errorf("no results for %q", query)
	}
	return []providers.SelectionOption{{Key: p.realID, Label: p.title}}, nil
}

func (p *staleMappingProvider) EpisodesList(showID, mode string) ([]string, error) {
	return []string{"1"}, nil
}

func (p *staleMappingProvider) GetEpisodeURL(config providers.PlaybackConfig, id string, epNo int) ([]string, error) {
	if id != p.realID {
		return nil, fmt.Errorf("invalid %s show id %q", p.name, id)
	}
	return []string{"https://example.test/stream.m3u8"}, nil
}

// Curd used to file the id of whichever provider served an episode under the
// name of the first configured one, so an anineko slug was stored as an anipub
// id and handed back to anipub forever after. Those histories already exist, so
// a stored id that does not work is re-derived rather than believed.
func TestStaleStoredMappingIsRederived(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	const title = "Rich Girl Caretaker"
	provider := &staleMappingProvider{name: "anipub", realID: "8433", title: title}
	restore := providers.SetFactoryForTest("anipub", func() providers.Provider { return provider })
	t.Cleanup(restore)

	anime := &Anime{
		Title: AnimeTitle{English: title},
		// The corrupted pairing: another host's slug filed under anipub.
		ProviderName: "anipub",
		ProviderId:   "rich-girl-caretaker-im-secretly-the-caregiver",
	}
	config := CurdConfig{AnimeNameLanguage: "english", SubOrDub: "sub"}

	result, err := episodeModeResultWithProviders(config, anime, 1, "sub", []string{"anipub"})
	if err != nil {
		t.Fatalf("expected the stale mapping to be re-derived, got %v", err)
	}
	if result.ProviderID != "8433" {
		t.Fatalf("expected the re-derived id, got %q", result.ProviderID)
	}
	if len(result.Links) == 0 {
		t.Fatal("expected links from the re-derived mapping")
	}
	if anime.ProviderId != "8433" {
		t.Fatalf("the anime should carry the corrected id, got %q", anime.ProviderId)
	}
}

// A good stored mapping must not trigger a needless second search.
func TestGoodStoredMappingIsNotResearched(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	provider := &staleMappingProvider{name: "anipub", realID: "8433", title: "Rich Girl Caretaker"}
	restore := providers.SetFactoryForTest("anipub", func() providers.Provider { return provider })
	t.Cleanup(restore)

	anime := &Anime{
		Title:        AnimeTitle{English: "Rich Girl Caretaker"},
		ProviderName: "anipub",
		ProviderId:   "8433",
	}

	if _, err := episodeModeResultWithProviders(CurdConfig{AnimeNameLanguage: "english"}, anime, 1, "sub", []string{"anipub"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.searched != 0 {
		t.Fatalf("expected no search for a working stored id, got %d", provider.searched)
	}
}

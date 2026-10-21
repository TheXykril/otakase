package internal

import "testing"

func TestNormalizeSearchTitleSeasons(t *testing.T) {
	for in, want := range map[string]string{
		"Dandadan 2nd Season":                  "dandadan s2",
		"DAN DA DAN Season 2":                  "dan da dan s2",
		"Dandadan S02":                         "dandadan s2",
		"Mushoku Tensei II":                    "mushoku tensei s2",
		"Dan Da Dan S1":                        "dan da dan",
		"SPY×FAMILY":                           "spy x family",
		"Shingeki no Kyojin: The Final Season": "shingeki no kyojin the final season",
	} {
		if got := normalizeSearchTitle(in); got != want {
			t.Errorf("normalizeSearchTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

// A search for a sequel also finds the first season, and the first season's
// name matches the broader title variants exactly. The sequel must still win,
// and the first season must win for the first season.
func TestSeasonDecidesBetweenSeasons(t *testing.T) {
	options := []SelectionOption{
		{Key: "Dandadan", Title: "Dandadan"},
		{Key: "Dandadan S2", Title: "Dandadan S2"},
	}
	sequel := &Anime{Title: AnimeTitle{Romaji: "Dandadan 2nd Season", English: "DAN DA DAN Season 2"}}
	if best, _ := selectBestProviderSearchResult(options, sequel, "Dandadan"); best.Key != "Dandadan S2" {
		t.Errorf("sequel mapped to %q", best.Key)
	}
	first := &Anime{Title: AnimeTitle{Romaji: "Dandadan", English: "DAN DA DAN"}}
	reversed := []SelectionOption{options[1], options[0]}
	if best, _ := selectBestProviderSearchResult(reversed, first, "Dandadan"); best.Key != "Dandadan" {
		t.Errorf("first season mapped to %q", best.Key)
	}
}

type looseSearchProvider struct{ results map[string][]SelectionOption }

func (p looseSearchProvider) Name() string { return "loose" }
func (p looseSearchProvider) SearchAnime(title, _ string) ([]SelectionOption, error) {
	return p.results[title], nil
}
func (p looseSearchProvider) EpisodesList(string, string) ([]string, error) { return nil, nil }
func (p looseSearchProvider) GetEpisodeURL(Config, string, int) ([]string, error) {
	return nil, nil
}

// A host that matches loosely answers a title it does not carry with whatever
// shares a word. That must not stop the search before a later title variant
// finds the show.
func TestUnrelatedResultsDoNotEndTheSearch(t *testing.T) {
	anime := &Anime{Title: AnimeTitle{Romaji: "Dandadan 2nd Season", English: "DAN DA DAN Season 2"}}
	provider := looseSearchProvider{results: map[string][]SelectionOption{
		"DAN DA DAN Season 2": {{Key: "Juuou Mujin Dandivine", Title: "Juuou Mujin Dandivine"}},
		"Dandadan 2nd Season": {{Key: "Dandadan S2", Title: "Dandadan S2"}},
	}}
	config := &Config{AnimeNameLanguage: "english"}
	id, err := searchProviderIDForAnime(provider, config, anime, "sub")
	if err != nil || id != "Dandadan S2" {
		t.Fatalf("mapped to %q (%v)", id, err)
	}

	// With nothing better anywhere, the loose answer is still taken.
	provider.results["Dandadan 2nd Season"] = nil
	if id, _ := searchProviderIDForAnime(provider, config, anime, "sub"); id != "Juuou Mujin Dandivine" {
		t.Fatalf("mapped to %q, want the only result", id)
	}
}

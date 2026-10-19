package internal

import "testing"

// marchComesInLikeALion is the case that needed a romaji AniList title to
// match a host listing the show by its English name, next to its sequel.
func marchComesInLikeALion() (*Anime, []SelectionOption) {
	anime := &Anime{
		AnilistId:     21366,
		MalId:         31646,
		TotalEpisodes: 22,
		Title:         AnimeTitle{Romaji: "3-gatsu no Lion", English: "March comes in like a lion"},
	}
	options := []SelectionOption{
		{Title: "March comes in like a lion Season 2", Key: "s2", Label: "March comes in like a lion Season 2 (22 episodes)", Thumbnail: "https://s4.anilist.co/file/anilistcdn/media/anime/cover/large/bx98478-dF3mpSKiZkQu.jpg"},
		{Title: "March comes in like a lion", Key: "s1", Label: "March comes in like a lion (23 episodes)", Thumbnail: "https://cdn.myanimelist.net/images/anime/1637/108857.jpg"},
	}
	return anime, options
}

func TestSelectBestProviderSearchResultMarchComesInLikeALion(t *testing.T) {
	anime, options := marchComesInLikeALion()
	best, ok := selectBestProviderSearchResult(options, anime, "3-gatsu no Lion")
	if !ok || best.Key != "s1" {
		t.Fatalf("got %q (%v), want season 1", best.Key, ok)
	}
}

func TestConfidentProviderSearchMatchMarchComesInLikeALion(t *testing.T) {
	anime, options := marchComesInLikeALion()
	best, ok := confidentProviderSearchMatch(options, anime, "3-gatsu no Lion")
	if !ok || best.Key != "s1" {
		t.Fatalf("got %q (%v), want a confident season 1", best.Key, ok)
	}
}

func TestConfidentProviderSearchMatchNeedsAClearWinner(t *testing.T) {
	// Only the query is compared, so an exact title is worth 100.
	anime := &Anime{}

	// Two exact titles score within 30 of each other.
	tied := []SelectionOption{{Key: "a", Title: "Frieren"}, {Key: "b", Title: "Frieren"}}
	if _, ok := confidentProviderSearchMatch(tied, anime, "Frieren"); ok {
		t.Fatal("picked one of two identical titles")
	}
	// A partial title alone is not enough.
	if _, ok := confidentProviderSearchMatch([]SelectionOption{{Key: "a", Title: "Frieren Season 2"}}, anime, "Frieren"); ok {
		t.Fatal("a partial title was taken as confident")
	}
	// A single exact title is.
	if best, ok := confidentProviderSearchMatch([]SelectionOption{{Key: "a", Title: "frieren!"}}, anime, "Frieren"); !ok || best.Key != "a" {
		t.Fatal("a single exact title was not taken")
	}
	// The AniList id in the cover is enough on its own.
	anime.AnilistId = 154587
	cover := []SelectionOption{
		{Key: "a", Title: "Frieren"},
		{Key: "b", Title: "Sousou no Frieren", Thumbnail: "https://example.org/bx154587.jpg"},
	}
	if best, ok := confidentProviderSearchMatch(cover, anime, "Frieren"); !ok || best.Key != "b" {
		t.Fatalf("got %q (%v), want the result with the AniList cover", best.Key, ok)
	}
	if _, ok := confidentProviderSearchMatch(nil, anime, "Frieren"); ok {
		t.Fatal("matched among no results")
	}
}

func TestSelectBestProviderSearchResultKeepsTheHostsOrderOnATie(t *testing.T) {
	anime := &Anime{}
	options := []SelectionOption{{Key: "first", Title: "x"}, {Key: "second", Title: "y"}}
	if best, _ := selectBestProviderSearchResult(options, anime, "unrelated"); best.Key != "first" {
		t.Fatalf("got %q, want the host's first result", best.Key)
	}
	if _, ok := selectBestProviderSearchResult(nil, anime, "x"); ok {
		t.Fatal("picked from no results")
	}
}

func TestNormalizeSearchTitle(t *testing.T) {
	for in, want := range map[string]string{
		"  Re:Zero - Starting Life  ": "rezero starting life",
		"JoJo's Bizarre_Adventure":    "jojos bizarre adventure",
		"K-On!!":                      "k on",
		"Steins;Gate (2011)":          "steins;gate 2011",
		`"Oshi no Ko"?`:               "oshi no ko",
		"Dr. Stone":                   "dr stone",
	} {
		if got := normalizeSearchTitle(in); got != want {
			t.Errorf("normalizeSearchTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAnimeSearchTitleFallsBackThroughTitles(t *testing.T) {
	if got := animeSearchTitle(nil); got != "" {
		t.Fatalf("nil anime: %q", got)
	}
	if got := animeSearchTitle(&Anime{Title: AnimeTitle{English: " Frieren "}}); got != "Frieren" {
		t.Fatalf("got %q", got)
	}
}

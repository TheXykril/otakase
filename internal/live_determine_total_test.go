package internal

import (
	"os"
	"testing"
)

// TestLiveProviderEpisodeTotals asks the real providers for One
// Piece's episode count. Anything under 1000 means the wrong show matched or
// the episode list came back truncated. Opt-in, since it hits the network.
func TestLiveProviderEpisodeTotals(t *testing.T) {
	if os.Getenv("OTAKASE_LIVE_DETERMINE_TOTAL") == "" {
		t.Skip("set OTAKASE_LIVE_DETERMINE_TOTAL=1 to run live")
	}
	const query = "One Piece"
	cfg := &Config{
		Provider:       "stacked",
		SubOrDub:       "sub",
		StoragePath:    os.ExpandEnv("$HOME/.local/share/otakase"),
		TrackingRemote: TrackingRemoteMyAnimeList,
	}
	withGlobalConfig(t, cfg) // the providers read it

	show := &Anime{
		AnilistId: 21,
		MalId:     21,
		Title: AnimeTitle{
			English:  query,
			Romaji:   query,
			Japanese: "ONE PIECE",
		},
		TotalEpisodes: 1150,
	}
	show.IsAiring = true // still running, so the total is open-ended

	// Per-provider breakdown, so a bad total can be traced to its source.
	for _, name := range configuredProviderNames(cfg) {
		p, perr := ProviderByName(name)
		if perr != nil {
			t.Logf("[%s] unavailable: %v", name, perr)
			continue
		}
		hits, serr := p.SearchAnime(query, "sub")
		if serr != nil || len(hits) == 0 {
			t.Logf("[%s] search: %d results, err=%v", name, len(hits), serr)
			continue
		}
		pick, _ := selectBestProviderSearchResult(hits, show, query)
		n, terr := getProviderTotalEpisodes(p, pick.Key, "sub")
		t.Logf("[%s] matched %q: total=%d err=%v", name, pick.Key, n, terr)
	}

	got, err := determineProviderTotalEpisodes(cfg, query, show, "sub")
	switch {
	case err != nil:
		t.Fatalf("no total from any provider: %v", err)
	case got < 1000:
		t.Fatalf("total %d is too low for One Piece: wrong match or a cut-off episode list", got)
	}
	t.Logf("One Piece has %d episodes", got)
}

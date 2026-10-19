package internal

import (
	"os"
	"testing"
	"time"
)

// TestIntroDBLive checks the two outside assumptions this source rests on: that
// the id mapping still carries AniList ids with TMDB seasons, and that
// theintrodb still answers for anime.
//
// Gated because it downloads several megabytes and talks to two services. Run
// it with OTAKASE_LIVE_INTRODB=1.
func TestIntroDBLive(t *testing.T) {
	if os.Getenv("OTAKASE_LIVE_INTRODB") != "1" {
		t.Skip("set OTAKASE_LIVE_INTRODB=1 to check against the real services")
	}

	storage := t.TempDir()
	mapping := tmdbMappingFor(storage)

	// The first ask starts the table and answers "not yet", which is the
	// behaviour that keeps this off the path of starting an episode.
	if _, ok := mapping.Show(154587); ok {
		t.Log("the table was already loaded")
	}

	deadline := time.Now().Add(3 * time.Minute)
	var show tmdbShow
	for time.Now().Before(deadline) {
		if found, ok := mapping.Show(154587); ok {
			show = found
			break
		}
		time.Sleep(2 * time.Second)
	}
	if show.ID == 0 {
		t.Fatal("Frieren never appeared in the mapping")
	}
	t.Logf("anilist 154587 -> tmdb %d season %d", show.ID, show.Season)

	times, found, err := newIntroDBSource(storage).Lookup(SkipRef{AniListID: 154587, Episode: 1})
	if err != nil {
		t.Fatalf("theintrodb: %v", err)
	}
	t.Logf("found=%v op=%+v ed=%+v", found, times.Op, times.Ed)
	if !found {
		t.Error("theintrodb no longer answers for an episode it used to know")
	}
}

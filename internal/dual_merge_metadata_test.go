package internal

import (
	"testing"
	"time"
)

// Only AniList reports a broadcast schedule. MyAnimeList has no equivalent, so
// whichever entry wins a dual-tracking merge, the merged one has to keep it.
//
// It is the MyAnimeList entry that wins in practice: otakase pushes progress there
// when an episode finishes, which makes it the more recently updated of the two.
// Losing the schedule then makes every show look like its airing dates are
// unknown -- no countdown in the list, and no way to tell "you are caught up"
// apart from "that episode could not be found", which is what the user sees.
func TestDualMergeKeepsTheAiringSchedule(t *testing.T) {
	airing := &NextAiringEpisodeInfo{Episode: 11, TimeUntilAiring: 211519}

	aniList := Entry{
		Media: Media{
			ID: 201514, Episodes: 12, Status: "RELEASING", Format: "TV",
			Title:             AnimeTitle{English: "Rich Girl Caretaker"},
			NextAiringEpisode: airing,
		},
		Progress:  10,
		UpdatedAt: time.Now().Add(-time.Hour),
	}
	// MyAnimeList knows nothing about broadcast dates, and is the fresher entry.
	myAnimeList := Entry{
		Media: Media{
			ID: 201514, MalID: 62876, Episodes: 12,
			Title: AnimeTitle{English: "Rich Girl Caretaker"},
		},
		Progress:  10,
		UpdatedAt: time.Now(),
	}

	merged := mergeAnimeEntries(aniList, myAnimeList)
	if merged.Media.NextAiringEpisode == nil {
		t.Fatal("the merged entry lost the airing schedule")
	}
	if merged.Media.NextAiringEpisode.Episode != 11 {
		t.Errorf("wrong episode: %d", merged.Media.NextAiringEpisode.Episode)
	}
	if merged.Media.NextAiringEpisode.TimeUntilAiring != 211519 {
		t.Errorf("wrong countdown: %d", merged.Media.NextAiringEpisode.TimeUntilAiring)
	}

	// The other fields only one side knows must survive too.
	if merged.Media.Status != "RELEASING" {
		t.Errorf("status lost: %q", merged.Media.Status)
	}
	if merged.Media.Format != "TV" {
		t.Errorf("format lost: %q", merged.Media.Format)
	}
	if merged.Media.MalID != 62876 {
		t.Errorf("MAL id lost: %d", merged.Media.MalID)
	}
}

// Merging the other way round must be just as lossless.
func TestDualMergeKeepsTheScheduleWhicheverSideWins(t *testing.T) {
	airing := &NextAiringEpisodeInfo{Episode: 11, TimeUntilAiring: 211519}
	aniList := Entry{
		Media:     Media{ID: 201514, Status: "RELEASING", NextAiringEpisode: airing},
		UpdatedAt: time.Now(),
	}
	myAnimeList := Entry{
		Media:     Media{ID: 201514, MalID: 62876},
		UpdatedAt: time.Now().Add(-time.Hour),
	}

	if merged := mergeAnimeEntries(aniList, myAnimeList); merged.Media.NextAiringEpisode == nil {
		t.Fatal("schedule lost when AniList wins")
	}
	if merged := mergeAnimeEntries(myAnimeList, aniList); merged.Media.NextAiringEpisode == nil {
		t.Fatal("schedule lost when MyAnimeList wins")
	}
}

// The whole point: a dual-tracking user must get the same "caught up" answer a
// single-tracker user gets.
func TestCaughtUpIsDetectedAfterADualMerge(t *testing.T) {
	airing := &NextAiringEpisodeInfo{Episode: 11, TimeUntilAiring: 211519}
	merged := mergeAnimeEntries(
		Entry{Media: Media{ID: 201514, Status: "RELEASING", NextAiringEpisode: airing}, UpdatedAt: time.Now().Add(-time.Hour)},
		Entry{Media: Media{ID: 201514, MalID: 62876}, UpdatedAt: time.Now()},
	)

	withTrackedAnime(t, merged)
	got := nextEpisodeAiring(&Anime{AnilistId: 201514, TotalEpisodes: 12}, 11)
	if got.Aired {
		t.Fatal("episode 11 should read as unaired for a dual-tracking user too")
	}
	if got.Delay != "2d" {
		t.Errorf("expected a countdown, got %q", got.Delay)
	}
}

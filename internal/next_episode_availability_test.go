package internal

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// withTrackedAnime puts one entry in the global list so the prompt can read the
// airing schedule the way it does at the end of an episode.
func withTrackedAnime(t *testing.T, entry Entry) {
	t.Helper()
	previous := GetGlobalUser()
	SetGlobalUser(&User{AnimeList: AnimeList{Watching: []Entry{entry}}})
	t.Cleanup(func() { SetGlobalUser(previous) })
}

func airingEntry(anilistID, nextEpisode, secondsUntil int) Entry {
	return Entry{
		Media: Media{
			ID:     anilistID,
			Status: "RELEASING",
			NextAiringEpisode: &NextAiringEpisodeInfo{
				Episode:         nextEpisode,
				TimeUntilAiring: secondsUntil,
			},
		},
	}
}

// Offering episode 11 when only 10 have aired sends the user into a dead end:
// every provider is searched and the failure reads as a broken tool, when in
// fact the episode has simply not been broadcast.
func TestNextEpisodeIsReportedAsUnairedWithTheWait(t *testing.T) {
	// Episode 11 is next, four days out; ten have aired.
	withTrackedAnime(t, airingEntry(201514, 11, int(4*24*time.Hour/time.Second)))
	anime := &Anime{AnilistId: 201514, TotalEpisodes: 12}

	got := nextEpisodeAiring(anime, 11)
	if got.Aired {
		t.Fatal("episode 11 has not aired and must not be offered")
	}
	if got.Delay != "4d" {
		t.Errorf("expected the wait, got %q", got.Delay)
	}
	if notice := unairedEpisodeNotice(11, got); notice != "Episode 11 airs in 4d" {
		t.Errorf("unexpected notice %q", notice)
	}
}

// Everything below the next-airing episode is watchable.
func TestAiredEpisodesAreStillOffered(t *testing.T) {
	withTrackedAnime(t, airingEntry(201514, 11, 3600))
	anime := &Anime{AnilistId: 201514, TotalEpisodes: 12}

	for _, ep := range []int{1, 9, 10} {
		if got := nextEpisodeAiring(anime, ep); !got.Aired {
			t.Errorf("episode %d has aired and must remain offerable", ep)
		}
	}
}

// A countdown belongs only to the very next episode. Episode 12 airs a week
// after 11, and reusing 11's wait would understate it.
func TestOnlyTheImmediateNextEpisodeGetsACountdown(t *testing.T) {
	withTrackedAnime(t, airingEntry(201514, 11, 3600))
	anime := &Anime{AnilistId: 201514, TotalEpisodes: 12}

	got := nextEpisodeAiring(anime, 12)
	if got.Aired {
		t.Fatal("episode 12 has not aired")
	}
	if got.Delay != "" {
		t.Errorf("expected no countdown for a later episode, got %q", got.Delay)
	}
	if notice := unairedEpisodeNotice(12, got); !strings.Contains(notice, "has not aired") {
		t.Errorf("expected a plain notice, got %q", notice)
	}
}

// When the schedule is unknown the episode must stay offerable. Refusing one the
// user could have watched is worse than letting a lookup fail, which is what
// happened before this existed anyway.
func TestUnknownScheduleLeavesTheEpisodeOfferable(t *testing.T) {
	anime := &Anime{AnilistId: 201514, TotalEpisodes: 12}

	// No tracked entry at all.
	withTrackedAnime(t, Entry{Media: Media{ID: 999}})
	if got := nextEpisodeAiring(anime, 11); !got.Aired {
		t.Error("an untracked show must not have episodes refused")
	}

	// A finished show has no next airing episode.
	withTrackedAnime(t, Entry{Media: Media{ID: 201514, Status: "FINISHED"}})
	if got := nextEpisodeAiring(anime, 11); !got.Aired {
		t.Error("a finished show has everything out; nothing should be refused")
	}

	// Untracked locally.
	if got := nextEpisodeAiring(&Anime{}, 2); !got.Aired {
		t.Error("an anime with no AniList id must not have episodes refused")
	}
}

// The list rows and the prompt phrase the same wait differently; both come from
// one formatter so they cannot drift apart.
func TestAiringDelayFormatting(t *testing.T) {
	cases := map[int]string{
		int(4 * 24 * time.Hour / time.Second): "4d",
		int(22 * time.Hour / time.Second):     "22h",
		int(35 * time.Minute / time.Second):   "35m",
		30:                                    "<1m",
		0:                                     "",
		-5:                                    "",
	}
	for seconds, want := range cases {
		if got := formatAiringDelay(seconds); got != want {
			t.Errorf("formatAiringDelay(%d) = %q, want %q", seconds, got, want)
		}
	}
	// The row phrasing still reads as before.
	if got := formatTimeUntilAiring(int(22 * time.Hour / time.Second)); got != "next in 22h" {
		t.Errorf("row countdown changed: %q", got)
	}
	if got := formatTimeUntilAiring(0); got != "" {
		t.Errorf("expected no countdown, got %q", got)
	}
}

// The same situation reached two ways -- finishing an episode, or picking the
// show again from the list a day later -- has to answer the same. The second
// route used to search every provider and fail on an episode that does not exist
// yet, which reads as a broken tool rather than as being caught up.
func TestUnairedEpisodeOffersToTryAnyway(t *testing.T) {
	withTrackedAnime(t, airingEntry(201514, 11, int(4*24*time.Hour/time.Second)))
	availability := nextEpisodeAiring(&Anime{AnilistId: 201514, TotalEpisodes: 12}, 11)

	var shown []SelectionOption
	withPromptSelect(t, func(options []SelectionOption) (SelectionOption, error) {
		shown = options
		return SelectionOption{Key: "wait"}, nil
	})

	if confirmUnairedEpisode(&CurdConfig{}, 11, availability) {
		t.Error("choosing Done must not start a search")
	}

	// The escape hatch matters: the schedule comes from a cached list, so an
	// episode that aired an hour ago can still read as upcoming, and a flat
	// refusal would have curd overruling the user on something it half knows.
	var keys []string
	for _, option := range shown {
		keys = append(keys, option.Key)
	}
	if !strings.Contains(strings.Join(keys, ","), "try") {
		t.Fatalf("expected a way to look anyway, got options %v", keys)
	}

	withPromptSelect(t, func(options []SelectionOption) (SelectionOption, error) {
		return SelectionOption{Key: "try"}, nil
	})
	if !confirmUnairedEpisode(&CurdConfig{}, 11, availability) {
		t.Error("choosing to look anyway must proceed")
	}
}

// A menu that cannot be shown must not be read as permission to continue.
func TestUnairedEpisodePromptFailureDoesNotProceed(t *testing.T) {
	withTrackedAnime(t, airingEntry(201514, 11, 3600))
	availability := nextEpisodeAiring(&Anime{AnilistId: 201514, TotalEpisodes: 12}, 11)

	withPromptSelect(t, func(options []SelectionOption) (SelectionOption, error) {
		return SelectionOption{}, errors.New("no display")
	})
	if confirmUnairedEpisode(&CurdConfig{}, 11, availability) {
		t.Error("a failed prompt must not be treated as consent to search")
	}
}

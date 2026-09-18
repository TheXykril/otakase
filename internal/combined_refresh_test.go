package internal

import (
	"testing"
	"time"
)

// Both trackers answer from cache and refresh behind the launch, so the merge
// that dual tracking shows is built from two caches. The refreshes did arrive --
// into the two sub-lists, which nothing read once the merged list replaced them
// -- so the whole session ran on stale data: an episode that aired an hour ago
// still read as unreleased, and progress shown was yesterday's.
func TestCombinedRefreshRepublishesTheMergedList(t *testing.T) {
	airing := &NextAiringEpisodeInfo{Episode: 11, TimeUntilAiring: 3600}

	// What the caches held at launch: episode 11 still to come.
	cached := AnimeList{Watching: []Entry{{
		Media:    Media{ID: 201514, Status: "RELEASING", NextAiringEpisode: airing},
		Progress: 9,
		Status:   "CURRENT",
	}}}
	// What the trackers actually say now: 10 watched, 12 next.
	freshAni := AnimeList{Watching: []Entry{{
		Media:     Media{ID: 201514, Status: "RELEASING", NextAiringEpisode: &NextAiringEpisodeInfo{Episode: 12, TimeUntilAiring: 600}},
		Progress:  10,
		Status:    "CURRENT",
		UpdatedAt: time.Now(),
	}}}
	freshMal := freshAni

	user := &User{ListSync: NewAnimeListSync(cached), AnimeList: cached}
	aniListUser := &User{ListSync: NewAnimeListSync(cached)}
	myAnimeListUser := &User{ListSync: NewAnimeListSync(cached)}

	updates := user.ListSync.Updates()

	go refreshCombinedRemoteAnimeList(&Config{StoragePath: t.TempDir()}, user, aniListUser, myAnimeListUser)

	// The refreshes land after the launch has already shown the cached merge.
	aniListUser.ListSync.Replace(freshAni, false)
	aniListUser.ListSync.MarkRefreshDone()
	myAnimeListUser.ListSync.Replace(freshMal, false)
	myAnimeListUser.ListSync.MarkRefreshDone()

	select {
	case <-user.ListSync.RefreshDone():
	case <-time.After(5 * time.Second):
		t.Fatal("the combined refresh never finished; callers block on this")
	}

	current := user.ListSync.Current()
	if len(current.Watching) != 1 {
		t.Fatalf("unexpected list %+v", current)
	}
	if got := current.Watching[0].Progress; got != 10 {
		t.Errorf("merged list still shows cached progress %d", got)
	}
	if next := current.Watching[0].Media.NextAiringEpisode; next == nil || next.Episode != 12 {
		t.Errorf("merged list still shows the cached airing schedule: %+v", next)
	}

	// Menus already on screen redraw from this channel.
	select {
	case <-updates:
	case <-time.After(time.Second):
		t.Error("expected the refreshed list to be published to open menus")
	}
}

// A tracker that never answers must not leave the list marked "refreshing",
// because playback waits on that before deciding what is available.
func TestCombinedRefreshGivesUpRatherThanHanging(t *testing.T) {
	previous := combinedRefreshDeadlineForTest
	combinedRefreshDeadlineForTest = 150 * time.Millisecond
	t.Cleanup(func() { combinedRefreshDeadlineForTest = previous })

	cached := AnimeList{Watching: []Entry{{Media: Media{ID: 1}, Progress: 3, Status: "CURRENT"}}}
	user := &User{ListSync: NewAnimeListSync(cached)}
	// This one never reports done.
	aniListUser := &User{ListSync: NewAnimeListSync(cached)}
	myAnimeListUser := &User{ListSync: NewAnimeListSync(cached)}

	go refreshCombinedRemoteAnimeList(&Config{StoragePath: t.TempDir()}, user, aniListUser, myAnimeListUser)

	select {
	case <-user.ListSync.RefreshDone():
	case <-time.After(3 * time.Second):
		t.Fatal("a silent tracker left the list refreshing forever")
	}

	// The cached merge stands, which is the right answer when nothing arrived.
	if got := user.ListSync.Current().Watching[0].Progress; got != 3 {
		t.Errorf("cached list should have been kept, got progress %d", got)
	}
}

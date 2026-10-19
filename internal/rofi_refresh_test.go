package internal

import (
	"testing"
	"time"
)

// A rofi menu cannot be updated while it is open, so the refreshed list has to
// be picked up before rofi starts rather than by restarting it.
func TestAwaitRofiListRefreshTakesListPublishedBeforeOpening(t *testing.T) {
	sync := NewAnimeListSync(AnimeList{})
	fresh := AnimeList{Watching: []Entry{{Progress: 3}}}
	go func() {
		sync.Replace(fresh, true)
		sync.MarkRefreshDone()
	}()

	list, ok := awaitRofiListRefresh(sync.Updates(), sync.RefreshDone())
	if !ok {
		t.Fatal("expected the refreshed list")
	}
	if !animeListEqual(list, fresh) {
		t.Fatalf("got %+v, want %+v", list, fresh)
	}
}

func TestAwaitRofiListRefreshOpensWithCacheWhenRefreshIsSlow(t *testing.T) {
	old := rofiRefreshWait
	rofiRefreshWait = 20 * time.Millisecond
	defer func() { rofiRefreshWait = old }()

	sync := NewAnimeListSync(AnimeList{})
	start := time.Now()
	if _, ok := awaitRofiListRefresh(sync.Updates(), sync.RefreshDone()); ok {
		t.Fatal("no list was published")
	}
	if waited := time.Since(start); waited > time.Second {
		t.Fatalf("waited %s for a refresh that never finished", waited)
	}
}

func TestAwaitRofiListRefreshUnchangedListKeepsOptions(t *testing.T) {
	sync := NewAnimeListSync(AnimeList{})
	sync.Replace(AnimeList{}, true) // same list: nothing published
	sync.MarkRefreshDone()
	if _, ok := awaitRofiListRefresh(sync.Updates(), sync.RefreshDone()); ok {
		t.Fatal("an unchanged list should not rebuild the menu")
	}
}

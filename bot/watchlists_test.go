package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func listShow(id, eps, aired int) watchShow {
	return watchShow{ID: id, Title: "Show", Episodes: eps, Aired: aired, Cover: "https://s4.anilist.co/c.jpg"}
}

func TestMergeWatchList(t *testing.T) {
	anilist := []watchListEntry{
		{show: listShow(1, 12, 0), progress: 4, notes: "fun\n[otakase:5@754]", updated: 30},
		{show: listShow(2, 0, 6), progress: 6, updated: 50},                            // airing, caught up
		{show: listShow(3, 24, 0), progress: 2, notes: "[otakase:2@600]", updated: 10}, // saved spot is an episode already counted
		{show: watchShow{ID: 4, Episodes: 2, Adult: true}, progress: 0, updated: 90},
		{show: listShow(5, 12, 0), status: "NOT_YET_RELEASED", updated: 80},
	}
	mal := []watchListEntry{
		{show: listShow(1, 12, 0), progress: 3, notes: "[otakase:5@100]", updated: 40},
		{show: listShow(3, 24, 0), progress: 7, updated: 20},
		{show: listShow(6, 12, 0), progress: 1, notes: "[otakase:2@5]", updated: 5}, // too early to count
	}
	got := mergeWatchList([][]watchListEntry{anilist, mal}, false)
	want := []struct{ id, next, resume int }{{1, 5, 754}, {3, 8, 0}, {6, 2, 0}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for k, w := range want {
		if got[k].ID != w.id || got[k].Next != w.next || got[k].Resume != w.resume {
			t.Errorf("item %d = %d ep %d @%d, want %+v", k, got[k].ID, got[k].Next, got[k].Resume, w)
		}
	}
	if adult := mergeWatchList([][]watchListEntry{anilist}, true); len(adult) != 3 || adult[0].ID != 4 {
		t.Errorf("adult room: %+v", adult)
	}
}

func TestWatchListsAPI(t *testing.T) {
	b, _ := watchBot(t, "https://cdn.example/ep.mp4")
	b.store.update(func(d *storeData) {
		d.AniList = map[string]anilistLink{
			"a": {ID: 11, Verified: true, Token: "ta"},
			"b": {ID: 22, Name: "b"},
		}
	})
	var mu sync.Mutex
	var asked []string
	old := anilistWatchList
	anilistWatchList = func(token string, id int) ([]watchListEntry, error) {
		mu.Lock()
		asked = append(asked, token)
		mu.Unlock()
		return []watchListEntry{{show: listShow(id, 12, 0), progress: 2, notes: "[otakase:3@99]"}}, nil
	}
	t.Cleanup(func() {
		anilistWatchList = old
		watchListCache.Lock()
		watchListCache.m = map[string]watchListCacheEntry{}
		watchListCache.Unlock()
	})
	get := func(user string) (int, string) {
		r := httptest.NewRequest("GET", "/x?room=r", nil)
		r.Header.Set("Authorization", "Bearer "+b.acts.add(user))
		w := httptest.NewRecorder()
		b.watchListsAPI(w, r)
		return w.Code, w.Body.String()
	}

	code, body := get("a")
	if code != http.StatusOK || !strings.Contains(body, `"anilist":true`) || !strings.Contains(body, `"next":3,"resume":99`) ||
		strings.Contains(body, "planning") || strings.Count(body, "api/watch/cover?u=https") != 1 {
		t.Fatalf("a: %d %s", code, body)
	}
	// From the cache the second time, covers the same.
	if _, again := get("a"); again != body {
		t.Errorf("second answer differs:\n%s\n%s", body, again)
	}
	// An unproven link only reads the public list.
	if _, body := get("b"); !strings.Contains(body, `"id":22`) {
		t.Errorf("b: %s", body)
	}
	mu.Lock()
	if strings.Join(asked, ",") != "ta," {
		t.Errorf("tokens used: %q", asked)
	}
	mu.Unlock()
	if _, body := get("c"); !strings.Contains(body, `"anilist":false,"mal":false,"continue":[]`) {
		t.Errorf("unlinked: %s", body)
	}
}

func TestWatchRoomResumesFromSavedPosition(t *testing.T) {
	b, now := watchBot(t, "https://cdn.example/ep.mp4")
	b.store.update(func(d *storeData) { d.WatchTickets = map[string]int{"a": 1} })
	watchPost(b, (*bot).watchOpen, "a", watchIn{Room: "r", Channel: "vc"}, nil)
	watchPost(b, (*bot).watchPick, "a", watchIn{Room: "r", Show: 7, Ep: 5, Pos: 754}, nil)
	waitLoaded(t, b)
	r := b.watch.only()
	b.watch.mu.Lock()
	pos := r.position(*now)
	b.watch.mu.Unlock()
	if pos != 754 {
		t.Fatalf("started at %v", pos)
	}
	watchPost(b, (*bot).watchBeat, "a", watchIn{Room: "r", Pos: 754, Playing: true, Dur: 1400}, nil)
	*now = now.Add(100 * time.Second)
	b.watch.mu.Lock()
	r.members["a"].watched = 100
	p, ok := r.resumeOf("a", *now)
	b.watch.mu.Unlock()
	if !ok || p.secs != 854 {
		t.Errorf("resume after a resumed start: %+v %v", p, ok)
	}

	// A bogus position plays from the start; next always does.
	watchPost(b, (*bot).watchPick, "a", watchIn{Room: "r", Show: 7, Ep: 6, Pos: -3}, nil)
	waitLoaded(t, b)
	b.watch.mu.Lock()
	start := r.startAt
	b.watch.mu.Unlock()
	if start != 0 {
		t.Errorf("negative start kept: %v", start)
	}
}

func TestDropOffWatching(t *testing.T) {
	a, b := listShow(1, 12, 0), listShow(2, 12, 0)
	a.MalID, b.MalID = 101, 102
	entries := []watchListEntry{{show: a}, {show: b}}
	kept := dropOffWatching(entries, func(s watchShow) bool { return map[int]bool{102: true}[s.MalID] })
	if len(kept) != 1 || kept[0].show.ID != 1 || len(entries) != 2 || entries[1].show.ID != 2 {
		t.Errorf("kept %+v from %+v", kept, entries)
	}
}

// A show watching on one tracker but moved on on the other leaves Continue,
// even with a saved position.
func TestWatchListsLeaveShowsMovedOn(t *testing.T) {
	b, _ := watchBot(t, "https://cdn.example/ep.mp4")
	b.store.update(func(d *storeData) {
		d.AniList = map[string]anilistLink{"a": {ID: 11, Verified: true, Token: "ta"}}
		d.MAL = map[string]malLink{"a": {AccessToken: "tm", Expires: appNow().Add(48 * time.Hour)}}
	})
	b.cfg.MALClientID, b.cfg.MALSecret = "id", "secret"
	shows := []watchShow{listShow(1, 12, 0), listShow(2, 12, 0), listShow(3, 12, 0)}
	for k := range shows {
		shows[k].MalID = 100 + shows[k].ID
	}
	olds := []any{anilistWatchList, malWatchList, anilistOffWatching, malOffWatching}
	// AniList watches 1 and 2; MAL watches 1 and 3, and finished 2 there.
	anilistWatchList = func(string, int) ([]watchListEntry, error) {
		return []watchListEntry{{show: shows[0], progress: 1}, {show: shows[1], progress: 1, notes: "[otakase:2@300]"}}, nil
	}
	malWatchList = func(string) ([]watchListEntry, error) {
		return []watchListEntry{{show: shows[0], progress: 1}, {show: shows[2], progress: 1}}, nil
	}
	// On AniList 3 is paused.
	anilistOffWatching = func(_ string, _ int, ids []int) (map[int]bool, error) { return map[int]bool{3: true}, nil }
	malOffWatching = func(string) (map[int]bool, error) { return map[int]bool{102: true}, nil }
	t.Cleanup(func() {
		anilistWatchList = olds[0].(func(string, int) ([]watchListEntry, error))
		malWatchList = olds[1].(func(string) ([]watchListEntry, error))
		anilistOffWatching = olds[2].(func(string, int, []int) (map[int]bool, error))
		malOffWatching = olds[3].(func(string) (map[int]bool, error))
		watchListCache.Lock()
		watchListCache.m = map[string]watchListCacheEntry{}
		watchListCache.Unlock()
	})
	lists := b.userWatchLists("a", false)
	if len(lists.Continue) != 1 || lists.Continue[0].ID != 1 {
		t.Errorf("continue: %+v", lists.Continue)
	}
}

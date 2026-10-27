package main

import (
	"sort"
	"sync"
	"testing"
	"time"
)

func TestPlanTrackerSync(t *testing.T) {
	anilist := map[int]trackerEntry{
		10: {aniID: 1, status: "CURRENT", progress: 0, updated: 200},  // moved to Watching on AniList, MAL still Planning
		20: {aniID: 2, status: "CURRENT", progress: 3, updated: 100},  // dropped on MAL later
		30: {aniID: 3, status: "CURRENT", progress: 5, updated: 100},  // same on both
		40: {aniID: 4, status: "PLANNING", progress: 0, updated: 100}, // only on AniList: left alone
		50: {aniID: 5, status: "COMPLETED", progress: 12, updated: 100},
	}
	mal := map[int]trackerEntry{
		10: {status: "PLANNING", updated: 50},
		20: {status: "DROPPED", progress: 4, updated: 300},
		30: {status: "CURRENT", progress: 5, updated: 900},
		50: {status: "PAUSED", progress: 6, updated: 100},  // a tie writes nothing
		60: {status: "CURRENT", progress: 1, updated: 100}, // only on MAL: left alone
	}
	toAniList, toMAL := planTrackerSync(anilist, mal)
	if len(toAniList) != 1 || toAniList[0] != (trackerWrite{aniID: 2, malID: 20, status: "DROPPED", progress: 4}) {
		t.Errorf("to AniList: %+v", toAniList)
	}
	if len(toMAL) != 1 || toMAL[0] != (trackerWrite{aniID: 1, malID: 10, status: "CURRENT"}) {
		t.Errorf("to MAL: %+v", toMAL)
	}
}

func TestSyncTrackers(t *testing.T) {
	b, _ := watchBot(t, "https://cdn.example/ep.mp4")
	b.store.update(func(d *storeData) {
		d.AniList = map[string]anilistLink{"a": {ID: 11, Verified: true, Token: "ta"}, "b": {ID: 22, Name: "b"}}
		d.MAL = map[string]malLink{
			"a": {AccessToken: "tm", Expires: appNow().Add(48 * time.Hour)},
			"b": {AccessToken: "tm", Expires: appNow().Add(48 * time.Hour)},
		}
	})
	b.cfg.MALClientID, b.cfg.MALSecret = "id", "secret"
	var mu sync.Mutex
	var wrote []string
	var reads int
	olds := []any{anilistFullList, malFullList, anilistSetEntry, malSetEntry, trackerSyncPause}
	anilistFullList = func(token string, id int) (map[int]trackerEntry, error) {
		mu.Lock()
		reads++
		mu.Unlock()
		return map[int]trackerEntry{10: {aniID: 1, status: "CURRENT", updated: 200}, 20: {aniID: 2, status: "CURRENT", updated: 100}}, nil
	}
	malFullList = func(string) (map[int]trackerEntry, error) {
		return map[int]trackerEntry{10: {status: "PLANNING", updated: 50}, 20: {status: "COMPLETED", progress: 12, updated: 300}}, nil
	}
	anilistSetEntry = func(token string, w trackerWrite) error {
		mu.Lock()
		wrote = append(wrote, "anilist:"+token+":"+w.status)
		mu.Unlock()
		return nil
	}
	malSetEntry = func(token string, w trackerWrite) error {
		mu.Lock()
		wrote = append(wrote, "mal:"+token+":"+w.status)
		mu.Unlock()
		return nil
	}
	trackerSyncPause = func() {}
	trackerSyncs.Lock()
	trackerSyncs.last, trackerSyncs.running = map[string]time.Time{}, map[string]bool{}
	trackerSyncs.Unlock()
	t.Cleanup(func() {
		anilistFullList = olds[0].(func(string, int) (map[int]trackerEntry, error))
		malFullList = olds[1].(func(string) (map[int]trackerEntry, error))
		anilistSetEntry = olds[2].(func(string, trackerWrite) error)
		malSetEntry = olds[3].(func(string, trackerWrite) error)
		trackerSyncPause = olds[4].(func())
		trackerSyncs.Lock()
		trackerSyncs.last, trackerSyncs.running = map[string]time.Time{}, map[string]bool{}
		trackerSyncs.Unlock()
	})
	wait := func() {
		for k := 0; k < 200; k++ {
			trackerSyncs.Lock()
			busy := len(trackerSyncs.running) > 0
			trackerSyncs.Unlock()
			if !busy {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("sync never finished")
	}

	b.syncTrackersSoon("a")
	wait()
	mu.Lock()
	sort.Strings(wrote)
	if len(wrote) != 2 || wrote[0] != "anilist:ta:COMPLETED" || wrote[1] != "mal:tm:CURRENT" {
		t.Errorf("wrote %v", wrote)
	}
	mu.Unlock()

	// Not again right away, and never without an AniList link with access.
	b.syncTrackersSoon("a")
	b.syncTrackersSoon("b")
	wait()
	mu.Lock()
	if reads != 1 {
		t.Errorf("lists read %d times", reads)
	}
	mu.Unlock()

	// Staff can turn it off.
	trackerSyncs.Lock()
	trackerSyncs.last = map[string]time.Time{}
	trackerSyncs.Unlock()
	b.store.update(func(d *storeData) {
		if d.Settings == nil {
			d.Settings = map[string]string{}
		}
		d.Settings["tracker_sync"] = "false"
	})
	b.syncTrackersSoon("a")
	wait()
	mu.Lock()
	if reads != 1 {
		t.Errorf("synced with tracker_sync off")
	}
	mu.Unlock()
}

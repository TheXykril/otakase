package internal

import (
	"testing"
	"time"
)

// A show kept private on AniList must not be copied to, or updated on,
// MyAnimeList, which cannot hide a single entry.
func TestDualSyncPlanKeepsAniListPrivateEntriesOffMyAnimeList(t *testing.T) {
	aniList := AnimeList{
		Watching: []Entry{
			{Media: Media{ID: 1, MalID: 101}, Status: "CURRENT", Progress: 5, Private: true,
				UpdatedAt: time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)},
			{Media: Media{ID: 2, MalID: 202}, Status: "CURRENT", Progress: 3, Private: true},
			{Media: Media{ID: 3, MalID: 303}, Status: "CURRENT", Progress: 1},
		},
	}
	myAnimeList := AnimeList{
		Watching: []Entry{{Media: Media{ID: 1, MalID: 101}, Status: "CURRENT", Progress: 2,
			UpdatedAt: time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)}},
	}

	plan := buildDualRemoteSyncPlan(aniList, myAnimeList)

	if len(plan.MyAnimeListUpdates) != 1 || plan.MyAnimeListUpdates[0].Media.ID != 3 {
		t.Fatalf("MyAnimeList updates = %+v, want only the public show 3", plan.MyAnimeListUpdates)
	}
	for _, entry := range getEntriesByCategory(plan.Merged, "ALL") {
		if want := entry.Media.ID != 3; entry.Private != want {
			t.Errorf("merged show %d private = %v, want %v", entry.Media.ID, entry.Private, want)
		}
	}
	if !isAniListPrivateMyAnimeListID(101) || !isAniListPrivateMyAnimeListID(202) {
		t.Error("private shows are not marked for the MyAnimeList writers")
	}
	if isAniListPrivateMyAnimeListID(303) {
		t.Error("a public show is marked private")
	}

	// Made public again on AniList: it syncs again, and a merge that takes
	// the newer MyAnimeList side does not keep a stale flag.
	aniList.Watching[0].Private = false
	myAnimeList.Watching[0].Private = true
	myAnimeList.Watching[0].UpdatedAt = time.Date(2026, 9, 30, 11, 0, 0, 0, time.UTC)
	plan = buildDualRemoteSyncPlan(aniList, myAnimeList)
	if isAniListPrivateMyAnimeListID(101) {
		t.Error("show 101 still marked private after AniList made it public")
	}
	for _, entry := range getEntriesByCategory(plan.Merged, "ALL") {
		if entry.Media.ID == 1 && entry.Private {
			t.Error("merged show 1 kept a stale private flag")
		}
	}
}

func TestParseAnimeListReadsPrivateFlag(t *testing.T) {
	data := map[string]interface{}{"data": map[string]interface{}{"MediaListCollection": map[string]interface{}{
		"lists": []interface{}{map[string]interface{}{
			"name": "Watching",
			"entries": []interface{}{
				map[string]interface{}{"id": float64(1), "status": "CURRENT", "private": true,
					"media": map[string]interface{}{"id": float64(10), "idMal": float64(100)}},
				map[string]interface{}{"id": float64(2), "status": "CURRENT",
					"media": map[string]interface{}{"id": float64(20), "idMal": float64(200)}},
			},
		}},
	}}}
	list := ParseAnimeList(data)
	if len(list.Watching) != 2 || !list.Watching[0].Private || list.Watching[1].Private {
		t.Fatalf("parsed = %+v, want first private, second not", list.Watching)
	}
}

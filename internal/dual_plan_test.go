package internal

import (
	"encoding/json"
	"os"
	"testing"
)

type cachedList struct {
	AnimeList AnimeList `json:"anime_list"`
}

func loadCachedList(t *testing.T, path string) AnimeList {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no cache at %s", path)
	}
	var c cachedList
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return c.AnimeList
}

// Read-only: it computes the plan and makes no request to either tracker.
//
// How many writes a dual sync would issue, computed without making any. The
// AniList loop sleeps 350ms between writes; the MyAnimeList loop has no delay,
// so this number is how many back-to-back PUTs MAL receives at launch.
func TestDualSyncPlanSizeLive(t *testing.T) {
	if os.Getenv("CURD_LIVE_DUAL_TEST") != "1" {
		t.Skip("set CURD_LIVE_DUAL_TEST=1")
	}
	storage := os.Getenv("CURD_TEST_STORAGE")
	if storage == "" {
		t.Skip("set CURD_TEST_STORAGE")
	}

	aniList := loadCachedList(t, storage+"/anilist_list_cache.json")
	malList := loadCachedList(t, storage+"/myanimelist_list_cache.json")

	plan := buildDualRemoteSyncPlan(aniList, malList)
	t.Logf("AniList writes:      %d (throttled 350ms apart)", len(plan.AniListUpdates))
	t.Logf("MyAnimeList writes:  %d (no delay between them)", len(plan.MyAnimeListUpdates))
	t.Logf("merged list size:    %d watching, %d completed",
		len(plan.Merged.Watching), len(plan.Merged.Completed))
}

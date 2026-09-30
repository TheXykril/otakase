package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNoteRecentShowKeepsNewestFirstWithoutRepeats(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	for i, id := range []int{1, 2, 3, 2} {
		noteRecentShow(dir, id, base.Add(time.Duration(i)*time.Minute))
	}
	shows := loadRecentShows(dir)
	got := []int{}
	for _, show := range shows {
		got = append(got, show.AnilistID)
	}
	if len(got) != 3 || got[0] != 2 || got[1] != 3 || got[2] != 1 {
		t.Fatalf("recent order = %v, want [2 3 1]", got)
	}
	if !shows[0].PlayedAt.Equal(base.Add(3 * time.Minute)) {
		t.Fatalf("played at = %v", shows[0].PlayedAt)
	}

	for id := 10; id < 10+recentShowsKept+5; id++ {
		noteRecentShow(dir, id, base)
	}
	if n := len(loadRecentShows(dir)); n != recentShowsKept {
		t.Fatalf("kept %d shows, want %d", n, recentShowsKept)
	}
}

func TestContinueWatchingLabel(t *testing.T) {
	cases := []struct {
		episode, seconds int
		want             string
	}{
		{13, 754, "▶ Frieren · ep 13 at 12:34"},
		{13, 20, "▶ Frieren · ep 13"},
		{2, 3725, "▶ Frieren · ep 2 at 1:02:05"},
		{0, 0, "▶ Frieren"},
	}
	for _, tc := range cases {
		if got := continueWatchingLabel("Frieren", tc.episode, tc.seconds); got != tc.want {
			t.Fatalf("label(%d, %d) = %q, want %q", tc.episode, tc.seconds, got, tc.want)
		}
	}
}

func TestResumeRowAnilistID(t *testing.T) {
	if id, ok := resumeRowAnilistID("RESUME:154587"); !ok || id != 154587 {
		t.Fatalf("got %d, %v", id, ok)
	}
	for _, key := range []string{"CURRENT", "RESUME:", "RESUME:x", "RESUME:-4", "154587"} {
		if _, ok := resumeRowAnilistID(key); ok {
			t.Fatalf("%q read as a resume row", key)
		}
	}
}

// Rows come from the recent list, in its order, named from the watch history;
// a show with no history, or no longer on the tracker's list, is left out.
func TestContinueWatchingRows(t *testing.T) {
	dir := t.TempDir()
	history := strings.Join([]string{
		"101,provider-a,13,754,24,Frieren",
		"202,provider-b,4,0,24,Dandadan",
		"303,provider-c,1,120,24,Removed Show",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "curd_history.txt"), []byte(history), 0o644); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	for i, id := range []int{303, 999, 101, 202} {
		noteRecentShow(dir, id, base.Add(time.Duration(i)*time.Minute))
	}

	config := &Config{StoragePath: dir, ContinueWatchingRows: 5, TrackingRemote: TrackingRemoteAniList}
	list := &AnimeList{Watching: []Entry{{Media: Media{ID: 101}}, {Media: Media{ID: 202}}}}
	rows := continueWatchingRows(config, list)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Key != "RESUME:202" || !strings.Contains(rows[0].Label, "ep 4") {
		t.Fatalf("first row = %+v", rows[0])
	}
	if rows[1].Key != "RESUME:101" || !strings.HasSuffix(rows[1].Label, "ep 13 at 12:34") {
		t.Fatalf("second row = %+v", rows[1])
	}

	config.ContinueWatchingRows = 1
	if rows := continueWatchingRows(config, list); len(rows) != 1 {
		t.Fatalf("expected one row, got %+v", rows)
	}
	config.ContinueWatchingRows = 0
	if rows := continueWatchingRows(config, list); len(rows) != 0 {
		t.Fatalf("rows off, got %+v", rows)
	}
}

func TestSortHomeMenuOptionsPutsResumeRowsFirst(t *testing.T) {
	previous := GetGlobalConfig()
	SetGlobalConfig(&Config{MenuOrder: "CURRENT,ALL"})
	t.Cleanup(func() { SetGlobalConfig(previous) })

	sorted := sortHomeMenuOptions([]SelectionOption{
		{Key: "ALL"}, {Key: "CURRENT"}, {Key: "RESUME:1"}, {Key: "RESUME:2"},
	})
	keys := []string{}
	for _, opt := range sorted {
		keys = append(keys, opt.Key)
	}
	if strings.Join(keys, ",") != "RESUME:1,RESUME:2,CURRENT,ALL" {
		t.Fatalf("order = %v", keys)
	}
}

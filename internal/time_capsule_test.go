package internal

import (
	"testing"
)

func TestSnapshotStats(t *testing.T) {
	s := snapshotStats(watchStats{AniList: &aniListStatsTotals{Count: 10, EpisodesWatched: 120, MinutesWatched: 2880,
		Genres: []statCount{{Name: "Drama", Count: 5}}, Statuses: []statCount{{Name: "COMPLETED", Count: 7}}}})
	if s == nil || s.Episodes != 120 || s.Completed != 7 || s.TopGenre != "Drama" {
		t.Fatalf("AniList snapshot %+v", s)
	}
	s = snapshotStats(watchStats{MyAnimeList: &myAnimeListStatsTotals{Items: 4, Episodes: 48, DaysWatched: 1, Completed: 3}})
	if s == nil || s.Minutes != 1440 || s.Completed != 3 {
		t.Fatalf("MyAnimeList snapshot %+v", s)
	}
	if snapshotStats(watchStats{}) != nil {
		t.Error("snapshot without stats")
	}
}

func TestTimeCapsuleFileRoundTrip(t *testing.T) {
	config := &Config{StoragePath: t.TempDir()}
	f := timeCapsuleFile{Offered: 2027, Letters: []capsuleLetter{{Text: "hello", Opens: 2028}}}
	if err := saveTimeCapsule(config, f); err != nil {
		t.Fatal(err)
	}
	got := loadTimeCapsule(config)
	if got.Offered != 2027 || len(got.Letters) != 1 || got.Letters[0].Text != "hello" {
		t.Fatalf("read back %+v", got)
	}
}

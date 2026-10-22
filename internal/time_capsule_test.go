package internal

import (
	"strings"
	"testing"
	"time"
)

func TestOtakaseBirthdayDates(t *testing.T) {
	if !isOtakaseBirthday(time.Date(2027, 4, 12, 23, 0, 0, 0, time.Local)) {
		t.Error("April 12 is the birthday")
	}
	if isOtakaseBirthday(time.Date(2027, 4, 13, 0, 0, 0, 0, time.Local)) {
		t.Error("April 13 is not")
	}
	cases := map[time.Time]int{
		time.Date(2026, 10, 22, 0, 0, 0, 0, time.Local): 2027,
		time.Date(2027, 4, 11, 0, 0, 0, 0, time.Local):  2027,
		time.Date(2027, 4, 12, 0, 0, 0, 0, time.Local):  2028,
		time.Date(2027, 12, 31, 0, 0, 0, 0, time.Local): 2028,
	}
	for at, want := range cases {
		if got := nextOtakaseBirthday(at); got != want {
			t.Errorf("%v: next birthday %d, want %d", at, got, want)
		}
	}
}

func TestLettersToOpen(t *testing.T) {
	f := timeCapsuleFile{Letters: []capsuleLetter{
		{Text: "missed one", Opens: 2027},
		{Text: "this year", Opens: 2028},
		{Text: "already read", Opens: 2028, Opened: true},
		{Text: "next year", Opens: 2029},
	}}
	due := lettersToOpen(f, 2028)
	if len(due) != 2 || due[0] != 0 || due[1] != 1 {
		t.Fatalf("due %v", due)
	}
	if i, ok := pendingLetter(f, 2029); !ok || i != 3 {
		t.Errorf("pending %d %v", i, ok)
	}
}

func TestLetterSections(t *testing.T) {
	then := &capsuleStats{Anime: 80, Episodes: 812, Minutes: 812 * 24, Completed: 50, MeanScore: 7.8, TopGenre: "Action"}
	now := &capsuleStats{Anime: 120, Episodes: 1240, Minutes: 1240 * 24, Completed: 81, MeanScore: 7.6, TopGenre: "Action"}
	l := capsuleLetter{Text: "Did you finish One Piece?", Written: time.Date(2027, 4, 12, 20, 0, 0, 0, time.Local), Opens: 2028, Stats: then}
	page := renderStatsPlain(letterSections(l, now))
	for _, want := range []string{"“Did you finish One Piece?”", "Then and now", "812 → 1,240 (+428)", "Action, still", "7.8 → 7.6", "Written April 12, 2027."} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q:\n%s", want, page)
		}
	}
	// No tracker then: just the message.
	page = renderStatsPlain(letterSections(capsuleLetter{Text: "hi", Written: l.Written}, now))
	if strings.Contains(page, "Then and now") {
		t.Errorf("stats shown without a snapshot:\n%s", page)
	}
}

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

func TestBirthdayCapsuleQuietOtherDays(t *testing.T) {
	old := timeCapsuleNow
	t.Cleanup(func() { timeCapsuleNow = old })
	timeCapsuleNow = func() time.Time { return time.Date(2027, 4, 11, 12, 0, 0, 0, time.Local) }
	if OfferBirthdayCapsule(&Config{StoragePath: t.TempDir()}) {
		t.Error("birthday shown on April 11")
	}
}

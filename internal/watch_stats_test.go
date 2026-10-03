package internal

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Every time here is fixed and in a named zone, so the tests mean the same
// thing on any machine and on any day they run.
var statsTestZone = time.FixedZone("UTC+3", 3*60*60)

// statsAt is a moment on a day in September 2026. The 30th is a Wednesday.
func statsAt(day, hour int) time.Time {
	return time.Date(2026, time.September, day, hour, 0, 0, 0, statsTestZone)
}

func watched(at time.Time, mediaID, episodes int) statsActivity {
	return statsActivity{At: at, MediaID: mediaID, Title: "Show " + string(rune('A'+mediaID)), Episodes: episodes, Duration: 24}
}

func TestStatsActivityEpisodesReadsRangesAndCompletions(t *testing.T) {
	cases := []struct {
		status, progress string
		want             int
	}{
		{"watched episode", "5", 1},
		{"watched episode", "3 - 5", 3},
		{"rewatched episode", "1 - 12", 12},
		{"watched episode", "", 1},
		// A malformed range still stands for something watched.
		{"watched episode", "7 - 3", 1},
		// Finishing a show reports no progress but was the final episode.
		{"completed", "", 1},
		{"plans to watch", "", 0},
		{"dropped", "4", 0},
		{"paused watching", "", 0},
	}
	for _, c := range cases {
		if got := statsActivityEpisodes(c.status, c.progress); got != c.want {
			t.Errorf("%q %q: got %d episodes, want %d", c.status, c.progress, got, c.want)
		}
	}
}

// "This week" starts on Monday and "this month" on the first, in the viewer's
// own time -- not a rolling seven or thirty days.
func TestStatsWeekAndMonthBuckets(t *testing.T) {
	now := statsAt(30, 12) // Wednesday
	if got, want := statsWeekStart(now), statsAt(28, 0); !got.Equal(want) {
		t.Errorf("week starts %v, want Monday %v", got, want)
	}
	// Sunday belongs to the week that began six days earlier.
	if got, want := statsWeekStart(statsAt(27, 23)), statsAt(21, 0); !got.Equal(want) {
		t.Errorf("Sunday's week starts %v, want %v", got, want)
	}
	if got, want := statsMonthStart(now), statsAt(1, 0); !got.Equal(want) {
		t.Errorf("month starts %v, want %v", got, want)
	}

	activities := []statsActivity{
		watched(statsAt(30, 9), 1, 2),                                     // this week
		watched(statsAt(28, 0), 2, 1),                                     // Monday midnight: this week
		watched(statsAt(27, 23), 2, 3),                                    // Sunday: this month only
		watched(time.Date(2026, 8, 31, 22, 0, 0, 0, statsTestZone), 3, 5), // last month
		{At: statsAt(29, 1), MediaID: 4, Episodes: 2},                     // unknown length
		{At: statsAt(29, 2), MediaID: 4, Episodes: 0, Duration: 24},       // a status change
	}
	episodes, minutes := statsPeriodTotals(activities, statsWeekStart(now))
	if episodes != 5 || minutes != 5*24 {
		t.Errorf("week: %d episodes / %d minutes, want 5 / %d", episodes, minutes, 5*24)
	}
	episodes, _ = statsPeriodTotals(activities, statsMonthStart(now))
	if episodes != 8 {
		t.Errorf("month: %d episodes, want 8", episodes)
	}
}

func TestStatsStreaks(t *testing.T) {
	now := statsAt(30, 20)
	activities := []statsActivity{
		// A five-day run earlier in the month.
		watched(statsAt(10, 12), 1, 1),
		watched(statsAt(11, 12), 1, 1),
		watched(statsAt(12, 12), 1, 2),
		watched(statsAt(13, 12), 1, 1),
		watched(statsAt(14, 12), 1, 1),
		// Two on one day count once.
		watched(statsAt(14, 18), 2, 1),
		// A status change is not watching.
		{At: statsAt(15, 12), MediaID: 3},
		// The current run: three days up to today.
		watched(statsAt(28, 12), 2, 1),
		watched(statsAt(29, 12), 2, 1),
		watched(statsAt(30, 12), 2, 1),
	}
	current, longest := statsStreaks(activities, now)
	if current != 3 || longest != 5 {
		t.Errorf("got current %d longest %d, want 3 and 5", current, longest)
	}

	// Nothing yet today does not break a streak that ran through yesterday.
	current, _ = statsStreaks(activities[:len(activities)-1], now)
	if current != 2 {
		t.Errorf("a streak ending yesterday reads %d, want 2", current)
	}

	// A gap of a whole day does.
	current, _ = statsStreaks(activities[:len(activities)-2], now)
	if current != 0 {
		t.Errorf("a streak ending two days ago reads %d, want 0", current)
	}

	if current, longest := statsStreaks(nil, now); current != 0 || longest != 0 {
		t.Errorf("no history gave %d / %d", current, longest)
	}
}

// Days are the viewer's days: an episode at 01:00 local is that day, even
// though it is still the day before in UTC.
func TestStatsStreaksUseTheViewersDay(t *testing.T) {
	now := statsAt(30, 12)
	lateNight := time.Date(2026, 9, 29, 22, 30, 0, 0, time.UTC) // 01:30 on the 30th locally
	current, _ := statsStreaks([]statsActivity{watched(lateNight, 1, 1)}, now)
	if current != 1 {
		t.Errorf("a 01:30 episode should count for today, streak is %d", current)
	}
}

func TestStatsStreaksAcrossAClockChange(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("no zone data")
	}
	// Clocks went back on 25 October 2026, a 25-hour day.
	var activities []statsActivity
	for day := 23; day <= 27; day++ {
		activities = append(activities, watched(time.Date(2026, 10, day, 21, 0, 0, 0, berlin), 1, 1))
	}
	current, longest := statsStreaks(activities, time.Date(2026, 10, 27, 23, 0, 0, 0, berlin))
	if current != 5 || longest != 5 {
		t.Errorf("got %d / %d across the clock change, want 5 / 5", current, longest)
	}
}

func TestStatsMostBinged(t *testing.T) {
	activities := []statsActivity{
		{At: statsAt(20, 1), MediaID: 1, Title: "Frieren", Episodes: 4},
		{At: statsAt(21, 1), MediaID: 2, Title: "Dandadan", Episodes: 3},
		{At: statsAt(22, 1), MediaID: 1, Title: "Frieren", Episodes: 2},
		{At: statsAt(23, 1), MediaID: 3, Title: "Apothecary", Episodes: 6},
	}
	title, episodes := statsMostBinged(activities)
	if title != "Frieren" && title != "Apothecary" {
		t.Fatalf("got %q", title)
	}
	// A tie goes to the show watched most recently.
	if title != "Apothecary" || episodes != 6 {
		t.Errorf("got %q with %d, want Apothecary with 6", title, episodes)
	}

	if title, _ := statsMostBinged(nil); title != "" {
		t.Errorf("no history named %q", title)
	}
}

func TestFormatWatchMinutes(t *testing.T) {
	cases := map[int]string{
		0:              "0m",
		45:             "45m",
		60:             "1h",
		200:            "3h 20m",
		2*24*60 + 4*60: "2d 4h",
		2*24*60 + 4*61: "2d 4h",
		-5:             "0m",
	}
	for minutes, want := range cases {
		if got := formatWatchMinutes(minutes); got != want {
			t.Errorf("%d minutes: got %q, want %q", minutes, got, want)
		}
	}
}

func TestFormatStatsCount(t *testing.T) {
	cases := map[int]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567", -1500: "-1,500"}
	for value, want := range cases {
		if got := formatStatsCount(value); got != want {
			t.Errorf("%d: got %q, want %q", value, got, want)
		}
	}
}

func TestAniListPageShowsTotalsHistoryAndCaveat(t *testing.T) {
	now := statsAt(30, 20)
	stats := watchStats{
		Source: "AniList",
		AniList: &aniListStatsTotals{
			Count: 312, EpisodesWatched: 4210, MinutesWatched: 4210 * 24, MeanScore: 78.4,
			Genres:   []statCount{{"Action", 120}, {"Comedy", 90}, {"Drama", 70}, {"Romance", 10}},
			Formats:  []statCount{{"TV", 250}, {"MOVIE", 30}},
			Statuses: []statCount{{"COMPLETED", 280}, {"CURRENT", 12}},
		},
		Activities: []statsActivity{
			{At: statsAt(29, 12), MediaID: 1, Title: "Frieren", Episodes: 3, Duration: 24},
			{At: statsAt(30, 12), MediaID: 1, Title: "Frieren", Episodes: 2, Duration: 24},
		},
	}
	page := renderStatsPlain(buildStatsSections(stats, now))
	for _, want := range []string{
		"4,210", "78.4", "Action 120 · Comedy 90 · Drama 70", "Movie 30", "Completed 280",
		"This week", "5 episodes · ~2h", "Current streak", "2 days", "Frieren (5 episodes)",
		statsTimeCaveat,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page is missing %q:\n%s", want, page)
		}
	}
	// Only the top three genres fit.
	if strings.Contains(page, "Romance") {
		t.Errorf("a fourth genre was shown:\n%s", page)
	}
	if strings.Contains(page, statsMyAnimeListNote) {
		t.Errorf("an AniList page said there is no dated history:\n%s", page)
	}

	// Values line up down the page.
	column := -1
	for _, line := range strings.Split(page, "\n") {
		if !strings.HasPrefix(line, "  ") {
			continue
		}
		// The value starts after the label and the run of padding behind it.
		row := line[2:]
		gap := strings.Index(row, "  ")
		idx := gap + len(row[gap:]) - len(strings.TrimLeft(row[gap:], " "))
		if column == -1 {
			column = idx
		} else if idx != column {
			t.Errorf("values are not aligned:\n%s", page)
			break
		}
	}
}

func TestMyAnimeListPageSaysThereIsNoHistory(t *testing.T) {
	stats := watchStats{
		Source: "MyAnimeList",
		MyAnimeList: &myAnimeListStatsTotals{
			Items: 150, Watching: 5, Completed: 120, Episodes: 2000, DaysWatched: 30.5, MeanScore: 7.82,
		},
	}
	page := renderStatsPlain(buildStatsSections(stats, statsAt(30, 12)))
	for _, want := range []string{"2,000", "30d 12h", "7.82", "Completed 120", statsMyAnimeListNote, statsTimeCaveat} {
		if !strings.Contains(page, want) {
			t.Errorf("page is missing %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "Current streak") {
		t.Errorf("a MyAnimeList page offered a streak it cannot know:\n%s", page)
	}
}

func TestStatsPageForLocalTrackingNamesTheTrackers(t *testing.T) {
	title, sections := watchStatsPage(&Config{TrackingRemote: TrackingRemoteNone})
	if title != "Stats" || len(sections) != 1 || sections[0].Note != statsNeedsTracker {
		t.Errorf("local tracking gave %q %+v", title, sections)
	}
	if !strings.Contains(statsNeedsTracker, "AniList") || !strings.Contains(statsNeedsTracker, "MyAnimeList") {
		t.Errorf("the local-only message should name both trackers: %q", statsNeedsTracker)
	}
}

func TestStatsRateLimitReadsAsWaitNotBroken(t *testing.T) {
	err := errors.New("failed with status 429: Too Many Requests")
	if !isRateLimitError(err) {
		t.Fatal("a 429 was not recognised")
	}
	if got := statsErrorMessage("AniList", err); !strings.Contains(got, "Try again in a minute") {
		t.Errorf("rate limit message: %q", got)
	}
	if isRateLimitError(errors.New("failed with status 500")) {
		t.Error("a 500 was taken for a rate limit")
	}
}

// A switched tracker must not be answered from the other tracker's cache.
func TestStatsCacheIsPerTracker(t *testing.T) {
	statsCacheMu.Lock()
	saved := statsCache
	statsCache = statsCacheEntry{key: TrackingRemoteAniList, stats: watchStats{Source: "AniList"}, fetchedAt: time.Now()}
	statsCacheMu.Unlock()
	t.Cleanup(func() {
		statsCacheMu.Lock()
		statsCache = saved
		statsCacheMu.Unlock()
	})

	stats, err := fetchWatchStats(&Config{TrackingRemote: TrackingRemoteAniList})
	if err != nil || stats.Source != "AniList" {
		t.Fatalf("a fresh cache was not reused: %+v %v", stats, err)
	}
	// Local tracking never reaches the cache or the network.
	if _, err := fetchWatchStats(&Config{TrackingRemote: TrackingRemoteNone}); !errors.Is(err, errStatsNeedTracker) {
		t.Errorf("local tracking gave %v", err)
	}
}

func TestStatsKeyOpensTheStatsPage(t *testing.T) {
	_, actions := SplitMenuOrder("CURRENT,ALL,STATS")
	if len(actions) != 1 || actions[0].Key != "STATS" || actions[0].Hint != "ctrl+s" {
		t.Fatalf("STATS did not become a footer action: %+v", actions)
	}
	if !isMenuActionKey("STATS") {
		t.Fatal("the caller would not dispatch STATS")
	}

	m := &Model{allOptions: []SelectionOption{{Key: "1", Label: "Frieren"}}}
	attachCategoryTabs(m, &SelectionRefreshConfig{Actions: actions})
	m.filterOptions()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("ctrl+s did not end the menu")
	}
	if got, _ := updated.(*Model).highlighted(); got.Key != "STATS" {
		t.Errorf("ctrl+s selected %q, want STATS", got.Key)
	}

	// No two actions share a key, or one of them could never be reached.
	seen := map[string]string{}
	for key, action := range menuActions {
		if other, clash := seen[action.Hint]; clash {
			t.Errorf("%s and %s are both bound to %s", key, other, action.Hint)
		}
		seen[action.Hint] = key
	}

	if !strings.Contains(strings.Join(defaultMenuKeys(), ","), "STATS") {
		t.Error("new installs should get STATS in the menu")
	}
}

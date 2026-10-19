package internal

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The stats page keeps no file of its own. Everything on it is read from the
// tracker when the page opens: the tracker already records every episode, and
// a second copy here would drift from it the first time someone updated their
// list from the website.
//
// This file holds the parts that are pure arithmetic over what came back --
// buckets, streaks, formatting -- so they can be tested against fixed times
// without a network.

// statsHistoryDays is how far back the dated history reaches. AniList pages
// its activity feed, and every page is a request against a rate limit shared
// with the rest of the program; three months answers "this month" and gives a
// streak room to be long without paging through years of history.
const statsHistoryDays = 90

// statsFallbackEpisodeMinutes stands in for a show whose length AniList does
// not know. Leaving such a show out would make the hours disagree with the
// episode count beside them; a typical TV episode is the least surprising guess.
const statsFallbackEpisodeMinutes = 24

// statsActivity is one dated entry of watching: some episodes of one show.
type statsActivity struct {
	At       time.Time
	MediaID  int
	Title    string
	Episodes int
	// Minutes per episode, 0 when unknown.
	Duration int
}

// statCount is one row of a "top few" breakdown: a genre, format or status.
type statCount struct {
	Name  string
	Count int
}

// aniListStatsTotals is AniList's own all-time summary of the user's list.
type aniListStatsTotals struct {
	Count           int
	EpisodesWatched int
	MinutesWatched  int
	MeanScore       float64
	Genres          []statCount
	Formats         []statCount
	Statuses        []statCount
}

// myAnimeListStatsTotals is MyAnimeList's anime_statistics block.
type myAnimeListStatsTotals struct {
	Items       int
	Watching    int
	Completed   int
	OnHold      int
	Dropped     int
	PlanToWatch int
	Episodes    int
	DaysWatched float64
	Rewatched   int
	MeanScore   float64
}

// watchStats is everything the page shows, from whichever tracker answered.
type watchStats struct {
	Source      string
	AniList     *aniListStatsTotals
	MyAnimeList *myAnimeListStatsTotals
	Activities  []statsActivity
	// HistoryNote says why the dated part is partial, when it is: the feed
	// stopped answering part way, or ran past the page cap. The totals above it
	// are still right, so the page is still worth showing.
	HistoryNote string
}

// statsActivityEpisodes reads how many episodes one activity covers.
//
// AniList folds consecutive updates to a show into one activity, so progress
// is either "5" or a range like "3 - 5". Finishing a show is reported as a
// "completed" activity with no progress at all, though it always stands for
// at least the final episode; counting it as one keeps a finished show from
// vanishing out of the week it was finished in.
func statsActivityEpisodes(status, progress string) int {
	status = strings.ToLower(strings.TrimSpace(status))
	progress = strings.TrimSpace(progress)

	if strings.Contains(status, "watched episode") {
		if progress == "" {
			return 1
		}
		if from, to, found := strings.Cut(progress, "-"); found {
			start, errStart := strconv.Atoi(strings.TrimSpace(from))
			end, errEnd := strconv.Atoi(strings.TrimSpace(to))
			if errStart != nil || errEnd != nil || end < start {
				return 1
			}
			return end - start + 1
		}
		return 1
	}
	if strings.Contains(status, "completed") {
		return 1
	}
	// Planning, pausing and dropping change the list, not what was watched.
	return 0
}

// statsDay is the calendar day a time falls on, in its own location.
func statsDay(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, t.Location())
}

// statsWeekStart is the Monday that begins now's week. "This week" is the
// calendar week rather than the last seven days, because that is what a
// person means by it and a rolling window changes under them day to day.
func statsWeekStart(now time.Time) time.Time {
	today := statsDay(now)
	offset := (int(today.Weekday()) + 6) % 7
	return today.AddDate(0, 0, -offset)
}

// statsMonthStart is the first of now's month.
func statsMonthStart(now time.Time) time.Time {
	year, month, _ := now.Date()
	return time.Date(year, month, 1, 0, 0, 0, 0, now.Location())
}

// statsPeriodTotals adds up episodes and estimated minutes since from.
func statsPeriodTotals(activities []statsActivity, from time.Time) (episodes, minutes int) {
	for _, activity := range activities {
		if activity.At.Before(from) || activity.Episodes <= 0 {
			continue
		}
		episodes += activity.Episodes
		duration := activity.Duration
		if duration <= 0 {
			duration = statsFallbackEpisodeMinutes
		}
		minutes += activity.Episodes * duration
	}
	return episodes, minutes
}

// statsStreaks counts days in a row with at least one episode watched.
//
// The current streak runs back from today, or from yesterday when nothing has
// been watched yet today: the day is not over, and a streak that read zero
// every morning would be a streak nobody could keep. Days are counted in
// now's location, so a late-night episode lands on the day the viewer lived.
func statsStreaks(activities []statsActivity, now time.Time) (current, longest int) {
	location := now.Location()
	watched := map[time.Time]bool{}
	for _, activity := range activities {
		if activity.Episodes <= 0 {
			continue
		}
		watched[statsDay(activity.At.In(location))] = true
	}
	if len(watched) == 0 {
		return 0, 0
	}

	days := make([]time.Time, 0, len(watched))
	for day := range watched {
		days = append(days, day)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })

	run := 0
	var previous time.Time
	for _, day := range days {
		// AddDate rather than adding 24 hours, which is wrong on the two days
		// a year the clocks change.
		if run > 0 && previous.AddDate(0, 0, 1).Equal(day) {
			run++
		} else {
			run = 1
		}
		if run > longest {
			longest = run
		}
		previous = day
	}

	day := statsDay(now)
	if !watched[day] {
		day = day.AddDate(0, 0, -1)
	}
	for watched[day] {
		current++
		day = day.AddDate(0, 0, -1)
	}
	return current, longest
}

// statsMostBinged is the show with the most episodes in the window. Ties go to
// the show watched most recently, which is the one the viewer will recognise.
func statsMostBinged(activities []statsActivity) (title string, episodes int) {
	type tally struct {
		title    string
		episodes int
		last     time.Time
	}
	byShow := map[string]*tally{}
	for _, activity := range activities {
		if activity.Episodes <= 0 {
			continue
		}
		key := strconv.Itoa(activity.MediaID)
		if activity.MediaID == 0 {
			key = "title:" + activity.Title
		}
		entry := byShow[key]
		if entry == nil {
			entry = &tally{title: activity.Title}
			byShow[key] = entry
		}
		entry.episodes += activity.Episodes
		if activity.At.After(entry.last) {
			entry.last = activity.At
		}
	}

	var best *tally
	for _, entry := range byShow {
		if best == nil || entry.episodes > best.episodes ||
			(entry.episodes == best.episodes && entry.last.After(best.last)) {
			best = entry
		}
	}
	if best == nil {
		return "", 0
	}
	return best.title, best.episodes
}

// formatWatchMinutes writes a duration the way a person says it: "45m",
// "3h 20m", "2d 4h". Past a day the minutes are noise.
func formatWatchMinutes(minutes int) string {
	if minutes <= 0 {
		return "0m"
	}
	days := minutes / (24 * 60)
	hours := minutes % (24 * 60) / 60
	rest := minutes % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0 && rest > 0:
		return fmt.Sprintf("%dh %dm", hours, rest)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dm", rest)
	}
}

// formatStatsCount groups thousands, so a four-digit episode count reads at a
// glance.
func formatStatsCount(value int) string {
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}
	digits := strconv.Itoa(value)
	var b strings.Builder
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(digit)
	}
	return sign + b.String()
}

func formatStatsEpisodes(episodes int) string {
	if episodes == 1 {
		return "1 episode"
	}
	return formatStatsCount(episodes) + " episodes"
}

func formatStatsDays(days int) string {
	if days == 1 {
		return "1 day"
	}
	return strconv.Itoa(days) + " days"
}

// statsLabels maps AniList's enum spellings to the words the rest of the
// program uses for the same things.
var statsLabels = map[string]string{
	"CURRENT":   "Watching",
	"PLANNING":  "Planning",
	"COMPLETED": "Completed",
	"DROPPED":   "Dropped",
	"PAUSED":    "On Hold",
	"REPEATING": "Rewatching",
	"TV_SHORT":  "TV Short",
	"MOVIE":     "Movie",
	"SPECIAL":   "Special",
	"MUSIC":     "Music",
}

func statsLabel(name string) string {
	if label, ok := statsLabels[name]; ok {
		return label
	}
	return name
}

func formatStatCounts(counts []statCount, limit int) string {
	parts := make([]string, 0, len(counts))
	for _, count := range counts {
		if limit > 0 && len(parts) == limit {
			break
		}
		parts = append(parts, fmt.Sprintf("%s %s", statsLabel(count.Name), formatStatsCount(count.Count)))
	}
	return strings.Join(parts, " · ")
}

// statsRow is one labelled line of the page.
type statsRow struct {
	Label string
	Value string
}

// statsSection is a heading and its rows. A section with no heading is a
// line of prose, carried in Note.
type statsSection struct {
	Title string
	Rows  []statsRow
	Note  string
}

const (
	statsTimeCaveat      = "Time is estimated from episode counts; skipped openings are not subtracted."
	statsMyAnimeListNote = "MyAnimeList keeps no dated history, so there are no weekly numbers or streaks."
	statsNeedsTracker    = "Stats need AniList or MyAnimeList. Local tracking keeps no totals to show — switch tracker with Change Tracker."
)

// buildStatsSections lays the stats out as sections, shared by the terminal
// page and the rofi message so both say the same thing.
func buildStatsSections(stats watchStats, now time.Time) []statsSection {
	sections := []statsSection{}

	if totals := stats.AniList; totals != nil {
		rows := []statsRow{
			{"Anime", formatStatsCount(totals.Count)},
			{"Episodes", formatStatsCount(totals.EpisodesWatched)},
			{"Time watched", formatWatchMinutes(totals.MinutesWatched)},
		}
		if totals.MeanScore > 0 {
			rows = append(rows, statsRow{"Mean score", strconv.FormatFloat(totals.MeanScore, 'f', 1, 64)})
		}
		if genres := formatStatCounts(totals.Genres, 3); genres != "" {
			rows = append(rows, statsRow{"Top genres", genres})
		}
		if formats := formatStatCounts(totals.Formats, 3); formats != "" {
			rows = append(rows, statsRow{"Formats", formats})
		}
		if statuses := formatStatCounts(totals.Statuses, 0); statuses != "" {
			rows = append(rows, statsRow{"Statuses", statuses})
		}
		sections = append(sections, statsSection{Title: "All time", Rows: rows})

		weekEpisodes, weekMinutes := statsPeriodTotals(stats.Activities, statsWeekStart(now))
		monthEpisodes, monthMinutes := statsPeriodTotals(stats.Activities, statsMonthStart(now))
		current, longest := statsStreaks(stats.Activities, now)
		history := []statsRow{
			{"This week", formatStatsEpisodes(weekEpisodes) + " · ~" + formatWatchMinutes(weekMinutes)},
			{"This month", formatStatsEpisodes(monthEpisodes) + " · ~" + formatWatchMinutes(monthMinutes)},
			{"Current streak", formatStatsDays(current)},
			{"Longest streak", formatStatsDays(longest)},
		}
		if title, episodes := statsMostBinged(stats.Activities); title != "" {
			history = append(history, statsRow{"Most binged", fmt.Sprintf("%s (%s)", title, formatStatsEpisodes(episodes))})
		}
		sections = append(sections, statsSection{
			Title: fmt.Sprintf("Last %d days", statsHistoryDays),
			Rows:  history,
		})
		if stats.HistoryNote != "" {
			sections = append(sections, statsSection{Note: stats.HistoryNote})
		}
	}

	if totals := stats.MyAnimeList; totals != nil {
		rows := []statsRow{
			{"Anime", formatStatsCount(totals.Items)},
			{"Episodes", formatStatsCount(totals.Episodes)},
			// MyAnimeList reports days as a fraction; minutes keep the page's
			// one way of writing time.
			{"Time watched", formatWatchMinutes(int(math.Round(totals.DaysWatched * 24 * 60)))},
		}
		if totals.MeanScore > 0 {
			rows = append(rows, statsRow{"Mean score", strconv.FormatFloat(totals.MeanScore, 'f', 2, 64)})
		}
		rows = append(rows, statsRow{"Statuses", strings.Join([]string{
			"Watching " + formatStatsCount(totals.Watching),
			"Completed " + formatStatsCount(totals.Completed),
			"On Hold " + formatStatsCount(totals.OnHold),
			"Dropped " + formatStatsCount(totals.Dropped),
			"Planning " + formatStatsCount(totals.PlanToWatch),
		}, " · ")})
		if totals.Rewatched > 0 {
			rows = append(rows, statsRow{"Rewatches", formatStatsCount(totals.Rewatched)})
		}
		sections = append(sections, statsSection{Title: "All time", Rows: rows})
		sections = append(sections, statsSection{Note: statsMyAnimeListNote})
	}

	sections = append(sections, statsSection{Note: statsTimeCaveat})
	return sections
}

// statsLabelWidth is the widest row label across every section, so values
// line up down the whole page rather than section by section.
func statsLabelWidth(sections []statsSection) int {
	width := 0
	for _, section := range sections {
		for _, row := range section.Rows {
			if w := len([]rune(row.Label)); w > width {
				width = w
			}
		}
	}
	return width
}

// renderStatsRows writes rows indented under their heading, labels padded to
// labelWidth.
func renderStatsRows(rows []statsRow, labelWidth int) string {
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		padding := strings.Repeat(" ", max(labelWidth-len([]rune(row.Label)), 0))
		lines = append(lines, "  "+row.Label+padding+"  "+row.Value)
	}
	return strings.Join(lines, "\n")
}

// renderStatsPlain writes the sections as aligned plain text. Styling is left
// to the caller: rofi wraps it in markup.
func renderStatsPlain(sections []statsSection) string {
	labelWidth := statsLabelWidth(sections)
	blocks := make([]string, 0, len(sections))
	for _, section := range sections {
		lines := []string{}
		if section.Title != "" {
			lines = append(lines, section.Title)
		}
		if len(section.Rows) > 0 {
			lines = append(lines, renderStatsRows(section.Rows, labelWidth))
		}
		if section.Note != "" {
			lines = append(lines, section.Note)
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	return strings.Join(blocks, "\n\n")
}

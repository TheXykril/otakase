package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/icons"
)

// Time capsules, opened on Otakase's birthday: April 12, the first one in
// 2026.
//
// A personal one: a message to next year's you, kept on this computer next
// to your stats from the day it was written. On the next April 12 Otakase
// opens it as it starts and shows those stats against today's.
//
// A community one: members seal a message with /capsule in the Discord or
// from the Community menu here, and on April 12 the app shows one of them,
// picked at random by the bot, with a way to report it. The bot filters
// every message; nothing here needs to.
//
// On the birthday the app also offers to write next year's letter. All of
// it waits for a launch on April 12 in local time.
//
//	POST   /v1/capsule {"text":"…"} (auth) → {"opens":2027,"replaced":false}
//	GET    /v1/capsule/mine (auth)         → {"text":"…","opens":2027}
//	DELETE /v1/capsule (auth)              → {}
//	GET    /v1/capsule                     → {"id","text","written","year"} on the birthday
//	POST   /v1/capsule/report {"id"} (auth)

const (
	timeCapsuleFileName = "time_capsule.json"
	// otakaseBornYear is the year of the first birthday.
	otakaseBornYear         = 2026
	timeCapsuleLetterMax    = 500
	timeCapsuleCommunityMax = 300
	// timeCapsuleStatsWait caps how long opening or writing a letter waits
	// on the tracker for the stats.
	timeCapsuleStatsWait = 8 * time.Second
)

// timeCapsuleNow is the capsule's clock. OTAKASE_CAPSULE_DATE=2027-04-12
// pretends it's that day, to try the birthday out.
var timeCapsuleNow = func() time.Time {
	if day := os.Getenv("OTAKASE_CAPSULE_DATE"); day != "" {
		if t, err := time.ParseInLocation("2006-01-02", day, time.Local); err == nil {
			now := time.Now()
			return time.Date(t.Year(), t.Month(), t.Day(), now.Hour(), now.Minute(), now.Second(), 0, time.Local)
		}
	}
	return time.Now()
}

// isOtakaseBirthday reports whether t is April 12.
func isOtakaseBirthday(t time.Time) bool {
	return t.Month() == time.April && t.Day() == 12
}

// nextOtakaseBirthday is the year of the first April 12 after t's day.
func nextOtakaseBirthday(t time.Time) int {
	if t.Month() < time.April || (t.Month() == time.April && t.Day() < 12) {
		return t.Year()
	}
	return t.Year() + 1
}

// capsuleStats is what a letter keeps of the stats page, to compare with a
// year later.
type capsuleStats struct {
	Anime     int     `json:"anime"`
	Episodes  int     `json:"episodes"`
	Minutes   int     `json:"minutes"`
	Completed int     `json:"completed"`
	MeanScore float64 `json:"mean_score,omitempty"`
	TopGenre  string  `json:"top_genre,omitempty"`
}

// capsuleLetter is a message to future you.
type capsuleLetter struct {
	Text    string        `json:"text"`
	Written time.Time     `json:"written"`
	Opens   int           `json:"opens"`
	Stats   *capsuleStats `json:"stats,omitempty"`
	Opened  bool          `json:"opened,omitempty"`
}

type timeCapsuleFile struct {
	Letters []capsuleLetter `json:"letters,omitempty"`
	// Birthdays (years) the letter offer and the community message were
	// already shown on, so each comes once.
	Offered        int `json:"offered,omitempty"`
	CommunityShown int `json:"community_shown,omitempty"`
}

func timeCapsulePath(config *Config) string {
	return filepath.Join(storageDir(config.StoragePath), timeCapsuleFileName)
}

func loadTimeCapsule(config *Config) timeCapsuleFile {
	var f timeCapsuleFile
	if data, err := os.ReadFile(timeCapsulePath(config)); err == nil {
		if err := json.Unmarshal(data, &f); err != nil {
			Log(fmt.Sprintf("Time capsule: unreadable file: %v", err))
		}
	}
	return f
}

func saveTimeCapsule(config *Config, f timeCapsuleFile) error {
	path := timeCapsulePath(config)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// lettersToOpen are the letters due by year's birthday, oldest first. A
// birthday missed opens on the next one.
func lettersToOpen(f timeCapsuleFile, year int) []int {
	var due []int
	for i, l := range f.Letters {
		if !l.Opened && l.Opens <= year {
			due = append(due, i)
		}
	}
	return due
}

// pendingLetter is the unopened letter waiting for next birthday, if any.
func pendingLetter(f timeCapsuleFile, next int) (int, bool) {
	for i, l := range f.Letters {
		if !l.Opened && l.Opens == next {
			return i, true
		}
	}
	return 0, false
}

// snapshotStats takes the numbers a letter keeps from the stats.
func snapshotStats(stats watchStats) *capsuleStats {
	switch {
	case stats.AniList != nil:
		t := stats.AniList
		s := &capsuleStats{Anime: t.Count, Episodes: t.EpisodesWatched, Minutes: t.MinutesWatched, MeanScore: t.MeanScore}
		for _, st := range t.Statuses {
			if st.Name == "COMPLETED" {
				s.Completed = st.Count
			}
		}
		if len(t.Genres) > 0 {
			s.TopGenre = t.Genres[0].Name
		}
		return s
	case stats.MyAnimeList != nil:
		t := stats.MyAnimeList
		return &capsuleStats{Anime: t.Items, Episodes: t.Episodes, Minutes: int(math.Round(t.DaysWatched * 24 * 60)),
			Completed: t.Completed, MeanScore: t.MeanScore}
	}
	return nil
}

// fetchCapsuleStats reads today's stats, or nil when there's no tracker or
// it doesn't answer in time.
func fetchCapsuleStats(config *Config) *capsuleStats {
	if statsTrackerKey(config) == TrackingRemoteNone {
		return nil
	}
	done := make(chan *capsuleStats, 1)
	go func() {
		stats, err := fetchWatchStats(config)
		if err != nil {
			Log(fmt.Sprintf("Time capsule: stats: %v", err))
			done <- nil
			return
		}
		done <- snapshotStats(stats)
	}()
	select {
	case s := <-done:
		return s
	case <-time.After(timeCapsuleStatsWait):
		Log("Time capsule: stats took too long")
		return nil
	}
}

// statsChange is "812 → 1,240 (+428)".
func statsChange(then, now int, format func(int) string) string {
	line := format(then) + " → " + format(now)
	switch diff := now - then; {
	case diff > 0:
		line += " (+" + format(diff) + ")"
	case diff < 0:
		line += " (−" + format(-diff) + ")"
	}
	return line
}

// letterSections lays an opened letter out like the stats page: the
// message, then the stats from when it was written next to today's.
func letterSections(l capsuleLetter, now *capsuleStats) []statsSection {
	sections := []statsSection{{Note: "“" + l.Text + "”"}}
	then := l.Stats
	switch {
	case then != nil && now != nil:
		rows := []statsRow{
			{"Anime", statsChange(then.Anime, now.Anime, formatStatsCount)},
			{"Episodes", statsChange(then.Episodes, now.Episodes, formatStatsCount)},
			{"Time watched", statsChange(then.Minutes, now.Minutes, formatWatchMinutes)},
			{"Completed", statsChange(then.Completed, now.Completed, formatStatsCount)},
		}
		if then.MeanScore > 0 && now.MeanScore > 0 {
			rows = append(rows, statsRow{"Mean score", strconv.FormatFloat(then.MeanScore, 'f', 1, 64) + " → " + strconv.FormatFloat(now.MeanScore, 'f', 1, 64)})
		}
		if then.TopGenre != "" && now.TopGenre != "" {
			genre := then.TopGenre + " → " + now.TopGenre
			if then.TopGenre == now.TopGenre {
				genre = now.TopGenre + ", still"
			}
			rows = append(rows, statsRow{"Top genre", genre})
		}
		sections = append(sections, statsSection{Title: "Then and now", Rows: rows})
	case then != nil:
		sections = append(sections, statsSection{Title: "Back then", Rows: []statsRow{
			{"Anime", formatStatsCount(then.Anime)},
			{"Episodes", formatStatsCount(then.Episodes)},
			{"Time watched", formatWatchMinutes(then.Minutes)},
		}})
	}
	sections = append(sections, statsSection{Note: "Written " + l.Written.Local().Format("January 2, 2006") + "."})
	return sections
}

// OfferBirthdayCapsule runs the birthday at launch: opens letters that are
// due, shows a community message and offers to write next year's letter.
// It reports whether it showed anything (so the startup progress can be put
// back).
func OfferBirthdayCapsule(config *Config) bool {
	now := timeCapsuleNow()
	if config == nil || !isOtakaseBirthday(now) || castWindowNonInteractive() {
		return false
	}
	year := now.Year()
	f := loadTimeCapsule(config)
	shown := false

	if due := lettersToOpen(f, year); len(due) > 0 {
		EndStartupProgress()
		shown = true
		today := fetchCapsuleStats(config)
		for _, i := range due {
			showLetter(config, f.Letters[i], today)
			f.Letters[i].Opened = true
			if err := saveTimeCapsule(config, f); err != nil {
				Log(fmt.Sprintf("Time capsule: %v", err))
			}
		}
	}

	if f.CommunityShown < year && discordEnabled(config) {
		note, err := fetchCommunityCapsule(config)
		if err == nil || errors.Is(err, errCapsuleEmpty) {
			f.CommunityShown = year
			_ = saveTimeCapsule(config, f)
		}
		if err == nil {
			EndStartupProgress()
			shown = true
			showCommunityCapsule(config, note)
		} else if !errors.Is(err, errCapsuleEmpty) {
			Log(fmt.Sprintf("Time capsule: community message: %v", err))
		}
	}

	if _, written := pendingLetter(f, year+1); f.Offered < year && !written {
		EndStartupProgress()
		shown = true
		f.Offered = year
		_ = saveTimeCapsule(config, f)
		offerBirthdayLetter(config, year)
	}
	if shown {
		EnterAppScreen()
	}
	return shown
}

func showLetter(config *Config, l capsuleLetter, today *capsuleStats) {
	title := fmt.Sprintf("A letter from %d you", l.Written.Local().Year())
	sections := letterSections(l, today)
	if config.RofiSelection {
		showWatchStatsRofi(title, renderStatsPlain(sections))
		return
	}
	EnterAppScreen()
	showWatchStatsTerminal(title, sections)
}

func otakaseAge(year int) int { return year - otakaseBornYear }

// offerBirthdayLetter is the birthday's question: a letter for next year,
// and a message for the community capsule too when the app is linked.
func offerBirthdayLetter(config *Config, year int) {
	selected, err := selectWithOptionalMessage(config, []SelectionOption{
		{Key: "write", Label: "Write to next year's me", Icon: icons.Edit},
		{Key: "no", Label: "Not this year", Icon: icons.No},
	}, "Time capsule", fmt.Sprintf("Otakase turns %d today! Leave a message for next year's you? It opens on April 12, %d, next to your stats from today.",
		otakaseAge(year), year+1))
	if err != nil {
		Log(fmt.Sprintf("Time capsule: %v", err))
		return
	}
	selected = NormalizeSelectionKey(selected)
	if SelectionMeansQuit(selected) {
		Exit(nil)
	}
	if selected.Key != "write" {
		return
	}
	if !writeLetter(config, year+1) {
		return
	}
	if discordLinked(config) {
		selected, err = selectWithOptionalMessage(config, []SelectionOption{
			{Key: "community", Label: "Leave one in the community time capsule too", Icon: icons.Surprise},
			{Key: "no", Label: "No thanks", Icon: icons.No},
		}, "Time capsule", "Sealed. One more? The community capsule opens the same day, and anyone might read it.")
		if err == nil && NormalizeSelectionKey(selected).Key == "community" {
			writeCommunityCapsule(config)
		}
	}
}

// writeLetter asks for a letter that opens on opens's birthday and keeps
// it with today's stats. It reports whether one was written.
func writeLetter(config *Config, opens int) bool {
	stats := make(chan *capsuleStats, 1)
	// Read while the letter is being written.
	go func() { stats <- fetchCapsuleStats(config) }()
	EnterAppScreen()
	text, cancelled, err := promptCancelable(config, "Time capsule",
		fmt.Sprintf("Your message for April 12, %d", opens), "Only you will see it · it stays on this computer")
	text = clipRunes(strings.TrimSpace(text), timeCapsuleLetterMax)
	if err != nil || cancelled || text == "" {
		return false
	}
	f := loadTimeCapsule(config)
	letter := capsuleLetter{Text: text, Written: timeCapsuleNow(), Opens: opens, Stats: <-stats}
	if i, ok := pendingLetter(f, opens); ok {
		f.Letters[i] = letter
	} else {
		f.Letters = append(f.Letters, letter)
	}
	if err := saveTimeCapsule(config, f); err != nil {
		Log(fmt.Sprintf("Time capsule: %v", err))
		Out("Could not save your message: " + err.Error())
		return false
	}
	Out(fmt.Sprintf("Sealed. See you on April 12, %d.", opens))
	return true
}

// The community capsule.

type communityCapsule struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Written string `json:"written"`
	Year    int    `json:"year"`
}

var errCapsuleEmpty = errors.New("no community message to show")

func fetchCommunityCapsule(config *Config) (communityCapsule, error) {
	var note communityCapsule
	ctx, cancel := context.WithTimeout(context.Background(), discordRequestTimeout)
	defer cancel()
	status, err := discordDo(ctx, config, http.MethodGet, "/v1/capsule", "", "", nil, &note)
	if status == http.StatusNotFound {
		return note, errCapsuleEmpty
	}
	if err == nil && strings.TrimSpace(note.Text) == "" {
		return note, errCapsuleEmpty
	}
	return note, err
}

func showCommunityCapsule(config *Config, note communityCapsule) {
	options := []SelectionOption{{Key: "ok", Label: "Back to Otakase", Icon: icons.Yes}}
	if discordLinked(config) {
		options = append(options, SelectionOption{Key: "report", Label: "Report this message", Icon: icons.No})
	}
	written := ""
	if note.Written != "" {
		written = ", " + note.Written
	}
	selected, err := selectWithOptionalMessage(config, options, "Time capsule",
		fmt.Sprintf("From the community time capsule:\n\n“%s”\n\n— someone in the Otakase community%s", note.Text, written))
	if err != nil {
		Log(fmt.Sprintf("Time capsule: %v", err))
		return
	}
	selected = NormalizeSelectionKey(selected)
	if SelectionMeansQuit(selected) {
		Exit(nil)
	}
	if selected.Key != "report" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), discordRequestTimeout)
	defer cancel()
	if _, err := discordPostJSON(ctx, config, "/v1/capsule/report", loadDiscordLink(config).Token, map[string]string{"id": note.ID}, nil); err != nil {
		Log(fmt.Sprintf("Time capsule: report: %v", err))
		Out("Could not send the report: " + err.Error())
		return
	}
	Out("Thanks, reported. Enough reports take a message out on their own.")
}

// writeCommunityCapsule asks for a community message and seals it with the
// bot, which says why when its filter turns one down.
func writeCommunityCapsule(config *Config) {
	for {
		EnterAppScreen()
		text, cancelled, err := promptCancelable(config, "Time capsule", "Your message for the community capsule",
			"Anyone might read it on April 12 · no names, links or contact details")
		text = strings.TrimSpace(text)
		if err != nil || cancelled || text == "" {
			return
		}
		var answer struct {
			Opens    int  `json:"opens"`
			Replaced bool `json:"replaced"`
		}
		ctx, cancel := context.WithTimeout(context.Background(), discordRequestTimeout)
		_, err = discordPostJSON(ctx, config, "/v1/capsule", loadDiscordLink(config).Token, map[string]string{"text": clipRunes(text, timeCapsuleCommunityMax)}, &answer)
		cancel()
		var apiErr *discordAPIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnprocessableEntity {
			// Turned down by the filter: say why and let them try again.
			selected, menuErr := selectWithOptionalMessage(config, []SelectionOption{
				{Key: "again", Label: "Write it again", Icon: icons.Edit},
				{Key: "no", Label: "Leave it", Icon: icons.No},
			}, "Time capsule", apiErr.Message)
			if menuErr != nil || NormalizeSelectionKey(selected).Key != "again" {
				return
			}
			continue
		}
		if err != nil {
			Log(fmt.Sprintf("Time capsule: seal: %v", err))
			Out("Could not seal your message: " + err.Error())
			return
		}
		what := "Sealed"
		if answer.Replaced {
			what = "Sealed, in place of your earlier one"
		}
		Out(fmt.Sprintf("%s. It opens on April 12, %d.", what, answer.Opens))
		return
	}
}

// The Community menu row: write either message any time of the year.

func init() {
	addCommunityRow(communityRow{options: timeCapsuleOptions, handle: handleTimeCapsule})
}

func timeCapsuleOptions(config *Config, linked bool) []SelectionOption {
	return []SelectionOption{{Key: "capsule", Label: fmt.Sprintf("Time capsule · opens April 12, %d", nextOtakaseBirthday(timeCapsuleNow())), Icon: icons.Surprise}}
}

func handleTimeCapsule(config *Config, key string) (handled, play bool) {
	if key != "capsule" {
		return false, false
	}
	next := nextOtakaseBirthday(timeCapsuleNow())
	f := loadTimeCapsule(config)
	letter := "Write to next year's me · only you will see it"
	if i, ok := pendingLetter(f, next); ok {
		letter = "Rewrite my letter · “" + clipRunes(f.Letters[i].Text, 40) + "”"
	}
	options := []SelectionOption{{Key: "letter", Label: letter, Icon: icons.Edit}}
	linked := discordLinked(config)
	var mine struct {
		Text string `json:"text"`
	}
	if linked {
		ctx, cancel := context.WithTimeout(context.Background(), discordRequestTimeout)
		status, err := discordDo(ctx, config, http.MethodGet, "/v1/capsule/mine", loadDiscordLink(config).Token, "", nil, &mine)
		cancel()
		if err != nil && status != http.StatusNotFound {
			Log(fmt.Sprintf("Time capsule: own message: %v", err))
		}
		community := SelectionOption{Key: "community", Label: "Leave a message for the community · anyone might read it", Icon: icons.Surprise}
		if mine.Text != "" {
			community.Label = "Replace my community message · “" + clipRunes(mine.Text, 40) + "”"
		}
		options = append(options, community)
		if mine.Text != "" {
			options = append(options, SelectionOption{Key: "unseal", Label: "Take my community message out", Icon: icons.No})
		}
	}
	note := fmt.Sprintf("Messages in the time capsule open on Otakase's birthday, April 12, %d.", next)
	if !linked {
		note += " Link to Discord to leave one for the community too."
	}
	selected, err := selectWithOptionalMessage(config, options, "Time capsule", note)
	if err != nil {
		Log(fmt.Sprintf("Time capsule: %v", err))
		return true, false
	}
	selected = NormalizeSelectionKey(selected)
	if SelectionMeansQuit(selected) {
		Exit(nil)
	}
	switch selected.Key {
	case "letter":
		writeLetter(config, next)
	case "community":
		writeCommunityCapsule(config)
	case "unseal":
		ctx, cancel := context.WithTimeout(context.Background(), discordRequestTimeout)
		defer cancel()
		if _, err := discordDo(ctx, config, http.MethodDelete, "/v1/capsule", loadDiscordLink(config).Token, "", nil, nil); err != nil {
			Out("Could not take it out: " + err.Error())
			return true, false
		}
		Out("Taken out of the time capsule.")
	}
	return true, false
}

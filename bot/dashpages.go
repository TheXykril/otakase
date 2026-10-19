package main

import (
	"fmt"
	"html/template"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// More dashboard pages: settings, safety, airing, support, games and the
// bot's own log. The pages with controls add them under what they show.

func init() {
	addDashSection("Log", (*bot).dashLog)
}

// Settings.

func (b *bot) dashSettings() template.HTML {
	parts := []template.HTML{dashNote("Changes work at once, no restart. Reset goes back to the add-on option.")}
	for _, s := range settings {
		v, changed := b.settingValue(s.Name)
		c := dashControl{Section: "settings", Do: "setting", Name: s.Name, Button: "Save"}
		switch s.Type {
		case settingBool:
			c.Choices = []dashChoice{{"true", "on", v == "true"}, {"false", "off", v != "true"}}
		case settingInt:
			c.Number = &dashNumber{Value: v, Min: formatFloat(s.Min), Max: formatFloat(s.Max), Step: "1"}
		default:
			c.Number = &dashNumber{Value: v, Min: formatFloat(s.Min), Max: formatFloat(s.Max), Step: "0.01"}
		}
		ctls := []dashControl{c}
		now := s.show(v) + " · add-on option"
		if changed {
			now = s.show(v) + " · changed (add-on option: " + s.show(s.Default(b.cfg)) + ")"
			ctls = append(ctls, dashControl{Section: "settings", Do: "setting-reset", Name: s.Name, Button: "Reset", Quiet: true})
		}
		parts = append(parts, dashExec("setting", struct {
			Name, Desc, Now string
			Controls        template.HTML
		}{s.Name, s.Desc, now, dashControls(ctls...)}))
	}
	return dashJoin(parts...)
}

func init() {
	template.Must(dashPartsTmpl.New("setting").Parse(
		`<div class="card"><b>{{.Name}}</b> <span class="now">{{.Now}}</span><div class="note">{{.Desc}}</div>{{.Controls}}</div>`))
}

// Safety.

func (b *bot) dashSafety() template.HTML {
	now := time.Now()
	var v verifyData
	var spoiler string
	b.store.view(func(d *storeData) {
		if d.Verify != nil {
			v = *d.Verify
		}
		spoiler = d.SpoilerGuard
	})
	if spoiler == "" {
		spoiler = spoilerNudge
	}
	verify := "off"
	if verifyActive(&v, now) {
		verify = "on"
	}
	b.raid.mu.Lock()
	raid := b.raid.alerted
	b.raid.mu.Unlock()
	if v.Raid.After(raid) {
		raid = v.Raid
	}
	lastRaid := "none since start"
	if !raid.IsZero() {
		lastRaid = dashAgo(now.Sub(raid)) + " ago"
	}
	mode := modeName(v.Mode)
	return dashJoin(
		dashStats(
			dashStat{"Verification", mode + " · " + verify + " now"},
			dashStat{"Waiting at the check", fmt.Sprint(len(v.Pending))},
			dashStat{"Last burst of joins", lastRaid},
			dashStat{"Spoiler guard", spoiler},
		),
		dashHeading("Join verification"),
		dashNote("Auto turns on when lots of members join at once and off after an hour of calm. Off lets everyone waiting in."),
		dashControls(dashControl{Section: "safety", Do: "verify", Button: "Set", Choices: []dashChoice{
			{"auto", "auto", mode == "auto"}, {"on", "on", mode == "on"}, {"off", "off", mode == "off"}}}),
		dashHeading("Spoiler guard"),
		dashNote("Nudge posts a short reminder that deletes itself. Hide reposts the message inside spoiler tags."),
		dashControls(dashControl{Section: "safety", Do: "spoiler", Button: "Set", Choices: []dashChoice{
			{spoilerOff, "off", spoiler == spoilerOff}, {spoilerNudge, "nudge", spoiler == spoilerNudge}, {spoilerHide, "hide", spoiler == spoilerHide}}}),
		dashHeading("Invites"),
		dashNote("Stop new members joining through invites for half an hour, like the raid guard does, or let them in again."),
		dashControls(
			dashControl{Section: "safety", Do: "invites-pause", Button: "Pause invites 30 min"},
			dashControl{Section: "safety", Do: "invites-resume", Button: "Resume invites", Quiet: true},
		),
	)
}

// Airing.

func (b *bot) dashAiring() template.HTML {
	var board *airingBoard
	b.store.view(func(d *storeData) {
		if d.Airing != nil {
			c := *d.Airing
			c.Shows = append([]airing(nil), d.Airing.Shows...)
			c.All = nil
			board = &c
		}
	})
	daily := "off"
	if b.settingBool("daily_airing") {
		daily = fmt.Sprintf("on, at %02d:00 UTC", b.settingInt("airing_hour_utc"))
	}
	stats := []dashStat{{"Daily board", daily}}
	button := "Post board now"
	var rows [][]string
	if board == nil || board.Message == "" {
		stats = append(stats, dashStat{"Today's board", "not posted"})
	} else {
		button = "Redraw board now"
		stats = append(stats, dashStat{"Board for", board.Day.Format("Mon 2 Jan")}, dashStat{"Shows on it", fmt.Sprint(len(board.Shows))})
		now := time.Now().Unix()
		for _, a := range board.Shows {
			when := time.Unix(a.At, 0).UTC().Format("15:04 UTC")
			state := "coming"
			switch {
			case a.At+3600 <= now:
				state = "aired"
			case a.At <= now:
				state = "airing now"
			}
			rows = append(rows, []string{when, a.Title, epLabel(a), state})
		}
	}
	parts := []template.HTML{dashStats(stats...)}
	if b.cfg.AiringChannel == "" {
		parts = append(parts, dashNote("No airing channel found, so the board is off."))
	} else {
		parts = append(parts, dashControls(dashControl{Section: "airing", Do: "airing", Button: button}),
			dashNote("Redraw updates the live parts (airing now, up next) at once instead of on the next hour."))
	}
	if board != nil && board.Message != "" {
		parts = append(parts, dashHeading("On the board"), dashTable([]string{"Time", "Show", "Episode", ""}, rows, "Nothing airs today."))
	}
	return dashJoin(parts...)
}

// Support.

type supportCounts struct {
	Open, Solved, Bugs, BugsLinked int
	OpenPosts                      []supportPost
	Helpers                        []guessRank
	Answers                        []answerStatRow
}

type answerStatRow struct {
	Key               string
	Helped, NotHelped int
}

func (st *store) supportCounts() supportCounts {
	var c supportCounts
	st.view(func(d *storeData) {
		for _, p := range d.SupportPosts {
			if p.Solved {
				c.Solved++
			} else {
				c.Open++
				c.OpenPosts = append(c.OpenPosts, supportPost{ID: p.ID, Title: p.Title})
			}
		}
		for _, r := range d.BugReports {
			c.Bugs++
			if r.Issue != 0 {
				c.BugsLinked++
			}
		}
		for u, h := range d.Helpers {
			if h.Points > 0 {
				c.Helpers = append(c.Helpers, guessRank{u, h.Points})
			}
		}
		for k, t := range d.AnswerStats {
			c.Answers = append(c.Answers, answerStatRow{k, t.Helped, t.NotHelped})
		}
	})
	sort.Slice(c.Helpers, func(i, j int) bool {
		if c.Helpers[i].Points != c.Helpers[j].Points {
			return c.Helpers[i].Points > c.Helpers[j].Points
		}
		return c.Helpers[i].User < c.Helpers[j].User
	})
	sort.Slice(c.Answers, func(i, j int) bool {
		ti, tj := c.Answers[i].Helped+c.Answers[i].NotHelped, c.Answers[j].Helped+c.Answers[j].NotHelped
		if ti != tj {
			return ti > tj
		}
		return c.Answers[i].Key < c.Answers[j].Key
	})
	return c
}

func (b *bot) dashSupport() template.HTML {
	c := b.store.supportCounts()
	now := time.Now()
	var open [][]string
	for _, p := range c.OpenPosts {
		if len(open) == 15 {
			break
		}
		age := "—"
		if t, err := discordgo.SnowflakeTimestamp(p.ID); err == nil {
			age = dashAgo(now.Sub(t)) + " ago"
		}
		open = append(open, []string{p.Title, age})
	}
	var helpers [][]string
	for k, h := range c.Helpers {
		if k == 10 {
			break
		}
		helpers = append(helpers, []string{b.dashName(h.User), fmt.Sprint(h.Points)})
	}
	var answers [][]string
	for k, a := range c.Answers {
		if k == 15 {
			break
		}
		answers = append(answers, []string{a.Key, fmt.Sprint(a.Helped), fmt.Sprint(a.NotHelped)})
	}
	parts := []template.HTML{
		dashStats(
			dashStat{"Open posts", fmt.Sprint(c.Open)},
			dashStat{"Solved posts", fmt.Sprint(c.Solved)},
			dashStat{"Bugs waiting on a fix", fmt.Sprint(c.Bugs)},
			dashStat{"Of those with an issue", fmt.Sprint(c.BugsLinked)},
		),
	}
	if b.cfg.SupportForum != "" {
		parts = append(parts, dashControls(dashControl{Section: "support", Do: "stale", Button: "Check quiet posts now"}),
			dashNote("Posts quiet for a week get a nudge; nudged posts quiet for 3 more days close. This also runs every hour by itself."))
	}
	parts = append(parts,
		dashHeading("Open posts, newest first"),
		dashTable([]string{"Post", "Opened"}, open, "No open support posts."),
		dashHeading("Top helpers"),
		dashTable([]string{"Member", "Points"}, helpers, "No helper points yet."),
		dashHeading("Automatic answers"),
		dashTable([]string{"Answer", "Helped", "Didn't help"}, answers, "No votes on automatic answers yet."),
		b.dashKnownNotes(),
	)
	return dashJoin(parts...)
}

// Log.

// logRing keeps the bot's last log lines for the dashboard. The standard
// logger writes to it as well as to stderr.
type logRing struct {
	mu    sync.Mutex
	lines []string
	part  string
}

const logLinesKept = 300

var botLog = &logRing{}

func (l *logRing) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	text := l.part + string(p)
	parts := strings.Split(text, "\n")
	l.part = parts[len(parts)-1]
	for _, line := range parts[:len(parts)-1] {
		l.lines = append(l.lines, line)
	}
	if over := len(l.lines) - logLinesKept; over > 0 {
		l.lines = append(l.lines[:0:0], l.lines[over:]...)
	}
	return len(p), nil
}

// last returns up to n lines, newest first.
func (l *logRing) last(n int) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for i := len(l.lines) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, l.lines[i])
	}
	return out
}

func init() {
	template.Must(dashPartsTmpl.New("log").Parse(`<pre class="log">{{range .}}{{.}}
{{end}}</pre>`))
}

func (b *bot) dashLog() template.HTML {
	lines := botLog.last(200)
	if len(lines) == 0 {
		return dashNote("Nothing logged since the bot started.")
	}
	return dashJoin(dashNote("The bot's last log lines since it started, newest first."), dashExec("log", lines))
}

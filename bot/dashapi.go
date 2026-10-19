package main

import (
	"crypto/subtle"
	"net/http"
	"os"
	"runtime"
	"sort"
	"time"

	"github.com/bwmarrin/discordgo"
)

// The dashboard's JSON API, for the Home Assistant cards (assets/ha-card.js).
// It is served on the dashboard's port, so only Home Assistant's ingress
// reaches it, like the page. GET /api/summary returns everything the cards
// show and the token for actions; POST /api/do runs one dashboard control
// with that token in the X-Otakase-Token header, so another site can't.

type apiJob struct {
	Name    string `json:"name"`
	State   string `json:"state"`
	LastOK  string `json:"last_ok"`
	Problem string `json:"problem,omitempty"`
}

type apiRank struct {
	Name   string `json:"name"`
	Points int    `json:"points"`
	Level  int    `json:"level,omitempty"`
}

type apiShow struct {
	At      int64  `json:"at"`
	Title   string `json:"title"`
	Episode string `json:"episode"`
	State   string `json:"state"` // coming, airing, aired
	URL     string `json:"url,omitempty"`
}

type apiParty struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Start      int64  `json:"start"`
	Live       bool   `json:"live"`
	Host       string `json:"host"`
	Interested int    `json:"interested"`
}

type apiRoom struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Owner  string `json:"owner"`
	People int    `json:"people"`
	Party  bool   `json:"party"`
}

type apiSetting struct {
	Name    string  `json:"name"`
	Desc    string  `json:"desc"`
	Type    string  `json:"type"` // bool, int, float
	Value   string  `json:"value"`
	Changed bool    `json:"changed"`
	Min     float64 `json:"min"`
	Max     float64 `json:"max"`
}

type apiSummary struct {
	Token     string       `json:"token"`
	Version   string       `json:"version"`
	Uptime    int64        `json:"uptime"` // seconds
	Connected bool         `json:"connected"`
	LatencyMS int64        `json:"latency_ms"`
	Members   int          `json:"members"`
	MemoryMB  float64      `json:"memory_mb"`
	DataKB    int64        `json:"data_kb"`
	Jobs      []apiJob     `json:"jobs"`
	Failing   int          `json:"failing"`
	Safety    apiSafety    `json:"safety"`
	Airing    []apiShow    `json:"airing"`
	Parties   []apiParty   `json:"parties"`
	Rooms     []apiRoom    `json:"rooms"`
	Guess     apiGuess     `json:"guess"`
	Games     apiGames     `json:"games"`
	Levels    []apiRank    `json:"levels"`
	Support   apiSupport   `json:"support"`
	Settings  []apiSetting `json:"settings"`
	Follows   int          `json:"follows"`
	Linked    int          `json:"linked_apps"`
}

type apiSafety struct {
	VerifyMode string `json:"verify_mode"` // auto, on, off
	VerifyOn   bool   `json:"verify_on"`
	Waiting    int    `json:"waiting"`
	Spoiler    string `json:"spoiler"`
	LastRaid   int64  `json:"last_raid,omitempty"`
	Warnings30 int    `json:"warnings_30d"`
}

type apiGuess struct {
	Rounds []string  `json:"rounds"` // channel names
	Week   []apiRank `json:"week"`
	All    []apiRank `json:"all"`
}

// apiGames is the game points of every game, and today's daily puzzle.
type apiGames struct {
	Week  []apiRank `json:"week"`
	All   []apiRank `json:"all"`
	Daily struct {
		Num     int `json:"num"`
		Players int `json:"players"`
		Solved  int `json:"solved"`
	} `json:"daily"`
}

type apiSupport struct {
	Open        int       `json:"open"`
	Solved      int       `json:"solved"`
	Bugs        int       `json:"bugs"`
	Suggestions int       `json:"suggestions_open"`
	Helpers     []apiRank `json:"helpers"`
	Notes       []string  `json:"known_notes"`
}

func (b *bot) dashAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	switch {
	case r.URL.Path == "/api/summary" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, b.apiSummary())
	case r.URL.Path == "/api/do" && r.Method == http.MethodPost:
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Otakase-Token")), []byte(dashToken)) != 1 {
			apiError(w, http.StatusForbidden, "reload the card")
			return
		}
		var in dashInput
		if !readJSON(w, r, 16<<10, &in) {
			return
		}
		takeFlash() // drop an old page result
		err := b.dashDo(in, dashWho(r))
		msg, _ := takeFlash()
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
	default:
		apiError(w, http.StatusNotFound, "not found")
	}
}

func (b *bot) apiSummary() apiSummary {
	now := time.Now()
	s := apiSummary{Token: dashToken, Version: botVersion(), Uptime: int64(time.Since(startedAt).Seconds())}
	if b.s != nil {
		if l := b.s.HeartbeatLatency(); l > 0 {
			s.Connected, s.LatencyMS = true, l.Milliseconds()
		}
		if b.s.State != nil {
			if g, err := b.s.State.Guild(b.cfg.GuildID); err == nil {
				s.Members = g.MemberCount
			}
		}
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	s.MemoryMB = float64(mem.HeapAlloc*10/(1<<20)) / 10
	if fi, err := os.Stat(b.store.path); err == nil {
		s.DataKB = fi.Size() / 1024
	}

	// Jobs, failing first.
	var failing map[string]time.Time
	var jobs map[string]jobStatus
	if b.health != nil {
		failing, jobs = b.health.failingJobs(), b.health.jobsSnapshot()
	}
	names := map[string]bool{}
	for n := range jobs {
		names[n] = true
	}
	for n := range failing {
		names[n] = true
	}
	for n := range names {
		state, last, problem := dashJobState(jobs[n], now)
		if _, told := failing[n]; told && state != "failing" {
			state, problem = "failing", "staff were told"
		}
		if state == "failing" {
			s.Failing++
		}
		s.Jobs = append(s.Jobs, apiJob{n, state, last, problem})
	}
	sort.Slice(s.Jobs, func(i, j int) bool {
		if (s.Jobs[i].State == "failing") != (s.Jobs[j].State == "failing") {
			return s.Jobs[i].State == "failing"
		}
		return s.Jobs[i].Name < s.Jobs[j].Name
	})

	// Store: one look for everything.
	var board *airingBoard
	var v verifyData
	var xp, week, all, helpers, gweek, gall []guessRank
	var notes []string
	b.store.view(func(d *storeData) {
		if d.Airing != nil {
			c := *d.Airing
			c.Shows = append([]airing(nil), d.Airing.Shows...)
			board = &c
		}
		if d.Verify != nil {
			v = *d.Verify
			v.Pending = nil
			s.Safety.Waiting = len(d.Verify.Pending)
		}
		s.Safety.Spoiler = d.SpoilerGuard
		for _, ws := range d.Warnings {
			for _, w := range ws {
				if now.Sub(w.At) < 30*24*time.Hour {
					s.Safety.Warnings30++
				}
			}
		}
		for id, r := range d.Rooms {
			s.Rooms = append(s.Rooms, apiRoom{ID: id, Owner: r.Owner, Party: r.Event != ""})
		}
		xp = xpRanked(d.XP)
		gweek, gall = ranked(weekPoints(d, weekKey(now.UTC())), 10), ranked(allPoints(d), 10)
		if d.Daily != nil && d.Daily.Day == dailyDay(now) {
			s.Games.Daily.Num = d.Daily.Num
			for _, p := range d.DailyPlays {
				if p.Day == d.Daily.Day && len(p.Guesses) > 0 {
					s.Games.Daily.Players++
					if p.Solved {
						s.Games.Daily.Solved++
					}
				}
			}
		}
		week = guessLeaders(d.Guess, weekKey(now), true)
		all = guessLeaders(d.Guess, weekKey(now), false)
		for u, h := range d.Helpers {
			if h.Points > 0 {
				helpers = append(helpers, guessRank{u, h.Points})
			}
		}
		for _, p := range d.SupportPosts {
			if p.Solved {
				s.Support.Solved++
			} else {
				s.Support.Open++
			}
		}
		s.Support.Bugs = len(d.BugReports)
		for _, sg := range d.Suggestions {
			if !statusByKey(sg.Status).Closed {
				s.Support.Suggestions++
			}
		}
		notes = append(notes, d.KnownNotes...)
		for _, l := range d.Follows {
			if len(l) > 0 {
				s.Follows++
			}
		}
		s.Linked = len(d.AppLinks)
	})
	if s.Safety.Spoiler == "" {
		s.Safety.Spoiler = spoilerNudge
	}
	s.Safety.VerifyMode, s.Safety.VerifyOn = modeName(v.Mode), verifyActive(&v, now)
	b.raid.mu.Lock()
	raid := b.raid.alerted
	b.raid.mu.Unlock()
	if v.Raid.After(raid) {
		raid = v.Raid
	}
	if !raid.IsZero() {
		s.Safety.LastRaid = raid.Unix()
	}

	if board != nil && board.Message != "" {
		for _, a := range board.Shows {
			st := "coming"
			switch {
			case a.At+3600 <= now.Unix():
				st = "aired"
			case a.At <= now.Unix():
				st = "airing"
			}
			s.Airing = append(s.Airing, apiShow{a.At, a.Title, epLabel(a), st, a.URL})
		}
	}

	if events, err := b.scheduledEvents(); err == nil {
		for _, ev := range events {
			live := ev.Status == discordgo.GuildScheduledEventStatusActive
			if !live && ev.Status != discordgo.GuildScheduledEventStatusScheduled {
				continue
			}
			host := ""
			if id := partyHost(ev); id != "" {
				host = b.dashName(id)
			}
			s.Parties = append(s.Parties, apiParty{ev.ID, ev.Name, ev.ScheduledStartTime.Unix(), live, host, ev.UserCount})
		}
		sort.Slice(s.Parties, func(i, j int) bool { return s.Parties[i].Start < s.Parties[j].Start })
	}

	for k := range s.Rooms {
		r := &s.Rooms[k]
		r.Name, r.Owner = b.dashChannel(r.ID), b.dashName(r.Owner)
		if b.s != nil && b.s.State != nil {
			if g, err := b.s.State.Guild(b.cfg.GuildID); err == nil {
				for _, vs := range g.VoiceStates {
					if vs.ChannelID == r.ID {
						r.People++
					}
				}
			}
		}
	}
	sort.Slice(s.Rooms, func(i, j int) bool { return s.Rooms[i].Name < s.Rooms[j].Name })

	named := func(list []guessRank, n int, levels bool) []apiRank {
		out := []apiRank{}
		for k, r := range list {
			if k == n {
				break
			}
			x := apiRank{Name: b.dashName(r.User), Points: r.Points}
			if levels {
				x.Level, _, _ = levelFor(r.Points)
			}
			out = append(out, x)
		}
		return out
	}
	sort.Slice(helpers, func(i, j int) bool { return helpers[i].Points > helpers[j].Points })
	s.Levels, s.Guess.Week, s.Guess.All = named(xp, 10, true), named(week, 10, false), named(all, 10, false)
	s.Support.Helpers, s.Support.Notes = named(helpers, 5, false), notes
	s.Games.Week, s.Games.All = named(gweek, 10, false), named(gall, 10, false)
	s.Guess.Rounds = []string{}

	for _, st := range settings {
		val, changed := b.settingValue(st.Name)
		typ := map[settingType]string{settingBool: "bool", settingInt: "int", settingFloat: "float"}[st.Type]
		s.Settings = append(s.Settings, apiSetting{st.Name, st.Desc, typ, val, changed, st.Min, st.Max})
	}
	if s.Jobs == nil {
		s.Jobs = []apiJob{}
	}
	if s.Airing == nil {
		s.Airing = []apiShow{}
	}
	if s.Parties == nil {
		s.Parties = []apiParty{}
	}
	if s.Rooms == nil {
		s.Rooms = []apiRoom{}
	}
	if s.Support.Notes == nil {
		s.Support.Notes = []string{}
	}
	return s
}

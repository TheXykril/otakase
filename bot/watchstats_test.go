package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestWatchStreaksAndYear(t *testing.T) {
	var st watchStats
	day := time.Date(2026, 10, 1, 20, 0, 0, 0, time.UTC)
	// Nine minutes is not a streak day; the tenth is.
	addHeartbeat(&st, nil, 540, day)
	if st.Streak != 0 {
		t.Fatalf("streak after 9 min: %d", st.Streak)
	}
	addHeartbeat(&st, nil, 60, day)
	if st.Streak != 1 || st.StreakDay != "2026-10-01" {
		t.Fatalf("streak after 10 min: %+v", st)
	}
	addHeartbeat(&st, nil, 600, day) // the same day again doesn't count twice
	var earned []string
	for k := 1; k < 7; k++ {
		earned = append(earned, addHeartbeat(&st, nil, 600, day.AddDate(0, 0, k))...)
	}
	if st.Streak != 7 || st.BestStreak != 7 || currentStreak(st, day.AddDate(0, 0, 7)) != 7 {
		t.Fatalf("seven days: %+v", st)
	}
	if strings.Join(earned, ",") != "streak-3,streak-7" {
		t.Errorf("earned %v", earned)
	}
	// A day off ends it.
	if currentStreak(st, day.AddDate(0, 0, 8)) != 0 {
		t.Error("streak should be over after a day off")
	}
	addHeartbeat(&st, nil, 600, day.AddDate(0, 0, 9))
	if st.Streak != 1 || st.BestStreak != 7 {
		t.Errorf("after a gap: %+v", st)
	}
	w := st.Years[2026]
	if w.Days != 8 || w.Streak != 7 || w.Seconds != 600*9 {
		t.Errorf("year: %+v", w)
	}
	// Old years go.
	st.Years[2022] = yearWrap{Seconds: 5}
	addHeartbeat(&st, nil, 60, day)
	if _, ok := st.Years[2022]; ok {
		t.Error("2022 should be dropped")
	}
}

func TestWatchEpisodesBingeAndSeasons(t *testing.T) {
	var st watchStats
	now := time.Date(2026, 10, 2, 18, 0, 0, 0, time.UTC)
	play := func(id, ep, eps int) []string {
		var got []string
		show := &appShow{AniListID: id, Title: "Show", Episode: ep, Episodes: eps}
		for k := 0; k < 3; k++ { // 6 minutes
			got = append(got, addHeartbeat(&st, show, 120, now)...)
			now = now.Add(2 * time.Minute)
		}
		return got
	}
	play(1, 10, 12)
	play(1, 11, 12)
	play(1, 12, 12)
	if st.DayEps != 3 || st.Seasons != 1 || st.Binged {
		t.Fatalf("three episodes: %+v", st)
	}
	if !containsInt(st.Finished, 1) {
		t.Error("show 1 should be finished")
	}
	got := play(1, 12, 12) // the last episode again: no second season, but a fourth episode
	if st.Seasons != 1 {
		t.Errorf("finished twice: %d", st.Seasons)
	}
	if st.DayEps != 3 {
		t.Errorf("same episode again counted: %d", st.DayEps)
	}
	got = append(got, play(2, 1, 1)...) // a movie isn't a season
	if st.DayEps != 4 || st.Binges != 1 || !st.Binged || st.Seasons != 1 {
		t.Fatalf("binge: %+v", st)
	}
	if strings.Join(got, ",") != "binge" {
		t.Errorf("earned %v", got)
	}
	if _, ok := st.Badges["finisher"]; !ok {
		t.Error("no Season Finisher badge")
	}
	// Not shared: time counts, episodes don't.
	addHeartbeat(&st, nil, 120, now)
	if w := st.Years[2026]; w.Episodes != 4 || w.Shows[1].Seconds != 4*360 {
		t.Errorf("year: %+v", w)
	}
}

func TestWrapKeepsTopShows(t *testing.T) {
	var w yearWrap
	for id := 1; id <= keepWrapShows+5; id++ {
		addWrapShow(&w, id, fmt.Sprint("Show ", id), int64(id*100))
	}
	if len(w.Shows) != keepWrapShows {
		t.Fatalf("%d shows kept", len(w.Shows))
	}
	if _, ok := w.Shows[keepWrapShows+5]; !ok {
		t.Error("the show just watched must stay")
	}
	if _, ok := w.Shows[1]; ok {
		t.Error("the least watched should go")
	}
	if top := topWrapShows(w); top[0] != keepWrapShows+5 {
		t.Errorf("top: %v", top)
	}
}

func TestWrapViewSkipsAdult(t *testing.T) {
	w := yearWrap{Seconds: 7200, Episodes: 5, Days: 3, Streak: 2, Shows: map[int]wrapShow{
		1: {Title: "Clean", Seconds: 100}, 2: {Title: "Lewd", Seconds: 900}, 3: {Title: "Unknown", Seconds: 50}}}
	lookup := func(id int) (airingShowInfo, bool) {
		var info airingShowInfo
		info.Title.English = map[int]string{1: "Clean", 2: "Lewd"}[id]
		info.IsAdult = id == 2
		return info, id != 3
	}
	e := wrapView("u1", true, 2026, w, time.Date(2026, 12, 3, 0, 0, 0, 0, time.UTC), lookup)
	text := fmt.Sprint(e.Fields[len(e.Fields)-1].Value)
	if !strings.Contains(text, "Clean") || strings.Contains(text, "Lewd") || strings.Contains(text, "Unknown") {
		t.Errorf("top shows: %s", text)
	}
	if e := wrapView("u1", true, 2025, yearWrap{}, time.Now(), lookup); e.Title != "" {
		t.Errorf("empty year: %+v", e)
	}
}

func TestBadgeLine(t *testing.T) {
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	st := watchStats{Badges: map[string]time.Time{"streak-3": at, "streak-7": at.Add(time.Hour), "binge": at.Add(2 * time.Hour)}}
	if got := badgeLine(st); got != "🍿 Binge Watcher · 🔥 7-Day Streak" {
		t.Errorf("line = %q", got)
	}
	if badgeLine(watchStats{}) != "" {
		t.Error("no badges should be no line")
	}
}

func TestWatchHeartbeatHooks(t *testing.T) {
	b := appTestBot(t)
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	appNow = func() time.Time { return now }
	tok := testToken(b, "u1")
	b.store.update(func(d *storeData) {
		d.Featured = &featuredPick{Day: "2026-10-12", AniListID: 154587, Title: "Frieren"}
		d.Club = &watchClub{AniListID: 154587, Title: "Frieren", Episode: 12, Deadline: now.Add(48 * time.Hour)}
	})
	body := `{"share_watching":true,"watching":{"anilist_id":154587,"title":"Frieren","episode":12,"episodes":28},"watched_seconds":120}`
	for k := 0; k < 5; k++ {
		heartbeatLimit = newRateLimit(4, time.Minute)
		if w := apiJSON(t, b, "/v1/heartbeat", body, tok); w.Code != http.StatusOK {
			t.Fatalf("heartbeat: %d %s", w.Code, w.Body)
		}
		now = now.Add(2 * time.Minute)
	}
	b.store.view(func(d *storeData) {
		if st := d.WatchStats["u1"]; st.Streak != 1 || st.Years[2026].Episodes != 1 {
			t.Errorf("stats: %+v", st)
		}
		if _, ok := d.Club.Caught["u1"]; !ok {
			t.Error("not caught up with the club")
		}
		if w := d.FeaturedWatch["u1"]; !w.Paid {
			t.Errorf("featured: %+v", w)
		}
		if g := d.Games["u1"]; g.Games[featuredGame] != featuredBonus || g.Games[badgeGame] != 0 {
			t.Errorf("points: %+v", g.Games)
		}
		if s := d.ServerWatch["2026-W42"][154587]; s.Seconds != 600 || len(s.Viewers) != 1 {
			t.Errorf("server week: %+v", s)
		}
	})
	// Not sharing: only the private counts move.
	heartbeatLimit = newRateLimit(4, time.Minute)
	apiJSON(t, b, "/v1/heartbeat", `{"share_watching":false,"watching":{"anilist_id":5,"episode":1},"watched_seconds":120}`, tok)
	b.store.view(func(d *storeData) {
		if _, ok := d.ServerWatch["2026-W42"][5]; ok {
			t.Error("unshared show counted for the server")
		}
	})
}

func TestClubAPI(t *testing.T) {
	b := appTestBot(t)
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	appNow = func() time.Time { return now }
	clubLimit = newRateLimit(30, time.Minute)
	get := func() map[string]any {
		w := apiDo(t, b, http.MethodGet, "/v1/club", nil, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("club: %d", w.Code)
		}
		return decode(t, w)
	}
	if m := get(); m["club"] != nil {
		t.Fatalf("no club: %v", m)
	}
	b.cfg.GuildID = "g"
	b.store.update(func(d *storeData) {
		d.Club = &watchClub{AniListID: 7, Title: "Show", Episode: 3, Deadline: now.Add(time.Hour), Channel: "c", Message: "m",
			Joined: []string{"a", "b"}, Caught: map[string]time.Time{"a": now}}
	})
	c, _ := get()["club"].(map[string]any)
	if c["anilist_id"] != 7.0 || c["episode"] != 3.0 || c["caught_up"] != 1.0 || c["joined"] != 2.0 || c["url"] != "https://discord.com/channels/g/c/m" {
		t.Errorf("club: %v", c)
	}
	now = now.Add(2 * time.Hour)
	if m := get(); m["club"] != nil {
		t.Errorf("past the deadline: %v", m)
	}
}

func TestClubEmbed(t *testing.T) {
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	c := &watchClub{AniListID: 7, Title: "Show", Episode: 3, Deadline: now.Add(time.Hour),
		Caught: map[string]time.Time{"late": now, "early": now.Add(-time.Hour)}}
	e, comps := clubEmbed(c, now)
	if !strings.Contains(e.Description, "<@early> <@late>") || len(comps) != 1 {
		t.Errorf("board: %s %v", e.Description, comps)
	}
	if _, comps := clubEmbed(c, now.Add(2*time.Hour)); comps != nil {
		t.Error("an ended club has no buttons")
	}
}

func TestServerWatchTop(t *testing.T) {
	weeks := map[string]map[int]serverShow{}
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	addServerWatch(weeks, "a", appShow{AniListID: 1, Title: "One"}, 120, now)
	addServerWatch(weeks, "b", appShow{AniListID: 1, Title: "One"}, 120, now)
	addServerWatch(weeks, "a", appShow{AniListID: 2, Title: "Adult"}, 600, now)
	addServerWatch(weeks, "a", appShow{AniListID: 3, Title: "Three"}, 3600, now)
	for k := 1; k <= keepServerWeeks+2; k++ {
		addServerWatch(weeks, "a", appShow{AniListID: 1}, 60, now.AddDate(0, 0, 7*k))
	}
	if len(weeks) != keepServerWeeks {
		t.Errorf("%d weeks kept", len(weeks))
	}
	week := map[int]serverShow{
		1: {Title: "One", Seconds: 240, Viewers: []string{"a", "b"}},
		2: {Title: "Adult", Seconds: 600, Viewers: []string{"a"}},
		3: {Title: "Three", Seconds: 3600, Viewers: []string{"a"}},
	}
	lookup := func(id int) (airingShowInfo, bool) {
		var info airingShowInfo
		info.IsAdult = id == 2
		return info, true
	}
	e, n := serverTopView("Top", week, lookup)
	if n != 2 || strings.Contains(e.Description, "Adult") || !strings.HasPrefix(e.Description, "**1.** **Three**") || !strings.Contains(e.Description, "2 members") {
		t.Errorf("top (%d): %s", n, e.Description)
	}
}

func TestPickFeatured(t *testing.T) {
	shows := []featuredPick{{AniListID: 3}, {AniListID: 1}, {AniListID: 2}}
	a, _ := pickFeatured("2026-10-14", shows)
	b, _ := pickFeatured("2026-10-14", []featuredPick{shows[2], shows[0], shows[1]})
	if a.AniListID != b.AniListID || a.Day != "2026-10-14" {
		t.Errorf("order changed the pick: %+v %+v", a, b)
	}
	seen := map[int]bool{}
	for d := 1; d <= 20; d++ {
		p, _ := pickFeatured(fmt.Sprintf("2026-10-%02d", d), shows)
		seen[p.AniListID] = true
	}
	if len(seen) < 2 {
		t.Error("the pick never changes")
	}
	if _, ok := pickFeatured("2026-10-14", nil); ok {
		t.Error("nothing to pick from")
	}

	var w featuredWatch
	if addFeaturedWatch(&w, "d1", 300) || !addFeaturedWatch(&w, "d1", 300) || addFeaturedWatch(&w, "d1", 300) {
		t.Errorf("bonus once at 10 min: %+v", w)
	}
	if addFeaturedWatch(&w, "d2", 120) || w.Paid || w.Seconds != 120 {
		t.Errorf("a new day starts over: %+v", w)
	}
}

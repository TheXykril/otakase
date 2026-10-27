package main

import (
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Watch badges: heartbeats from linked apps add up to day streaks (days in
// a row with 10 minutes watched, by UTC day), binge nights (3 hours or 4
// episodes in a day) and seasons finished (the last episode of a shared
// show). A badge gives a few game points once and, when the server has a
// role of the same name ("7-Day Streak", "Binge Watcher"…), that role.
// Nothing is posted: badges show on /badges and /profile. The same
// counting keeps each member's year for /wrap (watchwrap.go).

const (
	streakDaySecs = 10 * 60  // a day counts for a streak from 10 minutes
	bingeSecs     = 3 * 3600 // a binge night: 3 hours in a day…
	bingeEps      = 4        // …or 4 episodes
	episodeSecs   = 5 * 60   // an episode counts once 5 minutes of it played
	keepFinished  = 100      // AniList ids of finished seasons kept
	keepWrapYears = 3        // years kept for /wrap
	keepWrapShows = 20       // shows kept per year
	badgeGame     = "badges" // game points key
	dayLayout     = "2006-01-02"
)

// watchStats is what a member's heartbeats add up to.
type watchStats struct {
	Day        string               `json:"day,omitempty"` // the UTC day the Day fields are for
	DaySecs    int64                `json:"day_secs,omitempty"`
	DayEps     int                  `json:"day_eps,omitempty"`
	Binged     bool                 `json:"binged,omitempty"` // today already counted as a binge night
	Ep         string               `json:"ep,omitempty"`     // the episode playing, "154587:12"
	EpSecs     int                  `json:"ep_secs,omitempty"`
	Streak     int                  `json:"streak,omitempty"`
	StreakDay  string               `json:"streak_day,omitempty"` // last day of the streak
	BestStreak int                  `json:"best_streak,omitempty"`
	Binges     int                  `json:"binges,omitempty"`
	Seasons    int                  `json:"seasons,omitempty"`
	Finished   []int                `json:"finished,omitempty"` // seasons finished, newest last
	Badges     map[string]time.Time `json:"badges,omitempty"`   // badge key → when earned
	Years      map[int]yearWrap     `json:"years,omitempty"`
}

// watchBadge is one badge and what earns it.
type watchBadge struct {
	Key    string
	Name   string // also the role it gives, when the server has one
	Emoji  string
	How    string
	Points int
	// Has earns the badge from heartbeats. Hidden badges have none: they
	// come some other way, and /badges keeps quiet about them until earned.
	Has func(st watchStats) bool
}

var watchBadges = []watchBadge{
	{"streak-3", "3-Day Streak", "🔥", "Watch 10 minutes a day, 3 days in a row", 3, func(st watchStats) bool { return st.BestStreak >= 3 }},
	{"streak-7", "7-Day Streak", "🔥", "Watch 10 minutes a day, 7 days in a row", 8, func(st watchStats) bool { return st.BestStreak >= 7 }},
	{"streak-30", "30-Day Streak", "☄️", "Watch 10 minutes a day, 30 days in a row", 20, func(st watchStats) bool { return st.BestStreak >= 30 }},
	{"binge", "Binge Watcher", "🍿", "Watch 3 hours, or 4 episodes, in one day", 5, func(st watchStats) bool { return st.Binges >= 1 }},
	{"finisher", "Season Finisher", "🏁", "Watch the last episode of a season", 5, func(st watchStats) bool { return st.Seasons >= 1 }},
	{"collector", "Season Collector", "📚", "Finish 10 seasons", 15, func(st watchStats) bool { return st.Seasons >= 10 }},
}

func badgeByKey(key string) (watchBadge, bool) {
	for _, bd := range watchBadges {
		if bd.Key == key {
			return bd, true
		}
	}
	return watchBadge{}, false
}

func init() {
	gameNames[badgeGame] = "Watch badges"
	onHeartbeat((*bot).badgeHeartbeat)
	registerCommand(&discordgo.ApplicationCommand{Name: "badges", Description: "Watch badges from Otakase: streaks, binge nights, seasons finished",
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionUser, Name: "member", Description: "Whose (default you)"},
		}}, (*bot).badgesCommand)
	registerHelp("`/badges` your watch badges from Otakase: day streaks, binge nights and seasons finished")
}

// addHeartbeat counts one heartbeat into a member's stats and returns the
// keys of the badges it just earned. show is nil when they don't share
// what they watch: the time still counts, episodes and shows don't.
func addHeartbeat(st *watchStats, show *appShow, secs int, now time.Time) []string {
	now = now.UTC()
	day := now.Format(dayLayout)
	if st.Day != day {
		st.Day, st.DaySecs, st.DayEps, st.Binged = day, 0, 0, false
	}
	if st.Years == nil {
		st.Years = map[int]yearWrap{}
	}
	w := st.Years[now.Year()]
	if secs > 0 {
		if st.DaySecs == 0 {
			w.Days++
		}
		before := st.DaySecs
		st.DaySecs += int64(secs)
		w.Seconds += int64(secs)
		if before < streakDaySecs && st.DaySecs >= streakDaySecs {
			switch st.StreakDay {
			case day:
			case now.AddDate(0, 0, -1).Format(dayLayout):
				st.Streak++
			default:
				st.Streak = 1
			}
			st.StreakDay = day
			st.BestStreak = max(st.BestStreak, st.Streak)
			w.Streak = max(w.Streak, st.Streak)
		}
	}
	if show != nil && secs > 0 {
		key := show.Title
		if show.AniListID > 0 {
			key = strconv.Itoa(show.AniListID)
		}
		key += ":" + strconv.Itoa(show.Episode)
		if key != st.Ep {
			st.Ep, st.EpSecs = key, 0
		}
		before := st.EpSecs
		st.EpSecs += secs
		if show.Episode > 0 && before < episodeSecs && st.EpSecs >= episodeSecs {
			st.DayEps++
			w.Episodes++
			if show.AniListID > 0 && show.Episodes >= 2 && show.Episode == show.Episodes && !containsInt(st.Finished, show.AniListID) {
				st.Seasons++
				st.Finished = append(st.Finished, show.AniListID)
				if len(st.Finished) > keepFinished {
					st.Finished = st.Finished[len(st.Finished)-keepFinished:]
				}
			}
		}
		if show.AniListID > 0 {
			addWrapShow(&w, show.AniListID, show.Title, int64(secs))
		}
	}
	if !st.Binged && (st.DaySecs >= bingeSecs || st.DayEps >= bingeEps) {
		st.Binged = true
		st.Binges++
	}
	st.Years[now.Year()] = w
	for y := range st.Years {
		if y <= now.Year()-keepWrapYears {
			delete(st.Years, y)
		}
	}
	var earned []string
	for _, bd := range watchBadges {
		if _, had := st.Badges[bd.Key]; !had && bd.Has != nil && bd.Has(*st) {
			if st.Badges == nil {
				st.Badges = map[string]time.Time{}
			}
			st.Badges[bd.Key] = now
			earned = append(earned, bd.Key)
		}
	}
	return earned
}

// currentStreak is the streak still going: its last day is today or
// yesterday.
func currentStreak(st watchStats, now time.Time) int {
	now = now.UTC()
	if st.StreakDay == now.Format(dayLayout) || st.StreakDay == now.AddDate(0, 0, -1).Format(dayLayout) {
		return st.Streak
	}
	return 0
}

// badgeHeartbeat counts a heartbeat and pays out new badges: points at
// once, the role (when there is one) in the background.
func (b *bot) badgeHeartbeat(ev heartbeatEvent) {
	var earned []string
	b.store.touch(func(d *storeData) {
		if d.WatchStats == nil {
			d.WatchStats = map[string]watchStats{}
		}
		st := d.WatchStats[ev.User]
		earned = addHeartbeat(&st, ev.Show, ev.Seconds, ev.At)
		d.WatchStats[ev.User] = st
		for _, k := range earned {
			if bd, ok := badgeByKey(k); ok {
				addGamePoints(d, ev.User, badgeGame, bd.Points, weekKey(ev.At.UTC()))
			}
		}
	})
	if len(earned) > 0 && b.s != nil {
		go b.giveBadgeRoles(ev.User, earned)
	}
}

// giveBadgeRoles gives the roles named like the badges, where the server
// has them. Missing roles are fine: the badge is enough.
func (b *bot) giveBadgeRoles(user string, keys []string) {
	g, err := b.s.State.Guild(b.cfg.GuildID)
	if err != nil {
		return
	}
	for _, k := range keys {
		bd, _ := badgeByKey(k)
		for _, r := range g.Roles {
			if strings.EqualFold(strings.TrimSpace(r.Name), bd.Name) {
				if err := b.s.GuildMemberRoleAdd(b.cfg.GuildID, user, r.ID); err != nil {
					log.Printf("badge role %s: %v", bd.Name, err)
				}
				break
			}
		}
	}
}

func (b *bot) watchStatsOf(user string) (watchStats, bool) {
	var st watchStats
	var ok bool
	b.store.view(func(d *storeData) {
		st, ok = d.WatchStats[user]
		st = st.clone()
	})
	return st, ok
}

// clone copies the maps, for reading outside the store's lock.
func (st watchStats) clone() watchStats {
	badges := make(map[string]time.Time, len(st.Badges))
	for k, v := range st.Badges {
		badges[k] = v
	}
	years := make(map[int]yearWrap, len(st.Years))
	for y, w := range st.Years {
		shows := make(map[int]wrapShow, len(w.Shows))
		for id, s := range w.Shows {
			shows[id] = s
		}
		w.Shows = shows
		years[y] = w
	}
	st.Badges, st.Years = badges, years
	st.Finished = append([]int(nil), st.Finished...)
	return st
}

func (b *bot) badgesCommand(i *discordgo.InteractionCreate) {
	who := i.Member.User
	if o, ok := optionMap(i)["member"]; ok {
		who = optUser(i, o)
	}
	st, _ := b.watchStatsOf(who.ID)
	b.respondQuiet(i, onlyAsker(i), badgesView(who.ID, who.ID == i.Member.User.ID, st, appNow()))
}

// badgesView lists the badges earned with when, the ones still to get, and
// the counts behind them.
func badgesView(user string, self bool, st watchStats, now time.Time) *discordgo.MessageEmbed {
	var have, todo []string
	for _, bd := range watchBadges {
		if at, ok := st.Badges[bd.Key]; ok {
			have = append(have, fmt.Sprintf("%s **%s** · <t:%d:d>", bd.Emoji, bd.Name, at.Unix()))
		} else if bd.Has != nil {
			todo = append(todo, fmt.Sprintf("%s %s: %s", bd.Emoji, bd.Name, bd.How))
		}
	}
	whose := fmt.Sprintf("<@%s> has", user)
	if self {
		whose = "You have"
	}
	desc := whose + " no watch badges yet."
	if len(have) > 0 {
		desc = strings.Join(have, "\n")
	}
	e := &discordgo.MessageEmbed{Title: "Watch badges", Color: shu, Description: desc,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Streak", Value: fmt.Sprintf("%s now · best %d", dashPlural(currentStreak(st, now), "day"), st.BestStreak), Inline: true},
			{Name: "Binge nights", Value: strconv.Itoa(st.Binges), Inline: true},
			{Name: "Seasons finished", Value: strconv.Itoa(st.Seasons), Inline: true},
		},
		Footer: &discordgo.MessageEmbedFooter{Text: "From the linked Otakase app · episodes and seasons count with Share what I watch on"}}
	if len(todo) > 0 {
		e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: "Still to earn", Value: strings.Join(todo, "\n")})
	}
	return e
}

// badgeLine is the /profile line, "🔥 7-Day Streak · 🍿 Binge Watcher",
// newest first; "" without badges. A lower streak badge is left out when a
// higher one is there.
func badgeLine(st watchStats) string {
	type got struct {
		bd watchBadge
		at time.Time
	}
	var list []got
	for _, bd := range watchBadges {
		if at, ok := st.Badges[bd.Key]; ok {
			list = append(list, got{bd, at})
		}
	}
	skip := map[string]bool{}
	for _, g := range list {
		switch g.bd.Key {
		case "streak-30":
			skip["streak-7"], skip["streak-3"] = true, true
		case "streak-7":
			skip["streak-3"] = true
		case "collector":
			skip["finisher"] = true
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].at.After(list[j].at) })
	var parts []string
	for _, g := range list {
		if !skip[g.bd.Key] {
			parts = append(parts, g.bd.Emoji+" "+g.bd.Name)
		}
	}
	return strings.Join(parts, " · ")
}

// profileWatchLine is the small line under the /profile card: the
// member's badges and, in December, their year wrap. "" when there is
// nothing to say.
func (b *bot) profileWatchLine(user string, now time.Time) string {
	st, ok := b.watchStatsOf(user)
	if !ok {
		return ""
	}
	var lines []string
	if l := badgeLine(st); l != "" {
		lines = append(lines, "-# "+l)
	}
	if now.UTC().Month() == time.December {
		if w, ok := st.Years[now.UTC().Year()]; ok && w.Seconds >= 3600 {
			lines = append(lines, fmt.Sprintf("-# Their %d in Otakase is wrapped: `/wrap`", now.UTC().Year()))
		}
	}
	return strings.Join(lines, "\n")
}

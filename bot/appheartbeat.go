package main

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Heartbeats from linked apps: once at start, every minute while an episode
// plays and once when it stops. They keep the app's version and system up
// to date, add playing time to the member's Otakase watch time (/watch-time
// and the /profile card), show what they watch in /whos-watching when they
// opted in, and give the Tester role to members running a development
// build.

const (
	watchingTTL      = 3 * time.Minute // dropped from /whos-watching without a heartbeat
	maxWatching      = 1000
	maxHeartbeatSecs = 120
	keepWatchWeeks   = 12
)

var heartbeatLimit = newRateLimit(4, time.Minute)

// heartbeatEvent is one heartbeat as the features listening to them see it.
type heartbeatEvent struct {
	User     string // Discord user id
	Hash     string // the app's token hash
	Version  string // "" when it didn't look like one
	OS       string
	DevBuild bool
	Show     *appShow // what plays, only when the member shares it
	Seconds  int      // watched since the last heartbeat, clamped
	At       time.Time
}

// heartbeatHooks run on every accepted heartbeat, in the request: keep them
// quick (store.touch, not update) and start a goroutine for anything that
// talks to Discord.
var heartbeatHooks []func(b *bot, ev heartbeatEvent)

// onHeartbeat adds a heartbeat listener; call it from init().
func onHeartbeat(fn func(b *bot, ev heartbeatEvent)) { heartbeatHooks = append(heartbeatHooks, fn) }

type heartbeatReq struct {
	Version        string   `json:"version"`
	DevBuild       bool     `json:"dev_build"`
	OS             string   `json:"os"`
	ShareWatching  bool     `json:"share_watching"`
	Watching       *appShow `json:"watching"`
	WatchedSeconds int      `json:"watched_seconds"`
}

// appShow is the episode the app plays.
type appShow struct {
	AniListID int    `json:"anilist_id"`
	Title     string `json:"title"`
	Episode   int    `json:"episode"`
	Episodes  int    `json:"episodes"`
}

// appStats is a member's Otakase watch time and whether they got the
// Tester role.
type appStats struct {
	Seconds int64            `json:"seconds,omitempty"`
	Weeks   map[string]int64 `json:"weeks,omitempty"` // by ISO week, "2026-W41"
	Tester  bool             `json:"tester,omitempty"`
}

// appReport is a support post made from the app.
type appReport struct {
	User string    `json:"user"`
	At   time.Time `json:"at"`
}

// nowWatching is a member watching something in Otakase right now.
type nowWatching struct {
	User  string
	Show  appShow
	Since time.Time // first heartbeat on this episode
	At    time.Time // latest heartbeat
}

var watchingNow = struct {
	sync.Mutex
	m map[string]nowWatching
}{m: map[string]nowWatching{}}

func init() {
	apiHandle("POST /v1/heartbeat", (*bot).apiHeartbeat)
}

var versionTextRe = regexp.MustCompile(`^v?(\d{1,4}\.\d{1,4}\.\d{1,4}(?:[-+][0-9A-Za-z.+\-]{1,30})?)$`)

// cleanVersion is the app's version when it looks like one, else "".
func cleanVersion(v string) string {
	if m := versionTextRe.FindStringSubmatch(strings.TrimSpace(v)); m != nil {
		return m[1]
	}
	return ""
}

// cleanOS keeps the systems Go names that Otakase runs on.
func cleanOS(s string) string {
	switch s = strings.ToLower(strings.TrimSpace(s)); s {
	case "linux", "windows", "darwin", "android", "freebsd", "openbsd", "netbsd":
		return s
	case "":
		return ""
	}
	return "other"
}

// cleanShow checks the episode the app reports; nil when there is nothing
// usable.
func cleanShow(s *appShow) *appShow {
	if s == nil {
		return nil
	}
	c := appShow{AniListID: s.AniListID, Title: clip(cleanText(s.Title, false), 100), Episode: s.Episode, Episodes: s.Episodes}
	if c.AniListID < 0 || c.AniListID > 10_000_000 {
		c.AniListID = 0
	}
	if c.Episode < 0 || c.Episode > 100_000 {
		c.Episode = 0
	}
	if c.Episodes < 0 || c.Episodes > 100_000 {
		c.Episodes = 0
	}
	if c.AniListID == 0 && c.Title == "" {
		return nil
	}
	return &c
}

func isoWeekKey(t time.Time) string {
	y, w := t.UTC().ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// addWatchTime adds playing seconds to the total and this week, keeping the
// last few weeks.
func addWatchTime(st *appStats, secs int, now time.Time) {
	if secs <= 0 {
		return
	}
	st.Seconds += int64(secs)
	if st.Weeks == nil {
		st.Weeks = map[string]int64{}
	}
	st.Weeks[isoWeekKey(now)] += int64(secs)
	if len(st.Weeks) > keepWatchWeeks {
		keys := make([]string, 0, len(st.Weeks))
		for k := range st.Weeks {
			keys = append(keys, k)
		}
		sort.Strings(keys) // "2025-W52" < "2026-W01"
		for _, k := range keys[:len(keys)-keepWatchWeeks] {
			delete(st.Weeks, k)
		}
	}
}

// appActivityDirty is set when heartbeats changed the store without saving.
var appActivityDirty atomic.Bool

// touch changes the data without writing the file; appActivityLoop (or
// any other write) saves it. Heartbeats come every minute per app, too
// often to rewrite the file each time.
func (st *store) touch(fn func(d *storeData)) {
	st.mu.Lock()
	defer st.mu.Unlock()
	fn(&st.data)
	appActivityDirty.Store(true)
}

// appActivityLoop saves heartbeat changes every few minutes.
func (b *bot) appActivityLoop() {
	for {
		time.Sleep(5 * time.Minute)
		if appActivityDirty.Swap(false) {
			b.store.update(func(*storeData) {})
		}
	}
}

func (b *bot) apiHeartbeat(w http.ResponseWriter, r *http.Request) {
	user := b.requireApp(w, r)
	if user == "" {
		return
	}
	h := hashToken(bearerToken(r))
	now := appNow()
	if !heartbeatLimit.allow(h, now) {
		apiError(w, http.StatusTooManyRequests, "too many heartbeats")
		return
	}
	var hb heartbeatReq
	if !readJSON(w, r, 4096, &hb) {
		return
	}
	name, gone := b.appMemberName(user)
	if gone {
		b.unlinkAll(user)
		apiError(w, http.StatusUnauthorized, "you are no longer in the Otakase Discord")
		return
	}
	secs := min(max(hb.WatchedSeconds, 0), maxHeartbeatSecs)
	version, goos := cleanVersion(hb.Version), cleanOS(hb.OS)
	linked := false
	b.store.touch(func(d *storeData) {
		l, ok := d.AppLinks[h]
		if !ok {
			return // unlinked meanwhile
		}
		linked = true
		l.LastSeen = now
		if version != "" {
			l.Version = version
		}
		if goos != "" {
			l.OS = goos
		}
		d.AppLinks[h] = l
		if secs > 0 {
			st := d.AppStats[user]
			addWatchTime(&st, secs, now)
			d.AppStats[user] = st
		}
	})
	if !linked {
		apiError(w, http.StatusUnauthorized, "link this app with otakase -discord-link first")
		return
	}
	show := cleanShow(hb.Watching)
	setWatching(user, hb.ShareWatching, show, now)
	if hb.DevBuild {
		go b.giveTester(user, version, goos)
	}
	if !hb.ShareWatching {
		show = nil
	}
	ev := heartbeatEvent{User: user, Hash: h, Version: version, OS: goos, DevBuild: hb.DevBuild, Show: show, Seconds: secs, At: now}
	for _, fn := range heartbeatHooks {
		fn(b, ev)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "discord_name": name, "inbox": b.inboxCount(h)})
}

// appMemberName is the member's server name; gone when Discord says they
// left the server.
func (b *bot) appMemberName(user string) (name string, gone bool) {
	if b.s == nil || b.s.State == nil {
		return "", false
	}
	if m, err := b.s.State.Member(b.cfg.GuildID, user); err == nil && m.User != nil {
		return displayName(m), false
	}
	m, err := b.s.GuildMember(b.cfg.GuildID, user)
	if err != nil {
		return "", unknownMember(err)
	}
	m.GuildID = b.cfg.GuildID
	b.s.State.MemberAdd(m)
	return displayName(m), false
}

// unknownMember reports whether Discord said the member is not in the server.
func unknownMember(err error) bool {
	rest, ok := err.(*discordgo.RESTError)
	return ok && rest.Message != nil && rest.Message.Code == discordgo.ErrCodeUnknownMember
}

// setWatching notes or clears what a member watches now.
func setWatching(user string, share bool, show *appShow, now time.Time) {
	watchingNow.Lock()
	defer watchingNow.Unlock()
	pruneWatching(now)
	if !share || show == nil {
		delete(watchingNow.m, user)
		return
	}
	prev, had := watchingNow.m[user]
	if !had && len(watchingNow.m) >= maxWatching {
		return
	}
	e := nowWatching{User: user, Show: *show, Since: now, At: now}
	if had && prev.Show.AniListID == show.AniListID && prev.Show.Title == show.Title && prev.Show.Episode == show.Episode {
		e.Since = prev.Since
	}
	watchingNow.m[user] = e
}

func pruneWatching(now time.Time) {
	for u, e := range watchingNow.m {
		if now.Sub(e.At) > watchingTTL {
			delete(watchingNow.m, u)
		}
	}
}

// watchingList is who watches what now, longest watching first.
func watchingList(now time.Time) []nowWatching {
	watchingNow.Lock()
	defer watchingNow.Unlock()
	pruneWatching(now)
	out := make([]nowWatching, 0, len(watchingNow.m))
	for _, e := range watchingNow.m {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Since.Equal(out[j].Since) {
			return out[i].Since.Before(out[j].Since)
		}
		return out[i].User < out[j].User
	})
	return out
}

// Tester role.

var testerRole = struct {
	sync.Mutex
	id string
	at time.Time
}{}

// testerRoleID finds the "Tester" role by name, looked up again every ten
// minutes so a role made later is found.
func (b *bot) testerRoleID() string {
	testerRole.Lock()
	defer testerRole.Unlock()
	if time.Since(testerRole.at) < 10*time.Minute {
		return testerRole.id
	}
	roles, err := b.s.GuildRoles(b.cfg.GuildID)
	if err != nil {
		log.Printf("listing roles: %v", err)
		return testerRole.id
	}
	testerRole.id, testerRole.at = "", time.Now()
	for _, r := range roles {
		if strings.EqualFold(r.Name, "Tester") {
			testerRole.id = r.ID
		}
	}
	return testerRole.id
}

var (
	testerMu    sync.Mutex
	testerTried = map[string]time.Time{} // failed role adds, not retried for an hour
)

// giveTester gives a member running a development build the Tester role,
// once: staff can take it away again and it stays away.
func (b *bot) giveTester(user, version, goos string) {
	testerMu.Lock()
	defer testerMu.Unlock()
	var given bool
	b.store.view(func(d *storeData) { given = d.AppStats[user].Tester })
	if given || b.s == nil || time.Since(testerTried[user]) < time.Hour {
		return
	}
	role := b.testerRoleID()
	if role == "" {
		return
	}
	if err := b.s.GuildMemberRoleAdd(b.cfg.GuildID, user, role); err != nil {
		log.Printf("tester role: %v", err)
		if len(testerTried) > 1000 {
			clear(testerTried)
		}
		testerTried[user] = time.Now()
		return
	}
	b.store.update(func(d *storeData) {
		st := d.AppStats[user]
		st.Tester = true
		d.AppStats[user] = st
	})
	if version == "" {
		version = "unknown version"
	}
	b.staffNote(&discordgo.MessageEmbed{Title: "Tester role", Color: shu,
		Description: fmt.Sprintf("Gave <@%s> the Tester role: they run a development build of Otakase (%s on %s).", user, version, osLabel(goos))})
}

// Watch time.

// watchDuration writes seconds as "3 h 20 min", "45 min" or "0 min".
func watchDuration(secs int64) string {
	h, m := secs/3600, secs%3600/60
	switch {
	case h == 0:
		return fmt.Sprintf("%d min", m)
	case m == 0:
		return fmt.Sprintf("%d h", h)
	}
	return fmt.Sprintf("%d h %d min", h, m)
}

// watchHours writes seconds as whole hours for the profile card, or
// minutes under an hour.
func watchHours(secs int64) string {
	if secs < 3600 {
		return fmt.Sprintf("%d min", secs/60)
	}
	return fmt.Sprintf("%d h", secs/3600)
}

func (b *bot) appStatsOf(user string) appStats {
	var st appStats
	b.store.view(func(d *storeData) {
		st = d.AppStats[user]
		weeks := make(map[string]int64, len(st.Weeks))
		for k, v := range st.Weeks {
			weeks[k] = v
		}
		st.Weeks = weeks
	})
	return st
}

// otakaseWatched is the "42 h" on the /profile card, "" when they never
// watched with a linked app.
func (b *bot) otakaseWatched(user string) string {
	if st := b.appStatsOf(user); st.Seconds >= 60 {
		return watchHours(st.Seconds)
	}
	return ""
}

func (b *bot) watchTimeEmbed(user string, self bool) *discordgo.MessageEmbed {
	return watchTimeView(user, self, b.appStatsOf(user), appNow())
}

func watchTimeView(user string, self bool, st appStats, now time.Time) *discordgo.MessageEmbed {
	if st.Seconds < 60 {
		if self {
			return errorEmbed("Nothing yet. Link Otakase with `otakase -discord-link` and what you play counts here.")
		}
		return errorEmbed("<@%s> hasn't watched anything in Otakase with a linked app yet.", user)
	}
	lines := []string{fmt.Sprintf("<@%s> watched **%s** in Otakase.", user, watchDuration(st.Seconds)), ""}
	labels := []string{"This week", "Last week"}
	for k := 0; k < 4; k++ {
		label := fmt.Sprintf("%d weeks ago", k)
		if k < len(labels) {
			label = labels[k]
		}
		lines = append(lines, fmt.Sprintf("%s: %s", label, watchDuration(st.Weeks[isoWeekKey(now.AddDate(0, 0, -7*k))])))
	}
	return &discordgo.MessageEmbed{Title: "Watch time", Color: shu, Description: strings.Join(lines, "\n"),
		Footer: &discordgo.MessageEmbedFooter{Text: "Counted by the Otakase app while an episode plays"}}
}

// Who's watching.

// showInfo looks up a show on AniList for /whos-watching, cached for six
// hours; ok is false when it couldn't be found.
var showInfoCache = struct {
	sync.Mutex
	m map[int]showInfoEntry
}{m: map[int]showInfoEntry{}}

type showInfoEntry struct {
	info airingShowInfo
	ok   bool
	at   time.Time
}

// appShowInfo is variable so tests skip AniList.
var appShowInfo = func(id int) (airingShowInfo, bool) {
	showInfoCache.Lock()
	e, hit := showInfoCache.m[id]
	showInfoCache.Unlock()
	if hit && time.Since(e.at) < 6*time.Hour {
		return e.info, e.ok
	}
	var r struct {
		Data struct {
			Media *airingShowInfo `json:"Media"`
		} `json:"data"`
	}
	err := anilistPost("", airingShowQuery, map[string]any{"id": id}, &r)
	if err != nil {
		log.Printf("anilist show %d: %v", id, err)
		return airingShowInfo{}, false // not cached: try again next time
	}
	e = showInfoEntry{at: time.Now()}
	if r.Data.Media != nil {
		e.info, e.ok = *r.Data.Media, true
	}
	showInfoCache.Lock()
	if len(showInfoCache.m) > 500 {
		showInfoCache.m = map[int]showInfoEntry{}
	}
	showInfoCache.m[id] = e
	showInfoCache.Unlock()
	return e.info, e.ok
}

// whosWatchingView lists who watches what, adult titles left out as
// everywhere else in the bot, with the cover of the most watched show.
func whosWatchingView(list []nowWatching, lookup func(int) (airingShowInfo, bool)) *discordgo.MessageEmbed {
	infos := map[int]airingShowInfo{}
	found := map[int]bool{}
	var lines []string
	counts := map[int]int{}
	for _, e := range list {
		id := e.Show.AniListID
		if id > 0 {
			if _, done := found[id]; !done && len(found) < 15 {
				infos[id], found[id] = lookup(id)
			}
		}
		info, ok := infos[id], found[id]
		if ok && info.IsAdult {
			continue
		}
		title := e.Show.Title
		if ok {
			if title = info.Title.English; title == "" {
				title = info.Title.Romaji
			}
		}
		if title == "" {
			continue
		}
		title = "**" + showText(clip(title, 60)) + "**"
		if ok && info.SiteURL != "" {
			title = fmt.Sprintf("[%s](%s)", strings.ReplaceAll(title, "]", ")"), info.SiteURL)
		}
		ep := ""
		if e.Show.Episode > 0 {
			ep = fmt.Sprintf(" · ep %d", e.Show.Episode)
			if e.Show.Episodes > 0 {
				ep += fmt.Sprintf("/%d", e.Show.Episodes)
			}
		}
		if id > 0 && ok {
			counts[id]++
		}
		if len(lines) < 20 {
			lines = append(lines, fmt.Sprintf("<@%s> %s%s · since <t:%d:R>", e.User, title, ep, e.Since.Unix()))
		}
	}
	if len(lines) == 0 {
		return &discordgo.MessageEmbed{Color: shu, Description: "Nobody is sharing what they watch right now.\n" +
			"Turn on **Share what I watch** in Otakase's Discord settings to show up here."}
	}
	e := &discordgo.MessageEmbed{Title: "Watching in Otakase now", Color: shu, Description: strings.Join(lines, "\n"),
		Footer: &discordgo.MessageEmbedFooter{Text: "Only members who turned on Share what I watch"}}
	best, bestN := 0, 0
	for id, n := range counts {
		if n > bestN || (n == bestN && id < best) {
			best, bestN = id, n
		}
	}
	if best > 0 && infos[best].CoverImage.ExtraLarge != "" {
		e.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: infos[best].CoverImage.ExtraLarge}
	}
	return e
}

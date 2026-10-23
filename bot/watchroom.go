package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// The watch room: the bot hosts an anime episode inside the games Activity
// and everyone in the call watches it in step. Opening one takes a watch
// ticket (from /redeem codes), spent once the first episode plays; staff
// need one too. Only one room runs at a time across the server, since the
// bot fetches and relays the video itself (watchrelay.go); members' own
// /watchparty parties are separate and unaffected.
//
// The member who opened it hosts: they pick the show and episode and
// control playback. Everyone else follows the host's position and can ask
// the host to pause, play, skip the opening or go to the next episode.
// Staff can take control. When the host leaves, the member who has been in
// the room longest takes over. A room lasts four hours from the first
// episode, as many episodes as fit; 18+ shows only in age-restricted voice
// channels. Members with a linked AniList (proven) or MAL account get their
// progress moved on once they've been in the room for most of an episode.
//
// Pages long-poll GET api/watch/state like the party rooms do; the host's
// page reports its position every few seconds, and that is the clock the
// others follow.

const (
	watchRoomLength = 4 * time.Hour
	watchGone       = 45 * time.Second // a member who hasn't polled this long has left
	watchEmptyClose = time.Minute      // an empty room closes after this
	watchPickWait   = 15 * time.Minute // a room that never played closes after this
	watchAskTTL     = 30 * time.Second
	watchPollWait   = 25 * time.Second
	watchTick       = 2 * time.Second
	watchDrift      = 2.0 // seconds the host may be off before everyone is told
	watchSkipOP     = 85  // seconds a skip of the opening jumps
	watchLogKeep    = 20
)

// watchNow is the clock, swapped in tests.
var watchNow = time.Now

// watchChannel finds a voice channel, and watchClock starts a room's
// clock; tests swap both.
var (
	watchChannel = func(b *bot, id string) *discordgo.Channel {
		if b.s == nil || id == "" {
			return nil
		}
		return b.channel(id)
	}
	watchClock = func(b *bot, r *watchRoom) { go b.watchLoop(r) }
	watchAhead = func(fn func()) { go fn() }
)

type watchMember struct {
	name    string
	joined  time.Time
	seen    time.Time
	watched float64 // seconds of this episode they were in the room while it played
	counted bool    // their lists were moved on for this episode
}

type watchAsk struct {
	ID   int       `json:"id"`
	User string    `json:"user"`
	Name string    `json:"name"`
	Kind string    `json:"kind"` // pause, play, skip (the opening), next
	At   time.Time `json:"-"`
}

type watchSub struct {
	Label string `json:"label"`
	Lang  string `json:"lang,omitempty"`
	URL   string `json:"url"`
}

type watchRoom struct {
	id      string // the Activity instance
	channel string
	adult   bool // age-restricted channel: 18+ shows allowed
	owner   string
	host    string
	opened  time.Time
	started time.Time // first episode played; zero before
	spent   bool      // the owner's ticket is used
	members map[string]*watchMember

	show    *watchShow
	ep      int
	dub     bool
	phase   string // pick, loading, play, error, over
	errText   string
	loadStep  string // current episode preparation step
	loadSince time.Time
	gen       int // bumps with every episode load, so a slow one can't land late

	streams       []streamLink
	link          int // index into streams
	torrent       *watchTorrent
	torrentCancel context.CancelFunc
	media         *watchMedia
	old           *watchMedia // the episode before, still served for a moment
	entry         string
	audioEntries map[string]string
	alternateMedia *watchMedia
	subs          []watchSub

	playing bool
	pos     float64
	at      time.Time
	dur     float64

	asks []watchAsk
	askN int

	ver  int
	wake chan struct{}
	tick time.Time

	bytesIn, bytesOut int64 // of finished episodes
	peak              int
	episodes          int
}

// watchRoomLog is a finished room, kept to learn what rooms cost.
type watchRoomLog struct {
	Owner    string    `json:"owner"`
	Started  time.Time `json:"started"`
	Ended    time.Time `json:"ended"`
	Shows    string    `json:"shows,omitempty"`
	Episodes int       `json:"episodes"`
	Peak     int       `json:"peak"`
	InMB     int64     `json:"in_mb"`
	OutMB    int64     `json:"out_mb"`
}

type watchRooms struct {
	mu    sync.Mutex
	cur   *watchRoom
	cache *segCache
	infos watchInfos
}

// changed bumps the version and wakes the waiting pages. Hold the lock.
func (r *watchRoom) changed() {
	r.ver++
	if r.wake != nil {
		close(r.wake)
	}
	r.wake = make(chan struct{})
}

// position is where the episode is now. Hold the lock.
func (r *watchRoom) position(now time.Time) float64 {
	if !r.playing || r.at.IsZero() {
		return r.pos
	}
	p := r.pos + now.Sub(r.at).Seconds()
	if r.dur > 0 {
		p = math.Min(p, r.dur)
	}
	return p
}

// setPos moves the clock. Hold the lock.
func (r *watchRoom) setPos(p float64, playing bool, now time.Time) {
	r.pos, r.playing, r.at = math.Max(0, p), playing, now
}

// people is the members, longest in the room first. Hold the lock.
func (r *watchRoom) people() []string {
	ids := make([]string, 0, len(r.members))
	for id := range r.members {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(a, b int) bool {
		ja, jb := r.members[ids[a]].joined, r.members[ids[b]].joined
		if !ja.Equal(jb) {
			return ja.Before(jb)
		}
		return ids[a] < ids[b]
	})
	return ids
}

// prune drops members who stopped polling and hands the room on when the
// host is gone. Hold the lock.
func (r *watchRoom) prune(now time.Time) {
	gone := false
	for id, m := range r.members {
		if now.Sub(m.seen) > watchGone {
			delete(r.members, id)
			gone = true
		}
	}
	if _, ok := r.members[r.host]; !ok {
		if ids := r.people(); len(ids) > 0 {
			r.host = ids[0]
			gone = true
		}
	}
	if gone {
		r.changed()
	}
}

func (b *bot) watchCache() *segCache {
	if b.watch.cache == nil {
		b.watch.cache = newSegCache(watchCacheBytes)
	}
	return b.watch.cache
}

// ── The room's clock ──

// watchLoop runs while a room is open: it counts who watched what, expires
// requests and closes the room when its time is up or everyone left.
func (b *bot) watchLoop(r *watchRoom) {
	t := time.NewTicker(watchTick)
	defer t.Stop()
	for range t.C {
		if !b.watchStep(r, watchNow()) {
			return
		}
	}
}

// watchStep is one tick. False once the room is closed.
func (b *bot) watchStep(r *watchRoom, now time.Time) bool {
	b.watch.mu.Lock()
	if b.watch.cur != r {
		b.watch.mu.Unlock()
		return false
	}
	r.prune(now)
	elapsed := now.Sub(r.tick).Seconds()
	if r.tick.IsZero() || elapsed > 3*watchTick.Seconds() {
		elapsed = 0
	}
	r.tick = now
	r.peak = max(r.peak, len(r.members))

	var count []string
	if r.phase == "play" && r.playing && r.dur > 0 {
		for id, m := range r.members {
			m.watched += elapsed
			if !m.counted && m.watched >= watchedEnough*r.dur {
				m.counted = true
				count = append(count, id)
			}
		}
	}
	kept := r.asks[:0]
	for _, a := range r.asks {
		if now.Sub(a.At) < watchAskTTL {
			kept = append(kept, a)
		}
	}
	if len(kept) != len(r.asks) {
		r.asks = kept
		r.changed()
	} else {
		r.asks = kept
	}

	closeWhy := ""
	switch {
	case !r.started.IsZero() && now.After(r.started.Add(watchRoomLength)):
		closeWhy = "time"
	case len(r.members) == 0 && now.Sub(r.lastSeen()) > watchEmptyClose:
		closeWhy = "empty"
	case r.started.IsZero() && now.Sub(r.opened) > watchPickWait:
		closeWhy = "unused"
	}
	var show watchShow
	if r.show != nil {
		show = *r.show
	}
	ep := r.ep
	if closeWhy != "" {
		b.closeWatchRoom(r, now)
	}
	b.watch.mu.Unlock()

	if len(count) > 0 {
		go b.countWatched(b.watchSyncers(count), show, ep)
	}
	return closeWhy == ""
}

// lastSeen is when the room last had anyone in it. Hold the lock.
func (r *watchRoom) lastSeen() time.Time {
	last := r.opened
	for _, m := range r.members {
		if m.seen.After(last) {
			last = m.seen
		}
	}
	if r.tick.After(last) && len(r.members) > 0 {
		last = r.tick
	}
	return last
}

// watchSyncers is the members who let the room update their lists.
func (b *bot) watchSyncers(users []string) []string {
	var out []string
	b.store.view(func(d *storeData) {
		for _, u := range users {
			if !d.WatchNoSync[u] {
				out = append(out, u)
			}
		}
	})
	return out
}

// closeWatchRoom ends the room and logs what it cost. Hold the lock.
func (b *bot) closeWatchRoom(r *watchRoom, now time.Time) {
	r.phase = "over"
	r.finishMedia()
	r.changed()
	if b.watch.cur == r {
		b.watch.cur = nil
	}
	if r.started.IsZero() {
		return
	}
	entry := watchRoomLog{Owner: r.owner, Started: r.started, Ended: now, Episodes: r.episodes, Peak: r.peak,
		InMB: r.bytesIn >> 20, OutMB: r.bytesOut >> 20}
	if r.show != nil {
		entry.Shows = r.show.Title
	}
	log.Printf("watch room over: %d episodes, %d people at most, %d MB in, %d MB out", entry.Episodes, entry.Peak, entry.InMB, entry.OutMB)
	go b.store.update(func(d *storeData) {
		d.WatchRooms = append(d.WatchRooms, entry)
		if len(d.WatchRooms) > watchLogKeep {
			d.WatchRooms = d.WatchRooms[len(d.WatchRooms)-watchLogKeep:]
		}
	})
}

// finishMedia adds the episode's traffic to the room's and drops it. Hold
// the lock.
func (r *watchRoom) finishMedia() {
	if r.torrentCancel != nil {
		r.torrentCancel()
		r.torrentCancel = nil
	}
	if r.torrent != nil {
		go r.torrent.stop()
		r.torrent = nil
	}
	if r.media != nil {
		r.bytesIn += r.media.in.Load()
		r.bytesOut += r.media.out.Load()
	}
	r.old, r.media = r.media, nil
	r.alternateMedia = nil
}

// ── Episodes ──

// loadEpisode looks the episode up and starts it. Hold the lock; the
// lookup runs on its own and lands only if nothing was picked since.
func (b *bot) loadEpisode(r *watchRoom, show watchShow, ep int, dub bool) {
	r.show, r.ep, r.dub = &show, ep, dub
	r.phase, r.errText = "loading", ""
	r.loadStep, r.loadSince = "Checking catalog for episode", time.Now()
	r.finishMedia()
	r.streams, r.link, r.subs, r.entry, r.audioEntries = nil, 0, nil, "", nil
	r.setPos(0, false, watchNow())
	r.dur = 0
	r.asks = nil
	for _, m := range r.members {
		m.watched, m.counted = 0, false
	}
	r.gen++
	gen, adult, cli := r.gen, r.adult && show.Adult, b.cfg.OtakaseCLI
	r.changed()
	ctx, cancel := context.WithCancel(context.Background())
	r.torrentCancel = cancel
	go func() {
		info, err := b.watch.infos.get(ctx, cli, show.ID, ep, dub, adult)
		if err == nil && len(info.Streams) == 0 && len(info.Torrents) > 0 {
			b.watch.mu.Lock()
			if b.watch.cur == r && r.gen == gen {
				r.loadStep = "Connecting to playback relay"
				r.changed()
			}
			b.watch.mu.Unlock()
		}
		var torrent *watchTorrent
		if err == nil && len(info.Streams) == 0 && len(info.Torrents) > 0 {
			var link string
			for _, candidate := range info.Torrents {
				torrent, link, err = watchStartTorrent(ctx, cli, candidate.InfoHash, info.Mode)
				if err == nil || ctx.Err() != nil {
					break
				}
			}
			if err == nil {
				media := streamLink{URL: link}
				if parsed, parseErr := url.Parse(link); parseErr == nil && parsed.Query().Get("subtitles") == "en" {
					media.Subtitles = []streamSubtitle{{URL: fmt.Sprintf("http://%s/subs/en.vtt", parsed.Host), Language: "en", Label: "English"}}
				}
				info.Streams = []streamLink{media}
			}
		}
		b.watch.mu.Lock()
		defer b.watch.mu.Unlock()
		if b.watch.cur != r || r.gen != gen {
			cancel()
			if torrent != nil {
				go torrent.stop()
			}
			return
		}
		if err != nil {
			cancel()
			code := recordWatchFailure(show.Title, ep, r.loadStep, err)
			r.phase, r.errText = "error", fmt.Sprintf("Episode %d: %s. Report code %s to the moderators.", ep, publicWatchError(err), code)
			r.changed()
			return
		}
		r.streams = info.Streams
		// Shown as it plays: a show without a dub plays subbed.
		r.dub = info.Mode == "dub"
		r.torrentCancel = nil
		if !b.startStream(r, 0) {
			cancel()
			if torrent != nil {
				go torrent.stop()
			}
			return
		}
		r.torrent = torrent
		r.torrentCancel = cancel
        // Some providers expose Sub and Dub as separate releases rather than
        // multiple audio tracks. Look up the other version without delaying
        // the first viewer; only reuse directly playable links here.
        if r.audioEntries == nil {
            go b.findWatchAlternate(ctx, r, gen, cli, show.ID, ep, adult, !r.dub)
        }
		// The next episode is looked up ahead, so it starts at once.
		if ep < show.lastEpisode() {
			watchAhead(func() { b.watch.infos.get(context.Background(), cli, show.ID, ep+1, dub, adult) })
		}
	}()
}


// findWatchAlternate makes separately published audio versions selectable.
// Torrent alternatives are intentionally not started here: starting another
// download/encoder could overwhelm small add-on hosts.
func (b *bot) findWatchAlternate(ctx context.Context, room *watchRoom, gen int, cli string, showID, ep int, adult, dub bool) {
    info, err := b.watch.infos.get(ctx, cli, showID, ep, dub, adult)
    if err != nil || len(info.Streams) == 0 || ctx.Err() != nil || info.Mode != map[bool]string{true: "dub", false: "sub"}[dub] {
        return
    }
    mode := "sub"
    if dub { mode = "dub" }
    b.watch.mu.Lock()
    defer b.watch.mu.Unlock()
    if b.watch.cur != room || room.gen != gen || room.phase != "play" || room.media == nil {
        return
    }
    // Do not publish a second label for the exact same source.
    if info.Streams[0].URL == room.streams[room.link].URL {
        return
    }
    media, entry, _ := newWatchMedia(info.Streams[0], dub)
    room.alternateMedia = media
    if room.audioEntries == nil {
        room.audioEntries = map[string]string{}
    }
    current := "sub"
    if room.dub { current = "dub" }
    room.audioEntries[current] = watchMediaPath(room.media.key, room.entry)
    room.audioEntries[mode] = watchMediaPath(media.key, entry)
    room.changed()
}

// startStream plays link k of the episode. Hold the lock.
func (b *bot) startStream(r *watchRoom, k int) bool {
	if k >= len(r.streams) {
		r.finishMedia()
		r.phase, r.errText = "error", fmt.Sprintf("No source could play episode %d. Pick it again to retry, or another episode.", r.ep)
		r.changed()
		return false
	}
	if !r.spent && !b.spendTicket(r.owner) {
		r.finishMedia()
		r.phase, r.errText = "error", "The member who opened this room has no watch ticket left."
		r.changed()
		return false
	}
	now := watchNow()
	if !r.spent {
		r.spent, r.started = true, now
	}
	r.finishMedia()
	r.link = k
	m, entry, subIDs := newWatchMedia(r.streams[k], r.dub)
	r.media, r.entry, r.subs = m, entry, nil
	r.audioEntries = nil
	if u, err := url.Parse(r.streams[k].URL); err == nil && u.Hostname() == "127.0.0.1" && u.Query().Get("languages") != "" {
		r.audioEntries = map[string]string{}
		for _, mode := range strings.Split(u.Query().Get("languages"), ",") {
			if mode != "sub" && mode != "dub" { continue }
			variant := *u
			variant.Path = "/" + mode + "/playlist.m3u8"
			variant.RawQuery = ""
			r.audioEntries[mode] = watchMediaPath(m.key, m.add(variant.String(), "list"))
		}
	}
	for n, s := range r.streams[k].Subtitles {
		label := s.Label
		if label == "" {
			label = s.Language
		}
		if label == "" {
			label = "Subtitles " + strconv.Itoa(n+1)
		}
		r.subs = append(r.subs, watchSub{Label: label, Lang: s.Language, URL: watchMediaPath(m.key, subIDs[n])})
	}
	r.phase, r.errText = "play", ""
	r.episodes++
	r.setPos(0, true, now)
	r.changed()
	return true
}

func watchMediaPath(key, id string) string { return "api/watch/media/" + key + "/" + id }

// spendTicket uses one of the member's watch tickets.
func (b *bot) spendTicket(user string) bool {
	ok := false
	b.store.update(func(d *storeData) {
		if d.WatchTickets[user] > 0 {
			d.WatchTickets[user]--
			if d.WatchTickets[user] == 0 {
				delete(d.WatchTickets, user)
			}
			ok = true
		}
	})
	return ok
}

// watchMax is how many can be in the room (/settings watch_room_max).
// Staff and the member who opened it always get in.
func (b *bot) watchMax() int { return b.settingInt("watch_room_max") }

func (b *bot) watchTickets(user string) int {
	n := 0
	b.store.view(func(d *storeData) { n = d.WatchTickets[user] })
	return n
}

// ── What the page gets ──

type watchPerson struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Host bool   `json:"host,omitempty"`
}

type watchView struct {
	Ver     int           `json:"ver"`
	Room    string        `json:"room"`
	Me      string        `json:"me"`
	Host    string        `json:"host"`
	IsHost  bool          `json:"is_host"`
	Staff   bool          `json:"staff,omitempty"`
	Phase     string `json:"phase"`
	LoadStep  string `json:"load_step,omitempty"`
	LoadSince int64  `json:"load_since,omitempty"`
	Err     string        `json:"err,omitempty"`
	Adult   bool          `json:"adult,omitempty"`
	Show    *watchShow    `json:"show,omitempty"`
	Ep      int           `json:"ep,omitempty"`
	LastEp  int           `json:"last_ep,omitempty"`
	Dub     bool          `json:"dub,omitempty"`
	Media   string        `json:"media,omitempty"`
	HLS     bool          `json:"hls,omitempty"`
	AudioMedia map[string]string `json:"audio_media,omitempty"`
	Subs    []watchSub    `json:"subs,omitempty"`
	Playing bool          `json:"playing"`
	Pos     float64       `json:"pos"`
	Dur     float64       `json:"dur,omitempty"`
	Ends    int64         `json:"ends,omitempty"` // unix ms, once started
	Members []watchPerson `json:"members"`
	Asks    []watchAsk    `json:"asks"`
	Sync    bool          `json:"sync"`
	Linked  []string      `json:"linked,omitempty"`
	Languages watchLanguagePreference `json:"languages"` // "AniList", "MAL"
	Max     int           `json:"max"`
}

// viewOf is the room as user sees it. Hold the room lock; sync and linked
// come from the store, read before taking it.
func (b *bot) watchViewOf(r *watchRoom, user string, staff bool, now time.Time) watchView {
	v := watchView{Ver: r.ver, Room: r.id, Me: user, Host: r.host, IsHost: r.host == user, Staff: staff, Phase: r.phase,
		Err: r.errText, Adult: r.adult, Show: r.show, Ep: r.ep, Dub: r.dub, Playing: r.playing, Pos: r.position(now),
		Dur: r.dur, Members: []watchPerson{}, Asks: []watchAsk{}, Max: b.watchMax()}
	if r.phase == "loading" {
		v.LoadStep = r.loadStep
		v.LoadSince = r.loadSince.UnixMilli()
	}
	if r.show != nil {
		v.LastEp = r.show.lastEpisode()
	}
	if r.media != nil {
		v.Media = watchMediaPath(r.media.key, r.entry)
		v.HLS = strings.HasSuffix(r.entry, ".m3u8")
		v.AudioMedia = r.audioEntries
		v.Subs = r.subs
	}
	if !r.started.IsZero() {
		v.Ends = r.started.Add(watchRoomLength).UnixMilli()
	}
	for _, id := range r.people() {
		v.Members = append(v.Members, watchPerson{ID: id, Name: r.members[id].name, Host: id == r.host})
	}
	for _, a := range r.asks {
		if v.IsHost || a.User == user {
			v.Asks = append(v.Asks, a)
		}
	}
	return v
}

// watchExtras fills in what the view reads from the store.
func (b *bot) watchExtras(v *watchView, user string) {
	b.store.view(func(d *storeData) {
		v.Sync = !d.WatchNoSync[user]
		v.Languages = watchLanguagePreference{Audio: "auto", Subtitles: "auto"}
		if p, ok := d.WatchLanguages[user]; ok {
			if watchValidLanguage(p.Audio, false) { v.Languages.Audio = p.Audio }
			if watchValidLanguage(p.Subtitles, true) { v.Languages.Subtitles = p.Subtitles }
		}
		if l, ok := d.AniList[user]; ok && l.Verified && l.Token != "" {
			v.Linked = append(v.Linked, "AniList")
		}
		if _, ok := d.MAL[user]; ok {
			v.Linked = append(v.Linked, "MAL")
		}
	})
}

// ── API ──

func init() {
	apiHandle("GET /activity/api/watch/status", (*bot).watchStatus)
	apiHandle("POST /activity/api/watch/open", (*bot).watchOpen)
	apiHandle("GET /activity/api/watch/state", (*bot).watchState)
	apiHandle("POST /activity/api/watch/leave", (*bot).watchLeave)
	apiHandle("GET /activity/api/watch/search", (*bot).watchSearchAPI)
	apiHandle("POST /activity/api/watch/pick", (*bot).watchPick)
	apiHandle("POST /activity/api/watch/ctl", (*bot).watchCtl)
	apiHandle("POST /activity/api/watch/beat", (*bot).watchBeat)
	apiHandle("POST /activity/api/watch/ask", (*bot).watchAskAPI)
	apiHandle("POST /activity/api/watch/answer", (*bot).watchAnswer)
	apiHandle("POST /activity/api/watch/take", (*bot).watchTake)
	apiHandle("POST /activity/api/watch/sync", (*bot).watchSync)
	apiHandle("POST /activity/api/watch/languages", (*bot).watchLanguageSettings)
	apiHandle("POST /activity/api/watch/end", (*bot).watchEnd)
	apiHandle("GET /activity/api/watch/episodes", (*bot).watchEpisodes)
	apiHandle("GET /activity/api/watch/cover", (*bot).watchCover)
	apiHandle("GET /activity/api/watch/media/{key}/{id}", (*bot).watchMediaAPI)
	apiHandle("HEAD /activity/api/watch/media/{key}/{id}", (*bot).watchMediaAPI)
}

var (
	watchSearchLimit = newRateLimit(30, time.Minute)
	watchActLimit    = newRateLimit(120, time.Minute)
)

type watchIn struct {
	Room    string  `json:"room"`
	Channel string  `json:"channel"`
	Show    int     `json:"show"`
	Ep      int     `json:"ep"`
	Dub     bool    `json:"dub"`
	Audio string `json:"audio"`
	Subtitles string `json:"subtitles"`
	Act     string  `json:"act"`
	Pos     float64 `json:"pos"`
	Playing bool    `json:"playing"`
	Dur     float64 `json:"dur"`
	Kind    string  `json:"kind"`
	ID      int     `json:"id"`
	OK      bool    `json:"ok"`
	On      bool    `json:"on"`
}

// watchRead reads a request for the room the page is in. The room is the
// current one and user is in it, or an error has been answered.
func (b *bot) watchRead(w http.ResponseWriter, r *http.Request) (string, watchIn, bool) {
	user := b.actUser(w, r)
	if user == "" {
		return "", watchIn{}, false
	}
	var in watchIn
	if !readJSON(w, r, 4<<10, &in) {
		return "", in, false
	}
	if !togetherRoomID.MatchString(in.Room) {
		apiError(w, http.StatusBadRequest, "no room")
		return "", in, false
	}
	if !watchActLimit.allow(user, watchNow()) {
		apiError(w, http.StatusTooManyRequests, "slow down")
		return "", in, false
	}
	return user, in, true
}

// inRoom is the current room when it is this one and user is in it. Hold
// the lock.
func (b *bot) inRoom(w http.ResponseWriter, id, user string) *watchRoom {
	r := b.watch.cur
	if r == nil || r.id != id {
		apiError(w, http.StatusNotFound, "the watch room is over")
		return nil
	}
	if _, ok := r.members[user]; !ok {
		apiError(w, http.StatusForbidden, "you're not in the watch room")
		return nil
	}
	return r
}

// answer writes the room's view for user. Hold the lock; it is released.
func (b *bot) watchAnswerView(w http.ResponseWriter, r *watchRoom, user string, staff bool) {
	v := b.watchViewOf(r, user, staff, watchNow())
	b.watch.mu.Unlock()
	b.watchExtras(&v, user)
	writeJSON(w, http.StatusOK, v)
}

// watchStatus is what the tile and the lobby show: whether a room runs and
// where, and the member's tickets.
func (b *bot) watchStatus(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	room := r.URL.Query().Get("room")
	out := map[string]any{"tickets": b.watchTickets(user), "max": b.watchMax()}
	b.watch.mu.Lock()
	if cur := b.watch.cur; cur != nil {
		out["busy"] = true
		out["here"] = cur.id == room
		out["channel"] = cur.channel
		if c := watchChannel(b, cur.channel); c != nil {
			out["channel_name"] = c.Name
		}
		out["people"] = len(cur.members)
		if cur.show != nil {
			out["show"] = cur.show.Title
		}
		if !cur.started.IsZero() {
			out["ends"] = cur.started.Add(watchRoomLength).UnixMilli()
		}
	}
	b.watch.mu.Unlock()
	writeJSON(w, http.StatusOK, out)
}

// watchOpen joins the room in this call, or opens one with a ticket.
func (b *bot) watchOpen(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.watchRead(w, r)
	if !ok {
		return
	}
	staff := b.isAdmin(user)
	name := b.dashName(user)
	tickets := b.watchTickets(user)
	ch := watchChannel(b, in.Channel)
	adult := ch != nil && ch.NSFW
	now := watchNow()
	b.watch.mu.Lock()
	cur := b.watch.cur
	switch {
	case cur != nil && cur.id != in.Room:
		b.watch.mu.Unlock()
		where := "another call"
		if c := watchChannel(b, cur.channel); c != nil {
			where = "#" + c.Name
		}
		apiError(w, http.StatusConflict, "A watch room is already running in "+where+". Only one runs at a time.")
		return
	case cur != nil:
		if _, ok := cur.members[user]; !ok && len(cur.members) >= b.watchMax() && user != cur.owner && !staff {
			b.watch.mu.Unlock()
			apiError(w, http.StatusForbidden, fmt.Sprintf("The watch room is full (%d people).", b.watchMax()))
			return
		}
	default:
		if tickets < 1 {
			b.watch.mu.Unlock()
			apiError(w, http.StatusPaymentRequired, "Opening a watch room takes a watch ticket. Redeem a code with /redeem to get one.")
			return
		}
		if ch == nil || ch.GuildID != b.cfg.GuildID {
			b.watch.mu.Unlock()
			apiError(w, http.StatusBadRequest, "Open the Activity in a voice channel on the Otakase server.")
			return
		}
		cur = &watchRoom{id: in.Room, channel: in.Channel, adult: adult, owner: user, host: user, opened: now,
			phase: "pick", members: map[string]*watchMember{}}
		b.watch.cur = cur
		watchClock(b, cur)
		log.Printf("watch room opened by member %s in channel %s", user, in.Channel)
	}
	if m, ok := cur.members[user]; ok {
		m.seen = now
	} else {
		cur.members[user] = &watchMember{name: name, joined: now, seen: now}
		cur.changed()
	}
	b.watchAnswerView(w, cur, user, staff)
}

// watchState answers once the room's version differs from v, or after
// watchPollWait: a long poll that also keeps the member in the room.
func (b *bot) watchState(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	id := r.URL.Query().Get("room")
	have, _ := strconv.Atoi(r.URL.Query().Get("v"))
	staff := b.isAdmin(user)
	timeout := time.NewTimer(watchPollWait)
	defer timeout.Stop()
	for {
		b.watch.mu.Lock()
		cur := b.watch.cur
		if cur == nil || cur.id != id {
			b.watch.mu.Unlock()
			writeJSON(w, http.StatusOK, watchView{Room: id, Me: user, Phase: "over", Members: []watchPerson{}, Asks: []watchAsk{}})
			return
		}
		m, in := cur.members[user]
		if !in {
			b.watch.mu.Unlock()
			apiError(w, http.StatusForbidden, "you're not in the watch room")
			return
		}
		m.seen = watchNow()
		if cur.ver != have {
			b.watchAnswerView(w, cur, user, staff)
			return
		}
		wake := cur.wake
		if wake == nil {
			cur.wake = make(chan struct{})
			wake = cur.wake
		}
		b.watch.mu.Unlock()
		select {
		case <-wake:
		case <-timeout.C:
			have = -1
		case <-r.Context().Done():
			return
		}
	}
}

func (b *bot) watchLeave(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.watchRead(w, r)
	if !ok {
		return
	}
	b.watch.mu.Lock()
	if cur := b.watch.cur; cur != nil && cur.id == in.Room {
		if _, ok := cur.members[user]; ok {
			delete(cur.members, user)
			cur.prune(watchNow())
			cur.changed()
		}
	}
	b.watch.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// watchSearchAPI searches AniList for the host's picker.
func (b *bot) watchSearchAPI(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	if !watchSearchLimit.allow(user, watchNow()) {
		apiError(w, http.StatusTooManyRequests, "slow down")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 || len(q) > 100 {
		writeJSON(w, http.StatusOK, []watchShow{})
		return
	}
	adult := false
	b.watch.mu.Lock()
	if cur := b.watch.cur; cur != nil && cur.id == r.URL.Query().Get("room") {
		adult = cur.adult
	}
	b.watch.mu.Unlock()
	list, err := watchSearch(q, adult)
	if err != nil {
		log.Printf("watch search: %v", err)
		apiError(w, http.StatusBadGateway, "couldn't reach AniList, try again in a bit")
		return
	}
	for k := range list {
		if list[k].Cover != "" {
			list[k].Cover = "api/watch/cover?u=" + url.QueryEscape(list[k].Cover)
		}
		for j := range list[k].Previews {
			u := list[k].Previews[j].Thumbnail
			if strings.HasPrefix(u, "https://s4.anilist.co/") || strings.HasPrefix(u, "https://img1.ak.crunchyroll.com/") || strings.HasPrefix(u, "https://static.crunchyroll.com/") {
				list[k].Previews[j].Thumbnail = "api/watch/cover?u=" + url.QueryEscape(u)
			} else {
				list[k].Previews[j].Thumbnail = ""
			}
		}
	}
	writeJSON(w, http.StatusOK, list)
}

func (b *bot) watchCover(w http.ResponseWriter, r *http.Request) {
	proxyAniList(w, r, r.URL.Query().Get("u"))
}


 // watchEpisodes loads episode-specific metadata only when the picker opens.
 // Tenrai's Jikan-compatible data is indexed by MAL episode number; AniList
 // previews remain the fallback when the upstream has no cover.
 var episodePreviewCache = struct {
 sync.Mutex
 entries map[int]struct {
   until time.Time
   previews map[int]watchEpisodePreview
 }
}{entries: make(map[int]struct {
 until time.Time
 previews map[int]watchEpisodePreview
})}

func (b *bot) watchEpisodes(w http.ResponseWriter, r *http.Request) {
   user := b.actUser(w, r)
   if user == "" { return }
   if !watchSearchLimit.allow(user, watchNow()) {
     apiError(w, http.StatusTooManyRequests, "slow down"); return
   }
   id, err := strconv.Atoi(r.URL.Query().Get("id"))
   if err != nil || id < 1 { apiError(w, http.StatusBadRequest, "invalid anime"); return }
   show, err := watchShowByID(id)
   if err != nil { apiError(w, http.StatusBadGateway, "anime unavailable"); return }
   if show.Adult {
     b.watch.mu.Lock()
     allowed := b.watch.cur != nil && b.watch.cur.id == r.URL.Query().Get("room") && b.watch.cur.adult
     b.watch.mu.Unlock()
     if !allowed { apiError(w, http.StatusForbidden, "not available in this room"); return }
   }
   episodePreviewCache.Lock()
   cached, found := episodePreviewCache.entries[id]
   episodePreviewCache.Unlock()
   if found && time.Now().Before(cached.until) {
     writeJSON(w, http.StatusOK, cached.previews)
     return
   }
   ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
   defer cancel()
   tenrai := make(map[int]watchEpisodePreview)
   jikan := make(map[int]watchEpisodePreview)
   kitsu := make(map[int]watchEpisodePreview)
   var wg sync.WaitGroup
   wg.Add(3)
   go func() {
     defer wg.Done()
   if show.MalID > 0 {
     for page := 1; page <= 4; page++ {
       endpoint := fmt.Sprintf("https://api.tenrai.org/v1/anime/%d/episodes?page=%d", show.MalID, page)
       req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
       if err != nil { break }
       resp, err := activityClient.Do(req)
       if err != nil { break }
       if resp.StatusCode != http.StatusOK { resp.Body.Close(); break }
       var payload struct {
         Data []struct {
           MalID int `json:"mal_id"`
           Title string `json:"title"`
           Images struct {
             JPG struct { ImageURL string `json:"image_url"` } `json:"jpg"`
             WebP struct { ImageURL string `json:"image_url"` } `json:"webp"`
           } `json:"images"`
           ImageURL string `json:"image_url"`
         } `json:"data"`
         Pagination struct { HasNextPage bool `json:"has_next_page"` } `json:"pagination"`
       }
       err = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload)
       resp.Body.Close()
       if err != nil { break }
       for _, ep := range payload.Data {
         if ep.MalID < 1 || ep.MalID > show.lastEpisode() { continue }
         img := ep.Images.JPG.ImageURL
         if img == "" { img = ep.Images.WebP.ImageURL }
         if img == "" { img = ep.ImageURL }
         // The image proxy only permits known hosts, so never send arbitrary URLs.
         if strings.HasPrefix(img, "https://img1.ak.crunchyroll.com/") ||
           strings.HasPrefix(img, "https://static.crunchyroll.com/") ||
           strings.HasPrefix(img, "https://s4.anilist.co/") {
           img = "api/watch/cover?u=" + url.QueryEscape(img)
         } else { img = "" }
         tenrai[ep.MalID] = watchEpisodePreview{Title: ep.Title, Thumbnail: img}
       }
       if !payload.Pagination.HasNextPage || len(payload.Data) == 0 { break }
     }
   }
   }()
   go func() {
     defer wg.Done()
   // Jikan supplies episode titles for MAL entries absent from Tenrai.
   // Preserve Tenrai thumbnails and never replace a non-empty title.
   if show.MalID > 0 {
     for page := 1; page <= 4; page++ {
       endpoint := fmt.Sprintf("https://api.jikan.moe/v4/anime/%d/episodes?page=%d", show.MalID, page)
       req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
       if err != nil { break }
       resp, err := activityClient.Do(req)
       if err != nil { break }
       if resp.StatusCode != http.StatusOK { resp.Body.Close(); break }
       var payload struct {
         Data []struct {
           MalID int `json:"mal_id"`
           Title string `json:"title"`
           TitleJapanese string `json:"title_japanese"`
         } `json:"data"`
         Pagination struct { HasNextPage bool `json:"has_next_page"` } `json:"pagination"`
       }
       err = json.NewDecoder(http.MaxBytesReader(w, resp.Body, 2<<20)).Decode(&payload)
       resp.Body.Close()
       if err != nil { break }
       for _, ep := range payload.Data {
         if ep.MalID < 1 || ep.MalID > show.lastEpisode() { continue }
         current := jikan[ep.MalID]
         if current.Title == "" {
           current.Title = ep.Title
           if current.Title == "" { current.Title = ep.TitleJapanese }
           jikan[ep.MalID] = current
         }
       }
       if !payload.Pagination.HasNextPage || len(payload.Data) == 0 { break }
     }
   }
   }()
   go func() {
     defer wg.Done()
     extraEpisodePreviews(ctx, show, kitsu)
   }()
   wg.Wait()
   result := make(map[int]watchEpisodePreview)
   for ep, v := range kitsu { result[ep] = v }
   for ep, v := range jikan {
     old := result[ep]
     if v.Title != "" { old.Title = v.Title }
     result[ep] = old
   }
   for ep, v := range tenrai {
     old := result[ep]
     if v.Title != "" { old.Title = v.Title }
     if v.Thumbnail != "" { old.Thumbnail = v.Thumbnail }
     result[ep] = old
   }
   episodePreviewCache.Lock()
   if len(episodePreviewCache.entries) >= 256 {
     clear(episodePreviewCache.entries)
   }
   episodePreviewCache.entries[id] = struct {
     until time.Time
     previews map[int]watchEpisodePreview
   }{until: time.Now().Add(30 * time.Minute), previews: result}
   episodePreviewCache.Unlock()
   writeJSON(w, http.StatusOK, result)
 }

// watchPick starts an episode (host only).
func (b *bot) watchPick(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.watchRead(w, r)
	if !ok {
		return
	}
	show, err := watchShowByID(in.Show)
	if err != nil {
		apiError(w, http.StatusBadGateway, "couldn't reach AniList, try again in a bit")
		return
	}
	staff := b.isAdmin(user)
	b.watch.mu.Lock()
	cur := b.inRoom(w, in.Room, user)
	switch {
	case cur == nil:
		b.watch.mu.Unlock()
		return
	case cur.host != user:
		b.watch.mu.Unlock()
		apiError(w, http.StatusForbidden, "only the host picks what plays")
		return
	case show.Adult && !cur.adult:
		b.watch.mu.Unlock()
		apiError(w, http.StatusForbidden, "18+ shows only play in age-restricted voice channels")
		return
	case in.Ep < 1 || in.Ep > show.lastEpisode():
		b.watch.mu.Unlock()
		apiError(w, http.StatusBadRequest, fmt.Sprintf("pick an episode from 1 to %d", show.lastEpisode()))
		return
	case !cur.started.IsZero() && watchNow().After(cur.started.Add(watchRoomLength)):
		b.watch.mu.Unlock()
		apiError(w, http.StatusForbidden, "the watch room's time is up")
		return
	}
	b.loadEpisode(cur, show, in.Ep, in.Dub)
	b.watchAnswerView(w, cur, user, staff)
}

// watchCtl is the host's controls. play/pause/seek take pos; next and prev
// change episode; broken moves to the episode's next link when one fails
// in the browser.
func (b *bot) watchCtl(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.watchRead(w, r)
	if !ok {
		return
	}
	staff := b.isAdmin(user)
	b.watch.mu.Lock()
	cur := b.inRoom(w, in.Room, user)
	if cur == nil {
		b.watch.mu.Unlock()
		return
	}
	if cur.host != user {
		b.watch.mu.Unlock()
		apiError(w, http.StatusForbidden, "only the host controls playback")
		return
	}
	if err := b.watchDo(cur, in.Act, in.Pos); err != "" {
		b.watch.mu.Unlock()
		apiError(w, http.StatusBadRequest, err)
		return
	}
	b.watchAnswerView(w, cur, user, staff)
}

// watchDo carries out a control, the host's or an accepted request. It
// answers what went wrong. Hold the lock.
func (b *bot) watchDo(r *watchRoom, act string, pos float64) string {
	now := watchNow()
	switch act {
	case "cancel":
		if r.phase != "loading" {
			return "nothing is loading"
		}
		r.gen++
		r.finishMedia()
		r.phase, r.errText = "pick", ""
		r.loadStep = ""
		r.changed()
		return ""
	case "play", "pause", "seek":
		if r.phase != "play" {
			return "nothing is playing"
		}
		if act == "seek" {
			if pos < 0 || math.IsNaN(pos) || r.dur > 0 && pos > r.dur {
				return "that's outside the episode"
			}
		} else if pos < 0 || math.IsNaN(pos) {
			pos = r.position(now)
		}
		playing := r.playing
		if act != "seek" {
			playing = act == "play"
		}
		r.setPos(pos, playing, now)
	case "skip":
		if r.phase != "play" {
			return "nothing is playing"
		}
		p := r.position(now) + watchSkipOP
		if r.dur > 0 {
			p = math.Min(p, r.dur-1)
		}
		r.setPos(p, r.playing, now)
	case "next", "prev":
		if r.show == nil {
			return "no show picked"
		}
		ep := r.ep + 1
		if act == "prev" {
			ep = r.ep - 1
		}
		if ep < 1 || ep > r.show.lastEpisode() {
			return "there's no episode " + strconv.Itoa(ep)
		}
		if !r.started.IsZero() && now.After(r.started.Add(watchRoomLength)) {
			return "the watch room's time is up"
		}
		b.loadEpisode(r, *r.show, ep, r.dub)
		return ""
	case "broken":
		if r.phase == "play" {
			b.startStream(r, r.link+1)
		}
		return ""
	default:
		return "unknown control"
	}
	r.changed()
	return ""
}

// watchBeat is the host's page reporting where it is. The others only hear
// about it when it differs from where they think the host is.
func (b *bot) watchBeat(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.watchRead(w, r)
	if !ok {
		return
	}
	b.watch.mu.Lock()
	defer b.watch.mu.Unlock()
	cur := b.inRoom(w, in.Room, user)
	if cur == nil {
		return
	}
	if cur.host != user || cur.phase != "play" || math.IsNaN(in.Pos) || in.Pos < 0 {
		writeJSON(w, http.StatusOK, map[string]int{"ver": cur.ver})
		return
	}
	now := watchNow()
	if in.Dur > 0 && in.Dur < 6*3600 && math.Abs(in.Dur-cur.dur) > 1 {
		cur.dur = in.Dur
		cur.changed()
	}
	drift := math.Abs(cur.position(now) - in.Pos)
	moved := in.Playing != cur.playing || drift > watchDrift
	cur.setPos(in.Pos, in.Playing, now)
	if moved {
		cur.changed()
	}
	writeJSON(w, http.StatusOK, map[string]int{"ver": cur.ver})
}

// watchAskAPI is a member asking the host for a pause, play, skip of the
// opening or the next episode.
func (b *bot) watchAskAPI(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.watchRead(w, r)
	if !ok {
		return
	}
	if !slices.Contains([]string{"pause", "play", "skip", "next"}, in.Kind) {
		apiError(w, http.StatusBadRequest, "unknown request")
		return
	}
	staff := b.isAdmin(user)
	b.watch.mu.Lock()
	cur := b.inRoom(w, in.Room, user)
	if cur == nil {
		b.watch.mu.Unlock()
		return
	}
	if cur.host == user {
		b.watch.mu.Unlock()
		apiError(w, http.StatusBadRequest, "you're the host, just do it")
		return
	}
	for _, a := range cur.asks {
		if a.User == user {
			b.watch.mu.Unlock()
			apiError(w, http.StatusTooManyRequests, "wait for the host to answer your last request")
			return
		}
	}
	cur.askN++
	cur.asks = append(cur.asks, watchAsk{ID: cur.askN, User: user, Name: cur.members[user].name, Kind: in.Kind, At: watchNow()})
	cur.changed()
	b.watchAnswerView(w, cur, user, staff)
}

// watchAnswer is the host saying yes or no to a request.
func (b *bot) watchAnswer(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.watchRead(w, r)
	if !ok {
		return
	}
	staff := b.isAdmin(user)
	b.watch.mu.Lock()
	cur := b.inRoom(w, in.Room, user)
	if cur == nil {
		b.watch.mu.Unlock()
		return
	}
	if cur.host != user {
		b.watch.mu.Unlock()
		apiError(w, http.StatusForbidden, "only the host answers requests")
		return
	}
	k := slices.IndexFunc(cur.asks, func(a watchAsk) bool { return a.ID == in.ID })
	if k < 0 {
		b.watch.mu.Unlock()
		apiError(w, http.StatusNotFound, "that request is gone")
		return
	}
	a := cur.asks[k]
	cur.asks = slices.Delete(cur.asks, k, k+1)
	if in.OK {
		b.watchDo(cur, a.Kind, -1)
	}
	cur.changed()
	b.watchAnswerView(w, cur, user, staff)
}

// watchTake lets staff take control of the room.
func (b *bot) watchTake(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.watchRead(w, r)
	if !ok {
		return
	}
	if !b.isAdmin(user) {
		apiError(w, http.StatusForbidden, "only staff can take control")
		return
	}
	b.watch.mu.Lock()
	cur := b.inRoom(w, in.Room, user)
	if cur == nil {
		b.watch.mu.Unlock()
		return
	}
	cur.host = user
	cur.changed()
	b.watchAnswerView(w, cur, user, true)
}

// watchSync switches whether the room moves the member's lists on.
func (b *bot) watchSync(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.watchRead(w, r)
	if !ok {
		return
	}
	b.store.update(func(d *storeData) {
		if in.On {
			delete(d.WatchNoSync, user)
			return
		}
		if d.WatchNoSync == nil {
			d.WatchNoSync = map[string]bool{}
		}
		d.WatchNoSync[user] = true
	})
	writeJSON(w, http.StatusOK, map[string]bool{"sync": in.On})
}

// watchEnd closes the room early (the host or staff).
func (b *bot) watchEnd(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.watchRead(w, r)
	if !ok {
		return
	}
	staff := b.isAdmin(user)
	b.watch.mu.Lock()
	cur := b.inRoom(w, in.Room, user)
	if cur == nil {
		b.watch.mu.Unlock()
		return
	}
	if cur.host != user && cur.owner != user && !staff {
		b.watch.mu.Unlock()
		apiError(w, http.StatusForbidden, "only the host or staff can end the room")
		return
	}
	b.closeWatchRoom(cur, watchNow())
	b.watch.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// watchMediaAPI is the relay: the current episode, or the one just before
// while players let go of it.
func (b *bot) watchMediaAPI(w http.ResponseWriter, r *http.Request) {
	key, id := r.PathValue("key"), r.PathValue("id")
	b.watch.mu.Lock()
	var m *watchMedia
	if cur := b.watch.cur; cur != nil {
		for _, c := range []*watchMedia{cur.media, cur.old, cur.alternateMedia} {
			if c != nil && c.key == key {
				m = c
			}
		}
	}
	cache := b.watchCache()
	b.watch.mu.Unlock()
	if m == nil {
		http.NotFound(w, r)
		return
	}
	m.serve(w, r, id, cache)
}

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
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
// ticket (from /redeem codes), spent once the first episode starts and
// given back if the room ends before half a minute of it played; staff
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
	watchPerkLength = 8 * time.Hour    // a room opened by staff or the Game Champion
	watchGone       = 45 * time.Second // a member who hasn't polled this long has left
	watchEmptyClose = time.Minute      // an empty room closes after this
	watchPickWait   = 15 * time.Minute // a room that never played closes after this
	watchAskTTL     = 30 * time.Second
	watchAskJumpMax = 60 // seconds a -10s/+10s request adds up to
	watchPollWait   = 25 * time.Second
	watchTick       = 2 * time.Second
	watchDrift      = 2.0 // seconds the host may be off before everyone is told
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
	asked   bool    // they answered whether to count an episode they came in late to
	from    float64 // where the episode was when they joined it

	// What this member's player last reported, for the people bar.
	pos    float64
	stuck  bool // it was buffering or still loading
	beatAt time.Time
}

type watchAsk struct {
	ID   int       `json:"id"`
	User string    `json:"user"`
	Name string    `json:"name"`
	Kind string    `json:"kind"`          // pause, play, forward, seek, next
	By   float64   `json:"by,omitempty"`  // forward: seconds, back when below 0
	Pos  float64   `json:"pos,omitempty"` // seek: the spot asked for
	Left int64     `json:"left_ms"`       // until it lapses, set for each view
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
	// perks is "staff" or "champion" when staff or the Game Champion
	// opened it: no ticket, 8 hours instead of 4 and room for more people
	// (watch_room_max_perks, watch_room_max_champion). Empty otherwise.
	perks string
	// watched is set once the host's player got through half a minute of
	// video; a room that ends before that gives the ticket back.
	watched   bool
	beatFrom  float64 // the first playing position the host reported
	beatStart bool
	members   map[string]*watchMember
	resumes   []watchResume // positions of members who left, to save

	show      *watchShow
	ep        int
	dub       bool
	startAt   float64 // where the episode starts: 0, or a saved position to resume from
	phase     string  // pick, loading, play, error, over
	errText   string
	loadStep  string // current episode preparation step
	loadSince time.Time
	gen       int // bumps with every episode load, so a slow one can't land late

	streams         []streamLink
	provider        string // provider that resolved the selected episode
	providerID      string // the show's id there, for the language lookup
	alternateOrigin *watchPlaybackOrigin
	link            int // index into streams
	torrent         *watchTorrent
	torrentCancel   context.CancelFunc
	media           *watchMedia
	old             *watchMedia // the episode before, still served for a moment
	entry           string
	audioEntries    map[string]string
	audioReleases   map[string]streamLink
	extraAudioMedia map[string]*watchMedia
	alternateMedia  *watchMedia
	subs            []watchSub

	playing       bool
	pos           float64
	at            time.Time
	dur           float64
	autoSkipOP    bool // host controls room-wide opening/ending auto-skip
	autoSkipED    bool
	skipIntervals watchSkipIntervals
	skipReady     bool
	skipSeq       int // incremented when the server seeks past a real skip interval
	skipTarget    float64
	skipHoldUntil time.Time

	asks []watchAsk
	askN int

	chat  []watchChat
	chatN int

	events []watchEvent // what just happened, flashed over the video (watchevents.go)
	eventN int

	vote *watchVote // a running skip vote (watchvote.go)

	// The ready check (watchready.go): hold is the running check's id, 0
	// when there is none; the room waits at holdPos until holdUntil.
	hold      int
	holdPos   float64
	holdUntil time.Time
	ready     map[string]bool

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

// watchRooms is every open room, by Activity instance. Up to
// watch_rooms_max run at once; each one is streamed on its own, so every
// room adds its viewers' upload.
type watchRooms struct {
	mu    sync.Mutex
	rooms map[string]*watchRoom
	cache *segCache
	edge  edgeUse // segments handed to the edge cache today (watchedge.go)
	// lastSave is what saveWatchRooms last wrote (watchrestart.go). Only
	// its loop and shutdown touch it.
	lastSave string
	infos    watchInfos
	bg       sync.WaitGroup // an episode's lookups, so tests can wait them out
	// offers are the episodes members left near the end of after coming in
	// late, kept for a while so the leave screen can still count them.
	offers map[string]watchOffer
}

// room is the open room for this Activity instance, nil when there is
// none. Hold the lock.
func (w *watchRooms) room(id string) *watchRoom {
	return w.rooms[id]
}

// open reports whether r is still running. Hold the lock.
func (w *watchRooms) open(r *watchRoom) bool {
	return r != nil && w.rooms[r.id] == r
}

// add makes r an open room. Hold the lock.
func (w *watchRooms) add(r *watchRoom) {
	if w.rooms == nil {
		w.rooms = map[string]*watchRoom{}
	}
	w.rooms[r.id] = r
}

// list is the open rooms, oldest first. Hold the lock.
func (w *watchRooms) list() []*watchRoom {
	out := make([]*watchRoom, 0, len(w.rooms))
	for _, r := range w.rooms {
		out = append(out, r)
	}
	sort.Slice(out, func(a, b int) bool {
		if !out[a].opened.Equal(out[b].opened) {
			return out[a].opened.Before(out[b].opened)
		}
		return out[a].id < out[b].id
	})
	return out
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

// skipTo jumps the room past an opening or ending and keeps it playing or
// paused as it was. The skip number tells the host's page to follow, and
// its stale heartbeats are ignored until it gets there. Hold the lock.
func (r *watchRoom) skipTo(to float64, now time.Time) {
	if r.hold != 0 {
		r.holdAt(to, watchReadyWait, now)
	} else {
		r.setPos(to, r.playing, now)
	}
	r.skipSeq++
	r.skipTarget = to
	r.skipHoldUntil = now.Add(8 * time.Second)
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
			r.keepResume(id, m.seen)
			r.note("%s dropped out", r.name(id))
			delete(r.members, id)
			gone = true
		}
	}
	if _, ok := r.members[r.host]; !ok {
		if ids := r.people(); len(ids) > 0 {
			r.host = ids[0]
			r.note("%s is now the host", r.name(r.host))
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
	if !b.watch.open(r) {
		b.watch.mu.Unlock()
		return false
	}
	r.prune(now)
	if r.phase == "play" && r.holdDone(now) {
		r.release(now)
	}
	if r.dropVote(now) {
		r.changed()
	}
	r.dropEvents(now)
	elapsed := now.Sub(r.tick).Seconds()
	if r.tick.IsZero() || elapsed > 3*watchTick.Seconds() {
		elapsed = 0
	}
	r.tick = now
	r.peak = max(r.peak, len(r.members))
	if r.phase == "play" && r.playing {
		if target, ok := watchSkipTarget(r.skipIntervals.match(r.dur), r.position(now), r.dur, r.autoSkipOP, r.autoSkipED); ok {
			r.skipTo(target, now)
			r.noteSkip(target)
			r.changed()
		}
	}

	b.edgeTick(r, elapsed, now)
	var count []string
	if r.phase == "play" && r.playing && r.dur > 0 {
		for id, m := range r.members {
			m.watched += elapsed
			// A resumed episode counts on most of what was left of it.
			if !m.counted && m.watched >= watchedEnough*math.Max(r.dur-r.startAt, 30) {
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
	case !r.ends().IsZero() && now.After(r.ends()):
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
	b.flushResumes(r)
	b.watch.mu.Unlock()

	if len(count) > 0 && show.tracked() {
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
	for id := range r.members {
		r.keepResume(id, now)
	}
	b.flushResumes(r)
	r.phase = "over"
	r.finishMedia()
	r.changed()
	if b.watch.open(r) {
		delete(b.watch.rooms, r.id)
	}
	if r.spent && !r.watched {
		// Nothing played: the source failed or everyone left while it
		// loaded. The ticket goes back.
		r.spent = false
		owner := r.owner
		// On its own, but counted in bg so tests can wait for the write.
		b.watch.bg.Go(func() {
			b.store.update(func(d *storeData) {
				if d.WatchTickets == nil {
					d.WatchTickets = map[string]int{}
				}
				d.WatchTickets[owner]++
			})
		})
		log.Printf("watch room: ticket returned to member %s, nothing played", owner)
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
	b.watch.bg.Go(func() {
		b.store.update(func(d *storeData) {
			d.WatchRooms = append(d.WatchRooms, entry)
			if len(d.WatchRooms) > watchLogKeep {
				d.WatchRooms = d.WatchRooms[len(d.WatchRooms)-watchLogKeep:]
			}
		})
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
	r.alternateOrigin = nil
	r.extraAudioMedia = nil
}

// ── Episodes ──

// loadEpisode looks the episode up and starts it. Hold the lock; the
// lookup runs on its own and lands only if nothing was picked since.
func (b *bot) loadEpisode(r *watchRoom, show watchShow, ep int, dub bool, start float64) {
	// The room keeps its own copy: the lookups below read show unlocked.
	shown := show
	r.show, r.ep, r.dub, r.startAt = &shown, ep, dub, start
	r.phase, r.errText = "loading", ""
	r.loadStep, r.loadSince = "Checking catalog for episode", time.Now()
	r.finishMedia()
	r.streams, r.link, r.subs, r.entry, r.audioEntries = nil, 0, nil, "", nil
	r.provider, r.providerID = "", ""
	r.audioReleases = nil
	r.skipIntervals = watchSkipIntervals{}
	r.skipReady = show.MalID <= 0
	// Keep skipSeq monotonic across episodes: a quick next-episode load must
	// not reuse an old value before the host receives the new playback view.
	r.skipHoldUntil = time.Time{}
	r.skipTarget = 0
	r.unhold()
	r.vote = nil
	r.setPos(0, false, watchNow())
	r.dur = 0
	r.asks = nil
	for _, m := range r.members {
		m.watched, m.counted, m.asked, m.from = 0, false, false, 0
	}
	r.gen++
	gen, adult, cli := r.gen, r.adult && show.Adult, b.cfg.OtakaseCLI
	r.changed()
	ctx, cancel := context.WithCancel(context.Background())
	r.torrentCancel = cancel
	if show.MalID > 0 {
		b.watch.bg.Go(func() {
			spans, err := watchSkipLookup(ctx, show.MalID, ep)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				log.Printf("watch opening/ending lookup for %d:%d: %v", show.MalID, ep, err)
			}
			b.watch.mu.Lock()
			if b.watch.open(r) && r.gen == gen && r.phase != "over" {
				r.skipIntervals = spans
				r.skipReady = true
				r.changed()
			}
			b.watch.mu.Unlock()
		})
	}
	host, hostName := r.host, r.name(r.host)
	b.watch.bg.Go(func() {
		step := func(text string) {
			b.watch.mu.Lock()
			defer b.watch.mu.Unlock()
			if b.watch.open(r) && r.gen == gen && r.phase == "loading" && text != "" {
				r.loadStep = text
				r.changed()
			}
		}
		if show.isMovie() {
			b.loadWatchMovie(ctx, cancel, r, gen, cli, host, hostName, step)
			return
		}
		info, err := b.watch.infos.get(ctx, cli, show.ID, ep, dub, adult)
		if err == nil && len(info.Streams) == 0 && len(info.Torrents) > 0 {
			b.watch.mu.Lock()
			if b.watch.open(r) && r.gen == gen {
				r.loadStep = "Connecting to playback relay"
				r.changed()
			}
			b.watch.mu.Unlock()
		}
		var torrent *watchTorrent
		if err == nil && len(info.Streams) == 0 && len(info.Torrents) > 0 {
			var link string
			for _, candidate := range info.Torrents {
				torrent, link, err = watchStartTorrent(ctx, cli, candidate.InfoHash, info.Mode, step)
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
		if !b.watch.open(r) || r.gen != gen {
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
		r.audioReleases = info.AudioVariants
		r.provider, r.providerID = info.Provider, info.ProviderID
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
		// Inspect the exact stream being relayed for embedded HLS languages.
		// This runs after playback starts and never delays the first frame.
		if media := r.media; media != nil && media.link.isHLS() {
			b.watch.bg.Go(func() { b.findWatchHLSTracks(ctx, r, gen, media) })
		}
		// Some providers expose Sub and Dub as separate releases rather than
		// multiple audio tracks. Look up the other version without delaying
		// the first viewer; only reuse directly playable links here. The
		// lookups run one after another, so a small host runs one CLI at a
		// time, and each is skipped once an earlier one answered it.
		other := "dub"
		if r.dub {
			other = "sub"
		}
		_, haveOther := r.audioEntries[other]
		languages, providerID := r.provider == "kickassanime", r.providerID
		last := show.lastEpisode()
		b.watch.bg.Go(func() {
			if languages {
				// Its language pages usually carry the other version too.
				b.findWatchLanguages(ctx, r, gen, cli, show.ID, ep, adult, dub, providerID)
			}
			if !haveOther && !b.watchHasAudio(r, gen, other) {
				b.findWatchAlternate(ctx, r, gen, cli, show.ID, ep, adult, other == "dub")
			}
			// The next episode is looked up ahead, so it starts at once.
			if ep < last && ctx.Err() == nil {
				watchAhead(func() { b.watch.infos.get(context.Background(), cli, show.ID, ep+1, dub, adult) })
			}
		})
	})
}

// watchHasAudio reports whether the room's current episode already offers
// audio version mode.
func (b *bot) watchHasAudio(room *watchRoom, gen int, mode string) bool {
	b.watch.mu.Lock()
	defer b.watch.mu.Unlock()
	if !b.watch.open(room) || room.gen != gen {
		return true // nothing to look up for an episode no longer playing
	}
	_, ok := room.audioEntries[mode]
	return ok
}

// findWatchHLSTracks advertises the selected stream's real audio renditions.
// Each language gets its own relay URL with the correct default track, so
// native HLS playback works even without browser audio-track enumeration.
func (b *bot) findWatchHLSTracks(ctx context.Context, room *watchRoom, gen int, media *watchMedia) {
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	master, _, err := media.fetch(probeCtx, media.link.URL, 4<<20)
	if err == nil && media.link.FlixCloudKey != "" {
		master, err = flixDecodeManifest(master, media.link.FlixCloudKey)
	}
	if err != nil {
		if probeCtx.Err() == nil {
			log.Printf("watch HLS audio discovery: %v", err)
		}
		return
	}
	languages := hlsAvailableAudio(string(master))
	if len(languages) == 0 || probeCtx.Err() != nil {
		return
	}
	b.watch.mu.Lock()
	defer b.watch.mu.Unlock()
	if !b.watch.open(room) || room.gen != gen || room.phase != "play" || room.media != media {
		return
	}
	added := false
	for _, language := range languages {
		if room.audioEntries[language] != "" {
			continue
		}
		stream, entry, _ := newWatchMedia(media.link, media.dub)
		stream.audio = language
		if room.extraAudioMedia == nil {
			room.extraAudioMedia = map[string]*watchMedia{}
		}
		room.extraAudioMedia[language] = stream
		room.audioEntries[language] = watchMediaPath(stream.key, entry)
		added = true
	}
	if added {
		log.Printf("watch HLS audio: %s episode %d offers %s", room.provider, room.ep, strings.Join(languages, ", "))
		room.changed()
	}
}

// findWatchLanguages looks up other playable languages after the episode
// starts. Results belong only to the room generation that requested them.
func (b *bot) findWatchLanguages(ctx context.Context, room *watchRoom, gen int, cli string, showID, ep int, adult, dub bool, providerID string) {
	var info streamInfo
	err := errWatchCLITooOld
	if providerID != "" {
		info, err = watchResolveLanguages(ctx, cli, "kickassanime", providerID, ep)
	}
	if errors.Is(err, errWatchCLITooOld) {
		// Without the show's id, or with an older CLI, search it again.
		info, err = watchResolveAudio(ctx, cli, showID, ep, dub, adult)
	}
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("watch audio discovery for %d:%d: %v", showID, ep, err)
		}
		return
	}
	if ctx.Err() != nil || info.Provider != "kickassanime" {
		return
	}
	b.watch.mu.Lock()
	defer b.watch.mu.Unlock()
	if !b.watch.open(room) || room.gen != gen || room.phase != "play" || room.media == nil || room.provider != info.Provider {
		return
	}
	if room.audioReleases == nil {
		room.audioReleases = map[string]streamLink{}
	}
	for language, link := range info.AudioVariants {
		duplicate := false
		for _, active := range room.streams {
			duplicate = duplicate || active.URL == link.URL
		}
		if !duplicate {
			room.audioReleases[language] = link
		}
	}
	if room.addAudioReleases() {
		room.changed()
	}
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
	if dub {
		mode = "dub"
	}
	b.watch.mu.Lock()
	defer b.watch.mu.Unlock()
	if !b.watch.open(room) || room.gen != gen || room.phase != "play" || room.media == nil {
		return
	}
	// Skip duplicate releases and use the first distinct playable URL.
	selected := -1
	for i, candidate := range info.Streams {
		if candidate.URL == "" {
			continue
		}
		duplicate := false
		for _, active := range room.streams {
			if active.URL == candidate.URL {
				duplicate = true
				break
			}
		}
		if !duplicate {
			selected = i
			break
		}
	}
	if selected < 0 {
		return
	}
	media, entry, _ := newWatchMedia(info.Streams[selected], dub)
	room.alternateMedia = media
	room.alternateOrigin = &watchPlaybackOrigin{Provider: info.Provider, Host: watchPlaybackHost(info.Streams[selected].URL), Link: selected + 1, Mode: mode}
	if room.audioEntries == nil {
		room.audioEntries = map[string]string{}
	}
	current := "sub"
	if room.dub {
		current = "dub"
	}
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
	if !r.spent && r.perks == "" && !b.spendTicket(r.owner) {
		r.finishMedia()
		r.phase, r.errText = "error", "The member who opened this room has no watch ticket left."
		r.changed()
		return false
	}
	now := watchNow()
	if r.started.IsZero() {
		r.started = now
	}
	r.spent = r.perks == ""
	r.finishMedia()
	r.link = k
	m, entry, subIDs := newWatchMedia(r.streams[k], r.dub)
	r.media, r.entry, r.subs = m, entry, nil
	r.audioEntries = nil
	if u, err := url.Parse(r.streams[k].URL); err == nil && u.Hostname() == "127.0.0.1" && u.Query().Get("languages") != "" {
		r.audioEntries = map[string]string{}
		for _, language := range strings.Split(u.Query().Get("languages"), ",") {
			if !watchAudioLanguage(language) {
				continue
			}
			variant := *u
			variant.Path = "/" + language + "/playlist.m3u8"
			variant.RawQuery = ""
			r.audioEntries[language] = watchMediaPath(m.key, m.add(variant.String(), "list"))
		}
	}
	// A muxed video may have audio without advertising separate tracks.
	// Always expose the current release's language to each viewer.
	if r.audioEntries == nil {
		r.audioEntries = make(map[string]string)
	}
	currentMode := "sub"
	if r.dub {
		currentMode = "dub"
	}
	if _, ok := r.audioEntries[currentMode]; !ok {
		r.audioEntries[currentMode] = watchMediaPath(m.key, entry)
	}
	r.addAudioReleases()
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
	r.holdAt(r.startAt, watchReadyFirst, now)
	r.changed()
	return true
}

// addAudioReleases registers separate audio releases for independent viewers.
// The room lock must be held by the caller.
func (r *watchRoom) addAudioReleases() bool {
	if r.media == nil {
		return false
	}
	added := false
	for language, link := range r.audioReleases {
		if !watchAudioLanguage(language) || link.URL == "" || link.URL == r.streams[r.link].URL {
			continue
		}
		if _, exists := r.audioEntries[language]; exists {
			continue
		}
		other, otherEntry, _ := newWatchMedia(link, language == "en")
		if r.extraAudioMedia == nil {
			r.extraAudioMedia = map[string]*watchMedia{}
		}
		r.extraAudioMedia[language] = other
		path := watchMediaPath(other.key, otherEntry)
		r.audioEntries[language] = path
		added = true
		// Legacy selections and background sub/dub discovery use these aliases.
		alias := ""
		if language == "ja" {
			alias = "sub"
		} else if language == "en" {
			alias = "dub"
		}
		if alias != "" {
			if _, exists := r.audioEntries[alias]; !exists {
				r.audioEntries[alias] = path
			}
		}
	}
	return added
}

// Only allow audio codes emitted by the helper, never arbitrary URL paths.
func watchAudioLanguage(code string) bool {
	if code == "sub" || code == "dub" {
		return true
	}
	if len(code) == 2 {
		for _, ch := range code {
			if ch < 'a' || ch > 'z' {
				return false
			}
		}
		return true
	}
	if strings.HasPrefix(code, "track-") {
		ordinal := strings.TrimPrefix(code, "track-")
		if len(ordinal) == 0 || len(ordinal) > 3 {
			return false
		}
		for _, ch := range ordinal {
			if ch < '0' || ch > '9' {
				return false
			}
		}
		return true
	}
	return false
}

func watchMediaPath(key, id string) string { return "api/watch/media/" + key + "/" + id }

// watchCountsAsWatched is how many seconds must play before a room keeps
// the ticket it spent.
const watchCountsAsWatched = 30

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

// watchMax is how many can be in a room (/settings watch_room_max, or
// watch_room_max_perks and watch_room_max_champion for one opened by staff
// or the Game Champion). Staff and the member who opened it always get in.
func (b *bot) watchMax(perks string) int {
	switch perks {
	case "staff":
		return b.settingInt("watch_room_max_perks")
	case "champion":
		return b.settingInt("watch_room_max_champion")
	}
	return b.settingInt("watch_room_max")
}

// watchPerks is what user's rooms get: "staff", "champion" or nothing.
func (b *bot) watchPerks(user string) string {
	switch {
	case b.isAdmin(user):
		return "staff"
	case b.isChampion(user):
		return "champion"
	}
	return ""
}

// ends is when the room's time is up; zero before it started.
func (r *watchRoom) ends() time.Time {
	switch {
	case r.started.IsZero():
		return time.Time{}
	case r.perks != "":
		return r.started.Add(watchPerkLength)
	}
	return r.started.Add(watchRoomLength)
}

// isChampion reports whether user holds this week's Game Champion role.
func (b *bot) isChampion(user string) bool {
	champ := false
	b.store.view(func(d *storeData) { champ = contains(d.Champions, user) })
	return champ
}

func (b *bot) watchTickets(user string) int {
	n := 0
	b.store.view(func(d *storeData) { n = d.WatchTickets[user] })
	return n
}

// ── What the page gets ──

// memberState is how a member's player is doing, for the people bar. Hold
// the lock.
func (r *watchRoom) memberState(id string, now time.Time) (string, int) {
	m := r.members[id]
	if r.phase != "play" || m == nil {
		return "", 0
	}
	if r.hold != 0 {
		if r.ready[id] {
			return "watching", 0
		}
		return "loading", 0
	}
	if m.beatAt.IsZero() || now.Sub(m.beatAt) > 12*time.Second {
		return "", 0
	}
	if m.stuck {
		return "loading", 0
	}
	if behind := r.position(now) - m.pos; behind > 5 {
		return "behind", int(behind)
	}
	return "watching", 0
}

// memberStateOf is memberState's first result, for tests.
func (r *watchRoom) stateOnly(id string, now time.Time) string {
	st, _ := r.memberState(id, now)
	return st
}

type watchPerson struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Host bool   `json:"host,omitempty"`
	// State is how this person's player is doing, as it last reported:
	// "watching", "loading" (it has the episode but not the spot yet, or
	// it is buffering) or "behind" (more than a few seconds behind the
	// room). Empty means it hasn't said anything yet.
	State  string `json:"state,omitempty"`
	Behind int    `json:"behind,omitempty"` // whole seconds, when behind
}

// These details are only returned to the room's developers and administrators.
// Never send raw stream URLs, headers or credentials to the Activity.
type watchPlaybackOrigin struct {
	Provider string `json:"provider"`
	Host     string `json:"host,omitempty"`
	Link     int    `json:"link,omitempty"`
	Mode     string `json:"mode,omitempty"`
}

func watchPlaybackHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "127.0.0.1", "::1", "localhost":
		return "Local relay"
	}
	return host
}

// Source diagnostics are restricted to the guild owner, administrators and
// developer/maintainer roles. Moderation-only permissions do not grant access.
func (b *bot) watchCanInspectPlayback(user string) bool {
	if user == "" || b.s == nil || b.cfg.GuildID == "" {
		return false
	}
	guild, err := b.s.State.Guild(b.cfg.GuildID)
	if err != nil || guild == nil || len(guild.Roles) == 0 {
		guild, err = b.s.Guild(b.cfg.GuildID)
		if err != nil || guild == nil {
			return false
		}
	}
	if guild.OwnerID == user {
		return true
	}
	member, err := b.s.State.Member(b.cfg.GuildID, user)
	if err != nil || member == nil {
		member, err = b.s.GuildMember(b.cfg.GuildID, user)
		if err != nil || member == nil {
			return false
		}
	}
	for _, id := range member.Roles {
		for _, role := range guild.Roles {
			if role.ID != id {
				continue
			}
			if role.Permissions&(discordgo.PermissionAdministrator|discordgo.PermissionManageGuild) != 0 ||
				strings.EqualFold(role.Name, "Developer") ||
				strings.EqualFold(role.Name, "Maintainer") {
				return true
			}
		}
	}
	return false
}

type watchView struct {
	Ver             int                             `json:"ver"`
	Room            string                          `json:"room"`
	Me              string                          `json:"me"`
	Host            string                          `json:"host"`
	IsHost          bool                            `json:"is_host"`
	Staff           bool                            `json:"staff,omitempty"`
	Phase           string                          `json:"phase"`
	LoadStep        string                          `json:"load_step,omitempty"`
	LoadSince       int64                           `json:"load_since,omitempty"`
	Err             string                          `json:"err,omitempty"`
	Adult           bool                            `json:"adult,omitempty"`
	Show            *watchShow                      `json:"show,omitempty"`
	Ep              int                             `json:"ep,omitempty"`
	LastEp          int                             `json:"last_ep,omitempty"`
	Dub             bool                            `json:"dub,omitempty"`
	Media           string                          `json:"media,omitempty"`
	HLS             bool                            `json:"hls,omitempty"`
	AudioMedia      map[string]string               `json:"audio_media,omitempty"`
	AutoSkipOP      bool                            `json:"auto_skip_op"`
	AutoSkipED      bool                            `json:"auto_skip_ed"`
	SkipReady       bool                            `json:"skip_ready"`
	SkipTimes       watchSkipTimes                  `json:"skip_times"`
	SkipSeq         int                             `json:"skip_seq"`
	AudioOrigins    map[string]*watchPlaybackOrigin `json:"audio_origins,omitempty"`
	Origin          *watchPlaybackOrigin            `json:"origin,omitempty"`
	AlternateOrigin *watchPlaybackOrigin            `json:"alternate_origin,omitempty"`
	Subs            []watchSub                      `json:"subs,omitempty"`
	Playing         bool                            `json:"playing"`
	Pos             float64                         `json:"pos"`
	Dur             float64                         `json:"dur,omitempty"`
	Ends            int64                           `json:"ends,omitempty"` // unix ms, once started
	Members         []watchPerson                   `json:"members"`
	Asks            []watchAsk                      `json:"asks"`
	Chat            []watchChat                     `json:"chat"`
	Events          []watchEvent                    `json:"events,omitempty"`
	Vote            *watchVote                      `json:"vote,omitempty"`
	CanVote         bool                            `json:"can_vote,omitempty"`
	CountAsk        int                             `json:"count_ask,omitempty"` // an episode to offer counting, for a late joiner
	Hold            int                             `json:"hold,omitempty"`
	HoldWaiting     []string                        `json:"hold_waiting,omitempty"`
	HoldEnds        int64                           `json:"hold_ends,omitempty"` // unix ms
	HoldReady       bool                            `json:"hold_ready,omitempty"`
	Sync            bool                            `json:"sync"`
	Linked          []string                        `json:"linked,omitempty"`
	Languages       watchLanguagePreference         `json:"languages"`
	Preferences     watchPlaybackPreferences        `json:"preferences"`
	Max             int                             `json:"max"`
	// CDN is the edge cache's path when one is set up (watchedge.go), so
	// a player can tell its segments failing there.
	CDN string `json:"cdn,omitempty"`
}

// viewOf is the room as user sees it. Hold the room lock; sync and linked
// come from the store, read before taking it.
func (b *bot) watchViewOf(r *watchRoom, user string, staff bool, now time.Time) watchView {
	v := watchView{Ver: r.ver, Room: r.id, Me: user, Host: r.host, IsHost: r.host == user, Staff: staff, Phase: r.phase,
		Err: r.errText, Adult: r.adult, Show: r.show, Ep: r.ep, Dub: r.dub, Playing: r.playing, Pos: r.position(now),
		Dur: r.dur, AutoSkipOP: r.autoSkipOP, AutoSkipED: r.autoSkipED,
		SkipReady: r.skipReady, SkipTimes: r.skipIntervals.match(r.dur), SkipSeq: r.skipSeq,
		Members: []watchPerson{}, Asks: []watchAsk{}, Chat: slices.Clone(r.chat), Max: b.watchMax(r.perks),
		CDN: strings.TrimRight(b.cfg.WatchCDN, "/")}
	if v.Chat == nil {
		v.Chat = []watchChat{}
	}
	v.Events = slices.Clone(r.events)
	if r.hold != 0 {
		v.Hold, v.HoldWaiting, v.HoldEnds, v.HoldReady = r.hold, r.holdWaiting(), r.holdUntil.UnixMilli(), r.ready[user]
	}
	if _, _, inside := r.watchVoteSpan(now); inside {
		v.CanVote = true
	}
	if r.vote != nil {
		vote := *r.vote
		vote.Votes, vote.Need, vote.Mine = len(r.vote.who), watchVoteNeed(len(r.members)), r.vote.who[user]
		v.Vote = &vote
	}
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
		if r.phase == "play" && len(r.extraAudioMedia) > 0 {
			v.AudioOrigins = make(map[string]*watchPlaybackOrigin, len(r.extraAudioMedia))
			for lang := range r.extraAudioMedia {
				link := r.audioReleases[lang].URL
				if link == "" && r.link >= 0 && r.link < len(r.streams) {
					link = r.streams[r.link].URL
				}
				v.AudioOrigins[lang] = &watchPlaybackOrigin{Provider: r.provider,
					Host: watchPlaybackHost(link), Mode: lang}
			}
		}
		if r.phase == "play" && r.link >= 0 && r.link < len(r.streams) {
			v.Origin = &watchPlaybackOrigin{Provider: r.provider, Host: watchPlaybackHost(r.streams[r.link].URL), Link: r.link + 1}
			v.AlternateOrigin = r.alternateOrigin
		}
	}
	if e := r.ends(); !e.IsZero() {
		v.Ends = e.UnixMilli()
	}
	for _, id := range r.people() {
		p := watchPerson{ID: id, Name: r.members[id].name, Host: id == r.host}
		p.State, p.Behind = r.memberState(id, now)
		v.Members = append(v.Members, p)
	}
	v.CountAsk = r.countAsk(user, now)
	for _, a := range r.asks {
		if v.IsHost || a.User == user {
			a.Left = max(0, (watchAskTTL - now.Sub(a.At)).Milliseconds())
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
			if watchValidLanguage(p.Audio, false) {
				v.Languages.Audio = p.Audio
			}
			if watchValidLanguage(p.Subtitles, true) {
				v.Languages.Subtitles = p.Subtitles
			}
		}
		v.Preferences = watchNormalizePreferences(d.WatchPreferences[user])
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
	apiHandle("POST /activity/api/watch/ready", (*bot).watchReady)
	apiHandle("POST /activity/api/watch/vote", (*bot).watchVoteAPI)
	apiHandle("POST /activity/api/watch/host", (*bot).watchHandOver)
	apiHandle("POST /activity/api/watch/chat", (*bot).watchChatSay)
	apiHandle("POST /activity/api/watch/answer", (*bot).watchAnswer)
	apiHandle("POST /activity/api/watch/take", (*bot).watchTake)
	apiHandle("POST /activity/api/watch/sync", (*bot).watchSync)
	apiHandle("POST /activity/api/watch/count", (*bot).watchCount)
	apiHandle("POST /activity/api/watch/languages", (*bot).watchLanguageSettings)
	apiHandle("GET /activity/api/watch/preferences", (*bot).watchPreferencesGet)
	apiHandle("POST /activity/api/watch/preferences", (*bot).watchPreferencesSet)
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
	Room      string  `json:"room"`
	Channel   string  `json:"channel"`
	Show      int     `json:"show"`
	Ep        int     `json:"ep"`
	Dub       bool    `json:"dub"`
	Audio     string  `json:"audio"`
	Subtitles string  `json:"subtitles"`
	Act       string  `json:"act"`
	Pos       float64 `json:"pos"`
	Playing   bool    `json:"playing"`
	Buffering bool    `json:"buffering"` // the player means to play but is still loading
	Dur       float64 `json:"dur"`
	Kind      string  `json:"kind"`
	By        float64 `json:"by"` // an asked jump: seconds ahead, or back when below 0
	ID        int     `json:"id"`
	OK        bool    `json:"ok"`
	On        bool    `json:"on"`
	Why       string  `json:"why"` // why the host's player gave up, for the log
	Text      string  `json:"text"`
	User      string  `json:"user"` // the member an invite or a handover names
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
	if b.acts.user(r) == "" {
		b.watch.mu.Lock()
		allowed := b.watchAppAccess(w, r, b.watch.room(in.Room), user)
		b.watch.mu.Unlock()
		if !allowed {
			return "", in, false
		}
	}
	return user, in, true
}

// inRoom is the current room when it is this one and user is in it. Hold
// the lock.
func (b *bot) inRoom(w http.ResponseWriter, id, user string) *watchRoom {
	r := b.watch.room(id)
	if r == nil {
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
	if !b.watchCanInspectPlayback(user) {
		v.Origin, v.AlternateOrigin, v.AudioOrigins = nil, nil, nil
	}
	writeJSON(w, http.StatusOK, v)
}

// watchStatus is what the tile and the lobby show: the room in this call,
// the rooms running in other calls, whether there's space for another one,
// and the member's tickets.
func (b *bot) watchStatus(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	room := r.URL.Query().Get("room")
	perks := b.watchPerks(user)
	most := b.settingInt("watch_rooms_max")
	out := map[string]any{"tickets": b.watchTickets(user), "max": b.watchMax(perks), "perks": perks, "rooms_max": most}
	others := []map[string]any{}
	b.watch.mu.Lock()
	open := b.watch.list()
	for _, cur := range open {
		card := b.watchCard(cur)
		if b.acts.user(r) == "" && cur.adult && b.watchAppInChannel(user, cur.channel) {
			card["id"] = cur.id
		}
		if cur.id == room {
			maps.Copy(out, card)
			out["here"] = true
			continue
		}
		others = append(others, card)
	}
	b.watch.mu.Unlock()
	out["busy"] = len(open) > 0
	out["rooms"] = others
	out["full"] = out["here"] != true && len(open) >= most
	writeJSON(w, http.StatusOK, out)
}

// watchCard is one running room as the tile and the lobby show it, with its
// id so the lobby can join it from another call and a link to its voice
// channel. An 18+ room stays private: anyone can see that a room runs
// somewhere, but not what or who, and it can't be joined from outside its
// channel. Hold the lock.
func (b *bot) watchCard(cur *watchRoom) map[string]any {
	out := map[string]any{"channel": cur.channel, "people": len(cur.members), "max": b.watchMax(cur.perks)}
	if c := watchChannel(b, cur.channel); c != nil {
		out["channel_name"] = c.Name
	}
	if cur.adult {
		out["adult"] = true
	} else {
		out["id"] = cur.id
		if b.cfg.GuildID != "" && cur.channel != "" {
			out["link"] = "https://discord.com/channels/" + b.cfg.GuildID + "/" + cur.channel
		}
		if cur.show != nil {
			out["show"] = cur.show.Title
			out["cover"] = cur.show.Cover
			out["ep"] = cur.ep
			out["last_ep"] = cur.show.lastEpisode()
			out["playing"] = cur.phase == "play" && (cur.playing || cur.hold != 0)
		}
		if h, ok := cur.members[cur.host]; ok {
			out["host"] = h.name
		}
	}
	if e := cur.ends(); !e.IsZero() {
		out["ends"] = e.UnixMilli()
	}
	return out
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
	perks := b.watchPerks(user)
	var prefs watchPlaybackPreferences
	b.store.view(func(d *storeData) { prefs = watchNormalizePreferences(d.WatchPreferences[user]) })
	ch := watchChannel(b, in.Channel)
	adult := ch != nil && ch.NSFW
	now := watchNow()
	b.watch.mu.Lock()
	cur := b.watch.room(in.Room)
	if !b.watchAppAccess(w, r, cur, user) {
		b.watch.mu.Unlock()
		return
	}
	switch {
	case cur == nil && len(b.watch.rooms) >= b.settingInt("watch_rooms_max"):
		n := len(b.watch.rooms)
		b.watch.mu.Unlock()
		apiError(w, http.StatusConflict, fmt.Sprintf("All %d watch rooms are in use. Join one of those calls, or try again once one ends.", n))
		return
	case cur != nil:
		// A room in another call can be joined from the lobby, except an
		// 18+ one: that stays with whoever is in its age-restricted channel.
		if _, ok := cur.members[user]; !ok && cur.adult && in.Channel != cur.channel {
			b.watch.mu.Unlock()
			apiError(w, http.StatusForbidden, "That room is private. Join its voice channel to watch along.")
			return
		}
		if _, ok := cur.members[user]; !ok && len(cur.members) >= b.watchMax(cur.perks) && user != cur.owner && !staff {
			b.watch.mu.Unlock()
			apiError(w, http.StatusForbidden, fmt.Sprintf("The watch room is full (%d people).", b.watchMax(cur.perks)))
			return
		}
	default:
		if tickets < 1 && perks == "" {
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
			phase: "pick", members: map[string]*watchMember{}, perks: perks, autoSkipOP: prefs.SkipOP, autoSkipED: prefs.SkipED}
		b.watch.add(cur)
		watchClock(b, cur)
		log.Printf("watch room opened by member %s in channel %s", user, in.Channel)
	}
	if m, ok := cur.members[user]; ok {
		m.seen = now
	} else {
		m := &watchMember{name: name, joined: now, seen: now}
		if cur.phase == "play" {
			m.from = cur.position(now)
		}
		cur.members[user] = m
		if len(cur.members) > 1 {
			cur.note("%s joined", name)
		}
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
		cur := b.watch.room(id)
		if cur == nil {
			b.watch.mu.Unlock()
			writeJSON(w, http.StatusOK, watchView{Room: id, Me: user, Phase: "over", Members: []watchPerson{}, Asks: []watchAsk{}, Chat: []watchChat{}})
			return
		}
		m, in := cur.members[user]
		if !b.watchAppAccess(w, r, cur, user) {
			b.watch.mu.Unlock()
			return
		}
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
	out := map[string]any{"ok": true}
	b.watch.mu.Lock()
	if cur := b.watch.room(in.Room); cur != nil {
		if _, ok := cur.members[user]; ok {
			now := watchNow()
			if p, ok := cur.resumeOf(user, now); ok {
				cur.resumes = append(cur.resumes, p)
				out["ep"], out["secs"], out["done"] = p.ep, p.secs, p.done
			} else if ep := cur.countAsk(user, now); ep > 0 {
				// Came in late but stayed to near the end: the leave
				// screen asks whether to count it.
				b.watch.offer(user, watchResume{user: user, show: *cur.show, ep: ep, done: true, count: true}, now)
				out["ep"], out["ask"] = ep, true
			}
			cur.note("%s left", cur.name(user))
			delete(cur.members, user)
			cur.prune(now)
			cur.changed()
			b.flushResumes(cur)
		}
	}
	b.watch.mu.Unlock()
	writeJSON(w, http.StatusOK, out)
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
	if cur := b.watch.room(r.URL.Query().Get("room")); cur != nil {
		adult = cur.adult
	}
	b.watch.mu.Unlock()
	list, err := watchSearch(q, adult)
	if err != nil {
		log.Printf("watch search: %v", err)
		apiError(w, http.StatusBadGateway, "couldn't reach AniList, try again in a bit")
		return
	}
	if isWatchMovieQuery(q) {
		list = append([]watchShow{watchMovieShow()}, list...)
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
		until    time.Time
		previews map[int]watchEpisodePreview
	}
}{entries: make(map[int]struct {
	until    time.Time
	previews map[int]watchEpisodePreview
})}

func (b *bot) watchEpisodes(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	if !watchSearchLimit.allow(user, watchNow()) {
		apiError(w, http.StatusTooManyRequests, "slow down")
		return
	}
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id < 1 {
		apiError(w, http.StatusBadRequest, "invalid anime")
		return
	}
	show, err := watchShowByID(id)
	if err != nil {
		apiError(w, http.StatusBadGateway, "anime unavailable")
		return
	}
	if show.Adult {
		b.watch.mu.Lock()
		cur := b.watch.room(r.URL.Query().Get("room"))
		allowed := cur != nil && cur.adult
		b.watch.mu.Unlock()
		if !allowed {
			apiError(w, http.StatusForbidden, "not available in this room")
			return
		}
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
				if err != nil {
					break
				}
				resp, err := activityClient.Do(req)
				if err != nil {
					break
				}
				if resp.StatusCode != http.StatusOK {
					resp.Body.Close()
					break
				}
				var payload struct {
					Data []struct {
						MalID  int    `json:"mal_id"`
						Title  string `json:"title"`
						Images struct {
							JPG struct {
								ImageURL string `json:"image_url"`
							} `json:"jpg"`
							WebP struct {
								ImageURL string `json:"image_url"`
							} `json:"webp"`
						} `json:"images"`
						ImageURL string `json:"image_url"`
					} `json:"data"`
					Pagination struct {
						HasNextPage bool `json:"has_next_page"`
					} `json:"pagination"`
				}
				err = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload)
				resp.Body.Close()
				if err != nil {
					break
				}
				for _, ep := range payload.Data {
					if ep.MalID < 1 || ep.MalID > show.lastEpisode() {
						continue
					}
					img := ep.Images.JPG.ImageURL
					if img == "" {
						img = ep.Images.WebP.ImageURL
					}
					if img == "" {
						img = ep.ImageURL
					}
					// The image proxy only permits known hosts, so never send arbitrary URLs.
					if strings.HasPrefix(img, "https://img1.ak.crunchyroll.com/") ||
						strings.HasPrefix(img, "https://static.crunchyroll.com/") ||
						strings.HasPrefix(img, "https://s4.anilist.co/") {
						img = "api/watch/cover?u=" + url.QueryEscape(img)
					} else {
						img = ""
					}
					tenrai[ep.MalID] = watchEpisodePreview{Title: ep.Title, Thumbnail: img}
				}
				if !payload.Pagination.HasNextPage || len(payload.Data) == 0 {
					break
				}
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
				if err != nil {
					break
				}
				resp, err := activityClient.Do(req)
				if err != nil {
					break
				}
				if resp.StatusCode != http.StatusOK {
					resp.Body.Close()
					break
				}
				var payload struct {
					Data []struct {
						MalID         int    `json:"mal_id"`
						Title         string `json:"title"`
						TitleJapanese string `json:"title_japanese"`
					} `json:"data"`
					Pagination struct {
						HasNextPage bool `json:"has_next_page"`
					} `json:"pagination"`
				}
				err = json.NewDecoder(http.MaxBytesReader(w, resp.Body, 2<<20)).Decode(&payload)
				resp.Body.Close()
				if err != nil {
					break
				}
				for _, ep := range payload.Data {
					if ep.MalID < 1 || ep.MalID > show.lastEpisode() {
						continue
					}
					current := jikan[ep.MalID]
					if current.Title == "" {
						current.Title = ep.Title
						if current.Title == "" {
							current.Title = ep.TitleJapanese
						}
						jikan[ep.MalID] = current
					}
				}
				if !payload.Pagination.HasNextPage || len(payload.Data) == 0 {
					break
				}
			}
		}
	}()
	go func() {
		defer wg.Done()
		extraEpisodePreviews(ctx, show, kitsu)
	}()
	wg.Wait()
	result := make(map[int]watchEpisodePreview)
	for ep, v := range kitsu {
		result[ep] = v
	}
	for ep, v := range jikan {
		old := result[ep]
		if v.Title != "" {
			old.Title = v.Title
		}
		result[ep] = old
	}
	for ep, v := range tenrai {
		old := result[ep]
		if v.Title != "" {
			old.Title = v.Title
		}
		if v.Thumbnail != "" {
			old.Thumbnail = v.Thumbnail
		}
		result[ep] = old
	}
	if len(result) > 0 {
		episodePreviewCache.Lock()
		if len(episodePreviewCache.entries) >= 256 {
			clear(episodePreviewCache.entries)
		}
		episodePreviewCache.entries[id] = struct {
			until    time.Time
			previews map[int]watchEpisodePreview
		}{until: time.Now().Add(30 * time.Minute), previews: result}
		episodePreviewCache.Unlock()
	}
	writeJSON(w, http.StatusOK, result)
}

// watchPick starts an episode (host only).
func (b *bot) watchPick(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.watchRead(w, r)
	if !ok {
		return
	}
	show, err := watchMovieShow(), error(nil)
	if in.Show != watchMovieID {
		show, err = watchShowByID(in.Show)
	}
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
	case !cur.ends().IsZero() && watchNow().After(cur.ends()):
		b.watch.mu.Unlock()
		apiError(w, http.StatusForbidden, "the watch room's time is up")
		return
	}
	// Pos is a saved position to resume from, picked from the host's
	// Continue list.
	start := in.Pos
	if math.IsNaN(start) || start < watchResumeMin || start > 6*3600 {
		start = 0
	}
	b.loadEpisode(cur, show, in.Ep, in.Dub, start)
	cur.note("%s put on %s, episode %d", cur.name(user), show.Title, in.Ep)
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
	if in.Act == "broken" && cur.phase == "play" {
		why := watchHelperLine(in.Why, 300)
		if why == "" {
			why = "no reason given"
		}
		log.Printf("watch room: the host's player gave up on %s episode %d, link %d: %s", cur.provider, cur.ep, cur.link+1, why)
	}
	if err := b.watchDo(cur, in.Act, in.Pos); err != "" {
		b.watch.mu.Unlock()
		apiError(w, http.StatusBadRequest, err)
		return
	}
	cur.noteControl(user, in.Act)
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
		r.unhold()
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
		if act == "play" && r.hold != 0 {
			// Play while the room waits for everyone: the host starts it now.
			r.release(now)
			return ""
		}
		playing := r.playing || r.hold != 0
		if act != "seek" {
			playing = act == "play"
		}
		r.unhold()
		if playing {
			r.holdAt(pos, watchReadyWait, now)
		} else {
			r.setPos(pos, false, now)
		}
	case "forward":
		if r.phase != "play" {
			return "nothing is playing"
		}
		p := r.position(now) + 10
		if r.hold != 0 {
			p = r.holdPos + 10
		}
		if r.dur > 0 {
			p = math.Min(p, math.Max(0, r.dur-0.5))
		}
		if r.playing || r.hold != 0 {
			r.holdAt(p, watchReadyWait, now)
		} else {
			r.setPos(p, false, now)
		}
	case "op-on", "op-off", "ed-on", "ed-off":
		if r.phase != "play" {
			return "nothing is playing"
		}
		switch act {
		case "op-on":
			r.autoSkipOP = true
		case "op-off":
			r.autoSkipOP = false
		case "ed-on":
			r.autoSkipED = true
		case "ed-off":
			r.autoSkipED = false
		}
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
		if !r.ends().IsZero() && now.After(r.ends()) {
			return "the watch room's time is up"
		}
		b.loadEpisode(r, *r.show, ep, r.dub, 0)
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
	now := watchNow()
	if m := cur.members[user]; m != nil && cur.phase == "play" && !math.IsNaN(in.Pos) && in.Pos >= 0 {
		was, wasStuck := m.pos, m.stuck
		m.pos, m.stuck, m.beatAt = in.Pos, in.Buffering || !in.Playing && cur.playing, now
		// The people bar only has to hear about a real change of state.
		if wasStuck != m.stuck || math.Abs(was-in.Pos) > 5 {
			cur.changed()
		}
	}
	if cur.host != user || cur.phase != "play" || math.IsNaN(in.Pos) || in.Pos < 0 {
		writeJSON(w, http.StatusOK, map[string]int{"ver": cur.ver})
		return
	}
	if in.Dur > 0 && in.Dur < 6*3600 && math.Abs(in.Dur-cur.dur) > 1 {
		cur.dur = in.Dur
		cur.changed()
	}
	// While the room waits for everyone, the host's paused player says
	// nothing the room needs.
	if cur.hold != 0 {
		writeJSON(w, http.StatusOK, map[string]int{"ver": cur.ver})
		return
	}
	// The host may report its old video time while the new auto-skip seek
	// is still on its way through the Activity's state poll. Do not rewind
	// the whole room with that stale heartbeat.
	if now.Before(cur.skipHoldUntil) && in.Pos < cur.skipTarget-1 {
		writeJSON(w, http.StatusOK, map[string]int{"ver": cur.ver})
		return
	}
	if in.Pos >= cur.skipTarget-1 {
		cur.skipHoldUntil = time.Time{}
	}
	if in.Playing && !cur.watched {
		if !cur.beatStart {
			cur.beatFrom, cur.beatStart = in.Pos, true
		} else if in.Pos-cur.beatFrom >= watchCountsAsWatched {
			cur.watched = true
		}
	}
	drift := math.Abs(cur.position(now) - in.Pos)
	moved := in.Playing != cur.playing || drift > watchDrift
	cur.setPos(in.Pos, in.Playing, now)
	if moved {
		cur.changed()
	}
	writeJSON(w, http.StatusOK, map[string]int{"ver": cur.ver})
}

// watchAskAPI is a member asking the host for a pause, play, a jump (ahead
// or back by some seconds, or to a spot) or the next episode. Asking for
// another jump while one waits changes the waiting one, so pressing +10s
// five times asks for +50s.
func (b *bot) watchAskAPI(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.watchRead(w, r)
	if !ok {
		return
	}
	if !slices.Contains([]string{"pause", "play", "forward", "seek", "next"}, in.Kind) {
		apiError(w, http.StatusBadRequest, "unknown request")
		return
	}
	ask := watchAsk{User: user, Kind: in.Kind}
	switch in.Kind {
	case "forward":
		if in.By == 0 {
			in.By = 10
		}
		if math.IsNaN(in.By) {
			apiError(w, http.StatusBadRequest, "that's too far")
			return
		}
		// The -10s/+10s presses add up to a minute either way; further
		// is what tapping the bar is for.
		ask.By = math.Round(math.Max(-watchAskJumpMax, math.Min(watchAskJumpMax, in.By)))
	case "seek":
		if math.IsNaN(in.Pos) || in.Pos < 0 || in.Pos > 6*3600 {
			apiError(w, http.StatusBadRequest, "that's outside the episode")
			return
		}
		ask.Pos = math.Round(in.Pos)
	}
	jump := func(kind string) bool { return kind == "forward" || kind == "seek" }
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
	if jump(in.Kind) && cur.phase != "play" {
		b.watch.mu.Unlock()
		apiError(w, http.StatusBadRequest, "nothing is playing")
		return
	}
	if in.Kind == "seek" && cur.dur > 0 && ask.Pos > cur.dur {
		b.watch.mu.Unlock()
		apiError(w, http.StatusBadRequest, "that's outside the episode")
		return
	}
	for k, a := range cur.asks {
		if a.User != user {
			continue
		}
		if !jump(a.Kind) || !jump(in.Kind) {
			b.watch.mu.Unlock()
			apiError(w, http.StatusTooManyRequests, "wait for the host to answer your last request")
			return
		}
		// The same request, now for somewhere else: it keeps its place
		// and starts its wait again.
		cur.asks[k].Kind, cur.asks[k].By, cur.asks[k].Pos, cur.asks[k].At = ask.Kind, ask.By, ask.Pos, watchNow()
		cur.changed()
		b.watchAnswerView(w, cur, user, staff)
		return
	}
	cur.askN++
	ask.ID, ask.Name, ask.At = cur.askN, cur.members[user].name, watchNow()
	cur.asks = append(cur.asks, ask)
	cur.changed()
	b.watchAnswerView(w, cur, user, staff)
}

// askDo carries out a request the host accepted and says which control it
// was, for the room's events. A jump by some seconds counts from where the
// room is when the host says yes. Hold the lock.
func (b *bot) askDo(r *watchRoom, a watchAsk) (string, string) {
	switch a.Kind {
	case "forward":
		if a.By == 10 {
			return "forward", b.watchDo(r, "forward", -1)
		}
		from := r.position(watchNow())
		if r.hold != 0 {
			from = r.holdPos
		}
		to := math.Max(0, from+a.By)
		if r.dur > 0 {
			to = math.Min(to, math.Max(0, r.dur-0.5))
		}
		return "seek", b.watchDo(r, "seek", to)
	case "seek":
		return "seek", b.watchDo(r, "seek", a.Pos)
	}
	return a.Kind, b.watchDo(r, a.Kind, -1)
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
		if act, err := b.askDo(cur, a); err == "" {
			cur.noteControl(user, act)
		}
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
	cur.note("%s took control", cur.name(user))
	cur.changed()
	b.watchAnswerView(w, cur, user, true)
}

// watchHandOver passes hosting to another member of the room. The host
// does it from the people list; staff can too.
func (b *bot) watchHandOver(w http.ResponseWriter, r *http.Request) {
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
	switch {
	case cur.host != user && !staff:
		b.watch.mu.Unlock()
		apiError(w, http.StatusForbidden, "only the host passes it on")
		return
	case in.User == cur.host:
		b.watch.mu.Unlock()
		apiError(w, http.StatusBadRequest, "they're already the host")
		return
	}
	if _, there := cur.members[in.User]; !there {
		b.watch.mu.Unlock()
		apiError(w, http.StatusNotFound, "they're not in the room any more")
		return
	}
	cur.host = in.User
	cur.note("%s passed hosting to %s", cur.name(user), cur.name(in.User))
	cur.changed()
	log.Printf("watch room: member %s handed hosting to %s", user, in.User)
	b.watchAnswerView(w, cur, user, staff)
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
	for _, cur := range b.watch.rooms {
		for _, c := range []*watchMedia{cur.media, cur.old, cur.alternateMedia} {
			if c != nil && c.key == key {
				m = c
			}
		}
		for _, c := range cur.extraAudioMedia {
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
	m.serveVia(w, r, id, cache, b.watchEdgeFor(r))
}

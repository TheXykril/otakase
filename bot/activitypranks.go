package main

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"
)

// More admin tools for the games Activity: prank switches that change one
// screen until the game ends, prank names, a puppet line in the feed, and
// the moderation steps that keep someone out of a room (spectator, out for
// a while, a locked room), slow answers, hidden Discord names and games
// turned off for the whole server.

const (
	roomOutTime   = 10 * time.Minute
	slowEvery     = 3 * time.Second
	rouletteEvery = 8 * time.Second
)

// prankToggles are the pranks switched on and off per member. The page does
// the rest: a faster timer, Comic Sans and rainbows, text upside down, a
// fake first place, a fish cursor, typing backwards, a pink pen, quacks.
// ghost is on the server: their wrong answers show under someone else.
var prankToggles = map[string]bool{
	"ghost": true, "fast": true, "comic": true, "textflip": true, "fake_top": true,
	"fish": true, "reverse": true, "pink": true, "quack": true,
	// Round three: a fake admin button, slow taps, a keyboard that slips,
	// invisible typing, answer buttons in reverse, a wobbly screen, a
	// spotlight round the pointer and vote counts that lie.
	"fake_admin": true, "lag": true, "cursed": true, "invisible": true, "swapbtn": true,
	"drunk": true, "spotlight": true, "lie_votes": true,
}

// fxKinds are the one-off effects staff can send.
var fxKinds = map[string]bool{
	"confetti": true, "horn": true, "boom": true, "disconnect": true, "tiny": true, "huge": true,
	"fakekick": true, "fakeban": true, "quote": true,
	"fakewin": true, "update": true, "facerain": true, "boo": true, "applause": true, "jumpscare": true,
}

var animeQuotes = []string{
	"People die if they are killed.",
	"I'll take a potato chip... and eat it!",
	"Believe it!",
	"Omae wa mou shindeiru.",
	"It's over 9000!",
	"Just according to keikaku.",
	"Plus Ultra!",
	"I am the bone of my sword.",
	"El Psy Kongroo.",
	"You should enjoy the little detours.",
	"A lesson without pain is meaningless.",
	"Nico Nico Nii~",
}

// sillyNames are prank names when staff don't type one.
var sillyNames = []string{
	"Sir Losesalot", "Baka-chan", "Onigiri Enjoyer", "Filler Arc", "Tsundere Toaster",
	"Plot Armor", "Background Character", "Senpai (didn't notice)", "Mob #47", "Beach Episode",
}

// fxText is the text an effect carries: a quote picks one.
func fxText(kind string) string {
	if kind == "quote" {
		return animeQuotes[rand.IntN(len(animeQuotes))]
	}
	return ""
}

// outside says why user can't play in this room: kicked (until the next
// game), watch (a spectator), room (out for a while) or locked (a locked
// room they weren't in). Hold the lock.
func (r *togetherRoom) outside(user string, now time.Time) string {
	switch {
	case r.kicked[user]:
		return "kicked"
	case r.watchers[user]:
		return "watch"
	case now.Before(r.roomOut[user]):
		return "room"
	}
	if r.locked && !r.admins[user] {
		if _, in := r.members[user]; !in {
			return "locked"
		}
	}
	return ""
}

// cooled lets a slowed member answer once every few seconds. Hold the lock.
func (r *togetherRoom) cooled(user string, now time.Time) error {
	if !r.slow[user] {
		return nil
	}
	if last, ok := r.lastSaid[user]; ok && now.Sub(last) < slowEvery {
		return errPz("an admin slowed you down: one answer every 3 seconds")
	}
	if r.lastSaid == nil {
		r.lastSaid = map[string]time.Time{}
	}
	r.lastSaid[user] = now
	return nil
}

func (r *togetherRoom) togglePrank(user, kind string) {
	if r.toggles == nil {
		r.toggles = map[string]map[string]bool{}
	}
	if r.toggles[user] == nil {
		r.toggles[user] = map[string]bool{}
	}
	if r.toggles[user][kind] {
		delete(r.toggles[user], kind)
	} else {
		r.toggles[user][kind] = true
	}
}

// prankOn lists the prank switches on for user, sorted.
func (r *togetherRoom) prankOn(user string) []string {
	var out []string
	for k, on := range r.toggles[user] {
		if on {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// renamePrank gives user a silly name for this game; the same name again,
// or a blank one while they have a name, takes it off.
func (r *togetherRoom) renamePrank(user, name string) {
	old, had := r.renames[user]
	if had && (name == "" || name == old) {
		delete(r.renames, user)
		return
	}
	if name == "" {
		name = sillyNames[rand.IntN(len(sillyNames))]
	}
	if r.renames == nil {
		r.renames = map[string]string{}
	}
	r.renames[user] = name
}

// roomName is a member's name in a room: a prank name, "Player N" when
// their name is hidden, else their Discord name. Hold the lock.
func (b *bot) roomName(r *togetherRoom, user string) string {
	if r.roulette {
		// Name roulette: everyone wears someone else's name, reshuffled
		// every few seconds.
		now := togetherNow()
		if r.roulMap == nil || now.Sub(r.roulAt) > rouletteEvery {
			r.roulMap, r.roulAt = swapFaces(r.partyPeople(), rand.IntN), now
		}
		if other, ok := r.roulMap[user]; ok && other != user {
			return b.ownName(r, other)
		}
	}
	return b.ownName(r, user)
}

// ownName is roomName without the roulette.
func (b *bot) ownName(r *togetherRoom, user string) string {
	if n, ok := r.renames[user]; ok {
		return n
	}
	hidden := false
	b.store.view(func(d *storeData) { hidden = d.HiddenNames[user] })
	if hidden {
		ids := r.partyPeople()
		if i := slices.Index(ids, user); i >= 0 {
			return "Player " + strconv.Itoa(i+1)
		}
		return "Player"
	}
	return b.dashName(user)
}

// feedName is the name a feed line shows: a ghosted member's wrong answers
// show under someone else in the room. Hold the lock.
func (b *bot) feedName(r *togetherRoom, user string, ok bool) string {
	if !ok && r.toggles[user]["ghost"] {
		var others []string
		for _, id := range r.partyPeople() {
			if id != user {
				others = append(others, id)
			}
		}
		if len(others) > 0 {
			return b.roomName(r, others[rand.IntN(len(others))])
		}
	}
	return b.roomName(r, user)
}

// puppet puts a line in the running game's feed under target's name. It is
// only text: no points, no guess. Hold the lock.
func (b *bot) puppet(r *togetherRoom, target, text string) error {
	if text == "" {
		return errPz("type what they say")
	}
	feed := r.adminFeed()
	if feed == nil {
		return errPz("this game has no feed")
	}
	*feed = append(*feed, togetherFeed{Name: b.roomName(r, target), Text: text})
	if len(*feed) > 10 {
		*feed = (*feed)[len(*feed)-10:]
	}
	return nil
}

// ── Server-wide: hidden names and games turned off ──

// offable are the games staff can turn off.
var offable = func() []string {
	out := []string{"hl"}
	for k := range actWrong {
		out = append(out, k)
	}
	for k := range pzGames {
		if !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	for k := range socialGames {
		if !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}()

func (b *bot) hiddenNames() map[string]bool {
	out := map[string]bool{}
	b.store.view(func(d *storeData) {
		for id, on := range d.HiddenNames {
			if on {
				out[id] = true
			}
		}
	})
	return out
}

// toggleHiddenName hides or shows user's Discord name in the games; true
// when it is hidden now.
func (b *bot) toggleHiddenName(user string) (hidden bool) {
	b.store.update(func(d *storeData) {
		if d.HiddenNames[user] {
			delete(d.HiddenNames, user)
			return
		}
		if d.HiddenNames == nil {
			d.HiddenNames = map[string]bool{}
		}
		d.HiddenNames[user] = true
		hidden = true
	})
	return hidden
}

func (b *bot) gamesOff() []string {
	var out []string
	b.store.view(func(d *storeData) { out = slices.Clone(d.GamesOff) })
	return out
}

func (b *bot) gameOff(game string) bool {
	return slices.Contains(b.gamesOff(), game)
}

// toggleGameOff turns a game off or back on; true when it is off now.
func (b *bot) toggleGameOff(game string) (off bool) {
	b.store.update(func(d *storeData) {
		if i := slices.Index(d.GamesOff, game); i >= 0 {
			d.GamesOff = slices.Delete(d.GamesOff, i, i+1)
			return
		}
		d.GamesOff = append(d.GamesOff, game)
		sort.Strings(d.GamesOff)
		off = true
	})
	return off
}

// ── The admin log: the last steps, for the dashboard ──

type adminLogEntry struct {
	At           time.Time
	By, Kind     string
	Target, Text string
}

var adminLog struct {
	mu sync.Mutex
	l  []adminLogEntry
}

// logAdmin remembers a step taken from the menu or the dashboard (by is ""
// for the dashboard).
func logAdmin(now time.Time, by string, in adminIn) {
	if in.Kind == "points" && in.Text == "" {
		in.Text = fmt.Sprintf("%+d", in.N)
	}
	adminLog.mu.Lock()
	defer adminLog.mu.Unlock()
	adminLog.l = append(adminLog.l, adminLogEntry{At: now, By: by, Kind: in.Kind, Target: in.Target, Text: clip(in.Text, 80)})
	if len(adminLog.l) > 50 {
		adminLog.l = adminLog.l[len(adminLog.l)-50:]
	}
}

// adminLogged is the log, newest first.
func adminLogged() []adminLogEntry {
	adminLog.mu.Lock()
	defer adminLog.mu.Unlock()
	out := slices.Clone(adminLog.l)
	slices.Reverse(out)
	return out
}

package internal

import (
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The decisions of a synced watch party, kept apart from mpv and the network
// so they can be tested: how old the host's state is, when a member seeks,
// pauses or switches episode, and when the host sends its state.

const (
	// partyDriftLimit is how far a member may be from the host before it
	// seeks.
	partyDriftLimit = 2.0
	// partySeekCooldown lets a seek land (and the stream buffer) before the
	// position is judged again.
	partySeekCooldown = 3 * time.Second
	// partySeekLead is added when seeking into a playing episode: the seek
	// itself takes about this long.
	partySeekLead = 0.3
	// partyStateEvery is how often the host sends its state with nothing
	// changing.
	partyStateEvery = 5 * time.Second
	// partyMaxAge caps how much a state's age adds to the host's position,
	// so a stale state can't send a member far ahead.
	partyMaxAge = 30.0
)

// partyHostState is the host's player, as the host sends it and the bot
// relays it. SentAt is unix ms in the bot's clock.
type partyHostState struct {
	AniListID int     `json:"anilist_id"`
	Episode   int     `json:"episode"`
	Playing   bool    `json:"playing"`
	Position  float64 `json:"position"`
	SentAt    int64   `json:"sent_at"`
}

// partyHostPositionAt is where the host is at serverNow (bot clock, ms):
// the position it sent, plus the time since if it was playing.
func partyHostPositionAt(s partyHostState, serverNow int64) float64 {
	if !s.Playing || s.SentAt == 0 {
		return s.Position
	}
	age := float64(serverNow-s.SentAt) / 1000
	age = math.Max(0, math.Min(age, partyMaxAge))
	return s.Position + age
}

// partyLocal is a member's own player.
type partyLocal struct {
	Episode   int
	Loaded    bool // mpv reports a position
	Paused    bool
	Position  float64
	SinceSeek time.Duration // since this app last seeked to the host
}

// partyAction is what a member does to follow the host.
type partyAction struct {
	Switch      bool // load the host's episode
	Pause, Play bool
	Seek        bool
	SeekTo      float64
	OSD         string // shown in mpv
}

// decidePartySync compares a member's player with the host's state.
func decidePartySync(h partyHostState, l partyLocal, serverNow int64) partyAction {
	var a partyAction
	if h.Episode <= 0 {
		return a
	}
	if h.Episode != l.Episode {
		a.Switch = true
		a.OSD = "Host switched to episode " + strconv.Itoa(h.Episode)
		return a
	}
	if !l.Loaded {
		return a
	}
	var notes []string
	switch {
	case !h.Playing && !l.Paused:
		a.Pause = true
		notes = append(notes, "Host paused")
	case h.Playing && l.Paused:
		a.Play = true
		notes = append(notes, "Host resumed")
	}
	target := partyHostPositionAt(h, serverNow)
	if math.Abs(l.Position-target) > partyDriftLimit && l.SinceSeek >= partySeekCooldown {
		a.Seek = true
		a.SeekTo = target
		if h.Playing {
			a.SeekTo += partySeekLead
		}
		notes = append(notes, "Synced to host")
	}
	a.OSD = strings.Join(notes, " · ")
	return a
}

// partyShouldSend says whether the host sends cur, given the last state it
// sent (nil for none) and when: on any change of show, episode or play
// state, on a seek, and every partyStateEvery.
func partyShouldSend(last *partyHostState, lastAt time.Time, cur partyHostState, now time.Time) bool {
	if cur.AniListID <= 0 || cur.Episode <= 0 {
		return false
	}
	if last == nil || cur.AniListID != last.AniListID || cur.Episode != last.Episode || cur.Playing != last.Playing {
		return true
	}
	if now.Sub(lastAt) >= partyStateEvery {
		return true
	}
	expected := last.Position
	if last.Playing {
		expected += now.Sub(lastAt).Seconds()
	}
	return math.Abs(cur.Position-expected) > 1.5
}

// partyBackoff is the wait before reconnect attempt n (from 0): 1 s,
// doubling, at most 30 s.
func partyBackoff(n int) time.Duration {
	if n < 0 {
		n = 0
	}
	if n > 4 {
		return 30 * time.Second
	}
	return time.Duration(1<<n) * time.Second
}

// partyClock estimates the bot's clock from ping round trips, keeping the
// sample with the shortest trip. With no sample it trusts the local clock.
type partyClock struct {
	mu     sync.Mutex
	offset int64 // bot ms - local ms
	rtt    int64
	ok     bool
}

// sample records a ping sent at sentLocal and answered at recvLocal by a
// pong stamped server (all ms).
func (c *partyClock) sample(sentLocal, recvLocal, server int64) {
	rtt := recvLocal - sentLocal
	if rtt < 0 || rtt > 10000 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ok && rtt > c.rtt {
		return
	}
	c.offset, c.rtt, c.ok = server-(sentLocal+rtt/2), rtt, true
}

// serverNow is local (unix ms) in the bot's clock.
func (c *partyClock) serverNow(local int64) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return local + c.offset
}

var partyIDRe = regexp.MustCompile(`^\d{1,25}$`)

// parsePartyID takes what a member pastes: the event id, the Join in
// Otakase link (…/open/?party=ID), an otakase:// link with a party, or the
// Discord event link (…/events/GUILD/ID).
func parsePartyID(input string) (string, bool) {
	s := strings.TrimSpace(input)
	if partyIDRe.MatchString(s) {
		return s, true
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", false
	}
	if id := u.Query().Get("party"); partyIDRe.MatchString(id) {
		return id, true
	}
	parts := strings.Split(strings.Trim(u.Host+u.Path, "/"), "/")
	if last := parts[len(parts)-1]; partyIDRe.MatchString(last) && (len(parts) >= 2) {
		return last, true
	}
	return "", false
}

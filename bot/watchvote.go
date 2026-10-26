package main

import (
	"math"
	"net/http"
	"time"
)

// Skipping the opening or the ending by vote: anyone in the room can call
// a skip, and once more than half of the people in it agree, the room
// jumps past the opening or the ending for everyone. It works whether or
// not the host turned the room's automatic skips on; the host's own vote
// skips at once, since the host could press the buttons anyway. A vote
// lives for watchVoteTTL, or until the room leaves the interval it was
// called in.

const watchVoteTTL = 20 * time.Second

type watchVote struct {
	Kind  string    `json:"kind"` // op or ed
	Votes int       `json:"votes"`
	Need  int       `json:"need"`
	Mine  bool      `json:"mine"`
	At    time.Time `json:"-"`
	To    float64   `json:"-"` // where the skip lands
	who   map[string]bool
}

// watchVoteNeed is how many of n people must agree: more than half.
func watchVoteNeed(n int) int {
	if n < 2 {
		return 1
	}
	return n/2 + 1
}

// watchVoteSpan is the opening or ending the room is inside now, and where
// skipping it lands. Hold the lock.
func (r *watchRoom) watchVoteSpan(now time.Time) (string, float64, bool) {
	if r.phase != "play" || !r.skipReady || r.dur <= 0 {
		return "", 0, false
	}
	times := r.skipIntervals.match(r.dur)
	pos := r.position(now)
	for _, s := range []struct {
		kind string
		span watchSkipSpan
	}{{"op", times.OP}, {"ed", times.ED}} {
		if s.span.End > s.span.Start && pos >= s.span.Start-1 && pos < s.span.End-1 {
			return s.kind, math.Min(s.span.End, math.Max(0, r.dur-0.5)), true
		}
	}
	return "", 0, false
}

// dropVote forgets a vote that ran out or no longer fits where the room
// is. Hold the lock.
func (r *watchRoom) dropVote(now time.Time) bool {
	if r.vote == nil {
		return false
	}
	kind, _, ok := r.watchVoteSpan(now)
	if now.Sub(r.vote.At) < watchVoteTTL && ok && kind == r.vote.Kind {
		return false
	}
	r.vote = nil
	return true
}

// watchVoteAPI is a member calling or joining a skip vote.
func (b *bot) watchVoteAPI(w http.ResponseWriter, r *http.Request) {
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
	now := watchNow()
	cur.dropVote(now)
	kind, to, inside := cur.watchVoteSpan(now)
	if !inside {
		b.watch.mu.Unlock()
		apiError(w, http.StatusBadRequest, "there's no opening or ending to skip right now")
		return
	}
	if cur.vote == nil {
		cur.vote = &watchVote{Kind: kind, At: now, To: to, who: map[string]bool{}}
	}
	cur.vote.who[user] = true
	cur.vote.To = to
	if len(cur.vote.who) >= watchVoteNeed(len(cur.members)) || user == cur.host {
		cur.setPos(to, cur.playing, now)
		cur.vote = nil
	}
	cur.changed()
	b.watchAnswerView(w, cur, user, staff)
}

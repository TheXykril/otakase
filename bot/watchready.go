package main

import (
	"math"
	"net/http"
	"sync/atomic"
	"time"
)

// The ready check: when an episode starts, the host seeks or presses play,
// the room waits at that spot until every member's player has it loaded,
// so nobody starts the scene a few seconds after everyone else. A member
// whose player never gets there doesn't hold the room for long: after
// watchReadyWait (watchReadyFirst for a new episode) it plays anyway, and
// the host can start it at once. A room with one member never waits.

const (
	watchReadyWait  = 15 * time.Second
	watchReadyFirst = 30 * time.Second // a new episode loads from nothing
)

// watchHoldN numbers ready checks across every room. A page keeps the id it
// last said it was ready for, so a new room must never reuse one: with ids
// counted per room, a host opening a second room skipped its first check
// and everyone else saw them loading until the check timed out.
var watchHoldN atomic.Int64

// holdAt stops the room at pos until everyone is ready. Hold the lock.
func (r *watchRoom) holdAt(pos float64, wait time.Duration, now time.Time) {
	pos = math.Max(0, pos)
	// A spot inside an opening or ending the room skips waits at its end
	// instead, so nobody loads the opening only to jump past it right after.
	// (skipTo itself lands past the span, so this never repeats for it.)
	if to, ok := watchSkipTarget(r.skipIntervals.match(r.dur), pos, r.dur, r.autoSkipOP, r.autoSkipED); ok {
		pos = to
		r.noteSkip(to)
		// As skipTo: the host's page follows, its stale beats are ignored.
		r.skipSeq++
		r.skipTarget, r.skipHoldUntil = to, now.Add(8*time.Second)
	}
	if len(r.members) < 2 {
		r.unhold()
		r.setPos(pos, true, now)
		return
	}
	r.hold, r.holdPos, r.holdUntil = int(watchHoldN.Add(1)), pos, now.Add(wait)
	r.ready = map[string]bool{}
	r.setPos(pos, false, now)
}

// unhold drops a running check without starting the room. Hold the lock.
func (r *watchRoom) unhold() {
	r.hold, r.ready = 0, nil
}

// release starts the room playing from where the check held it. Hold the
// lock.
func (r *watchRoom) release(now time.Time) {
	pos := r.holdPos
	r.unhold()
	r.setPos(pos, true, now)
	r.changed()
}

// holdDone tells whether a running check can let go: everyone is ready or
// its time is up. Hold the lock.
func (r *watchRoom) holdDone(now time.Time) bool {
	if r.hold == 0 {
		return false
	}
	if !now.Before(r.holdUntil) {
		return true
	}
	for id := range r.members {
		if !r.ready[id] {
			return false
		}
	}
	return true
}

// holdWaiting is the names of the members not ready yet, longest in the
// room first. Hold the lock.
func (r *watchRoom) holdWaiting() []string {
	var out []string
	for _, id := range r.people() {
		if !r.ready[id] {
			out = append(out, r.members[id].name)
		}
	}
	return out
}

// watchReady is a member's player saying it has the held spot loaded.
func (b *bot) watchReady(w http.ResponseWriter, r *http.Request) {
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
	if cur.phase == "play" && cur.hold != 0 && in.ID == cur.hold && !cur.ready[user] {
		cur.ready[user] = true
		if now := watchNow(); cur.holdDone(now) {
			cur.release(now)
		} else {
			cur.changed()
		}
	}
	b.watchAnswerView(w, cur, user, staff)
}

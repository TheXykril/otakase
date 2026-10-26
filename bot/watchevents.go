package main

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Room events: short notes about what just happened in the room (who
// paused, who skipped ahead, a skip by vote, who joined or left). The
// Activity flashes each new one over the video for a moment; nothing is
// kept beyond a few seconds and they never go into the chat.

const (
	watchEventKeep = 8
	watchEventTTL  = 10 * time.Second
)

type watchEvent struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
	At   int64  `json:"at"` // unix ms
}

// note adds an event. Hold the lock; the caller marks the room changed.
func (r *watchRoom) note(format string, args ...any) {
	r.eventN++
	now := watchNow()
	r.events = append(r.events, watchEvent{ID: r.eventN, Text: fmt.Sprintf(format, args...), At: now.UnixMilli()})
	r.dropEvents(now)
}

// dropEvents forgets events past watchEventTTL, and all but the latest
// watchEventKeep. Hold the lock.
func (r *watchRoom) dropEvents(now time.Time) bool {
	keep := r.events[:0]
	for _, e := range r.events {
		if now.Sub(time.UnixMilli(e.At)) < watchEventTTL {
			keep = append(keep, e)
		}
	}
	if len(keep) > watchEventKeep {
		keep = keep[len(keep)-watchEventKeep:]
	}
	dropped := len(keep) != len(r.events)
	r.events = keep
	return dropped
}

// name is how the room calls a member in its events.
func (r *watchRoom) name(id string) string {
	if m, ok := r.members[id]; ok && m.name != "" {
		return m.name
	}
	return "Someone"
}

// noteControl describes a control a member carried out. Hold the lock.
func (r *watchRoom) noteControl(who, act string) {
	n := r.name(who)
	switch act {
	case "play":
		r.note("%s pressed play", n)
	case "pause":
		r.note("%s paused", n)
	case "seek":
		r.note("%s jumped to %s", n, watchTimeText(r.pos))
	case "forward":
		r.note("%s skipped ahead to %s", n, watchTimeText(r.pos))
	case "next", "prev":
		r.note("%s put on episode %d", n, r.ep)
	case "op-on", "op-off", "ed-on", "ed-off":
		part := map[byte]string{'o': "openings", 'e': "endings"}[act[0]]
		on := map[bool]string{true: "on", false: "off"}[strings.HasSuffix(act, "-on")]
		r.note("%s turned skipping %s %s", n, part, on)
	}
}

// watchTimeText is a position as the player shows it: 12:05 or 1:02:05.
func watchTimeText(secs float64) string {
	t := int(math.Max(0, secs))
	if t >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", t/3600, t/60%60, t%60)
	}
	return fmt.Sprintf("%d:%02d", t/60, t%60)
}

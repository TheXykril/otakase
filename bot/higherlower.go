package main

import (
	"fmt"
	"sync"
	"time"
)

// Higher or lower, played in the Activity: a popular show with its AniList
// score (or popularity) and a second one without. The player picks Higher
// or Lower for the second; right answers keep going with the second show
// as the new first. Every 3 right in a row is a game point, at most 5 a
// day. A run ends on a wrong answer.

type hlRun struct {
	user   string
	stat   string // score or popularity
	a, b   guessShow
	streak int
	seen   []int // shows used in this run
	last   time.Time
}

type hlGames struct {
	mu   sync.Mutex
	runs map[string]*hlRun
}

// hlValue is the stat being compared.
func hlValue(s guessShow, stat string) int {
	if stat == "popularity" {
		return s.Popular
	}
	return s.Score
}

func hlValueText(s guessShow, stat string) string {
	if stat == "popularity" {
		return thousands(s.Popular) + " members"
	}
	return fmt.Sprintf("%d%%", s.Score)
}

// hlNext picks a show for the second spot: one with the stat, not used in
// the run and not the same value as a, so there is always a right answer.
func hlNext(pool []guessShow, a guessShow, stat string, seen []int, intn func(int) int) (guessShow, bool) {
	var choices []guessShow
	for _, s := range pool {
		if v := hlValue(s, stat); v > 0 && v != hlValue(a, stat) && s.ID != a.ID && !containsInt(seen, s.ID) {
			choices = append(choices, s)
		}
	}
	if len(choices) == 0 {
		return guessShow{}, false
	}
	return choices[intn(len(choices))], true
}

// hlRight reports whether the answer (true = higher) is right for b against a.
func hlRight(a, b guessShow, stat string, higher bool) bool {
	return (hlValue(b, stat) > hlValue(a, stat)) == higher
}

// hlPoint gives a point for a run of hlEvery, unless today's cap is reached.
func (b *bot) hlPoint(user string) {
	day := time.Now().UTC().Format(time.DateOnly)
	week := weekKey(time.Now().UTC())
	b.store.update(func(d *storeData) {
		s := d.Games[user]
		if s.HLDay != day {
			s.HLDay, s.HLToday = day, 0
		}
		if s.HLToday >= hlDailyCap {
			return
		}
		s.HLToday++
		if d.Games == nil {
			d.Games = map[string]gameScore{}
		}
		d.Games[user] = s
		addGamePoints(d, user, "higher-lower", 1, week)
	})
}

// hlBest records a finished run and returns the member's best.
func (b *bot) hlBest(user string, streak int) int {
	var best int
	b.store.update(func(d *storeData) {
		if d.Games == nil {
			d.Games = map[string]gameScore{}
		}
		s := d.Games[user]
		if streak > s.HLBest {
			s.HLBest = streak
			d.Games[user] = s
		}
		best = s.HLBest
	})
	return best
}

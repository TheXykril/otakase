package main

import (
	"net/http"
	"time"
)

// Touch Grass: a hidden watch badge. About one exit in a hundred the app
// says goodbye with a touch-grass line, and about one of those in ten it
// also drops this badge, once per install. A linked app claims it here; an
// app that isn't linked yet keeps it until it is.
//
//	POST /v1/badge/touch-grass (auth) → {"new":true,"finders":3}
//
// new is false when the member already had it. finders is how many members
// have it. Like the other badges it gives game points once and the role of
// the same name ("Touch Grass") when the server has one. /badges says
// nothing about it until it is earned.

const touchGrassKey = "touch-grass"

func init() {
	watchBadges = append(watchBadges, watchBadge{Key: touchGrassKey, Name: "Touch Grass", Emoji: "🌱",
		How: "Found on the way out", Points: 10})
	apiHandle("POST /v1/badge/touch-grass", (*bot).apiTouchGrass)
}

func (b *bot) apiTouchGrass(w http.ResponseWriter, r *http.Request) {
	user := b.requireApp(w, r)
	if user == "" {
		return
	}
	now := appNow().UTC()
	var fresh bool
	finders := 0
	b.store.touch(func(d *storeData) {
		if d.WatchStats == nil {
			d.WatchStats = map[string]watchStats{}
		}
		st := d.WatchStats[user]
		if _, had := st.Badges[touchGrassKey]; !had {
			if st.Badges == nil {
				st.Badges = map[string]time.Time{}
			}
			st.Badges[touchGrassKey] = now
			d.WatchStats[user] = st
			bd, _ := badgeByKey(touchGrassKey)
			addGamePoints(d, user, badgeGame, bd.Points, weekKey(now))
			fresh = true
		}
		for _, other := range d.WatchStats {
			if _, ok := other.Badges[touchGrassKey]; ok {
				finders++
			}
		}
	})
	if fresh && b.s != nil {
		go b.giveBadgeRoles(user, []string{touchGrassKey})
	}
	writeJSON(w, http.StatusOK, map[string]any{"new": fresh, "finders": finders})
}

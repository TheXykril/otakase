package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// The first claim earns the badge and its points; a second one from the
// same member changes nothing, and finders counts members, not claims.
func TestTouchGrassClaim(t *testing.T) {
	b := appTestBot(t)
	if w := apiJSON(t, b, "/v1/badge/touch-grass", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("unlinked claim: %d", w.Code)
	}
	tok := testToken(b, "u1")
	w := apiJSON(t, b, "/v1/badge/touch-grass", "", tok)
	if got := decode(t, w); w.Code != http.StatusOK || got["new"] != true || got["finders"] != 1.0 {
		t.Fatalf("first claim: %d %v", w.Code, got)
	}
	w = apiJSON(t, b, "/v1/badge/touch-grass", "", tok)
	if got := decode(t, w); got["new"] != false || got["finders"] != 1.0 {
		t.Fatalf("second claim: %v", got)
	}
	w = apiJSON(t, b, "/v1/badge/touch-grass", "", testToken(b, "u2"))
	if got := decode(t, w); got["new"] != true || got["finders"] != 2.0 {
		t.Fatalf("other member: %v", got)
	}
	var points int
	b.store.view(func(d *storeData) { points = d.Games["u1"].Total })
	bd, _ := badgeByKey(touchGrassKey)
	if points != bd.Points {
		t.Fatalf("points %d, want %d once", points, bd.Points)
	}
}

// Heartbeats never earn it, and /badges keeps it secret until it is earned.
func TestTouchGrassStaysHidden(t *testing.T) {
	var st watchStats
	for range 50 {
		for _, k := range addHeartbeat(&st, nil, 120, appNow()) {
			if k == touchGrassKey {
				t.Fatal("a heartbeat earned the hidden badge")
			}
		}
	}
	e := badgesView("u1", true, watchStats{}, appNow())
	for _, f := range e.Fields {
		if strings.Contains(f.Value, "Touch Grass") {
			t.Fatalf("an unearned hidden badge was listed: %q", f.Value)
		}
	}
	st.Badges = map[string]time.Time{touchGrassKey: appNow()}
	if e := badgesView("u1", true, st, appNow()); !strings.Contains(e.Description, "Touch Grass") {
		t.Fatalf("an earned hidden badge was not listed: %q", e.Description)
	}
}

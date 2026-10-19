package main

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRoomOutWatchLock(t *testing.T) {
	asAdmins(t, "c")
	b, room, now := raceRoom(t)
	adminPost(b, "c", adminIn{Kind: "watch", Target: "a"}, nil)
	var v togetherView
	togetherPost(b, (*bot).togetherJoin, "a", togetherIn{Room: "r"}, &v)
	if v.Out != "watch" || slices.ContainsFunc(v.Members, func(p togetherPerson) bool { return p.ID == "a" }) {
		t.Fatalf("a should watch: %q", v.Out)
	}
	if code := togetherPost(b, (*bot).togetherAnswer, "a", togetherIn{Room: "r", Text: "show b"}, nil); code != http.StatusConflict {
		t.Errorf("a spectator answered: %d", code)
	}
	adminPost(b, "c", adminIn{Kind: "watch", Target: "a"}, nil)
	adminPost(b, "c", adminIn{Kind: "room-out", Target: "b"}, nil)
	v = togetherView{}
	togetherPost(b, (*bot).togetherJoin, "b", togetherIn{Room: "r"}, &v)
	if v.Out != "room" {
		t.Fatalf("b should be out of the room: %q", v.Out)
	}
	*now = now.Add(roomOutTime + time.Second)
	v = togetherView{}
	togetherPost(b, (*bot).togetherJoin, "b", togetherIn{Room: "r"}, &v)
	if v.Out != "" {
		t.Errorf("b should be back after 10 minutes: %q", v.Out)
	}
	// A locked room keeps newcomers watching, not members or staff.
	adminPost(b, "c", adminIn{Kind: "lock"}, nil)
	v = togetherView{}
	togetherPost(b, (*bot).togetherJoin, "d", togetherIn{Room: "r"}, &v)
	if v.Out != "locked" {
		t.Errorf("a newcomer got into a locked room: %q", v.Out)
	}
	v = togetherView{}
	togetherPost(b, (*bot).togetherJoin, "b", togetherIn{Room: "r"}, &v)
	if v.Out != "" {
		t.Errorf("a member is locked out: %q", v.Out)
	}
	_ = room
}

func TestHostSlowAndWarn(t *testing.T) {
	asAdmins(t, "c")
	b, room, now := raceRoom(t)
	adminPost(b, "c", adminIn{Kind: "host", Target: "b"}, nil)
	if room.host() != "b" {
		t.Errorf("host is %q", room.host())
	}
	adminPost(b, "c", adminIn{Kind: "slow", Target: "a"}, nil)
	if code := togetherPost(b, (*bot).togetherAnswer, "a", togetherIn{Room: "r", Text: "nope"}, nil); code != http.StatusOK {
		t.Fatalf("first answer: %d", code)
	}
	if code := togetherPost(b, (*bot).togetherAnswer, "a", togetherIn{Room: "r", Text: "nope 2"}, nil); code != http.StatusConflict {
		t.Errorf("a slowed member answered twice at once: %d", code)
	}
	*now = now.Add(slowEvery)
	if code := togetherPost(b, (*bot).togetherAnswer, "a", togetherIn{Room: "r", Text: "nope 3"}, nil); code != http.StatusOK {
		t.Errorf("answer after 3s: %d", code)
	}
	if code := adminPost(b, "c", adminIn{Kind: "warn", Target: "a"}, nil); code != http.StatusBadRequest {
		t.Errorf("a warning without a reason: %d", code)
	}
	adminPost(b, "c", adminIn{Kind: "warn", Target: "a", Text: "spam"}, nil)
	var n int
	b.store.view(func(d *storeData) { n = len(d.Warnings["a"]) })
	if n != 1 {
		t.Errorf("warnings: %d", n)
	}
	var v togetherView
	togetherPost(b, (*bot).togetherJoin, "a", togetherIn{Room: "r"}, &v)
	if !slices.ContainsFunc(v.Fx, func(f fxEvent) bool { return f.Kind == "warn" && f.Text == "spam" }) {
		t.Errorf("no warning popup: %+v", v.Fx)
	}
	if !slices.ContainsFunc(adminLogged(), func(e adminLogEntry) bool { return e.Kind == "warn" && e.Target == "a" }) {
		t.Error("the warning isn't in the admin log")
	}
}

func TestNamesGhostPuppet(t *testing.T) {
	asAdmins(t, "c")
	b, room, _ := raceRoom(t)
	adminPost(b, "c", adminIn{Kind: "rename", Target: "a", Text: "Baka"}, nil)
	adminPost(b, "c", adminIn{Kind: "hide-name", Target: "b"}, nil)
	var v togetherView
	togetherPost(b, (*bot).togetherJoin, "a", togetherIn{Room: "r"}, &v)
	names := map[string]string{}
	for _, p := range v.Members {
		names[p.ID] = p.Name
	}
	if names["a"] != "Baka" || !strings.HasPrefix(names["b"], "Player ") {
		t.Errorf("names %+v", names)
	}
	if v.Admin != nil {
		t.Error("a sees the admin view")
	}
	adminPost(b, "c", adminIn{Kind: "toggle", Target: "a", Text: "ghost"}, nil)
	togetherPost(b, (*bot).togetherAnswer, "a", togetherIn{Room: "r", Text: "wrong"}, nil)
	if room.feed[0].Name == "Baka" {
		t.Errorf("a ghosted answer shows under its own name")
	}
	if code := adminPost(b, "c", adminIn{Kind: "puppet", Target: "b", Text: "I love filler"}, nil); code != http.StatusOK {
		t.Fatalf("puppet: %d", code)
	}
	if last := room.feed[len(room.feed)-1]; last.Text != "I love filler" || last.OK {
		t.Errorf("puppet line %+v", last)
	}
	// Prank names end with the game; hidden names stay.
	adminPost(b, "c", adminIn{Kind: "end"}, nil)
	v = togetherView{}
	togetherPost(b, (*bot).togetherJoin, "a", togetherIn{Room: "r"}, &v)
	for _, p := range v.Members {
		if p.ID == "a" && p.Name == "Baka" {
			t.Error("the prank name outlived the game")
		}
		if p.ID == "b" && !strings.HasPrefix(p.Name, "Player ") {
			t.Error("the hidden name came back")
		}
	}
	if v.Troll != nil {
		t.Errorf("pranks outlived the game: %+v", v.Troll)
	}
}

func TestToggleAndFx(t *testing.T) {
	asAdmins(t, "c")
	b, _, _ := raceRoom(t)
	if code := adminPost(b, "c", adminIn{Kind: "toggle", Target: "a", Text: "nope"}, nil); code != http.StatusBadRequest {
		t.Errorf("unknown prank: %d", code)
	}
	adminPost(b, "c", adminIn{Kind: "toggle", Target: "a", Text: "fast"}, nil)
	adminPost(b, "c", adminIn{Kind: "toggle", Target: "a", Text: "quack"}, nil)
	adminPost(b, "c", adminIn{Kind: "fx", Target: "a", Text: "quote"}, nil)
	var v togetherView
	togetherPost(b, (*bot).togetherJoin, "a", togetherIn{Room: "r"}, &v)
	if v.Troll == nil || !slices.Equal(v.Troll.On, []string{"fast", "quack"}) {
		t.Fatalf("troll %+v", v.Troll)
	}
	if !slices.ContainsFunc(v.Fx, func(f fxEvent) bool { return f.Kind == "quote" && f.Text != "" }) {
		t.Errorf("no quote: %+v", v.Fx)
	}
	adminPost(b, "c", adminIn{Kind: "toggle", Target: "a", Text: "fast"}, nil)
	v = togetherView{}
	togetherPost(b, (*bot).togetherJoin, "a", togetherIn{Room: "r"}, &v)
	if v.Troll == nil || !slices.Equal(v.Troll.On, []string{"quack"}) {
		t.Errorf("fast should be off: %+v", v.Troll)
	}
	// Pranks aren't in the admin log.
	if slices.ContainsFunc(adminLogged(), func(e adminLogEntry) bool { return e.Kind == "toggle" && modSteps[e.Kind] != "" }) {
		t.Error("a prank shows in the admin log")
	}
}

func TestGameOff(t *testing.T) {
	asAdmins(t, "c")
	b, room, _ := raceRoom(t)
	adminPost(b, "c", adminIn{Kind: "game-off", Text: "hl"}, nil)
	b.together.mu.Lock()
	room.toLobby()
	b.together.mu.Unlock()
	if code := togetherPost(b, (*bot).togetherStart, "a", togetherIn{Room: "r", Game: "hl"}, nil); code != http.StatusForbidden {
		t.Errorf("an off game started: %d", code)
	}
	var v togetherView
	togetherPost(b, (*bot).togetherJoin, "a", togetherIn{Room: "r"}, &v)
	if !slices.Equal(v.Off, []string{"hl"}) {
		t.Errorf("off %v", v.Off)
	}
	adminPost(b, "c", adminIn{Kind: "game-off", Text: "hl"}, nil)
	if code := togetherPost(b, (*bot).togetherStart, "a", togetherIn{Room: "r", Game: "hl"}, nil); code != http.StatusOK {
		t.Errorf("a game turned back on didn't start: %d", code)
	}
	if code := adminPost(b, "c", adminIn{Kind: "game-off", Text: "nope"}, nil); code != http.StatusBadRequest {
		t.Errorf("no such game: %d", code)
	}
}

func TestRouletteAndCrowdFx(t *testing.T) {
	asAdmins(t, "c")
	b, _, _ := raceRoom(t)
	adminPost(b, "c", adminIn{Kind: "roulette"}, nil)
	var v togetherView
	togetherPost(b, (*bot).togetherJoin, "a", togetherIn{Room: "r"}, &v)
	for _, p := range v.Members {
		if p.Name == b.dashName(p.ID) {
			t.Errorf("%s kept their own name in name roulette", p.ID)
		}
	}
	adminPost(b, "c", adminIn{Kind: "fx", Target: "b", Text: "boo"}, nil)
	adminPost(b, "c", adminIn{Kind: "toggle", Target: "a", Text: "spotlight"}, nil)
	v = togetherView{}
	togetherPost(b, (*bot).togetherJoin, "a", togetherIn{Room: "r"}, &v)
	if !slices.ContainsFunc(v.Fx, func(f fxEvent) bool { return f.Kind == "boo" && f.To == "" && f.Text == "b" }) {
		t.Errorf("everyone should hear b booed: %+v", v.Fx)
	}
	if v.Troll == nil || !slices.Contains(v.Troll.On, "spotlight") {
		t.Errorf("spotlight off: %+v", v.Troll)
	}
	adminPost(b, "c", adminIn{Kind: "end"}, nil)
	v = togetherView{}
	togetherPost(b, (*bot).togetherJoin, "a", togetherIn{Room: "r"}, &v)
	for _, p := range v.Members {
		if p.Name != b.dashName(p.ID) {
			t.Errorf("name roulette outlived the game: %s is %q", p.ID, p.Name)
		}
	}
}

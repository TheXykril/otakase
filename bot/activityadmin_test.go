package main

import (
	"net/http"
	"slices"
	"testing"
	"time"
)

// asAdmins makes the given members staff for the test.
func asAdmins(t *testing.T, ids ...string) {
	t.Helper()
	old := isGameAdmin
	isGameAdmin = func(_ *bot, user string) bool { return slices.Contains(ids, user) }
	reset := func() {
		gameAdmins.Lock()
		gameAdmins.m = nil
		gameAdmins.Unlock()
	}
	reset()
	t.Cleanup(func() { isGameAdmin = old; reset() })
}

// raceRoom is room "r" with a, b and c in a race round.
func raceRoom(t *testing.T) (*bot, *togetherRoom, *time.Time) {
	t.Helper()
	b, now := togetherBot(t)
	for _, u := range []string{"a", "b", "c"} {
		togetherPost(b, (*bot).togetherJoin, u, togetherIn{Room: "r"}, nil)
		*now = now.Add(time.Second)
	}
	b.together.mu.Lock()
	room := b.together.m["r"]
	room.running = true // the test moves the clock itself
	room.phase, room.game, room.round, room.scores = "race", "riddle", 1, map[string]int{}
	room.cur = &soloRound{game: "riddle", name: "Show B", titles: []string{"Show B"}, puzzle: "🅱️", clues: []string{"Title: S _ _ _   B"}}
	room.start = *now
	room.ends = now.Add(togetherRoundTime(room.cur))
	b.together.mu.Unlock()
	return b, room, now
}

func adminPost(b *bot, user string, in adminIn, out any) int {
	if in.Room == "" {
		in.Room = "r"
	}
	return togetherPost(b, (*bot).togetherAdmin, user, in, out)
}

func TestAdminOnlyStaff(t *testing.T) {
	asAdmins(t, "c")
	b, _, _ := raceRoom(t)
	if code := adminPost(b, "a", adminIn{Kind: "pause"}, nil); code != http.StatusForbidden {
		t.Errorf("a member used the admin menu: %d", code)
	}
	var v togetherView
	togetherPost(b, (*bot).togetherJoin, "a", togetherIn{Room: "r"}, &v)
	if v.Admin != nil {
		t.Error("a member sees the admin view")
	}
	togetherPost(b, (*bot).togetherJoin, "c", togetherIn{Room: "r"}, &v)
	if v.Admin == nil || len(v.Admin.Members) != 3 || !v.Admin.Skip {
		t.Fatalf("staff view %+v", v.Admin)
	}
	// Staff can take the room back to the lobby and start games without
	// being the host.
	if code := togetherPost(b, (*bot).togetherLobby, "c", togetherIn{Room: "r"}, nil); code != http.StatusOK {
		t.Errorf("staff to lobby: %d", code)
	}
	if code := togetherPost(b, (*bot).togetherStart, "c", togetherIn{Room: "r", Game: "hl"}, nil); code != http.StatusOK {
		t.Errorf("staff start: %d", code)
	}
}

func TestAdminKickMuteHide(t *testing.T) {
	asAdmins(t, "c")
	b, room, _ := raceRoom(t)
	adminPost(b, "c", adminIn{Kind: "mute", Target: "b"}, nil)
	togetherPost(b, (*bot).togetherAnswer, "b", togetherIn{Room: "r", Text: "something rude"}, nil)
	if room.feed[0].Text != "•••" {
		t.Errorf("muted answer shows %q", room.feed[0].Text)
	}
	togetherPost(b, (*bot).togetherAnswer, "a", togetherIn{Room: "r", Text: "also rude"}, nil)
	adminPost(b, "c", adminIn{Kind: "hide", N: 1}, nil)
	if room.feed[1].Text != "(hidden by an admin)" {
		t.Errorf("hidden answer shows %q", room.feed[1].Text)
	}
	if code := adminPost(b, "c", adminIn{Kind: "kick", Target: "c"}, nil); code != http.StatusBadRequest {
		t.Errorf("kicked yourself: %d", code)
	}
	adminPost(b, "c", adminIn{Kind: "kick", Target: "b"}, nil)
	var v togetherView
	togetherPost(b, (*bot).togetherJoin, "b", togetherIn{Room: "r"}, &v)
	if !v.Kicked || slices.ContainsFunc(v.Members, func(p togetherPerson) bool { return p.ID == "b" }) {
		t.Fatalf("b should sit out: kicked %v members %+v", v.Kicked, v.Members)
	}
	if code := togetherPost(b, (*bot).togetherAnswer, "b", togetherIn{Room: "r", Text: "show b"}, nil); code != http.StatusConflict {
		t.Errorf("a kicked member answered: %d", code)
	}
	// The next game lets them back.
	b.together.mu.Lock()
	room.phase = "over"
	b.together.mu.Unlock()
	if code := togetherPost(b, (*bot).togetherStart, "a", togetherIn{Room: "r", Game: "hl"}, &v); code != http.StatusOK {
		t.Fatalf("next game: %d %+v", code, v)
	}
	v = togetherView{}
	togetherPost(b, (*bot).togetherJoin, "b", togetherIn{Room: "r"}, &v)
	if v.Kicked || !slices.ContainsFunc(v.Members, func(p togetherPerson) bool { return p.ID == "b" }) {
		t.Errorf("b should be back for the next game: %v %+v", v.Kicked, v.Members)
	}
}

func TestAdminPauseSkipVoid(t *testing.T) {
	asAdmins(t, "c")
	b, room, now := raceRoom(t)
	ends := room.ends
	adminPost(b, "c", adminIn{Kind: "pause"}, nil)
	if code := togetherPost(b, (*bot).togetherAnswer, "a", togetherIn{Room: "r", Text: "show b"}, nil); code != http.StatusConflict {
		t.Errorf("answered while paused: %d", code)
	}
	*now = now.Add(30 * time.Second)
	b.together.mu.Lock()
	b.togetherStep(room, *now)
	b.together.mu.Unlock()
	if room.roundOver {
		t.Fatal("the round ran on while paused")
	}
	adminPost(b, "c", adminIn{Kind: "resume"}, nil)
	if !room.ends.Equal(ends.Add(30 * time.Second)) {
		t.Errorf("resume should move the end by the pause: %v → %v", ends, room.ends)
	}
	adminPost(b, "c", adminIn{Kind: "skip"}, nil)
	b.together.mu.Lock()
	b.togetherStep(room, *now)
	b.together.mu.Unlock()
	if !room.roundOver || room.winner != "" {
		t.Errorf("skip: over %v winner %q", room.roundOver, room.winner)
	}

	// A win taken back costs the round and the point.
	b.together.mu.Lock()
	room.cur, room.roundOver, room.winner, room.feed = &soloRound{game: "riddle", name: "Show C", titles: []string{"Show C"}}, false, "", nil
	room.ends = now.Add(time.Minute)
	b.together.mu.Unlock()
	togetherPost(b, (*bot).togetherAnswer, "a", togetherIn{Room: "r", Text: "show c"}, nil)
	if room.scores["a"] != 1 || b.weekPointsOf("a") != 1 {
		t.Fatalf("win: score %d points %d", room.scores["a"], b.weekPointsOf("a"))
	}
	adminPost(b, "c", adminIn{Kind: "void"}, nil)
	if room.scores["a"] != 0 || room.winner != "" || b.weekPointsOf("a") != 0 {
		t.Errorf("void: score %d winner %q points %d", room.scores["a"], room.winner, b.weekPointsOf("a"))
	}
}

func (b *bot) weekPointsOf(user string) int {
	n := 0
	b.store.view(func(d *storeData) { n = d.Games[user].Week })
	return n
}

func TestGameBan(t *testing.T) {
	asAdmins(t, "c")
	b, _, _ := raceRoom(t)
	if code := adminPost(b, "c", adminIn{Kind: "bench", Target: "c", N: 1}, nil); code != http.StatusBadRequest {
		t.Errorf("benched yourself: %d", code)
	}
	adminPost(b, "c", adminIn{Kind: "bench", Target: "a", N: 2, Text: "spam"}, nil)
	if _, in := b.together.m["r"].members["a"]; in {
		t.Error("a benched member stays in the room")
	}
	if code := togetherPost(b, (*bot).togetherJoin, "a", togetherIn{Room: "r"}, nil); code != http.StatusForbidden {
		t.Errorf("a benched member joined: %d", code)
	}
	if b.actPoint("a", "riddle") != 1 || b.weekPointsOf("a") != 0 {
		t.Error("a benched member got points")
	}
	b.setGameBan("a", 0, "", "")
	if code := togetherPost(b, (*bot).togetherJoin, "a", togetherIn{Room: "r"}, nil); code != http.StatusOK {
		t.Errorf("after the break: %d", code)
	}
}

func TestAdjustPoints(t *testing.T) {
	b := testGamesBot(t)
	b.addPoints("a", "riddle", 3)
	if got := b.adjustPoints("a", -5); got != 0 || b.weekPointsOf("a") != 0 {
		t.Errorf("points go below zero: %d", got)
	}
	if got := b.adjustPoints("a", 4); got != 4 {
		t.Errorf("give 4: %d", got)
	}
}

func TestPranks(t *testing.T) {
	asAdmins(t, "c")
	b, room, _ := raceRoom(t)
	adminPost(b, "c", adminIn{Kind: "flip", Target: "a", Text: "upside"}, nil)
	adminPost(b, "c", adminIn{Kind: "fake-wrong", Target: "a"}, nil)
	adminPost(b, "c", adminIn{Kind: "hint", Target: "a"}, nil)
	adminPost(b, "c", adminIn{Kind: "confetti"}, nil)
	adminPost(b, "c", adminIn{Kind: "swap"}, nil)
	var va, vb togetherView
	togetherPost(b, (*bot).togetherJoin, "a", togetherIn{Room: "r"}, &va)
	togetherPost(b, (*bot).togetherJoin, "b", togetherIn{Room: "r"}, &vb)
	if va.Troll == nil || va.Troll.Flip != "upside" || !va.Troll.FakeWrong || vb.Troll != nil {
		t.Errorf("pranks: a %+v b %+v", va.Troll, vb.Troll)
	}
	if len(va.Fx) != 2 || va.Fx[0].Kind != "hint" || va.Fx[0].Text == "" || len(vb.Fx) != 1 || vb.Fx[0].Kind != "confetti" {
		t.Errorf("effects: a %+v b %+v", va.Fx, vb.Fx)
	}
	if len(va.Swap) != 3 || va.Swap["a"] == "a" {
		t.Errorf("swap %v", va.Swap)
	}
	// Flip again turns it off.
	adminPost(b, "c", adminIn{Kind: "flip", Target: "a", Text: "upside"}, nil)
	if room.trolls["a"].Flip != "" {
		t.Error("flip should toggle")
	}
	// Back in the lobby every prank is gone.
	togetherPost(b, (*bot).togetherLobby, "c", togetherIn{Room: "r"}, nil)
	if room.trolls != nil || room.swap != nil {
		t.Error("pranks outlived the game")
	}
}

func TestSwapFaces(t *testing.T) {
	for n := 2; n <= 6; n++ {
		ids := []string{"a", "b", "c", "d", "e", "f"}[:n]
		for k := range n - 1 {
			m := swapFaces(slices.Clone(ids), func(int) int { return k })
			seen := map[string]bool{}
			for id, to := range m {
				if id == to || seen[to] {
					t.Fatalf("%d players, k %d: %v", n, k, m)
				}
				seen[to] = true
			}
		}
	}
	if swapFaces([]string{"a"}, nil) != nil {
		t.Error("one player has nobody to swap with")
	}
}

func TestRiggedImposterAndBomb(t *testing.T) {
	b, r, now := socialRoom(t, "imposter", "a", "b", "c")
	r.imp.rig = "b"
	for range 3 {
		r.imp.next(r.pool, *now)
		if r.imp.imp != "b" {
			t.Fatalf("rigged imposter: %s", r.imp.imp)
		}
	}

	b, r, now = socialRoom(t, "bomb", "a", "b", "c")
	g := r.bomb
	rig := g.alive[(g.holder+1)%len(g.alive)]
	r.bombRig = rig
	*now = g.explode
	b.bombStep(r, *now)
	if g.stage != "play" {
		t.Fatal("the bomb went off before reaching the rigged player")
	}
	g.holder = slices.Index(g.alive, rig)
	b.bombStep(r, *now)
	if g.stage != "boom" || g.boomed != rig {
		t.Errorf("rigged bomb: %s %s", g.stage, g.boomed)
	}
}

func TestKoiKoiTrashTalk(t *testing.T) {
	asAdmins(t, "a")
	b, now := togetherBot(t)
	b.guess.pool = pzPool()
	for i := range b.guess.pool {
		b.guess.pool[i].Cover = "https://s4.anilist.co/x.jpg"
	}
	togetherPost(b, (*bot).togetherJoin, "a", togetherIn{Room: "r"}, nil)
	adminPost(b, "a", adminIn{Kind: "trash"}, nil)
	b.together.m["r"].running = true
	var e struct{ Error string }
	if code := togetherPost(b, (*bot).togetherStart, "a", togetherIn{Room: "r", Game: "koikoi", Stat: "2"}, &e); code != http.StatusOK {
		t.Fatalf("start: %d %s", code, e.Error)
	}
	b.together.mu.Lock()
	defer b.together.mu.Unlock()
	room := b.together.m["r"]
	tb := room.kk.tables[0]
	if !tb.talk {
		t.Fatal("bots should talk")
	}
	for range 40 {
		*now = now.Add(kkBotTime * 40)
		tb.step(*now, func(string) bool { return true }, func(n int) int { return 0 })
		if tb.say != "" {
			return
		}
	}
	t.Error("the bot never said anything")
}

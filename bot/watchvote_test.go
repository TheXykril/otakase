package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func watchVoteRoom(t *testing.T, now time.Time) (*bot, *watchRoom) {
	room := &watchRoom{
		id: "r", opened: now, phase: "play", playing: true, pos: 90, at: now, dur: 1440,
		skipReady: true,
		members: map[string]*watchMember{
			"host": {name: "Host", seen: now, joined: now}, "b": {name: "Bo", seen: now, joined: now},
			"c": {name: "Ci", seen: now, joined: now}, "d": {name: "Di", seen: now, joined: now},
		},
		host:          "host",
		skipIntervals: watchSkipIntervals{OP: []watchSkipSpan{{Start: 74, End: 162}}, ED: []watchSkipSpan{{Start: 1300, End: 1400}}},
	}
	b := testGamesBot(t)
	b.cfg.GuildID = "g"
	b.watch.cur = room
	t.Cleanup(func() {
		b.watch.mu.Lock()
		b.watch.cur = nil
		b.watch.mu.Unlock()
	})
	return b, room
}

func TestWatchSkipVote(t *testing.T) {
	now := time.Date(2026, 10, 26, 20, 0, 0, 0, time.UTC)
	oldClock := watchNow
	watchNow = func() time.Time { return now }
	t.Cleanup(func() { watchNow = oldClock })
	b, room := watchVoteRoom(t, now)

	// Four people: three must agree. Two votes don't move it.
	var v watchView
	if code := watchPost(b, (*bot).watchVoteAPI, "b", watchIn{Room: "r"}, &v); code != 200 || v.Vote == nil || v.Vote.Votes != 1 || v.Vote.Need != 3 || v.Pos != 90 {
		t.Fatalf("first vote: %d %+v pos %v", code, v.Vote, v.Pos)
	}
	// The same person voting again doesn't count twice.
	watchPost(b, (*bot).watchVoteAPI, "b", watchIn{Room: "r"}, &v)
	if v.Vote.Votes != 1 {
		t.Fatalf("double vote counted: %+v", v.Vote)
	}
	watchPost(b, (*bot).watchVoteAPI, "c", watchIn{Room: "r"}, &v)
	if v.Vote == nil || v.Vote.Votes != 2 || room.pos != 90 {
		t.Fatalf("two of three: %+v pos %v", v.Vote, room.pos)
	}
	// The third carries it: the room jumps past the opening.
	watchPost(b, (*bot).watchVoteAPI, "d", watchIn{Room: "r"}, nil)
	if room.vote != nil || room.pos != 162 || !room.playing || room.skipSeq != 1 {
		t.Fatalf("majority: vote %+v pos %v playing %v skip %d", room.vote, room.pos, room.playing, room.skipSeq)
	}

	// Outside an opening or ending there's nothing to vote on.
	room.setPos(400, true, now)
	if code := watchPost(b, (*bot).watchVoteAPI, "b", watchIn{Room: "r"}, nil); code != http.StatusBadRequest {
		t.Fatalf("mid-episode vote: %d", code)
	}

	// The host's own vote skips at once.
	room.setPos(1320, true, now)
	watchPost(b, (*bot).watchVoteAPI, "host", watchIn{Room: "r"}, nil)
	if room.vote != nil || room.pos < 1395 {
		t.Fatalf("host skip: vote %+v pos %v", room.vote, room.pos)
	}

	// A vote that ages out is forgotten.
	room.setPos(80, true, now)
	watchPost(b, (*bot).watchVoteAPI, "b", watchIn{Room: "r"}, &v)
	if v.Vote == nil {
		t.Fatal("expected a running vote")
	}
	now = now.Add(watchVoteTTL + time.Second)
	for _, m := range room.members {
		m.seen = now
	}
	b.watchStep(room, now)
	if room.vote != nil {
		t.Fatal("stale vote not dropped")
	}
}

func TestWatchHandOver(t *testing.T) {
	now := time.Date(2026, 10, 26, 20, 0, 0, 0, time.UTC)
	oldClock, oldAdmin := watchNow, isGameAdmin
	watchNow = func() time.Time { return now }
	isGameAdmin = func(_ *bot, u string) bool { return u == "staff" }
	t.Cleanup(func() { watchNow, isGameAdmin = oldClock, oldAdmin })
	b, room := watchVoteRoom(t, now)
	room.members["staff"] = &watchMember{name: "Staff", seen: now, joined: now}

	var v watchView
	if code := watchPost(b, (*bot).watchHandOver, "b", watchIn{Room: "r", User: "c"}, nil); code != http.StatusForbidden {
		t.Fatalf("member hand-off: %d", code)
	}
	if code := watchPost(b, (*bot).watchHandOver, "host", watchIn{Room: "r", User: "ghost"}, nil); code != http.StatusNotFound {
		t.Fatalf("hand to a non-member: %d", code)
	}
	if code := watchPost(b, (*bot).watchHandOver, "host", watchIn{Room: "r", User: "b"}, &v); code != 200 || v.Host != "b" || room.host != "b" {
		t.Fatalf("hand-off: %d host %q", code, v.Host)
	}
	// Staff can take it too.
	if code := watchPost(b, (*bot).watchHandOver, "staff", watchIn{Room: "r", User: "c"}, &v); code != 200 || room.host != "c" {
		t.Fatalf("staff hand-off: %d host %q", code, room.host)
	}
}

func TestWatchMemberState(t *testing.T) {
	now := time.Date(2026, 10, 26, 20, 0, 0, 0, time.UTC)
	oldClock := watchNow
	watchNow = func() time.Time { return now }
	t.Cleanup(func() { watchNow = oldClock })
	b, room := watchVoteRoom(t, now)
	room.setPos(300, true, now)

	// The host reports its own spot; it's watching.
	watchPost(b, (*bot).watchBeat, "host", watchIn{Room: "r", Pos: 300, Playing: true, Dur: 1440}, nil)
	// b is far behind, c is buffering, d never reported.
	watchPost(b, (*bot).watchBeat, "b", watchIn{Room: "r", Pos: 280, Playing: true, Dur: 1440}, nil)
	watchPost(b, (*bot).watchBeat, "c", watchIn{Room: "r", Pos: 300, Playing: false, Dur: 1440}, nil)
	state := map[string]watchPerson{}
	b.watch.mu.Lock()
	for _, p := range b.watchViewOf(room, "host", false, now).Members {
		state[p.ID] = p
	}
	b.watch.mu.Unlock()
	if state["host"].State != "watching" || state["b"].State != "behind" || state["b"].Behind < 19 || state["c"].State != "loading" || state["d"].State != "" {
		t.Fatalf("states: %+v", state)
	}
	// A stale report (too old) drops back to blank.
	now = now.Add(20 * time.Second)
	b.watch.mu.Lock()
	st := room.stateOnly("b", now)
	b.watch.mu.Unlock()
	if st != "" {
		t.Fatalf("stale report still %q", st)
	}
}

func TestWatchStatusShowsLiveRoom(t *testing.T) {
	b, _ := watchBot(t, "https://cdn.example/ep.mp4")
	b.store.update(func(d *storeData) { d.WatchTickets = map[string]int{"a": 1, "z": 1} })
	status := func(user, room string) map[string]any {
		r := httptest.NewRequest("GET", "/x?room="+room, nil)
		r.Header.Set("Authorization", "Bearer "+b.acts.add(user))
		w := httptest.NewRecorder()
		b.watchStatus(w, r)
		out := map[string]any{}
		json.NewDecoder(w.Body).Decode(&out)
		return out
	}
	watchPost(b, (*bot).watchOpen, "a", watchIn{Room: "r1", Channel: "vc"}, nil)
	watchPost(b, (*bot).watchPick, "a", watchIn{Room: "r1", Show: 5, Ep: 3}, nil)
	waitLoaded(t, b)
	st := status("x", "elsewhere")
	if st["busy"] != true || st["here"] != false || st["show"] != "Show 5" || st["ep"] != float64(3) ||
		st["host"] != "member a" || st["people"] != float64(1) || st["channel_name"] != "lounge" || st["playing"] != true {
		t.Fatalf("live room: %v", st)
	}
	// An 18+ room says only that it runs, and where.
	b.watch.mu.Lock()
	b.watch.cur.adult = true
	b.watch.mu.Unlock()
	st = status("x", "elsewhere")
	if st["adult"] != true || st["show"] != nil || st["host"] != nil || st["cover"] != nil || st["ep"] != nil {
		t.Fatalf("18+ room leaked: %v", st)
	}
}

func TestWatchRoomEvents(t *testing.T) {
	b, now := watchBot(t, "https://cdn.example/ep.mp4")
	b.store.update(func(d *storeData) { d.WatchTickets = map[string]int{"a": 1} })
	watchPost(b, (*bot).watchOpen, "a", watchIn{Room: "r", Channel: "vc"}, nil)
	watchPost(b, (*bot).watchPick, "a", watchIn{Room: "r", Show: 5, Ep: 1}, nil)
	waitLoaded(t, b)
	watchPost(b, (*bot).watchOpen, "b", watchIn{Room: "r", Channel: "vc"}, nil)
	watchPost(b, (*bot).watchCtl, "a", watchIn{Room: "r", Act: "pause", Pos: 61}, nil)
	watchPost(b, (*bot).watchCtl, "a", watchIn{Room: "r", Act: "seek", Pos: 750}, nil)
	watchPost(b, (*bot).watchHandOver, "a", watchIn{Room: "r", User: "b"}, nil)
	var v watchView
	watchPost(b, (*bot).watchOpen, "b", watchIn{Room: "r", Channel: "vc"}, &v)
	var texts []string
	for _, e := range v.Events {
		texts = append(texts, e.Text)
	}
	want := []string{"member a put on Show 5, episode 1", "member b joined", "member a paused", "member a jumped to 12:30", "member a passed hosting to member b"}
	if strings.Join(texts, "|") != strings.Join(want, "|") {
		t.Fatalf("events %q", texts)
	}
	// They only last a moment.
	r := b.watch.cur
	*now = now.Add(watchEventTTL + time.Second)
	for _, m := range r.members {
		m.seen = *now
	}
	b.watchStep(r, *now)
	if len(r.events) != 0 {
		t.Fatalf("old events kept: %+v", r.events)
	}
	if watchTimeText(3725) != "1:02:05" || watchTimeText(65) != "1:05" {
		t.Fatal("time text")
	}
}

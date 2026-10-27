package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// watchAllReady is every member's player having the held spot loaded.
func watchAllReady(b *bot) {
	b.watch.mu.Lock()
	defer b.watch.mu.Unlock()
	r := b.watch.only()
	if r.hold == 0 {
		return
	}
	for id := range r.members {
		r.ready[id] = true
	}
	r.release(watchNow())
}

// watchSee posts in and answers the view it gets back, fresh each time so
// fields left out of the answer read as empty.
func watchSee(b *bot, fn func(*bot, http.ResponseWriter, *http.Request), user string, in watchIn) watchView {
	var v watchView
	watchPost(b, fn, user, in, &v)
	return v
}

func TestWatchReadyCheck(t *testing.T) {
	b, now := watchBot(t, "https://cdn.example/ep.mp4")
	b.store.update(func(d *storeData) { d.WatchTickets = map[string]int{"a": 1} })
	watchPost(b, (*bot).watchOpen, "a", watchIn{Room: "r", Channel: "vc"}, nil)
	watchPost(b, (*bot).watchOpen, "b", watchIn{Room: "r", Channel: "vc"}, nil)
	watchPost(b, (*bot).watchPick, "a", watchIn{Room: "r", Show: 7, Ep: 1}, nil)
	waitLoaded(t, b)
	r := b.watch.only()

	// A new episode waits for both players.
	var v watchView
	v = watchSee(b, (*bot).watchOpen, "b", watchIn{Room: "r", Channel: "vc"})
	if v.Hold == 0 || v.Playing || len(v.HoldWaiting) != 2 {
		t.Fatalf("new episode: %+v", v)
	}
	// The host's beat while it waits changes nothing.
	watchPost(b, (*bot).watchBeat, "a", watchIn{Room: "r", Pos: 50, Playing: true, Dur: 1400}, nil)
	if r.playing || r.pos != 0 || r.dur != 1400 {
		t.Fatalf("beat during the check: playing %v pos %v dur %v", r.playing, r.pos, r.dur)
	}
	// A stale check's id is ignored.
	v = watchSee(b, (*bot).watchReady, "a", watchIn{Room: "r", ID: v.Hold + 5})
	if v.HoldReady {
		t.Fatal("stale ready counted")
	}
	v = watchSee(b, (*bot).watchReady, "a", watchIn{Room: "r", ID: v.Hold})
	if !v.HoldReady || v.Playing || len(v.HoldWaiting) != 1 || v.HoldWaiting[0] != r.members["b"].name {
		t.Fatalf("one ready: %+v", v)
	}
	v = watchSee(b, (*bot).watchReady, "b", watchIn{Room: "r", ID: v.Hold})
	if v.Hold != 0 || !v.Playing {
		t.Fatalf("both ready: %+v", v)
	}

	// A seek while playing holds again; a slow player only holds it until
	// the wait runs out.
	v = watchSee(b, (*bot).watchCtl, "a", watchIn{Room: "r", Act: "seek", Pos: 300})
	if v.Hold == 0 || v.Playing || v.Pos != 300 {
		t.Fatalf("seek: %+v", v)
	}
	watchPost(b, (*bot).watchReady, "a", watchIn{Room: "r", ID: v.Hold}, nil)
	*now = now.Add(watchReadyWait - time.Second)
	r.members["a"].seen, r.members["b"].seen = *now, *now
	b.watchStep(r, *now)
	if r.hold == 0 {
		t.Fatal("let go before the wait ran out")
	}
	*now = now.Add(2 * time.Second)
	b.watchStep(r, *now)
	if r.hold != 0 || !r.playing || r.pos != 300 {
		t.Fatalf("after the wait: hold %d playing %v pos %v", r.hold, r.playing, r.pos)
	}

	// The host can start a waiting room at once; pause drops the check.
	v = watchSee(b, (*bot).watchCtl, "a", watchIn{Room: "r", Act: "forward"})
	if v.Hold == 0 || v.Pos < 309.9 {
		t.Fatalf("forward: %+v", v)
	}
	v = watchSee(b, (*bot).watchCtl, "a", watchIn{Room: "r", Act: "play", Pos: 1})
	if v.Hold != 0 || !v.Playing || v.Pos < 309.9 {
		t.Fatalf("start now: %+v", v)
	}
	v = watchSee(b, (*bot).watchCtl, "a", watchIn{Room: "r", Act: "seek", Pos: 500})
	v = watchSee(b, (*bot).watchCtl, "a", watchIn{Room: "r", Act: "pause", Pos: 500})
	if v.Hold != 0 || v.Playing {
		t.Fatalf("pause: %+v", v)
	}
	// A seek while paused just moves.
	v = watchSee(b, (*bot).watchCtl, "a", watchIn{Room: "r", Act: "seek", Pos: 10})
	if v.Hold != 0 || v.Playing || v.Pos != 10 {
		t.Fatalf("paused seek: %+v", v)
	}
	// Someone who leaves mid-check stops holding the others.
	v = watchSee(b, (*bot).watchCtl, "a", watchIn{Room: "r", Act: "play", Pos: 10})
	watchPost(b, (*bot).watchReady, "a", watchIn{Room: "r", ID: v.Hold}, nil)
	watchPost(b, (*bot).watchLeave, "b", watchIn{Room: "r"}, nil)
	b.watchStep(r, *now)
	if r.hold != 0 || !r.playing {
		t.Fatalf("after b left: hold %d playing %v", r.hold, r.playing)
	}
	// Alone, the host never waits.
	v = watchSee(b, (*bot).watchCtl, "a", watchIn{Room: "r", Act: "seek", Pos: 40})
	if v.Hold != 0 || !v.Playing {
		t.Fatalf("alone: %+v", v)
	}
}

func TestWatchChat(t *testing.T) {
	b, _ := watchBot(t, "https://cdn.example/ep.mp4")
	b.store.update(func(d *storeData) { d.WatchTickets = map[string]int{"a": 1} })
	watchPost(b, (*bot).watchOpen, "a", watchIn{Room: "r", Channel: "vc"}, nil)
	watchPost(b, (*bot).watchOpen, "b", watchIn{Room: "r", Channel: "vc"}, nil)
	watchPost(b, (*bot).watchOpen, "c", watchIn{Room: "r", Channel: "vc"}, nil)

	var v watchView
	if code := watchPost(b, (*bot).watchChatSay, "b", watchIn{Room: "r", Text: "  he \n\tdies ||in ep 12||\u202e "}, &v); code != 200 {
		t.Fatalf("say: %d", code)
	}
	if len(v.Chat) != 1 || v.Chat[0].Text != "he dies ||in ep 12||" || v.Chat[0].User != "b" {
		t.Fatalf("chat: %+v", v.Chat)
	}
	for _, bad := range []string{"   ", "join discord.gg/abc"} {
		if code := watchPost(b, (*bot).watchChatSay, "b", watchIn{Room: "r", Text: bad}, nil); code != http.StatusBadRequest {
			t.Fatalf("%q: %d", bad, code)
		}
	}
	if code := watchPost(b, (*bot).watchChatSay, "x", watchIn{Room: "r", Text: "hi"}, nil); code != http.StatusForbidden {
		t.Fatalf("outsider: %d", code)
	}
	long := watchChatText(strings.Repeat("ab ", 400))
	if n := len([]rune(long)); n > watchChatRunes {
		t.Fatalf("long message kept %d runes", n)
	}
	// Only the author, the host or staff take a message down.
	id := v.Chat[0].ID
	if code := watchPost(b, (*bot).watchChatSay, "c", watchIn{Room: "r", Act: "delete", ID: id}, nil); code != http.StatusForbidden {
		t.Fatalf("other member deletes: %d", code)
	}
	if code := watchPost(b, (*bot).watchChatSay, "a", watchIn{Room: "r", Act: "delete", ID: id}, &v); code != 200 || len(v.Chat) != 0 {
		t.Fatalf("host deletes: %d %+v", code, v.Chat)
	}
	// The room keeps the latest messages only.
	r := b.watch.only()
	for k := 0; k < watchChatKeep+5; k++ {
		r.chatN++
		r.chat = append(r.chat, watchChat{ID: r.chatN, User: "c", Text: "x"})
	}
	v = watchSee(b, (*bot).watchChatSay, "c", watchIn{Room: "r", Text: "last"})
	if len(v.Chat) != watchChatKeep || v.Chat[len(v.Chat)-1].Text != "last" {
		t.Fatalf("kept %d, last %+v", len(v.Chat), v.Chat[len(v.Chat)-1])
	}
}

func TestWatchLadder(t *testing.T) {
	master := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=400000,RESOLUTION=426x240,AUDIO="a"
240.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=900000,RESOLUTION=640x360,AUDIO="a"
360.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=1400000,RESOLUTION=854x480,AUDIO="a"
480.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=1300000,RESOLUTION=854x480,AUDIO="other"
480b.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2500000,RESOLUTION=1280x720,AUDIO="a"
720.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080,AUDIO="a"
1080.m3u8
`
	out := trimMaster(master, 720, false)
	var uris []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasSuffix(line, ".m3u8") {
			uris = append(uris, line)
		}
	}
	if strings.Join(uris, " ") != "720.m3u8 480.m3u8 240.m3u8" {
		t.Fatalf("ladder %v:\n%s", uris, out)
	}
	// Without sizes, bandwidth decides.
	out = trimMaster("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=3000000\nmid.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=800000\nlow.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=6000000\nhigh.m3u8\n", 720, false)
	if !strings.Contains(out, "mid.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=800000\nlow.m3u8") || strings.Contains(out, "high") {
		t.Fatalf("unsized:\n%s", out)
	}
	// One variant stays one.
	out = trimMaster("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=3000000,RESOLUTION=1280x720\nonly.m3u8\n", 720, false)
	if strings.Count(out, "#EXT-X-STREAM-INF") != 1 {
		t.Fatalf("single:\n%s", out)
	}
}

// A page remembers the last check it answered, so two rooms (or one room
// opened again) must never hand out the same check id.
func TestWatchHoldIDsNeverRepeat(t *testing.T) {
	now := time.Now()
	room := func() *watchRoom {
		return &watchRoom{members: map[string]*watchMember{"a": {}, "b": {}}}
	}
	first, second := room(), room()
	first.holdAt(0, watchReadyFirst, now)
	second.holdAt(0, watchReadyFirst, now)
	if first.hold == 0 || first.hold == second.hold {
		t.Fatalf("hold ids %d and %d", first.hold, second.hold)
	}
	was := second.hold
	second.holdAt(30, watchReadyWait, now)
	if second.hold == was {
		t.Fatal("the next check in a room kept the old id")
	}
}

// Skips one after another: every skip starts a fresh check at the new spot,
// a ready for an earlier one doesn't count, and the room plays from the last
// spot once everyone has it.
func TestWatchReadyCheckSkipsInARow(t *testing.T) {
	b, now := watchBot(t, "https://cdn.example/ep.mp4")
	b.store.update(func(d *storeData) { d.WatchTickets = map[string]int{"a": 1} })
	watchPost(b, (*bot).watchOpen, "a", watchIn{Room: "r", Channel: "vc"}, nil)
	watchPost(b, (*bot).watchOpen, "b", watchIn{Room: "r", Channel: "vc"}, nil)
	watchPost(b, (*bot).watchPick, "a", watchIn{Room: "r", Show: 7, Ep: 1}, nil)
	waitLoaded(t, b)
	watchAllReady(b)
	r := b.watch.only()
	watchPost(b, (*bot).watchBeat, "a", watchIn{Room: "r", Pos: 100, Playing: true, Dur: 1400}, nil)

	// The host presses +10s five times before anyone has loaded.
	var holds []int
	var v watchView
	for k := 1; k <= 5; k++ {
		v = watchSee(b, (*bot).watchCtl, "a", watchIn{Room: "r", Act: "forward"})
		if v.Hold == 0 || v.Playing || v.Pos != 100+float64(10*k) {
			t.Fatalf("forward %d: %+v", k, v)
		}
		if len(holds) > 0 && v.Hold == holds[len(holds)-1] {
			t.Fatalf("forward %d kept the old check", k)
		}
		holds = append(holds, v.Hold)
		*now = now.Add(300 * time.Millisecond)
	}
	// Readies for the earlier checks arrive late and count for nothing.
	for _, id := range holds[:4] {
		watchPost(b, (*bot).watchReady, "a", watchIn{Room: "r", ID: id}, nil)
		watchPost(b, (*bot).watchReady, "b", watchIn{Room: "r", ID: id}, nil)
	}
	if r.hold != holds[4] || r.playing || len(r.holdWaiting()) != 2 {
		t.Fatalf("stale readies let go: hold %d playing %v waiting %v", r.hold, r.playing, r.holdWaiting())
	}
	watchPost(b, (*bot).watchReady, "a", watchIn{Room: "r", ID: holds[4]}, nil)
	v = watchSee(b, (*bot).watchReady, "b", watchIn{Room: "r", ID: holds[4]})
	if v.Hold != 0 || !v.Playing || v.Pos != 150 {
		t.Fatalf("after the last skip: %+v", v)
	}

	// A member asks for +10s three times, the host says yes each time.
	for k := 1; k <= 3; k++ {
		v = watchSee(b, (*bot).watchAskAPI, "b", watchIn{Room: "r", Kind: "forward"})
		if len(v.Asks) != 1 {
			t.Fatalf("ask %d: %+v", k, v.Asks)
		}
		v = watchSee(b, (*bot).watchAnswer, "a", watchIn{Room: "r", ID: v.Asks[0].ID, OK: true})
		if v.Hold == 0 || v.Pos != 150+float64(10*k) || len(v.Asks) != 0 {
			t.Fatalf("answer %d: %+v", k, v)
		}
	}
	// Both load the last spot; the host's ready reaches the bot before the
	// member's, the room plays only after both.
	v = watchSee(b, (*bot).watchReady, "a", watchIn{Room: "r", ID: r.hold})
	if v.Playing || len(v.HoldWaiting) != 1 {
		t.Fatalf("one of two ready: %+v", v)
	}
	v = watchSee(b, (*bot).watchReady, "b", watchIn{Room: "r", ID: r.hold})
	if v.Hold != 0 || !v.Playing || v.Pos != 180 {
		t.Fatalf("after the asked skips: %+v", v)
	}

	// A seek into the opening waits at its end instead, and the room plays
	// from there once both have it, with nothing left to skip.
	r.autoSkipOP = true
	r.skipIntervals = watchSkipIntervals{OP: []watchSkipSpan{{Start: 185, End: 275}}}
	v = watchSee(b, (*bot).watchCtl, "a", watchIn{Room: "r", Act: "seek", Pos: 186})
	if v.Hold == 0 || v.Playing || v.Pos != 275 {
		t.Fatalf("seek into the opening: %+v", v)
	}
	// And +10s pressed twice inside it lands past it too.
	r.skipIntervals.OP = []watchSkipSpan{{Start: 280, End: 370}}
	watchSee(b, (*bot).watchCtl, "a", watchIn{Room: "r", Act: "forward"})
	v = watchSee(b, (*bot).watchCtl, "a", watchIn{Room: "r", Act: "forward"})
	if v.Hold == 0 || v.Pos != 380 {
		t.Fatalf("forward through the opening: %+v", v)
	}
	watchPost(b, (*bot).watchReady, "a", watchIn{Room: "r", ID: v.Hold}, nil)
	watchPost(b, (*bot).watchReady, "b", watchIn{Room: "r", ID: v.Hold}, nil)
	seq := r.skipSeq
	*now = now.Add(time.Second)
	r.members["a"].seen, r.members["b"].seen = *now, *now
	b.watchStep(r, *now)
	if r.hold != 0 || !r.playing || r.skipSeq != seq || r.position(*now) != 381 {
		t.Fatalf("after the skips: hold %d playing %v seq %d->%d pos %v", r.hold, r.playing, seq, r.skipSeq, r.position(*now))
	}
}

// Members can ask for any jump: ahead or back by some seconds, or to a spot.
// A second jump while one waits changes the waiting one.
func TestWatchAskJumps(t *testing.T) {
	b, _ := watchBot(t, "https://cdn.example/ep.mp4")
	b.store.update(func(d *storeData) { d.WatchTickets = map[string]int{"a": 1} })
	watchPost(b, (*bot).watchOpen, "a", watchIn{Room: "r", Channel: "vc"}, nil)
	watchPost(b, (*bot).watchOpen, "b", watchIn{Room: "r", Channel: "vc"}, nil)
	watchPost(b, (*bot).watchPick, "a", watchIn{Room: "r", Show: 7, Ep: 1}, nil)
	waitLoaded(t, b)
	watchAllReady(b)
	r := b.watch.only()
	watchPost(b, (*bot).watchBeat, "a", watchIn{Room: "r", Pos: 100, Playing: true, Dur: 1400}, nil)
	answer := func(ok bool) watchView {
		t.Helper()
		b.watch.mu.Lock()
		v := b.watchViewOf(r, "a", false, watchNow())
		b.watch.mu.Unlock()
		if len(v.Asks) != 1 {
			t.Fatalf("host sees %+v", v.Asks)
		}
		v = watchSee(b, (*bot).watchAnswer, "a", watchIn{Room: "r", ID: v.Asks[0].ID, OK: ok})
		watchAllReady(b)
		return v
	}

	// +10s pressed five times is one request for +50s.
	var v watchView
	for k := 1; k <= 5; k++ {
		v = watchSee(b, (*bot).watchAskAPI, "b", watchIn{Room: "r", Kind: "forward", By: float64(10 * k)})
		if len(v.Asks) != 1 || v.Asks[0].By != float64(10*k) || v.Asks[0].ID != 1 {
			t.Fatalf("ask %d: %+v", k, v.Asks)
		}
	}
	answer(true)
	if r.pos != 150 || !r.playing {
		t.Fatalf("+50s: pos %v playing %v", r.pos, r.playing)
	}
	// Back 30s, then a jump to a spot that replaces it.
	watchPost(b, (*bot).watchAskAPI, "b", watchIn{Room: "r", Kind: "forward", By: -30}, nil)
	v = watchSee(b, (*bot).watchAskAPI, "b", watchIn{Room: "r", Kind: "seek", Pos: 725.4})
	if len(v.Asks) != 1 || v.Asks[0].Kind != "seek" || v.Asks[0].Pos != 725 {
		t.Fatalf("seek ask: %+v", v.Asks)
	}
	answer(true)
	if r.pos != 725 {
		t.Fatalf("jump to 12:05: pos %v", r.pos)
	}
	watchPost(b, (*bot).watchAskAPI, "b", watchIn{Room: "r", Kind: "forward", By: -30}, nil)
	answer(true)
	if r.pos != 695 {
		t.Fatalf("back 30s: pos %v", r.pos)
	}
	// No moves the room nowhere; a spot past the end isn't a request.
	watchPost(b, (*bot).watchAskAPI, "b", watchIn{Room: "r", Kind: "seek", Pos: 10}, nil)
	answer(false)
	if r.pos != 695 {
		t.Fatalf("said no: pos %v", r.pos)
	}
	if code := watchPost(b, (*bot).watchAskAPI, "b", watchIn{Room: "r", Kind: "seek", Pos: 1500}, nil); code != http.StatusBadRequest {
		t.Fatalf("past the end: %d", code)
	}
	// A jump doesn't replace a different request.
	watchPost(b, (*bot).watchAskAPI, "b", watchIn{Room: "r", Kind: "pause"}, nil)
	if code := watchPost(b, (*bot).watchAskAPI, "b", watchIn{Room: "r", Kind: "seek", Pos: 20}, nil); code != http.StatusTooManyRequests {
		t.Fatalf("jump over a pause request: %d", code)
	}
}

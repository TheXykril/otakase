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

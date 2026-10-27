package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// A restart keeps the room: same people, same host, same ticket, the
// episode loads again and waits for everyone at the spot it was at.
func TestWatchRoomSurvivesRestart(t *testing.T) {
	b, now := watchBot(t, "https://cdn.example/ep.mp4")
	b.store.update(func(d *storeData) { d.WatchTickets = map[string]int{"a": 1} })
	watchPost(b, (*bot).watchOpen, "a", watchIn{Room: "r", Channel: "vc"}, nil)
	watchPost(b, (*bot).watchOpen, "b", watchIn{Room: "r", Channel: "vc"}, nil)
	watchPost(b, (*bot).watchPick, "a", watchIn{Room: "r", Show: 7, Ep: 3, Dub: true}, nil)
	waitLoaded(t, b)
	watchAllReady(b)
	watchPost(b, (*bot).watchChatSay, "b", watchIn{Room: "r", Text: "hi"}, nil)
	watchPost(b, (*bot).watchHandOver, "a", watchIn{Room: "r", User: "b"}, nil)
	*now = now.Add(100 * time.Second)
	b.saveWatchRooms()
	if b.watchTickets("a") != 0 {
		t.Fatal("ticket not spent before the restart")
	}

	b.watch.mu.Lock()
	dub := b.watch.only().dub
	b.watch.mu.Unlock()
	b.watch.bg.Wait()

	// The bot comes back up 20 seconds later from the same store file.
	*now = now.Add(20 * time.Second)
	restart := func() *bot {
		nb := testGamesBot(t)
		nb.cfg.GuildID = "g"
		nb.store = openStore(b.store.path)
		t.Cleanup(func() {
			nb.watch.mu.Lock()
			for _, r := range nb.watch.rooms {
				r.finishMedia()
			}
			nb.watch.mu.Unlock()
			nb.watch.bg.Wait()
		})
		nb.restoreWatchRooms()
		return nb
	}
	b2 := restart()
	waitLoaded(t, b2)
	b2.watch.mu.Lock()
	r := b2.watch.only()
	if r == nil || r.host != "b" || r.owner != "a" || !r.spent || r.ep != 3 || r.dub != dub || len(r.members) != 2 ||
		r.hold == 0 || r.holdPos < 99 || r.holdPos > 101 || len(r.chat) != 1 {
		b2.watch.mu.Unlock()
		t.Fatalf("restored room: %+v", r)
	}
	b2.watch.mu.Unlock()
	// Members polling with the version they had get the room, not "over".
	v := watchSee(b2, (*bot).watchOpen, "b", watchIn{Room: "r", Channel: "vc"})
	if v.Phase != "play" || !v.IsHost || v.Hold == 0 {
		t.Fatalf("view after the restart: %+v", v)
	}
	if b2.watchTickets("a") != 0 {
		t.Fatal("the restart gave the ticket back or spent another")
	}

	// A room whose time ran out while the bot was down stays closed.
	b2.saveWatchRooms()
	*now = now.Add(watchRoomLength)
	b2.watch.bg.Wait()
	b3 := restart()
	b3.watch.mu.Lock()
	n := len(b3.watch.rooms)
	b3.watch.mu.Unlock()
	if n != 0 {
		t.Fatal("a room past its time came back")
	}
}

// Pages stay signed in across a restart, and the store holds no token.
func TestActivitySessionsSurviveRestart(t *testing.T) {
	b := testGamesBot(t)
	token := b.acts.add("u")
	b.saveActivitySessions()
	raw, _ := os.ReadFile(b.store.path)
	if strings.Contains(string(raw), token) {
		t.Fatal("the store holds the token itself")
	}
	b2 := &bot{store: openStore(b.store.path)}
	b2.loadActivitySessions()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	if u := b2.acts.user(r); u != "u" {
		t.Fatalf("after the restart: %q", u)
	}
	r.Header.Set("Authorization", "Bearer nope")
	if b2.acts.user(r) != "" {
		t.Fatal("a wrong token signed in")
	}
}

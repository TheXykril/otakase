package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func watchAppToken(b *bot, user string) string {
	token := strings.Repeat("a", 64)
	b.store.update(func(d *storeData) {
		if d.AppLinks == nil {
			d.AppLinks = map[string]appLink{}
		}
		d.AppLinks[hashToken(token)] = appLink{User: user}
	})
	return token
}
func watchAppCall(b *bot, token, method, path string, in any, handler func(*bot, http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
	data, _ := json.Marshal(in)
	r := httptest.NewRequest(method, "/activity/api/watch/"+path, strings.NewReader(string(data)))
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler(b, w, r)
	return w
}
func TestWatchAppExistingRoomProtocol(t *testing.T) {
	b, _ := watchBot(t, "https://cdn.example/episode.m3u8")
	b.store.update(func(d *storeData) { d.WatchTickets = map[string]int{"host": 1} })
	if code := watchPost(b, (*bot).watchOpen, "host", watchIn{Room: "room-app", Channel: "vc"}, nil); code != 200 {
		t.Fatalf("open: %d", code)
	}
	token := watchAppToken(b, "viewer")
	status := watchAppCall(b, token, "GET", "status", nil, (*bot).watchStatus)
	if status.Code != 200 || !strings.Contains(status.Body.String(), "room-app") {
		t.Fatalf("status: %d %s", status.Code, status.Body)
	}
	joined := watchAppCall(b, token, "POST", "open", watchIn{Room: "room-app", Channel: "vc"}, (*bot).watchOpen)
	var view watchView
	_ = json.Unmarshal(joined.Body.Bytes(), &view)
	if joined.Code != 200 || view.Me != "viewer" || view.IsHost {
		t.Fatalf("join: %d %s", joined.Code, joined.Body)
	}
	if code := watchAppCall(b, token, "POST", "open", watchIn{Room: "new-room", Channel: "vc"}, (*bot).watchOpen).Code; code != 404 {
		t.Fatalf("app created a room: %d", code)
	}
	if code := watchAppCall(b, token, "POST", "ctl", watchIn{Room: "room-app", Act: "pause"}, (*bot).watchCtl).Code; code != 403 {
		t.Fatalf("viewer controlled host: %d", code)
	}
	if code := watchAppCall(b, token, "POST", "leave", watchIn{Room: "room-app"}, (*bot).watchLeave).Code; code != 200 {
		t.Fatalf("leave: %d", code)
	}
	b.store.update(func(d *storeData) { delete(d.AppLinks, hashToken(token)) })
	if code := watchAppCall(b, token, "GET", "status", nil, (*bot).watchStatus).Code; code != 401 {
		t.Fatalf("revoked token: %d", code)
	}
}
func TestWatchAppPrivateCallUsesDiscordVoiceState(t *testing.T) {
	b, _ := watchBot(t, "https://cdn.example/episode.m3u8")
	b.store.update(func(d *storeData) { d.WatchTickets = map[string]int{"host": 1} })
	if code := watchPost(b, (*bot).watchOpen, "host", watchIn{Room: "private-app", Channel: "nsfw"}, nil); code != 200 {
		t.Fatalf("open: %d", code)
	}
	token := watchAppToken(b, "viewer")
	if code := watchAppCall(b, token, "POST", "open", watchIn{Room: "private-app", Channel: "nsfw"}, (*bot).watchOpen).Code; code != 403 {
		t.Fatalf("forged channel accepted: %d", code)
	}
	if status := watchAppCall(b, token, "GET", "status", nil, (*bot).watchStatus); strings.Contains(status.Body.String(), "private-app") {
		t.Fatal("private room ID exposed outside its call")
	}
	b.s = &discordgo.Session{State: discordgo.NewState(), StateEnabled: true}
	_ = b.s.State.GuildAdd(&discordgo.Guild{ID: "g", Roles: []*discordgo.Role{{ID: "everyone", Name: "@everyone"}}, VoiceStates: []*discordgo.VoiceState{{UserID: "viewer", ChannelID: "nsfw", GuildID: "g"}}})
	_ = b.s.State.MemberAdd(&discordgo.Member{GuildID: "g", User: &discordgo.User{ID: "viewer", Username: "Viewer"}})
	if status := watchAppCall(b, token, "GET", "status", nil, (*bot).watchStatus); !strings.Contains(status.Body.String(), "private-app") {
		t.Fatalf("member cannot see own call: %s", status.Body)
	}
	if code := watchAppCall(b, token, "POST", "open", watchIn{Room: "private-app", Channel: "nsfw"}, (*bot).watchOpen).Code; code != 200 {
		t.Fatalf("member join: %d", code)
	}
	b.s.State.OnInterface(b.s, &discordgo.VoiceStateUpdate{VoiceState: &discordgo.VoiceState{UserID: "viewer", GuildID: "g", ChannelID: ""}})
	if code := watchAppCall(b, token, "GET", "state?room=private-app&v=-1", nil, (*bot).watchState).Code; code != 403 {
		t.Fatalf("state after leaving voice: %d", code)
	}
	if code := watchAppCall(b, token, "POST", "leave", watchIn{Room: "private-app"}, (*bot).watchLeave).Code; code != 200 {
		t.Fatalf("leave after voice leave: %d", code)
	}
}
func TestWatchAppTokenDoesNotGrantOtherActivityRoutes(t *testing.T) {
	b, _ := watchBot(t, "https://cdn.example/episode.m3u8")
	token := watchAppToken(b, "viewer")
	for _, path := range []string{"/activity/api/board", "/activity/api/watch/pick", "/activity/api/watch/end", "/activity/api/watch/take"} {
		r := httptest.NewRequest("POST", path, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		if user := b.actUser(w, r); user != "" || w.Code != 401 {
			t.Fatalf("app token granted %s: %s %d", path, user, w.Code)
		}
	}
}
func TestWatchAppSameAccountAsHost(t *testing.T) {
	b, now := watchBot(t, "https://cdn.example/episode.m3u8")
	b.store.update(func(d *storeData) { d.WatchTickets = map[string]int{"host": 1} })
	if code := watchPost(b, (*bot).watchOpen, "host", watchIn{Room: "room-same", Channel: "vc"}, nil); code != 200 {
		t.Fatalf("open: %d", code)
	}
	token := watchAppToken(b, "host")
	joined := watchAppCall(b, token, "POST", "open", watchIn{Room: "room-same", Channel: "vc"}, (*bot).watchOpen)
	var view watchView
	_ = json.Unmarshal(joined.Body.Bytes(), &view)
	if joined.Code != 200 || !view.IsHost || !view.MeActivity {
		t.Fatalf("second device join: %d %s", joined.Code, joined.Body)
	}
	b.watch.mu.Lock()
	cur := b.watch.room("room-same")
	cur.phase = "play"
	cur.setPos(100, true, *now)
	b.watch.mu.Unlock()
	if code := watchAppCall(b, token, "POST", "beat", watchIn{Room: "room-same", Pos: 900, Playing: false}, (*bot).watchBeat).Code; code != 200 {
		t.Fatalf("app beat: %d", code)
	}
	b.watch.mu.Lock()
	pos, playing := cur.position(*now), cur.playing
	b.watch.mu.Unlock()
	if pos != 100 || !playing {
		t.Fatalf("app beat moved a room its Activity drives: %v %v", pos, playing)
	}
	if code := watchAppCall(b, token, "POST", "leave", watchIn{Room: "room-same"}, (*bot).watchLeave).Code; code != 200 {
		t.Fatalf("app leave: %d", code)
	}
	b.watch.mu.Lock()
	_, still := cur.members["host"]
	b.watch.mu.Unlock()
	if !still {
		t.Fatal("leaving on the app took the Activity out of the room")
	}
	// Back on the app, then the Activity closes: the app keeps the room
	// and drives it from then on.
	watchAppCall(b, token, "POST", "open", watchIn{Room: "room-same", Channel: "vc"}, (*bot).watchOpen)
	if code := watchPost(b, (*bot).watchLeave, "host", watchIn{Room: "room-same"}, nil); code != 200 {
		t.Fatalf("activity leave: %d", code)
	}
	state := watchAppCall(b, token, "GET", "state?room=room-same&v=-1", nil, (*bot).watchState)
	view = watchView{}
	_ = json.Unmarshal(state.Body.Bytes(), &view)
	if state.Code != 200 || !view.IsHost || view.MeActivity {
		t.Fatalf("app after Activity left: %d %s", state.Code, state.Body)
	}
	watchAppCall(b, token, "POST", "beat", watchIn{Room: "room-same", Pos: 300, Playing: true}, (*bot).watchBeat)
	b.watch.mu.Lock()
	pos = cur.position(*now)
	b.watch.mu.Unlock()
	if pos != 300 {
		t.Fatalf("app host beat ignored once alone: %v", pos)
	}
}

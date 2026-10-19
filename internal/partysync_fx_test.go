package internal

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestPartyOverlayASS(t *testing.T) {
	now := time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)
	chat := []partyChatLine{
		{Name: "Old", Text: "gone", At: now.Add(-time.Minute)},
		{Name: "Ann", Text: "the {\\b1}op{\\b0} slaps", At: now.Add(-time.Second)},
	}
	reacts := []partyReaction{
		{Name: "Bo", Emoji: "🔥", At: now.Add(-time.Second)},
		{Name: "Cy", Emoji: "😂", At: now.Add(-10 * time.Second)},
	}
	got := partyOverlayASS(chat, reacts, true, now, 1280)
	if strings.Contains(got, "gone") || strings.Contains(got, "😂") {
		t.Errorf("old lines and reactions should be gone: %q", got)
	}
	if !strings.Contains(got, "{\\b1}Ann{\\b0}  the \\{\\⁠b1\\}op") {
		t.Errorf("chat line, escaped: %q", got)
	}
	if !strings.Contains(got, "\\pos(1250,500)") || !strings.Contains(got, "🔥") {
		t.Errorf("a second-old reaction rises 20 px from 520: %q", got)
	}
	if got := partyOverlayASS(chat, nil, false, now, 1280); got != "" {
		t.Errorf("chat off: %q", got)
	}
}

func TestPartySkipTarget(t *testing.T) {
	st := SkipTimes{Op: Skip{Start: 60, End: 150}, Ed: Skip{Start: 1300, End: 1390}}
	for _, c := range []struct{ pos, want float64 }{
		{70, 150}, {57, 150}, {1310, 1390}, {200, 285}, {10, 95},
	} {
		if got := partySkipTarget(c.pos, st); got != c.want {
			t.Errorf("skip from %v: %v, want %v", c.pos, got, c.want)
		}
	}
	if got := partySkipTarget(30, SkipTimes{}); got != 115 {
		t.Errorf("no skip times: %v", got)
	}
}

func TestPartyFXMessagesAndKeys(t *testing.T) {
	var mu sync.Mutex
	var sent []map[string]string
	fx := newPartyFX(func(v any) {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, v.(map[string]string))
	})
	voted := ""
	fx.onVoted = func(socket, what string) { voted = what }
	for k := 0; k < partyChatLines+2; k++ {
		if !fx.handle(partyMessage{Type: "chat", Name: "Ann", Text: "hi"}) {
			t.Fatal("chat is the extras' own")
		}
	}
	if len(fx.chat) != partyChatLines {
		t.Errorf("kept %d chat lines", len(fx.chat))
	}
	if !fx.handle(partyMessage{Type: "react", Name: "Bo", Emoji: "🔥"}) || len(fx.reacts) != 1 {
		t.Error("reaction")
	}
	if fx.handle(partyMessage{Type: "state"}) || fx.handle(partyMessage{Type: "ended"}) {
		t.Error("the party's own messages are left to it")
	}
	if !fx.handle(partyMessage{Type: "voted", What: "skip"}) || voted != "" {
		t.Error("nothing is done without a player")
	}
	fx.key("next")
	fx.key("chat")
	if len(sent) != 1 || sent[0]["type"] != "vote" || sent[0]["what"] != "next" {
		t.Errorf("sent: %v", sent)
	}
	if !fx.chatOff {
		t.Error("Alt+c should hide the chat")
	}
}

// fakeWatchWithMeBot answers /v1/party/start and hosts the party's
// WebSocket, recording what the app sends.
func fakeWatchWithMeBot(t *testing.T, got chan<- map[string]any) *httptest.Server {
	t.Helper()
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" {
			http.Error(w, `{"error":"link"}`, http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/party/start":
			body, _ := io.ReadAll(r.Body)
			var in map[string]any
			json.Unmarshal(body, &in)
			if in["anilist_id"] != 154587.0 || in["episode"] != 3.0 {
				t.Errorf("start: %s", body)
			}
			w.Write([]byte(`{"id":"4821093371526630","join_url":"https://otakase.xyverion.com/open/?party=4821093371526630"}`))
		case "/v1/party/4821093371526630/ws":
			ws, err := up.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer ws.Close()
			ws.WriteJSON(map[string]any{"type": "welcome", "host": true, "show": "Frieren", "server_at": time.Now().UnixMilli()})
			for {
				var m map[string]any
				if err := ws.ReadJSON(&m); err != nil {
					return
				}
				select {
				case got <- m:
				default:
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestWatchWithMeHostsAndEndsWhenIdle(t *testing.T) {
	got := make(chan map[string]any, 100)
	srv := fakeWatchWithMeBot(t, got)
	config := newDiscordTestConfig(t, srv.URL)
	if err := saveDiscordLink(config, discordLink{Token: "good"}); err != nil {
		t.Fatal(err)
	}
	oldIdle, oldPoll := watchWithMeIdle, watchWithMePoll
	watchWithMeIdle, watchWithMePoll = 300*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { watchWithMeIdle, watchWithMePoll = oldIdle, oldPoll })

	anime := &Anime{AnilistId: 154587}
	anime.Ep.Number = 3
	startWatchWithMe(config, anime, "/nonexistent/mpv.sock") // off: nothing happens
	if watchWithMeURL() != "" {
		t.Fatal("started with the option off")
	}
	config.DiscordWatchWithMe = true
	startWatchWithMe(config, anime, "/nonexistent/mpv.sock")
	deadline := time.Now().Add(5 * time.Second)
	for watchWithMeURL() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if u := watchWithMeURL(); u != "https://otakase.xyverion.com/open/?party=4821093371526630" {
		t.Fatalf("join link: %q", u)
	}
	// Nothing plays, so the party ends for everyone a moment later.
	timeout := time.After(5 * time.Second)
	for {
		select {
		case m := <-got:
			if m["type"] == "end" {
				for watchWithMeURL() != "" && time.Now().Before(deadline) {
					time.Sleep(10 * time.Millisecond)
				}
				if watchWithMeURL() != "" {
					t.Error("the button should go with the party")
				}
				return
			}
		case <-timeout:
			t.Fatal("no end sent")
		}
	}
}

func TestPartyStartLabel(t *testing.T) {
	now := time.Date(2026, 10, 14, 18, 0, 0, 0, time.Local)
	for _, c := range []struct {
		start time.Time
		want  string
	}{
		{now.Add(2 * time.Hour), "today 20:00"},
		{now.Add(26 * time.Hour), "tomorrow 20:00"},
		{now.Add(-5 * time.Minute), "started 17:55"},
		{time.Date(2026, 10, 20, 21, 30, 0, 0, time.Local), "Tue 20 Oct 21:30"},
	} {
		if got := partyStartLabel(c.start, now); got != c.want {
			t.Errorf("%v: %q, want %q", c.start, got, c.want)
		}
	}
	p := discordParty{Title: "Frieren", Start: now.Add(2 * time.Hour), Going: 3, Sync: true, RSVP: true}
	if got := partyLabel(p, now); got != "Frieren · today 20:00 · 3 going · synced · you're in" {
		t.Errorf("label: %q", got)
	}
	p.Live = true
	if got := partyLabel(p, now); !strings.Contains(got, "on now") {
		t.Errorf("live label: %q", got)
	}
}

func TestDiscordPartiesFetchAndRSVP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" {
			http.Error(w, `{"error":"link"}`, http.StatusUnauthorized)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/parties":
			w.Write([]byte(`{"parties":[{"id":"123","title":"Frieren","start":"2026-10-14T19:00:00Z","anilist_id":154587,"sync":true,"host":"Alex","going":3,"rsvp":false,"live":false,"join_url":"https://otakase.xyverion.com/open/?party=123","event_url":"https://discord.com/events/1/123"}]}`))
		case "POST /v1/party/123/rsvp":
			body, _ := io.ReadAll(r.Body)
			if string(body) != "{\"going\":true}\n" && string(body) != `{"going":true}` {
				t.Errorf("rsvp body: %q", body)
			}
			w.Write([]byte(`{"going":true,"count":4}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	config := newDiscordTestConfig(t, srv.URL)
	saveDiscordLink(config, discordLink{Token: "good"})
	parties, err := fetchDiscordParties(config)
	if err != nil || len(parties) != 1 {
		t.Fatalf("parties: %+v %v", parties, err)
	}
	if p := parties[0]; p.ID != "123" || !p.Sync || p.AnilistID != 154587 || p.Going != 3 || p.Host != "Alex" {
		t.Errorf("party: %+v", p)
	}
	going, count, err := rsvpDiscordParty(config, "123", true)
	if err != nil || !going || count != 4 {
		t.Errorf("rsvp: %v %d %v", going, count, err)
	}
}

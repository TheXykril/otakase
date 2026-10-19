package internal

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakePartyBot accepts token "good" for party 42: it welcomes, answers
// pings, sends one host state and, on the first connection only, drops the
// line to make the app reconnect.
func fakePartyBot(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var conns atomic.Int32
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/party/42/ws" {
			http.Error(w, `{"error":"no synced watch party with that id"}`, http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer good" {
			http.Error(w, `{"error":"link"}`, http.StatusUnauthorized)
			return
		}
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		n := conns.Add(1)
		ws.WriteJSON(map[string]any{"type": "welcome", "host": false, "show": "Frieren", "anilist_id": 154587, "server_at": time.Now().UnixMilli()})
		ws.WriteJSON(map[string]any{"type": "state", "anilist_id": 154587, "episode": 12, "playing": true, "position": 30.5, "sent_at": time.Now().UnixMilli()})
		ws.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			var m map[string]any
			if err := ws.ReadJSON(&m); err != nil {
				return
			}
			switch m["type"] {
			case "ping":
				ws.WriteJSON(map[string]any{"type": "pong", "t": m["t"], "server_at": time.Now().UnixMilli() + 60_000})
				if n == 1 {
					return // drop the first connection
				}
			case "ready":
				ws.WriteJSON(map[string]any{"type": "ended"})
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &conns
}

func TestPartyClientErrors(t *testing.T) {
	srv, _ := fakePartyBot(t)
	if _, _, err := dialParty(srv.URL, "bad", "42", "test"); !errors.Is(err, errPartyNotLinked) {
		t.Errorf("bad token: %v", err)
	}
	if _, _, err := dialParty(srv.URL, "good", "7", "test"); !errors.Is(err, errPartyNotFound) {
		t.Errorf("unknown party: %v", err)
	}
	if _, _, err := dialParty("", "good", "42", "test"); !errors.Is(err, errPartyOff) {
		t.Errorf("no bot URL: %v", err)
	}
}

func TestPartyClientReconnects(t *testing.T) {
	srv, conns := fakePartyBot(t)
	c, welcome, err := dialParty(srv.URL, "good", "42", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if welcome.Host || welcome.Show != "Frieren" || welcome.AniListID != 154587 {
		t.Fatalf("welcome: %+v", welcome)
	}
	next := func(kind string) partyMessage {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case m, ok := <-c.In:
				if !ok {
					t.Fatalf("closed waiting for %s", kind)
				}
				if m.Type == kind {
					return m
				}
			case <-deadline:
				t.Fatalf("no %s", kind)
			}
		}
	}
	if s := next("state"); s.Episode != 12 || !s.Playing || s.Position != 30.5 {
		t.Fatalf("state: %+v", s)
	}
	select {
	case <-c.Connected:
	case <-time.After(5 * time.Second):
		t.Fatal("no reconnect after the line dropped")
	}
	if conns.Load() < 2 {
		t.Fatalf("connections: %d", conns.Load())
	}
	next("state") // sent again on the new connection
	// The pong put the bot's clock a minute ahead.
	if d := c.ServerNow() - time.Now().UnixMilli(); d < 55_000 || d > 65_000 {
		t.Errorf("clock offset: %d ms", d)
	}
	c.Send(map[string]any{"type": "ready", "episode": 12})
	next("ended")
	select {
	case _, ok := <-c.In:
		if ok {
			t.Error("In should close after the party ended")
		}
	case <-time.After(5 * time.Second):
		t.Error("In not closed")
	}
}

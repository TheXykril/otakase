package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gorilla/websocket"
)

const (
	hostTok   = "hosthosthosthosthosthosthosthosthost"
	memberTok = "membermembermembermembermembermember"
	otherTok  = "otherotherotherotherotherotherother0"
)

// partySyncServer runs the party WebSocket route with a host and two linked
// members, and a synced party ev1 hosted by h.
func partySyncServer(t *testing.T) *httptest.Server {
	t.Helper()
	b := &bot{store: openStore(filepath.Join(t.TempDir(), "s.json"))}
	b.store.update(func(d *storeData) {
		d.AppLinks[hashToken(hostTok)] = appLink{User: "h"}
		d.AppLinks[hashToken(memberTok)] = appLink{User: "m"}
		d.AppLinks[hashToken(otherTok)] = appLink{User: "o"}
		d.Parties["ev1"] = watchParty{Show: "Frieren", Host: "h", AniListID: 154587, Sync: true}
		d.Parties["plain"] = watchParty{Show: "Frieren", Host: "h"}
	})
	syncHub.mu.Lock() // what an earlier test left behind
	syncHub.rooms = map[string]*partyRoom{}
	syncHub.mu.Unlock()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/party/{id}/ws", func(w http.ResponseWriter, r *http.Request) { partySyncWS(b, w, r) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func dialParty(t *testing.T, srv *httptest.Server, id, token string) (*websocket.Conn, int) {
	t.Helper()
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/party/" + id + "/ws"
	h := http.Header{}
	if token != "" {
		h.Set("Authorization", "Bearer "+token)
	}
	ws, resp, err := websocket.DefaultDialer.Dial(u, h)
	if err != nil {
		if resp == nil {
			t.Fatalf("dial: %v", err)
		}
		return nil, resp.StatusCode
	}
	t.Cleanup(func() { ws.Close() })
	return ws, resp.StatusCode
}

// next reads frames until one of type kind arrives.
func next(t *testing.T, ws *websocket.Conn, kind string) map[string]any {
	t.Helper()
	ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("waiting for %s: %v", kind, err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("bad frame %s", data)
		}
		if m["type"] == kind {
			return m
		}
	}
}

func send(t *testing.T, ws *websocket.Conn, v any) {
	t.Helper()
	if err := ws.WriteJSON(v); err != nil {
		t.Fatal(err)
	}
}

func TestPartySyncRejects(t *testing.T) {
	srv := partySyncServer(t)
	if _, code := dialParty(t, srv, "ev1", ""); code != http.StatusUnauthorized {
		t.Errorf("no token: %d", code)
	}
	if _, code := dialParty(t, srv, "ev1", strings.Repeat("x", 40)); code != http.StatusUnauthorized {
		t.Errorf("unknown token: %d", code)
	}
	if _, code := dialParty(t, srv, "plain", memberTok); code != http.StatusNotFound {
		t.Errorf("party without sync: %d", code)
	}
	if _, code := dialParty(t, srv, "nope", memberTok); code != http.StatusNotFound {
		t.Errorf("unknown party: %d", code)
	}
	// The token may come in the query instead.
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/party/ev1/ws?token=" + memberTok
	ws, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("query token: %v", err)
	}
	ws.Close()
}

func TestPartySyncRelay(t *testing.T) {
	srv := partySyncServer(t)
	member, _ := dialParty(t, srv, "ev1", memberTok)
	if w := next(t, member, "welcome"); w["host"] != false || w["anilist_id"] != 154587.0 || w["show"] != "Frieren" {
		t.Fatalf("member welcome: %v", w)
	}
	host, _ := dialParty(t, srv, "ev1", hostTok)
	if w := next(t, host, "welcome"); w["host"] != true {
		t.Fatalf("host welcome: %v", w)
	}
	send(t, member, map[string]any{"type": "hello", "version": "26.7.0"})

	// Only the host leads: a member's state goes nowhere.
	send(t, member, map[string]any{"type": "state", "anilist_id": 1, "episode": 1, "playing": true, "position": 5})
	now := time.Now().UnixMilli()
	send(t, host, map[string]any{"type": "state", "anilist_id": 154587, "episode": 12, "playing": true, "position": 312.4, "sent_at": now})
	s := next(t, member, "state")
	if s["anilist_id"] != 154587.0 || s["episode"] != 12.0 || s["playing"] != true || s["position"] != 312.4 || s["sent_at"] != float64(now) {
		t.Fatalf("relayed state: %v", s)
	}

	send(t, member, map[string]any{"type": "ready", "episode": 12})
	for {
		m := next(t, host, "members")
		list := m["list"].([]any)
		if len(list) == 2 && list[1].(map[string]any)["ready"] == true {
			if list[0].(map[string]any)["host"] != true {
				t.Fatalf("host should be listed first: %v", list)
			}
			break
		}
	}

	send(t, member, map[string]any{"type": "ping", "t": 42})
	if p := next(t, member, "pong"); p["t"] != 42.0 || p["server_at"] == nil {
		t.Fatalf("pong: %v", p)
	}

	// A member joining later gets the last state straight away.
	other, _ := dialParty(t, srv, "ev1", otherTok)
	if s := next(t, other, "state"); s["episode"] != 12.0 {
		t.Fatalf("late joiner state: %v", s)
	}

	syncHub.end("ev1")
	next(t, member, "ended")
	member.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := member.ReadMessage(); !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
		t.Fatalf("after ended: %v", err)
	}
}

func TestPartySyncReconnectReplaces(t *testing.T) {
	srv := partySyncServer(t)
	first, _ := dialParty(t, srv, "ev1", memberTok)
	next(t, first, "welcome")
	second, _ := dialParty(t, srv, "ev1", memberTok)
	next(t, second, "welcome")
	first.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		if _, _, err := first.ReadMessage(); err != nil {
			break // the older connection was closed
		}
	}
	m := next(t, second, "members")
	if n := len(m["list"].([]any)); n != 1 {
		t.Fatalf("members after reconnect: %v", m)
	}
}

func TestPartySyncMessageTooBig(t *testing.T) {
	srv := partySyncServer(t)
	ws, _ := dialParty(t, srv, "ev1", memberTok)
	next(t, ws, "welcome")
	ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"hello","version":"`+strings.Repeat("x", 5000)+`"}`))
	ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		if _, _, err := ws.ReadMessage(); err != nil {
			if !websocket.IsCloseError(err, websocket.CloseMessageTooBig) && !strings.Contains(err.Error(), "close") &&
				!strings.Contains(err.Error(), "reset") && !strings.Contains(err.Error(), "EOF") {
				t.Fatalf("expected a close, got %v", err)
			}
			return
		}
	}
}

func TestPartyHubCapAndRate(t *testing.T) {
	h := newPartyHub()
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	h.now = func() time.Time { return now }
	p := watchParty{Host: "h", Sync: true}
	conn := func(user string, host bool) *partyConn {
		return &partyConn{user: user, host: host, out: make(chan []byte, 1000)}
	}
	for k := 0; k < partySyncMaxMembers; k++ {
		if !h.join("ev", p, conn(string(rune('a'+k)), false)) {
			t.Fatalf("member %d refused", k)
		}
	}
	if !h.full("ev", "new") || h.join("ev", p, conn("new", false)) {
		t.Error("26th member let in")
	}
	if h.full("ev", "a") || !h.join("ev", p, conn("a", false)) {
		t.Error("reconnecting member refused")
	}
	if !h.join("ev", p, conn("h", true)) {
		t.Error("host refused from a full party")
	}

	c := conn("z", false)
	h2 := newPartyHub()
	h2.now = h.now
	h2.join("ev", p, c)
	for len(c.out) > 0 {
		<-c.out
	}
	for k := 0; k < 15; k++ {
		h2.handle("ev", c, []byte(`{"type":"ping","t":1}`))
	}
	if len(c.out) != partySyncRate {
		t.Errorf("pongs in one second: %d, want %d", len(c.out), partySyncRate)
	}
	now = now.Add(time.Second)
	h2.handle("ev", c, []byte(`{"type":"ping","t":1}`))
	if len(c.out) != partySyncRate+1 {
		t.Errorf("a new second should allow more: %d", len(c.out))
	}
}

func TestCleanState(t *testing.T) {
	now := time.UnixMilli(1_000_000_000)
	good := partyState{AniListID: 1, Episode: 2, Position: 10, SentAt: now.UnixMilli() - 200}
	if s, ok := cleanState(good, now); !ok || s.SentAt != good.SentAt || s.Type != "state" {
		t.Errorf("good state: %+v %v", s, ok)
	}
	far := good
	far.SentAt = 5
	if s, _ := cleanState(far, now); s.SentAt != now.UnixMilli() {
		t.Errorf("far-off sent_at should be restamped: %d", s.SentAt)
	}
	for _, bad := range []partyState{{Episode: 1}, {AniListID: 1}, {AniListID: 1, Episode: 1, Position: -1}} {
		if _, ok := cleanState(bad, now); ok {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestPartyPostSyncButton(t *testing.T) {
	e, comps := partyPost("ev1", watchParty{Show: "Frieren", Link: "https://discord.com/events/g/ev1", Sync: true})
	var join string
	for _, c := range comps[0].(discordgo.ActionsRow).Components {
		if b := c.(discordgo.Button); b.Label == "Join in Otakase" {
			join = b.URL
		}
	}
	if join != "https://otakase.xyverion.com/open/?party=ev1" {
		t.Errorf("join button: %q", join)
	}
	if !strings.Contains(e.Description, "synced to the host") {
		t.Errorf("post should explain sync: %q", e.Description)
	}
	if len(comps[0].(discordgo.ActionsRow).Components) > 5 {
		t.Error("more than 5 buttons in a row")
	}
	_, comps = partyPost("ev1", watchParty{Show: "Frieren", Link: "x"})
	for _, c := range comps[0].(discordgo.ActionsRow).Components {
		if c.(discordgo.Button).Label == "Join in Otakase" {
			t.Error("join button without sync")
		}
	}
}

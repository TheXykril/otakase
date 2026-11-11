package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func relayServer(t *testing.T, b *bot) *httptest.Server {
	t.Helper()
	relayHub.mu.Lock()
	relayHub.hosts = map[string]map[string]*relayHost{}
	relayHub.mu.Unlock()
	currentBot = b
	mux := http.NewServeMux()
	for p, h := range apiRoutes {
		mux.HandleFunc(p, h)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func relayDial(t *testing.T, srv *httptest.Server, path, token string) *websocket.Conn {
	t.Helper()
	ws, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+path,
		http.Header{"Authorization": {"Bearer " + token}})
	if err != nil {
		code := 0
		if resp != nil {
			code = resp.StatusCode
		}
		t.Fatalf("dial %s: %v (%d)", path, err, code)
	}
	t.Cleanup(func() { ws.Close() })
	return ws
}

func relayNext(t *testing.T, ws *websocket.Conn) map[string]any {
	t.Helper()
	ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, data, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("frame %q: %v", data, err)
	}
	return m
}

func TestRemoteRelayPassesFramesBetweenOwnApps(t *testing.T) {
	b := appTestBot(t)
	srv := relayServer(t, b)
	tvTok, phoneTok, otherTok := testToken(b, "u1"), testToken(b, "u1"), testToken(b, "u2")

	host := relayDial(t, srv, "/v1/remote/host?id=tv-abcdefgh&name=Living%20room", tvTok)
	// The TV shows up for its own member only.
	deadline := time.Now().Add(2 * time.Second)
	for {
		w := apiDo(t, b, http.MethodGet, "/v1/remote/tvs", nil, map[string]string{"Authorization": "Bearer " + phoneTok})
		if strings.Contains(w.Body.String(), "Living room") || time.Now().After(deadline) {
			if !strings.Contains(w.Body.String(), `"id":"tv-abcdefgh"`) {
				t.Fatalf("tvs = %s", w.Body.String())
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if w := apiDo(t, b, http.MethodGet, "/v1/remote/tvs", nil, map[string]string{"Authorization": "Bearer " + otherTok}); strings.Contains(w.Body.String(), "tv-abcdefgh") {
		t.Fatalf("another member sees the TV: %s", w.Body.String())
	}
	if _, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/v1/remote/control?id=tv-abcdefgh",
		http.Header{"Authorization": {"Bearer " + otherTok}}); err == nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("another member reached the TV: %v", err)
	}

	phone := relayDial(t, srv, "/v1/remote/control?id=tv-abcdefgh&name=Pixel", phoneTok)
	join := relayNext(t, host)
	if join["type"] != "join" || join["name"] != "Pixel" {
		t.Fatalf("join = %v", join)
	}
	from := join["from"].(string)

	phone.WriteMessage(websocket.TextMessage, []byte(`{"seq":1,"action":"pause"}`))
	got := relayNext(t, host)
	if got["from"] != from || got["data"].(map[string]any)["action"] != "pause" {
		t.Fatalf("host got %v", got)
	}
	host.WriteMessage(websocket.TextMessage, []byte(`{"to":"`+from+`","data":{"type":"ack","seq":1}}`))
	if ack := relayNext(t, phone); ack["type"] != "ack" {
		t.Fatalf("phone got %v", ack)
	}
	host.WriteMessage(websocket.TextMessage, []byte(`{"to":"","data":{"type":"status","playing":false}}`))
	if st := relayNext(t, phone); st["type"] != "status" {
		t.Fatalf("phone got %v", st)
	}
	// Junk from the phone never reaches the TV.
	phone.WriteMessage(websocket.TextMessage, []byte(`not json`))
	phone.WriteMessage(websocket.TextMessage, []byte(`{"seq":2}`))
	if got := relayNext(t, host); got["data"].(map[string]any)["seq"] != float64(2) {
		t.Fatalf("host got %v", got)
	}

	host.Close()
	if gone := relayNext(t, phone); gone["type"] != "gone" {
		t.Fatalf("phone got %v", gone)
	}
}

func TestRemoteRelayChecks(t *testing.T) {
	b := appTestBot(t)
	srv := relayServer(t, b)
	tok := testToken(b, "u1")
	for _, path := range []string{"/v1/remote/host?id=short", "/v1/remote/host?id=bad%20id%20here"} {
		_, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+path,
			http.Header{"Authorization": {"Bearer " + tok}})
		if err == nil || resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: %v", path, err)
		}
	}
	if w := apiDo(t, b, http.MethodGet, "/v1/remote/tvs", nil, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", w.Code)
	}
	if got := relayName("  "+strings.Repeat("あ", 50), "TV"); len([]rune(got)) != 40 {
		t.Fatalf("name = %q", got)
	}
}

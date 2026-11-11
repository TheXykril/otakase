package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

// The phone remote away from home. A TV or computer running Otakase that is
// linked to Discord keeps one WebSocket open here as a host; a phone linked
// to the same Discord account connects as a controller and the bot passes
// frames between them. That is also how the TV trusts the phone without a
// code: only the member's own linked apps can reach their TVs. The hub is
// memory only; apps reconnect after a restart.
//
//	GET /v1/remote/tvs (auth)                       → {"tvs":[{"id":"..","name":".."}]}
//	GET /v1/remote/host?id=<tv>&name=<name> (auth)  WebSocket, the TV
//	GET /v1/remote/control?id=<tv>&name=<phone> (auth) WebSocket, the phone
//
// Frames are JSON:
//
//	bot → host:        {"type":"join"|"leave","from":"<ctl>","name":".."}
//	controller → bot:  any object; the host gets {"from":"<ctl>","data":<it>}
//	host → bot:        {"to":"<ctl>"|"","data":<object>}; "" goes to every controller
//	bot → controller:  the host's data as sent, or {"type":"gone"} when the host left

const (
	relayMaxHosts       = 8    // TVs per member
	relayMaxControllers = 8    // phones per TV
	relayMaxMessage     = 8192 // bytes per frame
	relayQueue          = 32
	relayRate           = 20 // frames per second per connection
)

var relayHub = &remoteRelay{hosts: map[string]map[string]*relayHost{}}

type relayHost struct {
	id, name string
	out      chan []byte
	ctls     map[string]*relayCtl
}

type relayCtl struct {
	id, name string
	out      chan []byte
}

type remoteRelay struct {
	mu    sync.Mutex
	hosts map[string]map[string]*relayHost // user → tv id → host
}

var relayUpgrader = websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096}

func init() {
	apiHandle("GET /v1/remote/tvs", (*bot).apiRemoteTVs)
	apiHandle("GET /v1/remote/host", (*bot).apiRemoteHost)
	apiHandle("GET /v1/remote/control", (*bot).apiRemoteControl)
}

// relayID is a TV id as the app makes them: url-safe base64, 8–64 long.
func relayID(s string) bool {
	if len(s) < 8 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '=') {
			return false
		}
	}
	return true
}

// relayName is a device name to show, at most 40 characters.
func relayName(s, fallback string) string {
	s = strings.TrimSpace(cleanText(s, false))
	if !utf8.ValidString(s) || s == "" {
		return fallback
	}
	if r := []rune(s); len(r) > 40 {
		s = string(r[:40])
	}
	return s
}

// relayAuth lets apps that can't set headers pass the token in the query.
func relayAuth(b *bot, w http.ResponseWriter, r *http.Request) string {
	if r.Header.Get("Authorization") == "" {
		if tok := r.URL.Query().Get("token"); tok != "" {
			r.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	return b.requireApp(w, r)
}

func (b *bot) apiRemoteTVs(w http.ResponseWriter, r *http.Request) {
	user := relayAuth(b, w, r)
	if user == "" {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tvs": relayHub.list(user)})
}

type relayTV struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (h *remoteRelay) list(user string) []relayTV {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []relayTV{}
	for _, t := range h.hosts[user] {
		out = append(out, relayTV{ID: t.id, Name: t.name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (b *bot) apiRemoteHost(w http.ResponseWriter, r *http.Request) {
	user := relayAuth(b, w, r)
	if user == "" {
		return
	}
	id := r.URL.Query().Get("id")
	if !relayID(id) {
		apiError(w, http.StatusBadRequest, "bad TV id")
		return
	}
	if relayHub.hostsFull(user, id) {
		apiError(w, http.StatusTooManyRequests, "too many TVs on this account")
		return
	}
	ws, err := relayUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(relayMaxMessage)
	host := &relayHost{id: id, name: relayName(r.URL.Query().Get("name"), "Otakase TV"),
		out: make(chan []byte, relayQueue), ctls: map[string]*relayCtl{}}
	relayHub.addHost(user, host)
	go partyWriter(ws, host.out)
	relayRead(ws, func(data []byte) { relayHub.fromHost(user, host, data) })
	relayHub.dropHost(user, host)
	ws.Close()
}

func (b *bot) apiRemoteControl(w http.ResponseWriter, r *http.Request) {
	user := relayAuth(b, w, r)
	if user == "" {
		return
	}
	id := r.URL.Query().Get("id")
	ctl := &relayCtl{id: newToken(6), name: relayName(r.URL.Query().Get("name"), "Phone"), out: make(chan []byte, relayQueue)}
	switch relayHub.canJoin(user, id) {
	case http.StatusNotFound:
		apiError(w, http.StatusNotFound, "that TV is not online")
		return
	case http.StatusTooManyRequests:
		apiError(w, http.StatusTooManyRequests, "too many phones on that TV")
		return
	}
	ws, err := relayUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(relayMaxMessage)
	if !relayHub.join(user, id, ctl) {
		ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "that TV is not online"),
			time.Now().Add(partySyncWriteWait))
		ws.Close()
		return
	}
	go partyWriter(ws, ctl.out)
	relayRead(ws, func(data []byte) { relayHub.fromCtl(user, id, ctl, data) })
	relayHub.leave(user, id, ctl)
	ws.Close()
}

// relayRead hands each text frame to fn, at most relayRate a second.
func relayRead(ws *websocket.Conn, fn func([]byte)) {
	ws.SetReadDeadline(time.Now().Add(partySyncReadWait))
	ws.SetPongHandler(func(string) error { return ws.SetReadDeadline(time.Now().Add(partySyncReadWait)) })
	window, n := time.Now(), 0
	for {
		kind, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		ws.SetReadDeadline(time.Now().Add(partySyncReadWait))
		if kind != websocket.TextMessage {
			continue
		}
		if now := time.Now(); now.Sub(window) >= time.Second {
			window, n = now, 0
		}
		if n++; n > relayRate {
			continue
		}
		fn(data)
	}
}

// send queues a frame, dropping it when the receiver is far behind.
func relaySend(out chan []byte, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	select {
	case out <- data:
	default:
	}
}

func (h *remoteRelay) hostsFull(user, id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	tvs := h.hosts[user]
	_, again := tvs[id]
	return !again && len(tvs) >= relayMaxHosts
}

// addHost registers a TV; one already there with the same id (a reconnect)
// is closed and its phones move over.
func (h *remoteRelay) addHost(user string, host *relayHost) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.hosts[user] == nil {
		h.hosts[user] = map[string]*relayHost{}
	}
	if old := h.hosts[user][host.id]; old != nil {
		host.ctls = old.ctls
		old.ctls = map[string]*relayCtl{}
		close(old.out)
		for _, c := range host.ctls {
			relaySend(host.out, map[string]string{"type": "join", "from": c.id, "name": c.name})
		}
	}
	h.hosts[user][host.id] = host
}

func (h *remoteRelay) dropHost(user string, host *relayHost) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.hosts[user][host.id] != host {
		return // replaced by a reconnect, which closed it
	}
	delete(h.hosts[user], host.id)
	if len(h.hosts[user]) == 0 {
		delete(h.hosts, user)
	}
	for _, c := range host.ctls {
		relaySend(c.out, map[string]string{"type": "gone"})
	}
	close(host.out)
}

func (h *remoteRelay) canJoin(user, id string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	host := h.hosts[user][id]
	switch {
	case host == nil:
		return http.StatusNotFound
	case len(host.ctls) >= relayMaxControllers:
		return http.StatusTooManyRequests
	}
	return http.StatusOK
}

func (h *remoteRelay) join(user, id string, c *relayCtl) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	host := h.hosts[user][id]
	if host == nil || len(host.ctls) >= relayMaxControllers {
		return false
	}
	host.ctls[c.id] = c
	relaySend(host.out, map[string]string{"type": "join", "from": c.id, "name": c.name})
	return true
}

func (h *remoteRelay) leave(user, id string, c *relayCtl) {
	h.mu.Lock()
	defer h.mu.Unlock()
	host := h.hosts[user][id]
	if host == nil || host.ctls[c.id] != c {
		close(c.out)
		return
	}
	delete(host.ctls, c.id)
	close(c.out)
	relaySend(host.out, map[string]string{"type": "leave", "from": c.id})
}

// fromCtl passes a phone's frame to its TV.
func (h *remoteRelay) fromCtl(user, id string, c *relayCtl, data []byte) {
	if !json.Valid(data) || len(data) == 0 || data[0] != '{' {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	host := h.hosts[user][id]
	if host == nil || host.ctls[c.id] != c {
		return
	}
	relaySend(host.out, map[string]any{"from": c.id, "data": json.RawMessage(data)})
}

// fromHost passes a TV's frame to one phone or all of them.
func (h *remoteRelay) fromHost(user string, host *relayHost, data []byte) {
	var f struct {
		To   string          `json:"to"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(data, &f) != nil || len(f.Data) == 0 || f.Data[0] != '{' {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.hosts[user][host.id] != host {
		return
	}
	for _, c := range host.ctls {
		if f.To == "" || f.To == c.id {
			select {
			case c.out <- []byte(f.Data):
			default:
			}
		}
	}
}

package main

import (
	"encoding/json"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Synced watch parties: with /watchparty sync:True, everyone watches in
// their own Otakase and follows the host's player. Each linked app opens a
// WebSocket to GET /v1/party/{id}/ws; the host's app (the party creator's)
// sends its player state and the bot relays it to everyone else. The hub
// is memory only: a restart drops the connections and the apps reconnect.
// Watch with me parties (partylive.go) use the same hub with an id of their
// own and the member who started one as its host.
//
// Frames are JSON, one message each:
//
//	bot → app:  {"type":"welcome","host":true,"show":"Frieren","anilist_id":154587,"server_at":<ms>}
//	host → bot: {"type":"state","anilist_id":154587,"episode":12,"playing":true,"position":312.4,"sent_at":<ms>}
//	            {"type":"end"} (Watch with me parties only: ends it for everyone)
//	bot → members: the host's "state", {"type":"members","list":[{"name":"..","ready":true,"host":false}]},
//	               {"type":"ended"}
//	member → bot: {"type":"hello","version":".."}, {"type":"ready","episode":12}
//	any → bot:   {"type":"ping","t":<app ms>} → {"type":"pong","t":<same>,"server_at":<ms>}
//	             {"type":"vote","what":"skip"|"next"} (partyfx.go)
//	bot → all:   {"type":"votes","what":"skip","votes":2,"needed":3}, {"type":"voted","what":"skip"},
//	             {"type":"react","emoji":"🔥","name":".."}, {"type":"chat","name":"..","text":".."}
//
// sent_at is in the bot's clock: apps estimate their offset from it with
// ping/pong, so members can tell how old a state is however wrong the
// host's or their own clock is.

const (
	partySyncMaxMembers = 25   // apps following one host
	partySyncMaxMessage = 4096 // bytes per frame
	partySyncRate       = 10   // messages per second per connection
	partySyncPing       = 30 * time.Second
	partySyncReadWait   = 75 * time.Second // a ping plus slack
	partySyncWriteWait  = 10 * time.Second
	partySyncQueue      = 32 // frames waiting for a slow app before it's dropped
)

// partyJoinURL opens the party in Otakase, from the post's link button.
func partyJoinURL(eventID string) string {
	return "https://otakase.xyverion.com/open/?party=" + url.QueryEscape(eventID)
}

func init() {
	apiHandle("GET /v1/party/{id}/ws", partySyncWS)
}

// syncHub is the running bot's hub.
var syncHub = newPartyHub()

type partyHub struct {
	mu    sync.Mutex
	rooms map[string]*partyRoom
	now   func() time.Time
}

type partyRoom struct {
	host  string
	conns map[*partyConn]bool
	last  []byte // the host's latest state, for members who join later
	// episode is the host's, so members are ready only on the same one.
	episode int
	// votes: by what, the members who want it and when (partyfx.go).
	votes map[string]map[string]time.Time
	// relayed: chat and reactions from Discord in the second since relayAt.
	relayAt time.Time
	relayed int
}

type partyConn struct {
	user, name, version string
	host, ready         bool
	episode             int
	out                 chan []byte
	closed              bool
	// rate limiting: messages in the current second
	second time.Time
	count  int
}

func newPartyHub() *partyHub {
	return &partyHub{rooms: map[string]*partyRoom{}, now: time.Now}
}

// partyState is the host's player state.
type partyState struct {
	Type      string  `json:"type"`
	AniListID int     `json:"anilist_id"`
	Episode   int     `json:"episode"`
	Playing   bool    `json:"playing"`
	Position  float64 `json:"position"`
	SentAt    int64   `json:"sent_at"`
}

// partyIn is any message from an app; "ready" uses the state's episode.
type partyIn struct {
	partyState
	Version string `json:"version"`
	T       int64  `json:"t"`
	What    string `json:"what"`
}

type partyMember struct {
	Name  string `json:"name"`
	Ready bool   `json:"ready"`
	Host  bool   `json:"host,omitempty"`
}

var partyUpgrader = websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096}

// partySyncWS is GET /v1/party/{id}/ws.
func partySyncWS(b *bot, w http.ResponseWriter, r *http.Request) {
	// Clients that can't set headers pass the token in the query.
	if r.Header.Get("Authorization") == "" {
		if tok := r.URL.Query().Get("token"); tok != "" {
			r.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	user := b.requireApp(w, r)
	if user == "" {
		return
	}
	id := r.PathValue("id")
	var p watchParty
	var ok bool
	b.store.view(func(d *storeData) { p, ok = d.Parties[id] })
	if !ok {
		p, ok = liveSyncParty(id)
	}
	if !ok || !p.Sync {
		apiError(w, http.StatusNotFound, "no synced watch party with that id")
		return
	}
	isHost := user == p.Host
	if !isHost && syncHub.full(id, user) {
		apiError(w, http.StatusForbidden, "this watch party is full")
		return
	}
	ws, err := partyUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return // the upgrader answered
	}
	ws.SetReadLimit(partySyncMaxMessage)
	c := &partyConn{user: user, name: b.dashName(user), host: isHost, out: make(chan []byte, partySyncQueue)}
	if !syncHub.join(id, p, c) {
		ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "this watch party is full"),
			time.Now().Add(partySyncWriteWait))
		ws.Close()
		return
	}
	go partyWriter(ws, c.out)
	partyReader(ws, id, c)
	syncHub.leave(id, c)
	ws.Close()
}

// partyWriter sends c's frames and keeps the connection alive. It returns
// once the hub closes the queue.
func partyWriter(ws *websocket.Conn, out chan []byte) {
	ping := time.NewTicker(partySyncPing)
	defer ping.Stop()
	defer ws.Close()
	for {
		select {
		case m, ok := <-out:
			ws.SetWriteDeadline(time.Now().Add(partySyncWriteWait))
			if !ok {
				ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}
			if err := ws.WriteMessage(websocket.TextMessage, m); err != nil {
				return
			}
		case <-ping.C:
			if err := ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(partySyncWriteWait)); err != nil {
				return
			}
		}
	}
}

func partyReader(ws *websocket.Conn, id string, c *partyConn) {
	ws.SetReadDeadline(time.Now().Add(partySyncReadWait))
	ws.SetPongHandler(func(string) error { return ws.SetReadDeadline(time.Now().Add(partySyncReadWait)) })
	for {
		kind, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		ws.SetReadDeadline(time.Now().Add(partySyncReadWait))
		if kind != websocket.TextMessage {
			continue
		}
		if !syncHub.handle(id, c, data) {
			return
		}
	}
}

// full reports whether a member can't join: the party has its maximum of
// members already, not counting user's own earlier connection.
func (h *partyHub) full(id, user string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.memberCount(h.rooms[id], user) >= partySyncMaxMembers
}

func (h *partyHub) memberCount(r *partyRoom, except string) int {
	if r == nil {
		return 0
	}
	users := map[string]bool{}
	for c := range r.conns {
		if !c.host && c.user != except {
			users[c.user] = true
		}
	}
	return len(users)
}

// join adds c to the party: it gets the welcome and the host's latest
// state, and everyone gets the new member list. An earlier connection of
// the same app user is dropped, so a reconnect replaces it.
func (h *partyHub) join(id string, p watchParty, c *partyConn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rooms[id]
	if r == nil {
		r = &partyRoom{host: p.Host, conns: map[*partyConn]bool{}}
		h.rooms[id] = r
	}
	if !c.host && h.memberCount(r, c.user) >= partySyncMaxMembers {
		return false
	}
	for old := range r.conns {
		if old.user == c.user {
			h.drop(r, old)
		}
	}
	r.conns[c] = true
	h.sendTo(r, c, mustJSON(map[string]any{"type": "welcome", "host": c.host, "show": p.Show,
		"anilist_id": p.AniListID, "server_at": h.now().UnixMilli()}))
	if r.last != nil && !c.host {
		h.sendTo(r, c, r.last)
	}
	h.broadcastMembers(r)
	return true
}

// leave takes c out of the party, once its connection has ended.
func (h *partyHub) leave(id string, c *partyConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rooms[id]
	if r == nil || !r.conns[c] {
		return
	}
	h.drop(r, c)
	if len(r.conns) == 0 {
		delete(h.rooms, id) // the host's last state goes too; they resend within 5 s
		return
	}
	h.broadcastMembers(r)
	h.forgetVotes(r, c.user)
}

// end tells everyone the party is over and disconnects them.
func (h *partyHub) end(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.endLocked(id)
}

func (h *partyHub) endLocked(id string) {
	r := h.rooms[id]
	if r == nil {
		return
	}
	ended := mustJSON(map[string]string{"type": "ended"})
	for c := range r.conns {
		h.sendTo(r, c, ended)
		h.drop(r, c)
	}
	delete(h.rooms, id)
}

// handle answers one message from c. It returns false to disconnect.
func (h *partyHub) handle(id string, c *partyConn, data []byte) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rooms[id]
	if r == nil || !r.conns[c] {
		return false
	}
	now := h.now()
	if now.Sub(c.second) >= time.Second {
		c.second, c.count = now, 0
	}
	c.count++
	if c.count > partySyncRate {
		return true // over the limit: dropped
	}
	var m partyIn
	if json.Unmarshal(data, &m) != nil {
		return true
	}
	switch m.Type {
	case "state":
		if !c.host {
			return true // only the host leads
		}
		s, ok := cleanState(m.partyState, now)
		if !ok {
			return true
		}
		r.last = mustJSON(s)
		for o := range r.conns {
			if o != c {
				h.sendTo(r, o, r.last)
			}
		}
		if s.Episode != r.episode {
			r.episode = s.Episode
			r.votes = nil // they were about the last episode
			h.broadcastMembers(r)
		}
	case "hello":
		c.version = clip(strings.TrimSpace(m.Version), 32)
	case "ready":
		if m.Episode > 0 && m.Episode < 100000 {
			c.ready, c.episode = true, m.Episode
			h.broadcastMembers(r)
		}
	case "ping":
		h.sendTo(r, c, mustJSON(map[string]any{"type": "pong", "t": m.T, "server_at": now.UnixMilli()}))
	case "vote":
		h.vote(r, c, m.What, now)
	case "end":
		if c.host && dropLiveParty(id) {
			h.endLocked(id)
			return false
		}
	}
	return true
}

// cleanState checks the host's state and stamps it with the bot's clock
// when the host's own stamp is missing or far off.
func cleanState(s partyState, now time.Time) (partyState, bool) {
	if s.AniListID <= 0 || s.Episode <= 0 || s.Episode >= 100000 || math.IsNaN(s.Position) || math.IsInf(s.Position, 0) ||
		s.Position < 0 || s.Position > 24*3600 {
		return s, false
	}
	s.Type = "state"
	if d := now.UnixMilli() - s.SentAt; d < -5000 || d > 30000 {
		s.SentAt = now.UnixMilli()
	}
	return s, true
}

// broadcastMembers sends everyone who's in: the host first, then by name.
func (h *partyHub) broadcastMembers(r *partyRoom) {
	byUser := map[string]partyMember{}
	for c := range r.conns {
		m := byUser[c.user]
		m.Name, m.Host = c.name, c.host
		m.Ready = m.Ready || c.host || (c.ready && (r.episode == 0 || c.episode == r.episode))
		byUser[c.user] = m
	}
	list := make([]partyMember, 0, len(byUser))
	for _, m := range byUser {
		list = append(list, m)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Host != list[j].Host {
			return list[i].Host
		}
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
	msg := mustJSON(map[string]any{"type": "members", "list": list})
	for c := range r.conns {
		h.sendTo(r, c, msg)
	}
}

// sendTo queues a frame for c, dropping an app too slow to keep up.
func (h *partyHub) sendTo(r *partyRoom, c *partyConn, msg []byte) {
	if c.closed {
		return
	}
	select {
	case c.out <- msg:
	default:
		log.Printf("watch party sync: dropping a slow app of %s", c.user)
		h.drop(r, c)
	}
}

// drop closes c's queue, which makes its writer close the connection.
func (h *partyHub) drop(r *partyRoom, c *partyConn) {
	delete(r.conns, c)
	if !c.closed {
		c.closed = true
		close(c.out)
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

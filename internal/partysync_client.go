package internal

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// The connection to a synced watch party on the bot: GET
// /v1/party/{id}/ws. It reconnects with backoff until the party ends or
// Close is called, and hands every message to the session on In.

var (
	errPartyNotLinked = errors.New("this app isn't linked to the Otakase Discord: run otakase -discord-link first, then join the party again")
	errPartyNotFound  = errors.New("there's no synced watch party with that id (it may be over, or planned without sync)")
	errPartyFull      = errors.New("this watch party is full")
	errPartyOff       = errors.New("Discord features are off (DiscordBotURL is empty)")
)

const (
	partyPingEvery = 20 * time.Second
	partyReadWait  = 75 * time.Second
	partyWriteWait = 10 * time.Second
)

// partyMessage is any message from the bot.
type partyMessage struct {
	Type string `json:"type"`
	partyHostState
	// welcome
	Host     bool   `json:"host"`
	Show     string `json:"show"`
	ServerAt int64  `json:"server_at"`
	// pong
	T int64 `json:"t"`
	// members
	List []partyMemberInfo `json:"list"`
	// react, chat (from the party's Discord chat), votes and voted
	Name   string `json:"name"`
	Emoji  string `json:"emoji"`
	Text   string `json:"text"`
	What   string `json:"what"`
	Votes  int    `json:"votes"`
	Needed int    `json:"needed"`
	// gone (made here): why the connection stopped for good
	Err error `json:"-"`
}

type partyMemberInfo struct {
	Name  string `json:"name"`
	Ready bool   `json:"ready"`
	Host  bool   `json:"host"`
}

type partyClient struct {
	id      string
	url     string
	token   string
	version string
	clock   partyClock
	In      chan partyMessage
	// Connected gets a value on every reconnect (not the first connect).
	Connected chan struct{}

	mu     sync.Mutex
	ws     *websocket.Conn
	closed bool
	done   chan struct{}
}

// partyWSURL is the WebSocket address of a party on the bot at base.
func partyWSURL(base, id string) (string, error) {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("bad DiscordBotURL %q", base)
	}
	switch u.Scheme {
	case "https", "wss":
		u.Scheme = "wss"
	case "http", "ws":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("bad DiscordBotURL %q", base)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/party/" + url.PathEscape(id) + "/ws"
	return u.String(), nil
}

// dialParty connects to party id and waits for the bot's welcome, which
// says whether this app hosts it. Errors are final: not linked, no such
// party, full, or the bot can't be reached.
func dialParty(base, token, id, version string) (*partyClient, partyMessage, error) {
	if strings.TrimSpace(base) == "" {
		return nil, partyMessage{}, errPartyOff
	}
	u, err := partyWSURL(base, id)
	if err != nil {
		return nil, partyMessage{}, err
	}
	c := &partyClient{id: id, url: u, token: token, version: version,
		In: make(chan partyMessage, 64), Connected: make(chan struct{}, 1), done: make(chan struct{})}
	ws, welcome, err := c.connect()
	if err != nil {
		return nil, partyMessage{}, err
	}
	go c.run(ws)
	return c, welcome, nil
}

func (c *partyClient) connect() (*websocket.Conn, partyMessage, error) {
	dialer := websocket.Dialer{Proxy: http.ProxyFromEnvironment, HandshakeTimeout: 15 * time.Second}
	h := http.Header{}
	h.Set("Authorization", "Bearer "+c.token)
	h.Set("User-Agent", "otakase/"+c.version)
	ws, resp, err := dialer.Dial(c.url, h)
	if err != nil {
		if resp != nil {
			switch resp.StatusCode {
			case http.StatusUnauthorized:
				return nil, partyMessage{}, errPartyNotLinked
			case http.StatusNotFound:
				return nil, partyMessage{}, errPartyNotFound
			case http.StatusForbidden:
				return nil, partyMessage{}, errPartyFull
			}
			return nil, partyMessage{}, fmt.Errorf("the bot answered %s", resp.Status)
		}
		return nil, partyMessage{}, fmt.Errorf("couldn't reach the bot: %w", err)
	}
	ws.SetReadLimit(1 << 16)
	ws.SetReadDeadline(time.Now().Add(15 * time.Second))
	var welcome partyMessage
	if err := ws.ReadJSON(&welcome); err != nil || welcome.Type != "welcome" {
		ws.Close()
		if err == nil {
			err = fmt.Errorf("unexpected %q", welcome.Type)
		}
		return nil, partyMessage{}, fmt.Errorf("no welcome from the bot: %w", err)
	}
	c.mu.Lock()
	c.ws = ws
	c.mu.Unlock()
	c.Send(map[string]string{"type": "hello", "version": c.version})
	c.ping()
	return ws, welcome, nil
}

// run reads until the party ends or Close, reconnecting in between.
func (c *partyClient) run(ws *websocket.Conn) {
	defer close(c.In)
	for {
		ended := c.read(ws)
		if ended || c.isClosed() {
			return
		}
		for attempt := 0; ; attempt++ {
			select {
			case <-c.done:
				return
			case <-time.After(partyBackoff(attempt)):
			}
			Log(fmt.Sprintf("watch party: reconnecting (attempt %d)", attempt+1))
			next, _, err := c.connect()
			if err == nil {
				ws = next
				select {
				case c.Connected <- struct{}{}:
				default:
				}
				break
			}
			Log("watch party: " + err.Error())
			if errors.Is(err, errPartyNotLinked) || errors.Is(err, errPartyNotFound) || errors.Is(err, errPartyFull) {
				c.deliver(partyMessage{Type: "gone", Err: err})
				return
			}
		}
	}
}

// read delivers messages from ws until it fails; true when the party ended.
func (c *partyClient) read(ws *websocket.Conn) bool {
	stopPing := make(chan struct{})
	defer close(stopPing)
	go func() {
		t := time.NewTicker(partyPingEvery)
		defer t.Stop()
		for {
			select {
			case <-stopPing:
				return
			case <-t.C:
				c.ping()
			}
		}
	}()
	for {
		ws.SetReadDeadline(time.Now().Add(partyReadWait))
		var m partyMessage
		if err := ws.ReadJSON(&m); err != nil {
			if !c.isClosed() {
				Log("watch party: connection lost: " + err.Error())
			}
			ws.Close()
			return false
		}
		switch m.Type {
		case "pong":
			c.clock.sample(m.T, time.Now().UnixMilli(), m.ServerAt)
		case "ended":
			c.deliver(m)
			ws.Close()
			return true
		default:
			c.deliver(m)
		}
	}
}

func (c *partyClient) deliver(m partyMessage) {
	select {
	case c.In <- m:
	case <-c.done:
	}
}

func (c *partyClient) ping() {
	c.Send(map[string]any{"type": "ping", "t": time.Now().UnixMilli()})
}

// Send writes one message; while disconnected it is dropped.
func (c *partyClient) Send(v any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ws == nil || c.closed {
		return
	}
	c.ws.SetWriteDeadline(time.Now().Add(partyWriteWait))
	if err := c.ws.WriteJSON(v); err != nil {
		Log("watch party: send: " + err.Error())
	}
}

// ServerNow is now in the bot's clock (unix ms).
func (c *partyClient) ServerNow() int64 {
	return c.clock.serverNow(time.Now().UnixMilli())
}

func (c *partyClient) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// Close leaves the party.
func (c *partyClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.done)
	if c.ws != nil {
		c.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(time.Second))
		c.ws.Close()
	}
}

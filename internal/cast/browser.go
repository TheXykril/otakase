package cast

import (
	"crypto/rand"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// KindBrowser is any device with a web browser: a phone, a smart TV, a games
// console. Nothing is discovered for it. otakase serves a player page on the
// LAN and the viewer opens its link there.
const KindBrowser Kind = "browser"

// BrowserDeviceName is the browser entry's name, which is also what CastDevice
// is set to to pick it without a menu.
const BrowserDeviceName = "Browser"

// BrowserDeviceUUID is the browser entry's key in the device picker.
const BrowserDeviceUUID = "browser"

// DefaultBrowserPort is the port the player page is served on when the config
// names none. Fixed rather than random so the link stays the same from one
// episode and one run to the next: a TV can keep the page open, or bookmark it,
// and a firewall needs one rule for it.
const DefaultBrowserPort = 8012

// browserGone is how long the page may go without reporting before the cast
// counts it as closed. Long enough for a TV browser reloading the page or a
// phone waking its screen; short of castStallTimeout, so a closed tab ends the
// cast as lost rather than as a stall.
var browserGone = 30 * time.Second

// browserGonePaused is the same while paused. A phone that locks its screen
// pauses the video and stops running the page, and is not gone for that.
var browserGonePaused = 15 * time.Minute

// browserPrimaryWindow is how recent a page's report must be for its position
// to be the one the cast follows.
const browserPrimaryWindow = 5 * time.Second

//go:embed browserpage
var browserPage embed.FS

// BrowserSettings is how the player page is served.
type BrowserSettings struct {
	// Port is the port to serve on; 0 means DefaultBrowserPort.
	Port int
	// Token is the secret part of the link, so a neighbour on the same
	// network does not stumble onto what is playing.
	Token string
}

var (
	browserMu       sync.Mutex
	browserSettings BrowserSettings
	browserRunning  *browserHub
	// browserCommand receives what a viewer pressed on the page: one of
	// "toggle", "back", "forward", "volup", "voldown", "stop".
	browserCommand func(string)
)

// SetBrowser sets how the player page is served. It takes effect the first time
// the page is needed; a page already being served keeps its link.
func SetBrowser(settings BrowserSettings) {
	browserMu.Lock()
	defer browserMu.Unlock()
	browserSettings = settings
}

// SetBrowserCommands sets what receives the presses made on the page.
func SetBrowserCommands(handle func(command string)) {
	browserMu.Lock()
	defer browserMu.Unlock()
	browserCommand = handle
}

// NewBrowserToken makes a link secret: short enough to type with a TV remote,
// and drawn from letters and digits that cannot be misread for one another.
func NewBrowserToken() string {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	token := make([]byte, 5)
	for i := range token {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "watch"
		}
		token[i] = alphabet[n.Int64()]
	}
	return string(token)
}

// BrowserDevice is the browser entry for the device picker. It does not start
// serving anything: the device picker runs in processes that never cast, and
// the page is only served once the entry is connected to.
func BrowserDevice() Device {
	browserMu.Lock()
	port := browserSettings.Port
	browserMu.Unlock()
	if port == 0 {
		port = DefaultBrowserPort
	}
	addr, _ := outboundIP()
	return Device{Name: BrowserDeviceName, UUID: BrowserDeviceUUID, Addr: addr, Port: port, Kind: KindBrowser}
}

// BrowserLink is the address of the player page, starting it if needed.
func BrowserLink() (string, error) {
	hub, err := browserHubRunning()
	if err != nil {
		return "", err
	}
	return hub.link(), nil
}

// BrowserViewers is how many pages are open on the link now.
func BrowserViewers() int {
	browserMu.Lock()
	hub := browserRunning
	browserMu.Unlock()
	if hub == nil {
		return 0
	}
	return hub.viewers()
}

// browserHubRunning returns the page server, starting it the first time. It is
// kept for the life of the process, so the page stays connected between
// episodes.
func browserHubRunning() (*browserHub, error) {
	browserMu.Lock()
	defer browserMu.Unlock()
	if browserRunning != nil {
		return browserRunning, nil
	}
	settings := browserSettings
	if settings.Port == 0 {
		settings.Port = DefaultBrowserPort
	}
	if settings.Token == "" {
		settings.Token = NewBrowserToken()
	}
	hub, err := startBrowserHub(settings)
	if err != nil {
		return nil, err
	}
	browserRunning = hub
	return hub, nil
}

// browserReport is what a page says about itself, once a second.
type browserReport struct {
	Client    int     `json:"client"`
	Load      int     `json:"load"`
	Seek      int     `json:"seek"`
	Position  float64 `json:"pos"`
	Paused    bool    `json:"paused"`
	Ended     bool    `json:"ended"`
	Volume    float64 `json:"vol"`
	Error     string  `json:"error"`
	PlayerUse string  `json:"player"`
}

type browserClient struct {
	id     int
	send   chan []byte
	report browserReport
	seen   time.Time
}

// browserHub serves the player page and talks to every copy of it that is
// open.
type browserHub struct {
	token    string
	baseURL  string
	listener net.Listener

	mu      sync.Mutex
	clients map[int]*browserClient
	nextID  int
	// current is the last order every page should be following: what to play
	// and from where. A page that connects late is sent it at once.
	current map[string]any
	// load and seek number the orders, so a report about a stream the page
	// has since been told to leave is recognised as such.
	load, seek int
	// seekBase is where in the stream the last seek within it landed: the
	// position is reported from there, as from the start of a new load.
	seekBase  float64
	paused    bool
	volume    float64
	contactAt time.Time
	lastError string
}

func startBrowserHub(settings BrowserSettings) (*browserHub, error) {
	addr, err := outboundIP()
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(addr.String(), strconv.Itoa(settings.Port)))
	if err != nil {
		// Another otakase already serving the page holds the port. A link that
		// changes is better than no cast at all.
		Log(fmt.Sprintf("cast: port %d for the browser page is taken (%v), using a free one", settings.Port, err))
		listener, err = net.Listen("tcp", net.JoinHostPort(addr.String(), "0"))
		if err != nil {
			return nil, fmt.Errorf("cast: could not serve the browser page: %w", err)
		}
	}
	hub := &browserHub{
		token:    settings.Token,
		baseURL:  fmt.Sprintf("http://%s:%d", addr, listener.Addr().(*net.TCPAddr).Port),
		listener: listener,
		clients:  map[int]*browserClient{},
		volume:   1,
	}
	server := &http.Server{Handler: hub, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			Log(fmt.Sprintf("cast: the browser page server stopped: %v", err))
		}
	}()
	Log("cast: serving the browser player at " + hub.link())
	return hub, nil
}

func (h *browserHub) link() string {
	return h.baseURL + "/" + h.token
}

func (h *browserHub) viewers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// ServeHTTP answers only under the token: anything else is a 404, with no
// hint that there is a page at all.
func (h *browserHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rest, ok := strings.CutPrefix(r.URL.Path, "/"+h.token)
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch rest {
	case "":
		// The short form typed on a remote; the page's own requests are
		// relative to the directory, so it is served with the slash.
		http.Redirect(w, r, "/"+h.token+"/", http.StatusFound)
	case "/":
		h.serveFile(w, "browserpage/index.html", "text/html; charset=utf-8")
	case "/hls.js":
		h.serveFile(w, "browserpage/hls.light.min.js", "text/javascript; charset=utf-8")
	case "/events":
		h.serveEvents(w, r)
	case "/state":
		h.serveReport(w, r)
	case "/command":
		h.serveCommand(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *browserHub) serveFile(w http.ResponseWriter, name, contentType string) {
	body, err := browserPage.ReadFile(name)
	if err != nil {
		http.Error(w, "missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(body)
}

// serveEvents is the page's line from otakase: server-sent events, which even
// old TV browsers have, and which reconnect by themselves when otakase moves
// on to the next episode or is started again.
func (h *browserHub) serveEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	h.mu.Lock()
	h.nextID++
	client := &browserClient{id: h.nextID, send: make(chan []byte, 16), seen: time.Now()}
	h.clients[client.id] = client
	h.contactAt = time.Now()
	hello := map[string]any{"cmd": "hello", "client": client.id}
	current := h.current
	h.mu.Unlock()
	Log(fmt.Sprintf("cast: browser %s opened the player (viewer %d)", r.RemoteAddr, client.id))

	defer func() {
		h.mu.Lock()
		delete(h.clients, client.id)
		h.mu.Unlock()
		Log(fmt.Sprintf("cast: browser viewer %d left", client.id))
	}()

	write := func(message []byte) bool {
		if _, err := fmt.Fprintf(w, "data: %s\n\n", message); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	// A reconnect after two seconds, not the browser's default of three or
	// more: the gap is a viewer staring at a stalled page.
	fmt.Fprint(w, "retry: 2000\n\n")
	first, _ := json.Marshal(hello)
	if !write(first) {
		return
	}
	if current != nil {
		message, _ := json.Marshal(current)
		if !write(message) {
			return
		}
	}

	// A comment every so often keeps proxies and power-saving TVs from
	// closing a quiet connection, and finds a dead one.
	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case message := <-client.send:
			if !write(message) {
				return
			}
		case <-keepAlive.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (h *browserHub) serveReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var report browserReport
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&report); err != nil {
		http.Error(w, "bad report", http.StatusBadRequest)
		return
	}
	h.mu.Lock()
	if client, ok := h.clients[report.Client]; ok {
		client.report = report
		client.seen = time.Now()
		h.contactAt = client.seen
	}
	if report.Error != "" && report.Error != h.lastError && report.Load == h.load {
		h.lastError = report.Error
		Log(fmt.Sprintf("cast: browser viewer %d (%s) says: %s", report.Client, report.PlayerUse, report.Error))
	}
	h.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (h *browserHub) serveCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Command string `json:"cmd"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body); err != nil {
		http.Error(w, "bad command", http.StatusBadRequest)
		return
	}
	switch body.Command {
	case "toggle", "back", "forward", "volup", "voldown", "stop":
	default:
		http.Error(w, "unknown command", http.StatusBadRequest)
		return
	}
	browserMu.Lock()
	handle := browserCommand
	browserMu.Unlock()
	Log("cast: browser pressed " + body.Command)
	if handle != nil {
		handle(body.Command)
	}
	w.WriteHeader(http.StatusNoContent)
}

// broadcast sends a message to every open page. Called with mu held.
func (h *browserHub) broadcast(message map[string]any) {
	body, _ := json.Marshal(message)
	for _, client := range h.clients {
		select {
		case client.send <- body:
		default:
			// A page that has stopped reading is not worth blocking on; it
			// catches up from current when it reconnects.
		}
	}
}

// primary is the page whose position the cast follows: the longest connected
// of those reporting now. Each page plays on its own; following one keeps the
// panel and the tracking steady. Called with mu held.
func (h *browserHub) primary() *browserClient {
	var best *browserClient
	for _, client := range h.clients {
		if time.Since(client.seen) > browserPrimaryWindow {
			continue
		}
		if best == nil || client.id < best.id {
			best = client
		}
	}
	return best
}

// BrowserMedia is what the page is told about what it plays, besides the
// stream itself.
type BrowserMedia struct {
	Title string
	// Subtitles is a WebVTT file in episode time; empty for none.
	Subtitles string
	// SubtitleSize is the size of the subtitles as a percentage of normal.
	SubtitleSize int
	// Duration is the episode's length in seconds, 0 when not known.
	Duration float64
}

// BrowserPlayer is the open player page, as a cast device.
type BrowserPlayer struct {
	hub   *browserHub
	media BrowserMedia
}

func connectBrowser(Device) (*BrowserPlayer, error) {
	hub, err := browserHubRunning()
	if err != nil {
		return nil, err
	}
	return &BrowserPlayer{hub: hub}, nil
}

// Link is the address of the page.
func (p *BrowserPlayer) Link() string { return p.hub.link() }

// Describe sets what the page shows beside the next stream it is given.
func (p *BrowserPlayer) Describe(media BrowserMedia) { p.media = media }

// Play loads a stream that starts at the beginning of the episode.
func (p *BrowserPlayer) Play(url string) error { return p.PlayFrom(url, 0) }

// PlayFrom loads a stream that starts start seconds into the episode, which the
// page needs to line the subtitles up with it.
func (p *BrowserPlayer) PlayFrom(url string, start float64) error {
	h := p.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load++
	h.seek = 0
	h.seekBase = 0
	h.paused = false
	h.lastError = ""
	size := p.media.SubtitleSize
	if size <= 0 {
		size = 100
	}
	h.current = map[string]any{
		"cmd":      "load",
		"load":     h.load,
		"url":      url,
		"ts":       strings.TrimSuffix(url, PlaylistName) + ProgressiveName,
		"start":    start,
		"title":    p.media.Title,
		"duration": p.media.Duration,
		"subs":     p.media.Subtitles,
		"subSize":  size,
		"volume":   h.volume,
	}
	h.broadcast(h.current)
	return nil
}

// SeekWithin moves the page to offset seconds into the stream it already has.
// Unlike a receiver, a browser seeks within what is written itself, so this
// needs no new stream. The position is then reported from there, the way a
// device counts a stream it was just handed.
func (p *BrowserPlayer) SeekWithin(offset float64) error {
	h := p.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.current == nil {
		return errors.New("cast: nothing is loaded in the browser")
	}
	h.seek++
	h.seekBase = offset
	h.paused = false
	h.current["seek"] = h.seek
	h.current["at"] = offset
	h.broadcast(map[string]any{"cmd": "seek", "load": h.load, "seek": h.seek, "at": offset})
	return nil
}

// Progress is the primary page's position in the stream.
func (p *BrowserPlayer) Progress() (Progress, error) {
	h := p.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	gone := browserGone
	if h.paused {
		gone = browserGonePaused
	}
	if time.Since(h.contactAt) > gone {
		return Progress{}, fmt.Errorf("cast: no browser has had the player open for %s", gone)
	}
	client := h.primary()
	if client == nil || client.report.Load != h.load || client.report.Seek != h.seek {
		// Nothing from the stream it was last told to play yet.
		return Progress{Settling: true}, nil
	}
	return Progress{
		Position: client.report.Position - h.seekBase,
		Idle:     client.report.Ended,
	}, nil
}

// SeekToTime seeks within the stream as loaded, in the stream's own time.
func (p *BrowserPlayer) SeekToTime(seconds float64) error {
	return p.SeekWithin(seconds)
}

func (p *BrowserPlayer) Pause() error   { return p.setPaused(true) }
func (p *BrowserPlayer) Unpause() error { return p.setPaused(false) }

func (p *BrowserPlayer) setPaused(paused bool) error {
	h := p.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	h.paused = paused
	if h.current != nil {
		h.current["paused"] = paused
	}
	command := "play"
	if paused {
		command = "pause"
	}
	h.broadcast(map[string]any{"cmd": command})
	return nil
}

// SetVolume sets the volume of every page.
func (p *BrowserPlayer) SetVolume(level float64) error {
	level = min(max(level, 0), 1)
	h := p.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	h.volume = level
	if h.current != nil {
		h.current["volume"] = level
	}
	h.broadcast(map[string]any{"cmd": "volume", "volume": level})
	return nil
}

// Volume is the primary page's volume.
func (p *BrowserPlayer) Volume() float64 {
	h := p.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	if client := h.primary(); client != nil && client.report.Load > 0 {
		return client.report.Volume
	}
	return h.volume
}

// Stop clears the page, which goes back to waiting for the next stream.
func (p *BrowserPlayer) Stop() error {
	h := p.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	h.current = nil
	h.broadcast(map[string]any{"cmd": "stop"})
	return nil
}

package cast

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
)

// Kodi casting: Kodi itself, and everything that runs it -- an Android TV box
// or Fire TV with Kodi installed, a LibreELEC or OSMC Raspberry Pi. Found by
// the zeroconf service Kodi announces for its web interface, or by address
// from KodiHost; driven by JSON-RPC over HTTP.
//
// Kodi is handed the same plain transport stream a DLNA renderer gets. It plays
// HLS too, but it treats a playlist that is still growing as live TV and joins
// it near the newest segment, which would start an episode minutes in. Seeking
// stays with the caller, which rebuilds the stream at the target, as for every
// other device.
//
// Kodi only answers when Settings > Services > Control > "Allow remote control
// via HTTP" is on.

// KindKodi is a Kodi media center.
const KindKodi Kind = "kodi"

// kodiService is the zeroconf service Kodi announces its HTTP JSON-RPC under.
const kodiService = "_xbmc-jsonrpc-h._tcp"

// kodiDefaultPort is Kodi's web server port when nothing else is set.
const kodiDefaultPort = 8080

// KodiSettings is how to reach Kodi instances discovery cannot find, and the
// login Kodi's web server asks for when one is set.
type KodiSettings struct {
	// Hosts are host or host:port entries to offer whether or not they
	// announce themselves.
	Hosts    []string
	User     string
	Password string
}

var (
	kodiMu       sync.Mutex
	kodiSettings KodiSettings
)

// SetKodi sets how Kodi instances are reached. Call it before Discover.
func SetKodi(settings KodiSettings) {
	kodiMu.Lock()
	kodiSettings = settings
	kodiMu.Unlock()
}

func currentKodiSettings() KodiSettings {
	kodiMu.Lock()
	defer kodiMu.Unlock()
	return kodiSettings
}

// kodiHTTP is the client for JSON-RPC calls. Kodi on the LAN answers in
// milliseconds; Player.Open can take a moment while it opens the stream.
var kodiHTTP = &http.Client{Timeout: 10 * time.Second}

// kodiBrowse lists the Kodi instances announcing themselves. A variable so
// tests can stand in for multicast.
var kodiBrowse = func(ctx context.Context) ([]Device, error) {
	resolver, err := zeroconf.NewResolver(zeroconf.SelectIPTraffic(zeroconf.IPv4))
	if err != nil {
		return nil, fmt.Errorf("cast: Kodi discovery: %w", err)
	}
	entries := make(chan *zeroconf.ServiceEntry, 8)
	if err := resolver.Browse(ctx, kodiService, "local.", entries); err != nil {
		return nil, fmt.Errorf("cast: Kodi discovery: %w", err)
	}

	var devices []Device
	for {
		select {
		case <-ctx.Done():
			return devices, nil
		case entry, ok := <-entries:
			if !ok {
				return devices, nil
			}
			if entry == nil || len(entry.AddrIPv4) == 0 {
				continue
			}
			devices = append(devices, kodiDevice(entry.Instance, entry.AddrIPv4[0], entry.Port))
		}
	}
}

func kodiDevice(name string, addr net.IP, port int) Device {
	if port <= 0 {
		port = kodiDefaultPort
	}
	if strings.TrimSpace(name) == "" {
		name = addr.String()
	}
	return Device{
		Name:     name,
		UUID:     "kodi-" + net.JoinHostPort(addr.String(), strconv.Itoa(port)),
		Addr:     addr,
		Port:     port,
		Kind:     KindKodi,
		Location: "http://" + net.JoinHostPort(addr.String(), strconv.Itoa(port)) + "/jsonrpc",
	}
}

// discoverKodi lists the Kodi instances that announce themselves and the ones
// named in the settings that answer.
func discoverKodi(ctx context.Context) ([]Device, error) {
	settings := currentKodiSettings()

	var (
		mu      sync.Mutex
		devices []Device
		errs    []error
		wg      sync.WaitGroup
	)
	add := func(found ...Device) {
		mu.Lock()
		devices = append(devices, found...)
		mu.Unlock()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		found, err := kodiBrowse(ctx)
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
		}
		add(found...)
	}()

	for _, host := range settings.Hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			device, err := probeKodiHost(ctx, host, settings)
			if err != nil {
				Log(fmt.Sprintf("cast: Kodi at %s did not answer: %v", host, err))
				return
			}
			add(device)
		}(host)
	}
	wg.Wait()

	devices = dedupeKodi(devices)
	if len(devices) == 0 && len(errs) > 0 {
		return nil, errs[0]
	}
	return devices, nil
}

// dedupeKodi drops the second sighting of an instance: one named in the
// settings usually announces itself as well.
func dedupeKodi(devices []Device) []Device {
	seen := map[string]bool{}
	out := devices[:0]
	for _, d := range devices {
		if seen[d.Location] {
			continue
		}
		seen[d.Location] = true
		out = append(out, d)
	}
	return out
}

// probeKodiHost resolves a configured host and asks it for its name, which
// also proves it answers.
func probeKodiHost(ctx context.Context, host string, settings KodiSettings) (Device, error) {
	name, portText, err := net.SplitHostPort(host)
	if err != nil {
		name, portText = host, strconv.Itoa(kodiDefaultPort)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return Device{}, fmt.Errorf("bad port in %q", host)
	}
	addrs, err := net.DefaultResolver.LookupIP(ctx, "ip4", name)
	if err != nil || len(addrs) == 0 {
		return Device{}, fmt.Errorf("cannot resolve %q", name)
	}

	device := kodiDevice(name, addrs[0], port)
	client := kodiClient{endpoint: device.Location, user: settings.User, password: settings.Password}
	var labels map[string]string
	if err := client.call(ctx, "XBMC.GetInfoLabels", map[string]any{"labels": []string{"System.FriendlyName"}}, &labels); err != nil {
		return Device{}, err
	}
	if friendly := strings.TrimSpace(labels["System.FriendlyName"]); friendly != "" {
		device.Name = friendly
	}
	return device, nil
}

// kodiClient speaks JSON-RPC to one Kodi.
type kodiClient struct {
	endpoint       string
	user, password string
}

type kodiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *kodiError) Error() string {
	return fmt.Sprintf("Kodi error %d: %s", e.Code, e.Message)
}

func (c kodiClient) call(ctx context.Context, method string, params any, result any) error {
	payload := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		payload["params"] = params
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.user != "" || c.password != "" {
		req.SetBasicAuth(c.user, c.password)
	}

	resp, err := kodiHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("cast: Kodi wants a login; set KodiUser and KodiPassword to the ones in its web server settings")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cast: Kodi answered %s to %s", resp.Status, method)
	}

	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *kodiError      `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("cast: Kodi sent an unreadable answer to %s: %w", method, err)
	}
	if envelope.Error != nil {
		return envelope.Error
	}
	if result != nil && len(envelope.Result) > 0 {
		return json.Unmarshal(envelope.Result, result)
	}
	return nil
}

// kodiPlayer is a Kodi playing one stream.
type kodiPlayer struct {
	name   string
	client kodiClient

	mu      sync.Mutex
	playing bool
	volume  float64
}

func connectKodi(d Device) (*kodiPlayer, error) {
	settings := currentKodiSettings()
	p := &kodiPlayer{
		name:   d.Name,
		client: kodiClient{endpoint: d.Location, user: settings.User, password: settings.Password},
		volume: 1,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var pong string
	if err := p.client.call(ctx, "JSONRPC.Ping", nil, &pong); err != nil {
		return nil, fmt.Errorf("cast: cannot reach Kodi on %s: %w", d.Name, err)
	}
	p.refreshVolume()
	return p, nil
}

func (p *kodiPlayer) call(method string, params any, result any) error {
	ctx, cancel := context.WithTimeout(context.Background(), kodiHTTP.Timeout)
	defer cancel()
	return p.client.call(ctx, method, params, result)
}

// videoPlayer returns the id of Kodi's active video player, or -1 when nothing
// is playing.
func (p *kodiPlayer) videoPlayer() (int, error) {
	var players []struct {
		PlayerID int    `json:"playerid"`
		Type     string `json:"type"`
	}
	if err := p.call("Player.GetActivePlayers", nil, &players); err != nil {
		return -1, err
	}
	for _, player := range players {
		if player.Type == "video" {
			return player.PlayerID, nil
		}
	}
	return -1, nil
}

func (p *kodiPlayer) Play(streamURL string) error {
	streamURL = progressiveURL(streamURL)
	p.mu.Lock()
	p.playing = false
	p.mu.Unlock()
	return p.call("Player.Open", map[string]any{"item": map[string]any{"file": streamURL}}, nil)
}

type kodiTime struct {
	Hours        int `json:"hours"`
	Minutes      int `json:"minutes"`
	Seconds      int `json:"seconds"`
	Milliseconds int `json:"milliseconds"`
}

func (t kodiTime) seconds() float64 {
	return float64(t.Hours*3600+t.Minutes*60+t.Seconds) + float64(t.Milliseconds)/1000
}

func kodiTimeOf(seconds float64) kodiTime {
	if seconds < 0 {
		seconds = 0
	}
	ms := int(seconds*1000 + 0.5)
	return kodiTime{Hours: ms / 3600000, Minutes: ms / 60000 % 60, Seconds: ms / 1000 % 60, Milliseconds: ms % 1000}
}

func (p *kodiPlayer) Progress() (Progress, error) {
	id, err := p.videoPlayer()
	if err != nil {
		return Progress{}, err
	}
	if id < 0 {
		// Nothing playing: before the stream starts that is Kodi still
		// opening it; after, the episode ended or was stopped on the TV.
		p.mu.Lock()
		defer p.mu.Unlock()
		return Progress{Idle: p.playing}, nil
	}

	var props struct {
		Time      kodiTime `json:"time"`
		TotalTime kodiTime `json:"totaltime"`
	}
	if err := p.call("Player.GetProperties", map[string]any{"playerid": id, "properties": []string{"time", "totaltime"}}, &props); err != nil {
		return Progress{}, err
	}
	p.mu.Lock()
	p.playing = true
	p.mu.Unlock()
	return Progress{Position: props.Time.seconds(), Duration: props.TotalTime.seconds()}, nil
}

func (p *kodiPlayer) SeekToTime(seconds float64) error {
	id, err := p.videoPlayer()
	if err != nil {
		return err
	}
	if id < 0 {
		return fmt.Errorf("cast: nothing is playing on %s", p.name)
	}
	// Kodi 19 and later take the time wrapped in an object; Kodi 18 takes it
	// bare and rejects the wrapper as invalid params.
	err = p.call("Player.Seek", map[string]any{"playerid": id, "value": map[string]any{"time": kodiTimeOf(seconds)}}, nil)
	if kodiErr, ok := err.(*kodiError); ok && kodiErr.Code == -32602 {
		err = p.call("Player.Seek", map[string]any{"playerid": id, "value": kodiTimeOf(seconds)}, nil)
	}
	return err
}

func (p *kodiPlayer) playPause(play bool) error {
	id, err := p.videoPlayer()
	if err != nil {
		return err
	}
	if id < 0 {
		return fmt.Errorf("cast: nothing is playing on %s", p.name)
	}
	return p.call("Player.PlayPause", map[string]any{"playerid": id, "play": play}, nil)
}

func (p *kodiPlayer) Pause() error   { return p.playPause(false) }
func (p *kodiPlayer) Unpause() error { return p.playPause(true) }

func (p *kodiPlayer) refreshVolume() {
	var props struct {
		Volume int `json:"volume"`
	}
	if err := p.call("Application.GetProperties", map[string]any{"properties": []string{"volume"}}, &props); err != nil {
		return
	}
	p.mu.Lock()
	p.volume = clampVolume(float64(props.Volume) / 100)
	p.mu.Unlock()
}

func (p *kodiPlayer) SetVolume(level float64) error {
	level = clampVolume(level)
	if err := p.call("Application.SetVolume", map[string]any{"volume": int(level*100 + 0.5)}, nil); err != nil {
		return err
	}
	p.mu.Lock()
	p.volume = level
	p.mu.Unlock()
	return nil
}

func (p *kodiPlayer) Volume() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.volume
}

func (p *kodiPlayer) Stop() error {
	id, err := p.videoPlayer()
	if err != nil || id < 0 {
		return err
	}
	return p.call("Player.Stop", map[string]any{"playerid": id}, nil)
}

package cast

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// DLNA casting: any TV that shows up as a UPnP "media renderer" -- LG, Samsung,
// Sony, Hisense, Philips, most smart TVs of the last decade. Found by SSDP,
// driven by the AVTransport and RenderingControl SOAP services.
//
// The TV is handed a plain MPEG-TS stream (ProgressiveName) rather than the HLS
// playlist a Chromecast gets: renderers disagree about HLS, while every one of
// them plays a transport stream. Seeking stays with the caller, which rebuilds
// the stream at the target as it does for a Chromecast, so a renderer that
// cannot seek a live stream -- most cannot -- still skips openings.

// KindDLNA is a UPnP/DLNA media renderer.
const KindDLNA Kind = "dlna"

const (
	ssdpAddr           = "239.255.255.250:1900"
	mediaRendererType  = "urn:schemas-upnp-org:device:MediaRenderer:1"
	avTransportType    = "urn:schemas-upnp-org:service:AVTransport:1"
	renderingControlTy = "urn:schemas-upnp-org:service:RenderingControl:1"

	// dlnaProtocolInfo describes the stream as an MPEG-TS the renderer may not
	// seek in by bytes or time (OP=00): it is being written as it plays.
	dlnaProtocolInfo = "http-get:*:video/mpeg:DLNA.ORG_PN=MPEG_TS_HD_NA_ISO;DLNA.ORG_OP=00;DLNA.ORG_CI=0;DLNA.ORG_FLAGS=01700000000000000000000000000000"
)

// dlnaHTTP is the client for descriptions and SOAP calls. A renderer on the LAN
// answers in milliseconds or not at all.
var dlnaHTTP = &http.Client{Timeout: 5 * time.Second}

// discoverDLNA lists the media renderers that answer an SSDP search.
func discoverDLNA(ctx context.Context) ([]Device, error) {
	locations, err := ssdpSearch(ctx, mediaRendererType)
	if err != nil {
		return nil, err
	}

	var (
		mu      sync.Mutex
		devices []Device
		wg      sync.WaitGroup
	)
	for _, location := range locations {
		wg.Add(1)
		go func(location string) {
			defer wg.Done()
			desc, err := fetchRendererDescription(ctx, location)
			if err != nil {
				Log(fmt.Sprintf("cast: skipping the renderer at %s: %v", location, err))
				return
			}
			mu.Lock()
			devices = append(devices, desc.device())
			mu.Unlock()
		}(location)
	}
	wg.Wait()
	return devices, nil
}

// ssdpSearch multicasts one search and collects the description URLs that
// answer before the context ends.
func ssdpSearch(ctx context.Context, target string) ([]string, error) {
	conn, err := listenSSDP(currentDiscoveryPort())
	if err != nil {
		return nil, fmt.Errorf("SSDP: %w", err)
	}
	defer conn.Close()

	group, err := net.ResolveUDPAddr("udp4", ssdpAddr)
	if err != nil {
		return nil, err
	}
	search := "M-SEARCH * HTTP/1.1\r\n" +
		"HOST: " + ssdpAddr + "\r\n" +
		"MAN: \"ssdp:discover\"\r\n" +
		"MX: 2\r\n" +
		"ST: " + target + "\r\n\r\n"
	// Sent twice: it is UDP, and a renderer waking its network stack misses
	// the first one often enough to matter.
	for i := 0; i < 2; i++ {
		if _, err := conn.WriteTo([]byte(search), group); err != nil {
			return nil, fmt.Errorf("SSDP: %w", err)
		}
	}

	// Answers are read until shortly before the context ends, leaving time to
	// fetch each renderer's description inside the same deadline.
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(DefaultDiscoveryTimeout)
	}
	listenUntil := deadline.Add(-time.Second)
	if time.Until(listenUntil) < 500*time.Millisecond {
		listenUntil = time.Now().Add(500 * time.Millisecond)
	}
	_ = conn.SetReadDeadline(listenUntil)

	seen := map[string]bool{}
	var locations []string
	buf := make([]byte, 4096)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			break // the deadline: every answer that was coming has come
		}
		location := ssdpHeader(string(buf[:n]), "LOCATION")
		if location == "" || seen[location] {
			continue
		}
		seen[location] = true
		locations = append(locations, location)
	}
	return locations, nil
}

// listenSSDP opens the socket a search is sent from and answered on.
//
// Renderers answer an M-SEARCH by unicast to the port it came from, so that
// port is the one a host firewall has to let in. A random one cannot be
// allowed without opening every UDP port to the LAN; a fixed one is a single
// rule. When the fixed port is taken -- a second otakase searching at the same
// moment, or something else holding it -- a random one is used instead, so
// discovery still works wherever no firewall is in the way.
func listenSSDP(port int) (net.PacketConn, error) {
	if port > 0 {
		conn, err := net.ListenPacket("udp4", ":"+strconv.Itoa(port))
		if err == nil {
			return conn, nil
		}
		Log(fmt.Sprintf("cast: could not listen on discovery port %d, using a random one: %v", port, err))
	}
	return net.ListenPacket("udp4", ":0")
}

// ssdpHeader reads one header from an SSDP response, case-insensitively.
func ssdpHeader(response, name string) string {
	for _, line := range strings.Split(response, "\r\n") {
		key, value, found := strings.Cut(line, ":")
		if found && strings.EqualFold(strings.TrimSpace(key), name) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// rendererDescription is what a renderer says about itself.
type rendererDescription struct {
	location     string
	name         string
	udn          string
	avTransport  string // control URL, absolute
	renderingCtl string // control URL, absolute; empty when there is none
}

func (d rendererDescription) device() Device {
	device := Device{Name: d.name, UUID: d.udn, Kind: KindDLNA, Location: d.location}
	if u, err := url.Parse(d.location); err == nil {
		device.Addr = net.ParseIP(u.Hostname())
		device.Port, _ = strconv.Atoi(u.Port())
	}
	if device.UUID == "" {
		device.UUID = d.location
	}
	if device.Name == "" {
		device.Name = device.Addr.String()
	}
	return device
}

// upnpDevice is the part of a device description worth reading. Renderers
// nest the device that has the services under a root device often enough that
// the whole tree is searched.
type upnpDevice struct {
	DeviceType   string        `xml:"deviceType"`
	FriendlyName string        `xml:"friendlyName"`
	UDN          string        `xml:"UDN"`
	Services     []upnpService `xml:"serviceList>service"`
	Devices      []upnpDevice  `xml:"deviceList>device"`
}

type upnpService struct {
	ServiceType string `xml:"serviceType"`
	ControlURL  string `xml:"controlURL"`
}

func fetchRendererDescription(ctx context.Context, location string) (rendererDescription, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return rendererDescription{}, err
	}
	resp, err := dlnaHTTP.Do(req)
	if err != nil {
		return rendererDescription{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return rendererDescription{}, fmt.Errorf("description answered %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return rendererDescription{}, err
	}
	return parseRendererDescription(location, body)
}

func parseRendererDescription(location string, body []byte) (rendererDescription, error) {
	var root struct {
		URLBase string     `xml:"URLBase"`
		Device  upnpDevice `xml:"device"`
	}
	if err := xml.Unmarshal(body, &root); err != nil {
		return rendererDescription{}, fmt.Errorf("unreadable description: %w", err)
	}
	base := strings.TrimSpace(root.URLBase)
	if base == "" {
		base = location
	}

	desc := rendererDescription{location: location, name: strings.TrimSpace(root.Device.FriendlyName), udn: strings.TrimSpace(root.Device.UDN)}
	var walk func(d upnpDevice)
	walk = func(d upnpDevice) {
		for _, s := range d.Services {
			control := resolveURL(base, strings.TrimSpace(s.ControlURL))
			switch {
			case strings.HasPrefix(s.ServiceType, "urn:schemas-upnp-org:service:AVTransport:") && desc.avTransport == "":
				desc.avTransport = control
			case strings.HasPrefix(s.ServiceType, "urn:schemas-upnp-org:service:RenderingControl:") && desc.renderingCtl == "":
				desc.renderingCtl = control
			}
		}
		for _, child := range d.Devices {
			walk(child)
		}
	}
	walk(root.Device)
	if desc.avTransport == "" {
		return rendererDescription{}, fmt.Errorf("no AVTransport service")
	}
	return desc, nil
}

func resolveURL(base, ref string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

// soapCall invokes one action and returns the response's leaf elements by
// name. A UPnP fault comes back as an error carrying its code, which is what
// tells "cannot do that now" (701) from "never can" (other codes).
func soapCall(controlURL, serviceType, action string, args [][2]string) (map[string]string, error) {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)
	body.WriteString(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	body.WriteString(`<u:` + action + ` xmlns:u="` + serviceType + `">`)
	for _, arg := range args {
		body.WriteString("<" + arg[0] + ">" + html.EscapeString(arg[1]) + "</" + arg[0] + ">")
	}
	body.WriteString(`</u:` + action + `></s:Body></s:Envelope>`)

	req, err := http.NewRequest(http.MethodPost, controlURL, strings.NewReader(body.String()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", `"`+serviceType+"#"+action+`"`)
	resp, err := dlnaHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cast: %s: %w", action, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("cast: %s: %w", action, err)
	}
	fields := soapFields(raw)
	if resp.StatusCode != http.StatusOK {
		return fields, &soapRefusal{action: action, status: resp.StatusCode, statusText: resp.Status, code: fields["errorCode"], description: fields["errorDescription"]}
	}
	return fields, nil
}

// soapRefusal is a renderer answering an action with an error, kept apart so
// a refusal that only means "not allowed yet" can be told from a real one.
type soapRefusal struct {
	action      string
	status      int
	statusText  string
	code        string
	description string
}

func (e *soapRefusal) Error() string {
	if e.code != "" {
		return fmt.Sprintf("cast: %s refused: UPnP error %s %s", e.action, e.code, e.description)
	}
	return fmt.Sprintf("cast: %s refused: %s", e.action, e.statusText)
}

// dlnaAwaitingApproval reports whether err looks like a TV that has not been
// allowed to take orders from this machine yet.
//
// Most smart TVs (LG webOS, Hisense, Samsung) put up an "allow this device?"
// prompt the first time an unknown controller talks to them. While it is up
// they either hold the request until it times out or refuse it as
// unauthorized; once the viewer accepts, the same request goes through.
func dlnaAwaitingApproval(err error) bool {
	if err == nil {
		return false
	}
	var refusal *soapRefusal
	if errors.As(err, &refusal) {
		switch refusal.status {
		case http.StatusUnauthorized, http.StatusForbidden:
			return true
		}
		// 606 is UPnP's "action not authorized".
		return refusal.code == "606"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return errors.Is(err, syscall.ECONNRESET)
}

// DLNAApprovalWait is how long a cast waits for the viewer to accept this
// machine on the TV before giving up. A var so tests need not wait it out.
var DLNAApprovalWait = 2 * time.Minute

// dlnaApprovalRetry is the pause between tries while waiting.
var dlnaApprovalRetry = 2 * time.Second

var (
	approvalMu     sync.Mutex
	approvalNotice func(device string)
)

// SetApprovalNotice sets what to call, once per cast, when a TV looks like it
// is asking the viewer to allow this machine. nil turns it off.
func SetApprovalNotice(notice func(device string)) {
	approvalMu.Lock()
	approvalNotice = notice
	approvalMu.Unlock()
}

func noticeApproval(device string) {
	approvalMu.Lock()
	notice := approvalNotice
	approvalMu.Unlock()
	if notice != nil {
		notice(device)
	}
}

// soapFields collects every element holding only text, by local name.
func soapFields(raw []byte) map[string]string {
	fields := map[string]string{}
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var name string
	var text strings.Builder
	for {
		token, err := decoder.Token()
		if err != nil {
			return fields
		}
		switch t := token.(type) {
		case xml.StartElement:
			name = t.Name.Local
			text.Reset()
		case xml.CharData:
			text.Write(t)
		case xml.EndElement:
			if name == t.Name.Local {
				fields[name] = strings.TrimSpace(text.String())
			}
			name = ""
		}
	}
}

// dlnaPlayer drives one renderer.
type dlnaPlayer struct {
	name         string
	avTransport  string
	renderingCtl string

	mu sync.Mutex
	// playing is set once the renderer has reported playing the stream it
	// was last given. Until then a STOPPED is the renderer still switching
	// over -- which it reports between two streams -- not the end.
	playing bool
	volume  float64

	// Some TVs (seen on a Hisense) report PLAYING but never move RelTime on
	// a stream with no known length, which read as a stall and stopped a cast
	// that was playing fine. The clock counts the time spent PLAYING instead,
	// and stands in for RelTime until the device shows its own moving.
	clock         float64
	clockAt       time.Time
	firstReported float64
	reportedMoves bool
	reportedSeen  bool
	usingClock    bool
}

// dlnaClockAfter is how long a renderer may report PLAYING with a position
// that never moves before the clock is trusted over it.
const dlnaClockAfter = 10 * time.Second

// dlnaNow is the clock the position estimate reads; tests replace it.
var dlnaNow = time.Now

// resetClock forgets the estimate, for a new stream.
func (p *dlnaPlayer) resetClock() {
	p.clock, p.clockAt = 0, time.Time{}
	p.firstReported, p.reportedMoves, p.reportedSeen, p.usingClock = 0, false, false, false
}

// position is the reported position, or the clock's estimate when the
// renderer has been PLAYING for a while without ever moving its own. Called
// with mu held.
func (p *dlnaPlayer) position(state string, reported float64) float64 {
	now := dlnaNow()
	if !p.clockAt.IsZero() {
		p.clock += now.Sub(p.clockAt).Seconds()
	}
	if state == "PLAYING" {
		p.clockAt = now
	} else {
		p.clockAt = time.Time{}
	}

	if !p.reportedSeen {
		p.firstReported, p.reportedSeen = reported, true
	} else if reported != p.firstReported {
		p.reportedMoves = true
	}
	if p.reportedMoves {
		return reported
	}
	if p.clock >= dlnaClockAfter.Seconds() {
		if !p.usingClock {
			p.usingClock = true
			Log(fmt.Sprintf("cast: %s reports no playback position; counting it from the clock", p.name))
		}
		return p.firstReported + p.clock
	}
	return reported
}

func connectDLNA(d Device) (*dlnaPlayer, error) {
	if d.Location == "" {
		return nil, fmt.Errorf("cast: %s has no description address", d.Name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	desc, err := fetchRendererDescription(ctx, d.Location)
	if err != nil {
		return nil, fmt.Errorf("cast: could not connect to %s: %w", d.Name, err)
	}
	p := &dlnaPlayer{name: d.Name, avTransport: desc.avTransport, renderingCtl: desc.renderingCtl}
	p.refreshVolume()
	return p, nil
}

// progressiveURL turns the playlist URL the caller serves into the plain
// transport stream the same server offers beside it.
func progressiveURL(u string) string {
	if strings.HasSuffix(u, "/"+PlaylistName) {
		return strings.TrimSuffix(u, PlaylistName) + ProgressiveName
	}
	return u
}

func (p *dlnaPlayer) av(action string, args ...[2]string) (map[string]string, error) {
	return soapCall(p.avTransport, avTransportType, action, append([][2]string{{"InstanceID", "0"}}, args...))
}

func (p *dlnaPlayer) Play(streamURL string) error {
	streamURL = progressiveURL(streamURL)
	p.mu.Lock()
	p.playing = false
	p.resetClock()
	p.mu.Unlock()

	metadata := `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">` +
		`<item id="0" parentID="-1" restricted="1"><dc:title>otakase</dc:title><upnp:class>object.item.videoItem</upnp:class>` +
		`<res protocolInfo="` + dlnaProtocolInfo + `">` + html.EscapeString(streamURL) + `</res></item></DIDL-Lite>`

	set := func() error {
		_, err := p.av("SetAVTransportURI", [2]string{"CurrentURI", streamURL}, [2]string{"CurrentURIMetaData", metadata})
		return err
	}
	if err := p.awaitApproval(set); err != nil {
		// Many renderers refuse a new URI while one is playing (error 701,
		// "transition not available"); stopping first is what they expect.
		_, _ = p.av("Stop")
		if err := set(); err != nil {
			return err
		}
	}
	if _, err := p.av("Play", [2]string{"Speed", "1"}); err != nil {
		return err
	}
	return nil
}

// awaitApproval runs the first order a TV gets, and keeps retrying it while
// the TV looks like it is asking the viewer to allow this machine, telling
// the viewer once. Any other error, or the wait running out, is returned.
func (p *dlnaPlayer) awaitApproval(first func() error) error {
	err := first()
	if !dlnaAwaitingApproval(err) {
		return err
	}
	noticeApproval(p.name)
	Log(fmt.Sprintf("cast: %s is not answering yet, waiting for it to be allowed: %v", p.name, err))
	deadline := time.Now().Add(DLNAApprovalWait)
	for time.Now().Before(deadline) {
		time.Sleep(dlnaApprovalRetry)
		if err = first(); !dlnaAwaitingApproval(err) {
			return err
		}
	}
	return fmt.Errorf("cast: %s was not allowed to play from this machine in time -- accept it on the TV and cast again: %w", p.name, err)
}

func (p *dlnaPlayer) Progress() (Progress, error) {
	info, err := p.av("GetTransportInfo")
	if err != nil {
		return Progress{}, err
	}
	state := info["CurrentTransportState"]

	position, err := p.av("GetPositionInfo")
	if err != nil {
		return Progress{}, err
	}
	progress := Progress{
		Position: parseUPnPTime(position["RelTime"]),
		Duration: parseUPnPTime(position["TrackDuration"]),
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	progress.Position = p.position(state, progress.Position)
	switch state {
	case "PLAYING", "PAUSED_PLAYBACK":
		p.playing = true
	case "STOPPED", "NO_MEDIA_PRESENT":
		progress.Idle = p.playing
	}
	return progress, nil
}

// parseUPnPTime reads H+:MM:SS[.F+] into seconds. "NOT_IMPLEMENTED" and other
// non-times, which renderers send for a stream of unknown length, read as 0.
func parseUPnPTime(value string) float64 {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 3 {
		return 0
	}
	total := 0.0
	for _, part := range parts {
		n, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return 0
		}
		total = total*60 + n
	}
	return total
}

func formatUPnPTime(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	s := int(seconds + 0.5)
	return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
}

func (p *dlnaPlayer) SeekToTime(seconds float64) error {
	_, err := p.av("Seek", [2]string{"Unit", "REL_TIME"}, [2]string{"Target", formatUPnPTime(seconds)})
	return err
}

func (p *dlnaPlayer) Pause() error {
	_, err := p.av("Pause")
	return err
}

func (p *dlnaPlayer) Unpause() error {
	_, err := p.av("Play", [2]string{"Speed", "1"})
	return err
}

func (p *dlnaPlayer) refreshVolume() {
	if p.renderingCtl == "" {
		return
	}
	fields, err := soapCall(p.renderingCtl, renderingControlTy, "GetVolume", [][2]string{{"InstanceID", "0"}, {"Channel", "Master"}})
	if err != nil {
		return
	}
	if level, err := strconv.Atoi(fields["CurrentVolume"]); err == nil {
		p.mu.Lock()
		p.volume = clampVolume(float64(level) / 100)
		p.mu.Unlock()
	}
}

func (p *dlnaPlayer) SetVolume(level float64) error {
	if p.renderingCtl == "" {
		return fmt.Errorf("cast: %s does not take volume commands", p.name)
	}
	level = clampVolume(level)
	_, err := soapCall(p.renderingCtl, renderingControlTy, "SetVolume",
		[][2]string{{"InstanceID", "0"}, {"Channel", "Master"}, {"DesiredVolume", strconv.Itoa(int(level*100 + 0.5))}})
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.volume = level
	p.mu.Unlock()
	return nil
}

func (p *dlnaPlayer) Volume() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.volume
}

func (p *dlnaPlayer) Stop() error {
	_, err := p.av("Stop")
	return err
}

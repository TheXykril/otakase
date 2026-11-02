package mobilecore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/thexykril/otakase/internal/cast"
)

// castRequest is the "cast" object of a cast_* operation.
type castRequest struct {
	// Timeout is how long cast_devices listens, in seconds.
	Timeout float64 `json:"timeout"`
	// Device is the id cast_devices gave the device to play on.
	Device  string            `json:"device"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	HLS     bool              `json:"hls"`
	// Subtitle is the address of an external subtitle file, fetched with
	// Headers. Empty for none.
	Subtitle string  `json:"subtitle"`
	Start    float64 `json:"start"`
	// Duration is the episode's length when the app knows it.
	Duration float64 `json:"duration"`
	Title    string  `json:"title"`
	// Action and Value drive cast_control: pause, play, seek (Value is the
	// position in seconds), volume (Value is 0..1) or stop.
	Action string  `json:"action"`
	Value  float64 `json:"value"`
}

type castDevice struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Address string `json:"address"`
}

// The ways a cast stream reaches the device.
const (
	// castRelay passes the provider's own playlist or file through.
	castRelay = "relay"
	// castProgressive joins the playlist's segments into one transport
	// stream, for a DLNA renderer.
	castProgressive = "progressive"
	// castFFmpeg rebuilds the stream with ffmpeg, burning the subtitles in.
	castFFmpeg = "ffmpeg"
)

// castStatusGone is how long a device may fail to answer before the cast
// counts as lost.
const castStatusGone = 20 * time.Second

// caster owns the devices found and the one cast running.
type caster struct {
	mu      sync.Mutex
	devices map[string]cast.Device
	session *castSession
}

var castHooksOnce sync.Once

// castLog keeps the last lines the cast package logged, for cast_log.
var castLog struct {
	sync.Mutex
	lines []string
}

func castLogLine(line string) {
	castLog.Lock()
	defer castLog.Unlock()
	castLog.lines = append(castLog.lines, time.Now().Format("15:04:05 ")+line)
	if len(castLog.lines) > 200 {
		castLog.lines = castLog.lines[len(castLog.lines)-200:]
	}
}

// castSession is one device playing, episode after episode.
type castSession struct {
	core   *Core
	device cast.Device
	ctx    context.Context
	cancel context.CancelFunc

	// op serialises talking to the device and rebuilding the stream.
	op sync.Mutex

	mu       sync.Mutex
	player   cast.MediaPlayer
	relay    *cast.Relay
	server   *cast.Server
	remux    *cast.Remux
	dir      string
	gen      int
	load     int
	req      castRequest
	mode     string
	offset   float64
	duration float64
	pending  float64
	state    string
	paused   bool
	seen     bool
	position float64
	volume   float64
	note     string
	err      string
	link     string
	lastOK   time.Time
	subsVTT  string
}

func (c *Core) castOperation(ctx context.Context, req request) string {
	castHooksOnce.Do(func() {
		cast.Log = castLogLine
		cast.SetApprovalNotice(func(device string) {
			c.caster.note("Allow this device on " + device + " to start the cast.")
		})
		cast.SetBrowserCommands(c.caster.browserCommand)
	})
	in := castRequest{}
	if req.Cast != nil {
		in = *req.Cast
	}
	var data any
	var err error
	switch req.Operation {
	case "cast_devices":
		data, err = c.caster.discover(ctx, in)
	case "cast_start":
		data, err = c.caster.start(c, in)
	case "cast_status":
		data = c.caster.status()
	case "cast_control":
		data, err = c.caster.control(in)
	case "cast_log":
		castLog.Lock()
		data = map[string]any{"lines": append([]string{}, castLog.lines...)}
		castLog.Unlock()
	default:
		return failure("invalid_request", "Unknown operation.", false)
	}
	if err != nil {
		var refusal castRefusal
		if errors.As(err, &refusal) {
			return failure("invalid_request", refusal.Error(), false)
		}
		return failure("cast_error", castMessage(err), true)
	}
	return encode(response{Version: 1, Data: data})
}

type castRefusal string

func (r castRefusal) Error() string { return string(r) }

// castMessage is an error a viewer can read: the cast package's own wording,
// without the package prefix.
func castMessage(err error) string {
	text := strings.TrimPrefix(err.Error(), "cast: ")
	if text == "" {
		return "Casting failed."
	}
	return strings.ToUpper(text[:1]) + text[1:]
}

func castDeviceID(d cast.Device) string {
	if d.UUID != "" {
		return string(d.Kind) + ":" + d.UUID
	}
	return fmt.Sprintf("%s:%s:%d", d.Kind, d.Addr, d.Port)
}

func (k *caster) discover(ctx context.Context, in castRequest) (any, error) {
	timeout := time.Duration(in.Timeout * float64(time.Second))
	if timeout <= 0 || timeout > 15*time.Second {
		timeout = cast.DefaultDiscoveryTimeout
	}
	found, err := cast.Discover(ctx, timeout)
	if err != nil {
		cast.Log("cast: discovery failed: " + err.Error())
	}
	found = append(found, cast.BrowserDevice())
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.devices == nil {
		k.devices = map[string]cast.Device{}
	}
	out := make([]castDevice, 0, len(found))
	for _, d := range found {
		id := castDeviceID(d)
		k.devices[id] = d
		kind := d.Kind
		if kind == "" {
			kind = cast.KindChromecast
		}
		address := ""
		if d.Addr != nil && kind != cast.KindBrowser {
			address = fmt.Sprintf("%s:%d", d.Addr, d.Port)
		}
		out = append(out, castDevice{ID: id, Name: d.Name, Kind: kind.Label(), Address: address})
	}
	return map[string]any{"devices": out}, nil
}

func (k *caster) note(text string) {
	k.mu.Lock()
	s := k.session
	k.mu.Unlock()
	if s != nil {
		s.mu.Lock()
		s.note = text
		s.mu.Unlock()
	}
}

// start casts a stream. On the device already casting it loads the next
// episode; on another device it ends the old cast first. It returns at once:
// cast_status follows the load.
func (k *caster) start(c *Core, in castRequest) (any, error) {
	if in.URL == "" || len(in.URL) > 8<<10 {
		return nil, castRefusal("A stream address is required.")
	}
	if !strings.HasPrefix(in.URL, "http://") && !strings.HasPrefix(in.URL, "https://") {
		return nil, castRefusal("Only web streams can be cast.")
	}
	if in.Subtitle != "" && !strings.HasPrefix(in.Subtitle, "http://") && !strings.HasPrefix(in.Subtitle, "https://") {
		return nil, castRefusal("Subtitles must be a web address.")
	}
	k.mu.Lock()
	device, ok := k.devices[in.Device]
	old := k.session
	if !ok {
		k.mu.Unlock()
		return nil, castRefusal("Unknown device. Search for devices again.")
	}
	var s *castSession
	if old != nil && castDeviceID(old.device) == in.Device {
		s = old
	} else {
		ctx, cancel := context.WithCancel(context.Background())
		s = &castSession{core: c, device: device, ctx: ctx, cancel: cancel, volume: -1}
		k.session = s
	}
	k.mu.Unlock()
	if old != nil && old != s {
		old.close()
	}
	s.mu.Lock()
	s.load++
	load := s.load
	s.req = in
	s.state, s.err, s.note = "connecting", "", ""
	s.seen, s.paused, s.position = false, false, in.Start
	s.duration = in.Duration
	s.lastOK = time.Now()
	s.mu.Unlock()
	go s.begin(c, load)
	return map[string]any{"device": device.Name}, nil
}

func (s *castSession) stale(load int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load != load || s.ctx.Err() != nil
}

func (s *castSession) fail(load int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.load != load {
		return
	}
	cast.Log("cast: " + err.Error())
	s.state, s.err = "error", castMessage(err)
}

// begin connects if needed and starts the episode.
func (s *castSession) begin(c *Core, load int) {
	s.op.Lock()
	defer s.op.Unlock()
	if s.stale(load) {
		return
	}
	s.mu.Lock()
	player := s.player
	req := s.req
	s.mu.Unlock()
	if player == nil {
		p, err := cast.Connect(s.device)
		if err != nil {
			s.fail(load, err)
			return
		}
		mp, ok := p.(cast.MediaPlayer)
		if !ok {
			_ = p.Stop()
			s.fail(load, errors.New("this device cannot be cast to from the app"))
			return
		}
		player = mp
		s.mu.Lock()
		s.player = mp
		if bp, ok := p.(*cast.BrowserPlayer); ok {
			s.link = bp.Link()
		}
		s.mu.Unlock()
	}
	s.setState(load, "loading")
	if err := s.play(c, load, req, req.Start); err != nil {
		if s.stale(load) {
			return
		}
		s.fail(load, err)
	}
}

func (s *castSession) setState(load int, state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.load == load {
		s.state = state
	}
}

// pickMode chooses how the stream reaches the device.
func pickMode(kind cast.Kind, req castRequest, ffmpeg string) string {
	if kind == cast.KindBrowser || kind == cast.KindKodi {
		// Both take subtitles beside the stream and play playlists.
		return castRelay
	}
	if ffmpeg != "" && (req.Subtitle != "" || kind == cast.KindDLNA && req.HLS || kind == cast.KindChromecast && !req.HLS && cast.VideoTypeFor(req.URL) != "video/mp4") {
		return castFFmpeg
	}
	if kind == cast.KindDLNA && req.HLS {
		return castProgressive
	}
	return castRelay
}

// play starts req on the device at start seconds.
func (s *castSession) play(c *Core, load int, req castRequest, start float64) error {
	kind := s.device.Kind
	if kind == "" {
		kind = cast.KindChromecast
	}
	ffmpeg := findFFmpeg()
	mode := pickMode(kind, req, ffmpeg)
	note := ""
	if req.Subtitle != "" && mode != castFFmpeg && kind != cast.KindBrowser && kind != cast.KindKodi {
		note = "Subtitles need ffmpeg on this computer to cast here, so this plays without them."
	}
	s.stopRemux()
	switch mode {
	case castFFmpeg:
		return s.playFFmpeg(c, load, req, start, ffmpeg)
	default:
		relay, err := s.ensureRelay(c)
		if err != nil {
			return err
		}
		stream := relay.Serve(req.URL, req.Headers)
		media := cast.Media{Title: req.Title, Start: start}
		subtitles := ""
		if req.Subtitle != "" && (kind == cast.KindBrowser || kind == cast.KindKodi) {
			if vtt, err := fetchSubtitlesVTT(s.ctx, c, req); err == nil {
				subtitles = relay.SetSubtitles(vtt)
			} else {
				cast.Log("cast: subtitles failed: " + err.Error())
				note = "The subtitles could not be loaded, so this plays without them."
			}
		}
		media.Subtitles = subtitles
		offset, pending := 0.0, -1.0
		switch {
		case mode == castProgressive:
			from, err := relay.ProgressiveStart(s.ctx, start)
			if err != nil {
				return err
			}
			media.URL = stream.Progressive
			if from > 0 {
				media.URL += "?from=" + strconv.FormatFloat(from, 'f', 3, 64)
			}
			media.ContentType = "video/mp2t"
			media.Start = 0
			offset = from
		case req.HLS:
			media.URL = stream.Playlist
			media.ContentType = cast.HLSContentType
		default:
			media.URL = stream.File
			media.ContentType = cast.VideoTypeFor(req.URL)
		}
		if mode != castProgressive && start > 0 && (kind == cast.KindDLNA || kind == cast.KindKodi) {
			// These start at the top and are moved once they play.
			pending, media.Start = start, 0
		}
		if req.HLS && req.Duration <= 0 {
			go func() {
				ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
				defer cancel()
				if d := relay.Duration(ctx); d > 0 {
					s.mu.Lock()
					if s.load == load && s.duration <= 0 {
						s.duration = d
					}
					s.mu.Unlock()
				}
			}()
		}
		if s.stale(load) {
			return nil
		}
		if err := s.player.PlayMedia(media); err != nil {
			return err
		}
		s.mu.Lock()
		if s.load == load {
			s.mode, s.offset, s.pending, s.note = mode, offset, pending, note
			s.subsVTT = subtitles
			s.lastOK = time.Now()
		}
		s.mu.Unlock()
		return nil
	}
}

func (s *castSession) ensureRelay(c *Core) (*cast.Relay, error) {
	s.mu.Lock()
	relay := s.relay
	s.mu.Unlock()
	if relay != nil {
		return relay, nil
	}
	// No overall timeout: a whole file is read for as long as it plays.
	client := &http.Client{Transport: c.transport}
	relay, err := cast.NewRelay(client, 0)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.relay = relay
	s.mu.Unlock()
	return relay, nil
}

// playFFmpeg rebuilds the stream from start with ffmpeg, subtitles burned in,
// and serves it from a scratch folder.
func (s *castSession) playFFmpeg(c *Core, load int, req castRequest, start float64, ffmpeg string) error {
	s.mu.Lock()
	if s.dir == "" {
		dir, err := os.MkdirTemp("", "otakase-cast-")
		if err != nil {
			s.mu.Unlock()
			return err
		}
		s.dir = dir
	}
	s.gen++
	dir, gen := s.dir, s.gen
	s.mu.Unlock()
	out := filepath.Join(dir, strconv.Itoa(gen))
	if err := os.MkdirAll(out, 0o700); err != nil {
		return err
	}
	subtitlePath := ""
	note := ""
	if req.Subtitle != "" {
		path, err := fetchSubtitleFile(s.ctx, c, req, out)
		if err != nil {
			cast.Log("cast: subtitles failed: " + err.Error())
			note = "The subtitles could not be loaded, so this plays without them."
		} else {
			subtitlePath = path
		}
	}
	var args []string
	if subtitlePath == "" && start <= 0 {
		args = cast.BuildRemuxArgsFrom(req.URL, "", out, 0, nil, req.Headers)
	} else {
		enc := cast.ChooseEncoder(ffmpeg, cast.DefaultRenderNode, func(e cast.Encoder) error {
			return cast.ProbeEncoder(ffmpeg, e)
		}, func(p string) bool { _, err := os.Stat(p); return err == nil })
		args = cast.BuildBurnArgsWithFonts(req.URL, "", subtitlePath, "", out, enc, start, nil, req.Headers)
	}
	remux, err := cast.StartFFmpeg(ffmpeg, args, out)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.remux = remux
	s.mu.Unlock()
	if err := awaitPlaylist(s.ctx, remux, out); err != nil {
		return err
	}
	s.mu.Lock()
	if s.server == nil {
		server, err := cast.NewServer(dir)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		s.server = server
	}
	server := s.server
	s.mu.Unlock()
	if s.stale(load) {
		return nil
	}
	if req.Duration <= 0 {
		go s.probeDuration(load, ffmpeg, req)
	}
	if err := s.player.PlayMedia(cast.Media{URL: playURL(s.device, server.URL(strconv.Itoa(gen)+"/"+cast.PlaylistName)), ContentType: contentTypeFor(s.device), Title: req.Title}); err != nil {
		return err
	}
	s.mu.Lock()
	if s.load == load {
		s.mode, s.offset, s.pending, s.note = castFFmpeg, start, -1, note
		s.lastOK = time.Now()
	}
	s.mu.Unlock()
	return nil
}

// playURL is the address a device plays an ffmpeg stream from: a DLNA
// renderer takes the joined transport stream served beside the playlist.
func playURL(d cast.Device, playlist string) string {
	if d.Kind == cast.KindDLNA {
		return strings.TrimSuffix(playlist, cast.PlaylistName) + cast.ProgressiveName
	}
	return playlist
}

func contentTypeFor(d cast.Device) string {
	if d.Kind == cast.KindDLNA {
		return "video/mp2t"
	}
	return cast.HLSContentType
}

func awaitPlaylist(ctx context.Context, remux *cast.Remux, dir string) error {
	deadline := time.Now().Add(90 * time.Second)
	playlist := filepath.Join(dir, cast.PlaylistName)
	for time.Now().Before(deadline) {
		if segments, _ := cast.ReadPlaylist(playlist); len(segments) > 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-remux.Done():
			if err := remux.Err(); err != nil {
				return err
			}
			if segments, _ := cast.ReadPlaylist(playlist); len(segments) > 0 {
				return nil
			}
			return errors.New("ffmpeg ended before the stream started")
		case <-time.After(200 * time.Millisecond):
		}
	}
	return errors.New("the stream did not start within 90 seconds")
}

func (s *castSession) probeDuration(load int, ffmpeg string, req castRequest) {
	ffprobe, err := cast.FFprobePathFor(ffmpeg)
	if err != nil {
		return
	}
	info, err := cast.ProbeStream(ffprobe, req.URL, "", req.Headers)
	if err != nil || info.Duration <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.load == load && s.duration <= 0 {
		s.duration = info.Duration
	}
}

func (s *castSession) stopRemux() {
	s.mu.Lock()
	remux := s.remux
	s.remux = nil
	s.mu.Unlock()
	if remux != nil {
		remux.Stop()
	}
}

// close ends the cast and lets the device go.
func (s *castSession) close() {
	s.cancel()
	s.op.Lock()
	defer s.op.Unlock()
	s.stopRemux()
	s.mu.Lock()
	player, relay, server, dir := s.player, s.relay, s.server, s.dir
	s.player, s.relay, s.server, s.dir = nil, nil, nil, ""
	s.state = "stopped"
	s.mu.Unlock()
	if player != nil {
		_ = player.Stop()
	}
	if relay != nil {
		_ = relay.Close()
	}
	if server != nil {
		_ = server.Close()
	}
	if dir != "" {
		os.RemoveAll(dir)
	}
}

func (k *caster) current() *castSession {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.session
}

// status asks the device where it is. While the device is being connected
// to or the stream rebuilt, it answers from what is known.
func (k *caster) status() any {
	s := k.current()
	if s == nil {
		return map[string]any{"active": false}
	}
	if s.op.TryLock() {
		s.poll()
		s.op.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]any{
		"active":   s.state != "stopped",
		"device":   s.device.Name,
		"kind":     s.device.Kind.Label(),
		"state":    s.state,
		"position": s.position,
		"duration": s.duration,
		"paused":   s.paused,
		"load":     s.load,
		"mode":     s.mode,
	}
	if s.device.Kind == "" {
		out["kind"] = cast.KindChromecast.Label()
	}
	if s.volume >= 0 {
		out["volume"] = s.volume
	}
	if s.note != "" {
		out["note"] = s.note
	}
	if s.err != "" {
		out["error"] = s.err
	}
	if s.link != "" {
		out["link"] = s.link
		out["viewers"] = cast.BrowserViewers()
	}
	return out
}

// poll reads the device once. Called with op held.
func (s *castSession) poll() {
	s.mu.Lock()
	player, state, load := s.player, s.state, s.load
	s.mu.Unlock()
	if player == nil || state == "error" || state == "stopped" || state == "connecting" || state == "ended" {
		return
	}
	p, err := player.Progress()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.load != load {
		return
	}
	if err != nil {
		if time.Since(s.lastOK) > castStatusGone {
			s.state, s.err = "error", castMessage(err)
		}
		return
	}
	s.lastOK = time.Now()
	s.volume = player.Volume()
	if p.Settling {
		return
	}
	if p.Idle {
		if s.seen {
			s.state = "ended"
		}
		return
	}
	if p.Position > 0 || p.Buffering {
		s.seen = true
	}
	if p.Duration > 0 && (s.mode == castRelay || s.duration <= 0) && p.Duration > s.duration {
		s.duration = p.Duration
	}
	if p.Position > 0 {
		s.position = s.offset + p.Position
	}
	switch {
	case !s.seen:
		s.state = "loading"
	case p.Buffering:
		s.state = "buffering"
	case s.paused:
		s.state = "paused"
	default:
		s.state = "playing"
	}
	if s.pending >= 0 && s.seen && !p.Buffering {
		target := s.pending
		s.pending = -1
		go func() {
			if err := player.SeekToTime(target); err != nil {
				cast.Log("cast: could not move to the resume point: " + err.Error())
			}
		}()
	}
}

func (k *caster) control(in castRequest) (any, error) {
	s := k.current()
	if s == nil {
		return nil, castRefusal("Nothing is being cast.")
	}
	switch in.Action {
	case "stop":
		k.mu.Lock()
		if k.session == s {
			k.session = nil
		}
		k.mu.Unlock()
		s.close()
		return map[string]any{"stopped": true}, nil
	case "pause", "play", "volume", "seek":
	default:
		return nil, castRefusal("Unknown cast action.")
	}
	s.mu.Lock()
	player := s.player
	s.mu.Unlock()
	if player == nil {
		return nil, castRefusal("The device is still connecting.")
	}
	var err error
	switch in.Action {
	case "pause":
		err = player.Pause()
		if err == nil {
			s.mu.Lock()
			s.paused, s.state = true, "paused"
			s.mu.Unlock()
		}
	case "play":
		err = player.Unpause()
		if err == nil {
			s.mu.Lock()
			s.paused = false
			s.mu.Unlock()
		}
	case "volume":
		err = player.SetVolume(min(max(in.Value, 0), 1))
		if err == nil {
			s.mu.Lock()
			s.volume = min(max(in.Value, 0), 1)
			s.mu.Unlock()
		}
	case "seek":
		go s.seek(max(in.Value, 0))
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

// seek moves the device. A stream that was rebuilt or joined is rebuilt from
// the target; a playlist passed through is seeked by the device itself.
func (s *castSession) seek(target float64) {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	mode, load, req, player := s.mode, s.load, s.req, s.player
	if s.duration > 0 {
		target = min(target, s.duration-1)
	}
	s.position = target
	s.mu.Unlock()
	if player == nil {
		return
	}
	if mode == castRelay {
		if err := player.SeekToTime(target); err != nil {
			cast.Log("cast: seek failed: " + err.Error())
		}
		return
	}
	s.mu.Lock()
	s.load++
	load = s.load
	s.seen, s.state, s.paused = false, "loading", false
	s.mu.Unlock()
	if err := s.play(s.core, load, req, target); err != nil && !s.stale(load) {
		s.fail(load, err)
	}
}

// browserCommand answers a press on the browser page.
func (k *caster) browserCommand(command string) {
	s := k.current()
	if s == nil {
		return
	}
	s.mu.Lock()
	paused, position, volume := s.paused, s.position, s.volume
	s.mu.Unlock()
	if volume < 0 {
		volume = 1
	}
	var in castRequest
	switch command {
	case "toggle":
		in.Action = "pause"
		if paused {
			in.Action = "play"
		}
	case "back":
		in.Action, in.Value = "seek", position-10
	case "forward":
		in.Action, in.Value = "seek", position+30
	case "volup":
		in.Action, in.Value = "volume", volume+0.1
	case "voldown":
		in.Action, in.Value = "volume", volume-0.1
	case "stop":
		in.Action = "stop"
	default:
		return
	}
	_, _ = k.control(in)
}

// closeCast ends any cast, for Core.Close.
func (k *caster) closeCast() {
	k.mu.Lock()
	s := k.session
	k.session = nil
	k.mu.Unlock()
	if s != nil {
		s.close()
	}
}

const maxSubtitleBytes = 16 << 20

func fetchSubtitles(ctx context.Context, c *Core, req castRequest) ([]byte, error) {
	transport := http.DefaultTransport
	if c != nil && c.transport != nil {
		transport = c.transport
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, req.Subtitle, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range req.Headers {
		r.Header.Set(k, v)
	}
	resp, err := (&http.Client{Transport: transport}).Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("subtitles answered %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxSubtitleBytes))
}

func fetchSubtitlesVTT(ctx context.Context, c *Core, req castRequest) ([]byte, error) {
	body, err := fetchSubtitles(ctx, c, req)
	if err != nil {
		return nil, err
	}
	return subtitlesToVTT(body)
}

// fetchSubtitleFile saves the subtitles for ffmpeg, under a name that says
// their format.
func fetchSubtitleFile(ctx context.Context, c *Core, req castRequest, dir string) (string, error) {
	body, err := fetchSubtitles(ctx, c, req)
	if err != nil {
		return "", err
	}
	name := "subtitles" + subtitleExtension(body)
	path := filepath.Join(dir, name)
	return path, os.WriteFile(path, body, 0o600)
}

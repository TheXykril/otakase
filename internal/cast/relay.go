package cast

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Relay hands a provider's stream to a cast device without ffmpeg.
//
// The device cannot send the headers a provider wants, so it fetches from this
// machine and the relay fetches from the provider with them. A playlist is
// rewritten on the way through so every playlist, segment and key it names is
// fetched through the relay too. Nothing is re-encoded: what the device plays
// is exactly what the provider serves, which is why a playlist keeps its
// length and the device can seek in it on its own.
//
// Addresses the relay hands out carry a signature, so the relay only ever
// fetches what a playlist it rewrote pointed at: a neighbour on the network
// cannot use it to fetch anything else.
type Relay struct {
	listener net.Listener
	server   *http.Server
	base     string
	key      []byte
	client   *http.Client

	mu        sync.Mutex
	source    *relaySource
	nextID    int
	subtitles []byte
	fetched   bool
}

type relaySource struct {
	id      int
	url     string
	headers map[string]string

	mu      sync.Mutex
	media   *mediaPlaylist
	mediaAt time.Time
}

// RelayStream is where a device fetches one source from.
type RelayStream struct {
	// Playlist is the rewritten HLS playlist, for a source that is one.
	Playlist string
	// Progressive is one continuous transport stream assembled from the
	// playlist's segments, for a device that cannot play a playlist. Add
	// "?from=" with a start time in seconds to begin later.
	Progressive string
	// File is the source passed through as it is, for a source that is a
	// whole file.
	File string
}

// relayPlaylistCache is how long a live playlist is reused before it is read
// again.
const relayPlaylistCache = 20 * time.Second

// NewRelay starts serving on this machine's LAN address. Port 0 picks a free
// port. client fetches from the provider.
func NewRelay(client *http.Client, port int) (*Relay, error) {
	addr, err := outboundIP()
	if err != nil {
		return nil, err
	}
	return newRelayOn(client, net.JoinHostPort(addr.String(), strconv.Itoa(port)))
}

func newRelayOn(client *http.Client, address string) (*Relay, error) {
	if client == nil {
		client = &http.Client{Timeout: 0}
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("cast: could not listen: %w", err)
	}
	r := &Relay{
		listener: listener,
		key:      secret,
		client:   client,
		base:     "http://" + listener.Addr().String() + "/" + hex.EncodeToString(token),
	}
	r.server = &http.Server{Handler: r, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := r.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			Log(fmt.Sprintf("cast: the relay stopped: %v", err))
		}
	}()
	return r, nil
}

// Serve makes url the source the relay hands out, replacing the last one.
// Addresses of the last source stop working.
func (r *Relay) Serve(source string, headers map[string]string) RelayStream {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	r.source = &relaySource{id: r.nextID, url: source, headers: headers}
	prefix := fmt.Sprintf("%s/%d/", r.base, r.nextID)
	return RelayStream{
		Playlist:    prefix + PlaylistName,
		Progressive: prefix + ProgressiveName,
		File:        prefix + "video" + fileExtension(source),
	}
}

// SetSubtitles serves a WebVTT file beside the stream and returns its address.
func (r *Relay) SetSubtitles(vtt []byte) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subtitles = vtt
	return r.base + "/subtitles.vtt"
}

// Fetched reports whether a device has asked for anything yet.
func (r *Relay) Fetched() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fetched
}

// Close stops serving.
func (r *Relay) Close() error {
	return r.server.Close()
}

// Duration is the length of the current source when it is a finished
// playlist, or 0.
func (r *Relay) Duration(ctx context.Context) float64 {
	src := r.current()
	if src == nil {
		return 0
	}
	media, err := r.mediaOf(ctx, src)
	if err != nil || !media.finished {
		return 0
	}
	total := 0.0
	for _, s := range media.segments {
		total += s.duration
	}
	return total
}

// ProgressiveStart is where a progressive stream asked to start at from
// really starts: the start of the segment from falls in, which is what the
// device then counts its position from.
func (r *Relay) ProgressiveStart(ctx context.Context, from float64) (float64, error) {
	src := r.current()
	if src == nil {
		return 0, errors.New("cast: nothing is being relayed")
	}
	media, err := r.mediaOf(ctx, src)
	if err != nil {
		return 0, err
	}
	_, start := media.from(from)
	return start, nil
}

func (r *Relay) current() *relaySource {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.source
}

func fileExtension(source string) string {
	u, err := url.Parse(source)
	if err != nil {
		return ""
	}
	ext := strings.ToLower(path.Ext(u.Path))
	switch ext {
	case ".mp4", ".mkv", ".webm", ".m4v", ".ts", ".avi", ".mov":
		return ext
	}
	return ""
}

func (r *Relay) sign(kind, target string) string {
	mac := hmac.New(sha256.New, r.key)
	mac.Write([]byte(kind + "|" + target))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:18])
}

// nested is the relay address for target, a URL a playlist named.
func (r *Relay) nested(id int, kind, target string) string {
	name := path.Base(strings.SplitN(target, "?", 2)[0])
	if name == "" || name == "." || name == "/" || strings.ContainsAny(name, "%#") {
		name = "item"
	}
	switch kind {
	case relayPlaylist:
		if !strings.HasSuffix(strings.ToLower(name), ".m3u8") {
			name += ".m3u8"
		}
	case relaySegment:
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".ts") && !strings.HasSuffix(lower, ".m4s") && !strings.HasSuffix(lower, ".mp4") && !strings.HasSuffix(lower, ".aac") {
			// Hosts dress segments up as images; the device decides what a
			// segment is partly from its name.
			name += ".ts"
		}
	}
	return fmt.Sprintf("%s/%d/x/%s/%s/%s/%s", r.base, id, kind, r.sign(kind, target), base64.RawURLEncoding.EncodeToString([]byte(target)), url.PathEscape(name))
}

const (
	relayPlaylist = "p"
	relaySegment  = "s"
	relayOther    = "o"
)

func (r *Relay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	prefix := strings.TrimPrefix(r.base, "http://"+r.listener.Addr().String())
	rest, ok := strings.CutPrefix(req.URL.Path, prefix+"/")
	if !ok {
		http.NotFound(w, req)
		return
	}
	// A receiver plays adaptive media in a web player, which reads every
	// file cross-origin.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Range")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range")
	if req.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.mu.Lock()
	r.fetched = true
	src := r.source
	subtitles := r.subtitles
	r.mu.Unlock()
	Log(fmt.Sprintf("cast: %s requested %s", req.RemoteAddr, rest))

	if rest == "subtitles.vtt" {
		if subtitles == nil {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "text/vtt")
		_, _ = w.Write(subtitles)
		return
	}
	idText, rest, _ := strings.Cut(rest, "/")
	id, err := strconv.Atoi(idText)
	if err != nil || src == nil || id != src.id {
		http.NotFound(w, req)
		return
	}
	switch {
	case rest == PlaylistName:
		r.servePlaylist(w, req, src, src.url)
	case rest == ProgressiveName:
		r.serveProgressive(w, req, src)
	case strings.HasPrefix(rest, "video"):
		r.servePassThrough(w, req, src, src.url, relayOther)
	case strings.HasPrefix(rest, "x/"):
		parts := strings.SplitN(strings.TrimPrefix(rest, "x/"), "/", 4)
		if len(parts) < 3 {
			http.NotFound(w, req)
			return
		}
		raw, err := base64.RawURLEncoding.DecodeString(parts[2])
		if err != nil {
			http.NotFound(w, req)
			return
		}
		kind, target := parts[0], string(raw)
		if !hmac.Equal([]byte(parts[1]), []byte(r.sign(kind, target))) {
			http.NotFound(w, req)
			return
		}
		if kind == relayPlaylist {
			r.servePlaylist(w, req, src, target)
			return
		}
		r.servePassThrough(w, req, src, target, kind)
	default:
		http.NotFound(w, req)
	}
}

func (r *Relay) fetch(ctx context.Context, src *relaySource, target string, rangeHeader string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range src.headers {
		req.Header.Set(k, v)
	}
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("cast: the source answered %s", resp.Status)
	}
	return resp, nil
}

// maxPlaylist bounds a playlist read into memory.
const maxPlaylist = 8 << 20

func (r *Relay) servePlaylist(w http.ResponseWriter, req *http.Request, src *relaySource, target string) {
	resp, err := r.fetch(req.Context(), src, target, "")
	if err != nil {
		Log(err.Error())
		http.Error(w, "source unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPlaylist))
	if err != nil {
		http.Error(w, "source unavailable", http.StatusBadGateway)
		return
	}
	if !bytes.HasPrefix(bytes.TrimLeft(bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf}), " \r\n\t"), []byte("#EXTM3U")) {
		// Not a playlist after all: a whole file behind a playlist name.
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		_, _ = w.Write(body)
		return
	}
	base := resp.Request.URL
	rewritten := rewritePlaylist(string(body), base, func(kind, u string) string {
		return r.nested(src.id, kind, u)
	})
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	if req.Method == http.MethodHead {
		return
	}
	_, _ = io.WriteString(w, rewritten)
}

var uriAttribute = regexp.MustCompile(`URI="([^"]*)"`)

// rewritePlaylist points every address in a playlist at the relay. A master
// playlist names playlists; a media playlist names segments. Keys and init
// sections go through untouched.
func rewritePlaylist(body string, base *url.URL, address func(kind, u string) string) string {
	master := strings.Contains(body, "#EXT-X-STREAM-INF")
	resolve := func(ref string) string {
		u, err := base.Parse(strings.TrimSpace(ref))
		if err != nil {
			return ref
		}
		return u.String()
	}
	var out strings.Builder
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			out.WriteString(line)
		case strings.HasPrefix(trimmed, "#"):
			kind := relayOther
			if strings.HasPrefix(trimmed, "#EXT-X-MEDIA:") || strings.HasPrefix(trimmed, "#EXT-X-I-FRAME-STREAM-INF:") {
				kind = relayPlaylist
			}
			out.WriteString(uriAttribute.ReplaceAllStringFunc(line, func(match string) string {
				ref := uriAttribute.FindStringSubmatch(match)[1]
				if strings.HasPrefix(ref, "data:") || strings.HasPrefix(ref, "skd:") {
					return match
				}
				return `URI="` + address(kind, resolve(ref)) + `"`
			}))
		default:
			kind := relaySegment
			if master {
				kind = relayPlaylist
			}
			out.WriteString(address(kind, resolve(trimmed)))
		}
		out.WriteByte('\n')
	}
	return strings.TrimRight(out.String(), "\n") + "\n"
}

func (r *Relay) servePassThrough(w http.ResponseWriter, req *http.Request, src *relaySource, target, kind string) {
	resp, err := r.fetch(req.Context(), src, target, req.Header.Get("Range"))
	if err != nil {
		Log(err.Error())
		http.Error(w, "source unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if kind == relaySegment && resp.StatusCode == http.StatusOK {
		body := bufio.NewReaderSize(resp.Body, 64<<10)
		head, _ := body.Peek(32 << 10)
		if skip, ok := transportStreamStart(head); ok {
			// A segment dressed up as an image starts with the picture;
			// the transport stream begins at the first run of sync bytes.
			_, _ = body.Discard(skip)
			w.Header().Set("Content-Type", "video/mp2t")
			if resp.ContentLength > 0 {
				w.Header().Set("Content-Length", strconv.FormatInt(resp.ContentLength-int64(skip), 10))
			}
			w.WriteHeader(http.StatusOK)
			if req.Method != http.MethodHead {
				_, _ = io.Copy(w, body)
			}
			return
		}
		copyHeaders(w, resp)
		w.WriteHeader(resp.StatusCode)
		if req.Method != http.MethodHead {
			_, _ = io.Copy(w, body)
		}
		return
	}
	copyHeaders(w, resp)
	if target == src.url {
		// The whole file: say what it is, and that it can be read from any
		// point, which DLNA renderers ask before they seek.
		if ctype := videoContentType(target, resp.Header.Get("Content-Type")); ctype != "" {
			w.Header().Set("Content-Type", ctype)
		}
		w.Header().Set("transferMode.dlna.org", "Streaming")
		w.Header().Set("contentFeatures.dlna.org", "DLNA.ORG_OP=01;DLNA.ORG_CI=0;DLNA.ORG_FLAGS=01700000000000000000000000000000")
	}
	w.WriteHeader(resp.StatusCode)
	if req.Method != http.MethodHead {
		_, _ = io.Copy(w, resp.Body)
	}
}

func copyHeaders(w http.ResponseWriter, resp *http.Response) {
	for _, name := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag"} {
		if v := resp.Header.Get(name); v != "" {
			w.Header().Set(name, v)
		}
	}
}

// videoContentType is the type to serve a whole file as. A torrent server or
// a CDN often says application/octet-stream, which a receiver will not play.
func videoContentType(target, upstream string) string {
	if strings.HasPrefix(upstream, "video/") || strings.HasPrefix(upstream, "audio/") {
		return upstream
	}
	return VideoTypeFor(target)
}

// VideoTypeFor guesses a whole file's media type from its address.
func VideoTypeFor(target string) string {
	switch fileExtension(target) {
	case ".mkv":
		return "video/x-matroska"
	case ".webm":
		return "video/webm"
	case ".ts":
		return "video/mp2t"
	case ".avi":
		return "video/x-msvideo"
	case ".mov":
		return "video/quicktime"
	default:
		return "video/mp4"
	}
}

// transportStreamStart finds where an MPEG transport stream starts in head:
// three sync bytes a packet apart. A segment that starts with one already is
// left alone (ok false), as is anything that is not a transport stream.
func transportStreamStart(head []byte) (int, bool) {
	const packet = 188
	if len(head) > 0 && head[0] == 0x47 {
		return 0, false
	}
	for i := 0; i+2*packet < len(head); i++ {
		if head[i] == 0x47 && head[i+packet] == 0x47 && head[i+2*packet] == 0x47 {
			return i, true
		}
	}
	return 0, false
}

type mediaSegment struct {
	url      string
	duration float64
	key      *segmentKey
	sequence int64
}

type segmentKey struct {
	method string
	url    string
	iv     []byte
}

type mediaPlaylist struct {
	segments []mediaSegment
	finished bool
	// separateAudio is set when the chosen variant takes its sound from
	// another playlist, which a single transport stream cannot carry.
	separateAudio bool
	target        float64
}

// from is the first segment to send for a start at seconds, and where that
// segment starts.
func (m *mediaPlaylist) from(seconds float64) (int, float64) {
	at := 0.0
	for i, s := range m.segments {
		if at+s.duration > seconds {
			return i, at
		}
		at += s.duration
	}
	if len(m.segments) == 0 {
		return 0, 0
	}
	last := len(m.segments) - 1
	return last, at - m.segments[last].duration
}

func (r *Relay) mediaOf(ctx context.Context, src *relaySource) (*mediaPlaylist, error) {
	src.mu.Lock()
	defer src.mu.Unlock()
	if src.media != nil && (src.media.finished || time.Since(src.mediaAt) < relayPlaylistCache) {
		return src.media, nil
	}
	media, err := r.loadMedia(ctx, src)
	if err != nil {
		if src.media != nil {
			return src.media, nil
		}
		return nil, err
	}
	src.media, src.mediaAt = media, time.Now()
	return media, nil
}

func (r *Relay) readPlaylist(ctx context.Context, src *relaySource, target string) (string, *url.URL, error) {
	resp, err := r.fetch(ctx, src, target, "")
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPlaylist))
	if err != nil {
		return "", nil, err
	}
	return string(body), resp.Request.URL, nil
}

// loadMedia reads the source down to one media playlist: the best variant of
// a master playlist.
func (r *Relay) loadMedia(ctx context.Context, src *relaySource) (*mediaPlaylist, error) {
	body, base, err := r.readPlaylist(ctx, src, src.url)
	if err != nil {
		return nil, err
	}
	separateAudio := false
	if strings.Contains(body, "#EXT-X-STREAM-INF") {
		variant, audio := bestVariant(body, base)
		if variant == "" {
			return nil, errors.New("cast: the playlist names no stream")
		}
		separateAudio = audio
		body, base, err = r.readPlaylist(ctx, src, variant)
		if err != nil {
			return nil, err
		}
	}
	media := parseMediaPlaylist(body, base)
	media.separateAudio = separateAudio
	return media, nil
}

var bandwidthAttribute = regexp.MustCompile(`(?:^|[,:])BANDWIDTH=(\d+)`)
var audioGroupAttribute = regexp.MustCompile(`AUDIO="([^"]*)"`)
var groupIDAttribute = regexp.MustCompile(`GROUP-ID="([^"]*)"`)

// bestVariant is the highest-bandwidth stream of a master playlist, and
// whether its sound comes from a playlist of its own.
func bestVariant(body string, base *url.URL) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	best, bestBandwidth, bestGroup := "", int64(-1), ""
	audioGroups := map[string]bool{}
	for _, line := range lines {
		if strings.HasPrefix(line, "#EXT-X-MEDIA:") && strings.Contains(line, "TYPE=AUDIO") && strings.Contains(line, `URI="`) {
			if m := groupIDAttribute.FindStringSubmatch(line); m != nil {
				audioGroups[m[1]] = true
			}
		}
	}
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			continue
		}
		bandwidth := int64(0)
		if m := bandwidthAttribute.FindStringSubmatch(line); m != nil {
			bandwidth, _ = strconv.ParseInt(m[1], 10, 64)
		}
		group := ""
		if m := audioGroupAttribute.FindStringSubmatch(line); m != nil {
			group = m[1]
		}
		for j := i + 1; j < len(lines); j++ {
			ref := strings.TrimSpace(lines[j])
			if ref == "" || strings.HasPrefix(ref, "#") {
				continue
			}
			if bandwidth > bestBandwidth {
				if u, err := base.Parse(ref); err == nil {
					best, bestBandwidth, bestGroup = u.String(), bandwidth, group
				}
			}
			i = j
			break
		}
	}
	return best, bestGroup != "" && audioGroups[bestGroup]
}

var keyMethod = regexp.MustCompile(`METHOD=([A-Z0-9-]+)`)
var keyIV = regexp.MustCompile(`IV=0[xX]([0-9a-fA-F]+)`)

func parseMediaPlaylist(body string, base *url.URL) *mediaPlaylist {
	media := &mediaPlaylist{}
	var key *segmentKey
	duration := 0.0
	sequence := int64(0)
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case line == "#EXT-X-ENDLIST":
			media.finished = true
		case strings.HasPrefix(line, "#EXT-X-PLAYLIST-TYPE:VOD"):
			media.finished = true
		case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"):
			sequence, _ = strconv.ParseInt(strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"), 10, 64)
		case strings.HasPrefix(line, "#EXT-X-TARGETDURATION:"):
			media.target, _ = strconv.ParseFloat(strings.TrimPrefix(line, "#EXT-X-TARGETDURATION:"), 64)
		case strings.HasPrefix(line, "#EXT-X-KEY:"):
			key = nil
			method := ""
			if m := keyMethod.FindStringSubmatch(line); m != nil {
				method = m[1]
			}
			if method != "" && method != "NONE" {
				key = &segmentKey{method: method}
				if m := uriAttribute.FindStringSubmatch(line); m != nil {
					if u, err := base.Parse(m[1]); err == nil {
						key.url = u.String()
					}
				}
				if m := keyIV.FindStringSubmatch(line); m != nil {
					key.iv, _ = hex.DecodeString(fmt.Sprintf("%032s", m[1]))
				}
			}
		case strings.HasPrefix(line, "#EXTINF:"):
			value, _, _ := strings.Cut(strings.TrimPrefix(line, "#EXTINF:"), ",")
			duration, _ = strconv.ParseFloat(strings.TrimSpace(value), 64)
		case strings.HasPrefix(line, "#"):
		default:
			if u, err := base.Parse(line); err == nil {
				media.segments = append(media.segments, mediaSegment{url: u.String(), duration: duration, key: key, sequence: sequence})
			}
			sequence++
			duration = 0
		}
	}
	return media
}

// serveProgressive sends one continuous transport stream: every segment of
// the media playlist in order, decrypted where the playlist says they are.
func (r *Relay) serveProgressive(w http.ResponseWriter, req *http.Request, src *relaySource) {
	ctx := req.Context()
	media, err := r.mediaOf(ctx, src)
	if err != nil {
		Log(err.Error())
		http.Error(w, "source unavailable", http.StatusBadGateway)
		return
	}
	if media.separateAudio {
		Log("cast: this source keeps its sound in a separate playlist; a single stream plays without it")
	}
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("transferMode.dlna.org", "Streaming")
	w.Header().Set("contentFeatures.dlna.org", dlnaContentFeatures)
	w.WriteHeader(http.StatusOK)
	if req.Method == http.MethodHead {
		return
	}
	from, _ := strconv.ParseFloat(req.URL.Query().Get("from"), 64)
	next, _ := media.from(from)
	flusher, _ := w.(http.Flusher)
	// Segments are fetched a few ahead of the one being written, so the
	// device's buffer is not left to drain while each next one is
	// requested from the source.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ready := make(chan progressivePiece, progressiveAhead)
	go r.fetchProgressive(ctx, src, media, next, ready)
	for piece := range ready {
		if piece.err != nil {
			if ctx.Err() == nil {
				Log(fmt.Sprintf("cast: a segment failed: %v", piece.err))
			}
			return
		}
		if _, err := w.Write(piece.body); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}

// progressiveAhead is how many segments a progressive stream holds ready
// beyond the one being written.
const progressiveAhead = 3

// progressivePiece is one segment ready to write, or why it could not be.
type progressivePiece struct {
	body []byte
	err  error
}

// fetchProgressive reads the segments of media from next in order into
// ready, following a playlist still being written, and closes ready when
// the stream is done, failed or no longer wanted.
func (r *Relay) fetchProgressive(ctx context.Context, src *relaySource, media *mediaPlaylist, next int, ready chan<- progressivePiece) {
	defer close(ready)
	keys := map[string][]byte{}
	sent := int64(-1)
	put := func(p progressivePiece) bool {
		select {
		case ready <- p:
			return true
		case <-ctx.Done():
			return false
		}
	}
	for {
		for _, segment := range media.segments[next:] {
			if segment.sequence <= sent {
				continue
			}
			body, err := r.segmentBytes(ctx, src, segment, keys)
			if !put(progressivePiece{body: body, err: err}) || err != nil {
				return
			}
			sent = segment.sequence
		}
		if media.finished {
			return
		}
		// A playlist still being written: wait for more.
		wait := time.Duration(max(media.target, 2) * float64(time.Second) / 2)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		fresh, err := r.loadMedia(ctx, src)
		if err != nil {
			return
		}
		media, next = fresh, 0
	}
}

// segmentBytes reads one segment whole, decrypted where it is locked, from
// the start of its transport stream.
func (r *Relay) segmentBytes(ctx context.Context, src *relaySource, segment mediaSegment, keys map[string][]byte) ([]byte, error) {
	resp, err := r.fetch(ctx, src, segment.url, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return nil, err
	}
	if segment.key != nil {
		if segment.key.method != "AES-128" {
			return nil, fmt.Errorf("cast: segments locked with %s cannot be relayed", segment.key.method)
		}
		key, ok := keys[segment.key.url]
		if !ok {
			keyResp, err := r.fetch(ctx, src, segment.key.url, "")
			if err != nil {
				return nil, err
			}
			key, err = io.ReadAll(io.LimitReader(keyResp.Body, 64))
			keyResp.Body.Close()
			if err != nil {
				return nil, err
			}
			if len(key) != 16 {
				return nil, errors.New("cast: the segment key is not 16 bytes")
			}
			keys[segment.key.url] = key
		}
		if body, err = decryptSegment(body, key, segment.key.iv, segment.sequence); err != nil {
			return nil, err
		}
	}
	if skip, ok := transportStreamStart(body[:min(len(body), 32<<10)]); ok {
		body = body[skip:]
	}
	return body, nil
}

// decryptSegment undoes HLS AES-128: CBC with PKCS#7 padding, and the media
// sequence number as the IV when the playlist names none.
func decryptSegment(body, key, iv []byte, sequence int64) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 || len(body)%aes.BlockSize != 0 {
		return nil, errors.New("cast: an encrypted segment has a broken length")
	}
	if len(iv) != aes.BlockSize {
		iv = make([]byte, aes.BlockSize)
		binary.BigEndian.PutUint64(iv[8:], uint64(sequence))
	}
	plain := make([]byte, len(body))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, body)
	pad := int(plain[len(plain)-1])
	if pad > 0 && pad <= aes.BlockSize && pad <= len(plain) {
		plain = plain[:len(plain)-pad]
	}
	return plain, nil
}

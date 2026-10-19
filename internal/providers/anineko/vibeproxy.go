package anineko

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// vibeplayer serves each MPEG-TS segment behind a small PNG image, so a
// player sees a picture instead of video. The proxy serves the playlists
// with every URI pointing back at itself and cuts the image off each segment
// on the way through.
//
// Routes, all under /stream/{session}/:
//
//	master.m3u8            the master playlist
//	{variant}.m3u8         a variant playlist, named as the master names it
//	{variant}.m3u8/seg/{n} the n-th segment of that variant

// pngEnd is the IEND chunk type and its CRC, the last bytes of any PNG.
var pngEnd = []byte{0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82}

type vibeProxy struct {
	base   string
	server *http.Server

	mu       sync.Mutex
	sessions map[string]*vibeSession
}

type vibeSession struct {
	master  string
	referer string
	// segments holds each variant's upstream segment URLs, filled in when
	// the variant playlist is served.
	segments map[string][]string
}

var (
	proxyMu sync.Mutex
	proxy   *vibeProxy
)

// registerVibeStream returns a local URL that plays the given master playlist.
func registerVibeStream(master, referer string) (string, error) {
	p, err := startVibeProxy()
	if err != nil {
		return "", err
	}
	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return "", err
	}
	session := hex.EncodeToString(id)

	p.mu.Lock()
	p.sessions[session] = &vibeSession{master: master, referer: referer, segments: map[string][]string{}}
	p.mu.Unlock()
	return p.base + "/stream/" + session + "/master.m3u8", nil
}

// startVibeProxy starts the proxy on first use; later streams share it.
func startVibeProxy() (*vibeProxy, error) {
	proxyMu.Lock()
	defer proxyMu.Unlock()
	if proxy != nil {
		return proxy, nil
	}

	// Loopback only, on any free port: nothing but the local player needs it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("vibe proxy: listen on loopback: %w", err)
	}
	p := &vibeProxy{base: "http://" + ln.Addr().String(), sessions: map[string]*vibeSession{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/stream/", p.serve)
	p.server = &http.Server{Handler: mux}
	go func() { _ = p.server.Serve(ln) }()

	proxy = p
	return p, nil
}

// serve routes a request under /stream/{session}/ to the handler for what
// it asks for.
func (p *vibeProxy) serve(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/stream/"), "/")
	var session *vibeSession
	if len(parts) >= 2 {
		p.mu.Lock()
		session = p.sessions[parts[0]]
		p.mu.Unlock()
	}
	if session == nil {
		http.NotFound(w, r)
		return
	}

	id, rest := parts[0], parts[1:]
	switch {
	case len(rest) == 1 && rest[0] == "master.m3u8":
		p.serveMaster(w, id, session)
	case len(rest) == 1 && strings.HasSuffix(rest[0], ".m3u8"):
		p.serveVariant(w, id, session, rest[0])
	case len(rest) == 3 && rest[1] == "seg":
		p.serveSegment(w, r, session, rest[0], rest[2])
	default:
		http.NotFound(w, r)
	}
}

// fetchUpstream gets target from the real host. When that fails it answers
// the request with 502 itself and reports false.
func fetchUpstream(w http.ResponseWriter, target, referer string) ([]byte, bool) {
	data, err := get(target, referer)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return nil, false
	}
	return data, true
}

// serveMaster serves the master playlist with each variant pointing back here.
func (p *vibeProxy) serveMaster(w http.ResponseWriter, id string, session *vibeSession) {
	body, ok := fetchUpstream(w, session.master, session.referer)
	if !ok {
		return
	}
	writePlaylist(w, mapURIs(string(body), func(uri string) string {
		return p.base + "/stream/" + id + "/" + uri
	}))
}

// serveVariant serves a variant playlist with numbered local segment URLs,
// and remembers which upstream segment each number stands for.
func (p *vibeProxy) serveVariant(w http.ResponseWriter, id string, session *vibeSession, variant string) {
	variantURL := resolveRef(session.master, variant)
	body, ok := fetchUpstream(w, variantURL, session.referer)
	if !ok {
		return
	}
	var segments []string
	playlist := mapURIs(string(body), func(uri string) string {
		segments = append(segments, resolveRef(variantURL, uri))
		return fmt.Sprintf("%s/stream/%s/%s/seg/%d", p.base, id, variant, len(segments)-1)
	}) // called in playlist order, so each index is the segment's position

	p.mu.Lock()
	session.segments[variant] = segments
	p.mu.Unlock()
	writePlaylist(w, playlist)
}

// serveSegment serves one segment with its PNG disguise cut off.
func (p *vibeProxy) serveSegment(w http.ResponseWriter, r *http.Request, session *vibeSession, variant, index string) {
	n, err := strconv.Atoi(index)
	p.mu.Lock()
	segments := session.segments[variant]
	p.mu.Unlock()
	if err != nil || n < 0 || n >= len(segments) {
		http.NotFound(w, r) // also a variant whose playlist was never served
		return
	}

	raw, ok := fetchUpstream(w, segments[n], session.referer)
	if !ok {
		return
	}
	ts := stripPNG(raw)
	h := w.Header()
	h.Set("Content-Type", "video/mp2t")
	h.Set("Content-Length", strconv.Itoa(len(ts)))
	_, _ = w.Write(ts)
}

// mapURIs trims every line of a playlist and replaces each URI line with
// what rewrite returns for it. Tags and blank lines are kept.
func mapURIs(playlist string, rewrite func(string) string) string {
	rows := strings.Split(strings.ReplaceAll(playlist, "\r\n", "\n"), "\n")
	for i := range rows {
		row := strings.TrimSpace(rows[i])
		if row != "" && !strings.HasPrefix(row, "#") {
			row = rewrite(row)
		}
		rows[i] = row
	}
	return strings.Join(rows, "\n")
}

// hlsContentType is the media type players expect for an m3u8 playlist.
const hlsContentType = "application/vnd.apple.mpegurl"

func writePlaylist(w http.ResponseWriter, playlist string) {
	w.Header().Set("Content-Type", hlsContentType)
	_, _ = w.Write([]byte(playlist))
}

// stripPNG drops everything up to the end of a leading PNG image. Data with
// no PNG in it comes back unchanged.
func stripPNG(data []byte) []byte {
	if i := bytes.Index(data, pngEnd); i >= 0 {
		return data[i+len(pngEnd):]
	}
	return data
}

// resetVibeProxyForTest shuts the shared proxy down so a test starts afresh.
func resetVibeProxyForTest() {
	proxyMu.Lock()
	defer proxyMu.Unlock()
	if proxy != nil {
		_ = proxy.server.Close()
	}
	proxy = nil
}

package reanime

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/thexykril/otakase/internal/providerhost"
)

// flixBrowserHeaders marks video requests as cross-origin browser fetches.
// Some FlixCloud segment hosts reject requests with only a Referer and UA.
func flixBrowserHeaders() map[string]string {
	return map[string]string{
		"User-Agent":     userAgent,
		"Origin":         flixURL,
		"Accept":         "*/*",
		"Sec-Fetch-Site": "cross-site",
		"Sec-Fetch-Mode": "cors",
		"Sec-Fetch-Dest": "empty",
	}
}

var flixSegmentMask = [...]byte{157, 42, 241, 71, 179, 142, 92, 112, 166, 25, 228, 59, 216, 98, 15, 197}

var flixURI = regexp.MustCompile(`URI="([^"]+)"`)

// DecodeManifest reverses the player's Base64/XOR mask on FlixCloud playlists.
// A normal, unmasked playlist is also valid.
func DecodeManifest(body []byte, keyBase64 string) ([]byte, error) {
	plain := bytes.TrimSpace(body)
	if bytes.HasPrefix(plain, []byte("#EXTM3U")) {
		return bytes.Clone(plain), nil
	}
	key, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil || len(key) != 32 {
		return nil, errors.New("reanime: invalid HLS playlist key")
	}
	encrypted, err := base64.StdEncoding.DecodeString(string(plain))
	if err != nil || len(encrypted) == 0 || len(encrypted) > 4<<20 {
		return nil, errors.New("reanime: invalid encoded HLS playlist")
	}
	for i := range encrypted {
		encrypted[i] ^= key[i%len(key)]
	}
	if !bytes.HasPrefix(encrypted, []byte("#EXTM3U")) {
		return nil, errors.New("reanime: HLS playlist decryption failed")
	}
	return encrypted, nil
}

// DecodeSegment strips FlixCloud's image wrapper, then unmasks its payload
// when the transport stream was additionally obfuscated.
func DecodeSegment(body []byte) ([]byte, error) {
	data := body
	switch {
	case len(body) >= 12 && bytes.Equal(body[:4], []byte("RIFF")) && bytes.Equal(body[8:12], []byte("WEBP")):
		data = bytes.Clone(body[12:])
	case len(body) >= 8 && bytes.Equal(body[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}):
		data = bytes.Clone(body[8:])
	default:
		return body, nil
	}
	if len(data) == 0 {
		return nil, errors.New("reanime: empty video segment")
	}
	if data[0] == 0x47 || len(data) > 8 && string(data[4:8]) == "ftyp" {
		return data, nil
	}
	for i := range data {
		data[i] ^= flixSegmentMask[i%len(flixSegmentMask)]
	}
	if data[0] != 0x47 && !(len(data) > 8 && string(data[4:8]) == "ftyp") {
		return nil, errors.New("reanime: video segment decryption failed")
	}
	return data, nil
}

func flixMediaHost(host string) bool {
	host = strings.ToLower(host)
	return flixHost(host) || host == "overcdn.site" || strings.HasSuffix(host, ".overcdn.site")
}

func flixMediaURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.User == nil && u.Port() == "" && flixMediaHost(u.Hostname())
}

type relayItem struct {
	link string
	kind string
}

type hlsRelay struct {
	mu     sync.Mutex
	items  map[string]relayItem
	lookup map[string]string
	next   int
	client *http.Client
	key    string
	logged sync.Map
}

// relayStatus records the source hostname and fetch stage, never a signed
// stream URL or playlist key. Repeated failures are logged only once.
func (p *hlsRelay) relayStatus(item relayItem, status string) {
	if providerhost.Log == nil {
		return
	}
	host := "unknown"
	if parsed, err := url.Parse(item.link); err == nil {
		host = parsed.Hostname()
	}
	line := item.kind + " " + host + ": " + status
	if _, loaded := p.logged.LoadOrStore(line, struct{}{}); loaded {
		return
	}
	providerhost.Log("reanime HLS relay " + line)
}

func (p *hlsRelay) add(link, kind string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if id, ok := p.lookup[link]; ok {
		return id
	}
	p.next++
	ext := ".bin"
	if kind == "list" {
		ext = ".m3u8"
	} else if kind == "seg" {
		ext = ".ts"
	}
	id := strconv.Itoa(p.next) + ext
	p.items[id] = relayItem{link: link, kind: kind}
	p.lookup[link] = id
	return id
}

func (p *hlsRelay) get(id string) (relayItem, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	item, ok := p.items[id]
	return item, ok
}

func (p *hlsRelay) reference(base *url.URL, reference, kind string) (string, error) {
	u, err := base.Parse(reference)
	if err != nil {
		return "", errors.New("reanime: invalid HLS resource URL")
	}
	if !flixMediaURL(u.String()) {
		// Never log the full URL: FlixCloud resources can contain signed
		// tokens and media IDs. Only the scheme and hostname are useful
		// when deciding whether a new CDN belongs on the allowlist.
		host := u.Hostname()
		if len(host) > 253 {
			host = host[:253]
		}
		return "", fmt.Errorf("reanime: unsafe HLS resource (scheme=%q, host=%q)", u.Scheme, host)
	}
	if strings.HasSuffix(strings.ToLower(u.Path), ".m3u8") {
		kind = "list"
	}
	return p.add(u.String(), kind), nil
}

func (p *hlsRelay) rewrite(playlist []byte, base *url.URL) ([]byte, error) {
	var lines []string
	next := "seg"
	for _, original := range strings.Split(strings.ReplaceAll(string(playlist), "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(original)
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "#"):
			if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
				next = "list"
			}
			var rewriteErr error
			line = flixURI.ReplaceAllStringFunc(line, func(attr string) string {
				if rewriteErr != nil {
					return ""
				}
				match := flixURI.FindStringSubmatch(attr)
				// AES keys can be embedded as data: URIs. They are already
				// complete and must not be sent to an HTTP relay.
				if strings.HasPrefix(strings.ToLower(match[1]), "data:") {
					return attr
				}
				kind := "seg"
				if strings.HasPrefix(line, "#EXT-X-MEDIA:") || strings.HasPrefix(line, "#EXT-X-I-FRAME-STREAM-INF:") {
					kind = "list"
				}
				id, err := p.reference(base, match[1], kind)
				if err != nil {
					rewriteErr = err
					return ""
				}
				return `URI="` + id + `"`
			})
			if rewriteErr != nil {
				return nil, rewriteErr
			}
		default:
			id, err := p.reference(base, line, next)
			if err != nil {
				return nil, err
			}
			line, next = id, "seg"
		}
		lines = append(lines, line)
	}
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}

func (p *hlsRelay) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	item, ok := p.get(strings.TrimPrefix(r.URL.Path, "/"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	fail := func(status string) {
		p.relayStatus(item, status)
		http.Error(w, "video source unavailable", http.StatusBadGateway)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, item.link, nil)
	if err != nil {
		fail("request setup failed")
		return
	}
	req.Header.Set("Referer", flixURL+"/")
	for name, value := range flixBrowserHeaders() {
		req.Header.Set(name, value)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			fail("upstream timeout")
		case errors.Is(err, context.Canceled):
			fail("request canceled")
		default:
			// Transport errors can include the signed URL. Never print
			// the error value, only its type.
			fail(fmt.Sprintf("upstream transport error (%T)", err))
		}
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fail(fmt.Sprintf("upstream HTTP %d", resp.StatusCode))
		return
	}
	limit := int64(16 << 20)
	if item.kind == "list" {
		limit = 4 << 20
	}
	if item.kind == "key" {
		limit = 1 << 20
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		fail("upstream body read failed")
		return
	}
	if int64(len(body)) > limit {
		fail("upstream response too large")
		return
	}
	mime := resp.Header.Get("Content-Type")
	switch item.kind {
	case "list":
		body, err = DecodeManifest(body, p.key)
		if err == nil {
			base, _ := url.Parse(item.link)
			body, err = p.rewrite(body, base)
		}
		mime = "application/vnd.apple.mpegurl"
	case "seg":
		body, err = DecodeSegment(body)
		mime = "video/mp2t"
		if len(body) > 8 && string(body[4:8]) == "ftyp" {
			mime = "video/mp4"
		}
	}
	if err != nil {
		fail("decode failed: " + err.Error())
		return
	}
	p.relayStatus(item, "decoded OK")
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method == http.MethodGet {
		_, _ = w.Write(body)
	}
}

// StartRelay exposes a session's decoded HLS only on the local loopback.
// The returned stop function shuts down its listener and request handlers;
// the caller owns the listener for as long as the player uses it.
func StartRelay(streamURL, key string, client *http.Client) (string, func(), error) {
	if !flixMediaURL(streamURL) {
		return "", nil, errors.New("reanime: invalid HLS upstream")
	}
	decodedKey, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(decodedKey) != 32 {
		return "", nil, errors.New("reanime: invalid HLS playlist key")
	}
	if client == nil {
		client = http.DefaultClient
	}
	// Redirects must not let a playlist lead the local relay to a private
	// host. The source hosts are checked again on every redirect.
	httpClient := *client
	httpClient.CheckRedirect = func(req *http.Request, previous []*http.Request) error {
		if len(previous) >= 5 || !flixMediaURL(req.URL.String()) {
			return errors.New("reanime: unsafe HLS redirect")
		}
		return nil
	}
	session := &hlsRelay{items: make(map[string]relayItem), lookup: make(map[string]string), client: &httpClient, key: key}
	entry := session.add(streamURL, "list")
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", nil, err
	}
	// Set the handler before serving, so concurrent requests never race
	// with the server's initialization.
	root := "/" + hex.EncodeToString(nonce[:]) + "/"
	server := &http.Server{
		Handler:           http.StripPrefix(root, http.HandlerFunc(session.serve)),
		ReadHeaderTimeout: 5 * time.Second,
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("reanime: local relay: %w", err)
	}
	var once sync.Once
	stop := func() {
		once.Do(func() { _ = server.Close() })
	}
	go func() { _ = server.Serve(listener) }()
	if providerhost.Log != nil {
		providerhost.Log("reanime HLS relay ready on loopback")
	}
	return "http://" + listener.Addr().String() + root + entry, stop, nil
}

package main

import (
	"bufio"
	"container/list"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// The watch room's relay. Discord Activities can only load from their own
// address, so the bot fetches the episode from the provider and passes it
// on under /activity/api/watch/media/{key}/{id}. Playlists are rewritten so
// every address in them points back here; ids are handed out per address,
// so the relay never fetches anything a playlist didn't name. The master
// playlist keeps one variant (the best at or under watchMaxHeight) and one
// audio track: everyone then asks for the same segments, and the segment
// cache fetches each from the provider once however many are watching.
// key is random per episode and the only thing a viewer needs, which lets
// <video> and <track> load without the session header.

const (
	watchMaxHeight    = 720
	watchCacheBytes   = 192 << 20 // segments kept in memory, across episodes
	watchCacheItem    = 16 << 20  // anything bigger streams through uncached
	watchPrefetch     = 3         // segments fetched ahead of the one asked for
	watchUpstreamWait = 45 * time.Second
	watchBrowserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"
)

// watchUpstream is the client for provider fetches, a variable for tests.
var watchUpstream = &http.Client{Timeout: 2 * time.Minute}

// streamLink is one link from `otakase -stream-info`.
type streamLink struct {
	URL          string            `json:"url"`
	Referrer     string            `json:"referrer,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	FlixCloudKey string            `json:"flixcloud_key,omitempty"`
	Subtitles    []streamSubtitle  `json:"subtitles,omitempty"`
	// HLS marks a playlist whose address doesn't say so (megavid's /vid/
	// links); markHLSLinks sets it.
	HLS bool `json:"hls,omitempty"`
}

// isHLS tells a playlist from a file.
func (l streamLink) isHLS() bool {
	return l.HLS || looksLikeHLS(l.URL)
}

type streamSubtitle struct {
	URL      string `json:"url"`
	Language string `json:"language,omitempty"`
	Label    string `json:"label,omitempty"`
}

// watchMedia is one episode as the relay serves it.
type watchMedia struct {
	key   string
	link  streamLink
	dub   bool   // pick the English audio track
	audio string // force an HLS rendition for native players

	mu    sync.Mutex
	ids   map[string]string // id → upstream address
	byURL map[string]string // upstream address → id
	kind  map[string]string // id → "list", "seg", "file" or "sub"
	next  map[string][]string
	n     int

	in, out atomic.Int64 // bytes from the provider, bytes to viewers
}

// newWatchMedia registers an episode's link. Entry is the id of the main
// address and subs the ids of its subtitle tracks, in the link's order.
func newWatchMedia(link streamLink, dub bool) (m *watchMedia, entry string, subs []string) {
	m = &watchMedia{key: newToken(16), link: link, dub: dub, ids: map[string]string{}, byURL: map[string]string{},
		kind: map[string]string{}, next: map[string][]string{}}
	kind := "file"
	if link.isHLS() {
		kind = "list"
	}
	entry = m.add(link.URL, kind)
	for _, s := range link.Subtitles {
		subs = append(subs, m.add(s.URL, "sub"))
	}
	return m, entry, subs
}

// looksLikeHLS tells a playlist from a file by its address.
func looksLikeHLS(u string) bool {
	p, err := url.Parse(u)
	if err != nil {
		return false
	}
	return strings.HasSuffix(strings.ToLower(p.Path), ".m3u8") || strings.Contains(strings.ToLower(u), "m3u8")
}

// watchSniffWait bounds the request markHLSLinks makes per link.
const watchSniffWait = 5 * time.Second

// markHLSLinks asks the provider about each link whose address doesn't name
// a playlist and that the app hasn't already marked, and marks the ones
// that answer with one. Without this a playlist served under a bare address
// is relayed as a video file, which the browser cannot play.
func markHLSLinks(ctx context.Context, info *streamInfo) {
	var wg sync.WaitGroup
	mark := func(link *streamLink) {
		if link.URL == "" || link.isHLS() {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			link.HLS = sniffHLS(ctx, *link)
		}()
	}
	for k := range info.Streams {
		mark(&info.Streams[k])
	}
	variants := make(map[string]*streamLink, len(info.AudioVariants))
	for language, link := range info.AudioVariants {
		variants[language] = &link
		mark(variants[language])
	}
	wg.Wait()
	for language, link := range variants {
		info.AudioVariants[language] = *link
	}
}

// sniffHLS reads the start of an address to see whether it is a playlist.
func sniffHLS(ctx context.Context, link streamLink) bool {
	ctx, cancel := context.WithTimeout(ctx, watchSniffWait)
	defer cancel()
	req, err := (&watchMedia{link: link}).request(ctx, link.URL)
	if err != nil {
		return false
	}
	req.Header.Set("Range", "bytes=0-63")
	resp, err := watchUpstream.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return false
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "mpegurl") {
		return true
	}
	head := make([]byte, 7)
	n, _ := io.ReadFull(resp.Body, head)
	return string(head[:n]) == "#EXTM3U"
}

// add gives an upstream address an id. Ids carry an extension players
// recognise; they never repeat within an episode.
func (m *watchMedia) add(u, kind string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, ok := m.byURL[u]; ok {
		return id
	}
	m.n++
	ext := map[string]string{"list": ".m3u8", "seg": ".ts", "file": ".mp4", "sub": ".vtt"}[kind]
	id := strconv.Itoa(m.n) + ext
	m.ids[id], m.byURL[u], m.kind[id] = u, id, kind
	return id
}

func (m *watchMedia) lookup(id string) (u, kind string, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok = m.ids[id]
	return u, m.kind[id], ok
}

// request builds a provider request with the headers the link needs.
func (m *watchMedia) request(ctx context.Context, u string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", watchBrowserAgent)
	if m.link.Referrer != "" {
		req.Header.Set("Referer", m.link.Referrer)
	}
	for k, v := range m.link.Headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

// fetch reads an upstream address whole, up to max bytes.
func (m *watchMedia) fetch(ctx context.Context, u string, max int64) ([]byte, string, error) {
	req, err := m.request(ctx, u)
	if err != nil {
		return nil, "", err
	}
	resp, err := watchUpstream.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("provider answered %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	m.in.Add(int64(len(data)))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > max {
		return nil, "", errTooBig
	}
	return data, resp.Header.Get("Content-Type"), nil
}

var errTooBig = errors.New("too big to cache")

var hlsURIAttr = regexp.MustCompile(`URI="([^"]*)"`)

// rewritePlaylist points every address in a playlist at the relay. A master
// playlist is cut to one variant and one audio track first.
func (m *watchMedia) rewritePlaylist(body string, base *url.URL) string {
	if strings.Contains(body, "#EXT-X-STREAM-INF") {
		body = trimMasterAudio(body, watchMaxHeight, m.dub, m.audio)
	}
	var out []string
	var segs []string
	nextKind := "seg"
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "#"):
			if strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
				nextKind = "list"
			}
			line = hlsURIAttr.ReplaceAllStringFunc(line, func(attr string) string {
				ref := hlsURIAttr.FindStringSubmatch(attr)[1]
				kind := "seg"
				if strings.HasPrefix(line, "#EXT-X-MEDIA") {
					kind = "list"
				}
				return `URI="` + m.add(resolveRef(base, ref), kind) + `"`
			})
		default:
			id := m.add(resolveRef(base, line), nextKind)
			if nextKind == "seg" {
				segs = append(segs, id)
			}
			line, nextKind = id, "seg"
		}
		out = append(out, line)
	}
	if len(segs) > 0 {
		m.mu.Lock()
		for k, id := range segs {
			m.next[id] = segs[k+1 : min(len(segs), k+1+watchPrefetch)]
		}
		m.mu.Unlock()
	}
	return strings.Join(out, "\n") + "\n"
}

func resolveRef(base *url.URL, ref string) string {
	u, err := base.Parse(ref)
	if err != nil {
		return ref
	}
	return u.String()
}

// hlsVariant is one #EXT-X-STREAM-INF entry of a master playlist.
type hlsVariant struct {
	inf, uri  string
	height    int
	bandwidth int
	audio     string
}

var (
	hlsResolution = regexp.MustCompile(`RESOLUTION=\d+x(\d+)`)
	hlsBandwidth  = regexp.MustCompile(`[^-]BANDWIDTH=(\d+)`)
	hlsAudioGroup = regexp.MustCompile(`AUDIO="([^"]*)"`)
	hlsGroupID    = regexp.MustCompile(`GROUP-ID="([^"]*)"`)
	hlsDefault    = regexp.MustCompile(`DEFAULT=(YES|NO)`)
	hlsLanguage   = regexp.MustCompile(`LANGUAGE="([^"]*)"`)
)

// trimMaster keeps the best variant at or under maxHeight (the smallest
// when every one is bigger, the leanest under 4 Mbps when none says its
// size) and the audio track it plays with: English for a dub, Japanese
// otherwise, else the default one. A master with every language would
// otherwise play its default, Japanese, for a dub too. Subtitle groups
// stay; I-frame playlists go.
func trimMaster(body string, maxHeight int, dub bool) string {
	return trimMasterAudio(body, maxHeight, dub, "")
}

// trimMasterAudio selects one advertised language as the default. Separate
// relayed master URLs make HLS languages selectable in native video players.
func trimMasterAudio(body string, maxHeight int, dub bool, language string) string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var head []string
	var vars []hlsVariant
	var audio []string
	for k := 0; k < len(lines); k++ {
		line := strings.TrimSpace(lines[k])
		switch {
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF"):
			v := hlsVariant{inf: line}
			for k+1 < len(lines) {
				k++
				if u := strings.TrimSpace(lines[k]); u != "" && !strings.HasPrefix(u, "#") {
					v.uri = u
					break
				}
			}
			if m := hlsResolution.FindStringSubmatch(line); m != nil {
				v.height, _ = strconv.Atoi(m[1])
			}
			if m := hlsBandwidth.FindStringSubmatch(" " + line); m != nil {
				v.bandwidth, _ = strconv.Atoi(m[1])
			}
			if m := hlsAudioGroup.FindStringSubmatch(line); m != nil {
				v.audio = m[1]
			}
			if v.uri != "" {
				vars = append(vars, v)
			}
		case strings.HasPrefix(line, "#EXT-X-I-FRAME-STREAM-INF"):
		case strings.HasPrefix(line, "#EXT-X-MEDIA") && strings.Contains(line, "TYPE=AUDIO"):
			audio = append(audio, line)
		case line != "":
			head = append(head, line)
		}
	}
	if len(vars) == 0 {
		return body
	}
	v := pickVariant(vars, maxHeight)
	out := head
	// Some providers split languages across HLS audio groups while the
	// chosen video variant references only one of those groups. Remap
	// explicit audio rendition URIs into the chosen group so Hls.js can
	// expose all languages without switching the video rendition.
	var merged []string
	seen := map[string]bool{}
	for pass := 0; pass < 2; pass++ {
		for _, entry := range audio {
			group := hlsGroupID.FindStringSubmatch(entry)
			if v.audio == "" || group == nil {
				continue
			}
			inGroup := group[1] == v.audio
			if (pass == 0) != inGroup {
				continue
			}
			if !inGroup {
				// An implicit/muxed audio rendition cannot be transplanted.
				if !hlsURIAttr.MatchString(entry) {
					continue
				}
				entry = strings.Replace(entry, `GROUP-ID="`+group[1]+`"`, `GROUP-ID="`+v.audio+`"`, 1)
			}
			// The selected group's rendition wins when a language occurs
			// in multiple encoding groups.
			key := entry
			if lang := hlsLanguage.FindStringSubmatch(entry); lang != nil {
				key = strings.ToLower(lang[1])
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, entry)
		}
	}
	if v.audio == "" {
		// A variant without an audio group cannot switch renditions.
		merged = nil
	}
	preferred := pickAudio(merged, v.audio, dub)
	if language != "" {
		for _, entry := range merged {
			if hlsAudioLanguage(entry) == language {
				preferred = entry
				break
			}
		}
	}
	for _, entry := range merged {
		if hlsDefault.MatchString(entry) {
			if entry == preferred {
				entry = hlsDefault.ReplaceAllString(entry, "DEFAULT=YES")
			} else {
				entry = hlsDefault.ReplaceAllString(entry, "DEFAULT=NO")
			}
		} else if entry == preferred {
			entry += ",DEFAULT=YES"
		} else {
			entry += ",DEFAULT=NO"
		}
		out = append(out, entry)
	}
	return strings.Join(append(out, v.inf, v.uri), "\n") + "\n"
}

// hlsAudioLanguage converts the common ISO 639-2 tags in HLS manifests to
// the two-letter language codes accepted by the watch room.
func hlsAudioLanguage(line string) string {
	match := hlsLanguage.FindStringSubmatch(line)
	if match == nil {
		return ""
	}
	code := strings.ToLower(strings.Split(match[1], "-")[0])
	aliases := map[string]string{
		"jpn": "ja", "eng": "en", "deu": "de", "ger": "de",
		"spa": "es", "fra": "fr", "fre": "fr", "hin": "hi",
		"ita": "it", "por": "pt", "tam": "ta", "tel": "te",
		"kor": "ko", "zho": "zh", "chi": "zh", "rus": "ru",
	}
	if short, ok := aliases[code]; ok {
		code = short
	}
	if len(code) != 2 || !watchAudioLanguage(code) {
		return ""
	}
	return code
}

// hlsAvailableAudio reports only independently addressable renditions of the
// chosen video variant. A native HLS player receives one relay URL per code.
func hlsAvailableAudio(body string) []string {
	if !strings.Contains(body, "#EXT-X-STREAM-INF") {
		return nil
	}
	master := trimMaster(body, watchMaxHeight, false)
	seen := map[string]bool{}
	var languages []string
	for _, line := range strings.Split(master, "\n") {
		if !strings.HasPrefix(line, "#EXT-X-MEDIA:") || !strings.Contains(line, "TYPE=AUDIO") ||
			!hlsURIAttr.MatchString(line) {
			continue
		}
		language := hlsAudioLanguage(line)
		if language != "" && !seen[language] {
			seen[language] = true
			languages = append(languages, language)
		}
	}
	return languages
}

func pickVariant(vars []hlsVariant, maxHeight int) hlsVariant {
	sized := false
	for _, v := range vars {
		sized = sized || v.height > 0
	}
	sort.SliceStable(vars, func(a, b int) bool {
		if sized && vars[a].height != vars[b].height {
			return vars[a].height < vars[b].height
		}
		return vars[a].bandwidth < vars[b].bandwidth
	})
	best := vars[0]
	for _, v := range vars {
		if sized && v.height > 0 && v.height <= maxHeight || !sized && v.bandwidth <= 4_000_000 {
			best = v
		}
	}
	return best
}

func pickAudio(audio []string, group string, dub bool) string {
	var inGroup []string
	for _, a := range audio {
		if m := hlsGroupID.FindStringSubmatch(a); group == "" || m != nil && m[1] == group {
			inGroup = append(inGroup, a)
		}
	}
	if len(inGroup) == 0 {
		return ""
	}
	lang := func(prefix string) string {
		for _, a := range inGroup {
			if m := hlsLanguage.FindStringSubmatch(a); m != nil && strings.HasPrefix(strings.ToLower(m[1]), prefix) {
				return a
			}
		}
		return ""
	}
	if dub {
		if a := lang("en"); a != "" {
			return a
		}
	} else if a := lang("ja"); a != "" {
		return a
	}
	for _, a := range inGroup {
		if strings.Contains(a, "DEFAULT=YES") {
			return a
		}
	}
	return inGroup[0]
}

// ── Segment cache ──

type segEntry struct {
	key  string
	data []byte
	typ  string
}

// segCache keeps recent segments in memory, least recently used out first,
// and makes viewers asking for the same segment at once share one fetch.
type segCache struct {
	mu      sync.Mutex
	max     int64
	size    int64
	order   *list.List
	items   map[string]*list.Element
	pending map[string]*segCall
}

type segCall struct {
	done chan struct{}
	data []byte
	typ  string
	err  error
}

func newSegCache(max int64) *segCache {
	return &segCache{max: max, order: list.New(), items: map[string]*list.Element{}, pending: map[string]*segCall{}}
}

// get answers from the cache or runs load once for everyone waiting.
func (c *segCache) get(key string, load func() ([]byte, string, error)) ([]byte, string, error) {
	c.mu.Lock()
	if el, ok := c.items[key]; ok {
		c.order.MoveToFront(el)
		e := el.Value.(*segEntry)
		c.mu.Unlock()
		return e.data, e.typ, nil
	}
	if call, ok := c.pending[key]; ok {
		c.mu.Unlock()
		<-call.done
		return call.data, call.typ, call.err
	}
	call := &segCall{done: make(chan struct{})}
	c.pending[key] = call
	c.mu.Unlock()

	call.data, call.typ, call.err = load()
	c.mu.Lock()
	delete(c.pending, key)
	if call.err == nil {
		c.items[key] = c.order.PushFront(&segEntry{key: key, data: call.data, typ: call.typ})
		c.size += int64(len(call.data))
		for c.size > c.max && c.order.Len() > 1 {
			old := c.order.Back()
			e := old.Value.(*segEntry)
			c.order.Remove(old)
			delete(c.items, e.key)
			c.size -= int64(len(e.data))
		}
	}
	c.mu.Unlock()
	close(call.done)
	return call.data, call.typ, call.err
}

// ── Serving ──

// serveMedia answers one relay request.
func (m *watchMedia) serve(w http.ResponseWriter, r *http.Request, id string, cache *segCache) {
	u, kind, ok := m.lookup(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	switch kind {
	case "list":
		load := func() ([]byte, string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), watchUpstreamWait)
			defer cancel()
			body, _, err := m.fetch(ctx, u, 4<<20)
			if err != nil {
				return nil, "", err
			}
			if m.link.FlixCloudKey != "" {
				body, err = flixDecodeManifest(body, m.link.FlixCloudKey)
				if err != nil {
					return nil, "", err
				}
			}
			base, _ := url.Parse(u)
			return []byte(m.rewritePlaylist(string(body), base)), "application/vnd.apple.mpegurl", nil
		}
		var data []byte
		var err error
		if strings.HasPrefix(u, "http://127.0.0.1:") {
			w.Header().Set("Cache-Control", "no-store")
			data, _, err = load()
		} else {
			data, _, err = cache.get(m.key+" "+u, load)
		}
		m.write(w, r, data, "application/vnd.apple.mpegurl", err)
	case "seg":
		data, typ, err := m.segment(u, cache)
		if errors.Is(err, errTooBig) {
			if m.link.FlixCloudKey != "" {
				http.Error(w, "video segment too large", http.StatusBadGateway)
			} else {
				m.stream(w, r, u)
			}
			return
		}
		if err == nil {
			go m.prefetch(id, cache)
		}
		m.write(w, r, data, typ, err)
	case "sub":
		load := func() ([]byte, string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), watchUpstreamWait)
			defer cancel()
			body, _, err := m.fetch(ctx, u, 4<<20)
			if err != nil {
				return nil, "", err
			}
			return []byte(toWebVTT(string(body))), "text/vtt", nil
		}
		var data []byte
		var err error
		if strings.HasPrefix(u, "http://127.0.0.1:") {
			// The torrent helper's subtitles grow while the episode
			// downloads; the player fetches them again for new lines.
			w.Header().Set("Cache-Control", "no-store")
			data, _, err = load()
		} else {
			data, _, err = cache.get(m.key+" "+u, load)
		}
		m.write(w, r, data, "text/vtt; charset=utf-8", err)
	default:
		m.stream(w, r, u)
	}
}

func (m *watchMedia) segment(u string, cache *segCache) ([]byte, string, error) {
	key := u
	if strings.HasPrefix(u, "http://127.0.0.1:") || m.link.FlixCloudKey != "" {
		key = m.key + " " + u
	}
	return cache.get(key, func() ([]byte, string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), watchUpstreamWait)
		defer cancel()
		data, typ, err := m.fetch(ctx, u, watchCacheItem)
		if err != nil {
			return nil, "", err
		}
		if m.link.FlixCloudKey != "" {
			data, err = flixDecodeSegment(data)
			if err != nil {
				return nil, "", err
			}
		}
		data, typ = unmaskSegment(data, typ)
		return data, typ, nil
	})
}

// unmaskSegment undoes a trick some hosts play: video segments served as
// pictures, sometimes behind a real picture's first bytes, so they pass for
// images. A transport stream is found by its sync byte every 188 bytes.
func unmaskSegment(data []byte, typ string) ([]byte, string) {
	isTS := func(d []byte) bool {
		return len(d) >= 3*188 && d[0] == 0x47 && d[188] == 0x47 && d[376] == 0x47
	}
	switch {
	case isTS(data):
		return data, "video/mp2t"
	case len(data) > 8 && string(data[4:8]) == "ftyp":
		return data, "video/mp4"
	case !strings.HasPrefix(typ, "image/"):
		return data, typ
	}
	for k := 0; k < len(data) && k < 64<<10; k++ {
		if data[k] == 0x47 && isTS(data[k:]) {
			return data[k:], "video/mp2t"
		}
	}
	return data, typ
}

// prefetch warms the segments after id, so the next ones are in by the
// time anyone asks.
func (m *watchMedia) prefetch(id string, cache *segCache) {
	m.mu.Lock()
	next := m.next[id]
	m.mu.Unlock()
	for _, n := range next {
		if u, _, ok := m.lookup(n); ok {
			if _, _, err := m.segment(u, cache); err != nil {
				return
			}
		}
	}
}

func (m *watchMedia) write(w http.ResponseWriter, r *http.Request, data []byte, typ string, err error) {
	if err != nil {
		log.Printf("watch relay: %v", err)
		http.Error(w, "the source didn't answer", http.StatusBadGateway)
		return
	}
	if typ == "" || strings.HasPrefix(typ, "text/html") || strings.HasPrefix(typ, "application/octet-stream") {
		typ = "video/mp2t"
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "private, max-age=3600")
	}
	if r.Method == http.MethodHead {
		return
	}
	n, _ := w.Write(data)
	m.out.Add(int64(n))
}

// stream passes a big file through, ranges and all, without caching it.
func (m *watchMedia) stream(w http.ResponseWriter, r *http.Request, u string) {
	req, err := m.request(r.Context(), u)
	if err != nil {
		http.Error(w, "bad address", http.StatusBadGateway)
		return
	}
	if rg := r.Header.Get("Range"); rg != "" {
		req.Header.Set("Range", rg)
	}
	client := *watchUpstream
	client.Timeout = 0 // a whole episode can take a while; the viewer leaving cancels it
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "the source didn't answer", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		http.Error(w, "the source didn't answer", http.StatusBadGateway)
		return
	}
	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	if t := w.Header().Get("Content-Type"); t == "" || strings.HasPrefix(t, "application/octet-stream") {
		w.Header().Set("Content-Type", "video/mp4")
	}
	w.WriteHeader(resp.StatusCode)
	n, _ := io.Copy(w, resp.Body)
	m.in.Add(n)
	m.out.Add(n)
}

// ── Subtitles ──

var (
	srtTime = regexp.MustCompile(`(\d{1,2}:\d{2}:\d{2}),(\d{3})`)
	assTags = regexp.MustCompile(`\{[^}]*\}`)
)

// toWebVTT turns SRT or ASS into WebVTT; WebVTT passes as it is.
func toWebVTT(body string) string {
	body = strings.TrimPrefix(strings.ReplaceAll(body, "\r\n", "\n"), "\ufeff")
	switch {
	case strings.HasPrefix(strings.TrimSpace(body), "WEBVTT"):
		return body
	case strings.Contains(body, "[Events]") || strings.Contains(body, "Dialogue:"):
		return assToVTT(body)
	}
	return "WEBVTT\n\n" + srtTime.ReplaceAllString(body, "$1.$2")
}

func assToVTT(body string) string {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	fields := []string{"Layer", "Start", "End", "Style", "Name", "MarginL", "MarginR", "MarginV", "Effect", "Text"}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if f, ok := strings.CutPrefix(line, "Format:"); ok && strings.Contains(f, "Text") {
			fields = strings.Split(f, ",")
			for k := range fields {
				fields[k] = strings.TrimSpace(fields[k])
			}
			continue
		}
		rest, ok := strings.CutPrefix(line, "Dialogue:")
		if !ok {
			continue
		}
		parts := strings.SplitN(strings.TrimSpace(rest), ",", len(fields))
		if len(parts) < len(fields) {
			continue
		}
		var start, end, text string
		for k, name := range fields {
			switch name {
			case "Start":
				start = assTime(parts[k])
			case "End":
				end = assTime(parts[k])
			case "Text":
				text = parts[k]
			}
		}
		text = strings.NewReplacer(`\N`, "\n", `\n`, "\n", `\h`, " ").Replace(assTags.ReplaceAllString(text, ""))
		if start == "" || end == "" || strings.TrimSpace(text) == "" {
			continue
		}
		fmt.Fprintf(&b, "%s --> %s\n%s\n\n", start, end, strings.TrimSpace(text))
	}
	return b.String()
}

// assTime turns ASS's h:mm:ss.cc into WebVTT's hh:mm:ss.mmm.
func assTime(t string) string {
	h, rest, ok1 := strings.Cut(strings.TrimSpace(t), ":")
	mm, rest, ok2 := strings.Cut(rest, ":")
	ss, cs, ok3 := strings.Cut(rest, ".")
	hn, e1 := strconv.Atoi(h)
	mn, e2 := strconv.Atoi(mm)
	sn, e3 := strconv.Atoi(ss)
	cn, e4 := strconv.Atoi((cs + "00")[:2])
	if !ok1 || !ok2 || !ok3 || e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		return ""
	}
	return fmt.Sprintf("%02d:%02d:%02d.%03d", hn, mn, sn, cn*10)
}

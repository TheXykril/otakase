package reanime

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"golang.org/x/crypto/pbkdf2"

	"github.com/thexykril/otakase/internal/providerhost"
	"github.com/thexykril/otakase/internal/providers"
)

const (
	siteURL   = "https://reanime.to"
	flixURL   = "https://flixcloud.cc"
	userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

var slugPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,180}$`)

type apiClient struct {
	base string
	flix string
	http *http.Client

	mu           sync.Mutex
	anilistIDs   map[string]int
	resolveEmbed func(string) (resolved, error) // injectable in unit tests
}

func newClient() *apiClient {
	client := http.DefaultClient
	if providerhost.HTTPClient != nil {
		client = providerhost.HTTPClient()
	}
	return &apiClient{base: siteURL, flix: flixURL, http: client}
}

type statusError struct {
	code int
}

func (e statusError) Error() string { return fmt.Sprintf("reanime: HTTP %d", e.code) }

func (c *apiClient) get(rawURL, referer string, maxBytes int64) ([]byte, error) {
	return c.getWithContext(context.Background(), rawURL, referer, maxBytes)
}

func (c *apiClient) getWithContext(parent context.Context, rawURL, referer string, maxBytes int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("reanime: request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json, text/html;q=0.9, */*;q=0.8")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reanime: fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, statusError{code: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reanime: read: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, errors.New("reanime: oversized response")
	}
	return body, nil
}

func (c *apiClient) json(path string, target any) error {
	body, err := c.get(c.base+path, siteURL+"/", 4<<20)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("reanime: invalid API response: %w", err)
	}
	return nil
}

type show struct {
	AnimeID   string          `json:"anime_id"`
	ID        string          `json:"id"`
	Title     json.RawMessage `json:"title"`
	Cover     json.RawMessage `json:"cover_image"`
	Episodes  int             `json:"episodes"`
	Subbed    int             `json:"subbed"`
	Dubbed    int             `json:"dubbed"`
	MalID     int             `json:"mal_id"`
	AniListID int             `json:"anilist_id"`
}

func (s show) slug() string {
	if s.AnimeID != "" {
		return s.AnimeID
	}
	return s.ID
}

func (s show) title() string {
	var label string
	if json.Unmarshal(s.Title, &label) == nil {
		return label
	}
	var names map[string]string
	if json.Unmarshal(s.Title, &names) == nil {
		for _, key := range []string{"english", "romaji", "native"} {
			if names[key] != "" {
				return names[key]
			}
		}
	}
	return ""
}

func (s show) thumbnail() string {
	var cover string
	if json.Unmarshal(s.Cover, &cover) == nil {
		return cover
	}
	var sizes map[string]string
	if json.Unmarshal(s.Cover, &sizes) == nil {
		for _, key := range []string{"large", "extra_large", "medium"} {
			if sizes[key] != "" {
				return sizes[key]
			}
		}
	}
	return ""
}

func (c *apiClient) search(query string) ([]show, error) {
	path := "/api/v1/search?q=" + url.QueryEscape(query) + "&limit=20"
	var response struct {
		Results []show `json:"results"`
		Data    []show `json:"data"`
	}
	err := c.json(path, &response)
	var status statusError
	if errors.As(err, &status) && status.code == http.StatusNotFound {
		err = c.json("/api/search?q="+url.QueryEscape(query)+"&limit=20", &response)
	}
	if err != nil {
		return nil, err
	}
	results := response.Data
	if response.Results != nil {
		results = response.Results
	}
	for _, entry := range results {
		c.rememberAniListID(entry.slug(), entry.AniListID)
	}
	return results, nil
}

// rememberAniListID reuses the exact id from a search result for later
// episode requests, keeping only a bounded set in long-running clients.
func (c *apiClient) rememberAniListID(slug string, id int) {
	if !slugPattern.MatchString(slug) || id <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.anilistIDs == nil || len(c.anilistIDs) >= 256 && c.anilistIDs[slug] == 0 {
		c.anilistIDs = make(map[string]int)
	}
	c.anilistIDs[slug] = id
}

func (c *apiClient) animeID(slug string) (int, error) {
	c.mu.Lock()
	cached := c.anilistIDs[slug]
	c.mu.Unlock()
	if cached > 0 {
		return cached, nil
	}
	var detail show
	if err := c.json("/api/v1/anime/"+url.PathEscape(slug), &detail); err != nil {
		return 0, fmt.Errorf("reanime: anime details for %s: %w", slug, err)
	}
	if detail.AniListID <= 0 {
		return 0, fmt.Errorf("reanime: no AniList id for %s", slug)
	}
	c.rememberAniListID(slug, detail.AniListID)
	return detail.AniListID, nil
}

type episode struct {
	Number int `json:"episode_number"`
}

func (c *apiClient) episodes(slug string) ([]episode, error) {
	if !slugPattern.MatchString(slug) {
		return nil, errors.New("reanime: invalid show id")
	}
	path := "/api/v1/anime/" + url.PathEscape(slug) + "/episodes?limit=2000"
	body, err := c.get(c.base+path, siteURL+"/", 4<<20)
	var status statusError
	if errors.As(err, &status) && status.code == http.StatusNotFound {
		body, err = c.get(c.base+"/api/episodes/"+url.PathEscape(slug), siteURL+"/", 4<<20)
	}
	if err != nil {
		return nil, err
	}
	var list []episode
	if err := json.Unmarshal(body, &list); err == nil {
		return list, nil
	}
	var response struct {
		Data     []episode `json:"data"`
		Episodes []episode `json:"episodes"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("reanime: invalid episode listing: %w", err)
	}
	if response.Data != nil {
		return response.Data, nil
	}
	return response.Episodes, nil
}

type server struct {
	ID            string `json:"$id"`
	Name          string `json:"serverName"`
	Link          string `json:"dataLink"`
	DataType      string `json:"dataType"`
	SharedWithSub bool   `json:"-"`
}

type watchResponse struct {
	Links []server `json:"episode_links"`
}

// servers asks FlixCloud first using the anime's AniList id. The old watch
// endpoint is optional and only consulted if FlixCloud has no usable server
// for the requested audio.
func (c *apiClient) servers(slug string, ep int, mode string) ([]server, error) {
	if !slugPattern.MatchString(slug) || ep <= 0 {
		return nil, errors.New("reanime: invalid episode")
	}
	var links []server
	id, flixErr := c.animeID(slug)
	if flixErr == nil {
		var response struct {
			Success bool     `json:"success"`
			Servers []server `json:"servers"`
		}
		flixErr = c.json(fmt.Sprintf("/api/flix/%d/%d", id, ep), &response)
		if flixErr == nil {
			if response.Success {
				links = response.Servers
			} else {
				flixErr = errors.New("reanime: FlixCloud server list unavailable")
			}
		}
	}
	if found := selectServers(links, mode); len(found) > 0 {
		return found, nil
	}

	// This route may be 404. It must never prevent an already found
	// FlixCloud server from being used.
	var watch watchResponse
	if err := c.json(fmt.Sprintf("/api/watch/%s/%d", url.PathEscape(slug), ep), &watch); err == nil {
		if found := selectServers(watch.Links, mode); len(found) > 0 {
			return found, nil
		}
	}
	if flixErr != nil {
		return nil, fmt.Errorf("reanime: no %s server for %s episode %d: %w", mode, slug, ep, flixErr)
	}
	return nil, fmt.Errorf("reanime: no %s server for %s episode %d", mode, slug, ep)
}

func selectServers(links []server, mode string) []server {
	subLinks := make(map[string]bool)
	for _, s := range links {
		t := strings.ToLower(s.DataType)
		if t == "sub" || t == "s-sub" {
			subLinks[s.Link] = true
		}
	}
	var out []server
	seen := make(map[string]bool)
	for _, s := range links {
		t := strings.ToLower(s.DataType)
		if (mode == "dub" && t != "dub" && t != "s-dub") ||
			(mode != "dub" && t != "sub" && t != "s-sub") {
			continue
		}
		u, err := url.Parse(s.Link)
		if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "flixcloud.cc") ||
			!strings.HasPrefix(u.Path, "/e/") || seen[s.Link] {
			continue
		}
		seen[s.Link] = true
		s.SharedWithSub = mode == "dub" && subLinks[s.Link]
		out = append(out, s)
	}
	// HD-2 is preferred to HD-1, which remains a fallback.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && serverRank(out[j].Name) < serverRank(out[j-1].Name); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func serverRank(name string) int {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "HD-2":
		return 0
	case "HD-1":
		return 1
	default:
		return 2
	}
}

type chapter struct {
	Start float64
	End   float64
}

type resolved struct {
	URL         string
	ManifestKey string
	Subtitles   []providers.SubtitleTrack
	Subtitle    string
	Intro       chapter
	Outro       chapter
}

func (c *apiClient) stream(link string) (resolved, error) {
	if c.resolveEmbed != nil {
		return c.resolveEmbed(link)
	}
	return c.decrypt(link)
}

// verifyEnglishAudio requires a shared sub/dub embed's HLS master to
// advertise English audio on a referenced variant's AUDIO group. A server
// label alone does not establish that a dub actually exists.
func (c *apiClient) verifyEnglishAudio(stream resolved) error {
	u, err := url.Parse(stream.URL)
	if err != nil || u.Scheme != "https" || !flixHost(u.Hostname()) {
		return errors.New("reanime: invalid HLS stream host")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	body, err := c.getWithContext(ctx, stream.URL, flixURL+"/", 1<<20)
	if err != nil {
		return fmt.Errorf("reanime: checking English audio: %w", err)
	}
	playlist, err := DecodeManifest(body, stream.ManifestKey)
	if err != nil {
		return fmt.Errorf("reanime: checking English audio: %w", err)
	}
	if !playlistHasEnglishAudio(playlist) {
		return errors.New("reanime: shared sub/dub stream has no confirmed English audio rendition")
	}
	return nil
}

func playlistHasEnglishAudio(playlist []byte) bool {
	englishGroups := make(map[string]bool)
	activeGroups := make(map[string]bool)
	for _, line := range strings.Split(string(playlist), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "#EXT-X-MEDIA:"):
			attrs := hlsAttributes(strings.TrimPrefix(line, "#EXT-X-MEDIA:"))
			lang := strings.ToLower(strings.TrimSpace(attrs["LANGUAGE"]))
			name := strings.ToLower(strings.TrimSpace(attrs["NAME"]))
			if strings.EqualFold(attrs["TYPE"], "AUDIO") &&
				(lang == "en" || lang == "eng" || strings.HasPrefix(lang, "en-") ||
					strings.HasPrefix(lang, "eng-") || strings.HasPrefix(name, "english")) {
				if group := attrs["GROUP-ID"]; group != "" {
					englishGroups[group] = true
				}
			}
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF:"):
			attrs := hlsAttributes(strings.TrimPrefix(line, "#EXT-X-STREAM-INF:"))
			if group := attrs["AUDIO"]; group != "" {
				activeGroups[group] = true
			}
		}
	}
	for group := range englishGroups {
		if activeGroups[group] {
			return true
		}
	}
	return false
}

// hlsAttributes does not split commas inside quoted values like CODECS.
func hlsAttributes(input string) map[string]string {
	out := make(map[string]string)
	add := func(field string) {
		key, value, ok := strings.Cut(strings.TrimSpace(field), "=")
		if ok {
			out[strings.ToUpper(strings.TrimSpace(key))] = strings.Trim(strings.TrimSpace(value), "\"")
		}
	}
	start, quoted := 0, false
	for i := 0; i < len(input); i++ {
		switch input[i] {
		case '"':
			quoted = !quoted
		case ',':
			if !quoted {
				add(input[start:i])
				start = i + 1
			}
		}
	}
	add(input[start:])
	return out
}

func (c *apiClient) decrypt(link string) (resolved, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "flixcloud.cc") ||
		!strings.HasPrefix(u.Path, "/e/") {
		return resolved{}, errors.New("reanime: invalid FlixCloud embed")
	}
	html, err := c.getWithContext(ctx, link, siteURL+"/", 4<<20)
	if err != nil {
		return resolved{}, err
	}
	ssr := extractSSR(html)
	if ssr == "" {
		return resolved{}, errors.New("reanime: FlixCloud embed has no player data")
	}
	seed := jsStringField(ssr, "obfuscation_seed")
	if seed == "" {
		return resolved{}, errors.New("reanime: missing player seed")
	}
	// FlixCloud also uses 16-character seeds. Only the first eight
	// characters form the WASM seed number; the full value keys PBKDF2.
	if len(seed) < 8 {
		return resolved{}, errors.New("reanime: invalid player seed")
	}
	seedNumber, err := strconv.ParseUint(seed[:8], 16, 32)
	if err != nil {
		return resolved{}, errors.New("reanime: invalid player seed")
	}
	first := seed
	for i := 0; i < 3; i++ {
		first = shaHex(first + strconv.Itoa(i))
	}
	second := first
	for i := 0; i < 3; i++ {
		second = shaHex(second + strconv.Itoa(i))
	}
	token := jsStringField(ssr, first[48:64]+"_"+first[56:64])
	fragment, err := base64.StdEncoding.DecodeString(jsStringField(ssr, "kf_"+first[8:16]))
	if err != nil {
		return resolved{}, fmt.Errorf("reanime: encrypted key: %w", err)
	}
	iv, err := base64.StdEncoding.DecodeString(jsStringField(ssr, "ivf_"+first[16:24]))
	if err != nil {
		return resolved{}, fmt.Errorf("reanime: initialization vector: %w", err)
	}
	fragment2, err := base64.StdEncoding.DecodeString(jsStringField(ssr, second[:16]+"_"+second[16:24]))
	if err != nil {
		return resolved{}, fmt.Errorf("reanime: second key: %w", err)
	}
	if token == "" || len(token) > 512 || len(fragment) == 0 || len(fragment2) != len(fragment) || len(iv) != aes.BlockSize {
		return resolved{}, errors.New("reanime: invalid player crypto fields")
	}
	payload, err := base64.StdEncoding.DecodeString(jsStringField(ssr, "w_payload"))
	if err != nil || len(payload) == 0 || len(payload) > 2<<20 {
		return resolved{}, errors.New("reanime: invalid player WASM")
	}
	body, err := c.getWithContext(ctx, c.flix+"/api/m3u8/"+url.PathEscape(token), siteURL+"/", 1<<20)
	if err != nil {
		return resolved{}, err
	}
	videoBytes, tokenBytes, err := decodeStreamTokens(body, token)
	if err != nil {
		return resolved{}, err
	}
	if len(tokenBytes) != len(fragment) {
		return resolved{}, errors.New("reanime: invalid stream key token")
	}
	keyMaterial, playlistKey, err := wasmTransform(ctx, payload, fragment, fragment2, tokenBytes, uint32(seedNumber))
	if err != nil {
		return resolved{}, fmt.Errorf("reanime: player transform: %w", err)
	}
	derived := pbkdf2.Key(keyMaterial, []byte(seed), 1000, 32, sha256.New)
	for i := range derived {
		derived[i] ^= seed[i%len(seed)]
	}
	aesKey := sha256.Sum256(derived)
	plain, err := decryptCBC(aesKey[:], iv, videoBytes)
	if err != nil {
		return resolved{}, fmt.Errorf("reanime: stream decrypt: %w", err)
	}
	stream := strings.TrimSpace(strings.TrimRight(string(plain), "\x00"))
	streamURL, err := url.Parse(stream)
	if err != nil || streamURL.Scheme != "https" || !flixHost(streamURL.Hostname()) {
		return resolved{}, errors.New("reanime: unexpected stream host")
	}
	tracks, selected := parseSubtitles(ssr)
	return resolved{
		URL:         stream,
		ManifestKey: base64.StdEncoding.EncodeToString(playlistKey),
		Subtitles:   tracks,
		Subtitle:    selected,
		Intro:       parseChapter(ssr, "intro_chapter"),
		Outro:       parseChapter(ssr, "outro_chapter"),
	}, nil
}

// decodeStreamTokens reads only the encrypted string values required for
// playback. The response also contains structured metadata, which must not
// be decoded as a string.
func decodeStreamTokens(body []byte, token string) ([]byte, []byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, nil, fmt.Errorf("reanime: invalid stream token response: %w", err)
	}
	decode := func(name string) ([]byte, error) {
		key := shaHex(token + name)[:10]
		var encoded string
		if err := json.Unmarshal(fields[key], &encoded); err != nil || encoded == "" {
			return nil, fmt.Errorf("reanime: invalid %s stream token field", name)
		}
		value, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(value) == 0 {
			return nil, fmt.Errorf("reanime: invalid %s stream token data", name)
		}
		return value, nil
	}

	video, err := decode("vid")
	if err != nil {
		return nil, nil, err
	}
	key, err := decode("key")
	if err != nil {
		return nil, nil, err
	}
	return video, key, nil
}

func flixHost(host string) bool {
	return strings.EqualFold(host, "flixcloud.cc") || strings.HasSuffix(strings.ToLower(host), ".flixcloud.cc")
}

func shaHex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func extractSSR(body []byte) string {
	needle := []byte(`{type:"data",data:{`)
	at := bytes.Index(body, needle)
	if at < 0 {
		return ""
	}
	start := at + len(needle) - 1
	depth := 0
	quoted, escaped := false, false
	var quote byte
	for i := start; i < len(body); i++ {
		b := body[i]
		if quoted {
			if escaped {
				escaped = false
			} else if b == '\\' {
				escaped = true
			} else if b == quote {
				quoted = false
			}
			continue
		}
		switch b {
		case '"', '\'':
			quoted, quote = true, b
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return string(body[start : i+1])
			}
		}
	}
	return ""
}

func jsStringField(data, name string) string {
	pattern := `(?:^|[,{])\s*["']?` + regexp.QuoteMeta(name) + `["']?\s*:\s*("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*')`
	re := regexp.MustCompile(pattern)
	found := re.FindStringSubmatch(data)
	if len(found) < 2 {
		return ""
	}
	if found[1][0] == '"' {
		value, err := strconv.Unquote(found[1])
		if err == nil {
			return value
		}
		return ""
	}
	return strings.ReplaceAll(strings.Trim(found[1], "'"), "\\'", "'")
}

func parseSubtitles(ssr string) ([]providers.SubtitleTrack, string) {
	match := regexp.MustCompile(`(?:^|[,{])\s*"?subtitles"?\s*:\s*\[`).FindStringIndex(ssr)
	if match == nil {
		return nil, ""
	}
	tail := ssr[match[1]:]
	if end := strings.Index(tail, "]"); end >= 0 {
		tail = tail[:end]
	}
	var tracks []providers.SubtitleTrack
	var preferred string
	for _, object := range regexp.MustCompile(`\{[^{}]*\}`).FindAllString(tail, -1) {
		link := jsStringField(object, "url")
		if link == "" {
			link = jsStringField(object, "file")
		}
		u, err := url.Parse(link)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			continue
		}
		language := jsStringField(object, "language")
		label := jsStringField(object, "label")
		if label == "" {
			label = jsStringField(object, "title")
		}
		if label == "" {
			label = language
		}
		tracks = append(tracks, providers.SubtitleTrack{URL: link, Language: language, Label: label})
		if strings.Contains(object, "default:!0") || strings.Contains(object, "default:true") {
			preferred = link
		}
	}
	if preferred == "" {
		for _, track := range tracks {
			if strings.Contains(strings.ToLower(track.Language+" "+track.Label), "english") || track.Language == "en" {
				preferred = track.URL
				break
			}
		}
	}
	return tracks, preferred
}

func parseChapter(ssr, name string) chapter {
	at := strings.Index(ssr, name+":{")
	if at < 0 {
		return chapter{}
	}
	value := ssr[at:]
	if end := strings.Index(value, "}"); end >= 0 {
		value = value[:end]
	}
	lookup := func(field string) float64 {
		re := regexp.MustCompile(`\b` + field + `\s*:\s*([0-9]+(?:\.[0-9]+)?)`)
		m := re.FindStringSubmatch(value)
		if len(m) == 2 {
			f, _ := strconv.ParseFloat(m[1], 64)
			return f
		}
		return 0
	}
	return chapter{Start: lookup("start"), End: lookup("end")}
}

func wasmTransform(parent context.Context, module, first, second, token []byte, seed uint32) ([]byte, []byte, error) {
	if len(first) == 0 || len(first) != len(second) || len(first) != len(token) {
		return nil, nil, errors.New("invalid transform inputs")
	}
	ctx, cancel := context.WithTimeout(parent, 6*time.Second)
	defer cancel()
	runtimeConfig := wazero.NewRuntimeConfig().WithCloseOnContextDone(true).WithMemoryLimitPages(512)
	runtime := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)
	defer runtime.Close(ctx)
	instance, err := runtime.Instantiate(ctx, module)
	if err != nil {
		return nil, nil, err
	}
	memory := instance.Memory()
	setSeed, run, playlistKey := instance.ExportedFunction("_s"), instance.ExportedFunction("_r"), instance.ExportedFunction("_c")
	if memory == nil || setSeed == nil || run == nil || playlistKey == nil {
		return nil, nil, errors.New("missing WASM exports")
	}
	size := uint32(len(first))
	offset := uint32(1000)
	if uint64(offset)+uint64(size)*4 > uint64(memory.Size()) {
		return nil, nil, errors.New("WASM memory too small")
	}
	if !memory.Write(offset, first) || !memory.Write(offset+size, second) || !memory.Write(offset+2*size, token) {
		return nil, nil, errors.New("WASM memory write failed")
	}
	if _, err := setSeed.Call(ctx, uint64(seed)); err != nil {
		return nil, nil, err
	}
	if _, err := run.Call(ctx, uint64(offset), uint64(offset+size), uint64(offset+2*size), uint64(offset+3*size), uint64(size)); err != nil {
		return nil, nil, err
	}
	result, ok := memory.Read(offset+3*size, size)
	if !ok {
		return nil, nil, errors.New("WASM memory read failed")
	}
	// Keep this output before _c runs; WASM exports may reuse their memory.
	material := bytes.Clone(result)
	keys, err := playlistKey.Call(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("WASM playlist key: %w", err)
	}
	if len(keys) != 1 || keys[0]+32 > uint64(memory.Size()) {
		return nil, nil, errors.New("WASM playlist key out of bounds")
	}
	key, ok := memory.Read(uint32(keys[0]), 32)
	if !ok {
		return nil, nil, errors.New("WASM playlist key read failed")
	}
	return material, bytes.Clone(key), nil
}

func decryptCBC(key, iv, encrypted []byte) ([]byte, error) {
	if len(key) != 32 || len(iv) != aes.BlockSize || len(encrypted) == 0 || len(encrypted)%aes.BlockSize != 0 {
		return nil, errors.New("invalid AES-CBC input")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(encrypted))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, encrypted)
	pad := int(plain[len(plain)-1])
	if pad < 1 || pad > aes.BlockSize || pad > len(plain) {
		return nil, errors.New("invalid PKCS#7 padding")
	}
	for _, value := range plain[len(plain)-pad:] {
		if int(value) != pad {
			return nil, errors.New("invalid PKCS#7 padding")
		}
	}
	return plain[:len(plain)-pad], nil
}

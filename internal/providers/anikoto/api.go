// Package anikoto scrapes the anikoto site (anikototv.to and its mirrors).
//
// The site is a common anime-site engine: a search, an episode list per show,
// a server list per episode and a server lookup that hands back a megaplay
// embed. The embed page names a megaplay id, and megaplay's getSources answers
// with the HLS manifest (AES-encrypted since late 2026) and the captions.
//
// Show keys are the site's watch slugs ("one-piece-odmau"). Older builds keyed
// this provider by AniList id through a third-party index that has since gone
// away; those all-digit keys are refused so the caller searches again rather
// than landing on whichever show the site happens to number the same.
package anikoto

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// defaultMirrors are tried in order. The site moves domains every so often;
// both of these serve the same catalogue and ids.
var defaultMirrors = []string{"https://anikototv.to", "https://anikoto.cz"}

const (
	// referer is what megaplay's CDN wants on every manifest and segment.
	referer        = "https://megaplay.buzz/"
	requestTimeout = 25 * time.Second
	userAgent      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

	// episodeCacheTTL keeps one episode listing for the length of a play: the
	// language check and the stream lookup both need it.
	episodeCacheTTL = 5 * time.Minute
)

// megaplay encrypts getSources' manifest URL with AES-CBC under the same key
// its player uses for its domain list. The key is padded with zeros to 32
// bytes and the IV to 16, as the player does.
const (
	megaplayKey = "i?LMTAx0Q6,:}50U"
	megaplayIV  = "W0;27ToaUpl_P%'c"
)

var (
	// Search results, from both the quick search and the filter page.
	quickItemRE  = regexp.MustCompile(`(?s)<a class="item" href="[^"]*/watch/([a-z0-9-]+)".*?class="name d-title"[^>]*>([^<]*)<.*?class="meta">(.*?)</div>`)
	metaDotRE    = regexp.MustCompile(`<span class="dot">([^<]*)</span>`)
	filterItemRE = regexp.MustCompile(`(?s)class="ani poster tip" data-tip="\d+">\s*<a href="[^"]*/watch/([a-z0-9-]+)/ep-1">.*?<div class="right">([^<]*)</div>.*?class="name d-title"[^>]*>([^<]*)</a>`)

	watchIDRE   = regexp.MustCompile(`id="watch-main"[^>]*data-id="(\d+)"`)
	episodeRE   = regexp.MustCompile(`<a [^>]*data-num="[^"]*"[^>]*>`)
	attrRE      = regexp.MustCompile(`data-([a-z-]+)="([^"]*)"`)
	serverTypRE = regexp.MustCompile(`(?s)<div class="type" data-type="(sub|dub|raw)">(.*?)</ul>`)
	serverRE    = regexp.MustCompile(`<li [^>]*data-link-id="([^"]+)"[^>]*>([^<]*)</li>`)
	playerIDRE  = regexp.MustCompile(`data-id="(\d+)"`)
	slugRE      = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

// searchResult is one show from a search.
type searchResult struct {
	Slug   string
	Name   string
	Format string
	Year   int
}

// episode is one row of the episode list. Sub and dub share a row; Servers is
// the opaque token the server list is asked with.
type episode struct {
	Number  int
	Sub     bool
	Dub     bool
	Servers string
}

func (e episode) has(category string) bool {
	if category == "dub" {
		return e.Dub
	}
	return e.Sub
}

// server is one entry of an episode's server list.
type server struct {
	Name   string
	LinkID string
}

type subtitle struct {
	File    string `json:"file"`
	Label   string `json:"label"`
	Kind    string `json:"kind"`
	Default bool   `json:"default"`
}

// caption is false for megaplay's thumbnail track and anything without a file.
func (s subtitle) caption() bool {
	kind := strings.TrimSpace(s.Kind)
	return strings.TrimSpace(s.File) != "" && (kind == "" || strings.EqualFold(kind, "captions") || strings.EqualFold(kind, "subtitles"))
}

// linkResponse is a resolved stream: the manifest, the page that must be sent
// as its referrer, its captions and the host's intro and outro.
type linkResponse struct {
	URL       string
	Referer   string
	Server    string
	Subtitles []subtitle
	Intro     []int
	Outro     []int
}

type cachedLink struct {
	at   time.Time
	link linkResponse
}

type cachedEpisodes struct {
	at       time.Time
	episodes []episode
}

type client struct {
	mirrors []string
	http    *http.Client

	mu       sync.Mutex
	current  int // index into mirrors of the last one that answered
	ids      map[string]string
	episodes map[string]cachedEpisodes
	links    map[string]cachedLink
}

func newClient() *client {
	return &client{
		mirrors: defaultMirrors,
		http:    &http.Client{Timeout: requestTimeout},
	}
}

// fetch GETs rawURL with a browser user-agent and the given referrer.
func (c *client) fetch(rawURL, refer string, ajax bool) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if refer != "" {
		req.Header.Set("Referer", refer)
	}
	if ajax {
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return body, nil
}

// site GETs a path from the first mirror that answers, starting with the one
// that answered last time.
func (c *client) site(path string, ajax bool) ([]byte, error) {
	c.mu.Lock()
	start := c.current
	c.mu.Unlock()

	var errs []string
	for i := range c.mirrors {
		index := (start + i) % len(c.mirrors)
		base := strings.TrimRight(c.mirrors[index], "/")
		body, err := c.fetch(base+path, base+"/", ajax)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", hostOf(base), err))
			continue
		}
		c.mu.Lock()
		c.current = index
		c.mu.Unlock()
		return body, nil
	}
	return nil, fmt.Errorf("anikoto %s: %s", strings.SplitN(path, "?", 2)[0], strings.Join(errs, "; "))
}

// ajax GETs one of the site's JSON endpoints and returns its result field.
func (c *client) ajax(path string, out any) error {
	body, err := c.site(path, true)
	if err != nil {
		return err
	}
	var envelope struct {
		Status  int             `json:"status"`
		Result  json.RawMessage `json:"result"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("anikoto %s: decoding response: %w", path, err)
	}
	if envelope.Status != http.StatusOK || len(envelope.Result) == 0 {
		msg := envelope.Message
		if msg == "" {
			msg = fmt.Sprintf("status %d", envelope.Status)
		}
		return fmt.Errorf("anikoto %s: %s", path, msg)
	}
	return json.Unmarshal(envelope.Result, out)
}

// search merges the quick search, which carries the year, with the first page
// of the filter, which goes past the quick search's five results.
func (c *client) search(query string) ([]searchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("anikoto: empty search query")
	}
	keyword := url.Values{"keyword": {query}}.Encode()

	var results []searchResult
	seen := map[string]bool{}

	var quick struct {
		HTML string `json:"html"`
	}
	quickErr := c.ajax("/ajax/anime/search?"+keyword, &quick)
	for _, m := range quickItemRE.FindAllStringSubmatch(quick.HTML, -1) {
		result := searchResult{Slug: m[1], Name: clean(m[2])}
		for _, dot := range metaDotRE.FindAllStringSubmatch(m[3], -1) {
			value := clean(dot[1])
			if year, err := strconv.Atoi(value); err == nil && year > 1900 {
				result.Year = year
			} else if value != "" && result.Format == "" {
				result.Format = value
			}
		}
		if result.Name != "" && !seen[result.Slug] {
			seen[result.Slug] = true
			results = append(results, result)
		}
	}

	page, filterErr := c.site("/filter?"+keyword, false)
	for _, m := range filterItemRE.FindAllSubmatch(page, -1) {
		slug := string(m[1])
		name := clean(string(m[3]))
		if name == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		results = append(results, searchResult{Slug: slug, Name: name, Format: clean(string(m[2]))})
	}

	if len(results) == 0 {
		if quickErr != nil {
			return nil, quickErr
		}
		if filterErr != nil {
			return nil, filterErr
		}
	}
	return results, nil
}

// showID is the site's numeric id for a slug, read off its watch page.
func (c *client) showID(slug string) (string, error) {
	if err := checkSlug(slug); err != nil {
		return "", err
	}
	c.mu.Lock()
	id, ok := c.ids[slug]
	c.mu.Unlock()
	if ok {
		return id, nil
	}
	page, err := c.site("/watch/"+slug, false)
	if err != nil {
		return "", err
	}
	m := watchIDRE.FindSubmatch(page)
	if m == nil {
		return "", fmt.Errorf("anikoto: no show id on the %s page", slug)
	}
	id = string(m[1])
	c.mu.Lock()
	if c.ids == nil {
		c.ids = map[string]string{}
	}
	c.ids[slug] = id
	c.mu.Unlock()
	return id, nil
}

func (c *client) episodeList(slug string) ([]episode, error) {
	c.mu.Lock()
	cached, ok := c.episodes[slug]
	c.mu.Unlock()
	if ok && time.Since(cached.at) < episodeCacheTTL {
		return cached.episodes, nil
	}

	id, err := c.showID(slug)
	if err != nil {
		return nil, err
	}
	var listing string
	if err := c.ajax("/ajax/episode/list/"+id, &listing); err != nil {
		return nil, err
	}
	episodes := parseEpisodes(listing)

	c.mu.Lock()
	if c.episodes == nil {
		c.episodes = map[string]cachedEpisodes{}
	}
	c.episodes[slug] = cachedEpisodes{at: time.Now(), episodes: episodes}
	c.mu.Unlock()
	return episodes, nil
}

// parseEpisodes reads the episode anchors. Episodes numbered other than by a
// whole number (recaps like 12.5) are left out: nothing here can ask for them.
func parseEpisodes(listing string) []episode {
	var out []episode
	for _, tag := range episodeRE.FindAllString(listing, -1) {
		attrs := map[string]string{}
		for _, m := range attrRE.FindAllStringSubmatch(tag, -1) {
			attrs[m[1]] = m[2]
		}
		number, err := strconv.Atoi(attrs["num"])
		if err != nil || number <= 0 || attrs["ids"] == "" {
			continue
		}
		out = append(out, episode{
			Number:  number,
			Sub:     attrs["sub"] == "1",
			Dub:     attrs["dub"] == "1",
			Servers: attrs["ids"],
		})
	}
	return out
}

func (c *client) findEpisode(slug string, number int) (episode, error) {
	episodes, err := c.episodeList(slug)
	if err != nil {
		return episode{}, err
	}
	for _, ep := range episodes {
		if ep.Number == number {
			return ep, nil
		}
	}
	return episode{}, fmt.Errorf("anikoto: %s has no episode %d", slug, number)
}

// servers lists an episode's servers in one language, in the site's order.
func (c *client) servers(ep episode, category string) ([]server, error) {
	var listing string
	if err := c.ajax("/ajax/server/list?servers="+url.QueryEscape(ep.Servers), &listing); err != nil {
		return nil, err
	}
	var out []server
	for _, group := range serverTypRE.FindAllStringSubmatch(listing, -1) {
		if group[1] != category {
			continue
		}
		for _, m := range serverRE.FindAllStringSubmatch(group[2], -1) {
			out = append(out, server{LinkID: m[1], Name: clean(m[2])})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("anikoto: no %s servers for episode %d", category, ep.Number)
	}
	return out, nil
}

// embed is what the site's server lookup answers.
type embed struct {
	URL      string `json:"url"`
	SkipData struct {
		Intro []int `json:"intro"`
		Outro []int `json:"outro"`
	} `json:"skip_data"`
}

// link resolves an episode in one language to a playable manifest, trying the
// site's servers in order until one gives a stream.
func (c *client) link(slug, category string, number int) (linkResponse, error) {
	key := slug + "/" + category + "/" + strconv.Itoa(number)
	c.mu.Lock()
	cached, ok := c.links[key]
	c.mu.Unlock()
	if ok && time.Since(cached.at) < episodeCacheTTL {
		return cached.link, nil
	}
	out, err := c.resolve(slug, category, number)
	if err != nil {
		return out, err
	}
	c.mu.Lock()
	if c.links == nil {
		c.links = map[string]cachedLink{}
	}
	c.links[key] = cachedLink{at: time.Now(), link: out}
	c.mu.Unlock()
	return out, nil
}

func (c *client) resolve(slug, category string, number int) (linkResponse, error) {
	ep, err := c.findEpisode(slug, number)
	if err != nil {
		return linkResponse{}, err
	}
	if !ep.has(category) {
		return linkResponse{}, fmt.Errorf("anikoto: %s has no %s for episode %d", slug, category, number)
	}
	list, err := c.servers(ep, category)
	if err != nil {
		return linkResponse{}, err
	}

	var errs []string
	for _, srv := range list {
		var e embed
		if err := c.ajax("/ajax/server?get="+url.QueryEscape(srv.LinkID), &e); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", srv.Name, err))
			continue
		}
		out, err := c.megaplay(e.URL)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", srv.Name, err))
			continue
		}
		out.Server = srv.Name
		if out.Intro == nil {
			out.Intro = e.SkipData.Intro
		}
		if out.Outro == nil {
			out.Outro = e.SkipData.Outro
		}
		return out, nil
	}
	return linkResponse{}, fmt.Errorf("anikoto: no playable server for %s %s episode %d: %s", slug, category, number, strings.Join(errs, "; "))
}

// megaplaySources is what megaplay's getSources answers. Sources.File is the
// older plain form; Enc replaced it.
type megaplaySources struct {
	Sources json.RawMessage `json:"sources"`
	Enc     string          `json:"enc"`
	Tracks  []subtitle      `json:"tracks"`
	Intro   *span           `json:"intro"`
	Outro   *span           `json:"outro"`
}

type span struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

func (s *span) pair() []int {
	if s == nil {
		return nil
	}
	return []int{s.Start, s.End}
}

// megaplay turns a megaplay embed URL into its manifest and captions.
func (c *client) megaplay(embedURL string) (linkResponse, error) {
	page, err := url.Parse(strings.TrimSpace(embedURL))
	if err != nil || page.Scheme == "" || page.Host == "" {
		return linkResponse{}, fmt.Errorf("bad embed url %q", embedURL)
	}
	origin := page.Scheme + "://" + page.Host

	body, err := c.fetch(page.String(), c.mirrorOrigin()+"/", false)
	if err != nil {
		return linkResponse{}, fmt.Errorf("megaplay page: %w", err)
	}
	id := playerIDRE.FindSubmatch(body)
	if id == nil {
		return linkResponse{}, errors.New("megaplay player id not found")
	}

	raw, err := c.fetchAjax(origin+"/stream/getSources?id="+string(id[1]), page.String())
	if err != nil {
		return linkResponse{}, fmt.Errorf("megaplay sources: %w", err)
	}
	var sources megaplaySources
	if err := json.Unmarshal(raw, &sources); err != nil {
		return linkResponse{}, fmt.Errorf("megaplay sources: %w", err)
	}
	file, err := sourceFile(sources)
	if err != nil {
		return linkResponse{}, err
	}
	return linkResponse{
		URL:       file,
		Referer:   origin + "/",
		Subtitles: sources.Tracks,
		Intro:     sources.Intro.pair(),
		Outro:     sources.Outro.pair(),
	}, nil
}

func (c *client) fetchAjax(rawURL, refer string) ([]byte, error) {
	return c.fetch(rawURL, refer, true)
}

func (c *client) mirrorOrigin() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.TrimRight(c.mirrors[c.current], "/")
}

// sourceFile reads the manifest URL out of getSources, encrypted or not.
func sourceFile(sources megaplaySources) (string, error) {
	if enc := strings.TrimSpace(sources.Enc); enc != "" {
		plain, err := decryptMegaplay(enc)
		if err != nil {
			return "", fmt.Errorf("megaplay sources: %w", err)
		}
		var payload struct {
			File string `json:"file"`
		}
		if err := json.Unmarshal(plain, &payload); err != nil {
			return "", fmt.Errorf("megaplay sources: decrypted payload: %w", err)
		}
		if file := strings.TrimSpace(payload.File); file != "" {
			return file, nil
		}
	}
	var plain struct {
		File string `json:"file"`
	}
	if len(sources.Sources) > 0 && json.Unmarshal(sources.Sources, &plain) == nil && strings.TrimSpace(plain.File) != "" {
		return strings.TrimSpace(plain.File), nil
	}
	return "", errors.New("megaplay stream url missing")
}

// decryptMegaplay undoes getSources' encryption: URL-safe base64, AES-CBC,
// PKCS#7 padding.
func decryptMegaplay(enc string) ([]byte, error) {
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(enc, "="))
	if err != nil {
		return nil, fmt.Errorf("decoding: %w", err)
	}
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext length %d", len(data))
	}
	block, err := aes.NewCipher(padKey(megaplayKey, 32))
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, padKey(megaplayIV, aes.BlockSize)).CryptBlocks(plain, data)

	pad := int(plain[len(plain)-1])
	if pad == 0 || pad > aes.BlockSize || pad > len(plain) {
		return nil, errors.New("bad padding, the key has probably changed")
	}
	for _, b := range plain[len(plain)-pad:] {
		if int(b) != pad {
			return nil, errors.New("bad padding, the key has probably changed")
		}
	}
	return plain[:len(plain)-pad], nil
}

func padKey(key string, size int) []byte {
	out := make([]byte, size)
	copy(out, key)
	return out
}

// checkSlug refuses anything that is not a watch slug, including the AniList
// ids older builds stored for this provider.
func checkSlug(slug string) error {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return fmt.Errorf("anikoto: empty show id")
	}
	if _, err := strconv.Atoi(slug); err == nil {
		return fmt.Errorf("anikoto: %q is an id from the old anikoto index; search again", slug)
	}
	if !slugRE.MatchString(slug) {
		return fmt.Errorf("anikoto: %q is not a show id", slug)
	}
	return nil
}

func clean(s string) string {
	return strings.TrimSpace(html.UnescapeString(s))
}

func hostOf(base string) string {
	if u, err := url.Parse(base); err == nil && u.Host != "" {
		return u.Host
	}
	return base
}

// normalizeCategory maps this program's language names onto the host's. Anything
// unrecognised becomes sub, which is the one every show has.
func normalizeCategory(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), "dub") {
		return "dub"
	}
	return "sub"
}

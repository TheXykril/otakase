// Package animex plays episodes through animex.one's player API.
//
// animex keys its shows by AniList id: animex.one/anime/{anilistId} names the
// show's slug, and pp.animex.one answers, for a slug and an episode number,
// which of its servers carry the sub and the dub (/rest/api/servers) and each
// server's HLS stream, caption tracks, intro and outro, and the headers the
// stream's host wants (/rest/api/sources). Search goes to AniList, as for
// megavid, so a result is always the show on the user's list.
//
// Only some servers are used; see servers.
package animex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/thexykril/otakase/internal/providerhost"
)

const (
	siteURL        = "https://animex.one"
	apiURL         = "https://pp.animex.one"
	anilistURL     = "https://graphql.anilist.co"
	siteReferer    = "https://animex.one/"
	requestTimeout = 30 * time.Second
	userAgent      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

	// cacheTTL keeps a slug and a resolved episode for the length of a play:
	// the episode list, the stream lookup and the skip range all need them.
	cacheTTL = 5 * time.Minute
)

// servers are the animex servers used, in the order they are tried.
//
// yuki carries sub and dub with soft subtitles in many languages; its streams
// sit on the CDN megaplay (and so anikoto) uses, but reached through animex
// rather than through megaplay's site. zuna is sub only, from a host none of
// the other providers use. nero relays megavid's copies, which the megavid
// provider already offers; loli and sora rarely answered when this was
// written.
var servers = []string{"yuki", "zuna"}

// show is one AniList search result.
type show struct {
	AnilistID int
	MalID     int
	Title     string
	Format    string
	Year      int
	Episodes  int
}

type track struct {
	URL     string `json:"url"`
	Lang    string `json:"lang"`
	Label   string `json:"label"`
	Kind    string `json:"kind"`
	Default bool   `json:"default"`
}

// caption reports whether a track is a subtitle. animex lists the seek-bar
// thumbnails as an unlabelled caption track too.
func (t track) caption() bool {
	u := strings.TrimSpace(t.URL)
	if u == "" || !strings.EqualFold(strings.TrimSpace(t.Kind), "captions") {
		return false
	}
	return !strings.Contains(strings.ToLower(u), "thumbnails")
}

type chapter struct {
	Title string  `json:"title"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// source is one server's answer for an episode.
type source struct {
	Server  string
	Stream  string
	Tracks  []track
	Chapter []chapter
	Headers map[string]string
}

type serverList struct {
	Sub []string
	Dub []string
}

type cached[T any] struct {
	at  time.Time
	val T
	err error
}

type client struct {
	site    string
	api     string
	anilist string
	http    *http.Client

	mu      sync.Mutex
	slugs   map[int]cached[string]
	servers map[string]cached[serverList]
	sources map[string]cached[source]
}

func newClient() *client {
	return &client{site: siteURL, api: apiURL, anilist: anilistURL, http: &http.Client{Timeout: requestTimeout}}
}

func (c *client) do(req *http.Request) ([]byte, int, error) {
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode != http.StatusOK && providerhost.IsRateLimitedBody(body)) {
		return nil, resp.StatusCode, providerhost.ErrRateLimited
	}
	return body, resp.StatusCode, nil
}

func (c *client) get(endpoint string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	body, status, err := c.do(req)
	if err != nil {
		return nil, err
	}
	// The API answers a missing show or episode with 404 and a JSON error,
	// which the caller reads.
	if status != http.StatusOK && status != http.StatusNotFound {
		return nil, providerhost.HTTPStatusError("animex request", status, body)
	}
	return body, nil
}

const searchQuery = `query ($search: String) {
  Page(perPage: 25) {
    media(search: $search, type: ANIME, isAdult: false, sort: SEARCH_MATCH) {
      id
      idMal
      title { romaji english }
      format
      seasonYear
      episodes
    }
  }
}`

const episodesQuery = `query ($id: Int) {
  Media(id: $id, type: ANIME) {
    episodes
    status
    nextAiringEpisode { episode }
  }
}`

func (c *client) graphql(query string, variables map[string]any, out any) error {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.anilist, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	body, _, err := c.do(req)
	if err != nil {
		return fmt.Errorf("animex: anilist: %w", err)
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
			Status  int    `json:"status"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("animex: anilist: decoding response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		if envelope.Errors[0].Status == http.StatusTooManyRequests {
			return providerhost.ErrRateLimited
		}
		return fmt.Errorf("animex: anilist: %s", envelope.Errors[0].Message)
	}
	return json.Unmarshal(envelope.Data, out)
}

// search finds shows on AniList.
func (c *client) search(query string) ([]show, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("animex: empty search query")
	}
	var out struct {
		Page struct {
			Media []struct {
				ID    int `json:"id"`
				IDMal int `json:"idMal"`
				Title struct {
					Romaji  string `json:"romaji"`
					English string `json:"english"`
				} `json:"title"`
				Format     string `json:"format"`
				SeasonYear int    `json:"seasonYear"`
				Episodes   int    `json:"episodes"`
			} `json:"media"`
		} `json:"Page"`
	}
	if err := c.graphql(searchQuery, map[string]any{"search": query}, &out); err != nil {
		return nil, err
	}
	var shows []show
	for _, m := range out.Page.Media {
		title := strings.TrimSpace(m.Title.English)
		if title == "" {
			title = strings.TrimSpace(m.Title.Romaji)
		}
		if m.ID <= 0 || title == "" {
			continue
		}
		shows = append(shows, show{AnilistID: m.ID, MalID: m.IDMal, Title: title, Format: m.Format, Year: m.SeasonYear, Episodes: m.Episodes})
	}
	return shows, nil
}

// episodeCount is how many episodes have aired: the next one to air minus
// one while a show is running, else its total.
func (c *client) episodeCount(anilistID int) (int, error) {
	var out struct {
		Media *struct {
			Episodes          int    `json:"episodes"`
			Status            string `json:"status"`
			NextAiringEpisode *struct {
				Episode int `json:"episode"`
			} `json:"nextAiringEpisode"`
		} `json:"Media"`
	}
	if err := c.graphql(episodesQuery, map[string]any{"id": anilistID}, &out); err != nil {
		return 0, err
	}
	if out.Media == nil {
		return 0, fmt.Errorf("animex: no show with AniList id %d", anilistID)
	}
	if next := out.Media.NextAiringEpisode; next != nil && next.Episode > 0 {
		return next.Episode - 1, nil
	}
	if out.Media.Episodes > 0 && out.Media.Status != "NOT_YET_RELEASED" {
		return out.Media.Episodes, nil
	}
	return 0, nil
}

// slug finds animex's name for a show. The show page's data (SvelteKit's
// __data.json) is a flat array in which objects point at their values by
// index; the page's root names animeInfo, which names slug.
func (c *client) slug(anilistID int) (string, error) {
	c.mu.Lock()
	hit, ok := c.slugs[anilistID]
	c.mu.Unlock()
	if ok && time.Since(hit.at) < cacheTTL {
		return hit.val, hit.err
	}
	slug, err := c.fetchSlug(anilistID)
	c.mu.Lock()
	if c.slugs == nil {
		c.slugs = map[int]cached[string]{}
	}
	c.slugs[anilistID] = cached[string]{at: time.Now(), val: slug, err: err}
	c.mu.Unlock()
	return slug, err
}

func (c *client) fetchSlug(anilistID int) (string, error) {
	body, err := c.get(fmt.Sprintf("%s/anime/%d/__data.json", strings.TrimRight(c.site, "/"), anilistID))
	if err != nil {
		return "", fmt.Errorf("animex: show %d: %w", anilistID, err)
	}
	slug, err := slugFromData(body)
	if err != nil {
		return "", fmt.Errorf("animex: show %d: %w", anilistID, err)
	}
	return slug, nil
}

func slugFromData(body []byte) (string, error) {
	var page struct {
		Nodes []*struct {
			Type string            `json:"type"`
			Data []json.RawMessage `json:"data"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return "", fmt.Errorf("decoding page data: %w", err)
	}
	for _, node := range page.Nodes {
		if node == nil || node.Type != "data" || len(node.Data) == 0 {
			continue
		}
		var root map[string]json.RawMessage
		if json.Unmarshal(node.Data[0], &root) != nil {
			continue
		}
		info, ok := pointedObject(node.Data, root["animeInfo"])
		if !ok {
			continue
		}
		at, ok := index(info["slug"], len(node.Data))
		if !ok {
			continue
		}
		var slug string
		if json.Unmarshal(node.Data[at], &slug) == nil && strings.TrimSpace(slug) != "" {
			return strings.TrimSpace(slug), nil
		}
	}
	return "", fmt.Errorf("not on animex")
}

// index reads a pointer into the data array.
func index(raw json.RawMessage, n int) (int, bool) {
	var i int
	if json.Unmarshal(raw, &i) != nil || i < 0 || i >= n {
		return 0, false
	}
	return i, true
}

func pointedObject(data []json.RawMessage, raw json.RawMessage) (map[string]json.RawMessage, bool) {
	at, ok := index(raw, len(data))
	if !ok {
		return nil, false
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(data[at], &obj) != nil {
		return nil, false
	}
	return obj, true
}

// serverList asks which servers carry an episode.
func (c *client) serverList(slug string, episode int) (serverList, error) {
	key := fmt.Sprintf("%s/%d", slug, episode)
	c.mu.Lock()
	hit, ok := c.servers[key]
	c.mu.Unlock()
	if ok && time.Since(hit.at) < cacheTTL {
		return hit.val, hit.err
	}
	list, err := c.fetchServers(slug, episode)
	c.mu.Lock()
	if c.servers == nil {
		c.servers = map[string]cached[serverList]{}
	}
	c.servers[key] = cached[serverList]{at: time.Now(), val: list, err: err}
	c.mu.Unlock()
	return list, err
}

func (c *client) fetchServers(slug string, episode int) (serverList, error) {
	q := url.Values{"id": {slug}, "epNum": {strconv.Itoa(episode)}}
	body, err := c.get(strings.TrimRight(c.api, "/") + "/rest/api/servers?" + q.Encode())
	if err != nil {
		return serverList{}, fmt.Errorf("animex %s episode %d: %w", slug, episode, err)
	}
	type server struct {
		ID string `json:"id"`
	}
	var out struct {
		Error string   `json:"error"`
		Sub   []server `json:"subProviders"`
		Dub   []server `json:"dubProviders"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return serverList{}, fmt.Errorf("animex %s episode %d: decoding servers: %w", slug, episode, err)
	}
	if out.Error != "" {
		return serverList{}, fmt.Errorf("animex %s episode %d: %s", slug, episode, out.Error)
	}
	var list serverList
	for _, s := range out.Sub {
		list.Sub = append(list.Sub, strings.ToLower(strings.TrimSpace(s.ID)))
	}
	for _, s := range out.Dub {
		list.Dub = append(list.Dub, strings.ToLower(strings.TrimSpace(s.ID)))
	}
	return list, nil
}

// source asks one server for an episode in one language.
func (c *client) source(slug string, episode int, lang, server string) (source, error) {
	key := fmt.Sprintf("%s/%d/%s/%s", slug, episode, lang, server)
	c.mu.Lock()
	hit, ok := c.sources[key]
	c.mu.Unlock()
	if ok && time.Since(hit.at) < cacheTTL {
		return hit.val, hit.err
	}
	src, err := c.fetchSource(slug, episode, lang, server)
	c.mu.Lock()
	if c.sources == nil {
		c.sources = map[string]cached[source]{}
	}
	c.sources[key] = cached[source]{at: time.Now(), val: src, err: err}
	c.mu.Unlock()
	return src, err
}

func (c *client) fetchSource(slug string, episode int, lang, server string) (source, error) {
	what := fmt.Sprintf("animex %s episode %d %s (%s)", slug, episode, lang, server)
	q := url.Values{"id": {slug}, "epNum": {strconv.Itoa(episode)}, "type": {lang}, "providerId": {server}}
	body, err := c.get(strings.TrimRight(c.api, "/") + "/rest/api/sources?" + q.Encode())
	if err != nil {
		return source{}, fmt.Errorf("%s: %w", what, err)
	}
	var out struct {
		Error   string `json:"error"`
		Sources []struct {
			URL  string `json:"url"`
			Type string `json:"type"`
		} `json:"sources"`
		Tracks   []track           `json:"tracks"`
		Chapters []chapter         `json:"chapters"`
		Headers  map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return source{}, fmt.Errorf("%s: decoding response: %w", what, err)
	}
	if out.Error != "" {
		return source{}, fmt.Errorf("%s: %s", what, out.Error)
	}
	for _, s := range out.Sources {
		u, err := url.Parse(strings.TrimSpace(s.URL))
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			continue
		}
		return source{Server: server, Stream: u.String(), Tracks: out.Tracks, Chapter: out.Chapters, Headers: out.Headers}, nil
	}
	return source{}, fmt.Errorf("%s: no stream", what)
}

// parseID reads a show key, which is an AniList id.
func parseID(id string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(id))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("animex: %q is not an AniList id", id)
	}
	return n, nil
}

// normalizeLang maps this program's language names onto animex's. Anything
// unrecognised becomes sub.
func normalizeLang(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), "dub") {
		return "dub"
	}
	return "sub"
}

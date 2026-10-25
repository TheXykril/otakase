// Package megavid plays episodes from megavid.buzz, the player animekai uses.
//
// megavid is addressed by MAL id, episode number and language
// (/mal/52991/1/sub), so there is no guessing which of a host's shows is the
// one on the user's list. Search goes to AniList, which is where the tracker
// already gets its MAL ids, and each result carries its MAL id so the app can
// match it to the list entry exactly.
//
// Each episode answers from /mal/{id}/{ep}/{lang}/source with JSON naming the
// HLS stream, the caption tracks and the intro and outro. megavid keeps two
// copies: its own (noax=1, on cp.megavid.buzz) and one relayed from other
// players (the default, on megavid.se). Both are offered, its own first.
package megavid

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
	baseURL        = "https://megavid.buzz"
	anilistURL     = "https://graphql.anilist.co"
	referer        = "https://megavid.buzz/"
	requestTimeout = 40 * time.Second // the source endpoint can take a while
	userAgent      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

	// sourceCacheTTL keeps a resolved episode for the length of a play: the
	// dub check, the stream lookup and the skip range all need it.
	sourceCacheTTL = 5 * time.Minute
)

// copies are the variants of the source request, in the order they are
// offered. megavid's own copy is a plain .m3u8 every player and the watch room
// recognise; the relayed copy is named master.txt.
var copies = []string{"noax=1", ""}

// show is one AniList search result.
type show struct {
	MalID    int
	Title    string
	Format   string
	Year     int
	Episodes int
}

type track struct {
	File    string `json:"file"`
	Label   string `json:"label"`
	Kind    string `json:"kind"`
	Default bool   `json:"default"`
}

func (t track) caption() bool {
	return strings.TrimSpace(t.File) != "" && strings.EqualFold(strings.TrimSpace(t.Kind), "captions")
}

type chapter struct {
	Title string  `json:"title"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// source is what /source answers.
type source struct {
	Status   string    `json:"status"`
	Message  string    `json:"message"`
	Source   string    `json:"source"`
	Tracks   []track   `json:"tracks"`
	Chapters []chapter `json:"chapters"`
}

type cachedSource struct {
	at  time.Time
	src source
	err error
}

type client struct {
	base    string
	anilist string
	http    *http.Client

	mu      sync.Mutex
	sources map[string]cachedSource
}

func newClient() *client {
	return &client{base: baseURL, anilist: anilistURL, http: &http.Client{Timeout: requestTimeout}}
}

func (c *client) do(req *http.Request) ([]byte, error) {
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode != http.StatusOK && providerhost.IsRateLimitedBody(body)) {
		return nil, providerhost.ErrRateLimited
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return nil, providerhost.HTTPStatusError("megavid request", resp.StatusCode, body)
	}
	return body, nil
}

const searchQuery = `query ($search: String) {
  Page(perPage: 25) {
    media(search: $search, type: ANIME, isAdult: false, sort: SEARCH_MATCH) {
      idMal
      title { romaji english }
      format
      seasonYear
      episodes
    }
  }
}`

const episodesQuery = `query ($idMal: Int) {
  Media(idMal: $idMal, type: ANIME) {
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
	body, err := c.do(req)
	if err != nil {
		return fmt.Errorf("megavid: anilist: %w", err)
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
			Status  int    `json:"status"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("megavid: anilist: decoding response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		if envelope.Errors[0].Status == http.StatusTooManyRequests {
			return providerhost.ErrRateLimited
		}
		return fmt.Errorf("megavid: anilist: %s", envelope.Errors[0].Message)
	}
	return json.Unmarshal(envelope.Data, out)
}

// search finds shows on AniList. Shows without a MAL id are dropped: megavid
// cannot be asked for them.
func (c *client) search(query string) ([]show, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("megavid: empty search query")
	}
	var out struct {
		Page struct {
			Media []struct {
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
		if m.IDMal <= 0 || title == "" {
			continue
		}
		shows = append(shows, show{MalID: m.IDMal, Title: title, Format: m.Format, Year: m.SeasonYear, Episodes: m.Episodes})
	}
	return shows, nil
}

// episodeCount is how many episodes have aired: the next one to air minus
// one while a show is running, else its total.
func (c *client) episodeCount(malID int) (int, error) {
	var out struct {
		Media *struct {
			Episodes          int    `json:"episodes"`
			Status            string `json:"status"`
			NextAiringEpisode *struct {
				Episode int `json:"episode"`
			} `json:"nextAiringEpisode"`
		} `json:"Media"`
	}
	if err := c.graphql(episodesQuery, map[string]any{"idMal": malID}, &out); err != nil {
		return 0, err
	}
	if out.Media == nil {
		return 0, fmt.Errorf("megavid: no show with MAL id %d", malID)
	}
	if next := out.Media.NextAiringEpisode; next != nil && next.Episode > 0 {
		return next.Episode - 1, nil
	}
	if out.Media.Episodes > 0 && out.Media.Status != "NOT_YET_RELEASED" {
		return out.Media.Episodes, nil
	}
	return 0, nil
}

// source asks megavid for one copy of an episode. A missing episode is an
// error, so is an answer with no stream in it.
func (c *client) source(malID, episode int, lang, copyParam string) (source, error) {
	key := fmt.Sprintf("%d/%d/%s?%s", malID, episode, lang, copyParam)
	c.mu.Lock()
	cached, ok := c.sources[key]
	c.mu.Unlock()
	if ok && time.Since(cached.at) < sourceCacheTTL {
		return cached.src, cached.err
	}

	src, err := c.fetchSource(malID, episode, lang, copyParam)

	c.mu.Lock()
	if c.sources == nil {
		c.sources = map[string]cachedSource{}
	}
	c.sources[key] = cachedSource{at: time.Now(), src: src, err: err}
	c.mu.Unlock()
	return src, err
}

func (c *client) fetchSource(malID, episode int, lang, copyParam string) (source, error) {
	page := fmt.Sprintf("%s/mal/%d/%d/%s", strings.TrimRight(c.base, "/"), malID, episode, lang)
	endpoint := page + "/source"
	if copyParam != "" {
		endpoint += "?" + copyParam
	}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return source{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", page)
	body, err := c.do(req)
	if err != nil {
		return source{}, fmt.Errorf("megavid %d episode %d %s: %w", malID, episode, lang, err)
	}
	var out source
	if err := json.Unmarshal(body, &out); err != nil {
		return source{}, fmt.Errorf("megavid %d episode %d %s: decoding response: %w", malID, episode, lang, err)
	}
	if out.Status != "ok" || strings.TrimSpace(out.Source) == "" {
		msg := out.Message
		if msg == "" {
			msg = "no stream"
		}
		return source{}, fmt.Errorf("megavid %d episode %d %s: %s", malID, episode, lang, msg)
	}
	if u, err := url.Parse(out.Source); err != nil || u.Scheme == "" || u.Host == "" {
		return source{}, fmt.Errorf("megavid %d episode %d %s: bad stream url %q", malID, episode, lang, out.Source)
	}
	return out, nil
}

// parseID reads a show key, which is a MAL id.
func parseID(id string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(id))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("megavid: %q is not a MAL id", id)
	}
	return n, nil
}

// normalizeLang maps this program's language names onto megavid's. Anything
// unrecognised becomes sub.
func normalizeLang(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), "dub") {
		return "dub"
	}
	return "sub"
}

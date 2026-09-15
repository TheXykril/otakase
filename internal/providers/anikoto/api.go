// Package anikoto talks to an AniList-backed anime index.
//
// It is unusual among the providers here in that its identifiers are AniList
// media ids, which is what the tracker already knows every entry by. Where
// every other provider has to guess which of its shows corresponds to the one
// on the user's list -- the guessing that this project has spent most of its
// history repairing -- this one can be addressed directly.
package anikoto

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://noob2.broggl.farm"
	referer        = "https://megaplay.buzz/"
	requestTimeout = 25 * time.Second
)

// searchResult is one row of GET /search?q=.
type searchResult struct {
	// ID is an AniList media id, not an identifier private to this host.
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Format string `json:"format"`
	Year   int    `json:"year"`
}

type searchResponse struct {
	Results []searchResult `json:"results"`
}

// episode is one row of GET /episodes/{anilistID}. Sub and dub are listed as
// separate entries for the same episode number, distinguished by Category.
type episode struct {
	ID       string `json:"id"`
	Number   int    `json:"number"`
	Category string `json:"category"`
	Title    string `json:"title"`
	Filler   bool   `json:"filler"`
}

// stream is one playable source from GET /link?id=.
type stream struct {
	Type        string            `json:"type"`
	URL         string            `json:"url"`
	Referer     string            `json:"referer"`
	HTTPHeaders map[string]string `json:"httpHeaders"`
	Quality     string            `json:"quality"`
	Server      string            `json:"server"`
	Priority    int               `json:"priority"`
	Default     bool              `json:"default"`
}

type subtitle struct {
	File     string `json:"file"`
	Label    string `json:"label"`
	Language string `json:"language"`
	Default  bool   `json:"default"`
}

// linkResponse also carries intro and outro as [start, end] second pairs, which
// is a per-episode skip range from the same request that resolves the stream.
type linkResponse struct {
	Streams   []stream   `json:"streams"`
	Subtitles []subtitle `json:"subtitles"`
	Intro     []int      `json:"intro"`
	Outro     []int      `json:"outro"`
}

type client struct {
	baseURL string
	http    *http.Client
}

func newClient() *client {
	return &client{
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: requestTimeout},
	}
}

func (c *client) get(path string, query url.Values, out any) error {
	endpoint := strings.TrimRight(c.baseURL, "/") + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Referer", referer)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("anikoto %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("anikoto %s: status %d", path, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("anikoto %s: decoding response: %w", path, err)
	}
	return nil
}

func (c *client) search(query string) ([]searchResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("anikoto: empty search query")
	}
	var out searchResponse
	if err := c.get("/search", url.Values{"q": {query}}, &out); err != nil {
		return nil, err
	}
	return out.Results, nil
}

func (c *client) episodes(anilistID string) ([]episode, error) {
	if strings.TrimSpace(anilistID) == "" {
		return nil, fmt.Errorf("anikoto: empty show id")
	}
	var out []episode
	if err := c.get("/episodes/"+url.PathEscape(anilistID), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *client) link(anilistID, category string, episodeNumber int) (linkResponse, error) {
	var out linkResponse
	id := episodeRef(anilistID, category, episodeNumber)
	if err := c.get("/link", url.Values{"id": {id}}, &out); err != nil {
		return out, err
	}
	if len(out.Streams) == 0 {
		return out, fmt.Errorf("anikoto: no streams for %s", id)
	}
	return out, nil
}

// episodeRef builds the identifier the link endpoint expects. The shape is
// fixed by the host, so it is written in one place rather than assembled at
// each call site.
func episodeRef(anilistID, category string, episodeNumber int) string {
	return "watch/anikoto/" + anilistID + "/" + category + "/" + strconv.Itoa(episodeNumber)
}

// normalizeCategory maps this program's language names onto the host's. Anything
// unrecognised becomes sub, which is the one every show has.
func normalizeCategory(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), "dub") {
		return "dub"
	}
	return "sub"
}

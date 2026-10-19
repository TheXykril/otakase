// Package kickassanime resolves episodes from KickassAnime (kaa.lt).
//
// It is the most conventional source otakase talks to: a documented-shaped JSON
// API with no anti-bot gate, no persisted-query handshake, and stream URLs that
// sit in the player page rather than behind an obfuscated endpoint. It also
// carries dubs, which most of the other hosts do not.
package kickassanime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/thexykril/otakase/internal/providerhost"
)

const (
	apiBase = "https://kaa.lt"
	// referer is required: the API answers 403 without it.
	referer   = "https://kaa.lt/"
	userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

// localeForMode maps otakase's sub/dub to the locales this host indexes by. A show
// lists the locales it actually has, so asking for a dub that does not exist
// fails cleanly rather than silently returning the subtitled version.
func localeForMode(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), "dub") {
		return "en-US"
	}
	return "ja-JP"
}

type searchResult struct {
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

type showDetail struct {
	Slug    string   `json:"slug"`
	Title   string   `json:"title"`
	Locales []string `json:"locales"`
}

type episodeListing struct {
	Pages []struct {
		Eps []int `json:"eps"`
	} `json:"pages"`
	Result []struct {
		Slug          string `json:"slug"`
		EpisodeNumber int    `json:"episode_number"`
	} `json:"result"`
}

type episodeDetail struct {
	Servers []struct {
		Name string `json:"name"`
		Src  string `json:"src"`
	} `json:"servers"`
}

// request performs one API call. body is nil for GET.
func request(method, path string, body []byte, dest any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, apiBase+path, reader)
	if err != nil {
		return fmt.Errorf("kickassanime: build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", referer)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := providerhost.HTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("kickassanime: request failed: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("kickassanime: read response: %w", err)
	}
	if providerhost.IsRateLimitedBody(payload) {
		return providerhost.ErrRateLimited
	}
	if !providerhost.HTTPStatusOK(resp.StatusCode) {
		return providerhost.HTTPStatusError("kickassanime "+path, resp.StatusCode, payload)
	}
	if dest == nil {
		return nil
	}
	if err := json.Unmarshal(payload, dest); err != nil {
		return fmt.Errorf("kickassanime: parse %s response: %w", path, err)
	}
	return nil
}

func searchShows(query string) ([]searchResult, error) {
	body, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return nil, err
	}
	var results []searchResult
	if err := request(http.MethodPost, "/api/search", body, &results); err != nil {
		return nil, err
	}
	return results, nil
}

func fetchShow(slug string) (showDetail, error) {
	var show showDetail
	err := request(http.MethodGet, "/api/show/"+slug, nil, &show)
	return show, err
}

// fetchEpisodes returns the listing page containing around. The API paginates in
// blocks and selects the block by episode number, so asking for the episode
// being played is what returns its slug.
func fetchEpisodes(slug, locale string, around int) (episodeListing, error) {
	if around < 1 {
		around = 1
	}
	var listing episodeListing
	path := fmt.Sprintf("/api/show/%s/episodes?ep=%d&lang=%s", slug, around, locale)
	err := request(http.MethodGet, path, nil, &listing)
	return listing, err
}

// fetchEpisodeServers returns the players offered for one episode. The path
// embeds both the episode number and its slug.
func fetchEpisodeServers(showSlug string, epNo int, episodeSlug string) (episodeDetail, error) {
	var detail episodeDetail
	path := fmt.Sprintf("/api/show/%s/episode/ep-%d-%s", showSlug, epNo, episodeSlug)
	err := request(http.MethodGet, path, nil, &detail)
	return detail, err
}

// fetchPlayerPage retrieves a server's embed so its sources can be read.
func fetchPlayerPage(src string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, src, nil)
	if err != nil {
		return nil, fmt.Errorf("kickassanime: build player request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", referer)

	resp, err := providerhost.HTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("kickassanime: player request failed: %w", err)
	}
	defer resp.Body.Close()

	page, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("kickassanime: read player page: %w", err)
	}
	if !providerhost.HTTPStatusOK(resp.StatusCode) {
		return nil, providerhost.HTTPStatusError("kickassanime player", resp.StatusCode, page)
	}
	return page, nil
}

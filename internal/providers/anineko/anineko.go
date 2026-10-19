// Package anineko resolves streams from anineko.to.
//
// A watch page lists its servers grouped by language (hard sub, soft sub,
// dub). Each server is an embed on one of two players, bibiemb or vibeplayer,
// and both pages name an HLS master playlist in a script variable.
package anineko

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/thexykril/otakase/internal/providers"
)

func init() {
	providers.Register(providers.Meta{
		Name:     "anineko",
		Aliases:  []string{"ani-neko", "ani neko"},
		Referrer: siteURL + "/",
	}, func() providers.Provider { return &Provider{} })
}

// Provider resolves anineko.to.
type Provider struct{}

func (p *Provider) Name() string { return "anineko" }

var (
	// Search results link to the show's watch page; its slug is the show id.
	watchSlugRE = regexp.MustCompile(`/watch/([^/?#]+)`)
	episodeRE   = regexp.MustCompile(`/ep-(\d+)`)
)

// SearchAnime asks the site's search endpoint. The catalogue is not split by
// sub and dub, so mode plays no part.
func (p *Provider) SearchAnime(query, mode string) ([]providers.SelectionOption, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("empty search query")
	}

	body, err := get(baseURL+"/ajax/search?q="+url.QueryEscape(query), baseURL+"/")
	if err != nil {
		return nil, err
	}
	var answer struct {
		Success bool `json:"success"`
		Results []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
			Image string `json:"image"`
			Meta  string `json:"meta"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return nil, fmt.Errorf("parse anineko search response: %w", err)
	}
	if !answer.Success {
		return nil, errors.New("anineko search failed")
	}

	var results []providers.SelectionOption
	for _, r := range answer.Results {
		m := watchSlugRE.FindStringSubmatch(r.URL)
		if m == nil || strings.TrimSpace(m[1]) == "" {
			continue
		}
		title := strings.TrimSpace(r.Title)
		label := title
		if meta := strings.TrimSpace(r.Meta); meta != "" {
			label += " — " + meta
		}
		results = append(results, providers.SelectionOption{
			Key:       strings.TrimSpace(m[1]),
			Label:     label,
			Title:     title,
			Thumbnail: absolute(r.Image),
		})
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no results for %q", query)
	}
	return results, nil
}

// EpisodesList reads the episode links off the show's watch page.
func (p *Provider) EpisodesList(showID, mode string) ([]string, error) {
	slug := strings.TrimSpace(showID)
	if slug == "" {
		return nil, errors.New("empty show id")
	}
	page, err := get(watchURL(slug, 0), baseURL+"/")
	if err != nil {
		return nil, err
	}

	seen := map[int]bool{}
	var numbers []int
	for _, m := range episodeRE.FindAllSubmatch(page, -1) {
		n, err := strconv.Atoi(string(m[1]))
		if err != nil || n <= 0 || seen[n] {
			continue
		}
		seen[n] = true
		numbers = append(numbers, n)
	}
	if len(numbers) == 0 {
		return nil, fmt.Errorf("no episodes found for %q", slug)
	}
	sort.Ints(numbers)

	episodes := make([]string, len(numbers))
	for i, n := range numbers {
		episodes[i] = strconv.Itoa(n)
	}
	return episodes, nil
}

func (p *Provider) GetEpisodeURL(config providers.PlaybackConfig, id string, epNo int) ([]string, error) {
	links, _, err := p.GetEpisodeURLForModeWithHints(config, id, epNo, config.SubOrDub)
	return links, err
}

func (p *Provider) GetEpisodeURLForMode(config providers.PlaybackConfig, id string, epNo int, mode string) ([]string, error) {
	links, _, err := p.GetEpisodeURLForModeWithHints(config, id, epNo, mode)
	return links, err
}

// GetEpisodeURLForModeWithHints returns the first server that resolves, in
// the order the page lists them.
func (p *Provider) GetEpisodeURLForModeWithHints(config providers.PlaybackConfig, id string, epNo int, mode string) ([]string, map[string]providers.StreamPlaybackHint, error) {
	slug := strings.TrimSpace(id)
	if slug == "" {
		return nil, nil, errors.New("empty show id")
	}
	if epNo <= 0 {
		return nil, nil, fmt.Errorf("invalid episode number %d", epNo)
	}
	mode = providers.NormalizeTranslationType(mode)

	page, err := get(watchURL(slug, epNo), baseURL+"/")
	if err != nil {
		return nil, nil, err
	}
	embeds, err := pickServers(serverGroups(string(page)), mode, config.SubStyle)
	if err != nil {
		return nil, nil, err
	}
	if len(embeds) == 0 {
		return nil, nil, fmt.Errorf("no %s streams found for episode %d", mode, epNo)
	}

	for _, embed := range embeds {
		var s stream
		switch embedHost(embed) {
		case "bibiemb":
			s, err = resolveBibiemb(embed)
		case "vibeplayer":
			s, err = resolveVibeplayer(embed)
		default:
			continue
		}
		if err != nil {
			continue
		}
		hints := map[string]providers.StreamPlaybackHint{
			s.URL: {Referrer: s.Referrer, Subtitle: s.Subtitle},
		}
		return []string{s.URL}, hints, nil
	}
	return nil, nil, fmt.Errorf("no playable streams resolved for episode %d", epNo)
}

func watchURL(slug string, epNo int) string {
	if epNo > 0 {
		return fmt.Sprintf("%s/watch/%s/ep-%d", baseURL, slug, epNo)
	}
	return baseURL + "/watch/" + slug
}

// absolute turns the site's root-relative and protocol-relative links into
// full URLs.
func absolute(link string) string {
	link = strings.TrimSpace(link)
	switch {
	case link == "":
		return ""
	case strings.HasPrefix(link, "http://"), strings.HasPrefix(link, "https://"):
		return link
	case strings.HasPrefix(link, "//"):
		return "https:" + link
	case strings.HasPrefix(link, "/"):
		return baseURL + link
	}
	return baseURL + "/" + link
}

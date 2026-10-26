// Package reanime resolves episodes from ReAnime and HLS streams from FlixCloud.
package reanime

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/thexykril/otakase/internal/providers"
)

type Provider struct {
	mu        sync.Mutex
	client    *apiClient
	skips     map[string]skipTimes
	skipOrder []string
}

const maxSkipEntries = 64

type skipTimes struct {
	intro []int
	outro []int
}

func (p *Provider) Name() string { return "reanime" }

func (p *Provider) api() *apiClient {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client == nil {
		p.client = newClient()
	}
	return p.client
}

func (p *Provider) SearchAnime(query, mode string) ([]providers.SelectionOption, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("reanime: empty search query")
	}
	matches, err := p.api().search(query)
	if err != nil {
		return nil, err
	}
	results := make([]providers.SelectionOption, 0, len(matches))
	seen := make(map[string]bool)
	for _, match := range matches {
		slug := match.slug()
		title := strings.TrimSpace(match.title())
		if !slugPattern.MatchString(slug) || title == "" || seen[slug] {
			continue
		}
		if providers.NormalizeTranslationType(mode) == "dub" && match.Dubbed == 0 && match.Subbed > 0 {
			continue
		}
		seen[slug] = true
		hit := providers.SearchHit{MalID: match.MalID, Episodes: match.Episodes}
		results = append(results, providers.SelectionOption{
			Key:       slug,
			Title:     title,
			Label:     title,
			Thumbnail: match.thumbnail(),
			ExtraData: hit,
		})
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("reanime: no results for %q", query)
	}
	return results, nil
}

func (p *Provider) EpisodesList(id, mode string) ([]string, error) {
	list, err := p.api().episodes(id)
	if err != nil {
		return nil, err
	}
	maxDub := 0
	if providers.NormalizeTranslationType(mode) == "dub" {
		var detail show
		if err := p.api().json("/api/v1/anime/"+url.PathEscape(id), &detail); err == nil {
			if detail.Subbed > 0 && detail.Dubbed == 0 {
				return nil, fmt.Errorf("reanime: no dub for %s", id)
			}
			maxDub = detail.Dubbed
		}
	}
	seen := make(map[int]bool)
	episodes := make([]int, 0, len(list))
	for _, ep := range list {
		if ep.Number <= 0 || (maxDub > 0 && ep.Number > maxDub) || seen[ep.Number] {
			continue
		}
		seen[ep.Number] = true
		episodes = append(episodes, ep.Number)
	}
	if len(episodes) == 0 {
		return nil, fmt.Errorf("reanime: no %s episodes for %s", providers.NormalizeTranslationType(mode), id)
	}
	sort.Ints(episodes)
	labels := make([]string, len(episodes))
	for i, number := range episodes {
		labels[i] = strconv.Itoa(number)
	}
	return labels, nil
}

func (p *Provider) GetEpisodeURL(cfg providers.PlaybackConfig, id string, epNo int) ([]string, error) {
	links, _, err := p.GetEpisodeURLForModeWithHints(cfg, id, epNo, cfg.SubOrDub)
	return links, err
}

func (p *Provider) GetEpisodeURLForMode(cfg providers.PlaybackConfig, id string, epNo int, mode string) ([]string, error) {
	links, _, err := p.GetEpisodeURLForModeWithHints(cfg, id, epNo, mode)
	return links, err
}

func (p *Provider) GetEpisodeURLForModeWithHints(_ providers.PlaybackConfig, id string, epNo int, mode string) ([]string, map[string]providers.StreamPlaybackHint, error) {
	audio := providers.NormalizeTranslationType(mode)
	servers, err := p.api().servers(id, epNo, audio)
	if err != nil {
		return nil, nil, err
	}
	var lastErr error
	for _, server := range servers {
		stream, err := p.api().stream(server.Link)
		if err != nil {
			lastErr = err
			continue
		}
		// Sub and dub can point to the exact same FlixCloud embed.
		// Only accept a dub if its HLS master confirms English audio;
		// mpv and the watch relay then select that language.
		if audio == "dub" && server.SharedWithSub {
			if err := p.api().verifyEnglishAudio(stream); err != nil {
				lastErr = err
				continue
			}
		}
		hint := providers.StreamPlaybackHint{
			HLS:          true,
			Referrer:     flixURL + "/",
			Headers:      flixBrowserHeaders(),
			FlixCloudKey: stream.ManifestKey,
			Subtitles:    stream.Subtitles,
			Subtitle:     stream.Subtitle,
		}
		if audio == "dub" {
			hint.Subtitle = ""
		}
		intro := chapterRange(stream.Intro)
		outro := chapterRange(stream.Outro)
		p.rememberSkip(skipKey(id, epNo, audio), skipTimes{intro: intro, outro: outro})
		return []string{stream.URL}, map[string]providers.StreamPlaybackHint{stream.URL: hint}, nil
	}
	if lastErr != nil {
		return nil, nil, fmt.Errorf("reanime: no playable %s server for %s episode %d: %w", audio, id, epNo, lastErr)
	}
	return nil, nil, fmt.Errorf("reanime: no playable %s server for %s episode %d", audio, id, epNo)
}

func validRange(start, end int) []int {
	if start < 0 || end <= start {
		return nil
	}
	return []int{start, end}
}

func chapterRange(c chapter) []int {
	if c.Start < 0 || c.End <= c.Start {
		return nil
	}
	return validRange(int(c.Start), int(c.End))
}

// rememberSkip caps skip entries retained by long-running clients.
func (p *Provider) rememberSkip(key string, ranges skipTimes) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.skips == nil {
		p.skips = make(map[string]skipTimes)
	}
	if _, exists := p.skips[key]; !exists {
		if len(p.skipOrder) >= maxSkipEntries {
			delete(p.skips, p.skipOrder[0])
			p.skipOrder = p.skipOrder[1:]
		}
		p.skipOrder = append(p.skipOrder, key)
	}
	p.skips[key] = ranges
}

func skipKey(id string, epNo int, mode string) string {
	return id + "/" + strconv.Itoa(epNo) + "/" + mode
}

func (p *Provider) SkipRange(id, mode string, epNo int) (intro, outro []int, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	data := p.skips[skipKey(id, epNo, providers.NormalizeTranslationType(mode))]
	return data.intro, data.outro, nil
}

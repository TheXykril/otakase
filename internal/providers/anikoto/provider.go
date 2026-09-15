package anikoto

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/thexykril/otakase/internal/providers"
)

// Provider is the anikoto source. It resolves episodes and streams by AniList
// media id, which is the identifier the tracker already holds.
type Provider struct {
	client *client
}

func (p *Provider) Name() string { return "anikoto" }

func (p *Provider) api() *client {
	if p.client == nil {
		p.client = newClient()
	}
	return p.client
}

// SearchAnime returns matches labelled with format and year, because a search
// for a long-running title returns several seasons whose names differ only in
// ways that are easy to misread.
func (p *Provider) SearchAnime(query, mode string) ([]providers.SelectionOption, error) {
	results, err := p.api().search(query)
	if err != nil {
		return nil, err
	}
	options := make([]providers.SelectionOption, 0, len(results))
	for _, result := range results {
		if result.ID == 0 || strings.TrimSpace(result.Name) == "" {
			continue
		}
		options = append(options, providers.SelectionOption{
			Key:   strconv.Itoa(result.ID),
			Title: result.Name,
			Label: describe(result),
		})
	}
	if len(options) == 0 {
		return nil, fmt.Errorf("anikoto: nothing found for %q", query)
	}
	return options, nil
}

func describe(result searchResult) string {
	parts := []string{}
	if result.Format != "" {
		parts = append(parts, result.Format)
	}
	if result.Year > 0 {
		parts = append(parts, strconv.Itoa(result.Year))
	}
	if len(parts) == 0 {
		return result.Name
	}
	return fmt.Sprintf("%s (%s)", result.Name, strings.Join(parts, ", "))
}

// EpisodesList returns the episode numbers carried in the requested language.
//
// Sub and dub are separate entries for the same number here, so the list is
// filtered by language rather than deduplicated: a show that exists only in one
// of them must report an empty list for the other, not a list that cannot play.
func (p *Provider) EpisodesList(showID, mode string) ([]string, error) {
	episodes, err := p.api().episodes(showID)
	if err != nil {
		return nil, err
	}
	category := normalizeCategory(mode)

	numbers := make([]int, 0, len(episodes))
	seen := map[int]bool{}
	for _, ep := range episodes {
		if !strings.EqualFold(ep.Category, category) || ep.Number <= 0 || seen[ep.Number] {
			continue
		}
		seen[ep.Number] = true
		numbers = append(numbers, ep.Number)
	}
	if len(numbers) == 0 {
		return nil, fmt.Errorf("anikoto: no %s episodes for %s", category, showID)
	}
	sort.Ints(numbers)

	out := make([]string, 0, len(numbers))
	for _, n := range numbers {
		out = append(out, strconv.Itoa(n))
	}
	return out, nil
}

func (p *Provider) GetEpisodeURL(config providers.PlaybackConfig, id string, epNo int) ([]string, error) {
	urls, _, err := p.GetEpisodeURLForModeWithHints(config, id, epNo, config.SubOrDub)
	return urls, err
}

func (p *Provider) GetEpisodeURLForMode(config providers.PlaybackConfig, id string, epNo int, mode string) ([]string, error) {
	urls, _, err := p.GetEpisodeURLForModeWithHints(config, id, epNo, mode)
	return urls, err
}

// GetEpisodeURLForModeWithHints resolves the stream and the headers it needs.
//
// The host serves its segments from a CDN that rejects requests without a
// referrer and a browser user-agent, and it states both in the response rather
// than leaving them to be discovered by a 403. They are passed through as-is.
func (p *Provider) GetEpisodeURLForModeWithHints(config providers.PlaybackConfig, id string, epNo int, mode string) ([]string, map[string]providers.StreamPlaybackHint, error) {
	category := normalizeCategory(mode)
	response, err := p.api().link(id, category, epNo)
	if err != nil {
		return nil, nil, err
	}

	ordered := orderStreams(response.Streams)
	subtitle := preferredSubtitle(response.Subtitles)

	urls := make([]string, 0, len(ordered))
	hints := make(map[string]providers.StreamPlaybackHint, len(ordered))
	for _, s := range ordered {
		if strings.TrimSpace(s.URL) == "" {
			continue
		}
		urls = append(urls, s.URL)

		headers := map[string]string{}
		for key, value := range s.HTTPHeaders {
			headers[key] = value
		}
		refer := s.Referer
		if refer == "" {
			refer = referer
		}
		if _, ok := headers["Referer"]; !ok {
			headers["Referer"] = refer
		}
		hints[s.URL] = providers.StreamPlaybackHint{
			Referrer: refer,
			Subtitle: subtitle,
			Headers:  headers,
		}
	}
	if len(urls) == 0 {
		return nil, nil, fmt.Errorf("anikoto: no playable stream for %s episode %d", id, epNo)
	}
	return urls, hints, nil
}

// orderStreams puts the host's own preference first: the stream it marks
// default, then by its stated priority. Falling back down this list is what
// happens when a server is having a bad day, so the order matters.
func orderStreams(streams []stream) []stream {
	ordered := make([]stream, len(streams))
	copy(ordered, streams)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Default != ordered[j].Default {
			return ordered[i].Default
		}
		return ordered[i].Priority < ordered[j].Priority
	})
	return ordered
}

// preferredSubtitle picks the track to hand the player: the one marked default,
// otherwise the first English one, otherwise nothing rather than a guess.
func preferredSubtitle(subtitles []subtitle) string {
	for _, sub := range subtitles {
		if sub.Default && sub.File != "" {
			return sub.File
		}
	}
	for _, sub := range subtitles {
		if sub.File == "" {
			continue
		}
		if strings.EqualFold(sub.Language, "english") || strings.EqualFold(sub.Label, "English") {
			return sub.File
		}
	}
	return ""
}

// SkipRange reports the intro and outro the host gives for an episode, in
// seconds. It comes back from the same request that resolves the stream, so
// asking for it costs nothing extra.
func (p *Provider) SkipRange(id, mode string, epNo int) (intro, outro []int, err error) {
	response, err := p.api().link(id, normalizeCategory(mode), epNo)
	if err != nil {
		return nil, nil, err
	}
	return validSpan(response.Intro), validSpan(response.Outro), nil
}

// validSpan accepts only a well-formed [start, end] pair. A malformed or
// reversed span would make the player seek somewhere wrong, which is worse
// than not skipping at all.
func validSpan(span []int) []int {
	if len(span) != 2 || span[1] <= span[0] || span[1] <= 0 {
		return nil
	}
	return []int{span[0], span[1]}
}

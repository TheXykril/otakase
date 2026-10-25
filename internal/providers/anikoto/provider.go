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
		if result.Slug == "" || strings.TrimSpace(result.Name) == "" {
			continue
		}
		options = append(options, providers.SelectionOption{
			Key:   result.Slug,
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
// A show that exists only in one language must report an empty list for the
// other, not a list that cannot play.
func (p *Provider) EpisodesList(showID, mode string) ([]string, error) {
	episodes, err := p.api().episodeList(showID)
	if err != nil {
		return nil, err
	}
	category := normalizeCategory(mode)

	numbers := make([]int, 0, len(episodes))
	seen := map[int]bool{}
	for _, ep := range episodes {
		if !ep.has(category) || seen[ep.Number] {
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
// megaplay's CDN refuses manifests and segments without its own page as the
// referrer, so that and a browser user-agent travel with the link.
//
// A language the episode does not carry fails here rather than resolving to the
// other one under the wrong label, which lets GetEpisodeURLForPlayback's
// fallback offer the other language instead.
func (p *Provider) GetEpisodeURLForModeWithHints(config providers.PlaybackConfig, id string, epNo int, mode string) ([]string, map[string]providers.StreamPlaybackHint, error) {
	response, err := p.api().link(id, normalizeCategory(mode), epNo)
	if err != nil {
		return nil, nil, err
	}
	refer := response.Referer
	if refer == "" {
		refer = referer
	}
	hint := providers.StreamPlaybackHint{
		Referrer:  refer,
		Subtitle:  preferredSubtitle(response.Subtitles),
		Subtitles: subtitleTracks(response.Subtitles),
		Headers:   map[string]string{"Referer": refer, "User-Agent": userAgent},
	}
	return []string{response.URL}, map[string]providers.StreamPlaybackHint{response.URL: hint}, nil
}

// subtitleTracks lists every subtitle the host offers.
func subtitleTracks(subtitles []subtitle) []providers.SubtitleTrack {
	var tracks []providers.SubtitleTrack
	for _, sub := range subtitles {
		if !sub.caption() {
			continue
		}
		label := strings.TrimSpace(sub.Label)
		tracks = append(tracks, providers.SubtitleTrack{URL: sub.File, Language: label, Label: label})
	}
	return tracks
}

// preferredSubtitle picks the track to hand the player: the one marked default,
// otherwise the first English one, otherwise nothing rather than a guess.
func preferredSubtitle(subtitles []subtitle) string {
	for _, sub := range subtitles {
		if sub.Default && sub.caption() {
			return sub.File
		}
	}
	for _, sub := range subtitles {
		if sub.caption() && strings.Contains(strings.ToLower(sub.Label), "english") {
			return sub.File
		}
	}
	return ""
}

// SkipRange reports the intro and outro the host gives for an episode, in
// seconds. It comes back with the stream, and the client keeps the last few
// resolutions, so asking right after playing costs nothing extra.
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

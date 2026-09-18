package kickassanime

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/thexykril/otakase/internal/providerhost"
	"github.com/thexykril/otakase/internal/providers"
)

type Provider struct{}

func (p *Provider) Name() string { return "kickassanime" }

func (p *Provider) SearchAnime(query, mode string) ([]providers.SelectionOption, error) {
	results, err := searchShows(query)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no results for %q", query)
	}

	options := make([]providers.SelectionOption, 0, len(results))
	for _, result := range results {
		label := result.Title
		if result.Type != "" {
			label += " — " + strings.ToUpper(result.Type)
		}
		options = append(options, providers.SelectionOption{
			Key:   result.Slug,
			Title: result.Title,
			Label: label,
		})
	}
	return options, nil
}

func (p *Provider) EpisodesList(showID, mode string) ([]string, error) {
	locale := localeForMode(mode)
	if err := requireLocale(showID, locale, mode); err != nil {
		return nil, err
	}

	listing, err := fetchEpisodes(showID, locale, 1)
	if err != nil {
		return nil, err
	}

	// pages carries the full episode range across every block, so the whole list
	// is known without walking each page.
	seen := map[int]struct{}{}
	for _, page := range listing.Pages {
		for _, ep := range page.Eps {
			seen[ep] = struct{}{}
		}
	}
	for _, ep := range listing.Result {
		seen[ep.EpisodeNumber] = struct{}{}
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("no episodes listed for %q", showID)
	}

	numbers := make([]int, 0, len(seen))
	for ep := range seen {
		numbers = append(numbers, ep)
	}
	sort.Ints(numbers)

	episodes := make([]string, 0, len(numbers))
	for _, n := range numbers {
		episodes = append(episodes, strconv.Itoa(n))
	}
	return episodes, nil
}

// requireLocale reports a missing dub as such. The show detail lists the locales
// actually carried, so this is answerable before any episode request -- and a
// clean "no dub" lets otakase's audio fallback act instead of failing opaquely.
func requireLocale(showID, locale, mode string) error {
	show, err := fetchShow(showID)
	if err != nil {
		// The listing request below will surface a real outage; do not block on a
		// detail lookup that is only an optimisation.
		providerhost.Log(fmt.Sprintf("kickassanime: could not read locales for %q: %v", showID, err))
		return nil
	}
	if len(show.Locales) == 0 {
		return nil
	}
	for _, candidate := range show.Locales {
		if strings.EqualFold(candidate, locale) {
			return nil
		}
	}
	return fmt.Errorf("no %s release for this show (available: %s)",
		providers.NormalizeTranslationType(mode), strings.Join(show.Locales, ", "))
}

func (p *Provider) GetEpisodeURL(config providers.PlaybackConfig, id string, epNo int) ([]string, error) {
	links, _, err := p.GetEpisodeURLForModeWithHints(config, id, epNo, config.SubOrDub)
	return links, err
}

func (p *Provider) GetEpisodeURLForMode(config providers.PlaybackConfig, id string, epNo int, mode string) ([]string, error) {
	links, _, err := p.GetEpisodeURLForModeWithHints(config, id, epNo, mode)
	return links, err
}

func (p *Provider) GetEpisodeURLForModeWithHints(config providers.PlaybackConfig, id string, epNo int, mode string) ([]string, map[string]providers.StreamPlaybackHint, error) {
	locale := localeForMode(mode)
	if err := requireLocale(id, locale, mode); err != nil {
		return nil, nil, err
	}

	listing, err := fetchEpisodes(id, locale, epNo)
	if err != nil {
		return nil, nil, err
	}

	episodeSlug := ""
	for _, episode := range listing.Result {
		if episode.EpisodeNumber == epNo {
			episodeSlug = episode.Slug
			break
		}
	}
	if episodeSlug == "" {
		return nil, nil, fmt.Errorf("episode %d not found", epNo)
	}

	detail, err := fetchEpisodeServers(id, epNo, episodeSlug)
	if err != nil {
		return nil, nil, err
	}
	if len(detail.Servers) == 0 {
		return nil, nil, fmt.Errorf("no servers offered for episode %d", epNo)
	}

	// Servers are tried in the order given; one being down is ordinary.
	var lastErr error
	for _, server := range detail.Servers {
		page, err := fetchPlayerPage(server.Src)
		if err != nil {
			lastErr = err
			providerhost.Log(fmt.Sprintf("kickassanime: server %s unreachable: %v", server.Name, err))
			continue
		}
		sources, err := parsePlayerSources(page)
		if err != nil {
			lastErr = err
			providerhost.Log(fmt.Sprintf("kickassanime: server %s gave nothing playable: %v", server.Name, err))
			continue
		}

		// Two different hosts with two different rules, and getting this wrong
		// yields a manifest that opens and then plays nothing:
		//
		//   the manifest wants Referer: kaa.lt
		//   the video segments live elsewhere and 403 unless Origin names the
		//   player's domain -- a Referer, even the right one, is not accepted
		//
		// So the referrer stays kaa.lt for the manifest and Origin is sent
		// alongside for the segments. The subtitles are a separate file MPV has
		// to be told about.
		hints := map[string]providers.StreamPlaybackHint{
			sources.Manifest: {
				Referrer: referer,
				Subtitle: englishSubtitle(sources.Subtitles),
				Headers:  map[string]string{"Origin": playerOrigin(server.Src)},
			},
		}
		return []string{sources.Manifest}, hints, nil
	}

	if lastErr != nil {
		return nil, nil, fmt.Errorf("no playable server for episode %d: %w", epNo, lastErr)
	}
	return nil, nil, fmt.Errorf("no playable server for episode %d", epNo)
}

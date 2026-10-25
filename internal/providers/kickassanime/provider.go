package kickassanime

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

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
	return episodeURLForLocale(id, epNo, locale)
}

// episodeURLForLocale resolves a listed language without re-fetching show
// details for each individual language.
func episodeURLForLocale(id string, epNo int, locale string) ([]string, map[string]providers.StreamPlaybackHint, error) {
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
		// The host lists some shows with every episode and no player at all,
		// so finding the show says nothing about whether it plays.
		return nil, nil, fmt.Errorf("listed, but no video offered for episode %d", epNo)
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
				Referrer:  referer,
				Subtitle:  englishSubtitle(sources.Subtitles),
				Subtitles: providerTracks(sources.Subtitles),
				Headers:   map[string]string{"Origin": playerOrigin(server.Src)},
			},
		}
		return []string{sources.Manifest}, hints, nil
	}

	if lastErr != nil {
		return nil, nil, fmt.Errorf("no playable server for episode %d: %w", epNo, lastErr)
	}
	return nil, nil, fmt.Errorf("no playable server for episode %d", epNo)
}

// AudioVariant is a separately indexed language release with the headers it
// requires. The Activity can serve it beside the initially selected stream.
type AudioVariant struct {
	URL  string
	Hint providers.StreamPlaybackHint
}

// EpisodeAudioVariants looks up the actual episode in each language the show
// lists. Do not label a language as available just because the show lists its
// locale: some episodes do not have a playable release in that language.
func EpisodeAudioVariants(showID string, episode int, maxWait time.Duration) map[string]AudioVariant {
	variants := map[string]AudioVariant{}
	show, err := fetchShow(showID)
	if err != nil {
		return variants
	}
	locales := make(map[string]string)
	for _, locale := range show.Locales {
		lang := strings.ToLower(strings.SplitN(strings.ReplaceAll(locale, "_", "-"), "-", 2)[0])
		if len(lang) != 2 {
			continue
		}
		legal := true
		for _, ch := range lang {
			if ch < 'a' || ch > 'z' {
				legal = false
			}
		}
		if legal && locales[lang] == "" && len(locales) < 16 {
			locales[lang] = locale
		}
	}
	if len(locales) == 0 {
		return variants
	}
	type answer struct {
		lang  string
		value AudioVariant
	}
	results := make(chan answer, len(locales))
	gate := make(chan struct{}, 4)
	for lang, locale := range locales {
		go func(lang, locale string) {
			gate <- struct{}{}
			defer func() { <-gate }()
			links, hints, err := episodeURLForLocale(showID, episode, locale)
			if err != nil || len(links) == 0 {
				results <- answer{}
				return
			}
			results <- answer{lang: lang, value: AudioVariant{URL: links[0], Hint: hints[links[0]]}}
		}(lang, locale)
	}
	if maxWait <= 0 {
		maxWait = 5 * time.Second
	}
	timer := time.NewTimer(maxWait)
	defer timer.Stop()
	for remaining := len(locales); remaining > 0; remaining-- {
		select {
		case result := <-results:
			if result.lang != "" && result.value.URL != "" {
				variants[result.lang] = result.value
			}
		case <-timer.C:
			return variants
		}
	}
	return variants
}

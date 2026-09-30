package anizone

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/thexykril/otakase/internal/providers"
)

// Provider resolves anizone.to.
type Provider struct{}

func (p *Provider) Name() string { return "anizone" }

var (
	// A search result, as the Livewire payload embeds it. url, cover and
	// main_title appear in that order in every entry.
	searchResultRE = regexp.MustCompile(`"url":"https?://anizone\.to/anime/([a-z0-9-]+)","cover":"([^"]*)","main_title":"([^"]*)"`)

	// The player's configuration, handed to vidstack as a JSON string.
	playerConfigRE = regexp.MustCompile(`vidstackPlayer\(JSON\.parse\('(\{.*?\})'\)`)

	// Every episode of a show links to its own page.
	episodeLinkRE = regexp.MustCompile(`/anime/[a-z0-9-]+/(\d+)`)
)

// playerConfig is the part of the player payload worth reading.
type playerConfig struct {
	Src       string `json:"src"`
	Subtitles []struct {
		Title    string `json:"title"`
		Format   string `json:"format"`
		Language string `json:"language"`
		Default  bool   `json:"default"`
		File     string `json:"file"`
	} `json:"subtitles"`
}

// escapedSlashRE matches a slash behind any number of backslashes.
//
// Depth varies by where the payload sits: the player's configuration is
// escaped once, the search results are inside a Livewire snapshot attribute
// and escaped again on top of that. Matching any depth avoids caring which.
var escapedSlashRE = regexp.MustCompile(`\\+/`)

// decode turns a Livewire-embedded payload back into JSON.
//
// It arrives escaped more than once: as HTML attribute entities, as \u0022 for
// the quotes inside a nested JSON string, and with forward slashes escaped the
// way PHP's json_encode writes them by default.
func decode(page string) string {
	decoded := html.UnescapeString(page)
	decoded = strings.ReplaceAll(decoded, `\u0022`, `"`)
	return escapedSlashRE.ReplaceAllString(decoded, "/")
}

// SearchAnime finds shows by title.
//
// anizone carries one catalogue rather than separate sub and dub listings, so
// mode is not a search term here; it is settled when the episode resolves.
func (p *Provider) SearchAnime(query, mode string) ([]providers.SelectionOption, error) {
	page, err := fetch(baseURL + "/anime?search=" + url.QueryEscape(query))
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	results := []providers.SelectionOption{}
	for _, match := range searchResultRE.FindAllStringSubmatch(decode(page), -1) {
		slug, cover, title := match[1], match[2], match[3]
		if seen[slug] {
			continue
		}
		seen[slug] = true
		results = append(results, providers.SelectionOption{
			Key:       slug,
			Label:     title,
			Title:     title,
			Thumbnail: cover,
		})
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("anizone: no results for %q", query)
	}
	return results, nil
}

// EpisodesList reports the episode numbers a show has.
func (p *Provider) EpisodesList(showID, mode string) ([]string, error) {
	page, err := fetch(baseURL + "/anime/" + showID)
	if err != nil {
		return nil, err
	}

	seen := map[int]bool{}
	numbers := []int{}
	for _, match := range episodeLinkRE.FindAllStringSubmatch(decode(page), -1) {
		n, err := strconv.Atoi(match[1])
		if err != nil || n <= 0 || seen[n] {
			continue
		}
		seen[n] = true
		numbers = append(numbers, n)
	}
	if len(numbers) == 0 {
		return nil, fmt.Errorf("anizone: no episodes listed for %q", showID)
	}

	sort.Ints(numbers)
	episodes := make([]string, 0, len(numbers))
	for _, n := range numbers {
		episodes = append(episodes, strconv.Itoa(n))
	}
	return episodes, nil
}

// GetEpisodeURL resolves one episode to a playable stream.
func (p *Provider) GetEpisodeURL(config providers.PlaybackConfig, id string, epNo int) ([]string, error) {
	links, _, err := p.GetEpisodeURLForModeWithHints(config, id, epNo, config.SubOrDub)
	return links, err
}

// GetEpisodeURLForMode resolves an episode for an explicit audio mode.
func (p *Provider) GetEpisodeURLForMode(config providers.PlaybackConfig, id string, epNo int, mode string) ([]string, error) {
	links, _, err := p.GetEpisodeURLForModeWithHints(config, id, epNo, mode)
	return links, err
}

// GetEpisodeURLForModeWithHints resolves an episode and reports what the
// player needs to fetch it.
//
// anizone serves one stream per episode with subtitles alongside it, so there
// is no hardsubbed variant to choose: the subtitle track is handed back as a
// hint, and it is up to the caller whether to render it or draw it in.
func (p *Provider) GetEpisodeURLForModeWithHints(config providers.PlaybackConfig, id string, epNo int, mode string) ([]string, map[string]providers.StreamPlaybackHint, error) {
	page, err := fetch(fmt.Sprintf("%s/anime/%s/%d", baseURL, id, epNo))
	if err != nil {
		return nil, nil, err
	}

	match := playerConfigRE.FindStringSubmatch(decode(page))
	if len(match) < 2 {
		return nil, nil, fmt.Errorf("anizone: no player on the page for %s episode %d", id, epNo)
	}

	var player playerConfig
	if err := json.Unmarshal([]byte(match[1]), &player); err != nil {
		return nil, nil, fmt.Errorf("anizone: could not read the player configuration: %w", err)
	}
	if strings.TrimSpace(player.Src) == "" {
		return nil, nil, fmt.Errorf("anizone: the player carries no stream for %s episode %d", id, epNo)
	}

	hint := providers.StreamPlaybackHint{
		Referrer:  referer,
		Subtitle:  p.preferredSubtitle(player),
		Subtitles: subtitleTracks(player),
	}
	return []string{player.Src}, map[string]providers.StreamPlaybackHint{player.Src: hint}, nil
}

// subtitleTracks lists every subtitle the player offers.
func subtitleTracks(player playerConfig) []providers.SubtitleTrack {
	var tracks []providers.SubtitleTrack
	for _, track := range player.Subtitles {
		if track.File == "" {
			continue
		}
		tracks = append(tracks, providers.SubtitleTrack{URL: track.File, Language: track.Language, Label: track.Title})
	}
	return tracks
}

// preferredSubtitle picks the track to play, favouring the viewer's language
// and then whatever the site marks as default.
func (p *Provider) preferredSubtitle(player playerConfig) string {
	// English unless the host says otherwise. Every track carries a language
	// code and a human title, so both are worth matching against.
	wanted := "english"

	var fallback string
	for _, track := range player.Subtitles {
		if track.File == "" {
			continue
		}
		title := strings.ToLower(track.Title)
		language := strings.ToLower(track.Language)
		if strings.Contains(title, wanted) || language == wanted || (wanted == "english" && language == "en") {
			return track.File
		}
		if track.Default && fallback == "" {
			fallback = track.File
		}
		if fallback == "" {
			fallback = track.File
		}
	}
	return fallback
}

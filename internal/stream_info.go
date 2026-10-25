package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/providers/kickassanime"
	"github.com/thexykril/otakase/internal/providers/nyaa"
)

// StreamInfo is one episode's streams, found without asking anything. The
// Discord bot runs `otakase -stream-info <anilist id>:<episode>` to fetch the
// episode it plays in its watch room, so this never prompts: a show no
// provider can be matched to on its own is an error, not a menu.
type StreamInfo struct {
	AnilistID     int                   `json:"anilist_id"`
	Episode       int                   `json:"episode"`
	Title         string                `json:"title"`
	Episodes      int                   `json:"episodes,omitempty"`
	Provider      string                `json:"provider"`
	ProviderID    string                `json:"provider_id,omitempty"`
	Mode          string                `json:"mode"`
	Streams       []StreamLink          `json:"streams"`
	AudioVariants map[string]StreamLink `json:"audio_variants,omitempty"`
	Torrents      []TorrentLink         `json:"torrents,omitempty"`
}

// TorrentLink identifies a torrent without exposing the CLI's temporary local server.
type TorrentLink struct {
	InfoHash string `json:"info_hash"`
}

// StreamLink is one playable link and what fetching it takes.
type StreamLink struct {
	URL       string            `json:"url"`
	Referrer  string            `json:"referrer,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Subtitles []StreamSubtitle  `json:"subtitles,omitempty"`
	// HLS is set for a playlist whose address does not end in .m3u8, so the
	// watch room relays it as one.
	HLS bool `json:"hls,omitempty"`
}

// StreamSubtitle is a subtitle track of a link.
type StreamSubtitle struct {
	URL      string `json:"url"`
	Language string `json:"language,omitempty"`
	Label    string `json:"label,omitempty"`
}

// ParseStreamInfoArg reads "154587:12" as an AniList id and an episode.
func ParseStreamInfoArg(arg string) (anilistID, episode int, err error) {
	id, ep, ok := strings.Cut(strings.TrimSpace(arg), ":")
	anilistID, idErr := strconv.Atoi(id)
	episode, epErr := strconv.Atoi(ep)
	if !ok || idErr != nil || epErr != nil || anilistID <= 0 || episode <= 0 {
		return 0, 0, fmt.Errorf("want <anilist id>:<episode>, e.g. 154587:1, got %q", arg)
	}
	return anilistID, episode, nil
}

// ResolveStreamInfo finds an episode on the configured providers, preferred
// audio first, then the other. Only http(s) links count: torrents need the
// app's own streamer, which a server relaying the episode doesn't have.
func ResolveStreamInfo(config Config, anilistID, episode int) (StreamInfo, error) {
	return resolveStreamInfo(config, anilistID, episode, true)
}

// ResolveStreamInfoFast leaves the optional per-language provider lookup to
// the caller. The watch room starts playback with this result, then discovers
// the additional releases in the background.
func ResolveStreamInfoFast(config Config, anilistID, episode int) (StreamInfo, error) {
	return resolveStreamInfo(config, anilistID, episode, false)
}

func resolveStreamInfo(config Config, anilistID, episode int, discoverLanguages bool) (StreamInfo, error) {
	nyaa.SetStreamInfoOnly(true)
	defer nyaa.SetStreamInfoOnly(false)
	anime, err := GetAnimeDataByID(anilistID, "")
	if err != nil {
		return StreamInfo{}, err
	}
	info := StreamInfo{AnilistID: anilistID, Episode: episode, Title: GetAnimeName(anime), Episodes: anime.TotalEpisodes}
	if anime.TotalEpisodes > 0 && episode > anime.TotalEpisodes {
		return info, fmt.Errorf("%s has %d episodes", info.Title, anime.TotalEpisodes)
	}

	return searchStreamInfo(config, anime, info, episode, providerNamesForAnime(&config, &anime), discoverLanguages)
}

// streamInfoProviderWait bounds one site's answer. A site that is down can
// take a minute to say so, twice (both languages), and the watch room's
// whole lookup has three.
var streamInfoProviderWait = 30 * time.Second

// streamInfoLookup asks one site for the episode, a variable for tests.
var streamInfoLookup = episodeModeResultWithProviders

// lookupStreamInfo is streamInfoLookup, given up on after wait. The
// abandoned request finishes in the background; the CLI exits soon after.
func lookupStreamInfo(config Config, anime Anime, episode int, mode, name string, wait time.Duration) (ProviderEpisodeResult, error) {
	type answer struct {
		result ProviderEpisodeResult
		err    error
	}
	done := make(chan answer, 1)
	go func() {
		result, err := streamInfoLookup(config, &anime, episode, mode, []string{name})
		done <- answer{result, err}
	}()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case a := <-done:
		return a.result, a.err
	case <-timer.C:
		return ProviderEpisodeResult{}, fmt.Errorf("no answer within %s", wait)
	}
}

// searchStreamInfo asks the sites in order, in the preferred language
// first, for a web link; a download is the fallback. Once a download is
// found in the preferred language, the other language isn't searched.
func searchStreamInfo(config Config, anime Anime, info StreamInfo, episode int, names []string, discoverLanguages bool) (StreamInfo, error) {
	preferred := normalizeTranslationType(config.SubOrDub)
	var failures []string
	var torrentFallback *StreamInfo
	for _, mode := range []string{preferred, alternateTranslationType(preferred)} {
		if torrentFallback != nil {
			break
		}
		for _, name := range names {
			result, err := lookupStreamInfo(config, anime, episode, mode, name, streamInfoProviderWait)
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s %s: %v", name, mode, err))
				continue
			}
			streams := streamLinks(result)
			torrents := torrentLinks(result)
			if len(streams) == 0 && len(torrents) == 0 {
				failures = append(failures, fmt.Sprintf("%s %s: no web links", name, mode))
				continue
			}
			info.Provider, info.Mode, info.Streams, info.Torrents = result.ProviderName, mode, streams, torrents
			// The watch room asks for the other languages of this very
			// show later, without searching for it again.
			info.ProviderID = ""
			if result.ProviderName == "kickassanime" {
				info.ProviderID = result.ProviderID
			}
			if len(streams) > 0 {
				// This provider indexes different spoken languages as separate
				// releases. Discover real episode URLs, not merely locale
				// names, so the Activity can offer only playable choices.
				if discoverLanguages && result.ProviderName == "kickassanime" && result.ProviderID != "" {
					info.AudioVariants = kickassAudioVariants(result.ProviderID, episode, streams)
				}
				return info, nil
			}
			if torrentFallback == nil {
				fallback := info
				torrentFallback = &fallback
			}
		}
	}
	if torrentFallback != nil {
		return *torrentFallback, nil
	}
	if len(failures) == 0 {
		return info, errors.New("no provider is enabled")
	}
	return info, fmt.Errorf("episode %d not found: %s", episode, strings.Join(failures, "; "))
}

// ResolveStreamLanguages finds the other spoken-language releases of an
// episode the fast lookup already matched, by the provider's show id, so the
// watch room does not search for the show a second time.
func ResolveStreamLanguages(provider, providerID string, episode int) (StreamInfo, error) {
	if provider != "kickassanime" || providerID == "" || episode < 1 {
		return StreamInfo{}, fmt.Errorf("no separate language releases for %s", provider)
	}
	return StreamInfo{Episode: episode, Provider: provider, ProviderID: providerID,
		AudioVariants: kickassAudioVariants(providerID, episode, nil)}, nil
}

// kickassAudioVariants turns KickAssAnime's per-language episode pages into
// playable links. This provider indexes different spoken languages as
// separate releases. Discover real episode URLs, not merely locale names,
// so the Activity can offer only playable choices.
func kickassAudioVariants(providerID string, episode int, streams []StreamLink) map[string]StreamLink {
	alternates := kickassanime.EpisodeAudioVariants(providerID, episode, 45*time.Second)
	// Do not advertise a separate language when multiple locale pages
	// resolve to the same unlabelled stream. Its HLS audio renditions are
	// discovered by the player.
	urlCounts := map[string]int{}
	for _, alt := range alternates {
		urlCounts[alt.URL]++
	}
	var out map[string]StreamLink
	for lang, alternate := range alternates {
		if urlCounts[alternate.URL] > 1 || alternate.URL == "" {
			continue
		}
		same := false
		for _, original := range streams {
			if original.URL == alternate.URL {
				same = true
				break
			}
		}
		if same {
			continue
		}
		links := streamLinks(ProviderEpisodeResult{
			Links: []string{alternate.URL},
			LinkHints: map[string]StreamPlaybackHint{alternate.URL: {
				Referrer:  alternate.Hint.Referrer,
				Headers:   alternate.Hint.Headers,
				Subtitle:  alternate.Hint.Subtitle,
				Subtitles: alternate.Hint.Subtitles,
			}},
		})
		if len(links) > 0 {
			if out == nil {
				out = map[string]StreamLink{}
			}
			out[lang] = links[0]
		}
	}
	return out
}

// torrentLinks preserves local torrentstream results as portable infohashes.
// The bot starts its own torrent client rather than trying to contact a
// loopback server belonging to the short-lived CLI process.
func torrentLinks(result ProviderEpisodeResult) []TorrentLink {
	seen := map[string]bool{}
	var out []TorrentLink
	for _, link := range result.Links {
		u, err := url.Parse(link)
		if err == nil && u.Scheme == "magnet" {
			for _, xt := range u.Query()["xt"] {
				const prefix = "urn:btih:"
				if !strings.HasPrefix(strings.ToLower(xt), prefix) {
					continue
				}
				hash := strings.TrimPrefix(strings.ToLower(xt), prefix)
				if len(hash) == 40 && strings.Trim(hash, "0123456789abcdef") == "" && !seen[hash] {
					seen[hash] = true
					out = append(out, TorrentLink{InfoHash: hash})
				}
			}
			continue
		}
		if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" {
			continue
		}
		hash := strings.Trim(u.Path, "/")
		if len(hash) != 40 || strings.Trim(hash, "0123456789abcdefABCDEF") != "" {
			continue
		}
		hash = strings.ToLower(hash)
		if !seen[hash] {
			seen[hash] = true
			out = append(out, TorrentLink{InfoHash: hash})
		}
	}
	return out
}

// streamLinks keeps the web links of a result, best first.
func streamLinks(result ProviderEpisodeResult) []StreamLink {
	var out []StreamLink
	best := BestLink(result.Links)
	for _, link := range result.Links {
		if !strings.HasPrefix(link, "http://") && !strings.HasPrefix(link, "https://") {
			continue
		}
		if u, err := url.Parse(link); err == nil && u.Hostname() == "127.0.0.1" {
			continue
		}
		s := StreamLink{URL: link}
		if hint, ok := result.LinkHints[link]; ok {
			s.Referrer, s.Headers, s.HLS = hint.Referrer, hint.Headers, hint.HLS
			for _, t := range hint.Subtitles {
				s.Subtitles = append(s.Subtitles, StreamSubtitle{URL: t.URL, Language: t.Language, Label: t.Label})
			}
			if len(s.Subtitles) == 0 && hint.Subtitle != "" {
				s.Subtitles = []StreamSubtitle{{URL: hint.Subtitle}}
			}
		}
		if link == best {
			out = append([]StreamLink{s}, out...)
		} else {
			out = append(out, s)
		}
	}
	return out
}

// FormatStreamInfo is the one JSON line -stream-info prints last.
func FormatStreamInfo(info StreamInfo) string {
	b, _ := json.Marshal(info)
	return string(b)
}

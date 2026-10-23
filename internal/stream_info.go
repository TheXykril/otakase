package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/thexykril/otakase/internal/providers/nyaa"
)

// StreamInfo is one episode's streams, found without asking anything. The
// Discord bot runs `otakase -stream-info <anilist id>:<episode>` to fetch the
// episode it plays in its watch room, so this never prompts: a show no
// provider can be matched to on its own is an error, not a menu.
type StreamInfo struct {
	AnilistID int           `json:"anilist_id"`
	Episode   int           `json:"episode"`
	Title     string        `json:"title"`
	Episodes  int           `json:"episodes,omitempty"`
	Provider  string        `json:"provider"`
	Mode      string        `json:"mode"`
	Streams   []StreamLink  `json:"streams"`
	Torrents  []TorrentLink `json:"torrents,omitempty"`
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

	preferred := normalizeTranslationType(config.SubOrDub)
	var failures []string
	var torrentFallback *StreamInfo
	for _, mode := range []string{preferred, alternateTranslationType(preferred)} {
		for _, name := range providerNamesForAnime(&config, &anime) {
			show := anime
			result, err := episodeModeResultWithProviders(config, &show, episode, mode, []string{name})
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
			if len(streams) > 0 {
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
			s.Referrer, s.Headers = hint.Referrer, hint.Headers
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

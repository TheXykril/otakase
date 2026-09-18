package kickassanime

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
)

// The player is an Astro island, and it carries everything needed in the
// element's props attribute -- the HLS manifest and every subtitle track are
// already there in the HTML. There is no second request to make and nothing to
// decrypt; the earlier providers' habit of hiding stream URLs behind an
// obfuscated endpoint simply is not what this host does.
//
// Astro serialises props in a tagged form where each value is [typeCode, value]:
// 0 is the value itself, 1 an array of further tagged values. Only those two
// appear here, but an unknown tag is passed through rather than treated as an
// error, so a new one degrades to a missing field instead of a failed episode.
var astroProps = regexp.MustCompile(`<astro-island[^>]*\sprops="([^"]*)"`)

// PlayerSources is what a player page yields.
type PlayerSources struct {
	Manifest  string
	Subtitles []SubtitleTrack
}

// SubtitleTrack is one external subtitle file.
type SubtitleTrack struct {
	Language string
	Name     string
	Format   string
	URL      string
}

// untag unwraps one [typeCode, value] pair.
func untag(raw json.RawMessage) json.RawMessage {
	var pair []json.RawMessage
	if err := json.Unmarshal(raw, &pair); err != nil || len(pair) != 2 {
		// Not a tagged pair; use it as-is.
		return raw
	}
	return pair[1]
}

// untagString unwraps a tagged value expected to be a string.
func untagString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(untag(raw), &s); err != nil {
		return ""
	}
	return s
}

// parsePlayerSources reads the manifest and subtitle tracks out of a player page.
func parsePlayerSources(page []byte) (PlayerSources, error) {
	match := astroProps.FindSubmatch(page)
	if match == nil {
		return PlayerSources{}, fmt.Errorf("kickassanime: player page carries no astro-island props")
	}

	var props map[string]json.RawMessage
	if err := json.Unmarshal([]byte(html.UnescapeString(string(match[1]))), &props); err != nil {
		return PlayerSources{}, fmt.Errorf("kickassanime: parse player props: %w", err)
	}

	sources := PlayerSources{Manifest: normalizeURL(untagString(props["manifest"]))}
	if sources.Manifest == "" {
		return PlayerSources{}, fmt.Errorf("kickassanime: player props carry no manifest")
	}

	// subtitles is a tagged array of tagged objects.
	var tracks []json.RawMessage
	if err := json.Unmarshal(untag(props["subtitles"]), &tracks); err == nil {
		for _, track := range tracks {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(untag(track), &fields); err != nil {
				continue
			}
			url := normalizeURL(untagString(fields["src"]))
			if url == "" {
				continue
			}
			sources.Subtitles = append(sources.Subtitles, SubtitleTrack{
				Language: untagString(fields["language"]),
				Name:     untagString(fields["name"]),
				Format:   untagString(fields["format"]),
				URL:      url,
			})
		}
	}
	return sources, nil
}

// normalizeURL repairs the two malformed shapes this host emits: a
// protocol-relative manifest ("//host/path"), and subtitle links that carry an
// empty authority ("https:///host/path"), which no HTTP client will resolve.
func normalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	switch {
	case raw == "":
		return ""
	case strings.HasPrefix(raw, "//"):
		return "https:" + raw
	case strings.HasPrefix(raw, "https:///"):
		return "https://" + strings.TrimPrefix(raw, "https:///")
	case strings.HasPrefix(raw, "http:///"):
		return "http://" + strings.TrimPrefix(raw, "http:///")
	default:
		return raw
	}
}

// playerOrigin returns the scheme and host of a server's embed URL, which is
// what its CDN expects to see in Origin. Derived from the URL the API gave us
// rather than hardcoded, so a change of player host does not silently start
// producing streams that resolve but will not play.
func playerOrigin(src string) string {
	parsed, err := url.Parse(strings.TrimSpace(src))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

// englishSubtitle picks the track to hand MPV. otakase plays subtitled releases
// with English text, and the host lists a dozen languages per episode.
func englishSubtitle(tracks []SubtitleTrack) string {
	for _, track := range tracks {
		if strings.EqualFold(track.Language, "en") || strings.EqualFold(track.Language, "eng") {
			return track.URL
		}
	}
	return ""
}

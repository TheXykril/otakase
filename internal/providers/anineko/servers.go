package anineko

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/thexykril/otakase/internal/providers/substyle"
)

var (
	// A server list opens with its language: hsub is burned-in subtitles,
	// sub is soft subtitles carried in the embed's query, dub is dubbed.
	serverListRE = regexp.MustCompile(`data-id="(hsub|sub|dub)"`)
	dataVideoRE  = regexp.MustCompile(`data-video="([^"]+)"`)

	subtitleParamRE = regexp.MustCompile(`[?&](?:sub|caption_1|c1_file)=([^&"]+)`)
	pageSubtitleRE  = regexp.MustCompile(`const subtitle = "([^"]+)"`)
	trackFileRE     = regexp.MustCompile(`file:\s*"([^"]+\.(?:vtt|ass|srt))"`)
)

// subtitleParams are the query keys the players read a subtitle file from.
var subtitleParams = []string{"sub", "caption_1", "c1_file"}

// serverGroups maps each language on a watch page to its embed URLs, in page
// order. A list runs until the next one starts.
func serverGroups(page string) map[string][]string {
	groups := map[string][]string{}
	seen := map[string]bool{}
	lists := serverListRE.FindAllStringSubmatchIndex(page, -1)
	for i, list := range lists {
		lang := page[list[2]:list[3]]
		end := len(page)
		if i+1 < len(lists) {
			end = lists[i+1][0]
		}
		for _, m := range dataVideoRE.FindAllStringSubmatch(page[list[1]:end], -1) {
			embed := strings.TrimSpace(m[1])
			if embed == "" || seen[lang+" "+embed] {
				continue
			}
			seen[lang+" "+embed] = true
			groups[lang] = append(groups[lang], embed)
		}
	}
	return groups
}

// pickServers returns the embeds to try for a mode. In sub mode the episode
// may come with soft subs, hard subs or both; substyle settles which.
func pickServers(groups map[string][]string, mode, subStyle string) ([]string, error) {
	if mode == "dub" {
		return groups["dub"], nil
	}
	style, err := substyle.Choose(len(groups["sub"]) > 0, len(groups["hsub"]) > 0, subStyle)
	if err != nil {
		return nil, err
	}
	switch style {
	case "soft":
		return append([]string(nil), groups["sub"]...), nil
	case "hard":
		return append([]string(nil), groups["hsub"]...), nil
	}
	return nil, nil
}

// embedHost names the player behind an embed URL, or "" for one we cannot
// resolve. vibeplayer moves between domains; vivibebe is one of them.
func embedHost(embed string) string {
	u, err := url.Parse(embed)
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(u.Host))
	switch {
	case host == "":
		return ""
	case strings.Contains(host, "bibiemb."):
		return "bibiemb"
	case strings.Contains(host, "vibeplayer."), strings.Contains(host, "vivibebe."),
		strings.HasPrefix(host, "vibe") && strings.Contains(host, "be."):
		return "vibeplayer"
	}
	return ""
}

// subtitleFor finds the soft subtitle for an embed: in its query string when
// the site passed it along, otherwise in the player page itself.
func subtitleFor(embed, page string) string {
	if sub := subtitleFromQuery(embed); sub != "" {
		return sub
	}
	if m := pageSubtitleRE.FindStringSubmatch(page); m != nil {
		if sub := strings.TrimSpace(m[1]); sub != "" {
			return sub
		}
	}
	if m := trackFileRE.FindStringSubmatch(page); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func subtitleFromQuery(embed string) string {
	if u, err := url.Parse(embed); err == nil {
		query := u.Query()
		for _, name := range subtitleParams {
			for key, values := range query {
				if strings.EqualFold(key, name) {
					return unescape(values[0])
				}
			}
		}
	}
	// A query url.Parse gives up on can still carry the parameter.
	if m := subtitleParamRE.FindStringSubmatch(embed); m != nil {
		return unescape(m[1])
	}
	return ""
}

// unescape decodes a subtitle URL that arrives escaped once more than the
// query itself.
func unescape(s string) string {
	if decoded, err := url.QueryUnescape(s); err == nil {
		return decoded
	}
	return s
}

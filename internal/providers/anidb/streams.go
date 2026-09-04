package anidb

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/wraient/curd/internal/providers"
)

// language holds one entry of /api/frontend/episode/<id>/languages. Sub tracks are
// tagged "jpn" and dub tracks "eng".
type language struct {
	Language string `json:"language"`
	Name     string `json:"name"`
	EmbedURL string `json:"embed_url"`
}

type languageEnvelope struct {
	Languages []language `json:"languages"`
	Data      []language `json:"data"`
	Results   []language `json:"results"`
}

var (
	// The embed page assigns the master playlist as `file: '<url>'`.
	embedFileRE = regexp.MustCompile(`file:\s*['"]([^'"]+)['"]`)
	// Variant streams in the master playlist, for quality ordering.
	streamResolutionRE = regexp.MustCompile(`RESOLUTION=\d+x(\d+)`)
)

func decodeLanguages(raw []byte) ([]language, error) {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty language response")
	}

	var list []language
	if err := json.Unmarshal(raw, &list); err == nil && len(list) > 0 {
		return list, nil
	}

	var envelope languageEnvelope
	if err := json.Unmarshal(raw, &envelope); err == nil {
		for _, candidate := range [][]language{envelope.Languages, envelope.Data, envelope.Results} {
			if len(candidate) > 0 {
				return candidate, nil
			}
		}
	}

	return nil, fmt.Errorf("could not decode anidb language list")
}

// languageTag maps Curd's sub/dub mode onto anidb's track tags.
func languageTag(mode string) string {
	if providers.NormalizeTranslationType(mode) == "dub" {
		return "eng"
	}
	return "jpn"
}

// embedURLForMode picks the embed matching the requested audio track.
func embedURLForMode(languages []language, mode string) string {
	want := languageTag(mode)
	for _, entry := range languages {
		haystack := strings.ToLower(entry.Language + " " + entry.Name)
		if strings.Contains(haystack, want) && strings.TrimSpace(entry.EmbedURL) != "" {
			return strings.TrimSpace(entry.EmbedURL)
		}
	}
	return ""
}

func getEpisodeURLForMode(showID string, epNo int, mode string) ([]string, map[string]providers.StreamPlaybackHint, error) {
	episodeID, err := episodeIDFor(showID, epNo)
	if err != nil {
		return nil, nil, err
	}

	body, err := fetchString(fmt.Sprintf("%s/api/frontend/episode/%s/languages", baseURL, episodeID), baseURL+"/anime/"+showID)
	if err != nil {
		return nil, nil, err
	}

	languages, err := decodeLanguages([]byte(body))
	if err != nil {
		return nil, nil, err
	}

	embedURL := embedURLForMode(languages, mode)
	if embedURL == "" {
		return nil, nil, fmt.Errorf("anidb has no %s track for episode %d", providers.NormalizeTranslationType(mode), epNo)
	}

	embedPage, err := fetchString(embedURL, baseURL+"/")
	if err != nil {
		return nil, nil, err
	}

	match := embedFileRE.FindStringSubmatch(embedPage)
	if len(match) < 2 {
		return nil, nil, fmt.Errorf("no playlist found in anidb embed page")
	}
	master := strings.TrimSpace(match[1])
	if master == "" {
		return nil, nil, fmt.Errorf("empty playlist URL in anidb embed page")
	}

	links := resolveVariants(master, embedURL)
	hints := make(map[string]providers.StreamPlaybackHint, len(links))
	for _, link := range links {
		hints[link] = providers.StreamPlaybackHint{Referrer: embedURL}
	}
	return links, hints, nil
}

// resolveVariants expands a master playlist into its variant streams ordered by
// descending resolution, falling back to the master itself when it cannot be read
// or is already a media playlist. MPV can play the master directly, so a failure
// here is never fatal.
func resolveVariants(master, referer string) []string {
	body, err := fetchString(master, referer)
	if err != nil || !strings.Contains(body, "#EXT-X-STREAM-INF") {
		return []string{master}
	}

	type variant struct {
		url    string
		height int
	}

	var variants []variant
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "#EXT-X-STREAM-INF") {
			continue
		}
		height := 0
		if match := streamResolutionRE.FindStringSubmatch(line); len(match) > 1 {
			height, _ = strconv.Atoi(match[1])
		}
		for _, candidate := range lines[i+1:] {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" || strings.HasPrefix(candidate, "#") {
				continue
			}
			variants = append(variants, variant{url: resolveRelative(master, candidate), height: height})
			break
		}
	}

	if len(variants) == 0 {
		return []string{master}
	}

	sort.SliceStable(variants, func(i, j int) bool {
		return variants[i].height > variants[j].height
	})

	links := make([]string, 0, len(variants)+1)
	seen := make(map[string]struct{}, len(variants))
	for _, entry := range variants {
		if _, exists := seen[entry.url]; exists {
			continue
		}
		seen[entry.url] = struct{}{}
		links = append(links, entry.url)
	}
	// Keep the master as a last resort so playback still has something to try if
	// every variant URL turns out to be stale.
	links = append(links, master)
	return links
}

// resolveRelative resolves a playlist entry against its master playlist URL.
func resolveRelative(master, reference string) string {
	if strings.HasPrefix(reference, "http://") || strings.HasPrefix(reference, "https://") {
		return reference
	}
	if idx := strings.LastIndex(master, "/"); idx > 0 {
		return master[:idx+1] + strings.TrimPrefix(reference, "/")
	}
	return reference
}

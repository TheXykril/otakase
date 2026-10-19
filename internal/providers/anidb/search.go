package anidb

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/thexykril/otakase/internal/providers"
)

// anidb.app renders browse results server-side as anchors of the form
//
//	<a href="/anime/one-piece-21"><img alt="One Piece" src="..."></a>
//
// so the show id is the trailing numeric segment of the slug.
var (
	searchAnchorRE = regexp.MustCompile(`(?s)<a[^>]+href="[^"]*?/anime/([a-z0-9-]+-\d+)"[^>]*>.*?alt="([^"]*)"`)
	posterRE       = regexp.MustCompile(`src="([^"]+)"`)
)

func searchAnime(query, mode string) ([]providers.SelectionOption, error) {
	_ = mode

	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("empty search query")
	}

	body, err := fetchString(baseURL+"/browse?q="+url.QueryEscape(query), baseURL+"/")
	if err != nil {
		return nil, err
	}

	// Anchors are split apart first so a greedy match cannot span two results.
	chunks := strings.Split(body, "<a href")
	options := make([]providers.SelectionOption, 0, len(chunks))
	seen := make(map[string]struct{}, len(chunks))

	for _, chunk := range chunks {
		match := searchAnchorRE.FindStringSubmatch("<a href" + chunk)
		if len(match) < 3 {
			continue
		}
		slug := strings.TrimSpace(match[1])
		title := strings.TrimSpace(html.UnescapeString(match[2]))
		if slug == "" || title == "" {
			continue
		}
		if _, exists := seen[slug]; exists {
			continue
		}
		seen[slug] = struct{}{}

		thumbnail := ""
		if poster := posterRE.FindStringSubmatch(chunk); len(poster) > 1 {
			thumbnail = absoluteURL(poster[1])
		}

		options = append(options, providers.SelectionOption{
			Key:       slug,
			Label:     title,
			Title:     title,
			Thumbnail: thumbnail,
		})
	}

	if len(options) == 0 {
		return nil, fmt.Errorf("no results for %q", query)
	}
	return options, nil
}

// showIDFromSlug extracts the numeric anime id from a "title-words-1234" slug.
func showIDFromSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	if idx := strings.LastIndex(slug, "-"); idx >= 0 && idx+1 < len(slug) {
		return slug[idx+1:]
	}
	return slug
}

func absoluteURL(path string) string {
	path = strings.TrimSpace(html.UnescapeString(path))
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if strings.HasPrefix(path, "//") {
		return "https:" + path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return baseURL + path
}

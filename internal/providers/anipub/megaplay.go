package anipub

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/thexykril/otakase/internal/providers"
)

var (
	// anipub serves two episode link shapes. The legacy one embeds a megaplay
	// stream id directly; newer entries point at an anipub /play/ page keyed by MAL
	// id and episode number, which fronts a different megaplay route.
	videoPathRE = regexp.MustCompile(`/video/(\d+)/(sub|dub)`)
	playPathRE  = regexp.MustCompile(`/play/(\d+)/(\d+)/(sub|dub)`)
	dataIDRE    = regexp.MustCompile(`data-id="(\d+)"`)
)

// megaplayRoute is the megaplay stream page backing one anipub episode link.
type megaplayRoute struct {
	// path is the megaplay stream page path, minus the trailing sub/dub segment.
	path string
	mode string
}

func resolveMegaplayStream(videoLink, mode string) (string, string, error) {
	videoLink = strings.TrimSpace(videoLink)
	if videoLink == "" {
		return "", "", fmt.Errorf("empty video link")
	}

	route, err := parseVideoLink(videoLink)
	if err != nil {
		return "", "", err
	}
	mode = providers.NormalizeTranslationType(mode)
	linkMode := "sub"
	if mode == "dub" {
		linkMode = "dub"
	}

	streamPage := fmt.Sprintf("%s/%s/%s", megaplayBaseURL, route.path, linkMode)
	html, err := fetchString(streamPage, baseURL+"/")
	if err != nil {
		return "", "", err
	}

	dataID := dataIDRE.FindStringSubmatch(html)
	if len(dataID) < 2 {
		return "", "", fmt.Errorf("megaplay data-id not found")
	}

	sourcesURL := fmt.Sprintf("%s/stream/getSources?id=%s", megaplayBaseURL, dataID[1])
	var payload megaplaySourcesResponse
	if err := fetchJSON(sourcesURL, streamPage, &payload); err != nil {
		return "", "", err
	}

	streamURL := strings.TrimSpace(payload.Sources.File)
	if streamURL == "" {
		return "", "", fmt.Errorf("megaplay stream url missing")
	}
	subtitle := pickSubtitleTrack(payload, mode)
	return streamURL, subtitle, nil
}

// parseVideoLink maps an anipub episode link onto its megaplay stream route.
func parseVideoLink(videoLink string) (megaplayRoute, error) {
	parsed, err := url.Parse(videoLink)
	if err != nil {
		return megaplayRoute{}, fmt.Errorf("parse video link: %w", err)
	}

	// Legacy: /video/{embedId}/{mode} -> /stream/s-2/{embedId}/{mode}
	if matches := videoPathRE.FindStringSubmatch(parsed.Path); len(matches) >= 3 {
		embedID := matches[1]
		if _, err := strconv.Atoi(embedID); err != nil {
			return megaplayRoute{}, fmt.Errorf("invalid embed id %q", embedID)
		}
		return megaplayRoute{path: "stream/s-2/" + embedID, mode: matches[2]}, nil
	}

	// Newer: /play/{malId}/{episode}/{mode} -> /stream/mal/{malId}/{episode}/{mode}
	if matches := playPathRE.FindStringSubmatch(parsed.Path); len(matches) >= 4 {
		malID, episode := matches[1], matches[2]
		if _, err := strconv.Atoi(malID); err != nil {
			return megaplayRoute{}, fmt.Errorf("invalid mal id %q", malID)
		}
		if _, err := strconv.Atoi(episode); err != nil {
			return megaplayRoute{}, fmt.Errorf("invalid episode number %q", episode)
		}
		return megaplayRoute{path: "stream/mal/" + malID + "/" + episode, mode: matches[3]}, nil
	}

	return megaplayRoute{}, fmt.Errorf("unsupported video link %q", videoLink)
}

func pickSubtitleTrack(payload megaplaySourcesResponse, mode string) string {
	if mode == "dub" {
		return ""
	}
	var fallback string
	for _, track := range payload.Tracks {
		file := strings.TrimSpace(track.File)
		if file == "" || !strings.EqualFold(strings.TrimSpace(track.Kind), "captions") {
			continue
		}
		label := strings.ToLower(strings.TrimSpace(track.Label))
		if track.Default || strings.Contains(label, "english") {
			return file
		}
		if fallback == "" {
			fallback = file
		}
	}
	return fallback
}

package anipub

import (
	"errors"
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
	videoPathRE = regexp.MustCompile(`/video/(\d+)/(sub|dub)`)      // embed id, mode
	playPathRE  = regexp.MustCompile(`/play/(\d+)/(\d+)/(sub|dub)`) // MAL id, episode, mode

	// The stream page keeps megaplay's internal id for the episode on the
	// player element; getSources is asked by that id.
	playerIDRE = regexp.MustCompile(`data-id="(\d+)"`)
)

// megaplayRoute is the megaplay stream page backing one anipub episode link.
type megaplayRoute struct {
	// path is the megaplay stream page path, minus the trailing sub/dub segment.
	path string
	mode string
}

// megaplaySourcesResponse is what getSources answers.
type megaplaySourcesResponse struct {
	Sources struct {
		File string `json:"file"`
	} `json:"sources"`
	Tracks []struct {
		File    string `json:"file"`
		Label   string `json:"label"`
		Kind    string `json:"kind"`
		Default bool   `json:"default"`
	} `json:"tracks"`
}

// resolveMegaplayStream returns the stream behind an anipub episode link, the
// caption to load first and every caption track on offer. The mode asked for
// wins over the one in the link: the same route serves both.
func resolveMegaplayStream(link, mode string) (string, string, []providers.SubtitleTrack, error) {
	link = strings.TrimSpace(link)
	if link == "" {
		return "", "", nil, errors.New("empty video link")
	}
	route, err := parseVideoLink(link)
	if err != nil {
		return "", "", nil, err
	}
	audio := providers.NormalizeTranslationType(mode)

	streamPage := fmt.Sprintf("%s/%s/%s", megaplayBaseURL, route.path, audio)
	page, err := get(streamPage, baseURL+"/", "")
	if err != nil {
		return "", "", nil, err
	}
	id := playerIDRE.FindSubmatch(page)
	if id == nil {
		return "", "", nil, errors.New("megaplay data-id not found")
	}

	var sources megaplaySourcesResponse
	if err := getJSON(megaplayBaseURL+"/stream/getSources?id="+string(id[1]), streamPage, &sources); err != nil {
		return "", "", nil, err
	}
	stream := strings.TrimSpace(sources.Sources.File)
	if stream == "" {
		return "", "", nil, errors.New("megaplay stream url missing")
	}
	return stream, defaultSubtitle(sources, audio), subtitleTracks(sources, audio), nil
}

// parseVideoLink maps an anipub episode link onto its megaplay stream route.
func parseVideoLink(link string) (megaplayRoute, error) {
	u, err := url.Parse(link)
	if err != nil {
		return megaplayRoute{}, fmt.Errorf("parse video link: %w", err)
	}

	// Legacy: /video/{embedId}/{mode} -> /stream/s-2/{embedId}/{mode}
	if matches := videoPathRE.FindStringSubmatch(u.Path); len(matches) >= 3 {
		embedID := matches[1]
		if _, err := strconv.Atoi(embedID); err != nil {
			return megaplayRoute{}, fmt.Errorf("invalid embed id %q", embedID)
		}
		return megaplayRoute{path: "stream/s-2/" + embedID, mode: matches[2]}, nil
	}

	// Newer: /play/{malId}/{episode}/{mode} -> /stream/mal/{malId}/{episode}/{mode}
	if matches := playPathRE.FindStringSubmatch(u.Path); len(matches) >= 4 {
		malID, episode := matches[1], matches[2]
		if _, err := strconv.Atoi(malID); err != nil {
			return megaplayRoute{}, fmt.Errorf("invalid mal id %q", malID)
		}
		if _, err := strconv.Atoi(episode); err != nil {
			return megaplayRoute{}, fmt.Errorf("invalid episode number %q", episode)
		}
		return megaplayRoute{path: "stream/mal/" + malID + "/" + episode, mode: matches[3]}, nil
	}

	return megaplayRoute{}, fmt.Errorf("unsupported video link %q", link)
}

// subtitleTracks lists every caption track the host offers, so the player can
// pick a language other than the one chosen by default.
func subtitleTracks(payload megaplaySourcesResponse, mode string) []providers.SubtitleTrack {
	if mode == "dub" {
		return nil
	}
	var tracks []providers.SubtitleTrack
	for _, track := range payload.Tracks {
		file := captionFile(track.File, track.Kind)
		if file == "" {
			continue
		}
		label := strings.TrimSpace(track.Label)
		tracks = append(tracks, providers.SubtitleTrack{URL: file, Language: label, Label: label})
	}
	return tracks
}

// defaultSubtitle is the caption loaded first: the track megaplay marks as
// default or an English one, whichever comes first, else the first caption.
func defaultSubtitle(payload megaplaySourcesResponse, mode string) string {
	if mode == "dub" {
		return ""
	}
	first := ""
	for _, t := range payload.Tracks {
		file := captionFile(t.File, t.Kind)
		switch {
		case file == "":
		case t.Default || strings.Contains(strings.ToLower(t.Label), "english"):
			return file
		case first == "":
			first = file
		}
	}
	return first
}

// captionFile is a track's URL when it is a caption track that has one,
// else "".
func captionFile(file, kind string) string {
	if !strings.EqualFold(strings.TrimSpace(kind), "captions") {
		return ""
	}
	return strings.TrimSpace(file)
}

// Package anipub resolves streams from anipub.xyz.
//
// anipub has a JSON API for search, show info and episode links. The links
// lead to megaplay.buzz, which serves the HLS stream and its captions.
package anipub

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/thexykril/otakase/internal/providerhost"
	"github.com/thexykril/otakase/internal/providers"
	"github.com/thexykril/otakase/internal/providers/substyle"
)

func init() {
	providers.Register(providers.Meta{
		Name:     "anipub",
		Aliases:  []string{"anipub.xyz"},
		Referrer: "https://megaplay.buzz/",
	}, func() providers.Provider { return &Provider{} })
}

// Provider resolves anipub.xyz.
type Provider struct{}

func (p *Provider) Name() string { return "anipub" }

// SearchItem is stored in SelectionOption.ExtraData for mapping hints.
type SearchItem struct {
	ID       int
	MalID    int
	Name     string
	Finder   string
	Episodes int
}

type searchHit struct {
	Name   string `json:"Name"`
	ID     int    `json:"Id"`
	Image  string `json:"Image"`
	Finder string `json:"finder"`
}

type showInfo struct {
	ID      int    `json:"_id"`
	Name    string `json:"Name"`
	MALID   string `json:"MALID"`
	EpCount int    `json:"epCount"`
	Image   string `json:"ImagePath"`
	Cover   string `json:"Cover"`
}

// showDetails holds a show's episode links. The first episode's link stands
// apart from the rest: Link is episode 1 and Ep[i] is episode i+2.
type showDetails struct {
	Local struct {
		Link string `json:"link"`
		Ep   []struct {
			Link string `json:"link"`
		} `json:"ep"`
	} `json:"local"`
}

// SearchAnime searches by title and fills in each hit's MAL id and episode
// count from its info page. anipub lists sub and dub together, so mode plays
// no part.
func (p *Provider) SearchAnime(query, mode string) ([]providers.SelectionOption, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("empty search query")
	}
	body, err := get(baseURL+"/api/search/"+url.PathEscape(query), baseURL+"/", "")
	if err != nil {
		return nil, err
	}
	hits, err := parseSearch(body)
	if err != nil {
		return nil, err
	}

	infos := lookupInfos(hits)
	var results []providers.SelectionOption
	for i, hit := range hits {
		if hit.ID <= 0 {
			continue
		}
		info := infos[i]
		name := strings.TrimSpace(hit.Name)
		label := name
		if info.EpCount > 0 {
			label += fmt.Sprintf(" · %d eps", info.EpCount)
		}
		malID, _ := strconv.Atoi(strings.TrimSpace(info.MALID))
		results = append(results, providers.SelectionOption{
			Key:       strconv.Itoa(hit.ID),
			Label:     label,
			Title:     name,
			Thumbnail: firstNonEmpty(info.Image, info.Cover, hit.Image),
			ExtraData: SearchItem{
				ID:       hit.ID,
				MalID:    malID,
				Name:     name,
				Finder:   strings.TrimSpace(hit.Finder),
				Episodes: info.EpCount,
			},
		})
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no results for %q", query)
	}
	return results, nil
}

// parseSearch reads a search answer. It comes in three shapes: a list of
// hits, a single hit as a bare object, or {"found":false}.
func parseSearch(body []byte) ([]searchHit, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, errors.New("empty search response")
	}

	// anipub answers HTTP 200 with {"error":"Too many requests, ..."} when it is
	// throttling. Falling through to the parse path below reported that as
	// "parse anipub search response", which reads like a bug in otakase and, worse,
	// is not retried because it does not look transient.
	if providerhost.IsRateLimitedBody(body) {
		return nil, fmt.Errorf("anipub: %w", providerhost.ErrRateLimited)
	}

	var missing struct {
		Found bool `json:"found"`
	}
	if bytes.Contains(body, []byte(`"found"`)) && json.Unmarshal(body, &missing) == nil && !missing.Found {
		return nil, nil
	}
	var hits []searchHit
	if json.Unmarshal(body, &hits) == nil {
		return hits, nil
	}
	var hit searchHit
	if json.Unmarshal(body, &hit) == nil && hit.ID > 0 {
		return []searchHit{hit}, nil
	}
	return nil, errors.New("parse anipub search response")
}

// lookupInfos fetches the info page of every hit at once. A hit whose info
// fails keeps a zero value; it is still listed, only without the extras.
func lookupInfos(hits []searchHit) []showInfo {
	infos := make([]showInfo, len(hits))
	var wg sync.WaitGroup
	for i, hit := range hits {
		if hit.ID <= 0 {
			continue
		}
		wg.Add(1)
		go func(i, id int) {
			defer wg.Done()
			var info showInfo
			if getJSON(fmt.Sprintf("%s/api/info/%d", baseURL, id), baseURL+"/", &info) == nil {
				infos[i] = info
			}
		}(i, hit.ID)
	}
	wg.Wait()
	return infos
}

// EpisodesList numbers the show's episode links from 1.
func (p *Provider) EpisodesList(showID, mode string) ([]string, error) {
	details, err := fetchDetails(showID)
	if err != nil {
		return nil, err
	}
	count := len(details.Local.Ep) + 1
	episodes := make([]string, count)
	for i := range episodes {
		episodes[i] = strconv.Itoa(i + 1)
	}
	return episodes, nil
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
	if _, err := parseShowID(id); err != nil {
		return nil, nil, err
	}
	if epNo <= 0 {
		return nil, nil, fmt.Errorf("invalid episode number %d", epNo)
	}
	details, err := fetchDetails(id)
	if err != nil {
		return nil, nil, err
	}
	link, err := episodeLink(details, epNo)
	if err != nil {
		return nil, nil, err
	}

	mode = providers.NormalizeTranslationType(mode)
	if mode == "sub" {
		// megaplay only has soft subs; this asks a user set on hard subs
		// whether soft ones will do.
		if _, err := substyle.Choose(true, false, config.SubStyle); err != nil {
			return nil, nil, err
		}
	}
	streamURL, subtitle, tracks, err := resolveMegaplayStream(link, mode)
	if err != nil {
		return nil, nil, err
	}

	// Reject ad-injected decoy playlists served by the megaplay CDN so the
	// caller can fall back to another provider instead of opening an idle mpv.
	if err := validateResolvedStream(streamURL); err != nil {
		providerhost.Log(fmt.Sprintf("anipub stream %q rejected: %v", streamURL, err))
		return nil, nil, err
	}

	hints := map[string]providers.StreamPlaybackHint{
		streamURL: {
			Referrer:  megaplayBaseURL + "/",
			Subtitle:  subtitle,
			Subtitles: tracks,
		},
	}
	return []string{streamURL}, hints, nil
}

func fetchDetails(showID string) (showDetails, error) {
	id, err := parseShowID(showID)
	if err != nil {
		return showDetails{}, err
	}
	var details showDetails
	err = getJSON(baseURL+"/v1/api/details/"+id, baseURL+"/", &details)
	return details, err
}

// episodeLink returns the link of episode epNo. Links come prefixed "src=".
func episodeLink(details showDetails, epNo int) (string, error) {
	var link string
	switch {
	case epNo == 1:
		link = details.Local.Link
	case epNo >= 2 && epNo-2 < len(details.Local.Ep):
		link = details.Local.Ep[epNo-2].Link
	default:
		return "", fmt.Errorf("episode %d not found", epNo)
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(link), "src=")), nil
}

// parseShowID checks the id is anipub's numeric one. A slug from another
// provider can reach here through saved history; it fails here with a clear
// message rather than as a 404 from the API.
func parseShowID(showID string) (string, error) {
	showID = strings.TrimSpace(showID)
	if showID == "" {
		return "", errors.New("empty show id")
	}
	if _, err := strconv.Atoi(showID); err != nil {
		return "", fmt.Errorf("invalid anipub show id %q", showID)
	}
	return showID, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

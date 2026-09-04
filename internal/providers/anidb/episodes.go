package anidb

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// episode is one entry of /api/frontend/anime/<id>/episodes. The payload is
// decoded permissively because the endpoint has returned both a bare array and an
// object wrapping one under different keys.
type episode struct {
	ID     json.Number `json:"id"`
	Number json.Number `json:"number"`
}

type episodeEnvelope struct {
	Episodes []episode `json:"episodes"`
	Data     []episode `json:"data"`
	Results  []episode `json:"results"`
}

func decodeEpisodes(raw []byte) ([]episode, error) {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty episode response")
	}

	var list []episode
	if err := json.Unmarshal(raw, &list); err == nil && len(list) > 0 {
		return list, nil
	}

	var envelope episodeEnvelope
	if err := json.Unmarshal(raw, &envelope); err == nil {
		for _, candidate := range [][]episode{envelope.Episodes, envelope.Data, envelope.Results} {
			if len(candidate) > 0 {
				return candidate, nil
			}
		}
	}

	return nil, fmt.Errorf("could not decode anidb episode list")
}

// fetchEpisodes returns the show's episodes sorted by episode number.
func fetchEpisodes(showID string) ([]episode, error) {
	id := showIDFromSlug(showID)
	if id == "" {
		return nil, fmt.Errorf("missing anidb show id")
	}

	body, err := fetchString(fmt.Sprintf("%s/api/frontend/anime/%s/episodes", baseURL, id), baseURL+"/anime/"+showID)
	if err != nil {
		return nil, err
	}

	episodes, err := decodeEpisodes([]byte(body))
	if err != nil {
		return nil, err
	}

	sort.SliceStable(episodes, func(i, j int) bool {
		return episodeNumber(episodes[i]) < episodeNumber(episodes[j])
	})
	return episodes, nil
}

func episodeNumber(ep episode) int {
	value, err := strconv.Atoi(strings.TrimSpace(ep.Number.String()))
	if err != nil {
		return 0
	}
	return value
}

func episodesList(showID, mode string) ([]string, error) {
	_ = mode

	episodes, err := fetchEpisodes(showID)
	if err != nil {
		return nil, err
	}

	numbers := make([]string, 0, len(episodes))
	for _, ep := range episodes {
		if number := episodeNumber(ep); number > 0 {
			numbers = append(numbers, strconv.Itoa(number))
		}
	}
	if len(numbers) == 0 {
		return nil, fmt.Errorf("no episodes found for %q", showID)
	}
	return numbers, nil
}

// episodeIDFor maps a display episode number to the internal episode id the
// stream endpoint expects.
func episodeIDFor(showID string, epNo int) (string, error) {
	episodes, err := fetchEpisodes(showID)
	if err != nil {
		return "", err
	}
	for _, ep := range episodes {
		if episodeNumber(ep) == epNo {
			id := strings.TrimSpace(ep.ID.String())
			if id == "" {
				return "", fmt.Errorf("anidb episode %d has no id", epNo)
			}
			return id, nil
		}
	}
	return "", fmt.Errorf("anidb has no episode %d for %q", epNo, showID)
}

package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
)

// skipTimesResponse struct to hold the response from the AniSkip API
type skipTimesResponse struct {
	Found   bool         `json:"found"`
	Results []skipResult `json:"results"`
}

// skipResult struct to hold individual skip result data
type skipResult struct {
	Interval skipInterval `json:"interval"`
	SkipType string       `json:"skip_type"`
	SkipID   string       `json:"skip_id"`
	// EpisodeLength is the length of the release the entry was timed against.
	// Only the v2 API reports it; zero means unknown.
	EpisodeLength float64 `json:"episode_length"`
}

// skipInterval struct to hold the start and end times for skip intervals
type skipInterval struct {
	StartTime float64 `json:"start_time"`
	EndTime   float64 `json:"end_time"`
}

// aniSkipReadBase is a variable so tests can point the read at a local server;
// nothing else replaces it.
var (
	aniSkipReadBase = "https://api.aniskip.com/v2/skip-times"
	aniSkipV1Base   = "https://api.aniskip.com/v1/skip-times"
)

// aniSkipLengthTolerance is how far an entry's recorded episode length may be
// from the file playing and still be trusted for it. Two encodes of the same
// cut differ by a second or two; a cut with a cold open, a recap or a sponsor
// card added differs by far more, and its timings belong to that cut.
const aniSkipLengthTolerance = 20.0

// aniSkipV2Response is the v2 answer, which unlike v1 says how long the
// episode was that each entry was timed against.
type aniSkipV2Response struct {
	Found   bool `json:"found"`
	Results []struct {
		Interval struct {
			StartTime float64 `json:"startTime"`
			EndTime   float64 `json:"endTime"`
		} `json:"interval"`
		SkipType      string  `json:"skipType"`
		SkipID        string  `json:"skipId"`
		EpisodeLength float64 `json:"episodeLength"`
	} `json:"results"`
}

// getAniSkipResults fetches every entry AniSkip has for an episode, with the
// episode length each was timed against.
//
// It asks v2 for all lengths and chooses locally, so the choice can be
// explained in a log and tested. When v2 cannot be used it falls back to v1,
// which answers without lengths -- the times are then taken as they come, as
// they always were.
func getAniSkipResults(malID, episode int) ([]skipResult, bool, error) {
	url := fmt.Sprintf("%s/%d/%d?types=op&types=ed&types=mixed-op&types=mixed-ed&episodeLength=0",
		aniSkipReadBase, malID, episode)
	resp, err := sharedHTTPClient.Get(url)
	if err == nil {
		defer resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusNotFound:
			return nil, false, nil
		case http.StatusOK:
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			if readErr != nil {
				return nil, false, fmt.Errorf("aniskip: %w", readErr)
			}
			var data aniSkipV2Response
			if jsonErr := json.Unmarshal(body, &data); jsonErr != nil {
				return nil, false, fmt.Errorf("aniskip: %w", jsonErr)
			}
			results := make([]skipResult, 0, len(data.Results))
			for _, r := range data.Results {
				results = append(results, skipResult{
					Interval:      skipInterval{StartTime: r.Interval.StartTime, EndTime: r.Interval.EndTime},
					SkipType:      r.SkipType,
					SkipID:        r.SkipID,
					EpisodeLength: r.EpisodeLength,
				})
			}
			return results, data.Found && len(results) > 0, nil
		}
		Log(fmt.Sprintf("aniskip v2: status %d; asking v1", resp.StatusCode))
	} else {
		Log(fmt.Sprintf("aniskip v2: %v; asking v1", err))
	}

	body, err := GetAniSkipData(malID, episode)
	if err != nil {
		return nil, false, err
	}
	var data skipTimesResponse
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		return nil, false, fmt.Errorf("aniskip: %w", err)
	}
	return data.Results, data.Found, nil
}

// aniSkipResultsForLength keeps the entries that fit a file of the given
// length, closest match first.
//
// AniSkip keeps entries for every release anyone timed, and the same episode is
// cut differently by different sources: one has a cold open before the
// opening, another does not. An entry for the other cut sends the player to
// the wrong place -- a skip from 0:00 to 1:00 in an episode whose opening
// starts at 2:45. With no length known, the entries are kept as they came.
func aniSkipResultsForLength(results []skipResult, length float64) []skipResult {
	if length <= 0 {
		return results
	}
	type ranked struct {
		result skipResult
		off    float64
	}
	kept := make([]ranked, 0, len(results))
	for _, result := range results {
		if result.Interval.EndTime > length+1 {
			// Runs past the end of this file: timed against a longer cut.
			continue
		}
		off := aniSkipLengthTolerance
		if result.EpisodeLength > 0 {
			off = math.Abs(result.EpisodeLength - length)
			if off > aniSkipLengthTolerance {
				continue
			}
		}
		kept = append(kept, ranked{result: result, off: off})
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].off < kept[j].off })

	out := make([]skipResult, 0, len(kept))
	for _, k := range kept {
		out = append(out, k.result)
	}
	return out
}

// GetAniSkipData fetches skip times data for a given anime ID and episode
func GetAniSkipData(animeMalId int, episode int) (string, error) {
	url := fmt.Sprintf("%s/%d/%d?types=op&types=ed", aniSkipV1Base, animeMalId, episode)

	resp, err := sharedHTTPClient.Get(url)
	if err != nil {
		Log(fmt.Errorf("error fetching data from AniSkip API: %w", err))
		return "", fmt.Errorf("error fetching data from AniSkip API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		Log(fmt.Sprintf("failed with status %d", resp.StatusCode))
		return "", fmt.Errorf("failed with status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		Log(fmt.Errorf("failed to read response body %w", err))
		return "", fmt.Errorf("failed to read response body %w", err)
	}

	return string(body), nil
}

// RoundTime rounds a time value to the specified precision
func RoundTime(timeValue float64, precision int) float64 {
	multiplier := math.Pow(10, float64(precision))
	return math.Floor(timeValue*multiplier+0.5) / multiplier
}

// ParseAniSkipResponse parses the response text from the AniSkip API and updates the Anime struct
func ParseAniSkipResponse(responseText string, anime *Anime, timePrecision int) error {
	if responseText == "" {
		return fmt.Errorf("response text is empty")
	}

	var data skipTimesResponse
	err := json.Unmarshal([]byte(responseText), &data)
	if err != nil {
		return fmt.Errorf("error unmarshalling response: %w", err)
	}

	if !data.Found {
		return fmt.Errorf("no skip times found")
	}

	// Each result says which it is. Taking the first as the opening and the
	// last as the ending, as this used to, turns an episode with only an
	// ending on file into one that skips the ending as though it were the
	// opening -- which sends the player to the credits a minute in.
	times, _ := aniSkipTimesFrom(data.Results, timePrecision)
	anime.Ep.SkipTimes = times
	return nil
}

// aniSkipTimesFrom sorts the results into an opening and an ending, keeping the
// id of each so it can be voted on later.
func aniSkipTimesFrom(results []skipResult, timePrecision int) (SkipTimes, SkipIDs) {
	times := SkipTimes{}
	ids := SkipIDs{}

	for _, result := range results {
		span := Skip{
			Start: int(RoundTime(result.Interval.StartTime, timePrecision)),
			End:   int(RoundTime(result.Interval.EndTime, timePrecision)),
		}
		switch strings.ToLower(strings.TrimSpace(result.SkipType)) {
		case "op", "mixed-op":
			if !usableSpan(times.Op) {
				times.Op = span
				ids.Op = result.SkipID
			}
		case "ed", "mixed-ed":
			if !usableSpan(times.Ed) {
				times.Ed = span
				ids.Ed = result.SkipID
			}
		}
	}
	return times, ids
}

// GetAndParseAniSkipData fetches and parses skip times for a given anime ID and episode
func GetAndParseAniSkipData(animeMalId int, episode int, timePrecision int, anime *Anime) error {
	responseText, err := GetAniSkipData(animeMalId, episode)
	if err != nil {
		return err
	}
	return ParseAniSkipResponse(responseText, anime, timePrecision)
}

// Function to send OP and ED timings to MPV
func SendSkipTimesToMPV(anime *Anime) error {
	// Casting resolves skip times with no player running, and a command sent
	// to an empty socket path is a round trip to nowhere that logged an error
	// every time.
	if anime == nil || anime.Ep.Player.SocketPath == "" {
		return nil
	}
	chapterList := []map[string]interface{}{
		{
			"title": "Pre-Opening",
			"time":  0.0,
			"end":   float64(anime.Ep.SkipTimes.Op.Start),
		},
		{
			"title": "Opening",
			"time":  float64(anime.Ep.SkipTimes.Op.Start),
			"end":   float64(anime.Ep.SkipTimes.Op.End),
		},
		{
			"title": "Main",
			"time":  float64(anime.Ep.SkipTimes.Op.End),
			"end":   float64(anime.Ep.SkipTimes.Ed.Start),
		},
		{
			"title": "Ending",
			"time":  float64(anime.Ep.SkipTimes.Ed.Start),
			"end":   float64(anime.Ep.SkipTimes.Ed.End),
		},
		{
			"title": "Post-Credits",
			"time":  float64(anime.Ep.SkipTimes.Ed.End),
		},
	}

	_, err := MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{
		"set_property",
		"chapter-list",
		chapterList,
	})
	if err != nil {
		return fmt.Errorf("error sending command to MPV: %w", err)
	}

	return nil
}

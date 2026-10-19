// Package aniskip reads opening, ending and recap times from api.aniskip.com.
package aniskip

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"time"
)

// Entry is one timed stretch of an episode.
type Entry struct {
	Kind  string // op, ed, mixed-op, mixed-ed or recap
	ID    string
	Start float64
	End   float64
	// Length is how long the release it was timed on runs, in seconds.
	// Zero when the answer did not say.
	Length float64
}

// Client talks to AniSkip. The zero value is not usable; start from New.
type Client struct {
	HTTP *http.Client
	V2   string
	V1   string
}

// New returns a client for the public API.
func New(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{
		HTTP: httpClient,
		V2:   "https://api.aniskip.com/v2/skip-times",
		V1:   "https://api.aniskip.com/v1/skip-times",
	}
}

// ErrBadAnswer is returned when the service answered with something unreadable.
var ErrBadAnswer = errors.New("aniskip: unreadable answer")

// Lookup returns every entry on file for an episode. An episode with nothing
// on file returns no entries and no error.
//
// v2 is asked first because it reports the length each entry was timed on.
// When v2 fails, v1 is asked; its entries come without a length.
func (c *Client) Lookup(malID, episode int) ([]Entry, error) {
	entries, err := c.lookupV2(malID, episode)
	if err == nil {
		return entries, nil
	}
	v1, v1Err := c.lookupV1(malID, episode)
	if v1Err != nil {
		return nil, fmt.Errorf("aniskip: v2: %v; v1: %w", err, v1Err)
	}
	return v1, nil
}

func (c *Client) lookupV2(malID, episode int) ([]Entry, error) {
	url := fmt.Sprintf("%s/%d/%d?types=op&types=ed&types=mixed-op&types=mixed-ed&types=recap&episodeLength=0",
		c.V2, malID, episode)
	var answer struct {
		Found   bool `json:"found"`
		Results []struct {
			Interval struct {
				Start float64 `json:"startTime"`
				End   float64 `json:"endTime"`
			} `json:"interval"`
			Kind   string  `json:"skipType"`
			ID     string  `json:"skipId"`
			Length float64 `json:"episodeLength"`
		} `json:"results"`
	}
	found, err := c.get(url, &answer)
	if err != nil || !found || !answer.Found {
		return nil, err
	}
	entries := make([]Entry, 0, len(answer.Results))
	for _, r := range answer.Results {
		entries = append(entries, Entry{Kind: r.Kind, ID: r.ID, Start: r.Interval.Start, End: r.Interval.End, Length: r.Length})
	}
	return entries, nil
}

func (c *Client) lookupV1(malID, episode int) ([]Entry, error) {
	url := fmt.Sprintf("%s/%d/%d?types=op&types=ed", c.V1, malID, episode)
	var answer struct {
		Found   bool `json:"found"`
		Results []struct {
			Interval struct {
				Start float64 `json:"start_time"`
				End   float64 `json:"end_time"`
			} `json:"interval"`
			Kind string `json:"skip_type"`
			ID   string `json:"skip_id"`
		} `json:"results"`
	}
	found, err := c.get(url, &answer)
	if err != nil || !found || !answer.Found {
		return nil, err
	}
	entries := make([]Entry, 0, len(answer.Results))
	for _, r := range answer.Results {
		entries = append(entries, Entry{Kind: r.Kind, ID: r.ID, Start: r.Interval.Start, End: r.Interval.End})
	}
	return entries, nil
}

// get decodes a JSON answer into out. A 404 is "nothing on file": found is
// false and err is nil.
func (c *Client) get(url string, out any) (found bool, err error) {
	resp, err := c.HTTP.Get(url)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return false, fmt.Errorf("%w: %v", ErrBadAnswer, err)
	}
	return true, nil
}

// LengthTolerance is how far, in seconds, an entry's release may run from the
// file being played and still fit it. Encodes of one cut differ by a second or
// two; a cut with a cold open or sponsor card differs by much more.
const LengthTolerance = 20.0

// Fit keeps the entries that suit a file of the given length, the closest in
// length first. Entries that run past the end of the file are dropped. With
// no length known, the entries come back unchanged.
func Fit(entries []Entry, length float64) []Entry {
	if length <= 0 {
		return entries
	}
	type candidate struct {
		entry Entry
		gap   float64
	}
	var kept []candidate
	for _, e := range entries {
		if e.End > length+1 {
			continue
		}
		gap := LengthTolerance
		if e.Length > 0 {
			gap = math.Abs(e.Length - length)
			if gap > LengthTolerance {
				continue
			}
		}
		kept = append(kept, candidate{e, gap})
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].gap < kept[j].gap })
	out := make([]Entry, len(kept))
	for i, c := range kept {
		out[i] = c.entry
	}
	return out
}

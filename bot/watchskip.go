package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The Watch Together controls use real skip intervals from AniSkip, never a
// guessed number of seconds. Lookup is optional: missing data means no skip.
const watchSkipLengthTolerance = 20.0

type watchSkipSpan struct {
	Start  float64 `json:"start"`
	End    float64 `json:"end"`
	length float64 // length of the release that the timings describe
}

type watchSkipTimes struct {
	OP watchSkipSpan `json:"op"`
	ED watchSkipSpan `json:"ed"`
}

type watchSkipIntervals struct {
	OP []watchSkipSpan
	ED []watchSkipSpan
}

func (span watchSkipSpan) valid() bool {
	return !math.IsNaN(span.Start) && !math.IsNaN(span.End) &&
		!math.IsInf(span.Start, 0) && !math.IsInf(span.End, 0) &&
		span.Start >= 0 && span.End > span.Start && span.End-span.Start <= 600
}

func bestWatchSkipSpan(spans []watchSkipSpan, duration float64) watchSkipSpan {
	best := watchSkipSpan{}
	gap := math.Inf(1)
	if duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return best
	}
	for _, span := range spans {
		if !span.valid() || span.End > duration+1 {
			continue
		}
		difference := watchSkipLengthTolerance
		if span.length > 0 {
			difference = math.Abs(span.length - duration)
			if difference > watchSkipLengthTolerance {
				continue
			}
		}
		if difference < gap {
			gap, best = difference, span
		}
	}
	return best
}

// The resolved timings may describe different releases of an episode. Use
// the closest fit only after the host reports its actual video duration.
func (intervals watchSkipIntervals) match(duration float64) watchSkipTimes {
	return watchSkipTimes{OP: bestWatchSkipSpan(intervals.OP, duration), ED: bestWatchSkipSpan(intervals.ED, duration)}
}

// Returns the true end of the opening/ending the room is currently within.
// Unknown intervals, cut mismatches and positions outside a span do nothing.
func watchSkipTarget(times watchSkipTimes, pos, duration float64, op, ed bool) (float64, bool) {
	if duration <= 0 || math.IsNaN(pos) || math.IsInf(pos, 0) {
		return 0, false
	}
	for _, candidate := range []struct {
		on   bool
		span watchSkipSpan
	}{{op, times.OP}, {ed, times.ED}} {
		s := candidate.span
		if !candidate.on || !s.valid() || s.End > duration+1 || pos < s.Start || pos >= s.End {
			continue
		}
		target := math.Min(s.End, duration-0.5)
		if target > pos+0.1 {
			return target, true
		}
	}
	return 0, false
}

var (
	watchSkipAPI = "https://api.aniskip.com"
	watchSkipHTTP = &http.Client{Timeout: 8 * time.Second}
	watchSkipLookup = lookupWatchSkip
)

func fetchWatchSkip(ctx context.Context, route string, answer any) (bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(watchSkipAPI, "/")+route, nil)
	if err != nil {
		return false, err
	}
	resp, err := watchSkipHTTP.Do(request)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("AniSkip status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(body, answer); err != nil {
		return false, fmt.Errorf("AniSkip response: %w", err)
	}
	return true, nil
}

func skipRoute(version string, malID, episode int) string {
	query := url.Values{}
	query.Add("types", "op")
	query.Add("types", "ed")
	if version == "v2" {
		query.Add("types", "mixed-op")
		query.Add("types", "mixed-ed")
		query.Set("episodeLength", "0")
	}
	return "/" + version + "/skip-times/" + strconv.Itoa(malID) + "/" + strconv.Itoa(episode) + "?" + query.Encode()
}

// v2 carries release lengths; v1 is used only when v2 cannot answer.
// Keep all candidate timings so the actual media duration can select a fit.
func lookupWatchSkip(ctx context.Context, malID, episode int) (watchSkipIntervals, error) {
	if malID <= 0 || episode <= 0 {
		return watchSkipIntervals{}, nil
	}
	var v2 struct {
		Found bool `json:"found"`
		Results []struct {
			Interval struct {
				Start float64 `json:"startTime"`
				End float64 `json:"endTime"`
			} `json:"interval"`
			Kind string `json:"skipType"`
			Length float64 `json:"episodeLength"`
		} `json:"results"`
	}
	ok, err := fetchWatchSkip(ctx, skipRoute("v2", malID, episode), &v2)
	if err == nil && ok && v2.Found {
		spans := watchSkipIntervals{}
		for _, item := range v2.Results {
			spans.add(item.Kind, watchSkipSpan{Start:item.Interval.Start, End:item.Interval.End, length:item.Length})
		}
		return spans, nil
	}
	if ctx.Err() != nil {
		return watchSkipIntervals{}, ctx.Err()
	}
	var v1 struct {
		Found bool `json:"found"`
		Results []struct {
			Interval struct {
				Start float64 `json:"start_time"`
				End float64 `json:"end_time"`
			} `json:"interval"`
			Kind string `json:"skip_type"`
		} `json:"results"`
	}
	ok, fallbackErr := fetchWatchSkip(ctx, skipRoute("v1", malID, episode), &v1)
	if fallbackErr != nil {
		if err != nil { return watchSkipIntervals{}, fmt.Errorf("AniSkip v2: %v, v1: %w", err, fallbackErr) }
		return watchSkipIntervals{}, fallbackErr
	}
	if !ok || !v1.Found {
		return watchSkipIntervals{}, nil
	}
	spans := watchSkipIntervals{}
	for _, item := range v1.Results {
		spans.add(item.Kind, watchSkipSpan{Start:item.Interval.Start, End:item.Interval.End})
	}
	return spans, nil
}

func (intervals *watchSkipIntervals) add(kind string, span watchSkipSpan) {
	if !span.valid() { return }
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "op", "mixed-op":
		intervals.OP = append(intervals.OP, span)
	case "ed", "mixed-ed":
		intervals.ED = append(intervals.ED, span)
	}
}

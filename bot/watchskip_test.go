package main

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWatchSkipIntervalsMatchRelease(t *testing.T) {
	intervals := watchSkipIntervals{
		OP: []watchSkipSpan{
			{Start: 75, End: 165, length: 1440},
			{Start: 95, End: 185, length: 1530},
		},
		ED: []watchSkipSpan{{Start: 1300, End: 1390, length: 1440}},
	}
	if got := intervals.match(1530).OP; got.Start != 95 || got.End != 185 {
		t.Fatalf("selected the wrong cut: %+v", got)
	}
	if got := intervals.match(1440); got.OP.Start != 75 || got.ED.End != 1390 {
		t.Fatalf("1440s cut did not match: %+v", got)
	}
	if got := intervals.match(1600); got.OP.valid() || got.ED.valid() {
		t.Fatalf("mismatched release was accepted: %+v", got)
	}
	for _, bad := range []watchSkipSpan{
		{Start: 90, End: 60}, {Start: -1, End: 90},
		{Start: 0, End: 800}, {Start: math.NaN(), End: 90},
	} {
		if bad.valid() { t.Fatalf("accepted invalid skip span: %+v", bad) }
	}
}

func TestWatchSkipTargetOnlyInsideKnownIntervals(t *testing.T) {
	times := watchSkipTimes{
		OP: watchSkipSpan{Start: 65, End: 152},
		ED: watchSkipSpan{Start: 1235, End: 1318},
	}
	cases := []struct {
		name string
		pos float64
		op, ed bool
		want float64
	}{
		{"before opening", 32, true, true, 0},
		{"inside opening", 78, true, false, 152},
		{"opening disabled", 78, false, true, 0},
		{"after opening", 158, true, true, 0},
		{"inside ending", 1240, false, true, 1318},
		{"ending disabled", 1240, true, false, 0},
		{"after ending", 1350, true, true, 0},
		{"both off", 78, false, false, 0},
	}
	for _, tc := range cases {
		target, ok := watchSkipTarget(times, tc.pos, 1440, tc.op, tc.ed)
		if ok != (tc.want != 0) || ok && target != tc.want {
			t.Errorf("%s: got %.1f, %t; want %.1f", tc.name, target, ok, tc.want)
		}
	}
	if target, ok := watchSkipTarget(times, 80, 0, true, true); ok {
		t.Fatalf("unknown duration skipped to %v", target)
	}
	if target, ok := watchSkipTarget(times, 80, 120, true, true); ok {
		t.Fatalf("out-of-range interval skipped to %v", target)
	}
}

func TestLookupWatchSkipV2AndFallback(t *testing.T) {
	seen := []string{}
	useV1 := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		if r.URL.Query().Get("episodeLength") != "" && strings.HasPrefix(r.URL.Path, "/v1/") {
			t.Error("v1 request sent episodeLength")
		}
		if strings.HasPrefix(r.URL.Path, "/v2/") {
			if useV1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			fmt.Fprint(w, `{"found":true,"results":[{"skipType":"op","interval":{"startTime":55,"endTime":145},"episodeLength":1420},{"skipType":"ed","interval":{"startTime":1200,"endTime":1280},"episodeLength":1420}]}`)
			return
		}
		fmt.Fprint(w, `{"found":true,"results":[{"skip_type":"op","interval":{"start_time":80,"end_time":170}}]}`)
	}))
	defer server.Close()
	oldAPI, oldHTTP := watchSkipAPI, watchSkipHTTP
	watchSkipAPI, watchSkipHTTP = server.URL, server.Client()
	t.Cleanup(func() { watchSkipAPI, watchSkipHTTP = oldAPI, oldHTTP })
	spans, err := lookupWatchSkip(context.Background(), 154587, 2)
	if err != nil || len(spans.OP) != 1 || len(spans.ED) != 1 || spans.OP[0].length != 1420 {
		t.Fatalf("v2 timings: %+v, %v", spans, err)
	}
	if len(seen) != 1 || seen[0] != "/v2/skip-times/154587/2" {
		t.Fatalf("unexpected v2 lookup: %v", seen)
	}
	useV1 = true
	seen = nil
	spans, err = lookupWatchSkip(context.Background(), 154587, 3)
	if err != nil || len(spans.OP) != 1 || spans.OP[0].End != 170 || len(spans.ED) != 0 {
		t.Fatalf("v1 fallback: %+v, %v", spans, err)
	}
	if len(seen) != 2 || seen[1] != "/v1/skip-times/154587/3" {
		t.Fatalf("v1 fallback wasn't used: %v", seen)
	}
}

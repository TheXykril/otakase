package providers_test

import (
	"cmp"
	"slices"
	"testing"
	"time"
)

// Live benchmarks of the hosts the edge cases cover. Run with
//	OTAKASE_LIVE_BENCH=1 go test ./internal/providers -run TestLiveFlowLatency -bench . -v

const liveBenchQuery = "frieren"

// liveBenchCases are the hosts to time. showID is a known show for timing
// the episode and stream steps alone; a host without one is only searched.
var liveBenchCases = []struct {
	provider string
	showID   string
}{
	{provider: "anizone", showID: "2454"},
	{provider: "kickassanime"},
}

// benchCasesWithShow are the cases that can time the steps after search.
func benchCasesWithShow() (provider, showID []string) {
	for _, tc := range liveBenchCases {
		if tc.showID != "" {
			provider, showID = append(provider, tc.provider), append(showID, tc.showID)
		}
	}
	return provider, showID
}

func BenchmarkLiveSearch(b *testing.B) {
	requireLive(b, "OTAKASE_LIVE_BENCH", "live benchmarks")
	for _, tc := range liveBenchCases {
		p := liveProvider(tc.provider)
		b.Run(tc.provider, func(b *testing.B) {
			for b.Loop() {
				if _, err := p.search(liveBenchQuery, "sub"); err != nil {
					b.Fatalf("search: %v", err)
				}
			}
		})
	}
}

func BenchmarkLiveEpisodeAndStream(b *testing.B) {
	requireLive(b, "OTAKASE_LIVE_BENCH", "live benchmarks")
	names, shows := benchCasesWithShow()
	for i, name := range names {
		p, show := liveProvider(name), shows[i]
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				if _, err := p.episodes(show, "sub"); err != nil {
					b.Fatalf("episodes: %v", err)
				}
				if _, err := p.streams(show, "sub", 1); err != nil {
					b.Fatalf("streams: %v", err)
				}
			}
		})
	}
}

// TestLiveFlowLatency times search through to a stream on each host with a
// known show and warns when one is far slower than the fastest.
func TestLiveFlowLatency(t *testing.T) {
	requireLive(t, "OTAKASE_LIVE_BENCH", "live benchmarks")

	type timing struct {
		provider     string
		search, flow time.Duration
	}
	var timings []timing
	names, shows := benchCasesWithShow()
	for i, name := range names {
		p, show := liveProvider(name), shows[i]

		start := time.Now()
		if _, err := p.search(liveBenchQuery, "sub"); err != nil {
			t.Fatalf("%s search: %v", name, err)
		}
		search := time.Since(start)

		if _, err := p.episodes(show, "sub"); err != nil {
			t.Fatalf("%s episodes: %v", name, err)
		}
		switch links, err := p.streams(show, "sub", 1); {
		case err != nil:
			t.Fatalf("%s streams: %v", name, err)
		case len(links) == 0:
			t.Fatalf("%s: no stream links", name)
		}
		flow := time.Since(start)

		timings = append(timings, timing{provider: name, search: search, flow: flow})
		t.Logf("%s search=%s full_flow=%s", name, search, flow)
	}

	if len(timings) < 2 {
		t.Skip("fewer than two hosts have a known show to time")
	}
	slices.SortFunc(timings, func(a, b timing) int { return cmp.Compare(a.flow, b.flow) })
	fastest := timings[0]
	t.Logf("fastest full flow: %s (%s)", fastest.provider, fastest.flow)
	for _, other := range timings[1:] {
		if other.flow > 3*fastest.flow {
			t.Logf("warning: %s full flow is much slower than %s (%s vs %s)", other.provider, fastest.provider, other.flow, fastest.flow)
		}
	}
}

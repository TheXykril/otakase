package providers_test

import (
	"os"
	"strconv"
	"testing"
	"time"
)

// AniPub throttles bursts, so its run of the edge cases is paced and kept
// apart from the others:
//
//	OTAKASE_LIVE_ANIPUB=1 go test ./internal/providers -run TestLiveAnipubLong -v -timeout 60m
//
// OTAKASE_LIVE_ANIPUB_DELAY_MS sets the pause between cases (default 2000).
func anipubLiveDelay() time.Duration {
	if ms, err := strconv.Atoi(os.Getenv("OTAKASE_LIVE_ANIPUB_DELAY_MS")); err == nil && ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	return 2 * time.Second
}

func TestLiveAnipubLong(t *testing.T) {
	requireLive(t, "OTAKASE_LIVE_ANIPUB", "isolated long AniPub live tests")

	anipub := liveProvider("anipub")
	delay := anipubLiveDelay()
	t.Logf("anipub long run: %d cases, %v delay between requests", len(edgeAnimeCases), delay)

	var tally flowTally
	for i, tc := range edgeAnimeCases {
		if i > 0 {
			time.Sleep(delay)
		}
		res := runEdgeCase(anipub, tc)
		tally.add(res)
		t.Logf("[%d/%d] %s", i+1, len(edgeAnimeCases), edgeStatusLine(res))
	}
	t.Logf("anipub long: %s", tally)
}

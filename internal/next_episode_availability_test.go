package internal

import (
	"testing"
	"time"
)

// The list rows and the prompt phrase the same wait differently; both come from
// one formatter so they cannot drift apart.
func TestAiringDelayFormatting(t *testing.T) {
	cases := map[int]string{
		int(4 * 24 * time.Hour / time.Second): "4d",
		int(22 * time.Hour / time.Second):     "22h",
		int(35 * time.Minute / time.Second):   "35m",
		30:                                    "<1m",
		0:                                     "",
		-5:                                    "",
	}
	for seconds, want := range cases {
		if got := formatAiringDelay(seconds); got != want {
			t.Errorf("formatAiringDelay(%d) = %q, want %q", seconds, got, want)
		}
	}
	// The row phrasing still reads as before.
	if got := formatTimeUntilAiring(int(22 * time.Hour / time.Second)); got != "next in 22h" {
		t.Errorf("row countdown changed: %q", got)
	}
	if got := formatTimeUntilAiring(0); got != "" {
		t.Errorf("expected no countdown, got %q", got)
	}
}

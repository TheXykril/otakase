package internal

import (
	"testing"
)

func TestEpisodeProgressNextEpisode(t *testing.T) {
	for progress, want := range map[int]int{-3: 1, 0: 1, 1: 2, 7: 8, 24: 25} {
		if got := nextEpisodeFromProgress(progress); got != want {
			t.Errorf("nextEpisodeFromProgress(%d) = %d, want %d", progress, got, want)
		}
	}
}

func TestEpisodeProgressInputParsing(t *testing.T) {
	if n, err := parsePositiveIntInput("12", "episode number"); err != nil || n != 12 {
		t.Errorf("episode 12: got %d, %v", n, err)
	}
	for _, bad := range []string{"", "0", "-1", "one"} {
		if _, err := parsePositiveIntInput(bad, "episode number"); err == nil {
			t.Errorf("episode %q accepted", bad)
		}
	}
	if n, err := parseNonNegativeIntInput("0", "progress"); err != nil || n != 0 {
		t.Errorf("progress 0: got %d, %v", n, err)
	}
	if _, err := parseNonNegativeIntInput("-1", "progress"); err == nil {
		t.Error("negative progress accepted")
	}
}

package internal

import (
	"errors"
	"slices"
	"strings"
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

func TestEpisodeProgressYesAnswers(t *testing.T) {
	for answer, want := range map[string]bool{"y": true, "Y": true, " yes ": true, "no": false} {
		if got := isAffirmativeAnswer(answer); got != want {
			t.Errorf("isAffirmativeAnswer(%q) = %v", answer, got)
		}
	}
}

func TestEpisodeProgressTotalIgnoresSpecialsAndHalves(t *testing.T) {
	if got := inferTotalEpisodesFromEpisodeList([]string{"0", "3", "13", "13.5", "OVA", "2"}); got != 13 {
		t.Fatalf("got %d, want 13", got)
	}
}

func TestEpisodeProgressProviderTotalChecksBothAudios(t *testing.T) {
	p := &scriptedProvider{episodes: map[string][]string{
		"dub": {"1", "2", "10"},
		"sub": {"1", "47", "48", "48.5"},
	}}
	useProvider(t, p)

	total, err := GetProviderTotalEpisodes("show", "dub")
	if err != nil {
		t.Fatal(err)
	}
	if total != 48 {
		t.Errorf("total = %d, want 48", total)
	}
	if !slices.Equal(p.episodeCalls, []string{"dub", "sub"}) {
		t.Errorf("lookups = %v, want preferred audio first", p.episodeCalls)
	}
}

func TestEpisodeProgressProviderTotalReportsBothFailures(t *testing.T) {
	useProvider(t, &scriptedProvider{episodeErrs: map[string]error{
		"sub": errors.New("sub lookup broke"),
		"dub": errors.New("dub lookup broke"),
	}})

	_, err := GetProviderTotalEpisodes("show", "sub")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, part := range []string{"sub lookup broke", "dub lookup broke"} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("error %q is missing %q", err, part)
		}
	}
}

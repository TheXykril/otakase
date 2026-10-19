package internal

import (
	"slices"
	"testing"
)

func TestConfigFallbackBadValuesUseDefaults(t *testing.T) {
	cfg := PopulateConfig(map[string]string{
		"PercentageToMarkComplete": "eighty",
		"SkipOp":                   "maybe",
		"MpvPlaybackStartTimeout":  "soon",
	})

	if cfg.PercentageToMarkComplete != 85 {
		t.Errorf("PercentageToMarkComplete = %d, want 85", cfg.PercentageToMarkComplete)
	}
	if cfg.MpvPlaybackStartTimeout != MpvStartWaitSeconds {
		t.Errorf("MpvPlaybackStartTimeout = %d", cfg.MpvPlaybackStartTimeout)
	}
	if !cfg.SkipOp {
		t.Error("SkipOp lost its default")
	}
	if cfg.StoragePath == "" {
		t.Error("missing keys were not filled from defaults")
	}
}

// commas inside a quoted element must survive
func TestConfigFallbackStringArrayKeepsQuotedCommas(t *testing.T) {
	got := parseStringArray(`["--script-opts=x=1,2","--really-quiet"]`)
	want := []string{"--script-opts=x=1,2", "--really-quiet"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestConfigFallbackReferrerArgSpellings(t *testing.T) {
	const ref = "Referer: https://cdn.invalid/"
	for _, args := range [][]string{
		{"--http-header-fields-append=" + ref},
		{"--http-header-fields-append", ref},
		{"--mpv-http-header-fields-append=" + ref},
	} {
		if !hasMPVReferrerArg(args) {
			t.Errorf("referrer not found in %q", args)
		}
	}
}

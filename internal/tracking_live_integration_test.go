package internal

import (
	"os"
	"testing"
)

// Live tests against the real AniList and MyAnimeList accounts in the user's
// config. Both lists are put back as they were afterwards. Opt in with
// OTAKASE_RUN_TRACKER_LIVE_INTEGRATION=1; -short always skips them.

const liveTrackerEnv = "OTAKASE_RUN_TRACKER_LIVE_INTEGRATION"

func TestLiveTrackerContextForcesTerminalPrompts(t *testing.T) {
	if os.Getenv(liveTrackerEnv) == "1" {
		t.Skip("the live suite itself covers this")
	}

	cfg := Config{RofiSelection: true, ImagePreview: true, AlternateScreen: true}
	forceTerminalTestMode(&cfg)

	if cfg.RofiSelection || cfg.ImagePreview || cfg.AlternateScreen {
		t.Errorf("terminal prompt mode not forced: %+v", cfg)
	}
	if !cfg.MyAnimeListImportDismissed {
		t.Error("the import offer should be dismissed")
	}
}

// forceTerminalTestMode keeps every prompt in the terminal, where the tests
// can answer it, and skips the import offer.
func forceTerminalTestMode(cfg *Config) {
	cfg.RofiSelection, cfg.ImagePreview, cfg.AlternateScreen = false, false, false
	cfg.MyAnimeListImportDismissed = true
}

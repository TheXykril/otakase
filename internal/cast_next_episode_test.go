package internal

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// The countdown is the whole point of casting continuing: a viewer on a sofa
// should not walk to the keyboard between episodes, but should be able to stop
// it if they are at the desk.
func TestCastCountdownTick(t *testing.T) {
	prompting := &Config{NextEpisodePrompt: true}

	for _, tc := range []struct {
		name    string
		elapsed time.Duration
		key     bool
		config  *Config
		want    castCountdownDecision
	}{
		{"still counting", 3 * time.Second, false, prompting, castCountdownWaiting},
		{"time is up", castCountdownDuration, false, prompting, castCountdownAdvance},
		{"past the end", castCountdownDuration + time.Second, false, prompting, castCountdownAdvance},
		{"a key stops it", 3 * time.Second, true, prompting, castCountdownCancelled},
		{"a key at the last moment still stops it", castCountdownDuration, true, prompting, castCountdownCancelled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := castCountdownTick(tc.elapsed, tc.key, tc.config); got != tc.want {
				t.Errorf("castCountdownTick = %v, want %v", got, tc.want)
			}
		})
	}
}

// NextEpisodePrompt=false already means "do not ask me" for local playback, and
// it means the same here: no countdown, no wait.
func TestCastCountdownRespectsTheNoPromptSetting(t *testing.T) {
	config := &Config{NextEpisodePrompt: false}

	if got := castCountdownTick(0, false, config); got != castCountdownAdvance {
		t.Errorf("with prompting off, a zero-elapsed tick returned %v, want advance", got)
	}
}

// Review Focus 2. A key during the countdown is the viewer saying stop, even
// though the episode completed and the loop would otherwise be entitled to go
// on.
func TestCastCountdownCancelBeatsExpiry(t *testing.T) {
	config := &Config{NextEpisodePrompt: true}

	if got := castCountdownTick(castCountdownDuration*2, true, config); got != castCountdownCancelled {
		t.Errorf("a keypress past the deadline returned %v, want cancelled", got)
	}
}

// The panel line has to name both episodes and the remaining time: it is the
// only thing on screen, and a viewer glancing at it should not have to work out
// what is about to happen.
func TestCastCountdownMessage(t *testing.T) {
	line := castCountdownMessage(12, 13, 7*time.Second)

	for _, want := range []string{"12", "13", "7s", "any key"} {
		if !strings.Contains(line, want) {
			t.Errorf("the countdown line does not mention %q: %s", want, line)
		}
	}
}

// Review Focus 1. A provider link that has rotted fails every episode in
// seconds. A loop that advances on failure marks a whole season watched in
// about a minute, and the viewer finds out from their tracker.
func TestCastLoopStopsOnFailure(t *testing.T) {
	episodes := 0
	cast := func() error {
		episodes++
		return errors.New("cast: no episode links")
	}
	advanced := 0
	advance := func() bool {
		advanced++
		return true
	}

	runCastLoop(cast, advance)

	if episodes != 1 {
		t.Errorf("a failing cast was attempted %d times, want 1", episodes)
	}
	if advanced != 0 {
		t.Errorf("the loop advanced %d times past an episode that never played", advanced)
	}
}

// Stopping is not completing: the viewer asked for this to end.
func TestCastLoopStopsWhenTheViewerStops(t *testing.T) {
	advanced := 0
	runCastLoop(
		func() error { return ErrCastStopped },
		func() bool { advanced++; return true },
	)

	if advanced != 0 {
		t.Errorf("pressing q advanced %d times", advanced)
	}
}

// The ordinary case: episodes play until there is no next one.
func TestCastLoopRunsUntilThereIsNoNextEpisode(t *testing.T) {
	episodes := 0
	remaining := 3
	runCastLoop(
		func() error { episodes++; return nil },
		func() bool { remaining--; return remaining > 0 },
	)

	if episodes != 3 {
		t.Errorf("played %d episodes, want 3", episodes)
	}
}

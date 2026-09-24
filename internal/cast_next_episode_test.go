package internal

import (
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

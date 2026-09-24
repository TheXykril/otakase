package internal

import (
	"fmt"
	"time"
)

// castCountdownDuration is how long the next episode waits before it starts.
//
// Long enough to reach a keyboard from a sofa, short enough that a viewer who
// wants the next episode is not made to sit through a timer.
const castCountdownDuration = 10 * time.Second

// castCountdownDecision is what one tick of the countdown concluded.
type castCountdownDecision int

const (
	castCountdownWaiting castCountdownDecision = iota
	castCountdownAdvance
	castCountdownCancelled
)

// castCountdownTick decides what a countdown should do, given how long it has
// been running and whether the viewer has touched the keyboard.
//
// Pure, so the decision is tested without a terminal: the rendering and the key
// reading are the caller's, and neither carries a decision of its own.
func castCountdownTick(elapsed time.Duration, keyPressed bool, config *Config) castCountdownDecision {
	// Checked before the deadline: a viewer reaching for the keyboard as the
	// timer runs out means stop, not "too late".
	if keyPressed {
		return castCountdownCancelled
	}
	// NextEpisodePrompt=false already means "do not ask" for local playback.
	if config != nil && !config.NextEpisodePrompt {
		return castCountdownAdvance
	}
	if elapsed >= castCountdownDuration {
		return castCountdownAdvance
	}
	return castCountdownWaiting
}

// castCountdownMessage is the line the panel shows while the countdown runs.
func castCountdownMessage(watched, next int, remaining time.Duration) string {
	seconds := int(remaining.Seconds() + 0.5)
	if seconds < 0 {
		seconds = 0
	}
	return fmt.Sprintf("Episode %d watched · Episode %d in %ds — any key to stop", watched, next, seconds)
}

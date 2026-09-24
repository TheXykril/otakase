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

// castSeasonFinished reports that the episode just watched was the last one
// this show has, so there is nothing to count down to.
//
// It asks the same question advanceDecision's atEnd does, and must keep giving
// the same answer: the countdown promising an episode the advance then declines
// to play is what this exists to prevent.
func castSeasonFinished(anime *Anime) bool {
	return anime != nil && anime.TotalEpisodes > 0 && anime.Ep.Number >= anime.TotalEpisodes
}

// castCountdownMessage is the line the panel shows while the countdown runs.
//
// next is always the watched episode plus one. SkipFiller does not change that:
// it only picks which episode the prefetch in main's loop fetches ahead, while
// StartNextEpisode always advances by one, so there is no jump to render here
// and no reason to ask a provider what the next canon episode is.
func castCountdownMessage(watched, next int, remaining time.Duration) string {
	seconds := int(remaining.Seconds() + 0.5)
	if seconds < 0 {
		seconds = 0
	}
	return fmt.Sprintf("Episode %d watched · Episode %d in %ds — any key to stop", watched, next, seconds)
}

// castAwaitNextEpisode counts down to the next episode in the panel, reporting
// whether it should start.
//
// A nil panel means no terminal to draw in -- the countdown still runs, it is
// simply not shown, because a viewer who cannot see it can still be waiting for
// the next episode.
func castAwaitNextEpisode(config *Config, anime *Anime, panel *castPanelWriter, commands <-chan castCommand) bool {
	// At the end of a season there is no next episode to offer. Counting down
	// anyway told the viewer "Episode 12 watched · Episode 13 in 10s" on a path
	// every finished show reaches, and made them wait ten seconds for the score
	// prompt AdvanceAfterEpisode was always going to show instead. Returning
	// true hands straight to the advance, which declines and completes the show.
	if castSeasonFinished(anime) {
		return true
	}

	started := time.Now()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		var pressed bool
		select {
		case <-ticker.C:
		case <-commands:
			pressed = true
		}

		elapsed := time.Since(started)
		switch castCountdownTick(elapsed, pressed, config) {
		case castCountdownAdvance:
			return true
		case castCountdownCancelled:
			return false
		}

		if panel != nil {
			fmt.Print(panel.status(
				GetAnimeName(*anime), anime.Ep.Number, config.CastDevice,
				castCountdownMessage(anime.Ep.Number, anime.Ep.Number+1, castCountdownDuration-elapsed),
			))
		}
	}
}

// runCastLoop casts episodes until there is no next one, the viewer stops, or
// something fails.
//
// It holds no decisions of its own: cast plays one episode and reports how it
// ended, advance completes it and prepares the next. Both are injected, which
// is what lets the loop be tested without a device -- and what keeps the
// advancing logic in one place rather than two.
func runCastLoop(cast func() error, advance func() bool) {
	for {
		err := cast()
		if err != nil {
			// A failure and a stop both end the loop. Advancing past an
			// episode that never played would mark a season watched in about a
			// minute on a provider link that has rotted.
			return
		}
		if !advance() {
			return
		}
	}
}

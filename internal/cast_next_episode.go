package internal

import (
	"fmt"
	"strings"
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

// castNextEpisodeNumber is the episode the countdown should promise.
//
// StartNextEpisode only ever advances by one, but the terminal cast rejoins
// main's playback loop, which skips filler at the top of the next iteration
// (cmd/otakase/main.go): episode 13 being filler makes it jump to the next
// canon episode. Promising 13 and then playing 15 is telling the viewer
// something that does not happen, so the jump is applied to the label too.
//
// GetNextCanonEpisode is pure and FillerEpisodes is already on the struct, so
// this costs nothing and asks nobody -- and it walks a whole run of filler in
// one call, exactly as main's loop does by going round again.
//
// With no filler list -- the spawned cast session carries none -- the answer is
// the plain next episode, which is also what that process actually plays: its
// loop does no filler skipping at all.
//
// Recap skipping is deliberately not mirrored. IsRecap comes from Jikan
// (GetEpisodeData), not from anything on the struct, so deciding it here would
// mean a network call to render a label; and main's loop advances past a recap
// one at a time, so a run of them cannot be resolved locally either.
func castNextEpisodeNumber(config *Config, anime *Anime) int {
	if anime == nil {
		return 0
	}
	if config != nil && config.SkipFiller && len(anime.FillerEpisodes) > 0 {
		return GetNextCanonEpisode(anime.FillerEpisodes, anime.Ep.Number)
	}
	return anime.Ep.Number + 1
}

// castCountdownMessage is the line the panel shows while the countdown runs.
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

	// A key pressed in the last second of the episode is still sitting in the
	// buffered command channel when the episode finishes: the watch loop returns
	// on its own without draining it. Left there it cancels the countdown on the
	// first tick, so the season stops with no keypress the viewer would
	// recognise as having stopped it. Only what arrives from here on counts.
	for drained := false; !drained; {
		select {
		case <-commands:
		default:
			drained = true
		}
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
				castCountdownMessage(anime.Ep.Number, castNextEpisodeNumber(config, anime), castCountdownDuration-elapsed),
			))
		}
	}
}

// castAwaitYesNo waits out a window for the viewer to accept something,
// reporting whether they did.
//
// Drawn in the same frame as the episode countdown rather than as bare text: a
// cast session holds the terminal for the whole cast, so the panel this reaches
// is the one the viewer has been watching all season. Without a panel -- a cast
// launched in a terminal that has already released it -- it falls back to plain
// output, which is worse but still better than blocking.
//
// It takes a subscription of its own, because the episode that just finished
// released theirs and the process-global reader would otherwise swallow the
// keypress meant to accept.
//
// It leaves the last countdown frame on screen rather than blanking it, because
// a panel with an empty message row looks broken. The caller follows with
// castPanelSay to say how it ended.
func castAwaitYesNo(config *Config, anime *Anime, question string, window time.Duration) bool {
	commands, release, ok := startCastControls(config)
	if !ok {
		Log(fmt.Sprintf("cast: no keyboard for %q, declining", question))
		return false
	}
	defer release()

	// Whatever was pressed during the episode is still buffered, and the first
	// tick would read it as an answer to a question that had not been asked yet.
	// Same drain the episode countdown does.
	for drained := false; !drained; {
		select {
		case <-commands:
		default:
			drained = true
		}
	}

	draw := func(message string) {
		if panel := castPanelForControls; panel != nil {
			fmt.Print(panel.status(GetAnimeName(*anime), anime.Ep.Number, config.CastDevice, message))
			return
		}
		// Carriage return and trailing spaces so the line is overwritten rather
		// than left stacked above the outcome.
		fmt.Print("\r  " + message + strings.Repeat(" ", 40) + "\r")
	}

	started := time.Now()
	// Drawn before the loop rather than on the first tick. A tick is 250ms, so
	// waiting for one would leave the question invisible for a quarter of a
	// second -- and a window shorter than a tick would never be shown at all.
	draw(fmt.Sprintf("%s — %ds to answer, any key to accept",
		question, int(window.Seconds()+0.5)))

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
		case <-commands:
			return true
		}

		remaining := window - time.Since(started)
		if remaining <= 0 {
			return false
		}
		draw(fmt.Sprintf("%s — %ds to answer, any key to accept",
			question, int(remaining.Seconds()+0.5)))
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

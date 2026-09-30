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
	// Nothing asks for a countdown: neither the old prompt nor the countdown
	// that replaced it.
	if config != nil && !config.NextEpisodePrompt && config.NextEpisodeCountdown <= 0 {
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
	// A receive on a closed channel succeeds forever, so the loop has to stop on
	// the closed case as well as on the empty one. Nothing closes a subscription
	// in production today, but a test that hands over a closed channel should
	// fail rather than spin a goroutine at full tilt.
	for drained := false; !drained; {
		select {
		case _, open := <-commands:
			drained = !open
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
				castPanelPlaybackKeys,
			))
		}
	}
}

// castScoreMin and castScoreMax are the ends of the score the panel offers.
// AniList takes 0 to 10, but 0 means "no score" to it, so 1 is the lowest worth
// offering someone who chose to answer at all.
const (
	castScoreMin = 1
	castScoreMax = 10
	// castScoreStart is where the picker opens. High enough that a viewer who
	// walks over and hits enter immediately has not understated the show, which
	// is the mistake that is harder to notice and easier to leave in a tracker.
	castScoreStart = 8
)

// castScoreEngagedWindow is how long the picker waits after any input before
// giving up on the viewer.
//
// An inactivity window, so every press puts it back: someone deliberating with
// pauses -- tap, think, tap -- must not be cut off on a deadline set by their
// first keypress, least of all when the clock is not on the panel to warn them.
// Two minutes of genuine stillness is not interrupting anybody.
//
// A var so a test can shrink it, for the same reason castStallTimeout is one.
var castScoreEngagedWindow = 2 * time.Minute

// The two footers. They differ because what the viewer can do differs.
//
// Before they have touched anything, the only outcomes are "save" and "be
// skipped", so that is what the footer says. Once they are choosing, the clock
// is gone and q is the only way out, so the footer says that instead -- a viewer
// who has just been given all the time in the world should not have to guess
// how to give it back.
const (
	castScoreKeysUntouched = " ↑↓ score · enter save · or it is skipped "
	castScoreKeysChoosing  = " ↑↓ score · enter save · q to skip "
)

// castScoreMessage is the picker's line while nobody has touched it: the score
// and the time left to do something about it.
//
// The consequence of doing nothing is in the footer rather than here, because the
// message row truncates from the right on a narrow terminal and the consequence
// is the part that must survive. The episode countdown gets this for free by
// naming its outcome inline; here it has to be stated.
func castScoreMessage(score, seconds int) string {
	return fmt.Sprintf("Rate this anime:  %d/%d  ·  %ds left", score, castScoreMax, seconds)
}

// castScoreChoosingMessage is the picker's line once the viewer is choosing: the
// score, and no clock.
//
// No clock because they are standing at the keyboard choosing a number, and a
// number counting down at them is pressure rather than information. A viewer told
// "9s to answer" and left to watch it run out also reasonably concludes they are
// about to be rated 8 rather than about to lose the rating, which is the same
// confusion with the arithmetic in it.
func castScoreChoosingMessage(score int) string {
	return fmt.Sprintf("Rate this anime:  %d/%d", score, castScoreMax)
}

// castAwaitScore asks for a score in the panel, reporting the score and whether
// one was given.
//
// The panel is across the room, so this is arrows and one key rather than a
// typed number: asking someone to type "7" at a keyboard they are not standing
// at is how you get a show rated 1. Nothing but Enter accepts, so a stray
// keypress cannot decline on the viewer's behalf -- the window is the only thing
// that declines, and it says so on screen.
//
// Takes a subscription of its own for the same reason castAwaitYesNo does: the
// episode that just finished released theirs, and the process-global reader
// would eat the keypress meant for this.
func castAwaitScore(config *Config, anime *Anime, window time.Duration) (int, bool) {
	commands, release, ok := startCastControls(config)
	if !ok {
		Log("cast: no keyboard to pick a score with")
		return 0, false
	}
	defer release()

	// A key pressed during the episode is still buffered, and the first tick
	// would read it as an answer to a question not yet asked.
	for drained := false; !drained; {
		select {
		case _, open := <-commands:
			// A receive on a closed channel succeeds forever, so an unguarded
			// drain spins at full tilt rather than emptying and stopping.
			drained = !open
		default:
			drained = true
		}
	}

	score := castScoreStart
	// The window is for absence, not for the question.
	//
	// It exists because a cast window nobody is at must resolve on its own rather
	// than sit on a prompt forever -- that hang is the failure this whole feature
	// exists to remove. It is not a budget for answering: choosing a score is a
	// run of keypresses, and ten seconds cuts the viewer off halfway through with
	// a score nearly chosen and no way to save it.
	//
	// So the first adjustment swaps it for the longer one, and the clock comes off
	// the panel. From then on Enter saves and q declines, both named on screen.
	// The deadline does not go away with the clock: a viewer who touches a key and
	// walks away would otherwise leave the window hung on the rating prompt, which
	// is the same failure wearing a different hat. Two minutes is long enough that
	// nobody is actually going to be interrupted by it, and short enough that the
	// window still resolves if they do.
	// The first adjustment swaps the short window for the long one and takes the
	// clock off the panel; every later one puts the long window back.
	//
	// Resetting on each press is the part that matters. Someone choosing a number
	// thinks in pauses -- tap, consider, tap -- and a deadline measured from the
	// first keypress would expire mid-thought with nothing on screen to explain
	// it. The clock is what makes a deadline fair, and it is gone by then, so the
	// window has to be generous and has to be renewed by the act of carrying on.
	engaged := false
	deadline := time.Now().Add(window)
	engage := func() {
		engaged = true
		deadline = time.Now().Add(castScoreEngagedWindow)
	}

	draw := func() {
		message := castScoreChoosingMessage(score)
		keys := castScoreKeysChoosing
		if !engaged {
			seconds := int(time.Until(deadline).Seconds() + 0.5)
			if seconds < 0 {
				seconds = 0
			}
			message = castScoreMessage(score, seconds)
			keys = castScoreKeysUntouched
		}
		if panel := castPanelForControls; panel != nil {
			fmt.Print(panel.status(GetAnimeName(*anime), anime.Ep.Number,
				config.CastDevice, message, keys))
			return
		}
		fmt.Print("\r  " + message + strings.Repeat(" ", 40) + "\r")
	}

	// Drawn before the loop: a tick is 250ms, so waiting for one would leave the
	// question unseen for a quarter second, and a window shorter than a tick
	// would never be shown at all.
	draw()

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		// The ticker only wakes the loop; it never takes a command. A select over
		// both consumes whichever is ready and throws it away, which with one
		// keypress per tick silently discards every key the viewer presses.
		<-ticker.C

		// Drained rather than one per tick, so three quick taps move three points
		// instead of arriving over three quarters of a second.
		drained := false
		for !drained {
			select {
			case command, open := <-commands:
				if !open {
					// A receive on a closed channel succeeds forever. Without
					// this the picker spins instead of waiting for the viewer,
					// and every tick reads the zero command.
					drained = true
					break
				}
				switch command {
				case castCmdVolumeUp:
					if score < castScoreMax {
						score++
					}
					engage()
				case castCmdVolumeDown:
					if score > castScoreMin {
						score--
					}
					engage()
				case castCmdSelect:
					return score, true
				case castCmdStop:
					// q declines the rating. It does not stop the cast: the season
					// is already over, so there is nothing playing to stop, and
					// declining is what a viewer pressing q here means. The
					// completion still runs, which is the point.
					return 0, false
				default:
					// Seek and pause mean nothing here. Ignoring them is the
					// point: a viewer pressing something unexpected must not
					// decline on their behalf.
				}
			default:
				drained = true
			}
		}

		// Expiry after the drain, so a key arriving in the last tick still counts.
		// The reverse order is the mistake castCountdownTick documents: a viewer
		// reaching for the keyboard as the timer runs out means accept, not "too
		// late".
		//
		// Applies once they are choosing too, which is the whole reason the
		// engaged window exists: the clock is off the panel by then, so a deadline
		// that did not apply would be one they could neither see nor outlast.
		if !time.Now().Before(deadline) {
			return 0, false
		}

		draw()
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
		case _, open := <-commands:
			// A receive on a closed channel succeeds forever, so an unguarded
			// drain spins at full tilt rather than emptying and stopping.
			drained = !open
		default:
			drained = true
		}
	}

	draw := func(message string) {
		if panel := castPanelForControls; panel != nil {
			fmt.Print(panel.status(GetAnimeName(*anime), anime.Ep.Number, config.CastDevice, message, castPanelPlaybackKeys))
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

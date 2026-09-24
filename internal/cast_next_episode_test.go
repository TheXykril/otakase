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

// Important 3. On the last episode of a season the countdown used to promise
// an episode that does not exist -- "Episode 12 watched · Episode 13 in 10s"
// -- for ten seconds, and only then did AdvanceAfterEpisode decline and run
// completion. The viewer was told something false on the path every finished
// show reaches, and made to wait for the score prompt.
func TestCastAwaitSkipsTheCountdownAtTheEndOfASeason(t *testing.T) {
	config := &Config{NextEpisodePrompt: true}
	anime := &Anime{TotalEpisodes: 12}
	anime.Ep.Number = 12

	// A closed channel: any tick of the countdown would read a "keypress" from
	// it and cancel, so returning true can only mean it never started.
	commands := make(chan castCommand)
	close(commands)

	done := make(chan bool, 1)
	go func() { done <- castAwaitNextEpisode(config, anime, nil, commands) }()

	select {
	case advanced := <-done:
		if !advanced {
			t.Error("the finale returned cancelled; it must hand straight to the advance")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the countdown ran on the last episode of the season")
	}
}

// castSeasonFinished must agree with advanceDecision's atEnd: a countdown that
// promises an episode the advance then declines to play is the bug.
func TestCastSeasonFinishedAgreesWithAdvanceDecision(t *testing.T) {
	for _, tc := range []struct {
		name    string
		total   int
		episode int
	}{
		{"mid season", 12, 5},
		{"last episode", 12, 12},
		{"past the end", 12, 13},
		{"unknown length", 0, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			anime := &Anime{TotalEpisodes: tc.total}
			anime.Ep.Number = tc.episode

			wantEnd := advanceDecision(anime, true).SeriesFinished
			if got := castSeasonFinished(anime); got != wantEnd {
				t.Errorf("castSeasonFinished = %v, advanceDecision's atEnd = %v", got, wantEnd)
			}
		})
	}
}

// Minor 5. The commands channel is buffered and nothing drains it on the
// finish path, so a key pressed in the last second of the episode was still
// there when the countdown started and cancelled it on the first tick -- the
// season stopping with no keypress the viewer would connect to it.
func TestCastAwaitIgnoresAKeyPressedDuringTheEpisode(t *testing.T) {
	// Prompting off so the first tick decides: a stale key still cancels there,
	// which is the bug, and the test does not have to sit through ten seconds.
	config := &Config{NextEpisodePrompt: false}
	anime := &Anime{TotalEpisodes: 12}
	anime.Ep.Number = 5

	commands := make(chan castCommand, 8)
	commands <- castCmdSeekForward // pressed while the episode was still playing

	done := make(chan bool, 1)
	go func() { done <- castAwaitNextEpisode(config, anime, nil, commands) }()

	select {
	case advanced := <-done:
		if !advanced {
			t.Error("a keypress left over from the episode cancelled the countdown")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the countdown never finished")
	}
}

// A key pressed during the countdown itself still stops it.
func TestCastAwaitStillStopsOnALiveKeypress(t *testing.T) {
	config := &Config{NextEpisodePrompt: true}
	anime := &Anime{TotalEpisodes: 12}
	anime.Ep.Number = 5

	commands := make(chan castCommand, 8)
	done := make(chan bool, 1)
	go func() { done <- castAwaitNextEpisode(config, anime, nil, commands) }()

	// After the drain, not before: the countdown ticks every 250ms, so this
	// lands well inside the ten seconds.
	time.Sleep(500 * time.Millisecond)
	commands <- castCmdStop

	select {
	case advanced := <-done:
		if advanced {
			t.Error("a key pressed during the countdown did not stop it")
		}
	case <-time.After(castCountdownDuration + 5*time.Second):
		t.Fatal("the countdown never finished")
	}
}

// Important 3, the lesser version. The terminal cast rejoins main's playback
// loop, which skips filler at the top of the next iteration: after episode 12
// StartNextEpisode sets 13, and if 13 is filler that loop jumps to the next
// canon episode. The countdown promised 13 and the viewer got 15.
func TestCastNextEpisodeNumberFollowsTheFillerSkip(t *testing.T) {
	for _, tc := range []struct {
		name       string
		skipFiller bool
		filler     []int
		watched    int
		want       int
	}{
		{"one filler episode is stepped over", true, []int{13}, 12, 14},
		{"a run of filler is walked in one go", true, []int{13, 14}, 12, 15},
		{"a canon next episode is left alone", true, []int{17}, 12, 13},
		{"SkipFiller off keeps the plain next episode", false, []int{13, 14}, 12, 13},
		{"no filler list means no jump to render", true, nil, 12, 13},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := &Config{SkipFiller: tc.skipFiller}
			anime := &Anime{TotalEpisodes: 24, FillerEpisodes: tc.filler}
			anime.Ep.Number = tc.watched

			if got := castNextEpisodeNumber(config, anime); got != tc.want {
				t.Errorf("castNextEpisodeNumber = %d, want %d", got, tc.want)
			}
		})
	}
}

// The number the panel shows is the number the label carries: a regression
// here is invisible unless the two are checked together.
func TestCastCountdownMessageNamesTheEpisodeThatWillPlay(t *testing.T) {
	config := &Config{SkipFiller: true}
	anime := &Anime{TotalEpisodes: 24, FillerEpisodes: []int{13, 14}}
	anime.Ep.Number = 12

	line := castCountdownMessage(anime.Ep.Number, castNextEpisodeNumber(config, anime), castCountdownDuration)

	if !strings.Contains(line, "Episode 15") {
		t.Errorf("countdown line %q does not name episode 15, the one main's loop will play", line)
	}
	if strings.Contains(line, "Episode 13") {
		t.Errorf("countdown line %q still promises the filler episode", line)
	}
}

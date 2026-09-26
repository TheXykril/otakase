package internal

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// Enter has to exist as a command at all. The panel's other keys are playback
// controls -- pause, seek, volume, stop -- and a viewer being asked to rate
// something needs a key that means yes and is not one of those.
func TestEnterDecodesAsSelect(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []byte
		want castCommand
	}{
		{"carriage return, which is what Enter sends in raw mode", []byte{'\r'}, castCmdSelect},
		{"newline", []byte{'\n'}, castCmdSelect},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, used := decodeCastKey(tc.in)
			if got != tc.want || used != 1 {
				t.Errorf("decodeCastKey(%q) = %v, %d; want %v, 1", tc.in, got, used, tc.want)
			}
		})
	}
}

// The picker's own bounds. 0 is AniList's "no score", so offering it to someone
// who chose to answer would record a deliberate zero.
func TestScoreStaysInsideItsRange(t *testing.T) {
	anime := &Anime{}
	anime.Title.Romaji = "Test Show"
	anime.Ep.Number = 12

	score, given := driveCastScore(t, anime, []castCommand{
		castCmdVolumeDown, castCmdVolumeDown, castCmdVolumeDown, // 8 -> 7 -> 6 -> 5
		castCmdSelect,
	})
	if !given {
		t.Fatal("the picker declined despite an explicit accept")
	}
	if score != 5 {
		t.Errorf("score = %d, want 5", score)
	}

	// Down at the bottom, and up past the top.
	score, _ = driveCastScore(t, anime, append(
		[]castCommand{castCmdVolumeDown, castCmdVolumeDown, castCmdVolumeDown,
			castCmdVolumeDown, castCmdVolumeDown, castCmdVolumeDown,
			castCmdVolumeDown, castCmdVolumeDown},
		castCmdSelect))
	if score != castScoreMin {
		t.Errorf("score floored at %d, want %d", score, castScoreMin)
	}
}

// A viewer pressing something meaningless must not decline on their behalf. The
// window is the only thing that may decline.
func TestUnrelatedKeysDoNotDeclineTheScore(t *testing.T) {
	anime := &Anime{}
	anime.Title.Romaji = "Test Show"
	anime.Ep.Number = 12

	score, given := driveCastScore(t, anime, []castCommand{
		castCmdPauseToggle, castCmdSeekForward, castCmdSeekBack, castCmdSelect,
	})
	if !given {
		t.Error("a pause or a seek declined the score")
	}
	if score != castScoreStart {
		t.Errorf("score = %d, want it untouched at %d", score, castScoreStart)
	}
}

// q stops the cast. It must not also save a rating on the way past.
func TestStoppingDoesNotSaveAScore(t *testing.T) {
	anime := &Anime{}
	anime.Title.Romaji = "Test Show"
	anime.Ep.Number = 12

	if score, given := driveCastScore(t, anime, []castCommand{castCmdVolumeUp, castCmdStop}); given {
		t.Errorf("stopping saved a rating of %d", score)
	}
}

// The bug this whole change exists beside. RateAnime returned nil when the score
// prompt was cancelled, so every caller took the success branch and announced a
// rating that was never written -- "Anime rated successfully!", and a summary
// reading "rating saved". Escaping the prompt in a terminal did it too, and a
// cast window did it every time.
//
// Driven through the real function rather than a simulation of it, because the
// failure was precisely that nil and a declined rating looked like a success.
func TestRateAnimeReportsADeclineRatherThanSuccess(t *testing.T) {
	previous := GetGlobalConfig()
	previousAnime := GetGlobalAnime()
	SetGlobalConfig(&Config{TrackingRemote: "anilist+myanimelist", CastNonInteractive: true})
	SetGlobalAnime(&Anime{AnilistId: 1})
	t.Cleanup(func() {
		SetGlobalConfig(previous)
		SetGlobalAnime(previousAnime)
	})

	err := RateAnime("token", 1)

	if !errors.Is(err, ErrRatingDeclined) {
		t.Fatalf("RateAnime error = %v, want ErrRatingDeclined: a nil here is what "+
			"made a declined rating report itself as saved", err)
	}
	// And the reporting consequence, which is what the user actually saw.
	if err == nil {
		t.Error("a declined rating is indistinguishable from a successful one")
	}
}

// With a score in hand the write is not a prompt, so it cannot decline.
func TestRateAnimeWithScoreDoesNotPrompt(t *testing.T) {
	previous := GetGlobalConfig()
	previousAnime := GetGlobalAnime()
	// Remote tracking off: nothing is written, and the point is that the call
	// returns without asking anything rather than hanging on a prompt.
	SetGlobalConfig(&Config{TrackingRemote: "none", CastNonInteractive: true})
	SetGlobalAnime(&Anime{AnilistId: 1})
	t.Cleanup(func() {
		SetGlobalConfig(previous)
		SetGlobalAnime(previousAnime)
	})

	if err := RateAnimeWithScore("token", 1, 7); err != nil {
		t.Errorf("RateAnimeWithScore: %v", err)
	}
}

// The window must be visible while it runs. It was computed and then used only
// in the no-panel branch, so the panel showed a bare "8/10" and the viewer had no
// way of knowing how long they had.
func TestScorePickerShowsTheTimeRemaining(t *testing.T) {
	resetCastControlsForTest(t)
	resetCastSessionScreen(t)
	holdCastSessionPanel(t)
	castControlsBegin = func(*Config) bool { return true }

	anime := &Anime{}
	anime.Title.Romaji = "Test Show"
	anime.Ep.Number = 12

	// Long enough that only the opening frame is drawn, and it must carry seconds.
	out := captureStdout(t, func() {
		castAwaitScore(&Config{}, anime, 20*time.Second)
	})

	if !strings.Contains(out, "Rate this anime:  8/10") {
		t.Fatalf("the score is not shown:\n%s", out)
	}
	if !strings.Contains(out, "20s to answer") && !strings.Contains(out, "19s to answer") {
		t.Errorf("the panel does not say how long is left:\n%s", out)
	}
}

// Choosing a score is a run of keypresses. A fixed deadline shuts the window on
// someone halfway through the run -- arrows pressed, score nearly chosen, and no
// way to save it. Every adjustment buys another window.
func TestAdjustingTheScoreExtendsTheWindow(t *testing.T) {
	resetCastControlsForTest(t)
	resetCastSessionScreen(t)
	holdCastSessionPanel(t)
	castControlsBegin = func(*Config) bool { return true }

	anime := &Anime{}
	anime.Title.Romaji = "Test Show"
	anime.Ep.Number = 12

	// A window shorter than the whole run of keys, so a fixed deadline would
	// expire partway through and decline.
	const window = 400 * time.Millisecond
	go func() {
		waitForCastSubscription(t)
		time.Sleep(150 * time.Millisecond) // clear the pre-loop drain
		for i := 0; i < 4; i++ {
			castDeliverCommand(castCmdVolumeDown)
			time.Sleep(250 * time.Millisecond) // each gap is most of the window
		}
		castDeliverCommand(castCmdSelect)
	}()

	score, given := castAwaitScore(&Config{}, anime, window)

	if !given {
		t.Fatal("the window expired while the viewer was still choosing")
	}
	if score != 4 {
		t.Errorf("score = %d, want 4: four adjustments from 8", score)
	}
}

// And the window still ends on its own when nobody touches it, or the cast
// hangs on a question.
func TestScorePickerDeclinesWhenLeftAlone(t *testing.T) {
	resetCastControlsForTest(t)
	resetCastSessionScreen(t)
	holdCastSessionPanel(t)
	castControlsBegin = func(*Config) bool { return true }

	anime := &Anime{}
	anime.Title.Romaji = "Test Show"
	anime.Ep.Number = 12

	started := time.Now()
	if score, given := castAwaitScore(&Config{}, anime, 300*time.Millisecond); given {
		t.Errorf("a score of %d was saved with no keypress at all", score)
	}
	elapsed := time.Since(started)
	if elapsed < 250*time.Millisecond {
		t.Errorf("declined after %v, want the window to actually run", elapsed)
	}
	if elapsed > 3*time.Second {
		t.Errorf("took %v to decline, want it to end near the window", elapsed)
	}
}

// waitForCastSubscription blocks until the picker has subscribed.
//
// castDeliverCommand drops a command when no episode holds one, which is right in
// production and makes this flaky: a burst delivered before the subscription
// exists is discarded.
func waitForCastSubscription(t *testing.T) {
	t.Helper()
	for i := 0; i < 2000; i++ {
		castControls.mu.Lock()
		ready := castControls.live != nil
		castControls.mu.Unlock()
		if ready {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the picker never subscribed")
}

// Delivery waits for the subscription to exist. castDeliverCommand drops a
// command when no episode holds one, which is correct in production and would
// make this flaky: a goroutine started first would have its whole burst
// discarded before the picker had even subscribed.
//
// The gap is longer than a tick on purpose. castDeliverCommand refuses to queue
// a viewer leaning on a key, so a burst larger than the channel would have
// commands dropped rather than delivered.
func driveCastScore(t *testing.T, anime *Anime, commands []castCommand) (int, bool) {
	t.Helper()
	resetCastControlsForTest(t)
	resetCastSessionScreen(t)
	holdCastSessionPanel(t)
	castControlsBegin = func(*Config) bool { return true }

	go func() {
		waitForCastSubscription(t)
		// The picker subscribes and then discards whatever the episode left
		// buffered, so a key delivered the instant the subscription appears is
		// thrown away with it. A real viewer's keypress comes well after that.
		time.Sleep(100 * time.Millisecond)
		for _, command := range commands {
			castDeliverCommand(command)
			time.Sleep(60 * time.Millisecond)
		}
	}()

	return castAwaitScore(&Config{}, anime, 8*time.Second)
}

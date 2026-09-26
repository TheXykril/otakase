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

// The window must be visible while it runs, and it must say what happens if it
// runs out.
//
// The clock was computed and then used only in the no-panel branch, so the panel
// showed a bare "8/10" with no time at all. Then, once the clock was drawn, it
// read "9s to answer" -- which a viewer left alone would reasonably take as a
// countdown to being rated 8, rather than to losing the rating. Both are the
// same failure: the panel not telling the viewer what it is about to do to them.
func TestScorePickerShowsTheClockAndTheConsequence(t *testing.T) {
	resetCastControlsForTest(t)
	resetCastSessionScreen(t)
	holdCastSessionPanel(t)
	castControlsBegin = func(*Config) bool { return true }

	anime := &Anime{}
	anime.Title.Romaji = "Test Show"
	anime.Ep.Number = 12

	// Long enough that only the opening frame is drawn, and it must carry both.
	out := captureStdout(t, func() {
		castAwaitScore(&Config{}, anime, 20*time.Second)
	})

	if !strings.Contains(out, "Rate this anime:  8/10") {
		t.Fatalf("the score is not shown:\n%s", out)
	}
	if !strings.Contains(out, "20s left") && !strings.Contains(out, "19s left") {
		t.Errorf("the panel does not say how long is left:\n%s", out)
	}
	// The consequence, which is the part that was missing: doing nothing has to
	// read as losing the rating, not as getting one.
	if !strings.Contains(out, "or it is skipped") {
		t.Errorf("the panel does not say that doing nothing means no rating:\n%s", out)
	}
}

// Held separately because this is the sentence a rewrite would quietly drop, and
// the picker is the one place in the cast flow where the default outcome is to
// lose something the viewer wanted.
func TestScoreMessageNamesTheOutcomeOfWaiting(t *testing.T) {
	message := castScoreMessage(8, 7)

	if !strings.Contains(message, "8/10") {
		t.Errorf("the message does not show the score: %q", message)
	}
	if !strings.Contains(message, "7s left") {
		t.Errorf("the message does not show the clock: %q", message)
	}
}

// The consequence lives in the footer rather than the message, because the
// message row truncates from the right and the consequence is exactly what must
// not be the part that gets cut. Held separately so a tidy-up that merges the
// two lines back together fails here rather than in front of a viewer on a
// narrow terminal.
func TestScoreConsequenceIsInTheFooterNotTheMessage(t *testing.T) {
	if strings.Contains(castScoreMessage(8, 7), "skipped") {
		t.Error("the message carries the consequence, where truncation eats it")
	}
	if !strings.Contains(castScoreKeysUntouched, "skipped") {
		t.Errorf("the footer does not say that waiting loses the rating: %q", castScoreKeysUntouched)
	}
	// And the footer has to survive a narrow terminal, which is the whole reason
	// for the split. A cast window is an ordinary terminal and may be tiled.
	const wanted = "↑↓ score · enter save · or it is skipped"
	for _, width := range []int{50, 44, 40} {
		lines := castPanelStatusLines("Test Show", 12, "FakeTV",
			castScoreMessage(8, 7), width, castScoreKeysUntouched)
		if lipglossWidth(stripForTest(lines[2])) < lipglossWidth(wanted) {
			t.Errorf("at width %d the footer's consequence is truncated: %q",
				width, stripForTest(lines[2]))
		}
	}
}

// stripForTest removes the styling so a width can be measured on the text.
func stripForTest(line string) string {
	out := strings.Builder{}
	inEscape := false
	for _, r := range line {
		switch {
		case r == 0x1b:
			inEscape = true
		case inEscape && r == 'm':
			inEscape = false
		case !inEscape:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// Once the viewer has touched the score there is no clock and no deadline. The
// window is for absence -- a cast window nobody is at has to resolve by itself --
// not a budget for answering. A viewer standing at the keyboard moving a number
// has shown they are there, and a fixed deadline used to cut them off halfway
// through a run of presses with the score nearly chosen and no way to save it.
func TestTouchingTheScoreRemovesTheDeadline(t *testing.T) {
	resetCastControlsForTest(t)
	resetCastSessionScreen(t)
	holdCastSessionPanel(t)
	castControlsBegin = func(*Config) bool { return true }

	anime := &Anime{}
	anime.Title.Romaji = "Test Show"
	anime.Ep.Number = 12

	const window = 300 * time.Millisecond
	go func() {
		waitForCastSubscription(t)
		time.Sleep(150 * time.Millisecond) // clear the pre-loop drain
		castDeliverCommand(castCmdVolumeDown)
		// Well past the window that would have applied. With the deadline still
		// running this is where the picker would have declined.
		time.Sleep(4 * window)
		castDeliverCommand(castCmdSelect)
	}()

	score, given := castAwaitScore(&Config{}, anime, window)

	if !given {
		t.Fatal("the picker declined long after the viewer started choosing")
	}
	if score != 7 {
		t.Errorf("score = %d, want 7", score)
	}
}

// And the clock comes off the panel the moment they start, rather than sitting
// there counting at someone who is choosing.
func TestClockDisappearsOnceTheViewerIsChoosing(t *testing.T) {
	resetCastControlsForTest(t)
	resetCastSessionScreen(t)
	holdCastSessionPanel(t)
	castControlsBegin = func(*Config) bool { return true }

	anime := &Anime{}
	anime.Title.Romaji = "Test Show"
	anime.Ep.Number = 12

	out := captureStdout(t, func() {
		go func() {
			waitForCastSubscription(t)
			time.Sleep(150 * time.Millisecond)
			castDeliverCommand(castCmdVolumeUp)
			time.Sleep(400 * time.Millisecond)
			castDeliverCommand(castCmdSelect)
		}()
		castAwaitScore(&Config{}, anime, 20*time.Second)
	})

	if !strings.Contains(out, "Rate this anime:  8/10  ·  ") {
		t.Fatalf("the opening frame does not show a clock:\n%s", out)
	}

	// Every frame after the first adjustment must carry no clock. Compared per
	// frame rather than by substring, because the panel redraws in place and the
	// engaged frames are the ones that matter.
	var frames []string
	for _, line := range strings.Split(out, "\r\n") {
		if strings.Contains(line, "Rate this anime:") {
			frames = append(frames, line)
		}
	}
	if len(frames) < 2 {
		t.Fatalf("expected an opening frame and at least one after a keypress, got %d:\n%s",
			len(frames), out)
	}
	if !strings.Contains(frames[0], "s left") {
		t.Errorf("the opening frame has no clock: %q", frames[0])
	}
	for i, frame := range frames[1:] {
		if strings.Contains(frame, "s left") {
			t.Errorf("frame %d still shows a clock while choosing: %q", i+1, frame)
		}
	}
	if !strings.Contains(out, "9/10") {
		t.Errorf("the adjustment was not shown:\n%s", out)
	}
	if !strings.Contains(out, "q to skip") {
		t.Errorf("q is not offered as the way to decline once choosing:\n%s", out)
	}
}

// With the deadline gone once engaged, q is the only way out -- so it has to
// decline rather than fall through to a timeout the viewer can no longer see.
func TestQDeclinesTheRatingOnceChoosing(t *testing.T) {
	resetCastControlsForTest(t)
	resetCastSessionScreen(t)
	holdCastSessionPanel(t)
	castControlsBegin = func(*Config) bool { return true }

	anime := &Anime{}
	anime.Title.Romaji = "Test Show"
	anime.Ep.Number = 12

	go func() {
		waitForCastSubscription(t)
		time.Sleep(150 * time.Millisecond)
		castDeliverCommand(castCmdVolumeUp)
		castDeliverCommand(castCmdStop)
	}()

	// A short window that would have expired first if the deadline still applied.
	started := time.Now()
	if score, given := castAwaitScore(&Config{}, anime, 200*time.Millisecond); given {
		t.Errorf("q saved a rating of %d instead of declining it", score)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Errorf("q took %v to decline", elapsed)
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

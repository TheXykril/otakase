package internal

import (
	"strings"
	"testing"
	"time"
)

// resetCastSessionScreen puts the session screen back to unheld.
//
// Package-level state again: begun-ness, the panel pointer and the deferred
// summary are all shared by every test in this package, and a test that leaves
// one set makes the next believe a cast window is still holding the terminal.
func resetCastSessionScreen(t *testing.T) {
	t.Helper()
	endCastSessionScreen()
	takeCastDeferredSummary()
	castSetPanelOwnsScreen(false)
	t.Cleanup(func() {
		endCastSessionScreen()
		takeCastDeferredSummary()
		castSetPanelOwnsScreen(false)
	})
}

// beginCastSessionScreen asks for a terminal, and there is none under `go test`,
// so the panel pointer is installed directly to exercise what depends on it.
// What is under test is the ownership rule, not the escape sequences.
func holdCastSessionPanel(t *testing.T) {
	t.Helper()
	castSessionScreen.mu.Lock()
	castSessionScreen.panel = &castPanelWriter{width: 60}
	castSessionScreen.release = func() {}
	castSessionScreen.cancel = func() {}
	castPanelForControls = castSessionScreen.panel
	castSetPanelOwnsScreen(true)
	castSessionScreen.mu.Unlock()
}

// Holding the terminal for the cast is what gives the season-end prompts a frame
// to draw in. The panel pointer is the handle both CastEpisode and the prompts
// reach for, so it appearing and disappearing with the session is the contract.
func TestCastSessionScreenOwnsThePanelForItsLifetime(t *testing.T) {
	resetCastSessionScreen(t)

	if castPanelForControls != nil {
		t.Fatal("a panel is installed with no session running")
	}

	holdCastSessionPanel(t)
	held := castPanelForControls
	if held == nil {
		t.Fatal("holding the session screen installed no panel")
	}

	endCastSessionScreen()

	if castPanelForControls != nil {
		t.Error("the panel outlived the session that owned it")
	}
	if castPanelOwnsScreen() {
		t.Error("the screen is still owned after the session ended")
	}
	if held == castPanelForControls {
		t.Error("the same panel was left reachable")
	}
}

// A cast session holding the terminal turns Out into a notification, which is
// right for progress and wrong for the one line a viewer comes back to. The
// summary is held instead, and printed once the screen is released.
func TestCompletionSummaryIsHeldUntilTheScreenIsReleased(t *testing.T) {
	resetCastSessionScreen(t)
	holdCastSessionPanel(t)

	line := "Completion summary: rating skipped (cast window); sequel skipped (cast window)"

	// Through emitCompletionSummary, not deferCastSummary. Calling the defer
	// directly asserts that a setter sets, and passes even if the branch that
	// chooses to defer is deleted -- which is the failure this guards: without
	// it the summary goes to Out, becomes a desktop notification fired from
	// behind the panel, and the one line the viewer came back for is gone.
	emitCompletionSummary(line)

	if got := takeCastDeferredSummary(); got != line {
		t.Fatalf("deferred summary = %q, want %q", got, line)
	}
	// And it is consumed exactly once, so the session cannot print it twice.
	if got := takeCastDeferredSummary(); got != "" {
		t.Errorf("summary was handed out twice: %q", got)
	}
}

// With no session holding the terminal the summary is printed where it is
// written. Holding it would strand it in a variable nothing reads.
func TestCompletionSummaryIsNotHeldWithoutACastWindow(t *testing.T) {
	resetCastSessionScreen(t)
	castSetPanelOwnsScreen(false)

	// Again through the real decision. The previous version of this test called
	// takeCastDeferredSummary immediately after the reset had already emptied it,
	// so it asserted that a cleared variable was clear and would have passed
	// against an implementation that deferred every summary into the void.
	emitCompletionSummary("Completion summary: rating saved (9)")

	if got := takeCastDeferredSummary(); got != "" {
		t.Errorf("a summary was deferred with nothing to defer it for: %q", got)
	}
}

// Ending a session that was never begun is a no-op, so a caller does not have to
// remember whether it took one. RunCastSession releases unconditionally.
func TestEndingAnUnheldSessionScreenIsSafe(t *testing.T) {
	resetCastSessionScreen(t)

	endCastSessionScreen()
	endCastSessionScreen()

	if castPanelForControls != nil {
		t.Error("ending an unheld session left a panel behind")
	}
	if castPanelOwnsScreen() {
		t.Error("ending an unheld session left the screen owned")
	}
}

// A second begin must not take a second screen. CastEpisode's fallback and the
// session both ask, and only the first may win.
func TestBeginningTheSessionScreenTwiceTakesItOnce(t *testing.T) {
	resetCastSessionScreen(t)

	holdCastSessionPanel(t)
	first := castPanelForControls

	// beginCastSessionScreen finds no terminal and would return false here, but
	// it must not disturb a panel a session already holds.
	beginCastSessionScreen(&Config{})

	if castPanelForControls != first {
		t.Error("a second begin replaced the panel the session already held")
	}
}

// The rating countdown is drawn in the panel when there is one, and as plain
// text when there is not. Both are acceptable; a crash or a blocked prompt is
// not, so this pins that the panel path is taken when available.
func TestRatingCountdownDrawsInThePanelWhenThereIsOne(t *testing.T) {
	resetCastSessionScreen(t)
	resetCastControlsForTest(t)
	castControlsBegin = func(*Config) bool { return true }
	holdCastSessionPanel(t)

	anime := &Anime{}
	anime.Title.Romaji = "Test Show"
	anime.Ep.Number = 12

	out := captureStdout(t, func() {
		castAwaitYesNo(&Config{}, anime, "Rate this anime?", 400*time.Millisecond)
	})

	// The panel writes a framed line, so the box characters are the tell.
	if !strings.Contains(out, "╭") && !strings.Contains(out, "│") {
		t.Errorf("the countdown did not draw a panel frame:\n%s", out)
	}
	if !strings.Contains(out, "Rate this anime?") {
		t.Errorf("the question never reached the screen:\n%s", out)
	}
}

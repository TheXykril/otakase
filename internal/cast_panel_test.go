package internal

import (
	"strings"
	"testing"
)

func testPanelState() castPanelState {
	return castPanelState{
		Title:    "Rich Girl Caretaker",
		Episode:  12,
		Device:   "Office TV",
		Position: 724,
		Duration: 1451,
		State:    "PLAYING",
		Volume:   0.6,
	}
}

// The panel is the only thing on screen for twenty minutes, so everything a
// viewer needs has to be in it.
func TestCastPanelShowsWhatTheViewerNeeds(t *testing.T) {
	rendered := strings.Join(castPanelLines(testPanelState(), 60), "\n")

	for _, want := range []string{"Rich Girl Caretaker", "12", "Office TV", "12:04", "24:11", "PLAYING", "60%", "q"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the panel does not show %q:\n%s", want, rendered)
		}
	}
}

// A panel wider than the terminal wraps and destroys the in-place redraw, so
// every line has to fit the width it was given.
func TestCastPanelFitsItsWidth(t *testing.T) {
	for _, width := range []int{30, 40, 60, 100} {
		for i, line := range castPanelLines(testPanelState(), width) {
			if got := lipglossWidth(line); got > width {
				t.Errorf("at width %d, line %d is %d cells wide:\n%s", width, i, got, line)
			}
		}
	}
}

// A very narrow terminal must still produce something rather than a panel with
// negative interior width.
func TestCastPanelSurvivesANarrowTerminal(t *testing.T) {
	lines := castPanelLines(testPanelState(), 8)
	if len(lines) == 0 {
		t.Fatal("no panel was rendered at all")
	}
	for i, line := range lines {
		if lipglossWidth(line) > 8 {
			t.Errorf("line %d overflows a 8-cell terminal: %q", i, line)
		}
	}
}

// A device that has not reported a duration yet must not render a bar built by
// dividing by zero.
func TestCastPanelToleratesAnUnknownDuration(t *testing.T) {
	state := testPanelState()
	state.Duration = 0
	rendered := strings.Join(castPanelLines(state, 60), "\n")

	if strings.Contains(rendered, "NaN") || strings.Contains(rendered, "Inf") {
		t.Errorf("the panel divided by zero:\n%s", rendered)
	}
}

// The panel is redrawn in place, so it has to erase exactly what it drew last
// time. Getting this wrong leaves a trail of stale frames down the terminal.
func TestCastPanelRedrawErasesThePreviousFrame(t *testing.T) {
	panel := &castPanelWriter{width: 60}

	first := panel.frame(testPanelState())
	if strings.Contains(first, "\033[") && strings.Contains(first, "A") {
		t.Error("the first frame moves the cursor up, over output that is not the panel")
	}

	second := panel.frame(testPanelState())
	if !strings.Contains(second, "\033[4A") {
		t.Errorf("the second frame does not move back over the 4 lines it drew:\n%q", second)
	}
}

// In raw mode a bare \n moves down without returning to the left margin, so
// every line has to end \r\n or the frame staircases across the screen.
func TestCastPanelUsesRawModeLineEndings(t *testing.T) {
	panel := &castPanelWriter{width: 60}
	frame := panel.frame(testPanelState())

	for _, line := range strings.Split(strings.TrimSuffix(frame, "\r\n"), "\r\n") {
		if strings.Contains(line, "\n") {
			t.Errorf("a line ends with a bare newline, which staircases in raw mode: %q", line)
		}
	}
}

// A message printed mid-episode has to clear the panel first, and the next
// frame must then draw fresh rather than erasing lines the message now owns.
func TestCastPanelClearForgetsTheFrame(t *testing.T) {
	panel := &castPanelWriter{width: 60}
	panel.frame(testPanelState())

	cleared := panel.clear()
	if !strings.Contains(cleared, "\033[4A") {
		t.Errorf("clearing does not move back over the panel:\n%q", cleared)
	}

	next := panel.frame(testPanelState())
	if strings.Contains(next, "A") && strings.Contains(next, "\033[4A") {
		t.Error("after clearing, the next frame still tried to erase a panel that is gone")
	}
}

// Light-novel titles are long. The device has to stay readable anyway: it is
// the only thing telling the viewer where the episode is playing.
func TestCastPanelKeepsTheDeviceVisibleBesideALongTitle(t *testing.T) {
	state := testPanelState()
	state.Title = "Rich Girl Caretaker: I'm Secretly the Caregiver of the Most Popular Girl in This Rich Kid School"

	top := castPanelLines(state, 72)[0]

	if !strings.Contains(top, "Office TV") {
		t.Errorf("a long title pushed the device out of the header:\n%s", top)
	}
	if lipglossWidth(top) > 72 {
		t.Errorf("the header is %d cells wide, over the 72 it was given:\n%s", lipglossWidth(top), top)
	}
}

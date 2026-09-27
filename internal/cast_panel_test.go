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

// When the panel owns the screen it redraws by going home and wiping, which is
// what makes it survive a resize and a frame that changes height. Counting
// back over the last frame is only for the case where it shares the terminal.
func TestCastPanelDrawsFromHomeWhenItOwnsTheScreen(t *testing.T) {
	panel := &castPanelWriter{width: 60, home: true}

	first := panel.frame(testPanelState())
	if !strings.HasPrefix(first, "\033[H\033[J") {
		t.Errorf("the first frame does not go home and wipe:\n%q", first[:12])
	}

	second := panel.frame(testPanelState())
	if strings.Contains(second, "A") && strings.Contains(second, "\033[4A") {
		t.Error("a panel that owns the screen is still counting lines back")
	}
}

// The terminal can be resized mid-episode. A panel that measured once at
// startup keeps drawing at the old width, which either wraps or leaves a gap.
func TestCastPanelFollowsAResize(t *testing.T) {
	width, height := 100, 30
	panel := &castPanelWriter{home: true, size: func() (int, int) { return width, height }}

	wide := panel.frame(testPanelState())
	width = 50
	narrow := panel.frame(testPanelState())

	widest := func(frame string) int {
		longest := 0
		for _, line := range strings.Split(frame, "\r\n") {
			if w := lipglossWidth(line); w > longest {
				longest = w
			}
		}
		return longest
	}

	if widest(narrow) >= widest(wide) {
		t.Errorf("the panel did not narrow with the terminal: %d then %d", widest(wide), widest(narrow))
	}
	if widest(narrow) > 50 {
		t.Errorf("the panel is %d cells wide in a 50-cell terminal", widest(narrow))
	}
}

// Centred, so it looks deliberate at any size rather than pinned to a corner.
func TestCastPanelCentresItselfWhenItOwnsTheScreen(t *testing.T) {
	panel := &castPanelWriter{home: true, size: func() (int, int) { return 100, 24 }}

	// The frame opens with a cursor escape, which is not whitespace -- so the
	// blank lines above the panel are counted by finding the framed line
	// rather than by scanning for the first non-blank one.
	lines := strings.Split(strings.TrimSuffix(panel.frame(testPanelState()), "\r\n"), "\r\n")

	framedAt := -1
	for i, line := range lines {
		if strings.Contains(line, "╭") {
			framedAt = i
			break
		}
	}
	if framedAt < 0 {
		t.Fatal("no framed line was drawn")
	}
	if framedAt == 0 {
		t.Error("the panel is pinned to the top of the screen rather than centred")
	}
	if !strings.HasPrefix(lines[framedAt], " ") {
		t.Error("the panel is flush to the left edge rather than centred")
	}
}

// A terminal too short to centre in must still show the panel.
func TestCastPanelSurvivesAShortTerminal(t *testing.T) {
	panel := &castPanelWriter{home: true, size: func() (int, int) { return 80, 3 }}

	frame := panel.frame(testPanelState())
	if !strings.Contains(frame, "PLAYING") {
		t.Errorf("the panel vanished in a 3-line terminal:\n%q", frame)
	}
}

// Before playback there is no position to show, but there is something worth
// saying -- remuxing, waiting for the device. The frame carries it, so the
// terminal never has to scroll text past the panel.
func TestCastPanelStatusFrameCarriesAMessage(t *testing.T) {
	panel := &castPanelWriter{home: true, size: func() (int, int) { return 80, 24 }}

	frame := panel.status("Rich Girl Caretaker", 12, "Office TV", "Preparing the stream…", castPanelPlaybackKeys)

	for _, want := range []string{"Rich Girl Caretaker", "Office TV", "Preparing the stream…"} {
		if !strings.Contains(frame, want) {
			t.Errorf("the status frame does not show %q:\n%s", want, frame)
		}
	}
	if strings.Contains(frame, "vol ") {
		t.Errorf("the status frame shows a volume that is not known yet:\n%s", frame)
	}
}

// The status frame and the playing panel share a frame, so switching between
// them must not leave part of the other behind.
func TestCastPanelStatusAndPlayingFramesBothRedrawFromHome(t *testing.T) {
	panel := &castPanelWriter{home: true, size: func() (int, int) { return 80, 24 }}

	if s := panel.status("Show", 1, "TV", "Preparing…", castPanelPlaybackKeys); !strings.HasPrefix(s, "\033[H\033[J") {
		t.Error("the status frame does not go home and wipe")
	}
	if p := panel.frame(testPanelState()); !strings.HasPrefix(p, "\033[H\033[J") {
		t.Error("the playing frame does not go home and wipe")
	}
}

// The episode number is the shortest and most load-bearing thing in the
// header, and truncating title-and-episode as one string ate it first: the
// trim runs off the end, and the end is where the episode number sits.
func TestCastPanelKeepsTheEpisodeNumberBesideALongTitle(t *testing.T) {
	state := testPanelState()
	state.Title = "Rich Girl Caretaker: I'm Secretly the Caregiver of the Most Popular Girl in This Rich Kid School"

	top := castPanelLines(state, 84)[0]

	if !strings.Contains(top, "Ep 12") {
		t.Errorf("a long title cut the episode number out of the header:\n%s", top)
	}
	if !strings.Contains(top, "Office TV") {
		t.Errorf("a long title pushed the device out of the header:\n%s", top)
	}
	if lipglossWidth(top) > 84 {
		t.Errorf("the header is %d cells wide, over the 84 it was given", lipglossWidth(top))
	}
}

// A title short enough to fit keeps every character -- nothing is trimmed just
// because the budget exists.
func TestCastPanelDoesNotTrimAShortTitle(t *testing.T) {
	state := testPanelState()
	state.Title = "Frieren"

	top := castPanelLines(state, 84)[0]
	if !strings.Contains(top, "Frieren · Ep 12") {
		t.Errorf("a short title was altered:\n%s", top)
	}
}

// AniList reports an average episode length for a series, not the length of the
// episode in hand: 24:00 for one that runs 24:40. Casting falls back to that
// average whenever the probe could not read the source, and a viewer shown a
// flat total has no way to tell that from a measurement -- they just see the
// bar stop short of the end.
func TestAnEstimatedTotalIsMarkedAsAnEstimate(t *testing.T) {
	state := testPanelState()
	state.Duration = 24 * 60
	state.Estimated = true

	rendered := strings.Join(castPanelLines(state, 60), "\n")
	if !strings.Contains(rendered, "~24:00") {
		t.Fatalf("an estimated total was not marked as one:\n%s", rendered)
	}
}

func TestAMeasuredTotalIsShownPlainly(t *testing.T) {
	rendered := strings.Join(castPanelLines(testPanelState(), 60), "\n")
	if strings.Contains(rendered, "~") {
		t.Fatalf("a measured total was marked as an estimate:\n%s", rendered)
	}
}

// The narrow fallback shows the same clock and needs the same distinction: a
// terminal too small to frame is not a terminal owed worse information.
func TestTheNarrowPanelAlsoMarksAnEstimate(t *testing.T) {
	state := testPanelState()
	state.Duration = 24 * 60
	state.Estimated = true

	line := castPanelPlainLine(state, castPanelMinWidth-1)
	if !strings.Contains(line, "~") {
		t.Fatalf("the narrow panel dropped the estimate mark: %q", line)
	}
}

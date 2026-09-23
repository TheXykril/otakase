package internal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// castPanelState is everything the control panel shows.
type castPanelState struct {
	Title    string
	Episode  int
	Device   string
	Position float64
	Duration float64
	State    string
	Volume   float64
}

// castPanelMinWidth is the narrowest terminal the boxed panel is drawn in.
// Below it the box has no usable interior and a single plain line is clearer
// than a broken frame.
const castPanelMinWidth = 34

// castPanelMaxWidth stops the panel stretching across an ultrawide terminal,
// where a frame the full width of the screen is harder to read, not easier.
const castPanelMaxWidth = 72

// castPanelSegment is a run of text with one style. Width is measured on the
// text, so colour never changes the geometry.
type castPanelSegment struct {
	text  string
	style lipgloss.Style
}

// castPanelLines renders the panel as lines of exactly the given width.
//
// Lines carry no line endings: the caller adds them, and in raw mode that has
// to be \r\n rather than \n, because raw mode turns off the translation that
// would otherwise return the cursor to the left margin.
func castPanelLines(state castPanelState, width int) []string {
	if width > castPanelMaxWidth {
		width = castPanelMaxWidth
	}
	if width < castPanelMinWidth {
		return []string{castPanelPlainLine(state, width)}
	}

	inner := width - 4 // two border cells and one space of padding each side

	elapsed := castClock(state.Position)
	total := "--:--"
	if state.Duration > 0 {
		total = castClock(state.Duration)
	}
	clock := fmt.Sprintf("%s / %s", elapsed, total)

	bar := castPanelBar(state.Position, state.Duration, inner-lipgloss.Width(clock)-2)
	volume := fmt.Sprintf("vol %d%%", int(state.Volume*100+0.5))
	status := castPanelStateMark(state.State) + " " + state.State

	return []string{
		castPanelTop(state, width),
		castPanelRow([]castPanelSegment{
			{bar, castPanelBarStyle},
			{"  " + clock, castPanelDimStyle},
		}, inner),
		castPanelRow([]castPanelSegment{
			{status, castPanelStateStyle(state.State)},
			{strings.Repeat(" ", max(1, inner-lipgloss.Width(status)-lipgloss.Width(volume))), lipgloss.NewStyle()},
			{volume, castPanelDimStyle},
		}, inner),
		castPanelBottom(width),
	}
}

// castPanelPlainLine is the fallback for a terminal too narrow to frame.
func castPanelPlainLine(state castPanelState, width int) string {
	total := "--:--"
	if state.Duration > 0 {
		total = castClock(state.Duration)
	}
	line := fmt.Sprintf("%s %s/%s %d%%", state.State, castClock(state.Position), total, int(state.Volume*100+0.5))
	return castPanelTruncate(line, width)
}

// castPanelRow frames one content line and pads it to the interior width.
func castPanelRow(segments []castPanelSegment, inner int) string {
	var plain, styled strings.Builder
	for _, segment := range segments {
		plain.WriteString(segment.text)
		styled.WriteString(segment.style.Render(segment.text))
	}

	used := lipgloss.Width(plain.String())
	if used > inner {
		// Rendered styles cannot be truncated safely, so fall back to plain
		// text when the content does not fit.
		return castPanelBorderStyle.Render("│") + " " + castPanelTruncate(plain.String(), inner) + " " + castPanelBorderStyle.Render("│")
	}
	return castPanelBorderStyle.Render("│") + " " + styled.String() + strings.Repeat(" ", inner-used) + " " + castPanelBorderStyle.Render("│")
}

// castPanelTop is the framed header carrying the show, episode and device.
func castPanelTop(state castPanelState, width int) string {
	left := fmt.Sprintf(" %s · Ep %d ", state.Title, state.Episode)
	right := ""
	if state.Device != "" {
		right = fmt.Sprintf(" %s ", state.Device)
	}

	// The show's title is what gives when there is not enough room: the
	// episode number and the device are short and both matter.
	fixed := 2 + lipgloss.Width(right) + 2 // corners plus two dashes
	if over := lipgloss.Width(left) + fixed - width; over > 0 {
		left = castPanelTruncate(left, max(0, lipgloss.Width(left)-over))
	}

	fill := width - 2 - lipgloss.Width(left) - lipgloss.Width(right)
	if fill < 0 {
		fill = 0
	}
	return castPanelBorderStyle.Render("╭") +
		castPanelTitleStyle.Render(left) +
		castPanelBorderStyle.Render(strings.Repeat("─", fill)) +
		castPanelDimStyle.Render(right) +
		castPanelBorderStyle.Render("╮")
}

// castPanelBottom is the framed footer carrying the keys.
func castPanelBottom(width int) string {
	keys := " space · ←→ · ↑↓ · s skip · q stop "
	if lipgloss.Width(keys)+2 > width {
		keys = " space ←→ ↑↓ s q "
	}
	if lipgloss.Width(keys)+2 > width {
		keys = ""
	}
	fill := width - 2 - lipgloss.Width(keys)
	if fill < 0 {
		fill = 0
	}
	return castPanelBorderStyle.Render("╰") +
		castPanelDimStyle.Render(keys) +
		castPanelBorderStyle.Render(strings.Repeat("─", fill)) +
		castPanelBorderStyle.Render("╯")
}

// castPanelBar is the progress bar, drawn with full and empty blocks.
func castPanelBar(position, duration float64, width int) string {
	if width < 1 {
		return ""
	}
	if duration <= 0 {
		return strings.Repeat("░", width)
	}
	filled := int(position / duration * float64(width))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// castPanelStateMark is the glyph that makes the player state readable at a
// glance, without relying on colour alone.
func castPanelStateMark(state string) string {
	switch state {
	case "PLAYING":
		return "▶"
	case "PAUSED":
		return "⏸"
	default:
		return "•"
	}
}

// castPanelTruncate cuts text to fit width, measured in terminal cells rather
// than bytes so a title with wide characters does not overflow the frame.
func castPanelTruncate(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(text) <= width {
		return text + strings.Repeat(" ", width-lipgloss.Width(text))
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(text)
}

// lipglossWidth measures a rendered line in terminal cells, ignoring the ANSI
// escapes that colour it.
func lipglossWidth(line string) int { return lipgloss.Width(line) }

// Panel styles, set by ApplyTheme so the panel follows the same palette as the
// menus rather than inventing its own colours.
var (
	castPanelBorderStyle = lipgloss.NewStyle()
	castPanelTitleStyle  = lipgloss.NewStyle().Bold(true)
	castPanelDimStyle    = lipgloss.NewStyle()
	castPanelBarStyle    = lipgloss.NewStyle()
	castPanelPlayStyle   = lipgloss.NewStyle()
	castPanelPauseStyle  = lipgloss.NewStyle()
)

// castPanelStateStyle colours the player state, so a paused episode is
// distinguishable at a glance as well as by its glyph.
func castPanelStateStyle(state string) lipgloss.Style {
	switch state {
	case "PLAYING":
		return castPanelPlayStyle
	case "PAUSED":
		return castPanelPauseStyle
	default:
		return castPanelDimStyle
	}
}

// castPanelWriter draws the panel in place, erasing the frame it drew last.
//
// It writes nothing itself: frame and clear return the bytes, so the redraw
// arithmetic is testable without a terminal. Raw mode is assumed -- that is
// where the panel lives -- so lines end \r\n, because raw mode turns off the
// translation that would otherwise return the cursor to the left margin and a
// bare \n would staircase the frame across the screen.
type castPanelWriter struct {
	width int
	drawn int // lines currently on screen, 0 when nothing is
}

// frame is the escape sequence and text that replaces the panel on screen.
func (c *castPanelWriter) frame(state castPanelState) string {
	var out strings.Builder
	out.WriteString(c.erase())

	lines := castPanelLines(state, c.width)
	for _, line := range lines {
		out.WriteString(line)
		out.WriteString("\r\n")
	}
	c.drawn = len(lines)
	return out.String()
}

// clear removes the panel and forgets it, for a message that needs the screen.
func (c *castPanelWriter) clear() string {
	erased := c.erase()
	c.drawn = 0
	return erased
}

// erase moves back over the drawn frame and wipes from there to the end of the
// screen. Clearing to the end rather than line by line is what makes a frame
// that shrinks -- a narrowed terminal, a shorter title -- leave nothing behind.
func (c *castPanelWriter) erase() string {
	if c.drawn == 0 {
		return ""
	}
	return fmt.Sprintf("\033[%dA\r\033[J", c.drawn)
}

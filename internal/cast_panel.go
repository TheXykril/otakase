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
// where a frame the full width of the screen is harder to read, not easier --
// a long line of progress bar tells a viewer nothing the short one did not.
const castPanelMaxWidth = 84

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
	right := ""
	if state.Device != "" {
		right = fmt.Sprintf(" %s ", state.Device)
	}

	// The episode number is reserved before the title is measured, and the
	// title is trimmed to whatever is left over. Trimming the two together cut
	// the episode number off first -- it sits at the end, which is where a
	// trim starts eating -- so a long title left the header saying nothing
	// about which episode was playing.
	episode := fmt.Sprintf(" · Ep %d ", state.Episode)

	// The title is what gives when there is not enough room: the episode
	// number and the device are short and both matter, and a full light-novel
	// title would otherwise eat the whole frame.
	fixed := 2 + lipgloss.Width(right) + lipgloss.Width(episode) + 2 // corners, episode, two dashes
	budget := width - fixed
	if half := width / 2; budget > half {
		budget = half
	}

	title := " " + state.Title
	if budget < 1 {
		budget = 1
	}
	if lipgloss.Width(title) > budget {
		title = castPanelTruncate(title, budget)
	}
	left := title + episode

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
	keys := " space · ←→ · ↑↓ · q stop "
	if lipgloss.Width(keys)+2 > width {
		keys = " space ←→ ↑↓ q "
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
	// An ellipsis, so a cut title reads as a cut title rather than as a
	// rendering fault.
	if width == 1 {
		return "\u2026"
	}
	return lipgloss.NewStyle().MaxWidth(width-1).Render(text) + "\u2026"
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
	width int  // fixed width, used when size is nil
	drawn int  // lines currently on screen, 0 when nothing is
	home  bool // the panel owns the screen and draws from the top left

	// size reports the terminal's width and height. It is read on every frame
	// rather than once at startup, so a terminal resized mid-episode is
	// followed rather than ignored. nil means use width and do not centre,
	// which is the case where the panel shares the terminal with other output.
	size func() (int, int)
}

// frame is the escape sequence and text that replaces the panel on screen.
func (c *castPanelWriter) frame(state castPanelState) string {
	var out strings.Builder
	out.WriteString(c.erase())

	width, height := c.width, 0
	if c.size != nil {
		width, height = c.size()
		// A margin either side, so the frame is not welded to the edges of the
		// terminal.
		width -= 4
	}

	lines := castPanelLines(state, width)

	// Centred, both ways, so the panel looks placed rather than parked in a
	// corner -- and so it stays put as the terminal changes size.
	if c.size != nil {
		termWidth, _ := c.size()
		lines = castPanelCentre(lines, termWidth, height)
	}

	for _, line := range lines {
		out.WriteString(line)
		out.WriteString("\r\n")
	}
	c.drawn = len(lines)
	return out.String()
}

// castPanelCentre pads the panel into the middle of a terminal of this size.
//
// Vertical centring is dropped when the terminal is too short to hold the
// panel and any blank lines: showing the panel matters more than placing it.
func castPanelCentre(lines []string, width, height int) []string {
	panelWidth := 0
	for _, line := range lines {
		if w := lipgloss.Width(line); w > panelWidth {
			panelWidth = w
		}
	}

	if left := (width - panelWidth) / 2; left > 0 {
		pad := strings.Repeat(" ", left)
		padded := make([]string, len(lines))
		for i, line := range lines {
			padded[i] = pad + line
		}
		lines = padded
	}

	if above := (height - len(lines)) / 2; above > 0 {
		lines = append(make([]string, above), lines...)
	}
	return lines
}

// status draws the same frame before playback, carrying what otakase is doing
// instead of a position it does not have yet.
//
// It exists so nothing has to scroll past the panel: the terminal shows the
// frame from the moment a device is chosen, and every message that would have
// been printed becomes a notification.
func (c *castPanelWriter) status(title string, episode int, device, message string) string {
	var out strings.Builder
	out.WriteString(c.erase())

	width := c.width
	height := 0
	if c.size != nil {
		width, height = c.size()
		width -= 4
	}

	lines := castPanelStatusLines(title, episode, device, message, width)
	if c.size != nil {
		termWidth, _ := c.size()
		lines = castPanelCentre(lines, termWidth, height)
	}

	for _, line := range lines {
		out.WriteString(line)
		out.WriteString("\r\n")
	}
	c.drawn = len(lines)
	return out.String()
}

// castPanelStatusLines is the frame before playback: header, one message, keys.
func castPanelStatusLines(title string, episode int, device, message string, width int) []string {
	if width > castPanelMaxWidth {
		width = castPanelMaxWidth
	}
	if width < castPanelMinWidth {
		return []string{castPanelTruncate(message, width)}
	}

	inner := width - 4
	state := castPanelState{Title: title, Episode: episode, Device: device}

	return []string{
		castPanelTop(state, width),
		castPanelRow([]castPanelSegment{{message, castPanelDimStyle}}, inner),
		castPanelBottom(width),
	}
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
	// Owning the screen makes this trivial: go home and wipe. No counting of
	// lines, and nothing to get wrong when the terminal is resized or a frame
	// changes height.
	if c.home {
		return "\033[H\033[J"
	}
	if c.drawn == 0 {
		return ""
	}
	return fmt.Sprintf("\033[%dA\r\033[J", c.drawn)
}

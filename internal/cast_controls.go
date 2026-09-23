package internal

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"

	"github.com/thexykril/otakase/internal/cast"
)

// castCommand is one thing a viewer asked for while a cast is playing.
type castCommand int

const (
	castCmdNone castCommand = iota
	castCmdPauseToggle
	castCmdSeekBack
	castCmdSeekForward
	castCmdVolumeUp
	castCmdVolumeDown
	castCmdSkipSpan
	castCmdStop
)

// castSeekStep is how far one arrow press moves the position.
const castSeekStep = 10.0

// castVolumeStep is how much one arrow press moves the device volume, on the
// 0..1 scale the receiver uses.
const castVolumeStep = 0.05

// castStatusBarWidth is how many cells the progress bar occupies.
const castStatusBarWidth = 16

// decodeCastKey reads one keypress from the front of buf and reports how many
// bytes it consumed.
//
// A return of (castCmdNone, 0) means buf holds the start of an escape sequence
// whose remaining bytes have not arrived: a terminal is free to deliver an
// arrow key's three bytes across separate reads, and consuming a partial one
// would turn a single arrow into a stray "[" plus a wrong command.
func decodeCastKey(buf []byte) (castCommand, int) {
	if len(buf) == 0 {
		return castCmdNone, 0
	}

	if buf[0] == 0x1b {
		if len(buf) < 3 {
			return castCmdNone, 0
		}
		if buf[1] != '[' {
			return castCmdNone, 2
		}
		switch buf[2] {
		case 'A':
			return castCmdVolumeUp, 3
		case 'B':
			return castCmdVolumeDown, 3
		case 'C':
			return castCmdSeekForward, 3
		case 'D':
			return castCmdSeekBack, 3
		}
		return castCmdNone, 3
	}

	switch buf[0] {
	case ' ':
		return castCmdPauseToggle, 1
	case 'q', 'Q', 0x03:
		return castCmdStop, 1
	case 's', 'S':
		return castCmdSkipSpan, 1
	}
	return castCmdNone, 1
}

// castStatusLine renders the one line a viewer watches while casting.
//
// It is rewritten in place rather than appended, so it carries a leading
// carriage return and trailing spaces to cover whatever the previous, possibly
// longer, line left behind.
func castStatusLine(position, duration float64, state string, volume float64) string {
	bar := strings.Repeat("-", castStatusBarWidth)
	if duration > 0 {
		filled := int(position / duration * float64(castStatusBarWidth))
		if filled < 0 {
			filled = 0
		}
		if filled > castStatusBarWidth {
			filled = castStatusBarWidth
		}
		bar = strings.Repeat("#", filled) + strings.Repeat("-", castStatusBarWidth-filled)
	}

	elapsed := castClock(position)
	total := "--:--"
	if duration > 0 {
		total = castClock(duration)
	}

	return fmt.Sprintf("\r  %s / %s  %s  %s  vol %d%%   ",
		elapsed, total, bar, state, int(volume*100+0.5))
}

// castClock formats seconds as m:ss, or h:mm:ss once an hour is reached.
func castClock(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	total := int(seconds)
	h, m, s := total/3600, (total%3600)/60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// applyCastCommand carries out one viewer command against the device and
// reports whether the cast should end.
//
// It takes the last known position rather than polling for a fresh one: a
// keypress should act on what the viewer is looking at, and a poll here would
// add a network round trip to every press.
func applyCastCommand(command castCommand, session castSession, paused *bool, spans []cast.Span, position float64) (stop bool, moved float64, err error) {
	// seek centralises the two things every seek must do besides seeking: it
	// reports the new position so repeated presses compound instead of each
	// acting on the last polled one, and it clears paused, because the
	// receiver resumes playback on any seek (ResumeState "PLAYBACK_START").
	// Leaving paused set there would desync the status line and, worse, hold
	// the stall bound off for the rest of the episode.
	seek := func(target float64) (bool, float64, error) {
		if target < 0 {
			target = 0
		}
		*paused = false
		return false, target, session.SeekToTime(target)
	}

	switch command {
	case castCmdStop:
		return true, position, nil

	case castCmdPauseToggle:
		if *paused {
			*paused = false
			return false, position, session.Unpause()
		}
		*paused = true
		return false, position, session.Pause()

	case castCmdSeekBack:
		return seek(position - castSeekStep)

	case castCmdSeekForward:
		return seek(position + castSeekStep)

	case castCmdVolumeUp:
		return false, position, session.SetVolume(session.Volume() + castVolumeStep)

	case castCmdVolumeDown:
		return false, position, session.SetVolume(session.Volume() - castVolumeStep)

	case castCmdSkipSpan:
		// The span the position is inside, if any; otherwise the next one
		// ahead of it. Pressing s before the opening should reach it.
		if target, ok := cast.NextSkip(position, spans); ok {
			return seek(target)
		}
		// The nearest span that starts ahead of here. Tracked by Start and
		// seeked to by End, kept as separate variables: comparing a candidate
		// Start against a stored End picks the wrong span the moment two
		// overlap.
		nearestStart, target := 0.0, 0.0
		found := false
		for _, span := range spans {
			if span.End <= span.Start || span.Start <= position {
				continue
			}
			if !found || span.Start < nearestStart {
				nearestStart, target, found = span.Start, span.End, true
			}
		}
		if found {
			return seek(target)
		}
		// Said once, however many times the key is pressed: a viewer who holds
		// s on an episode with no known times would otherwise push the panel
		// down the screen a line at a time.
		if !noSpansReported {
			noSpansReported = true
			castOut(true, "No opening or ending times are known for this episode.")
		}
		return false, position, nil
	}
	return false, position, nil
}

// castPanelForControls is the panel the watch loop draws while controls are
// live, or nil when they are not. castOut needs to reach it to clear the frame
// before printing, and the loop is the only writer, so one package-level
// pointer is simpler than threading it through every message.
var castPanelForControls *castPanelWriter

// castOut prints a message that must not land on top of the control panel.
//
// In raw mode a bare \n does not return the cursor, so the message is written
// with \r\n line endings rather than through Out, which uses fmt.Println.
func castOut(haveControls bool, message string) {
	// When the panel owns the screen there is nowhere to print: Out turns the
	// message into a notification, which is the whole point of the takeover.
	if !haveControls || castPanelForControls == nil || castPanelOwnsScreen() {
		Out(message)
		return
	}
	fmt.Print(castPanelForControls.clear())
	for _, line := range strings.Split(message, "\n") {
		fmt.Print(line + "\r\n")
	}
	Log(message)
}

// startCastControls puts the terminal in raw mode and reads keys from it until
// the process exits, reporting whether controls are available at all.
//
// They need a terminal to read from: a rofi launch has no stdin worth reading,
// and a piped one is not a viewer. When unavailable the caller carries on with
// no controls rather than failing, because a cast with no keyboard is still a
// cast.
func startCastControls(config *Config) (<-chan castCommand, bool) {
	if config != nil && config.RofiSelection {
		return nil, false
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, false
	}

	state, err := term.MakeRaw(fd)
	if err != nil {
		Log(fmt.Sprintf("cast: could not read keys: %v", err))
		return nil, false
	}

	// Raw mode outlives every defer in this call when the process exits on a
	// signal, and a terminal left raw is a terminal the viewer has to reset by
	// hand. The exit path restores it for the same reason it tears down ffmpeg.
	castSetRawMode(true)
	restore := func() {
		castSetRawMode(false)
		_ = term.Restore(fd, state)
	}
	RegisterExitCleanup(restore)

	commands := make(chan castCommand, 8)
	go func() {
		defer restore()
		buf := make([]byte, 0, 16)
		chunk := make([]byte, 8)
		for {
			n, err := os.Stdin.Read(chunk)
			if n > 0 {
				buf = append(buf, chunk[:n]...)
				for len(buf) > 0 {
					command, used := decodeCastKey(buf)
					if used == 0 {
						break // an escape sequence still arriving
					}
					buf = buf[used:]
					if command != castCmdNone {
						select {
						case commands <- command:
						default: // a viewer leaning on a key is not a queue
						}
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()

	return commands, true
}

// castTerminalWidth is how wide the panel may be drawn, falling back to a
// conservative 80 when the terminal will not say.
func castTerminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width < 1 {
		return 80
	}
	return width
}

// castStdoutIsTerminal reports whether the panel has somewhere to draw.
func castStdoutIsTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// castRawMode records whether the terminal is in raw mode, so Out can pick the
// line ending that works there. A package-level flag rather than a parameter
// because Out is called from everywhere and only the cast path changes this.
var castRawMode struct {
	mu     sync.Mutex
	active bool
}

func castSetRawMode(active bool) {
	castRawMode.mu.Lock()
	castRawMode.active = active
	castRawMode.mu.Unlock()
}

func castRawModeActive() bool {
	castRawMode.mu.Lock()
	defer castRawMode.mu.Unlock()
	return castRawMode.active
}

// noSpansReported keeps the "no skip times" notice to once per cast.
var noSpansReported bool

// castScreen tracks whether the panel has taken the terminal over, so Out can
// send a notification instead of printing into a frame it would corrupt.
var castScreen struct {
	mu    sync.Mutex
	owned bool
}

func castSetPanelOwnsScreen(owned bool) {
	castScreen.mu.Lock()
	castScreen.owned = owned
	castScreen.mu.Unlock()
}

// castPanelOwnsScreen reports whether the cast panel is the only thing on the
// terminal.
func castPanelOwnsScreen() bool {
	castScreen.mu.Lock()
	defer castScreen.mu.Unlock()
	return castScreen.owned
}

// castTakeScreen gives the panel the whole terminal: the alternate buffer, so
// the viewer's scrollback survives, with the cursor hidden so it does not sit
// blinking inside the frame.
//
// It returns the function that gives it back, which the caller must run on
// every exit -- including the signal path, where it is registered as a cleanup.
func castTakeScreen() func() {
	fmt.Print("\033[?1049h\033[2J\033[H\033[?25l")
	castSetPanelOwnsScreen(true)

	var once sync.Once
	return func() {
		once.Do(func() {
			castSetPanelOwnsScreen(false)
			fmt.Print("\033[?25h\033[?1049l")
		})
	}
}

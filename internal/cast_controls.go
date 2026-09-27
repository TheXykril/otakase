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

// castControls is the process-wide keyboard reader.
//
// CastEpisode used to run once per process, so each call could enter raw mode
// and start a reader of its own. A cast now plays a whole season, and doing it
// per episode breaks twice over: two goroutines parked in os.Stdin.Read split
// the keypresses between them, roughly half of them vanishing into the
// abandoned channel of a finished episode, and a second term.MakeRaw snapshots
// the already-raw termios as the state to restore -- so the restores registered
// for the exit run in the wrong direction and leave the viewer's shell raw.
//
// So the terminal is taken over once and the reader runs for the process
// lifetime. Each episode takes a subscription instead and releases it on the
// way out; the reader fans every decoded command to the live subscription only.
//
// The reader is not stopped between episodes on purpose: it is parked in
// os.Stdin.Read and would not notice a stop channel until the next keypress,
// which is exactly the window where two readers would race.
var castControls struct {
	mu sync.Mutex
	// begun records that the terminal setup has been attempted, so a run with
	// no terminal does not retry it on every episode.
	begun bool
	// usable records whether that attempt succeeded.
	usable bool
	// live is the subscription of the episode playing now, or nil between
	// episodes.
	live chan castCommand
}

// castControlsBegin puts the terminal in raw mode and starts the one reader
// goroutine, reporting whether controls are available at all.
//
// A variable so a test can exercise the subscription bookkeeping twice in one
// process without a tty -- which is precisely what no existing test could do,
// and how a per-episode reader got this far.
var castControlsBegin = castControlsBeginTerminal

// startCastControls subscribes this episode to the viewer's keypresses.
//
// It reports whether controls are available at all: they need a terminal to
// read from, a rofi launch has no stdin worth reading and a piped one is not a
// viewer. When unavailable the caller carries on with no controls rather than
// failing, because a cast with no keyboard is still a cast.
//
// The returned release ends this episode's subscription and must be called on
// every path out, so the next episode's keys are not delivered to a channel
// nobody is reading.
func startCastControls(config *Config) (commands <-chan castCommand, release func(), ok bool) {
	castControls.mu.Lock()
	defer castControls.mu.Unlock()

	if !castControls.begun {
		castControls.begun = true
		castControls.usable = castControlsBegin(config)
	}
	if !castControls.usable {
		return nil, func() {}, false
	}

	subscription := make(chan castCommand, 8)
	castControls.live = subscription

	var once sync.Once
	return subscription, func() {
		once.Do(func() {
			castControls.mu.Lock()
			defer castControls.mu.Unlock()
			// Compared rather than simply cleared: a stale release running
			// after the next episode subscribed must not silence it.
			if castControls.live == subscription {
				castControls.live = nil
			}
		})
	}, true
}

// castDeliverCommand hands one decoded command to the episode playing now.
//
// A command decoded between episodes has nowhere to go and is dropped: the
// countdown holds a subscription of its own while it runs.
func castDeliverCommand(command castCommand) {
	castControls.mu.Lock()
	live := castControls.live
	castControls.mu.Unlock()

	if live == nil {
		return
	}
	select {
	case live <- command:
	default: // a viewer leaning on a key is not a queue
	}
}

// castControlsBeginTerminal is the real terminal setup behind castControlsBegin.
func castControlsBeginTerminal(config *Config) bool {
	if config != nil && config.RofiSelection {
		return false
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return false
	}

	state, err := term.MakeRaw(fd)
	if err != nil {
		Log(fmt.Sprintf("cast: could not read keys: %v", err))
		return false
	}

	// Raw mode outlives every defer in this call when the process exits on a
	// signal, and a terminal left raw is a terminal the viewer has to reset by
	// hand. The exit path restores it for the same reason it tears down ffmpeg.
	// Registered exactly once, because the state captured above is the cooked
	// one only for the first call.
	castSetRawMode(true)
	restore := func() {
		castSetRawMode(false)
		_ = term.Restore(fd, state)
	}
	RegisterExitCleanup(restore)

	// Known limitation: this reader owns fd 0 for the process's whole life, so it
	// competes with any interactive prompt that runs between episodes -- the
	// provider-failure recovery menu on a failed resolve, and the score prompt at
	// the end of a season. Both share stdin with it and lose roughly half their
	// keystrokes. A suspend flag does not fix it: the goroutine is parked inside
	// os.Stdin.Read and consumes the next byte whatever its state, and a byte
	// read here cannot be handed back for a Bubble Tea prompt to read again.
	// Fixing it properly means deciding whether a spawned cast window should host
	// interactive prompts at all, given the countdown exists precisely because
	// that viewer is across the room. Left as it is deliberately.
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
						castDeliverCommand(command)
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()

	return true
}

// castTerminalSize is the terminal's width and height, falling back to a
// conservative 80x24 when it will not say.
//
// Read on every frame rather than once, so the panel follows a resize.
func castTerminalSize() (int, int) {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width < 1 || height < 1 {
		return 80, 24
	}
	return width, height
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

// castControlsPossible reports whether this run can show a control panel at
// all, which is the same condition startCastControls uses to decide whether to
// read keys. It is asked before the work starts, so the panel can carry the
// progress of that work rather than letting it scroll past.
func castControlsPossible(config *Config) bool {
	if config != nil && config.RofiSelection {
		return false
	}
	return castStdoutIsTerminal()
}

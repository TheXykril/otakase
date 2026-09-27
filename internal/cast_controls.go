package internal

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

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
	// castCmdSelect is Enter. It exists for the prompts the panel asks, which
	// need a way to say yes; during playback nothing acts on it, because
	// applyCastCommand falls through to a no-op for a command it has no case
	// for.
	castCmdSelect
)

// castSeekStep is how far one arrow press moves the position.
// castSeekStep is how far one press moves the episode.
//
// Thirty seconds rather than ten, because a cast seek costs a stream restart:
// measured at about seven seconds on real hardware, which is a bad trade for ten
// seconds of episode.
const castSeekStep = 30.0

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
	case '\r', '\n':
		// Enter, which in raw mode arrives as a bare CR. A prompt drawn in the
		// panel needs an accept that is not also a playback control.
		return castCmdSelect, 1
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
// before printing, and the season-end prompts reach it to draw their countdowns,
// so it is package-level rather than threaded through every message. One writer
// at a time: a cast session sets it for the whole cast, and CastEpisode sets it
// only when no session already holds the screen.
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

// castSessionScreen is the terminal held for a whole cast rather than one
// episode at a time.
//
// Taking the screen per episode made it blink between episodes, and left the
// season-end prompts with nowhere to draw: CastEpisode releases the panel on its
// way out, so the rating countdown had bare text to fall back on. Holding it for
// the cast fixes both.
//
// The cost is the messages that used to print between episodes. They become
// desktop notifications, because Out sends them there whenever the panel owns
// the screen -- the same rule that already applies while an episode plays. The
// completion summary is the exception, and is deferred rather than notified: it
// is the one thing a viewer comes back to, so it is printed to the terminal once
// the screen is released.
//
// Strictly one owner at a time. CastEpisode falls back to taking the screen
// itself when no session holds it, which is the path a cast launched directly in
// a terminal takes.
var castSessionScreen struct {
	mu      sync.Mutex
	panel   *castPanelWriter
	release func()
	cancel  func()
}

// beginCastSessionScreen takes the terminal for a whole cast, reporting whether
// there was a terminal to take.
func beginCastSessionScreen(config *Config) bool {
	castSessionScreen.mu.Lock()
	defer castSessionScreen.mu.Unlock()
	if castSessionScreen.panel != nil {
		return true
	}
	if !castControlsPossible(config) {
		return false
	}

	release := castTakeScreen()
	castSessionScreen.release = release
	castSessionScreen.cancel = RegisterExitCleanup(release)
	castSessionScreen.panel = &castPanelWriter{home: true, size: castTerminalSize}
	castPanelForControls = castSessionScreen.panel
	return true
}

// endCastSessionScreen gives the terminal back. Safe to call when no session
// holds it, so a caller does not have to remember whether it took one.
func endCastSessionScreen() {
	castSessionScreen.mu.Lock()
	defer castSessionScreen.mu.Unlock()
	if castSessionScreen.panel == nil {
		return
	}
	castPanelForControls = nil
	castSessionScreen.panel = nil
	if castSessionScreen.cancel != nil {
		castSessionScreen.cancel()
	}
	if castSessionScreen.release != nil {
		castSessionScreen.release()
	}
	// Cleared here as well as by the release, because this is the ownership
	// transition and it should not depend on a closure registered elsewhere
	// remembering to do it. Setting it twice is harmless.
	castSetPanelOwnsScreen(false)
	castSessionScreen.cancel = nil
	castSessionScreen.release = nil
}

// castPanelSay replaces the panel's message line, for a moment that has no
// position to tick -- what the season ended, what a countdown concluded.
//
// It is a no-op without a panel, so a caller does not have to know whether one
// is on screen.
func castPanelSay(config *Config, anime *Anime, message string) {
	panel := castPanelForControls
	if panel == nil || anime == nil {
		return
	}
	device := ""
	if config != nil {
		device = config.CastDevice
	}
	// Done rather than playback keys: every caller of this draws after the season
	// has finished, so offering space/arrows/q would name keys that do nothing.
	fmt.Print(panel.status(GetAnimeName(*anime), anime.Ep.Number, device, message, castPanelDoneKeys))
}

// castDeferredSummary is a completion summary held back while a cast window owns
// the screen, printed once the screen is released so it lands in the scrollback
// rather than a notification.
var castDeferredSummary struct {
	mu      sync.Mutex
	summary string
}

func deferCastSummary(summary string) {
	castDeferredSummary.mu.Lock()
	castDeferredSummary.summary = summary
	castDeferredSummary.mu.Unlock()
}

func takeCastDeferredSummary() string {
	castDeferredSummary.mu.Lock()
	defer castDeferredSummary.mu.Unlock()
	summary := castDeferredSummary.summary
	castDeferredSummary.summary = ""
	return summary
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

// castSeekDebounce is how long a seek waits for the next press before it runs.
//
// A variable so tests do not spend it. It is deliberately longer than a
// comfortable double press and far shorter than the restart it is saving.
var castSeekDebounce = 450 * time.Millisecond

// seekStepFor reports which way a press moves the episode, if it moves it at all.
func seekStepFor(command castCommand) (int, bool) {
	switch command {
	case castCmdSeekForward:
		return 1, true
	case castCmdSeekBack:
		return -1, true
	}
	return 0, false
}

// collectSeekTarget gathers a burst of presses into one destination, showing each
// press as it arrives.
//
// Seeking a cast episode restarts ffmpeg, which takes several seconds and blocks
// this loop for all of them. Running a seek per press therefore does two bad
// things: it restarts the stream once per press, each restart discarding the work
// of the one before it, and it stops reading the keyboard while it happens -- so
// a viewer pressing again sees nothing change until the first seek has finished.
// Waiting a moment for the next press fixes both: show is called for every press,
// while the restart happens once, after the viewer stops.
//
// Presses that are not seeks are handed back rather than dropped: a viewer who
// seeks and then stops must still stop.
func collectSeekTarget(first castCommand, commands <-chan castCommand, position float64, show func(target float64)) (target float64, pending []castCommand) {
	steps, isSeek := seekStepFor(first)
	if !isSeek {
		return position, nil
	}

	target = position + castSeekStep*float64(steps)
	if target < 0 {
		target = 0
	}
	if show != nil {
		show(target)
	}

	timer := time.NewTimer(castSeekDebounce)
	defer timer.Stop()

	for {
		select {
		case command, open := <-commands:
			if !open {
				return target, pending
			}
			if step, isSeek := seekStepFor(command); isSeek {
				target += castSeekStep * float64(step)
				if target < 0 {
					target = 0
				}
				if show != nil {
					show(target)
				}
				// Reset per press, so a viewer walking through a recap keeps
				// moving the target rather than triggering a restart every
				// window.
				if !timer.Stop() {
					// Drained only when it had already fired, which the select
					// above would otherwise have taken on the next pass.
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(castSeekDebounce)
				continue
			}
			pending = append(pending, command)
		case <-timer.C:
			return target, pending
		}
	}
}

// applyCastSeek moves the episode to an absolute position.
//
// Absolute rather than a number of steps: the target was worked out while the
// presses were still arriving, and re-deriving it here from a position the poll
// may since have refreshed would undo the burst.
func applyCastSeek(session castSession, paused *bool, target float64) (moved float64, err error) {
	if target < 0 {
		target = 0
	}
	// Cleared for the same reason the seek inside applyCastCommand clears it: the
	// receiver resumes playback on any seek, and a panel left showing PAUSED
	// would also hold the stall bound off for the rest of the episode.
	*paused = false
	return target, session.SeekToTime(target)
}

package internal

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"time"

	"golang.org/x/term"
)

// Between one menu and the next, otakase often waits on the network: an AniList
// search, every provider in the stack, an episode's links, a torrent's
// metadata, the TV answering discovery. Each of those can take several seconds
// and, until now, showed nothing at all -- a blank terminal or no rofi window,
// which reads as "it hung" and gets a second key press or a Ctrl+C.
//
// BeginBusy covers those waits. In a terminal it draws a one-line spinner with
// the step and the seconds spent; launched from rofi it reuses the desktop
// notification the startup progress already sends. Both stay quiet for a short
// moment first, so a quick step shows nothing rather than a flicker.
//
// One indicator is active at a time. A BeginBusy inside another only changes
// the step it names, and its end puts the outer step back, so a caller can wrap
// a slow call without knowing whether its caller already did. A menu on screen
// suspends the indicator: the menu is the answer to "is it still working", and
// a spinner drawn under Bubble Tea or a notification over rofi is noise.

const (
	// busyTerminalQuietPeriod is short: a terminal spinner costs nothing to show
	// and a terminal that sits blank for a second already looks stuck.
	busyTerminalQuietPeriod = 400 * time.Millisecond
	busyFrameInterval       = 100 * time.Millisecond
	// A notification is louder than a spinner, so it waits as long as the
	// startup one does and refreshes as often.
	busyNotifyQuietPeriod  = startupQuietPeriod
	busyNotifyTickInterval = startupTickInterval
)

var busyFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type busyIndicator struct {
	mu        sync.Mutex
	stage     string
	started   time.Time
	notify    bool
	done      chan struct{}
	closed    bool
	suspended int
	// drawn is true while a spinner line sits on the terminal, so whatever
	// prints next knows to clear it first.
	drawn bool
	// lastNotice is when a notification last went out, from this indicator or
	// from Out, so a message Out just sent is not replaced before it is read.
	lastNotice time.Time
	announced  bool
}

var (
	busyMu     sync.Mutex
	activeBusy *busyIndicator

	// busyOutput is where the terminal spinner draws. Stderr keeps it out of
	// anything piping otakase's stdout.
	busyOutput io.Writer = os.Stderr
	// busyTerminal reports whether there is a terminal to draw on.
	busyTerminal = func() bool { return term.IsTerminal(int(os.Stderr.Fd())) }
	// busyNotifier sends the rofi-mode notification.
	busyNotifier = func(message string) { Out(message) }
	// busyNotifyRepeats is whether a notification can be refreshed in place.
	busyNotifyRepeats = runtime.GOOS == "linux"
)

// BeginBusy shows that otakase is working on stage until the returned function
// runs. The function is safe to call more than once.
func BeginBusy(config *Config, stage string) func() {
	if config == nil {
		config = GetGlobalConfig()
	}

	// A slow launch already has its own notification; name the step there
	// rather than starting a second one beside it.
	startupMu.Lock()
	startup := activeStartup
	startupMu.Unlock()
	if startup != nil {
		startup.mu.Lock()
		previous := startup.stage
		startup.stage = stage
		startup.mu.Unlock()
		return sync.OnceFunc(func() {
			startup.mu.Lock()
			if startup.stage == stage {
				startup.stage = previous
			}
			startup.mu.Unlock()
		})
	}

	busyMu.Lock()
	defer busyMu.Unlock()

	if outer := activeBusy; outer != nil {
		outer.mu.Lock()
		previous := outer.stage
		outer.stage = stage
		outer.mu.Unlock()
		return sync.OnceFunc(func() {
			outer.mu.Lock()
			if outer.stage == stage {
				outer.stage = previous
			}
			outer.mu.Unlock()
		})
	}

	notify := config != nil && config.RofiSelection
	if !notify && !busyTerminal() {
		// Nowhere to show it: output is going to a file or a pipe.
		return func() {}
	}

	b := &busyIndicator{
		stage:   stage,
		started: time.Now(),
		notify:  notify,
		done:    make(chan struct{}),
	}
	activeBusy = b
	go b.run()
	return sync.OnceFunc(func() { b.end() })
}

// setBusyStage renames the step the active indicator shows, if there is one.
func setBusyStage(stage string) {
	busyMu.Lock()
	b := activeBusy
	busyMu.Unlock()
	if b == nil {
		return
	}
	b.mu.Lock()
	b.stage = stage
	b.mu.Unlock()
}

// EndAllBusy stops any indicator, for exit paths that never reach the caller's
// end function.
func EndAllBusy() {
	busyMu.Lock()
	b := activeBusy
	busyMu.Unlock()
	if b != nil {
		b.end()
	}
}

// suspendBusy hides the indicator while a menu or prompt owns the screen. The
// returned function brings it back, with its clock restarted: the wait after a
// choice is a new one, and counting the time spent reading the menu would make
// it look slower than it is.
func suspendBusy() func() {
	busyMu.Lock()
	b := activeBusy
	busyMu.Unlock()
	if b == nil {
		return func() {}
	}

	b.mu.Lock()
	b.suspended++
	b.clearLocked()
	b.mu.Unlock()

	return sync.OnceFunc(func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.suspended > 0 {
			b.suspended--
		}
		if b.suspended == 0 {
			b.started = time.Now()
			b.lastNotice = time.Time{}
			b.announced = false
		}
	})
}

// busyNoteOutput is called by Out before it shows a message. In a terminal it
// takes the spinner off its line so the message does not land beside it; the
// next frame redraws below. In rofi mode it holds the next notification back
// so the message is not replaced before anyone reads it.
func busyNoteOutput() {
	busyMu.Lock()
	b := activeBusy
	busyMu.Unlock()
	if b == nil {
		return
	}
	b.mu.Lock()
	b.clearLocked()
	b.lastNotice = time.Now()
	b.mu.Unlock()
}

func (b *busyIndicator) end() {
	busyMu.Lock()
	if activeBusy == b {
		activeBusy = nil
	}
	busyMu.Unlock()

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	close(b.done)
	b.clearLocked()
}

func (b *busyIndicator) run() {
	interval := busyFrameInterval
	if b.notify {
		interval = 250 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-b.done:
			return
		case now := <-ticker.C:
			b.tick(now)
		}
	}
}

func (b *busyIndicator) tick(now time.Time) {
	b.mu.Lock()
	if b.closed || b.suspended > 0 {
		b.mu.Unlock()
		return
	}
	elapsed := now.Sub(b.started)

	if !b.notify {
		if elapsed >= busyTerminalQuietPeriod && !castPanelOwnsScreen() {
			fmt.Fprint(busyOutput, "\r\033[K"+busySpinnerLine(b.stage, elapsed))
			b.drawn = true
		}
		b.mu.Unlock()
		return
	}

	// Only notify-send can update a notification in place (see
	// desktop_notify.go). Elsewhere each one is a new toast, so the step is
	// announced once rather than stacked every few seconds.
	repeat := busyNotifyRepeats || !b.announced
	if elapsed < busyNotifyQuietPeriod || now.Sub(b.lastNotice) < busyNotifyTickInterval || !repeat {
		b.mu.Unlock()
		return
	}
	message := busyNotifyMessage(b.stage, elapsed)
	b.announced = true
	b.mu.Unlock()

	// Out calls back into busyNoteOutput, so the lock is not held here; that
	// call also stamps lastNotice, which is what spaces the next one.
	busyNotifier(message)
}

// clearLocked removes the spinner line. b.mu must be held.
func (b *busyIndicator) clearLocked() {
	if !b.drawn {
		return
	}
	fmt.Fprint(busyOutput, "\r\033[K")
	b.drawn = false
}

func busySpinnerLine(stage string, elapsed time.Duration) string {
	frame := busyFrames[int(elapsed/busyFrameInterval)%len(busyFrames)]
	line := frame + " " + stage + "…"
	if seconds := int(elapsed.Seconds()); seconds >= 1 {
		line += fmt.Sprintf(" %ds", seconds)
	}
	return line
}

func busyNotifyMessage(stage string, elapsed time.Duration) string {
	return fmt.Sprintf("%s… (%ds)", stage, int(elapsed.Seconds()))
}

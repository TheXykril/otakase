package internal

import (
	"fmt"
	"sync"
	"time"
)

// Launched from a keybind rather than a terminal, otakase gives no sign it is
// running until its first menu appears. Refreshing a tracker token and pulling
// a large list can take several seconds, during which the only honest reading
// of a silent desktop is "did that even start?" -- so people press the key
// again, and now two copies are racing.
//
// A notification fills that gap, but only when there is a gap worth filling: a
// launch that reaches the menu quickly should stay silent rather than flash a
// message nobody had time to read. The elapsed seconds tick so a slow start
// still looks alive rather than hung.

const (
	// startupQuietPeriod is how long a launch may take before it owes the user
	// an explanation. Below this the menu is effectively immediate.
	startupQuietPeriod = 1200 * time.Millisecond
	// startupTickInterval refreshes the notification so it neither expires
	// mid-wait nor sits there looking frozen.
	startupTickInterval = 3 * time.Second
)

type startupProgress struct {
	mu     sync.Mutex
	stage  string
	done   chan struct{}
	closed bool
	// announced records whether anything was shown, so Done knows if there is a
	// notification to replace.
	announced bool
}

var (
	startupMu     sync.Mutex
	activeStartup *startupProgress
	// startupBusyEnd ends the terminal spinner a launch in a terminal shows.
	startupBusyEnd  func()
	startupReporter = func(message string) { Out(message) }
)

// A slow launch is hard to argue about from the outside: the only evidence is
// that nothing happens for a while. Timing each stage into the log turns "otakase
// takes a minute" into a line naming which step spent it, and costs one
// timestamp per stage.
var (
	startupClockMu    sync.Mutex
	startupClockBegan time.Time
	startupClockLast  time.Time
	startupClockStage string
)

// reportStartupTotal closes the timing record when the first menu appears.
func reportStartupTotal() {
	now := time.Now()
	startupClockMu.Lock()
	previous, last, began := startupClockStage, startupClockLast, startupClockBegan
	startupClockBegan, startupClockStage = time.Time{}, ""
	startupClockMu.Unlock()

	if began.IsZero() {
		return
	}
	if previous != "" {
		Log(fmt.Sprintf("Startup timing: %q took %s", previous, now.Sub(last).Round(time.Millisecond)))
	}
	Log(fmt.Sprintf("Startup timing: ready after %s", now.Sub(began).Round(time.Millisecond)))
}

// EndStartupProgress stops reporting. It is safe to call more than once, and is
// called from the menus themselves: a visible menu is its own proof that otakase
// started, and any message still on screen at that point is stale.
func EndStartupProgress() {
	reportStartupTotal()

	startupMu.Lock()
	progress := activeStartup
	activeStartup = nil
	endBusy := startupBusyEnd
	startupBusyEnd = nil
	startupMu.Unlock()
	if endBusy != nil {
		endBusy()
	}
	if progress == nil {
		return
	}

	progress.mu.Lock()
	defer progress.mu.Unlock()
	if progress.closed {
		return
	}
	progress.closed = true
	close(progress.done)

	// Only worth a closing word if something was said in the first place;
	// otherwise the launch was quick and silence is the right outcome.
	if progress.announced {
		startupReporter("Ready.")
	}
}

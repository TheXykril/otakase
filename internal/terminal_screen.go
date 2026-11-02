package internal

import (
	"os"
	"sync"
	"time"
)

var (
	// exitOnce matters more than it looks: restoreAndExit is reached from
	// several places, and a second caller must wait for the first teardown
	// rather than start it again or exit before the cleanups finish.
	exitOnce sync.Once

	exitCleanupMu sync.Mutex
	exitCleanups  []func()
)

// RegisterExitCleanup adds work that must happen before the process exits on a
// signal. The interrupt handler calls os.Exit, so a deferred cleanup in a
// caller never runs; anything holding a resource outside this process --
// a child process, a device playing a stream, a scratch directory -- registers
// here instead.
func RegisterExitCleanup(fn func()) (cancel func()) {
	exitCleanupMu.Lock()
	defer exitCleanupMu.Unlock()
	exitCleanups = append(exitCleanups, fn)
	i := len(exitCleanups) - 1
	return func() {
		exitCleanupMu.Lock()
		defer exitCleanupMu.Unlock()
		// Bounds-checked rather than assumed: the registry is reset between
		// tests, which shortens the slice under a cancel that is still live.
		if i < len(exitCleanups) {
			exitCleanups[i] = nil
		}
	}
}

func runExitCleanups() {
	exitCleanupMu.Lock()
	fns := append([]func(){}, exitCleanups...)
	exitCleanupMu.Unlock()

	// Bounded: a cleanup that talks to a device over the network must not be
	// able to wedge a Ctrl+C. Whatever has not finished is abandoned.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, fn := range fns {
			if fn != nil {
				fn()
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(exitCleanupBudget):
	}
}

// exitCleanupBudget is how long the cleanups get, all of them together. It
// has to leave room for castStopTimeout and the local steps after it.
const exitCleanupBudget = 4 * time.Second

// restoreAndExit ends the process with status, after putting the terminal
// back and running the exit cleanups.
func restoreAndExit(status int) {
	exitOnce.Do(func() {
		// The terminal comes back first: cleanups can take up to four seconds
		// (exitCleanupBudget), and leaving the alternate screen up for
		// that whole window makes Ctrl+C look like it did nothing.
		LeaveAppScreen()
		runExitCleanups()
		// After the cleanups, so "closed" is not shown while a cast is still
		// being torn down. Signals land here too: closing the cast window
		// sends SIGHUP and never reaches Exit().
		announceClose()
		// Any exit through here is a clean one, a signal included: only a
		// crash or a kill leaves the run marker for the next launch.
		EndCrashWatch()
		// The log handle is held open for the process lifetime; release it so the
		// final lines are flushed to disk before the process goes away. Closed
		// last so anything a cleanup logs is still captured.
		_ = CloseLogFile()
		os.Exit(status)
	})
}

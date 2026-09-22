package internal

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var (
	interruptHandlerOnce sync.Once
	interruptExitOnce    sync.Once

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

// resetExitCleanupsForTest drops every registered cleanup. Tests share this
// package-global registry, so a test that registers must not leak into the
// next one.
func resetExitCleanupsForTest() {
	exitCleanupMu.Lock()
	defer exitCleanupMu.Unlock()
	exitCleanups = nil
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
	case <-time.After(2 * time.Second):
	}
}

// InstallTerminalInterruptHandler restores the terminal before exiting on Ctrl+C or SIGTERM.
func InstallTerminalInterruptHandler() {
	interruptHandlerOnce.Do(func() {
		interrupts := make(chan os.Signal, 1)
		signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)
		go func() {
			for range interrupts {
				exitWithRestore(130)
			}
		}()
	})
}

func exitWithRestore(code int) {
	interruptExitOnce.Do(func() {
		// The terminal comes back first: cleanups can take up to two seconds
		// (runExitCleanups' budget), and leaving the alternate screen up for
		// that whole window makes Ctrl+C look like it did nothing.
		RestoreScreen()
		runExitCleanups()
		// The log handle is held open for the process lifetime; release it so the
		// final lines are flushed to disk before the process goes away. Closed
		// last so anything a cleanup logs is still captured.
		_ = CloseLogFile()
		os.Exit(code)
	})
}

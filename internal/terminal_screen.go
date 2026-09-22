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
		exitCleanups[i] = nil
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
		runExitCleanups()
		RestoreScreen()
		// The log handle is held open for the process lifetime; release it so the
		// final lines are flushed to disk before the process goes away.
		_ = CloseLogFile()
		os.Exit(code)
	})
}

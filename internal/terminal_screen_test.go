package internal

import (
	"testing"
	"time"
)

func TestRestoreScreenOnlyWhenAlternateScreenActive(t *testing.T) {
	previous := alternateScreenActive
	t.Cleanup(func() {
		alternateScreenActive = previous
	})

	alternateScreenActive = false
	RestoreScreen()
	if alternateScreenActive {
		t.Fatal("restore should stay inactive when screen was not cleared")
	}

	alternateScreenActive = true
	RestoreScreen()
	if alternateScreenActive {
		t.Fatal("restore should deactivate alternate screen")
	}
}

func TestInstallTerminalInterruptHandlerIsIdempotent(t *testing.T) {
	InstallTerminalInterruptHandler()
	InstallTerminalInterruptHandler()
}

// A cancelled registration must not run: CastEpisode cancels its cleanup once
// its own defer has already torn things down, and a leftover call firing
// anyway would double-close resources a later cast has already replaced.
func TestRegisterExitCleanupCancelPreventsRun(t *testing.T) {
	// Reset on both sides: t.Cleanup only protects whatever runs after this
	// test, and runExitCleanups below would otherwise also run anything an
	// earlier test in the package registered without cancelling.
	resetExitCleanupsForTest()
	t.Cleanup(resetExitCleanupsForTest)

	ran := false
	cancel := RegisterExitCleanup(func() { ran = true })
	cancel()

	runExitCleanups()

	if ran {
		t.Error("a cancelled cleanup ran")
	}
}

// A registered cleanup that is not cancelled must run when the process is
// about to exit on a signal -- this is the only path that reaches it, since
// exitWithRestore calls os.Exit and skips every deferred cleanup.
func TestRunExitCleanupsRunsRegistered(t *testing.T) {
	resetExitCleanupsForTest()
	t.Cleanup(resetExitCleanupsForTest)

	done := make(chan struct{})
	cancel := RegisterExitCleanup(func() { close(done) })
	defer cancel()

	runExitCleanups()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("a registered cleanup did not run")
	}
}

// Closing a terminal window closes its pty, and the kernel sends SIGHUP to the
// foreground process group. A cast started from rofi lives in a window whose
// close is the intended stop gesture, so an unhandled SIGHUP means the process
// dies with no teardown at all: the device is never told to stop and the
// scratch directory is left on disk.
func TestInterruptSignalsIncludeHangup(t *testing.T) {
	signals := interruptSignals()

	want := map[string]bool{"interrupt": false, "terminated": false, "hangup": false}
	for _, s := range signals {
		want[s.String()] = true
	}
	for name, found := range want {
		if !found {
			t.Errorf("%s is not handled; signals = %v", name, signals)
		}
	}
}

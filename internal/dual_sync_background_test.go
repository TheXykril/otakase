package internal

import (
	"testing"
)

// withInstantRemoteWritePacing removes the real 350ms so a test measures the
// structure rather than the rate limit.
func withInstantRemoteWritePacing(t *testing.T) {
	t.Helper()
	previous := sleepBetweenRemoteWrites
	sleepBetweenRemoteWrites = func() {}
	t.Cleanup(func() { sleepBetweenRemoteWrites = previous })
}

// Nothing to write must not spawn anything.
func TestDualSyncSkipsAnEmptyPlan(t *testing.T) {
	withInstantRemoteWritePacing(t)

	startDualSyncWrites(&Config{}, dualSyncWrites{})
	dualSyncMu.Lock()
	running := dualSyncRunning
	dualSyncMu.Unlock()
	if running {
		t.Fatal("an empty plan should not start a writer")
	}
}

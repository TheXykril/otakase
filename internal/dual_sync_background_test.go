package internal

import (
	"sync/atomic"
	"testing"
	"time"
)

// withInstantRemoteWritePacing removes the real 350ms so a test measures the
// structure rather than the rate limit.
func withInstantRemoteWritePacing(t *testing.T) {
	t.Helper()
	previous := sleepBetweenRemoteWrites
	sleepBetweenRemoteWrites = func() {}
	t.Cleanup(func() { sleepBetweenRemoteWrites = previous })
}

// Reconciling two trackers used to happen in front of the first menu, one paced
// write per entry they disagreed on. That disagreement is not small and does
// not shrink on its own -- 78 entries present on AniList but not MyAnimeList
// were re-sent every launch -- so roughly half a minute passed before anything
// appeared, spent pushing a planning list the user was not about to watch.
func TestDualSyncDoesNotBlockTheLaunch(t *testing.T) {
	withInstantRemoteWritePacing(t)

	released := make(chan struct{})
	var started atomic.Bool
	previous := sleepBetweenRemoteWrites
	sleepBetweenRemoteWrites = func() {
		started.Store(true)
		<-released // hold the writer open, as a slow tracker would
	}
	t.Cleanup(func() {
		sleepBetweenRemoteWrites = previous
		close(released)
		WaitForDualSyncWrites()
	})

	writes := dualSyncWrites{myAnimeList: []Entry{
		{Media: Media{ID: 1}}, {Media: Media{ID: 2}}, {Media: Media{ID: 3}},
	}}

	begin := time.Now()
	startDualSyncWrites(&Config{}, writes)
	elapsed := time.Since(begin)

	// The point of the change: queueing returns immediately even while the
	// writer is stuck on a tracker that will not answer.
	if elapsed > time.Second {
		t.Fatalf("queueing the writes blocked for %s; the launch waits on it again", elapsed)
	}

	deadline := time.Now().Add(2 * time.Second)
	for !started.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !started.Load() {
		t.Fatal("the background writer never ran")
	}
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

// A second launch-time reconciliation while one is still writing must not
// double the work: the next launch reconciles from scratch regardless, so
// duplicating in-flight writes only doubles the pressure on a rate limit.
func TestDualSyncDoesNotStackConcurrentRounds(t *testing.T) {
	withInstantRemoteWritePacing(t)

	released := make(chan struct{})
	var writes atomic.Int32
	previous := sleepBetweenRemoteWrites
	sleepBetweenRemoteWrites = func() {
		writes.Add(1)
		<-released
	}
	t.Cleanup(func() {
		sleepBetweenRemoteWrites = previous
		close(released)
		WaitForDualSyncWrites()
	})

	plan := dualSyncWrites{myAnimeList: []Entry{{Media: Media{ID: 1}}, {Media: Media{ID: 2}}}}
	startDualSyncWrites(&Config{}, plan)

	deadline := time.Now().Add(2 * time.Second)
	for writes.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	startDualSyncWrites(&Config{}, plan) // second round while the first is stuck

	dualSyncMu.Lock()
	running := dualSyncRunning
	dualSyncMu.Unlock()
	if !running {
		t.Fatal("expected the first round to still be running")
	}
}

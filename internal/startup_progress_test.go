package internal

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// captureStartupReports redirects progress messages for inspection and shortens
// nothing: the timings under test are the real ones.
func captureStartupReports(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var messages []string

	previous := startupReporter
	startupReporter = func(message string) {
		mu.Lock()
		messages = append(messages, message)
		mu.Unlock()
	}
	t.Cleanup(func() {
		startupReporter = previous
		EndStartupProgress()
	})

	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), messages...)
	}
}

// A launch that reaches its menu quickly should say nothing at all -- a
// notification nobody has time to read is worse than silence.
func TestFastStartupSaysNothing(t *testing.T) {
	read := captureStartupReports(t)

	BeginStartupProgress(&CurdConfig{RofiSelection: true}, "Curd is starting")
	time.Sleep(startupQuietPeriod / 4)
	EndStartupProgress()
	time.Sleep(startupQuietPeriod)

	if got := read(); len(got) != 0 {
		t.Fatalf("expected silence for a fast launch, got %v", got)
	}
}

// A launch slow enough to look broken should explain itself, and say how long
// it has been going so it reads as working rather than hung.
func TestSlowStartupReportsProgressAndElapsedTime(t *testing.T) {
	read := captureStartupReports(t)

	BeginStartupProgress(&CurdConfig{RofiSelection: true}, "Signing in to your tracker")
	time.Sleep(startupQuietPeriod + 300*time.Millisecond)

	messages := read()
	if len(messages) == 0 {
		t.Fatal("expected a progress message once the quiet period elapsed")
	}
	if !strings.Contains(messages[0], "Signing in to your tracker") {
		t.Errorf("expected the current stage, got %q", messages[0])
	}
	if !strings.Contains(messages[0], "s)") {
		t.Errorf("expected elapsed seconds, got %q", messages[0])
	}

	// Ending after something was shown replaces it, so no stale message is left
	// describing work that has finished.
	EndStartupProgress()
	final := read()
	if final[len(final)-1] != "Ready." {
		t.Errorf("expected a closing message, got %q", final[len(final)-1])
	}
}

// The stage should follow what the launch is actually doing.
func TestStartupStageUpdatesTheMessage(t *testing.T) {
	read := captureStartupReports(t)

	BeginStartupProgress(&CurdConfig{RofiSelection: true}, "Curd is starting")
	StartupStage("Loading your anime list")
	time.Sleep(startupQuietPeriod + 300*time.Millisecond)

	messages := read()
	if len(messages) == 0 || !strings.Contains(messages[0], "Loading your anime list") {
		t.Fatalf("expected the updated stage, got %v", messages)
	}
}

// In a terminal the output is already visible, so notifications would only
// duplicate it.
func TestNoProgressWhenRunningInATerminal(t *testing.T) {
	read := captureStartupReports(t)

	BeginStartupProgress(&CurdConfig{RofiSelection: false}, "Curd is starting")
	time.Sleep(startupQuietPeriod + 300*time.Millisecond)

	if got := read(); len(got) != 0 {
		t.Fatalf("expected no notifications in terminal mode, got %v", got)
	}
}

// Ending twice must not panic on a closed channel; the menus call it on every
// open, and ExitCurd calls it again on the way out.
func TestEndStartupProgressIsIdempotent(t *testing.T) {
	captureStartupReports(t)
	BeginStartupProgress(&CurdConfig{RofiSelection: true}, "Curd is starting")
	EndStartupProgress()
	EndStartupProgress()
	EndStartupProgress()
}

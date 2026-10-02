package internal

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureBusyTerminal points the spinner at a buffer standing in for a terminal.
func captureBusyTerminal(t *testing.T) *lockedBuffer {
	t.Helper()
	out := &lockedBuffer{}
	previousOutput, previousTerminal := busyOutput, busyTerminal
	busyOutput = out
	busyTerminal = func() bool { return true }
	t.Cleanup(func() {
		EndAllBusy()
		busyOutput, busyTerminal = previousOutput, previousTerminal
	})
	return out
}

func captureBusyNotifications(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var messages []string
	previous := busyNotifier
	busyNotifier = func(message string) {
		busyNoteOutput()
		mu.Lock()
		messages = append(messages, message)
		mu.Unlock()
	}
	t.Cleanup(func() {
		EndAllBusy()
		busyNotifier = previous
	})
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), messages...)
	}
}

// A step that finishes quickly shows nothing: a spinner that flashes for a
// frame is noise.
func TestBusyQuickStepDrawsNothing(t *testing.T) {
	out := captureBusyTerminal(t)

	end := BeginBusy(&Config{}, "Searching AniList")
	time.Sleep(busyTerminalQuietPeriod / 4)
	end()
	time.Sleep(busyTerminalQuietPeriod)

	if got := out.String(); got != "" {
		t.Fatalf("expected nothing drawn for a quick step, got %q", got)
	}
}

// A slow step names itself, and the line is cleared once it is done so the
// next output does not land beside a stale spinner.
func TestBusySlowStepDrawsAndClears(t *testing.T) {
	out := captureBusyTerminal(t)

	end := BeginBusy(&Config{}, "Finding episode 5")
	time.Sleep(busyTerminalQuietPeriod + 3*busyFrameInterval)
	drawn := out.String()
	end()

	if !strings.Contains(drawn, "Finding episode 5…") {
		t.Fatalf("expected the step on the spinner line, got %q", drawn)
	}
	if !strings.HasSuffix(out.String(), "\r\033[K") {
		t.Errorf("expected the spinner line cleared at the end, got %q", out.String())
	}
}

// A wrapped call inside another names its own step, and hands the outer one
// back when it is done.
func TestBusyNestedStepRestoresOuter(t *testing.T) {
	captureBusyTerminal(t)

	endOuter := BeginBusy(&Config{}, "Outer")
	defer endOuter()
	endInner := BeginBusy(&Config{}, "Inner")

	stage := func() string {
		busyMu.Lock()
		b := activeBusy
		busyMu.Unlock()
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.stage
	}
	if got := stage(); got != "Inner" {
		t.Fatalf("expected the inner step, got %q", got)
	}
	endInner()
	if got := stage(); got != "Outer" {
		t.Fatalf("expected the outer step back, got %q", got)
	}
}

// Nothing is drawn while a menu owns the screen.
func TestBusySuspendedWhileMenuShows(t *testing.T) {
	out := captureBusyTerminal(t)

	end := BeginBusy(&Config{}, "Looking for cast devices")
	defer end()
	resume := suspendBusy()
	time.Sleep(busyTerminalQuietPeriod + 3*busyFrameInterval)
	if got := out.String(); got != "" {
		t.Fatalf("expected nothing drawn under a menu, got %q", got)
	}
	resume()
	time.Sleep(busyTerminalQuietPeriod + 3*busyFrameInterval)
	if !strings.Contains(out.String(), "Looking for cast devices") {
		t.Fatalf("expected the spinner back after the menu, got %q", out.String())
	}
}

// Launched from rofi there is no terminal, so a slow step becomes a
// notification with the seconds spent.
func TestBusyRofiNotifiesSlowStep(t *testing.T) {
	read := captureBusyNotifications(t)
	previousTerminal := busyTerminal
	busyTerminal = func() bool { return false }
	t.Cleanup(func() { busyTerminal = previousTerminal })

	end := BeginBusy(&Config{RofiSelection: true}, "Searching all configured providers")
	time.Sleep(busyNotifyQuietPeriod + 400*time.Millisecond)
	end()

	messages := read()
	if len(messages) != 1 {
		t.Fatalf("expected one notification, got %v", messages)
	}
	if !strings.HasPrefix(messages[0], "Searching all configured providers… (") {
		t.Errorf("unexpected notification %q", messages[0])
	}
}

// With no terminal and no rofi, there is nowhere to show anything.
func TestBusyWithoutTerminalIsSilent(t *testing.T) {
	previousTerminal := busyTerminal
	busyTerminal = func() bool { return false }
	t.Cleanup(func() { busyTerminal = previousTerminal })

	end := BeginBusy(&Config{}, "Searching")
	defer end()
	busyMu.Lock()
	active := activeBusy
	busyMu.Unlock()
	if active != nil {
		t.Fatal("expected no indicator without a terminal")
	}
}

func TestBusySpinnerLineShowsSeconds(t *testing.T) {
	if got := busySpinnerLine("Searching", 500*time.Millisecond); strings.Contains(got, "s ") || strings.HasSuffix(got, "0s") {
		t.Errorf("expected no seconds under one second, got %q", got)
	}
	if got := busySpinnerLine("Searching", 3200*time.Millisecond); !strings.HasSuffix(got, "Searching… 3s") {
		t.Errorf("expected seconds, got %q", got)
	}
}

// Where a notification cannot be updated in place, a slow step is announced
// once rather than as a new toast every few seconds.
func TestBusyNotifiesOnceWhereNotificationsStack(t *testing.T) {
	read := captureBusyNotifications(t)
	previousTerminal, previousRepeats := busyTerminal, busyNotifyRepeats
	busyTerminal = func() bool { return false }
	busyNotifyRepeats = false
	t.Cleanup(func() { busyTerminal, busyNotifyRepeats = previousTerminal, previousRepeats })

	end := BeginBusy(&Config{RofiSelection: true}, "Finding episode 2")
	time.Sleep(busyNotifyQuietPeriod + busyNotifyTickInterval + 500*time.Millisecond)
	end()

	if got := read(); len(got) != 1 {
		t.Fatalf("expected a single notification, got %v", got)
	}
}

package internal

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// A failed MPV launch left an empty socket path in play. Every poll then retried
// three times against "dial unix: missing address", turning the main loop into a
// hot loop that wrote millions of log lines.
func TestMPVSendCommandFailsFastWithoutSocket(t *testing.T) {
	for _, socket := range []string{"", "   "} {
		start := time.Now()
		_, err := MPVSendCommand(socket, []interface{}{"get_property", "time-pos"})
		elapsed := time.Since(start)

		if err == nil {
			t.Fatalf("expected an error for socket %q", socket)
		}
		if !errors.Is(err, ErrMPVNoSocket) {
			t.Fatalf("expected ErrMPVNoSocket for socket %q, got %v", socket, err)
		}
		// The retry loop sleeps 100ms per attempt; failing fast must skip it.
		if elapsed > 50*time.Millisecond {
			t.Fatalf("expected an immediate failure, took %s", elapsed)
		}
		if strings.Contains(err.Error(), "after 3 attempts") {
			t.Fatalf("expected no retries for a missing socket, got %v", err)
		}
	}
}

// Callers poll until isMPVConnectionGoneError reports the session is over, so a
// missing socket has to count as gone or they never stop.
func TestIsMPVConnectionGoneErrorTreatsMissingSocketAsGone(t *testing.T) {
	if !isMPVConnectionGoneError(ErrMPVNoSocket) {
		t.Fatal("expected ErrMPVNoSocket to count as a gone connection")
	}
	if !isMPVConnectionGoneError(errors.New("dial unix: missing address")) {
		t.Fatal("expected a missing-address dial error to count as gone")
	}
	if isMPVConnectionGoneError(nil) {
		t.Fatal("nil is not a gone connection")
	}
	if isMPVConnectionGoneError(errors.New("property unavailable")) {
		t.Fatal("an unavailable property is not a gone connection")
	}
}

// WaitForMPVPlaybackStart must give up immediately rather than spin to its
// deadline when MPV never produced a socket.
func TestWaitForMPVPlaybackStartStopsWhenSocketMissing(t *testing.T) {
	start := time.Now()
	if WaitForMPVPlaybackStart("/tmp/curd-nonexistent-socket-for-test", 2*time.Second) {
		t.Fatal("expected playback start to fail for a socket that does not exist")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("expected an early exit, took %s", elapsed)
	}
}

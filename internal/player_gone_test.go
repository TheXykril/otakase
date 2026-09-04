//go:build !windows

package internal

import (
	"net"
	"path/filepath"
	"testing"
	"time"
)

// The polling loops decide whether to keep going by asking MPVConnectionGone.
// Classifying a real dead-socket error as merely transient is what let them spin
// forever, logging the same failure every iteration (Wraient/curd#58), so this
// exercises the errors the real code path actually produces rather than
// hand-written strings.
func TestDeadSocketErrorsAreClassifiedAsGone(t *testing.T) {
	dir := t.TempDir()

	t.Run("socket never existed", func(t *testing.T) {
		socket := filepath.Join(dir, "never-created.sock")

		_, err := MPVSendCommand(socket, []interface{}{"get_property", "time-pos"})
		if err == nil {
			t.Fatal("expected an error for a socket that does not exist")
		}
		if !MPVConnectionGone(err) {
			t.Fatalf("expected a gone connection, got %v", err)
		}
	})

	t.Run("socket closed after MPV exits", func(t *testing.T) {
		socket := filepath.Join(dir, "closed.sock")
		listener, err := net.Listen("unix", socket)
		if err != nil {
			t.Skipf("cannot create a unix socket here: %v", err)
		}
		// Stand in for MPV exiting mid-playback.
		listener.Close()

		_, err = MPVSendCommand(socket, []interface{}{"get_property", "time-pos"})
		if err == nil {
			t.Fatal("expected an error after the socket closed")
		}
		if !MPVConnectionGone(err) {
			t.Fatalf("expected a gone connection, got %v", err)
		}
	})

	// HasActivePlayback wraps the underlying error; the wrapping must not hide
	// that the session is over, or the caller keeps polling.
	t.Run("HasActivePlayback surfaces it as gone", func(t *testing.T) {
		socket := filepath.Join(dir, "absent.sock")

		playing, err := HasActivePlayback(socket)
		if playing {
			t.Fatal("nothing can be playing on a socket that does not exist")
		}
		if err == nil {
			t.Fatal("expected an error")
		}
		if !MPVConnectionGone(err) {
			t.Fatalf("expected the wrapped error to still read as gone, got %v", err)
		}
	})
}

// A dead socket must not cost the retry ladder on every poll: the loops call this
// once per second.
func TestMissingSocketFailsWithoutTheFullRetryLadder(t *testing.T) {
	start := time.Now()
	_, err := MPVSendCommand("", []interface{}{"get_property", "time-pos"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error for an empty socket path")
	}
	if !MPVConnectionGone(err) {
		t.Fatalf("expected a gone connection, got %v", err)
	}
	if elapsed > 50*time.Millisecond {
		t.Fatalf("expected an immediate failure, took %s", elapsed)
	}
}

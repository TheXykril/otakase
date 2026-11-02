//go:build !windows

package internal

import (
	"testing"
	"time"
)

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

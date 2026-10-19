package cast

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A launch that is merely slow must not be reported as a broken device. The
// vendored library gives up after five seconds, which a TV waking from standby
// routinely exceeds.
func TestPlayRetriesASlowLaunch(t *testing.T) {
	previous := launchRetryInterval
	launchRetryInterval = time.Millisecond
	t.Cleanup(func() { launchRetryInterval = previous })

	calls := 0
	loader := func(string) error {
		calls++
		if calls < 3 {
			return context.DeadlineExceeded
		}
		return nil
	}

	if err := playWithin(loader, "http://example.test/stream.m3u8", time.Second); err != nil {
		t.Fatalf("a launch that succeeded on the third attempt reported %v", err)
	}
	if calls != 3 {
		t.Errorf("attempts = %d, want 3", calls)
	}
}

// A refusal is not a timeout. Retrying it would turn a clear failure into a
// ninety second one.
func TestPlayDoesNotRetryARefusal(t *testing.T) {
	refused := errors.New("receiver refused the stream")
	calls := 0
	loader := func(string) error {
		calls++
		return refused
	}

	err := playWithin(loader, "http://example.test/stream.m3u8", time.Minute)
	if !errors.Is(err, refused) {
		t.Fatalf("error = %v, want the refusal", err)
	}
	if calls != 1 {
		t.Errorf("attempts = %d, want 1: a refusal was retried", calls)
	}
}

// The deadline bounds it: a device that never finishes launching must still fail.
func TestPlayGivesUpAtTheDeadline(t *testing.T) {
	previous := launchRetryInterval
	launchRetryInterval = time.Millisecond
	t.Cleanup(func() { launchRetryInterval = previous })

	loader := func(string) error { return context.DeadlineExceeded }

	start := time.Now()
	err := playWithin(loader, "http://example.test/stream.m3u8", 20*time.Millisecond)
	if err == nil {
		t.Fatal("a device that never launched reported success")
	}
	if time.Since(start) > time.Second {
		t.Errorf("gave up after %v, far past its deadline", time.Since(start))
	}
}

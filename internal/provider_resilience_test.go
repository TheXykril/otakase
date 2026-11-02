package internal

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/thexykril/otakase/internal/providerhost"
)

type fakeNetTimeout struct{}

func (fakeNetTimeout) Error() string   { return "i/o timeout" }
func (fakeNetTimeout) Timeout() bool   { return true }
func (fakeNetTimeout) Temporary() bool { return true }

func TestIsRetryableProviderError(t *testing.T) {
	var netErr net.Error = fakeNetTimeout{}

	retryable := []error{
		netErr,
		io.EOF,
		io.ErrUnexpectedEOF,
		errors.New(`Post "https://anizone.live/anime/filter": EOF`),
		errors.New("context deadline exceeded (Client.Timeout exceeded while awaiting headers)"),
		fmt.Errorf("wrapped: %w", io.ErrUnexpectedEOF),
		errors.New("read: connection reset by peer"),
	}
	for _, err := range retryable {
		if !isRetryableProviderError(err) {
			t.Fatalf("expected %v to be retryable", err)
		}
	}

	// "No such show here" is a definitive answer and must not be retried, or a
	// stacked search would take twice as long for every miss.
	notRetryable := []error{
		nil,
		errors.New(`no results for "some show"`),
		errors.New("provider request failed with status 404"),
		errors.New("anizone: could not decode search response"),
	}
	for _, err := range notRetryable {
		if isRetryableProviderError(err) {
			t.Fatalf("expected %v to not be retryable", err)
		}
	}
}

// A provider throttling us is temporary. Treating it as definitive means no
// retry, no cooldown, and a user told their anime does not exist when otakase was
// simply being rate limited.
func TestRateLimitsAreRetryable(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("anizone: %w", providerhost.ErrRateLimited),
		errors.New(`{"error":"Too many requests, try again in a minute"}`),
		errors.New("anizone request failed with status 429: rate limit exceeded"),
	} {
		if !isRetryableProviderError(err) {
			t.Fatalf("expected %v to be retryable", err)
		}
	}
}

// ...and it must be reported as the host being unavailable, not as a clean miss.
func TestRateLimitReadsAsUnreachableNotAMiss(t *testing.T) {
	failure := providerFailure{provider: "anizone", err: fmt.Errorf("anizone: %w", providerhost.ErrRateLimited)}
	if got := failure.kind(); got != failureUnreachable {
		t.Fatalf("kind = %v, want failureUnreachable", got)
	}

	message := newProviderSearchError("Some Show", []providerFailure{failure}).Error()
	if strings.Contains(message, "none carries it") {
		t.Fatalf("a rate limit must not be reported as the show not existing: %s", message)
	}
}

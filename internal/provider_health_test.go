package internal

import (
	"testing"
	"time"
)

// A cooling provider must not turn into a confusing error for the user.
func TestCoolingProviderReportsAsUnreachable(t *testing.T) {
	failure := providerFailure{
		provider: "anizone",
		err:      &errProviderCooling{provider: "anizone", until: time.Now().Add(time.Minute)},
	}
	if failure.kind() != failureUnreachable {
		t.Fatalf("expected a cooling provider to report as unreachable, got %v", failure.kind())
	}
}

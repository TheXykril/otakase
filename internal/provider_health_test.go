package internal

import (
	"errors"
	"testing"
	"time"

	"github.com/wraient/curd/internal/providers"
)

func withProviderHealthClock(t *testing.T) *time.Time {
	t.Helper()

	now := time.Now()
	previousNow := providerHealthNow
	providerHealthNow = func() time.Time { return now }
	ResetProviderHealth()
	t.Cleanup(func() {
		providerHealthNow = previousNow
		ResetProviderHealth()
	})
	return &now
}

func TestProviderCooldownTripsAfterRepeatedUnreachableFailures(t *testing.T) {
	withProviderHealthClock(t)

	unreachable := errors.New(`Post "https://senshi.live/anime/filter": EOF`)

	noteProviderFailure("senshi", unreachable)
	if _, cooling := providerCoolingUntil("senshi"); cooling {
		t.Fatal("one failure should not trip the cooldown")
	}

	noteProviderFailure("senshi", unreachable)
	if _, cooling := providerCoolingUntil("senshi"); !cooling {
		t.Fatalf("expected a cooldown after %d failures", providerFailuresBeforeCooldown)
	}
}

// A provider that answers "no results" is healthy. Cooling it down would hide a
// working host from every later search.
func TestProviderCooldownIgnoresCleanMisses(t *testing.T) {
	withProviderHealthClock(t)

	for i := 0; i < 5; i++ {
		noteProviderFailure("anipub", errors.New(`no results for "demo"`))
	}
	if _, cooling := providerCoolingUntil("anipub"); cooling {
		t.Fatal("a clean miss must never trip the cooldown")
	}
}

func TestProviderCooldownExpires(t *testing.T) {
	now := withProviderHealthClock(t)

	unreachable := errors.New("context deadline exceeded")
	noteProviderFailure("senshi", unreachable)
	noteProviderFailure("senshi", unreachable)
	if _, cooling := providerCoolingUntil("senshi"); !cooling {
		t.Fatal("expected a cooldown")
	}

	*now = now.Add(providerCooldown + time.Second)
	if _, cooling := providerCoolingUntil("senshi"); cooling {
		t.Fatal("expected the cooldown to expire")
	}

	// The slate is clean, so a single failure must not immediately re-trip it.
	noteProviderFailure("senshi", unreachable)
	if _, cooling := providerCoolingUntil("senshi"); cooling {
		t.Fatal("expected a fresh failure count after the cooldown expired")
	}
}

func TestProviderSuccessClearsFailures(t *testing.T) {
	withProviderHealthClock(t)

	unreachable := errors.New("connection reset by peer")
	noteProviderFailure("anineko", unreachable)
	noteProviderSuccess("anineko")
	noteProviderFailure("anineko", unreachable)

	if _, cooling := providerCoolingUntil("anineko"); cooling {
		t.Fatal("a success between failures must reset the streak")
	}
}

// The point of the cooldown: a dead provider stops being contacted at all.
func TestSearchSkipsCoolingProviderWithoutContactingIt(t *testing.T) {
	withAllProvidersEnabledForTest(t)
	withProviderHealthClock(t)

	eof := errors.New(`Post "https://senshi.live/anime/filter": EOF`)
	// A whole search counts as one failure regardless of its internal retries, so
	// the cooldown trips after providerFailuresBeforeCooldown failed searches.
	dead := &stubSearchProvider{
		name: "senshi",
		errs: []error{eof, eof, eof, eof, eof, eof},
	}
	stubProvider(t, "senshi", dead)

	for i := 0; i < providerFailuresBeforeCooldown; i++ {
		if _, err := searchProviderWithRetry("senshi", "demo", "sub"); err == nil {
			t.Fatalf("search %d: expected the dead provider to fail", i+1)
		}
	}
	callsAfterFirst := dead.calls.Load()

	_, err := searchProviderWithRetry("senshi", "demo", "sub")
	if err == nil {
		t.Fatal("expected the cooling provider to report an error")
	}
	var cooling *errProviderCooling
	if !errors.As(err, &cooling) {
		t.Fatalf("expected a cooling error, got %v", err)
	}
	if got := dead.calls.Load(); got != callsAfterFirst {
		t.Fatalf("cooling provider was contacted again: %d -> %d calls", callsAfterFirst, got)
	}
}

// A cooling provider must not turn into a confusing error for the user.
func TestCoolingProviderReportsAsUnreachable(t *testing.T) {
	failure := providerFailure{
		provider: "senshi",
		err:      &errProviderCooling{provider: "senshi", until: time.Now().Add(time.Minute)},
	}
	if failure.kind() != failureUnreachable {
		t.Fatalf("expected a cooling provider to report as unreachable, got %v", failure.kind())
	}
}

// A healthy provider must still be searched normally alongside a cooling one.
func TestSearchStillUsesHealthyProvidersWhileAnotherCools(t *testing.T) {
	withAllProvidersEnabledForTest(t)
	withProviderHealthClock(t)

	unreachable := errors.New("EOF")
	noteProviderFailure("senshi", unreachable)
	noteProviderFailure("senshi", unreachable)

	stubProvider(t, "senshi", &stubSearchProvider{name: "senshi"})
	stubProvider(t, "anipub", &stubSearchProvider{
		name:    "anipub",
		results: []providers.SelectionOption{{Key: "8433", Label: "Rich Girl Caretaker"}},
	})

	results, err := searchAnimeWithProviders([]string{"senshi", "anipub"}, "demo", "sub")
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected the healthy provider's result, got %v", results)
	}
}

package internal

import (
	"fmt"
	"sync"
	"time"
)

// Streaming hosts go down for days at a time, and nothing in a search run
// remembers that. Every search paid the full timeout for a host that had already
// failed moments earlier -- with senshi that was a guaranteed wasted round trip
// on every single query.
//
// providerHealth records consecutive unreachable failures per provider and skips
// a provider for a short cooldown once it has clearly stopped answering. Only
// network-level failures count: a provider that answers "no results" is working
// fine and must never be cooled down.

const (
	// providerFailuresBeforeCooldown is how many consecutive unreachable failures
	// trip the cooldown.
	providerFailuresBeforeCooldown = 2
	// providerCooldown is how long a tripped provider is skipped for.
	providerCooldown = 5 * time.Minute
)

// errProviderCooling reports that a provider is being skipped after repeated
// failures. It is not a user-facing error on its own.
type errProviderCooling struct {
	provider string
	until    time.Time
}

func (e *errProviderCooling) Error() string {
	remaining := time.Until(e.until).Round(time.Second)
	if remaining < 0 {
		remaining = 0
	}
	return fmt.Sprintf("skipped: %s failed repeatedly, retrying in %s", e.provider, remaining)
}

type providerHealthEntry struct {
	consecutiveFailures int
	coolingUntil        time.Time
}

var (
	providerHealthMu sync.Mutex
	providerHealthBy = map[string]*providerHealthEntry{}
	// providerHealthNow is overridable so tests need not sleep out a cooldown.
	providerHealthNow = time.Now
)

// providerCoolingUntil reports the cooldown expiry for a provider, if it is
// currently being skipped.
func providerCoolingUntil(providerName string) (time.Time, bool) {
	providerHealthMu.Lock()
	defer providerHealthMu.Unlock()

	entry, ok := providerHealthBy[providerName]
	if !ok || entry.coolingUntil.IsZero() {
		return time.Time{}, false
	}
	if !providerHealthNow().Before(entry.coolingUntil) {
		// Cooldown elapsed: give the provider a clean slate so one more failure
		// does not immediately re-trip it.
		entry.coolingUntil = time.Time{}
		entry.consecutiveFailures = 0
		return time.Time{}, false
	}
	return entry.coolingUntil, true
}

// noteProviderSuccess clears any recorded failures for a provider.
func noteProviderSuccess(providerName string) {
	providerHealthMu.Lock()
	defer providerHealthMu.Unlock()
	delete(providerHealthBy, providerName)
}

// noteProviderFailure records a failure. Only unreachable failures count toward
// the cooldown; a definitive "no results" means the host is healthy.
func noteProviderFailure(providerName string, err error) {
	if err == nil || !isRetryableProviderError(err) {
		noteProviderSuccess(providerName)
		return
	}

	providerHealthMu.Lock()
	defer providerHealthMu.Unlock()

	entry, ok := providerHealthBy[providerName]
	if !ok {
		entry = &providerHealthEntry{}
		providerHealthBy[providerName] = entry
	}
	entry.consecutiveFailures++
	if entry.consecutiveFailures >= providerFailuresBeforeCooldown && entry.coolingUntil.IsZero() {
		entry.coolingUntil = providerHealthNow().Add(providerCooldown)
		Log(fmt.Sprintf("Provider %s failed %d times in a row; skipping it for %s", providerName, entry.consecutiveFailures, providerCooldown))
	}
}

// ResetProviderHealth clears all recorded provider failures, so a user retrying
// on purpose is never told to wait.
func ResetProviderHealth() {
	providerHealthMu.Lock()
	defer providerHealthMu.Unlock()
	providerHealthBy = map[string]*providerHealthEntry{}
}

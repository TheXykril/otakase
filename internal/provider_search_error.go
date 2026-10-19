package internal

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/thexykril/otakase/internal/providerhost"
)

// A failed stacked search used to report every provider error concatenated into
// one line:
//
//	all provider searches failed: anikoto: Post "https://noob2.broggl.farm/api":
//	EOF; anipub: no results for "..."; anineko: context deadline exceeded ...
//
// which buries the one distinction that matters to the user: whether the hosts
// are down (retry, or the show is fine) or whether they simply do not carry this
// show (search under a different name).

// providerFailure records why one provider produced no results.
type providerFailure struct {
	provider string
	err      error
	// timedOut marks a provider that never finished before the search deadline.
	timedOut bool
}

// failureKind classifies a provider failure for reporting.
type failureKind int

const (
	// failureNoResults means the provider answered and does not carry the show.
	failureNoResults failureKind = iota
	// failureUnreachable means the host could not be reached or did not answer.
	failureUnreachable
	// failureDisabled means the provider is switched off, not broken.
	failureDisabled
	// failureOther covers answers that were neither a clean miss nor a network fault.
	failureOther
)

func (f providerFailure) kind() failureKind {
	if f.timedOut {
		return failureUnreachable
	}
	if f.err == nil {
		return failureNoResults
	}

	var cooling *errProviderCooling
	if errors.As(f.err, &cooling) {
		return failureUnreachable
	}

	message := strings.ToLower(f.err.Error())
	switch {
	case strings.Contains(message, "is disabled"):
		return failureDisabled
	case strings.Contains(message, "no results for"), strings.Contains(message, "not found"):
		return failureNoResults
	case errors.Is(f.err, providerhost.ErrRateLimited), strings.Contains(message, "too many requests"):
		return failureUnreachable
	case isRetryableProviderError(f.err), strings.Contains(message, "cloudflare"), strings.Contains(message, "maintenance"):
		return failureUnreachable
	default:
		return failureOther
	}
}

// reason renders the short cause shown beside a provider name.
func (f providerFailure) reason() string {
	if f.timedOut {
		return "no answer before the search timed out"
	}
	if f.err == nil {
		return "no results"
	}

	message := strings.TrimSpace(f.err.Error())
	// Provider errors already quote the query back; the header states it once, so
	// drop the repetition from each line.
	if idx := strings.Index(message, ` for "`); idx > 0 && strings.HasPrefix(message, "no results") {
		message = message[:idx]
	}
	if len(message) > 120 {
		message = message[:120] + "..."
	}
	return message
}

// newProviderSearchError builds the message shown when no provider had the show.
func newProviderSearchError(query string, failures []providerFailure) error {
	grouped := make(map[failureKind][]providerFailure, 4)
	for _, failure := range failures {
		kind := failure.kind()
		grouped[kind] = append(grouped[kind], failure)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "no provider had %q", query)

	// A clean miss everywhere is a different problem from every host being down,
	// so lead with whichever explanation actually applies.
	switch {
	case len(grouped[failureNoResults]) == len(failures):
		b.WriteString(" — every provider answered, none carries it. Try searching by another name.")
		return fmt.Errorf("%s", b.String())
	case len(grouped[failureUnreachable]) == len(failures):
		b.WriteString(" — no provider could be reached. Check your connection, or the hosts may be down.")
	}

	b.WriteString("\n")
	for _, kind := range []failureKind{failureUnreachable, failureOther, failureNoResults, failureDisabled} {
		entries := grouped[kind]
		if len(entries) == 0 {
			continue
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].provider < entries[j].provider })

		if kind == failureNoResults {
			// These all say the same thing, so name them on one line.
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				names = append(names, entry.provider)
			}
			fmt.Fprintf(&b, "  %s: no results\n", strings.Join(names, ", "))
			continue
		}
		for _, entry := range entries {
			fmt.Fprintf(&b, "  %s: %s\n", entry.provider, entry.reason())
		}
	}

	return fmt.Errorf("%s", strings.TrimRight(b.String(), "\n"))
}

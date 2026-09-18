package internal

import (
	"errors"
	"strings"
	"testing"
)

func TestProviderFailureKindClassification(t *testing.T) {
	cases := []struct {
		name    string
		failure providerFailure
		want    failureKind
	}{
		{"clean miss", providerFailure{provider: "anipub", err: errors.New(`no results for "demo"`)}, failureNoResults},
		{"dead host", providerFailure{provider: "anipub", err: errors.New(`Post "https://anipub.live/anime/filter": EOF`)}, failureUnreachable},
		{"timeout", providerFailure{provider: "anineko", err: errors.New("context deadline exceeded")}, failureUnreachable},
		{"never finished", providerFailure{provider: "anineko", timedOut: true}, failureUnreachable},
		{"maintenance", providerFailure{provider: "anidb", err: errors.New("anidb.app is under maintenance")}, failureUnreachable},
		{"cloudflare", providerFailure{provider: "anineko", err: errors.New("Cloudflare challenge")}, failureUnreachable},
		{"switched off", providerFailure{provider: "anikoto", err: errors.New(`provider "anikoto" is disabled: ...`)}, failureDisabled},
		{"odd answer", providerFailure{provider: "anipub", err: errors.New("could not decode search response")}, failureOther},
	}

	for _, tc := range cases {
		if got := tc.failure.kind(); got != tc.want {
			t.Fatalf("%s: kind = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// When every provider answered and none carries the show, the user should be told
// to search under a different name -- not shown a wall of network errors.
func TestProviderSearchErrorAllCleanMisses(t *testing.T) {
	err := newProviderSearchError("Some Obscure OVA", []providerFailure{
		{provider: "anipub", err: errors.New(`no results for "Some Obscure OVA"`)},
		{provider: "anineko", err: errors.New(`no results for "Some Obscure OVA"`)},
	})

	message := err.Error()
	if !strings.Contains(message, "none carries it") {
		t.Fatalf("expected a 'not carried' explanation, got: %s", message)
	}
	if !strings.Contains(message, "another name") {
		t.Fatalf("expected actionable advice, got: %s", message)
	}
	// The query is stated once in the header, not repeated per provider.
	if strings.Count(message, "Some Obscure OVA") != 1 {
		t.Fatalf("expected the query named once, got: %s", message)
	}
}

func TestProviderSearchErrorAllUnreachable(t *testing.T) {
	err := newProviderSearchError("One Piece", []providerFailure{
		{provider: "anipub", err: errors.New("context deadline exceeded")},
		{provider: "anineko", timedOut: true},
	})

	message := err.Error()
	if !strings.Contains(message, "could be reached") {
		t.Fatalf("expected an outage explanation, got: %s", message)
	}
	if !strings.Contains(message, "anipub") || !strings.Contains(message, "anineko") {
		t.Fatalf("expected both providers named, got: %s", message)
	}
}

// Mixed causes must stay distinguishable: one host down is a different situation
// from the others simply not carrying the show.
func TestProviderSearchErrorGroupsMixedCauses(t *testing.T) {
	err := newProviderSearchError("Demo Show", []providerFailure{
		{provider: "anipub", err: errors.New(`Post "https://anipub.live/anime/filter": EOF`)},
		{provider: "anipub", err: errors.New(`no results for "Demo Show"`)},
		{provider: "anineko", err: errors.New(`no results for "Demo Show"`)},
	})

	message := err.Error()
	if !strings.Contains(message, "anipub:") {
		t.Fatalf("expected the unreachable host called out, got: %s", message)
	}
	// The two clean misses collapse onto one line rather than repeating.
	if !strings.Contains(message, "anineko, anipub: no results") {
		t.Fatalf("expected clean misses grouped on one line, got: %s", message)
	}
	// Unreachable hosts are listed before clean misses.
	if strings.Index(message, "anipub:") > strings.Index(message, "no results") {
		t.Fatalf("expected unreachable hosts listed first, got: %s", message)
	}
}

func TestProviderSearchErrorTruncatesLongCauses(t *testing.T) {
	err := newProviderSearchError("Demo", []providerFailure{
		{provider: "anipub", err: errors.New("boom " + strings.Repeat("x", 400))},
	})
	for _, line := range strings.Split(err.Error(), "\n") {
		if len(line) > 200 {
			t.Fatalf("line too long (%d): %s", len(line), line)
		}
	}
}

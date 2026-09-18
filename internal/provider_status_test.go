package internal

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestProviderHealthOK(t *testing.T) {
	if !(ProviderHealth{Results: 3}).OK() {
		t.Fatal("results with no error is healthy")
	}
	if (ProviderHealth{Results: 3, Err: errors.New("boom")}).OK() {
		t.Fatal("an error is never healthy")
	}
	if (ProviderHealth{Results: 0}).OK() {
		t.Fatal("zero results is not healthy")
	}
}

func TestProviderHealthSummary(t *testing.T) {
	healthy := ProviderHealth{Results: 8, Latency: 173 * time.Millisecond}.Summary()
	if !strings.Contains(healthy, "8 result(s)") {
		t.Fatalf("unhelpful summary %q", healthy)
	}

	failed := ProviderHealth{Latency: time.Second, Err: errors.New(`Post "https://anipub.live/anime/filter": EOF`)}.Summary()
	if !strings.Contains(failed, "FAILED") || !strings.Contains(failed, "anipub.live") {
		t.Fatalf("summary must carry the cause, got %q", failed)
	}

	empty := ProviderHealth{Latency: 200 * time.Millisecond}.Summary()
	if !strings.Contains(empty, "no results") {
		t.Fatalf("unhelpful summary %q", empty)
	}
}

func TestFormatProviderStatusReportsStateAndReasons(t *testing.T) {
	report := []ProviderHealth{
		{Name: "anipub", Enabled: true, InStack: true, Results: 56, Latency: 161 * time.Millisecond},
		{Name: "anipub", Enabled: false, DisabledReason: "domain lapsed and is parked for sale", Err: errors.New("EOF")},
	}

	out := FormatProviderStatus(report, "one piece")

	for _, want := range []string{
		`"one piece"`,
		"anipub",
		"enabled, in stack",
		"56 result(s)",
		"anipub",
		"disabled",
		"domain lapsed and is parked for sale",
		"1 of 2 providers returned results.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in report:\n%s", want, out)
		}
	}
}

func TestFormatProviderStatusFlagsTotalOutage(t *testing.T) {
	out := FormatProviderStatus([]ProviderHealth{
		{Name: "anipub", Enabled: true, Err: errors.New("timeout")},
	}, "demo")

	if !strings.Contains(out, "No provider answered") {
		t.Fatalf("expected a total-outage hint:\n%s", out)
	}
}

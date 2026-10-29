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

	failed := ProviderHealth{Latency: time.Second, Err: errors.New(`Post "https://anizone.live/anime/filter": EOF`)}.Summary()
	if !strings.Contains(failed, "FAILED") || !strings.Contains(failed, "anizone.live") {
		t.Fatalf("summary must carry the cause, got %q", failed)
	}

	empty := ProviderHealth{Latency: 200 * time.Millisecond}.Summary()
	if !strings.Contains(empty, "no results") {
		t.Fatalf("unhelpful summary %q", empty)
	}
}

func TestFormatProviderStatusReportsStateAndReasons(t *testing.T) {
	report := []ProviderHealth{
		{Name: "anizone", Enabled: true, InStack: true, Results: 56, Latency: 161 * time.Millisecond},
		{Name: "anizone", Enabled: false, DisabledReason: "domain lapsed and is parked for sale", Err: errors.New("EOF")},
	}

	out := FormatProviderStatus(report, "one piece")

	for _, want := range []string{
		`"one piece"`,
		"anizone",
		"enabled, in stack",
		"56 result(s)",
		"anizone",
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
		{Name: "anizone", Enabled: true, Err: errors.New("timeout")},
	}, "demo")

	if !strings.Contains(out, "No provider answered") {
		t.Fatalf("expected a total-outage hint:\n%s", out)
	}
}

func TestStatusQueryForAdultProvider(t *testing.T) {
	if got := statusQueryFor("sukebei", "one piece", "overflow"); got != "overflow" {
		t.Fatalf("18+ provider searched %q, want its own query", got)
	}
	if got := statusQueryFor("nyaa", "one piece", "overflow"); got != "one piece" {
		t.Fatalf("general provider searched %q, want the main query", got)
	}
}

func TestFormatProviderStatusShowsOwnQuery(t *testing.T) {
	out := FormatProviderStatus([]ProviderHealth{
		{Name: "nyaa", Query: "one piece", Results: 3},
		{Name: "sukebei", Query: "overflow", Results: 2},
	}, "one piece")
	if !strings.Contains(out, `searched: "overflow"`) {
		t.Fatalf("own query missing:\n%s", out)
	}
	if strings.Count(out, "searched:") != 1 {
		t.Fatalf("main query repeated on rows:\n%s", out)
	}
}

func TestProviderStatusSummaryCountsWithoutNamingSources(t *testing.T) {
	report := []ProviderHealth{
		{Name: "anizone", Enabled: true, Results: 3},
		{Name: "kickassanime", Enabled: true, Err: errors.New("timeout")},
		{Name: "sukebei", Enabled: true, Results: 1},
	}
	out := FormatProviderStatusSummary(report, false)
	if !strings.Contains(out, "Sources: 1 of 2 working.") || strings.Contains(out, "18+") {
		t.Fatalf("summary:\n%s", out)
	}
	for _, health := range report {
		if strings.Contains(out, health.Name) {
			t.Fatalf("%s named:\n%s", health.Name, out)
		}
	}
	if adult := FormatProviderStatusSummary(report, true); !strings.Contains(adult, "18+ sources: 1 of 1 working.") {
		t.Fatalf("adult summary:\n%s", adult)
	}
}

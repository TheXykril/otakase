package internal

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/thexykril/otakase/internal/providers"
)

// Streaming hosts rot constantly: domains lapse, APIs start demanding signatures,
// and Cloudflare appears overnight. ProviderStatus turns "otakase can't find any
// anime" into a per-provider answer the user can act on.

// ProviderHealth is the outcome of probing one provider.
type ProviderHealth struct {
	Name           string
	Query          string // what the probe searched for
	Enabled        bool
	DisabledReason string
	InStack        bool
	Results        int
	Latency        time.Duration
	Err            error
}

// OK reports whether the provider returned usable search results.
func (h ProviderHealth) OK() bool {
	return h.Err == nil && h.Results > 0
}

// Summary renders a single status line for the report.
func (h ProviderHealth) Summary() string {
	switch {
	case h.OK():
		return fmt.Sprintf("%d result(s) in %s", h.Results, h.Latency.Round(time.Millisecond))
	case h.Err != nil:
		return fmt.Sprintf("FAILED after %s: %v", h.Latency.Round(time.Millisecond), h.Err)
	default:
		return fmt.Sprintf("no results in %s", h.Latency.Round(time.Millisecond))
	}
}

// Default probe queries. 18+ providers carry nothing under a general title, so
// they get one of their own.
const (
	DefaultStatusQuery      = "one piece"
	DefaultAdultStatusQuery = "overflow"
)

// CheckProviders probes every registered provider with a search and reports what
// each one did. Disabled providers are probed too, so the report can show whether
// a provider is down or merely switched off. 18+ providers search adultQuery.
func CheckProviders(config *Config, query, adultQuery string) []ProviderHealth {
	query = strings.TrimSpace(query)
	if query == "" {
		query = DefaultStatusQuery
	}
	adultQuery = strings.TrimSpace(adultQuery)
	if adultQuery == "" {
		adultQuery = DefaultAdultStatusQuery
	}

	names := providers.RegisteredNames()
	sort.Strings(names)

	inStack := make(map[string]struct{})
	for _, name := range configuredProviderNames(config) {
		inStack[name] = struct{}{}
	}

	report := make([]ProviderHealth, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func(index int, providerName string) {
			defer wg.Done()

			health := ProviderHealth{Name: providerName, Query: statusQueryFor(providerName, query, adultQuery)}
			health.DisabledReason = ProviderDisabledReason(providerName)
			health.Enabled = health.DisabledReason == ""
			_, health.InStack = inStack[providerName]

			provider, err := providers.New(providerName)
			if err != nil {
				health.Err = err
				report[index] = health
				return
			}

			// Probe the provider directly rather than through ProviderByName so a
			// disabled provider still reports whether its host is actually alive.
			start := time.Now()
			options, err := provider.SearchAnime(health.Query, "sub")
			health.Latency = time.Since(start)
			health.Results = len(options)
			health.Err = err
			report[index] = health
		}(i, name)
	}
	wg.Wait()

	return report
}

// statusQueryFor picks the probe query that suits the provider.
func statusQueryFor(name, query, adultQuery string) string {
	if isAdultProvider(name) {
		return adultQuery
	}
	return query
}

// FormatProviderStatus renders the health report as plain text.
func FormatProviderStatus(report []ProviderHealth, query string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Provider status (search query: %q)\n\n", query)

	working := 0
	for _, health := range report {
		marker := "✗"
		if health.OK() {
			marker = "✓"
			working++
		}

		state := "enabled"
		if !health.Enabled {
			state = "disabled"
		}
		if health.InStack {
			state += ", in stack"
		}

		fmt.Fprintf(&b, "  %s %-11s [%s]\n", marker, health.Name, state)
		fmt.Fprintf(&b, "      %s\n", health.Summary())
		if health.Query != "" && health.Query != query {
			fmt.Fprintf(&b, "      searched: %q\n", health.Query)
		}
		if health.DisabledReason != "" {
			fmt.Fprintf(&b, "      reason: %s\n", health.DisabledReason)
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "%d of %d providers returned results.\n", working, len(report))
	if working == 0 {
		b.WriteString("\nNo provider answered. Check your network, or the hosts may all be down.\n")
	}

	return b.String()
}

// ProviderStatusDetailsEnv set to 1 makes -provider-status print every
// source by name, for the Discord bot and debugging. Viewers otherwise see
// counts only.
const ProviderStatusDetailsEnv = "OTAKASE_PROVIDER_STATUS_DETAILS"

// FormatProviderStatusSummary is the health report as viewers see it: how
// many sources work, never which. 18+ sources get their own count, shown
// only with adult on.
func FormatProviderStatusSummary(report []ProviderHealth, adult bool) string {
	var working, total, adultWorking, adultTotal int
	for _, health := range report {
		if isAdultProvider(health.Name) {
			adultTotal++
			if health.OK() {
				adultWorking++
			}
			continue
		}
		total++
		if health.OK() {
			working++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Sources: %d of %d working.\n", working, total)
	if adult && adultTotal > 0 {
		fmt.Fprintf(&b, "18+ sources: %d of %d working.\n", adultWorking, adultTotal)
	}
	switch {
	case working == 0:
		b.WriteString("No source answered. Check your network, or try again later.\n")
	case working < total:
		b.WriteString("Some are down; otakase tries the others by itself.\n")
	}
	return b.String()
}

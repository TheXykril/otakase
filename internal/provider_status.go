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
// and Cloudflare appears overnight. ProviderStatus turns "curd can't find any
// anime" into a per-provider answer the user can act on.

// ProviderHealth is the outcome of probing one provider.
type ProviderHealth struct {
	Name           string
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

// CheckProviders probes every registered provider with a search and reports what
// each one did. Disabled providers are probed too, so the report can show whether
// a provider is down or merely switched off.
func CheckProviders(config *CurdConfig, query string) []ProviderHealth {
	query = strings.TrimSpace(query)
	if query == "" {
		query = "one piece"
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

			health := ProviderHealth{Name: providerName}
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
			options, err := provider.SearchAnime(query, "sub")
			health.Latency = time.Since(start)
			health.Results = len(options)
			health.Err = err
			report[index] = health
		}(i, name)
	}
	wg.Wait()

	return report
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

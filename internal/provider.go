package internal

import (
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/providers"
)

const (
	// providerSearchAttempts bounds retries for a single provider search.
	providerSearchAttempts = 2
	// providerSearchRetryDelay is the base backoff between search attempts.
	providerSearchRetryDelay = 500 * time.Millisecond
	// providerSearchGrace is how long a straggler gets once another provider has
	// already produced results. Animepahe spends ~17s on its browser challenge;
	// without this the whole search waits on it even when anipub answered in 0.2s.
	providerSearchGrace = 2500 * time.Millisecond
	// providerSearchDeadline caps a stacked search when nothing has succeeded yet.
	providerSearchDeadline = 20 * time.Second
)

// isRetryableProviderError reports whether a provider failure looks transient
// (timeout, reset connection, truncated response) rather than a definitive
// "this host has no such show" answer, which must not be retried.
func isRetryableProviderError(err error) bool {
	if err == nil {
		return false
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	message := strings.ToLower(err.Error())
	for _, transient := range []string{
		"timeout",
		"deadline exceeded",
		"connection reset",
		"connection refused",
		"unexpected eof",
		"eof",
		"temporary failure",
		"no such host",
		"broken pipe",
		"server misbehaving",
		// Throttling is temporary by definition. Without these a rate-limited
		// provider is treated as a definitive answer: not retried, not cooled
		// down, and reported to the user as though the show does not exist.
		"too many requests",
		"rate limited",
		"rate limit",
		"status 429",
	} {
		if strings.Contains(message, transient) {
			return true
		}
	}
	return false
}

// Provider is a streaming host as the rest of the app sees it: a
// providers.Provider whose episode lookups take the whole Config.
type Provider interface {
	// Name is the provider's id as the Provider setting spells it.
	Name() string
	// SearchAnime finds shows by title in the given audio (sub or dub).
	SearchAnime(title, mode string) ([]SelectionOption, error)
	// EpisodesList is a show's episodes, labelled as the host labels them.
	EpisodesList(id, mode string) ([]string, error)
	// GetEpisodeURL resolves one episode in config.SubOrDub.
	GetEpisodeURL(config Config, id string, epNo int) ([]string, error)
}

// ProviderModeResolver is a Provider that can be asked for a given audio
// rather than the one config.SubOrDub names.
type ProviderModeResolver interface {
	GetEpisodeURLForMode(config Config, id string, epNo int, mode string) ([]string, error)
}

// providerHintResolver also reports the player hints for each stream.
type providerHintResolver interface {
	GetEpisodeURLForModeWithHints(config Config, id string, epNo int, mode string) ([]string, map[string]StreamPlaybackHint, error)
}

// ProviderEpisodeResult is a resolved episode: its stream links and which
// provider, show id and audio they came from.
type ProviderEpisodeResult struct {
	Links     []string                      // stream URLs, preferred first
	LinkHints map[string]StreamPlaybackHint // keyed by link; may be nil

	ProviderName string // host the links came from
	ProviderID   string // the show's id on that host
	Mode         string // audio the links are in
}

// providerIDSeparator joins a provider name to one of its show ids, as in
// "anikoto::21". Stacked search results are keyed this way, since an id means
// nothing to any other host.
const providerIDSeparator = "::" // not valid inside a provider name

// CurrentProvider caches GetProvider's answer. Changing the Provider setting
// resets it to nil.
var CurrentProvider Provider

// normalizeProviderName is the canonical spelling of a provider name, or ""
// when it names no provider.
func normalizeProviderName(name string) string {
	return providers.NormalizeName(name)
}

// uniqueProviderNames returns the canonical form of each name that is a
// provider, keeping the first of any repeats.
func uniqueProviderNames(names []string) []string {
	unique := make([]string, 0, len(names))
	for _, name := range names {
		if name = normalizeProviderName(name); name != "" && !slices.Contains(unique, name) {
			unique = append(unique, name)
		}
	}
	return unique
}

// splitProviderList splits a list written with any of the separators the
// Provider setting has accepted over time.
func splitProviderList(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		return strings.ContainsRune(",+|;", r)
	})
}

// parseProviderConfigParts splits a Provider value into the names it lists,
// unchecked. The value is a JSON list when written by otakase, but anything
// that splits into names is accepted.
func parseProviderConfigParts(value string) []string {
	if parts := parseStringArray(value); len(parts) > 0 {
		return parts
	}
	return splitProviderList(value)
}

// parseProviderConfig returns the providers a Provider value names, in order.
// The stack stands for itself, and a value naming nothing usable falls back
// to the first enabled provider.
func parseProviderConfig(raw string) []string {
	value := strings.TrimSpace(raw)
	if value == "" || isStackedProviderConfig(value) {
		return defaultEnabledProviderStack()
	}

	names := uniqueProviderNames(parseProviderConfigParts(value))
	if len(names) == 0 {
		// parseStringArray splits on commas only, so "anikoto+anineko" comes
		// back as one name that is not a provider.
		names = uniqueProviderNames(splitProviderList(value))
	}
	if len(names) == 0 {
		return []string{firstEnabledProviderName()}
	}
	return names
}

func configuredProviderNames(config *Config) []string {
	raw := ""
	if config != nil {
		raw = config.Provider
	}
	return ensureEnabledProviderNames(parseProviderConfig(raw))
}

// ConfiguredProviderNames returns the enabled providers the config's
// Provider setting names, in the order they are tried.
func ConfiguredProviderNames(config *Config) []string {
	return configuredProviderNames(config)
}

// canonicalProviderConfigValue is the form a Provider value is saved in: the
// stack as "stacked", anything else as a JSON list of canonical names.
func canonicalProviderConfigValue(raw string) string {
	value := strings.TrimSpace(raw)
	// A list naming a provider this build no longer has is not a list to tidy
	// up -- it is one to replace. Canonicalising it would quietly rewrite the
	// user's choice as whichever provider happens to head the stack.
	replace := isStackedProviderConfig(value) || (value != "" && namesRemovedProvider(value))
	if !replace {
		// A list that is exactly the default stack is saved as the stack, so
		// it keeps following the default when that changes.
		if names := parseProviderConfig(value); !slices.Equal(names, defaultEnabledProviderStack()) {
			return formatProviderConfigValue(names)
		}
	}
	return stackedProviderConfigValue
}

// formatProviderConfigValue writes names as the JSON list the config holds.
func formatProviderConfigValue(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = strconv.Quote(name)
	}
	return "[" + strings.Join(quoted, ",") + "]"
}

// ProviderStackContains reports whether the configured stack includes the
// named provider.
func ProviderStackContains(config *Config, providerName string) bool {
	name := normalizeProviderName(providerName)
	return name != "" && slices.Contains(configuredProviderNames(config), name)
}

// ProviderByName returns a fresh instance of a provider, unless it is
// disabled.
func ProviderByName(requested string) (Provider, error) {
	name := normalizeProviderName(requested)
	if why := providerDisabledReason(name); why != "" {
		return nil, fmt.Errorf("provider %q is disabled: %s", name, why)
	}
	impl, err := providers.New(name)
	if err != nil {
		return nil, err
	}
	return wrapProvider(impl), nil
}

// GetProvider returns the provider at the head of the configured stack,
// falling back to the first enabled one. It is only the right provider for
// ids that came from it; a show's own provider is providerIDForAnime's.
func GetProvider() Provider {
	if CurrentProvider == nil {
		// Stays nil, and is retried next call, when nothing can be made.
		CurrentProvider = headProvider()
	}
	return CurrentProvider
}

// headProvider makes the provider at the head of the configured stack, or
// the first enabled one when that fails. It is nil when neither can be made.
func headProvider() Provider {
	if head, err := ProviderByName(configuredProviderNames(GetGlobalConfig())[0]); err == nil {
		return head
	}
	fallback, err := ProviderByName(firstEnabledProviderName())
	if err != nil {
		return nil
	}
	return fallback
}

// QualifyProviderID prefixes a show id with the provider it belongs to.
func QualifyProviderID(provider, id string) string {
	name := normalizeProviderName(provider)
	if name == "" || id == "" {
		return id
	}
	if owner, _, ok := ParseProviderQualifiedID(id); ok && owner == name {
		return id // already qualified for this provider
	}
	return name + providerIDSeparator + id
}

// ParseProviderQualifiedID splits an id made by QualifyProviderID. A bare id
// comes back unchanged with ok false.
func ParseProviderQualifiedID(qualified string) (providerName, rawProviderID string, ok bool) {
	name, rawID, found := strings.Cut(qualified, providerIDSeparator)
	if name = normalizeProviderName(name); !found || name == "" || rawID == "" {
		return "", qualified, false
	}
	return name, rawID, true
}

// CurrentAnimeProviderName is the provider an anime's stored id belongs to.
func CurrentAnimeProviderName(anime *Anime) string {
	return providerNameForAnime(anime)
}

func providerNameForAnime(anime *Anime) string {
	name, _ := providerIDForAnime(anime)
	return name
}

// AnimeProviderID returns the anime's provider and its id there.
func AnimeProviderID(anime *Anime) (string, string) {
	return providerIDForAnime(anime)
}

// providerIDForAnime reads the provider and id stored for an anime. A
// qualified id names its own provider; otherwise ProviderName does, and
// without either the id is taken to be GetProvider's.
func providerIDForAnime(anime *Anime) (name, id string) {
	if anime != nil {
		id = anime.ProviderId
		if owner, rawID, ok := ParseProviderQualifiedID(id); ok {
			return owner, rawID
		}
		if owner := normalizeProviderName(anime.ProviderName); owner != "" {
			return owner, id
		}
	}
	return GetProvider().Name(), id
}

// providerNamesForAnime is the order providers are tried in for a show: the
// stack that suits it, then the provider its stored id belongs to, when that
// one is usable for it but not already in the stack.
func providerNamesForAnime(config *Config, anime *Anime) []string {
	names := uniqueProviderNamesKeepingOrder(providerNamesForShow(config, anime))

	stored := normalizeProviderName(providerNameForAnime(anime))
	if stored != "" && ProviderEnabled(stored) && providerSuitsShow(config, anime, stored) && !slices.Contains(names, stored) {
		names = append(names, stored)
	}
	if len(names) == 0 {
		names = append(names, firstEnabledProviderName())
	}
	return names
}

// uniqueProviderNamesKeepingOrder drops repeats from names that are already
// canonical.
func uniqueProviderNamesKeepingOrder(names []string) []string {
	unique := make([]string, 0, len(names))
	for _, name := range names {
		if !slices.Contains(unique, name) {
			unique = append(unique, name)
		}
	}
	return unique
}

// qualifySelectionOption marks a stacked search result with its provider, in
// the key so it can be resolved later and in the label so the user can tell
// two hosts' copies of a show apart.
func qualifySelectionOption(providerName string, opt SelectionOption) SelectionOption {
	tag := "[" + providerName + "]"
	opt.Key = QualifyProviderID(providerName, opt.Key)
	if !strings.Contains(strings.ToLower(opt.Label), tag) {
		opt.Label += " " + tag
	}
	return opt
}

// SearchAnime searches every configured provider.
func SearchAnime(title, mode string) ([]SelectionOption, error) {
	stack := configuredProviderNames(GetGlobalConfig())
	return searchAnimeWithProviders(stack, title, mode)
}

// searchProviderWithRetry runs one provider search, retrying transient network
// failures. A single slow response used to sink the whole search: providers were
// tried one after another against a shared 15s client timeout, so one stalled host
// consumed the budget and the run reported "all provider searches failed".
func searchProviderWithRetry(providerName, query, mode string) ([]SelectionOption, error) {
	provider, err := ProviderByName(providerName)
	if err != nil {
		return nil, err
	}

	// A provider that has just failed repeatedly is skipped rather than waited on
	// again, so a host that is down stops costing a timeout per search.
	if until, cooling := providerCoolingUntil(providerName); cooling {
		return nil, &errProviderCooling{provider: providerName, until: until}
	}

	var lastErr error
	for attempt := 1; attempt <= providerSearchAttempts; attempt++ {
		options, err := provider.SearchAnime(query, mode)
		if err == nil {
			noteProviderSuccess(providerName)
			noteProviderResult(providerName, "search", nil)
			return options, nil
		}
		lastErr = err
		if !isRetryableProviderError(err) || attempt == providerSearchAttempts {
			break
		}
		Log(fmt.Sprintf("Provider %s search attempt %d/%d for %q failed transiently: %v", providerName, attempt, providerSearchAttempts, query, err))
		time.Sleep(time.Duration(attempt) * providerSearchRetryDelay)
	}
	noteProviderFailure(providerName, lastErr)
	noteProviderResult(providerName, "search", lastErr)
	return nil, lastErr
}

// searchAnimeWithProviders queries every configured provider concurrently and
// merges the results in stack order, so a dead or slow host costs one timeout in
// parallel rather than delaying every provider behind it.
func searchAnimeWithProviders(stack []string, query, mode string) ([]SelectionOption, error) {
	switch len(stack) {
	case 0:
		return nil, fmt.Errorf("no providers configured")
	case 1:
		// A single provider's results are left unqualified: there is no
		// other host they could be confused with.
		return searchProviderWithRetry(stack[0], query, mode)
	}

	type providerSearch struct {
		index   int
		options []SelectionOption
		err     error
		done    bool
	}

	completed := make(chan providerSearch, len(stack))
	for i, providerName := range stack {
		go func(index int, name string) {
			options, err := searchProviderWithRetry(name, query, mode)
			completed <- providerSearch{index: index, options: options, err: err, done: true}
		}(i, providerName)
	}

	outcomes := make([]providerSearch, len(stack))
	// A straggler only gets providerSearchGrace once some other provider has
	// produced results; until then the search waits out providerSearchDeadline.
	var grace <-chan time.Time
	deadline := time.After(providerSearchDeadline)
	remaining := len(stack)

collect:
	for remaining > 0 {
		select {
		case outcome := <-completed:
			outcomes[outcome.index] = outcome
			remaining--
			if grace == nil && outcome.err == nil && len(outcome.options) > 0 {
				grace = time.After(providerSearchGrace)
			}
		case <-grace:
			Log(fmt.Sprintf("Provider search for %q returning early; %d provider(s) still pending", query, remaining))
			break collect
		case <-deadline:
			Log(fmt.Sprintf("Provider search for %q hit the %s deadline with %d provider(s) pending", query, providerSearchDeadline, remaining))
			break collect
		}
	}

	merged := []SelectionOption{}
	seenKeys := map[string]bool{}
	failures := make([]providerFailure, 0, len(stack))
	for i, providerName := range stack {
		outcome := outcomes[i]
		if !outcome.done {
			failures = append(failures, providerFailure{provider: providerName, timedOut: true})
			continue
		}
		if outcome.err != nil {
			Log(fmt.Sprintf("Provider %s search failed for %q: %v", providerName, query, outcome.err))
			failures = append(failures, providerFailure{provider: providerName, err: outcome.err})
		} else {
			merged = appendQualifiedOptions(merged, seenKeys, providerName, outcome.options)
		}
	}

	if len(merged) == 0 && len(failures) > 0 {
		return nil, newProviderSearchError(query, failures)
	}
	return merged, nil
}

// appendQualifiedOptions adds one provider's results to merged, qualified
// with the provider and skipping any key already seen.
func appendQualifiedOptions(merged []SelectionOption, seen map[string]bool, providerName string, options []SelectionOption) []SelectionOption {
	for _, opt := range options {
		opt = qualifySelectionOption(providerName, opt)
		if seen[opt.Key] {
			continue
		}
		seen[opt.Key] = true
		merged = append(merged, opt)
	}
	return merged
}

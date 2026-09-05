package internal

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wraient/curd/internal/curdhost"
	"github.com/wraient/curd/internal/providers"
)

type fakeNetTimeout struct{}

func (fakeNetTimeout) Error() string   { return "i/o timeout" }
func (fakeNetTimeout) Timeout() bool   { return true }
func (fakeNetTimeout) Temporary() bool { return true }

func TestIsRetryableProviderError(t *testing.T) {
	var netErr net.Error = fakeNetTimeout{}

	retryable := []error{
		netErr,
		io.EOF,
		io.ErrUnexpectedEOF,
		errors.New(`Post "https://senshi.live/anime/filter": EOF`),
		errors.New("context deadline exceeded (Client.Timeout exceeded while awaiting headers)"),
		fmt.Errorf("wrapped: %w", io.ErrUnexpectedEOF),
		errors.New("read: connection reset by peer"),
	}
	for _, err := range retryable {
		if !isRetryableProviderError(err) {
			t.Fatalf("expected %v to be retryable", err)
		}
	}

	// "No such show here" is a definitive answer and must not be retried, or a
	// stacked search would take twice as long for every miss.
	notRetryable := []error{
		nil,
		errors.New(`no results for "some show"`),
		errors.New("provider request failed with status 404"),
		errors.New("anipub: could not decode search response"),
	}
	for _, err := range notRetryable {
		if isRetryableProviderError(err) {
			t.Fatalf("expected %v to not be retryable", err)
		}
	}
}

// stubSearchProvider records how often it was searched and answers on a script.
type stubSearchProvider struct {
	name    string
	calls   atomic.Int32
	delay   time.Duration
	results []providers.SelectionOption
	errs    []error
}

func (s *stubSearchProvider) Name() string { return s.name }

func (s *stubSearchProvider) SearchAnime(query, mode string) ([]providers.SelectionOption, error) {
	index := int(s.calls.Add(1)) - 1
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	if index < len(s.errs) && s.errs[index] != nil {
		return nil, s.errs[index]
	}
	return s.results, nil
}

func (s *stubSearchProvider) EpisodesList(showID, mode string) ([]string, error) { return nil, nil }

func (s *stubSearchProvider) GetEpisodeURL(config providers.PlaybackConfig, id string, epNo int) ([]string, error) {
	return nil, nil
}

func stubProvider(t *testing.T, name string, stub *stubSearchProvider) {
	t.Helper()
	restore := providers.SetFactoryForTest(name, func() providers.Provider { return stub })
	t.Cleanup(restore)
}

func TestSearchProviderWithRetryRetriesTransientFailures(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	stub := &stubSearchProvider{
		name: "anipub",
		errs: []error{errors.New("context deadline exceeded")},
		results: []providers.SelectionOption{
			{Key: "1", Label: "Recovered Show"},
		},
	}
	stubProvider(t, "anipub", stub)

	options, err := searchProviderWithRetry("anipub", "demo", "sub")
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if len(options) != 1 || options[0].Label != "Recovered Show" {
		t.Fatalf("unexpected options %v", options)
	}
	if got := stub.calls.Load(); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
}

func TestSearchProviderWithRetryDoesNotRetryDefinitiveMiss(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	stub := &stubSearchProvider{
		name: "anipub",
		errs: []error{errors.New(`no results for "demo"`), nil},
	}
	stubProvider(t, "anipub", stub)

	if _, err := searchProviderWithRetry("anipub", "demo", "sub"); err == nil {
		t.Fatal("expected the miss to be reported")
	}
	if got := stub.calls.Load(); got != 1 {
		t.Fatalf("expected a single attempt for a definitive miss, got %d", got)
	}
}

// A dead provider used to delay every provider behind it, because the stack was
// searched one host at a time against a shared client timeout.
func TestSearchAnimeWithProvidersRunsConcurrently(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	const stall = 300 * time.Millisecond

	slow := &stubSearchProvider{name: "anineko", delay: stall, results: []providers.SelectionOption{{Key: "slow", Label: "Slow"}}}
	alsoSlow := &stubSearchProvider{name: "anipub", delay: stall, results: []providers.SelectionOption{{Key: "fast", Label: "Fast"}}}
	stubProvider(t, "anineko", slow)
	stubProvider(t, "anipub", alsoSlow)

	start := time.Now()
	results, err := searchAnimeWithProviders([]string{"anipub", "anineko"}, "demo", "sub")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected results from both providers, got %v", results)
	}
	// Sequentially this would take at least 2x the stall.
	if elapsed >= 2*stall {
		t.Fatalf("expected concurrent search, took %s for two %s providers", elapsed, stall)
	}
}

// Results must stay in configured stack order regardless of which provider
// happens to answer first.
func TestSearchAnimeWithProvidersPreservesStackOrder(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	fast := &stubSearchProvider{name: "anineko", results: []providers.SelectionOption{{Key: "n1", Label: "From anineko"}}}
	slow := &stubSearchProvider{name: "anipub", delay: 150 * time.Millisecond, results: []providers.SelectionOption{{Key: "p1", Label: "From anipub"}}}
	stubProvider(t, "anineko", fast)
	stubProvider(t, "anipub", slow)

	results, err := searchAnimeWithProviders([]string{"anipub", "anineko"}, "demo", "sub")
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %v", results)
	}
	if !strings.HasPrefix(results[0].Key, "anipub"+providerIDSeparator) {
		t.Fatalf("expected the anipub result first despite it being slower, got %q", results[0].Key)
	}
}

// One healthy provider must carry the search even when the rest are down.
func TestSearchAnimeWithProvidersSurvivesDeadProvider(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	dead := &stubSearchProvider{
		name: "senshi",
		errs: []error{
			errors.New(`Post "https://senshi.live/anime/filter": EOF`),
			errors.New(`Post "https://senshi.live/anime/filter": EOF`),
		},
	}
	alive := &stubSearchProvider{name: "anipub", results: []providers.SelectionOption{{Key: "8433", Label: "Rich Girl Caretaker"}}}
	stubProvider(t, "senshi", dead)
	stubProvider(t, "anipub", alive)

	results, err := searchAnimeWithProviders([]string{"senshi", "anipub"}, "demo", "sub")
	if err != nil {
		t.Fatalf("expected the healthy provider to carry the search, got %v", err)
	}
	if len(results) != 1 || !strings.Contains(results[0].Label, "Rich Girl Caretaker") {
		t.Fatalf("unexpected results %v", results)
	}
}

func TestSearchAnimeWithProvidersReportsWhenAllFail(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	stubProvider(t, "senshi", &stubSearchProvider{name: "senshi", errs: []error{errors.New("EOF"), errors.New("EOF")}})
	stubProvider(t, "anipub", &stubSearchProvider{name: "anipub", errs: []error{errors.New(`no results for "demo"`)}})

	_, err := searchAnimeWithProviders([]string{"senshi", "anipub"}, "demo", "sub")
	if err == nil {
		t.Fatal("expected an error when every provider fails")
	}
	for _, want := range []string{"senshi", "anipub"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q named in %v", want, err)
		}
	}
}

// Guards the concurrent merge against a data race on the shared result slice.
func TestSearchAnimeWithProvidersIsRaceFree(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	stubProvider(t, "anipub", &stubSearchProvider{name: "anipub", results: []providers.SelectionOption{{Key: "a", Label: "A"}}})
	stubProvider(t, "anineko", &stubSearchProvider{name: "anineko", results: []providers.SelectionOption{{Key: "b", Label: "B"}}})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := searchAnimeWithProviders([]string{"anipub", "anineko"}, "demo", "sub"); err != nil {
				t.Errorf("search failed: %v", err)
			}
		}()
	}
	wg.Wait()
}

// Animepahe spends ~17s on its browser challenge. Before the grace window, a
// stacked search waited on it even when another provider had answered in 0.2s.
func TestSearchAnimeWithProvidersDoesNotWaitOnStragglers(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	fast := &stubSearchProvider{name: "anipub", results: []providers.SelectionOption{{Key: "p1", Label: "Fast"}}}
	// Far longer than providerSearchGrace, standing in for animepahe.
	straggler := &stubSearchProvider{name: "animepahe", delay: 15 * time.Second, results: []providers.SelectionOption{{Key: "x", Label: "Slow"}}}
	stubProvider(t, "anipub", fast)
	stubProvider(t, "animepahe", straggler)

	start := time.Now()
	results, err := searchAnimeWithProviders([]string{"anipub", "animepahe"}, "demo", "sub")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 || results[0].Label != "Fast [anipub]" {
		t.Fatalf("expected the fast provider's result, got %v", results)
	}
	if elapsed > providerSearchGrace+2*time.Second {
		t.Fatalf("expected the straggler to be abandoned after the grace window, took %s", elapsed)
	}
}

// The grace window must not slow down the common case where every provider is
// healthy and fast -- that should still return as soon as all have answered.
func TestSearchAnimeWithProvidersReturnsImmediatelyWhenAllFast(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	stubProvider(t, "anipub", &stubSearchProvider{name: "anipub", results: []providers.SelectionOption{{Key: "a", Label: "A"}}})
	stubProvider(t, "anineko", &stubSearchProvider{name: "anineko", results: []providers.SelectionOption{{Key: "b", Label: "B"}}})

	start := time.Now()
	results, err := searchAnimeWithProviders([]string{"anipub", "anineko"}, "demo", "sub")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected both results, got %v", results)
	}
	if elapsed > providerSearchGrace {
		t.Fatalf("healthy providers should not pay the grace window, took %s", elapsed)
	}
}

// With nothing succeeding there is no grace window, so a hung provider is bounded
// by the overall deadline instead.
func TestSearchAnimeWithProvidersReportsPendingProvidersAsUnreachable(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	fast := &stubSearchProvider{name: "anipub", results: []providers.SelectionOption{{Key: "p1", Label: "Fast"}}}
	straggler := &stubSearchProvider{name: "animepahe", delay: 15 * time.Second, errs: []error{errors.New("boom")}}
	stubProvider(t, "anipub", fast)
	stubProvider(t, "animepahe", straggler)

	// The fast provider succeeds, so the straggler is abandoned and simply absent
	// from the results rather than reported as an error.
	results, err := searchAnimeWithProviders([]string{"anipub", "animepahe"}, "demo", "sub")
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected only the fast provider's result, got %v", results)
	}
}

// A provider throttling us is temporary. Treating it as definitive means no
// retry, no cooldown, and a user told their anime does not exist when curd was
// simply being rate limited.
func TestRateLimitsAreRetryable(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("anipub: %w", curdhost.ErrRateLimited),
		errors.New(`{"error":"Too many requests, try again in a minute"}`),
		errors.New("anipub request failed with status 429: rate limit exceeded"),
	} {
		if !isRetryableProviderError(err) {
			t.Fatalf("expected %v to be retryable", err)
		}
	}
}

// ...and it must be reported as the host being unavailable, not as a clean miss.
func TestRateLimitReadsAsUnreachableNotAMiss(t *testing.T) {
	failure := providerFailure{provider: "anipub", err: fmt.Errorf("anipub: %w", curdhost.ErrRateLimited)}
	if got := failure.kind(); got != failureUnreachable {
		t.Fatalf("kind = %v, want failureUnreachable", got)
	}

	message := newProviderSearchError("Some Show", []providerFailure{failure}).Error()
	if strings.Contains(message, "none carries it") {
		t.Fatalf("a rate limit must not be reported as the show not existing: %s", message)
	}
}

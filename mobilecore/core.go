// Package mobilecore is the JSON boundary used by the Android spike.
// Only this package is bound with gomobile. Provider and HTTP types stay in Go.
package mobilecore

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/thexykril/otakase/internal/providerhost"
	"github.com/thexykril/otakase/internal/providers"
	"github.com/thexykril/otakase/internal/providers/anikoto"
	"github.com/thexykril/otakase/internal/providers/megavid"
)

const requestTimeout = 90 * time.Second
const maxRequests = 16

var hooksOnce sync.Once
var errSelectionRequired = errors.New("selection required")

// Core owns outstanding requests. Create one per Android engine and Close it
// when the engine is detached. Start/Result/Cancel are safe across threads.
// Never import this package into the desktop CLI or bot: it owns providerhost.
type Core struct {
	mu        sync.Mutex
	next      int64
	closed    bool
	tasks     map[int64]*task
	transport http.RoundTripper
	timeout   time.Duration
}

type task struct {
	ctx     context.Context
	cancel  context.CancelFunc
	result  chan string
	claimed bool
}

type request struct {
	Version   int    `json:"version"`
	Operation string `json:"operation"`
	Provider  string `json:"provider"`
	Query     string `json:"query"`
	ShowID    string `json:"show_id"`
	Episode   int    `json:"episode"`
	Mode      string `json:"mode"`
}

type apiError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type response struct {
	Version int       `json:"version"`
	Data    any       `json:"data,omitempty"`
	Error   *apiError `json:"error,omitempty"`
}

type show struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Label string `json:"label"`
}

type subtitle struct {
	URL      string `json:"url"`
	Language string `json:"language"`
	Label    string `json:"label"`
}

type stream struct {
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	Referrer  string            `json:"referrer"`
	HLS       bool              `json:"hls"`
	Subtitle  string            `json:"subtitle"`
	Subtitles []subtitle        `json:"subtitles"`
}

// NewCore sets non-interactive, quiet providerhost hooks once. No provider
// text or upstream errors (which sometimes contain signed URLs) reach logs.
func NewCore() *Core {
	hooksOnce.Do(func() {
		providerhost.HTTPClient = func() *http.Client { return &http.Client{Timeout: requestTimeout} }
		providerhost.Log = func(string) {}
		providerhost.Out = func(string) {}
		providerhost.PromptSelect = func([]providerhost.PromptOption) (providerhost.PromptOption, error) {
			return providerhost.PromptOption{}, errSelectionRequired
		}
		providerhost.CurrentSubStyle = func() string { return "soft" }
		providerhost.PersistSubStylePreference = func(string) error { return errSelectionRequired }
	})
	return &Core{tasks: make(map[int64]*task), transport: http.DefaultTransport, timeout: requestTimeout}
}

// Start schedules a JSON request and returns its cancellation handle. Zero
// means the core is closed or the outstanding-request limit was reached.
// Call Result once for every handle, including after Cancel.
func (c *Core) Start(input string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || len(c.tasks) >= maxRequests {
		return 0
	}
	c.next++
	id := c.next
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	t := &task{ctx: ctx, cancel: cancel, result: make(chan string, 1)}
	c.tasks[id] = t
	go func() {
		t.result <- c.execute(ctx, input)
	}()
	return id
}

// Result waits for a single request off the Android UI thread. Cancellation
// returns promptly even while a provider is unwinding its fallback loop.
func (c *Core) Result(id int64) string {
	c.mu.Lock()
	t, ok := c.tasks[id]
	if !ok || t.claimed {
		c.mu.Unlock()
		return failure("invalid_handle", "Unknown or already collected request.", false)
	}
	t.claimed = true
	c.mu.Unlock()
	defer func() { t.cancel(); c.mu.Lock(); delete(c.tasks, id); c.mu.Unlock() }()
	select {
	case value := <-t.result:
		if t.ctx.Err() != nil {
			return contextFailure(t.ctx.Err())
		}
		return value
	case <-t.ctx.Done():
		return contextFailure(t.ctx.Err())
	}
}

// Cancel stops the request and its in-flight HTTP requests. Result still
// needs collecting so the handle is released.
func (c *Core) Cancel(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t := c.tasks[id]; t != nil {
		t.cancel()
	}
}

// Close cancels all outstanding work. No new request is accepted afterwards.
func (c *Core) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for _, t := range c.tasks {
		t.cancel()
	}
}

func contextFailure(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return failure("timeout", "Provider request timed out.", true)
	}
	return failure("cancelled", "Request cancelled.", false)
}

func failure(code, message string, retryable bool) string {
	return encode(response{Version: 1, Error: &apiError{Code: code, Message: message, Retryable: retryable}})
}

func encode(value response) string {
	b, err := json.Marshal(value)
	if err != nil {
		return `{"version":1,"error":{"code":"internal","message":"Response encoding failed.","retryable":false}}`
	}
	return string(b)
}

func (c *Core) execute(ctx context.Context, input string) string {
	if len(input) > 16<<10 {
		return failure("invalid_request", "Request exceeds 16 KiB.", false)
	}
	var req request
	dec := json.NewDecoder(strings.NewReader(input))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return failure("invalid_request", "Invalid JSON request.", false)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return failure("invalid_request", "Expected one JSON object.", false)
	}
	if req.Version != 1 || (req.Mode != "sub" && req.Mode != "dub") {
		return failure("invalid_request", "Use version 1 and mode sub or dub.", false)
	}
	transport := &requestTransport{ctx: ctx, base: c.transport}
	client := &http.Client{Transport: transport, Timeout: requestTimeout}
	var p providers.HintResolver
	var searcher providers.Provider
	switch req.Provider {
	case "megavid":
		v := megavid.NewWithHTTPClient(client)
		p = v
		searcher = v
	case "anikoto":
		v := anikoto.NewWithHTTPClient(client)
		p = v
		searcher = v
	default:
		return failure("unsupported_provider", "Choose megavid or anikoto.", false)
	}
	var data any
	var err error
	switch req.Operation {
	case "search":
		if strings.TrimSpace(req.Query) == "" || len(req.Query) > 300 {
			return failure("invalid_request", "Enter a search query of 1 to 300 bytes.", false)
		}
		var options []providers.SelectionOption
		options, err = searcher.SearchAnime(req.Query, req.Mode)
		shows := make([]show, 0, len(options))
		for _, v := range options {
			shows = append(shows, show{ID: v.Key, Title: v.Title, Label: v.Label})
		}
		data = map[string]any{"shows": shows}
	case "episodes":
		if !validShowID(req.ShowID) {
			return failure("invalid_request", "A show id is required.", false)
		}
		var episodes []string
		episodes, err = searcher.EpisodesList(req.ShowID, req.Mode)
		data = map[string]any{"episodes": episodes}
	case "resolve":
		if !validShowID(req.ShowID) || req.Episode <= 0 || req.Episode > 10000 {
			return failure("invalid_request", "A show id and positive episode are required.", false)
		}
		var urls []string
		var hints map[string]providers.StreamPlaybackHint
		urls, hints, err = p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{SubOrDub: req.Mode, SubStyle: "soft"}, req.ShowID, req.Episode, req.Mode)
		streams := make([]stream, 0, len(urls))
		for _, u := range urls {
			h := hints[u]
			headers := maps.Clone(h.Headers)
			if headers == nil {
				headers = make(map[string]string)
			}
			if h.Referrer != "" {
				headers["Referer"] = h.Referrer
			}
			subs := make([]subtitle, 0, len(h.Subtitles))
			for _, s := range h.Subtitles {
				subs = append(subs, subtitle{URL: s.URL, Language: s.Language, Label: s.Label})
			}
			streams = append(streams, stream{URL: u, Headers: headers, Referrer: h.Referrer, HLS: h.HLS || strings.Contains(strings.ToLower(u), ".m3u8"), Subtitle: h.Subtitle, Subtitles: subs})
		}
		if err == nil && len(streams) == 0 {
			return failure("no_stream", "Provider returned no playable streams.", true)
		}
		data = map[string]any{"streams": streams}
	default:
		return failure("invalid_request", "Unknown operation.", false)
	}
	if ctx.Err() != nil {
		return contextFailure(ctx.Err())
	}
	if err != nil {
		if errors.Is(err, errSelectionRequired) {
			return failure("selection_required", "Interactive selection is unavailable in the spike.", false)
		}
		if errors.Is(err, providerhost.ErrRateLimited) || transport.rateLimited.Load() {
			return failure("rate_limited", "Provider rate limit reached. Try again later.", true)
		}
		return failure("provider_error", "Provider request failed. Try another source or audio mode.", true)
	}
	return encode(response{Version: 1, Data: data})
}

func validShowID(id string) bool { return strings.TrimSpace(id) != "" && len(id) <= 300 }

type requestTransport struct {
	ctx         context.Context
	base        http.RoundTripper
	rateLimited atomic.Bool
}

func (t *requestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.ctx.Err(); err != nil {
		return nil, err
	}
	resp, err := t.base.RoundTrip(req.Clone(t.ctx))
	if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
		t.rateLimited.Store(true)
	}
	return resp, err
}

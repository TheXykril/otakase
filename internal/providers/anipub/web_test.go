package anipub

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/thexykril/otakase/internal/providerhost"
	"github.com/thexykril/otakase/internal/providers/substyle"
)

// fakeWeb answers for every host the provider talks to from one test server,
// routed by "host/path". Unknown routes are 404s. It keeps the headers each
// route was last asked with.
type fakeWeb struct {
	mu      sync.Mutex
	routes  map[string]http.HandlerFunc
	headers map[string]http.Header
}

func newFakeWeb(t *testing.T) *fakeWeb {
	t.Helper()
	web := &fakeWeb{routes: map[string]http.HandlerFunc{}, headers: map[string]http.Header{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Host + r.URL.Path
		web.mu.Lock()
		route := web.routes[key]
		web.headers[key] = r.Header.Clone()
		web.mu.Unlock()
		if route == nil {
			http.NotFound(w, r)
			return
		}
		route(w, r)
	}))

	dialer := &net.Dialer{}
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, server.Listener.Addr().String())
		},
	}}

	previousClient, previousBase, previousMega := providerhost.HTTPClient, baseURL, megaplayBaseURL
	providerhost.HTTPClient = func() *http.Client { return client }
	baseURL, megaplayBaseURL = "http://anipub.test", "http://megaplay.test"
	t.Cleanup(func() {
		providerhost.HTTPClient, baseURL, megaplayBaseURL = previousClient, previousBase, previousMega
		server.Close()
	})
	return web
}

func (web *fakeWeb) text(key, body string) {
	web.routes[key] = func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }
}

func (web *fakeWeb) json(key string, v any) {
	web.routes[key] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
}

func (web *fakeWeb) status(key string, code int, body string) {
	web.routes[key] = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		_, _ = io.WriteString(w, body)
	}
}

func (web *fakeWeb) header(key, name string) string {
	web.mu.Lock()
	defer web.mu.Unlock()
	return web.headers[key].Get(name)
}

func (web *fakeWeb) asked(key string) bool {
	web.mu.Lock()
	defer web.mu.Unlock()
	_, ok := web.headers[key]
	return ok
}

// quietSubStyle keeps substyle from prompting or saving during a test.
func quietSubStyle(t *testing.T) {
	t.Helper()
	prompt, out, persist, current := providerhost.PromptSelect, providerhost.Out, providerhost.PersistSubStylePreference, providerhost.CurrentSubStyle
	providerhost.PromptSelect, providerhost.Out, providerhost.PersistSubStylePreference = nil, nil, nil
	providerhost.CurrentSubStyle = func() string { return "soft" }
	substyle.ResetForTest()
	t.Cleanup(func() {
		providerhost.PromptSelect, providerhost.Out, providerhost.PersistSubStylePreference, providerhost.CurrentSubStyle = prompt, out, persist, current
		substyle.ResetForTest()
	})
}

package internal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// MyAnimeList answers a rate-limited write with a redirect to /error.json, which
// never responds. Following it turned throttling into a minute-long hang and a
// failed launch, so a redirect from the API must surface as an error instead.
func TestMyAnimeListRequestDoesNotFollowRedirects(t *testing.T) {
	var blackholeHit bool

	blackhole := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		blackholeHit = true
		// Stand in for /error.json: accept the connection and never answer.
		time.Sleep(30 * time.Second)
	}))
	defer blackhole.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, blackhole.URL+"/error.json", http.StatusFound)
	}))
	defer api.Close()

	config := &Config{StoragePath: t.TempDir()}
	writeMyAnimeListTestToken(t, config)

	done := make(chan error, 1)
	go func() {
		var out map[string]any
		done <- myAnimeListRequest(config, http.MethodPut, api.URL+"/anime/1/my_list_status",
			map[string][]string{"num_watched_episodes": {"1"}}, &out)
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a redirect from the API must be reported as a failure")
		}
		if blackholeHit {
			t.Fatalf("the redirect was followed; that is what caused the hang: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("request hung instead of reporting the redirect")
	}
}

func TestMyAnimeListRedirectErrorIsLegible(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://api.myanimelist.net/error.json", http.StatusFound)
	}))
	defer api.Close()

	config := &Config{StoragePath: t.TempDir()}
	writeMyAnimeListTestToken(t, config)

	var out map[string]any
	err := myAnimeListRequest(config, http.MethodPut, api.URL+"/anime/1/my_list_status",
		map[string][]string{"num_watched_episodes": {"1"}}, &out)
	if err == nil {
		t.Fatal("expected an error")
	}
	// The status should be in the message so the cause is diagnosable.
	if !strings.Contains(err.Error(), "302") && !strings.Contains(strings.ToLower(err.Error()), "redirect") {
		t.Fatalf("error should name the redirect, got: %v", err)
	}
}

// writeMyAnimeListTestToken writes a token that will not be considered expired,
// so the request path is exercised rather than the login flow.
func writeMyAnimeListTestToken(t *testing.T, config *Config) {
	t.Helper()
	token := &AnilistToken{
		AccessToken:  "test-token",
		RefreshToken: "test-refresh",
		ExpiresAt:    time.Now().Add(24 * time.Hour),
	}
	if err := saveToken(myAnimeListTokenPath(config), token); err != nil {
		t.Fatalf("write token: %v", err)
	}
}

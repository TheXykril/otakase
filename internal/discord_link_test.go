package internal

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

func newDiscordTestConfig(t *testing.T, url string) *Config {
	t.Helper()
	return &Config{StoragePath: t.TempDir(), DiscordBotURL: url}
}

// stubDiscordOpen records the links that would have opened in a browser.
func stubDiscordOpen(t *testing.T) *[]string {
	t.Helper()
	var opened []string
	previous := discordOpenURL
	discordOpenURL = func(url string) error {
		opened = append(opened, url)
		return nil
	}
	t.Cleanup(func() { discordOpenURL = previous })
	return &opened
}

func shortDiscordPolls(t *testing.T) {
	t.Helper()
	previous := discordPollInterval
	discordPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { discordPollInterval = previous })
}

func TestDiscordLinkPollsUntilLinkedAndSavesToken(t *testing.T) {
	shortDiscordPolls(t)
	var mu sync.Mutex
	polls := 0
	heartbeats := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/link/start":
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("start: %s %s", r.Method, r.Header.Get("Content-Type"))
			}
			json.NewEncoder(w).Encode(map[string]any{"code": "ABCD-2345", "poll": "secret", "expires_in": 600})
		case "/v1/link/poll":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["poll"] != "secret" {
				t.Errorf("poll secret = %q", body["poll"])
			}
			mu.Lock()
			polls++
			n := polls
			mu.Unlock()
			if n < 3 {
				w.WriteHeader(http.StatusAccepted)
				json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"token": "tok123", "discord_name": "Xykril"})
		case "/v1/heartbeat":
			heartbeats <- r.Header.Get("Authorization")
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "discord_name": "Xykril"})
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	config := newDiscordTestConfig(t, srv.URL+"/")

	start, err := startDiscordLink(config)
	if err != nil {
		t.Fatal(err)
	}
	if start.Code != "ABCD-2345" {
		t.Fatalf("code = %q", start.Code)
	}
	link, err := waitForDiscordLink(t.Context(), config, start)
	if err != nil {
		t.Fatal(err)
	}
	if link.Token != "tok123" || link.DiscordName != "Xykril" {
		t.Fatalf("link = %+v", link)
	}
	if !discordLinked(config) {
		t.Fatal("not linked after a successful poll")
	}
	info, err := os.Stat(discordLinkPath(config))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("link file mode = %v, want 0600", info.Mode().Perm())
	}
	select {
	case auth := <-heartbeats:
		if auth != "Bearer tok123" {
			t.Errorf("heartbeat auth = %q", auth)
		}
	case <-time.After(2 * time.Second):
		t.Error("no heartbeat after linking")
	}
}

func TestDiscordLinkExpiredCode(t *testing.T) {
	shortDiscordPolls(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
		json.NewEncoder(w).Encode(map[string]string{"error": "expired"})
	}))
	defer srv.Close()
	config := newDiscordTestConfig(t, srv.URL)
	_, err := waitForDiscordLink(t.Context(), config, discordLinkStart{Code: "X", Poll: "p", ExpiresIn: 600})
	if !errors.Is(err, errDiscordLinkExpired) {
		t.Fatalf("err = %v, want expired", err)
	}
	if discordLinked(config) {
		t.Fatal("linked after an expired code")
	}
}

func TestDiscordLinkKeepsPollingThroughErrors(t *testing.T) {
	shortDiscordPolls(t)
	var mu sync.Mutex
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/heartbeat" {
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
			return
		}
		mu.Lock()
		polls++
		n := polls
		mu.Unlock()
		if n == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"token": "t", "discord_name": "A"})
	}))
	defer srv.Close()
	config := newDiscordTestConfig(t, srv.URL)
	link, err := waitForDiscordLink(t.Context(), config, discordLinkStart{Code: "X", Poll: "p", ExpiresIn: 600})
	if err != nil || link.Token != "t" {
		t.Fatalf("link = %+v, err = %v", link, err)
	}
}

func TestDiscordUnauthorizedForgetsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unknown token"})
	}))
	defer srv.Close()
	config := newDiscordTestConfig(t, srv.URL)
	if err := saveDiscordLink(config, discordLink{Token: "old", DiscordName: "Xykril"}); err != nil {
		t.Fatal(err)
	}
	err := sendDiscordHeartbeat(config, nil, 0)
	if !errors.Is(err, errDiscordUnauthorized) {
		t.Fatalf("err = %v, want unauthorized", err)
	}
	link := loadDiscordLink(config)
	if link.Token != "" || !link.Revoked {
		t.Fatalf("link after 401 = %+v, want forgotten and marked revoked", link)
	}
	if discordLinked(config) {
		t.Fatal("still linked after 401")
	}
}

func TestDiscordUnlinkRevokesAndForgets(t *testing.T) {
	auth := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/unlink" {
			t.Errorf("unexpected %s", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	config := newDiscordTestConfig(t, srv.URL)
	saveDiscordLink(config, discordLink{Token: "tok"})
	reached, err := unlinkDiscord(config)
	if err != nil || !reached {
		t.Fatalf("reached = %v, err = %v", reached, err)
	}
	if auth != "Bearer tok" {
		t.Errorf("auth = %q", auth)
	}
	if _, err := os.Stat(discordLinkPath(config)); !os.IsNotExist(err) {
		t.Errorf("link file still there: %v", err)
	}
}

func TestDiscordUnlinkUnreachableStillForgets(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	config := newDiscordTestConfig(t, url)
	saveDiscordLink(config, discordLink{Token: "tok"})
	reached, _ := unlinkDiscord(config)
	if reached {
		t.Error("reported the bot reached when it was down")
	}
	if loadDiscordLink(config).Token != "" {
		t.Error("token kept after unlinking")
	}
}

func TestDiscordDisabledSendsNothing(t *testing.T) {
	config := &Config{StoragePath: t.TempDir()}
	saveDiscordLink(config, discordLink{Token: "tok"})
	if discordLinked(config) {
		t.Error("linked with DiscordBotURL empty")
	}
	if err := sendDiscordHeartbeat(config, nil, 10); err != nil {
		t.Errorf("heartbeat with the features off: %v", err)
	}
	if _, err := startDiscordLink(config); err == nil {
		t.Error("link start worked with the features off")
	}
	if trackingCategoryEnabled(config, "COMMUNITY") {
		t.Error("Community offered with the features off")
	}
}

func TestCommunityMenuEntryFollowsDiscordBotURL(t *testing.T) {
	has := func(config *Config) bool {
		_, actions := mainMenuLayout(config, AnimeList{})
		for _, action := range actions {
			if action.Key == "COMMUNITY" {
				return true
			}
		}
		return false
	}
	if has(&Config{MenuActions: "STATS"}) {
		t.Error("Community shown with no DiscordBotURL")
	}
	if !has(&Config{MenuActions: "STATS", DiscordBotURL: DefaultDiscordBotURL}) {
		t.Error("Community missing with DiscordBotURL set")
	}
	if categoryIcon("COMMUNITY") == 0 {
		t.Error("Community has no icon")
	}
}

func TestDiscordRequestsTimeOut(t *testing.T) {
	if discordHTTPClient.Timeout <= 0 || discordHTTPClient.Timeout > 5*time.Second {
		t.Fatalf("discord timeout = %v, want at most 5s", discordHTTPClient.Timeout)
	}
}

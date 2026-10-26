package internal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubNotifications records desktop notifications instead of sending them.
func stubNotifications(t *testing.T) *[]string {
	t.Helper()
	var mu sync.Mutex
	var sent []string
	previous := notifySendRun
	notifySendRun = func(args []string) (string, error) {
		mu.Lock()
		sent = append(sent, args[len(args)-1])
		mu.Unlock()
		return "", nil
	}
	t.Cleanup(func() { notifySendRun = previous })
	return &sent
}

func TestDiscordInboxHandlesAndAcksEverything(t *testing.T) {
	stubNotifications(t)
	acked := make(chan []string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/inbox":
			if r.URL.Query().Get("wait") != "25" || r.Header.Get("Authorization") != "Bearer tok" {
				t.Errorf("inbox request %s %q", r.URL, r.Header.Get("Authorization"))
			}
			json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
				{"id": "a", "kind": "queue", "anilist_id": 5, "title": "Frieren", "from": "Mika"},
				{"id": "b", "kind": "notice", "title": "Support", "text": "A mod answered", "url": "https://discord.com/x"},
				{"id": "c", "kind": "from-the-future"},
				{"id": "d", "kind": "notice", "title": "Old", "expires": "2001-01-01T00:00:00Z"},
			}})
		case "/v1/inbox/ack":
			var body map[string][]string
			json.NewDecoder(r.Body).Decode(&body)
			acked <- body["ids"]
		}
	}))
	defer srv.Close()
	config := newDiscordTestConfig(t, srv.URL)
	if err := saveDiscordLink(config, discordLink{Token: "tok"}); err != nil {
		t.Fatal(err)
	}
	items, err := fetchDiscordInbox(config, discordInboxWait)
	if err != nil || len(items) != 4 {
		t.Fatalf("fetch: %v %+v", err, items)
	}
	handleDiscordInbox(config, items)
	if got := strings.Join(<-acked, ","); got != "a,b,c,d" {
		t.Errorf("acked %q", got)
	}
	kept := loadKeptInbox(config)
	if len(kept) != 2 || kept[0].ID != "b" || kept[1].ID != "a" {
		t.Errorf("kept %+v (newest first, expired and unknown left out)", kept)
	}
}

func TestKeptInboxMovesARequeuedShowUp(t *testing.T) {
	sent := stubNotifications(t)
	config := newDiscordTestConfig(t, "http://bot")
	keepInboxItem(config, discordInboxItem{ID: "1", Kind: "queue", AnilistID: 5, Title: "Frieren"})
	keepInboxItem(config, discordInboxItem{ID: "2", Kind: "notice", Title: "Hi"})
	keepInboxItem(config, discordInboxItem{ID: "3", Kind: "queue", AnilistID: 5, Title: "Frieren", From: "Mika"})
	kept := loadKeptInbox(config)
	if len(kept) != 2 || kept[0].ID != "3" || kept[1].ID != "2" {
		t.Errorf("kept %+v", kept)
	}
	if len(*sent) != 3 || !strings.Contains((*sent)[2], "Mika recommends Frieren") {
		t.Errorf("notifications %q", *sent)
	}
	if label := inboxRowLabel(config); label != "From Discord · 2" {
		t.Errorf("row %q", label)
	}
	if o := inboxOption(kept[0]); o.Key != "item:3" || o.Label != "Play Frieren · from Mika" {
		t.Errorf("option %+v", o)
	}
}

func TestDiscordSettingsOnlyAllowed(t *testing.T) {
	stubNotifications(t)
	dir := t.TempDir()
	previous := ConfigFilePath
	ConfigFilePath = filepath.Join(dir, "otakase.conf")
	t.Cleanup(func() { ConfigFilePath = previous })
	if err := SaveConfigToFile(ConfigFilePath, map[string]string{"DevBuilds": "false", "DiscordBotURL": "http://bot"}); err != nil {
		t.Fatal(err)
	}
	config := &Config{StoragePath: dir, DiscordBotURL: "http://bot"}
	applyDiscordSetting(config, discordInboxItem{Kind: "setting", Key: "DiscordBotURL", Value: "https://evil"})
	applyDiscordSetting(config, discordInboxItem{Kind: "setting", Key: "DevBuilds", Value: "true", Text: "Tester"})
	saved, err := LoadConfigFromFile(ConfigFilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !config.DevBuilds || saved["DevBuilds"] != "true" || saved["DiscordBotURL"] != "http://bot" || config.DiscordBotURL != "http://bot" {
		t.Errorf("config %+v saved %v", config.DevBuilds, saved)
	}
}

func TestDiscordInboxRetriesAfterFailure(t *testing.T) {
	stubNotifications(t)
	var mu sync.Mutex
	calls := 0
	got := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/inbox" {
			return
		}
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		switch {
		case n == 1:
			w.WriteHeader(http.StatusBadGateway)
		case n == 2:
			json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "x", "kind": "notice", "title": "Back"}}})
			close(got)
		default:
			time.Sleep(50 * time.Millisecond)
			json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		}
	}))
	defer srv.Close()
	config := newDiscordTestConfig(t, srv.URL)
	saveDiscordLink(config, discordLink{Token: "tok"})
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		runDiscordInbox(config, time.Millisecond, 5*time.Millisecond, stop)
	}()
	defer func() {
		close(stop)
		<-stopped
	}()
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("the reader did not try again")
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(loadKeptInbox(config)) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if kept := loadKeptInbox(config); len(kept) != 1 {
		t.Errorf("kept %+v", kept)
	}
	srv.Close()
}

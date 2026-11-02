package internal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscordClubNoneRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"club":null}`))
	}))
	t.Cleanup(srv.Close)
	config := newDiscordTestConfig(t, srv.URL)
	refreshDiscordClubIfStale(config)
	if _, ok := cachedDiscordClub(config); ok {
		t.Error("no club should show no row")
	}
	if c, ok := loadDiscordClub(config); !ok || c.FetchedAt.IsZero() || !strings.Contains(discordClubPath(config), discordClubFileName) {
		t.Errorf("cache = %+v %v", c, ok)
	}
}

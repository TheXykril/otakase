package internal

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiscordClubRowPlaysTheShow(t *testing.T) {
	var hits atomic.Int32
	deadline := time.Now().Add(48 * time.Hour).UTC()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/club" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		hits.Add(1)
		fmt.Fprintf(w, `{"club":{"anilist_id":154587,"title":"Frieren","episode":12,"deadline":%q,"caught_up":3,"joined":5}}`, deadline.Format(time.RFC3339))
	}))
	t.Cleanup(srv.Close)
	config := newDiscordTestConfig(t, srv.URL)

	if options := discordClubOptions(config, false); len(options) != 0 {
		t.Fatalf("club shown before any fetch: %v", options)
	}
	refreshDiscordClubIfStale(config)
	refreshDiscordClubIfStale(config)
	if hits.Load() != 1 {
		t.Fatalf("fetched %d times, want once", hits.Load())
	}
	options := discordClubOptions(config, false)
	want := "Server club: Frieren · ep 12 by " + deadline.Local().Format("2 Jan")
	if len(options) != 1 || options[0].Label != want {
		t.Fatalf("options = %+v", options)
	}

	if handled, _ := handleDiscordClub(config, "news"); handled {
		t.Error("handled another row's key")
	}
	if handled, play := handleDiscordClub(config, "club-play"); !handled || !play {
		t.Fatalf("club-play: handled %v play %v", handled, play)
	}
	if link, ok := takeOpenLink(); !ok || link.AnilistID != 154587 || link.Episode != 0 {
		t.Errorf("open link = %+v %v", link, ok)
	}

	// Past its deadline the row goes, even before the next fetch.
	c, _ := loadDiscordClub(config)
	c.Club.Deadline = time.Now().Add(-time.Minute)
	saveDiscordClub(config, c)
	if options := discordClubOptions(config, false); len(options) != 0 {
		t.Errorf("ended club still shown: %v", options)
	}
}

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

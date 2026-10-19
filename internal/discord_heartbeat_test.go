package internal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

func TestIsDevBuildVersion(t *testing.T) {
	cases := map[string]bool{
		"26.8.0-dev.3":     true,
		"v26.8.0-dev.12":   true,
		"26.1.0-dev.4+abc": true,
		"26.8.0":           false,
		"dev":              false,
		"":                 false,
	}
	for version, want := range cases {
		if got := isDevBuildVersion(version); got != want {
			t.Errorf("isDevBuildVersion(%q) = %v, want %v", version, got, want)
		}
	}
}

func TestHeartbeatPayloadSharesOnlyWhenOptedIn(t *testing.T) {
	watching := &heartbeatWatching{AnilistID: 154587, Title: "Frieren", Episode: 12, Episodes: 28}

	off := newHeartbeatPayload(&Config{}, watching, 57)
	if off.Watching != nil || off.ShareWatching {
		t.Errorf("share off sent %+v", off)
	}
	if off.WatchedSeconds != 57 || off.OS != runtime.GOOS || off.Version == "" {
		t.Errorf("payload = %+v", off)
	}

	on := newHeartbeatPayload(&Config{DiscordShareWatching: true}, watching, 57)
	if on.Watching == nil || on.Watching.Title != "Frieren" || !on.ShareWatching {
		t.Errorf("share on sent %+v", on)
	}

	data, _ := json.Marshal(newHeartbeatPayload(&Config{}, nil, -3))
	var raw map[string]any
	json.Unmarshal(data, &raw)
	if v, ok := raw["watching"]; !ok || v != nil {
		t.Errorf("watching should be sent as null, got %v (present %v)", v, ok)
	}
	if raw["watched_seconds"].(float64) != 0 {
		t.Errorf("negative seconds sent: %v", raw["watched_seconds"])
	}
}

func TestWatchMeterCountsOnlyPlayingTime(t *testing.T) {
	var m watchMeter
	t0 := time.Unix(1000, 0)
	m.observe(0, t0)
	m.observe(10, t0.Add(10*time.Second))  // playing: 10
	m.observe(10, t0.Add(20*time.Second))  // paused: 0
	m.observe(600, t0.Add(25*time.Second)) // seek forward: 0
	m.observe(620, t0.Add(35*time.Second)) // 2x speed: the 10s of wall time
	m.observe(100, t0.Add(40*time.Second)) // seek back: 0
	m.observe(102.5, t0.Add(42500*time.Millisecond))
	if got := m.take(); got != 22 {
		t.Fatalf("watched = %d, want 22", got)
	}
	// The half second is kept for the next heartbeat.
	m.observe(104, t0.Add(44*time.Second))
	if got := m.take(); got != 2 {
		t.Fatalf("watched after carry = %d, want 2", got)
	}
	m.restart()
	m.observe(0, t0.Add(50*time.Second))
	if got := m.take(); got != 0 {
		t.Fatalf("watched after restart = %d, want 0", got)
	}
}

type recordedHeartbeat struct {
	auth    string
	payload heartbeatPayload
}

func heartbeatServer(t *testing.T) (*httptest.Server, chan recordedHeartbeat) {
	t.Helper()
	beats := make(chan recordedHeartbeat, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/heartbeat" {
			t.Errorf("unexpected %s", r.URL.Path)
			return
		}
		var payload heartbeatPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("heartbeat body: %v", err)
		}
		beats <- recordedHeartbeat{auth: r.Header.Get("Authorization"), payload: payload}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "discord_name": "Renamed"})
	}))
	t.Cleanup(srv.Close)
	return srv, beats
}

func nextBeat(t *testing.T, beats chan recordedHeartbeat) recordedHeartbeat {
	t.Helper()
	select {
	case beat := <-beats:
		return beat
	case <-time.After(3 * time.Second):
		t.Fatal("no heartbeat")
	}
	return recordedHeartbeat{}
}

func TestDiscordWatchSessionHeartbeats(t *testing.T) {
	resetExitCleanupsForTest()
	t.Cleanup(resetExitCleanupsForTest)
	srv, beats := heartbeatServer(t)
	config := newDiscordTestConfig(t, srv.URL)
	config.DiscordShareWatching = true
	saveDiscordLink(config, discordLink{Token: "tok", DiscordName: "Xykril"})

	anime := &Anime{AnilistId: 154587, TotalEpisodes: 28}
	anime.Title.English = "Frieren"
	anime.Ep.Number = 12
	session := startDiscordWatch(config, anime)
	if session == nil {
		t.Fatal("no session for a linked app")
	}

	// The first position sends at once, without waiting the minute.
	session.observe(12, 100)
	first := nextBeat(t, beats)
	if first.auth != "Bearer tok" {
		t.Errorf("auth = %q", first.auth)
	}
	w := first.payload.Watching
	if w == nil || w.AnilistID != 154587 || w.Episode != 12 || w.Episodes != 28 || w.Title == "" {
		t.Fatalf("first heartbeat watching = %+v", w)
	}

	session.end()
	last := nextBeat(t, beats)
	if last.payload.Watching != nil {
		t.Errorf("closing heartbeat still watching: %+v", last.payload.Watching)
	}
	<-session.finished
	if loadDiscordLink(config).DiscordName != "Renamed" {
		t.Error("the name from the heartbeat answer was not kept")
	}
	// end and the exit cleanup both finish the session; only one closing
	// heartbeat goes out.
	session.finish()
	select {
	case extra := <-beats:
		t.Errorf("second closing heartbeat: %+v", extra)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestDiscordWatchSessionNilWhenNotLinked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("request while not linked: %s", r.URL.Path)
	}))
	defer srv.Close()
	config := newDiscordTestConfig(t, srv.URL)
	session := startDiscordWatch(config, &Anime{})
	if session != nil {
		t.Fatal("session started without a link")
	}
	// A nil session is safe to use, so call sites need no checks.
	session.observe(1, 10)
	session.end()
	StartDiscordMPVWatch(config, &Anime{}, "/tmp/nothing", make(chan struct{}))
}

func TestDiscordWatchSessionPeriodicHeartbeat(t *testing.T) {
	resetExitCleanupsForTest()
	t.Cleanup(resetExitCleanupsForTest)
	previous := discordHeartbeatInterval
	discordHeartbeatInterval = 20 * time.Millisecond
	t.Cleanup(func() { discordHeartbeatInterval = previous })

	srv, beats := heartbeatServer(t)
	config := newDiscordTestConfig(t, srv.URL)
	saveDiscordLink(config, discordLink{Token: "tok"})
	session := startDiscordWatch(config, &Anime{AnilistId: 1})
	// The closing heartbeat saves the name it is answered with, so it has to
	// land before the temporary storage directory is removed.
	defer func() {
		session.end()
		<-session.finished
	}()

	beat := nextBeat(t, beats)
	if beat.payload.Watching != nil {
		t.Errorf("watching sent with sharing off: %+v", beat.payload.Watching)
	}
}

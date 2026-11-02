package internal

import (
	"encoding/json"
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

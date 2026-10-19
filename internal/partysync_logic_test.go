package internal

import (
	"math"
	"testing"
	"time"
)

func TestPartyHostPositionAt(t *testing.T) {
	s := partyHostState{Episode: 1, Playing: true, Position: 100, SentAt: 10_000}
	if got := partyHostPositionAt(s, 11_500); math.Abs(got-101.5) > 1e-9 {
		t.Errorf("playing 1.5 s later: %v", got)
	}
	if got := partyHostPositionAt(s, 9_000); got != 100 {
		t.Errorf("a state from the future adds nothing: %v", got)
	}
	if got := partyHostPositionAt(s, 10_000+120_000); got != 100+partyMaxAge {
		t.Errorf("a stale state adds at most partyMaxAge: %v", got)
	}
	s.Playing = false
	if got := partyHostPositionAt(s, 20_000); got != 100 {
		t.Errorf("paused host stays put: %v", got)
	}
}

func TestDecidePartySync(t *testing.T) {
	const now = 1_000_000
	playing := partyHostState{AniListID: 1, Episode: 3, Playing: true, Position: 300, SentAt: now - 1000}
	settled := 10 * time.Second

	t.Run("in sync does nothing", func(t *testing.T) {
		a := decidePartySync(playing, partyLocal{Episode: 3, Loaded: true, Position: 300.5, SinceSeek: settled}, now)
		if a != (partyAction{}) {
			t.Errorf("%+v", a)
		}
	})
	t.Run("drift over 2 s seeks with latency compensated", func(t *testing.T) {
		a := decidePartySync(playing, partyLocal{Episode: 3, Loaded: true, Position: 290, SinceSeek: settled}, now)
		// The state is 1 s old, so the host is at 301; plus the seek lead.
		if !a.Seek || math.Abs(a.SeekTo-(301+partySeekLead)) > 1e-9 || a.OSD != "Synced to host" {
			t.Errorf("%+v", a)
		}
	})
	t.Run("drift just under the limit is left alone", func(t *testing.T) {
		a := decidePartySync(playing, partyLocal{Episode: 3, Loaded: true, Position: 299.1, SinceSeek: settled}, now)
		if a.Seek {
			t.Errorf("%+v", a)
		}
	})
	t.Run("no seek while the last one settles", func(t *testing.T) {
		a := decidePartySync(playing, partyLocal{Episode: 3, Loaded: true, Position: 250, SinceSeek: time.Second}, now)
		if a.Seek {
			t.Errorf("%+v", a)
		}
	})
	t.Run("host paused", func(t *testing.T) {
		h := playing
		h.Playing = false
		a := decidePartySync(h, partyLocal{Episode: 3, Loaded: true, Position: 300.2, SinceSeek: settled}, now)
		if !a.Pause || a.Play || a.Seek || a.OSD != "Host paused" {
			t.Errorf("%+v", a)
		}
		// A paused host's position doesn't move with time, and no lead is added.
		a = decidePartySync(h, partyLocal{Episode: 3, Loaded: true, Position: 280, SinceSeek: settled}, now)
		if !a.Pause || !a.Seek || a.SeekTo != 300 || a.OSD != "Host paused · Synced to host" {
			t.Errorf("%+v", a)
		}
	})
	t.Run("host resumed", func(t *testing.T) {
		a := decidePartySync(playing, partyLocal{Episode: 3, Loaded: true, Paused: true, Position: 301, SinceSeek: settled}, now)
		if !a.Play || a.Pause || a.OSD != "Host resumed" {
			t.Errorf("%+v", a)
		}
	})
	t.Run("episode switch", func(t *testing.T) {
		a := decidePartySync(playing, partyLocal{Episode: 2, Loaded: true, Position: 1000, SinceSeek: settled}, now)
		if !a.Switch || a.Seek || a.OSD != "Host switched to episode 3" {
			t.Errorf("%+v", a)
		}
	})
	t.Run("nothing until mpv is playing", func(t *testing.T) {
		a := decidePartySync(playing, partyLocal{Episode: 3, Loaded: false, SinceSeek: settled}, now)
		if a != (partyAction{}) {
			t.Errorf("%+v", a)
		}
	})
	t.Run("no host state yet", func(t *testing.T) {
		if a := decidePartySync(partyHostState{}, partyLocal{Episode: 1}, now); a != (partyAction{}) {
			t.Errorf("%+v", a)
		}
	})
}

func TestPartyShouldSend(t *testing.T) {
	t0 := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	last := partyHostState{AniListID: 1, Episode: 3, Playing: true, Position: 100}
	cases := []struct {
		name string
		cur  partyHostState
		at   time.Duration
		want bool
	}{
		{"steady playback", partyHostState{AniListID: 1, Episode: 3, Playing: true, Position: 102}, 2 * time.Second, false},
		{"heartbeat", partyHostState{AniListID: 1, Episode: 3, Playing: true, Position: 105}, 5 * time.Second, true},
		{"paused", partyHostState{AniListID: 1, Episode: 3, Playing: false, Position: 101}, time.Second, true},
		{"seek forward", partyHostState{AniListID: 1, Episode: 3, Playing: true, Position: 160}, time.Second, true},
		{"seek back", partyHostState{AniListID: 1, Episode: 3, Playing: true, Position: 90}, time.Second, true},
		{"episode", partyHostState{AniListID: 1, Episode: 4, Playing: true, Position: 0}, time.Second, true},
		{"show", partyHostState{AniListID: 2, Episode: 3, Playing: true, Position: 101}, time.Second, true},
		{"nothing picked yet", partyHostState{}, 10 * time.Second, false},
	}
	for _, c := range cases {
		if got := partyShouldSend(&last, t0, c.cur, t0.Add(c.at)); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
	if !partyShouldSend(nil, time.Time{}, last, t0) {
		t.Error("the first state is always sent")
	}
	paused := last
	paused.Playing = false
	if partyShouldSend(&paused, t0, paused, t0.Add(2*time.Second)) {
		t.Error("a paused host with nothing changing waits for the heartbeat")
	}
}

func TestPartyBackoff(t *testing.T) {
	want := []time.Duration{1, 2, 4, 8, 16, 30, 30, 30}
	for n, w := range want {
		if got := partyBackoff(n); got != w*time.Second {
			t.Errorf("attempt %d: %v", n, got)
		}
	}
}

func TestPartyClock(t *testing.T) {
	var c partyClock
	if got := c.serverNow(5000); got != 5000 {
		t.Errorf("no sample trusts the local clock: %d", got)
	}
	// The bot's clock is 10 s ahead; a 200 ms round trip.
	c.sample(1000, 1200, 11100)
	if got := c.serverNow(2000); got != 12000 {
		t.Errorf("offset: %d", got)
	}
	// A slower trip is a worse estimate and is ignored.
	c.sample(3000, 4000, 13900)
	if got := c.serverNow(2000); got != 12000 {
		t.Errorf("slow sample replaced a fast one: %d", got)
	}
	// A faster one refines it.
	c.sample(5000, 5020, 15020)
	if got := c.serverNow(2000); got != 12010 {
		t.Errorf("fast sample: %d", got)
	}
}

func TestParsePartyID(t *testing.T) {
	good := map[string]string{
		"1290000000000000001":    "1290000000000000001",
		" 1290000000000000001\n": "1290000000000000001",
		"https://otakase.xyverion.com/open/?party=1290000000000000001":    "1290000000000000001",
		"otakase://party?party=42":                                        "42",
		"otakase://party/1290000000000000001":                             "1290000000000000001",
		"https://discord.com/events/1111111111111111/1290000000000000001": "1290000000000000001",
	}
	for in, want := range good {
		if got, ok := parsePartyID(in); !ok || got != want {
			t.Errorf("%q: %q %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "frieren", "https://otakase.xyverion.com/open/", "12ab"} {
		if got, ok := parsePartyID(in); ok {
			t.Errorf("%q accepted as %q", in, got)
		}
	}
}

func TestPartyWSURL(t *testing.T) {
	cases := map[string]string{
		"https://bot.xyverion.com":        "wss://bot.xyverion.com/v1/party/42/ws",
		"https://bot.xyverion.com/":       "wss://bot.xyverion.com/v1/party/42/ws",
		"http://localhost:8100":           "ws://localhost:8100/v1/party/42/ws",
		"https://example.com/otakase-bot": "wss://example.com/otakase-bot/v1/party/42/ws",
	}
	for in, want := range cases {
		if got, err := partyWSURL(in, "42"); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	if _, err := partyWSURL("ftp://x", "42"); err == nil {
		t.Error("ftp accepted")
	}
}

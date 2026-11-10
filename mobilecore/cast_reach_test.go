package mobilecore

import (
	"testing"
	"time"

	"github.com/thexykril/otakase/internal/cast"
)

func shortGraces(t *testing.T) {
	t.Helper()
	fetch, idle := castNoFetchGrace, castIdleGrace
	castNoFetchGrace, castIdleGrace = 200*time.Millisecond, 400*time.Millisecond
	t.Cleanup(func() { castNoFetchGrace, castIdleGrace = fetch, idle })
}

func TestCastNeverFetchedGoesStraightToSource(t *testing.T) {
	if cast.LocalAddress() == "" {
		t.Skip("no LAN address in this environment")
	}
	shortGraces(t)
	c := NewCore()
	defer c.Close()
	device := &fakeDevice{}
	s := fakeSession(t, c, cast.KindChromecast, device)
	startOn(t, c, s, `"url":"https://cdn.example/ep.mp4","start":42,"title":"Ep 1"`)
	waitFor(t, "the device to be told", func() bool { _, n := device.last(); return n == 1 })
	waitFor(t, "the direct load", func() bool {
		status(t, c)
		_, n := device.last()
		return n == 2
	})
	media, _ := device.last()
	if media.URL != "https://cdn.example/ep.mp4" || media.Start != 42 {
		t.Fatalf("direct load %+v", media)
	}
	if st := status(t, c); st["state"] == "error" {
		t.Fatalf("direct load reported an error: %v", st)
	}
}

func TestCastNeverFetchedWithHeadersIsBlocked(t *testing.T) {
	if cast.LocalAddress() == "" {
		t.Skip("no LAN address in this environment")
	}
	shortGraces(t)
	c := NewCore()
	defer c.Close()
	device := &fakeDevice{}
	s := fakeSession(t, c, cast.KindChromecast, device)
	startOn(t, c, s, `"url":"https://cdn.example/ep.m3u8","hls":true,"headers":{"Referer":"https://site.example/"}`)
	waitFor(t, "the blocked error", func() bool {
		st := status(t, c)
		return st["state"] == "error" && st["blocked"] == true && st["error"] == castBlockedMessage
	})
	if _, n := device.last(); n != 1 {
		t.Fatalf("device told %d times, want 1", n)
	}
}

func TestCastIdleWithoutPlayingIsAnError(t *testing.T) {
	if cast.LocalAddress() == "" {
		t.Skip("no LAN address in this environment")
	}
	shortGraces(t)
	c := NewCore()
	defer c.Close()
	device := &fakeDevice{idle: true}
	s := fakeSession(t, c, cast.KindChromecast, device)
	startOn(t, c, s, `"url":"https://cdn.example/ep.mp4"`)
	waitFor(t, "the device to be told", func() bool { _, n := device.last(); return n == 1 })
	// The device fetched the stream, then gave up on it.
	s.mu.Lock()
	s.fetchCheck = false
	s.mu.Unlock()
	waitFor(t, "the error", func() bool {
		st := status(t, c)
		return st["state"] == "error" && st["blocked"] == nil
	})
}

func TestDirectOK(t *testing.T) {
	for _, tc := range []struct {
		kind cast.Kind
		mode string
		req  castRequest
		want bool
	}{
		{cast.KindChromecast, castRelay, castRequest{}, true},
		{cast.KindChromecast, castRelay, castRequest{Headers: map[string]string{"User-Agent": "x"}}, true},
		{cast.KindChromecast, castRelay, castRequest{Headers: map[string]string{"Referer": "x"}}, false},
		{cast.KindChromecast, castRelay, castRequest{Subtitle: "https://x/s.vtt"}, false},
		{cast.KindChromecast, castFFmpeg, castRequest{}, false},
		{cast.KindDLNA, castProgressive, castRequest{}, false},
		{cast.KindBrowser, castRelay, castRequest{}, false},
	} {
		if got := directOK(tc.kind, tc.mode, tc.req); got != tc.want {
			t.Errorf("directOK(%s, %s, %+v) = %v", tc.kind, tc.mode, tc.req, got)
		}
	}
}

func TestCastDiscoveryTakesInterface(t *testing.T) {
	c := NewCore()
	defer c.Close()
	defer cast.SetDiscoveryInterface("", 0)
	out := run(t, c, `{"version":1,"operation":"cast_devices","cast":{"timeout":0.2,"interface":{"name":"lo","index":1}}}`)
	if errorCode(out) != "" {
		t.Fatalf("discovery: %v", out)
	}
}

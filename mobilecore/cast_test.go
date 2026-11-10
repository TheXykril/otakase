package mobilecore

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/thexykril/otakase/internal/cast"
)

func run(t *testing.T, c *Core, input string) map[string]any {
	t.Helper()
	id := c.Start(input)
	if id == 0 {
		t.Fatal("no handle")
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(c.Result(id)), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func errorCode(out map[string]any) string {
	if e, ok := out["error"].(map[string]any); ok {
		return e["code"].(string)
	}
	return ""
}

func TestCastOperationsValidate(t *testing.T) {
	c := NewCore()
	defer c.Close()
	if out := run(t, c, `{"version":1,"operation":"cast_status"}`); out["data"].(map[string]any)["active"] != false {
		t.Fatalf("idle status: %v", out)
	}
	if code := errorCode(run(t, c, `{"version":1,"operation":"cast_start","cast":{"device":"nope","url":"https://a.example/x.m3u8"}}`)); code != "invalid_request" {
		t.Fatalf("unknown device: %s", code)
	}
	if code := errorCode(run(t, c, `{"version":1,"operation":"cast_start","cast":{"device":"x","url":"file:///etc/passwd"}}`)); code != "invalid_request" {
		t.Fatalf("file url: %s", code)
	}
	if code := errorCode(run(t, c, `{"version":1,"operation":"cast_control","cast":{"action":"pause"}}`)); code != "invalid_request" {
		t.Fatalf("control without cast: %s", code)
	}
	if code := errorCode(run(t, c, `{"version":1,"operation":"cast_nothing"}`)); code != "invalid_request" {
		t.Fatalf("unknown op: %s", code)
	}
	if out := run(t, c, `{"version":1,"operation":"cast_log"}`); errorCode(out) != "" {
		t.Fatalf("log: %v", out)
	}
}

func TestCastToBrowserPage(t *testing.T) {
	c := NewCore()
	defer c.Close()
	device := cast.BrowserDevice()
	if device.Addr == nil {
		t.Skip("no LAN address in this environment")
	}
	id := castDeviceID(device)
	c.caster.devices = map[string]cast.Device{id: device}
	out := run(t, c, `{"version":1,"operation":"cast_start","cast":{"device":"`+id+`","url":"https://cdn.example/master.m3u8","hls":true,"title":"Show 1","duration":1440}}`)
	if errorCode(out) != "" {
		t.Fatalf("start: %v", out)
	}
	var status map[string]any
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status = run(t, c, `{"version":1,"operation":"cast_status"}`)["data"].(map[string]any)
		if status["state"] == "loading" && status["link"] != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status["state"] != "loading" || !strings.HasPrefix(status["link"].(string), "http://") || status["duration"] != 1440.0 {
		t.Fatalf("status: %v", status)
	}
	if out := run(t, c, `{"version":1,"operation":"cast_control","cast":{"action":"stop"}}`); errorCode(out) != "" {
		t.Fatalf("stop: %v", out)
	}
	if status := run(t, c, `{"version":1,"operation":"cast_status"}`)["data"].(map[string]any); status["active"] != false {
		t.Fatalf("after stop: %v", status)
	}
}

func TestPickMode(t *testing.T) {
	hls := castRequest{HLS: true, URL: "https://a/x.m3u8"}
	subbed := castRequest{HLS: true, URL: "https://a/x.m3u8", Subtitle: "https://a/s.vtt"}
	mkv := castRequest{URL: "http://127.0.0.1:9/ep.mkv"}
	cases := []struct {
		kind   cast.Kind
		req    castRequest
		ffmpeg string
		want   string
	}{
		{cast.KindChromecast, hls, "", castRelay},
		{cast.KindChromecast, hls, "/usr/bin/ffmpeg", castRelay},
		{cast.KindChromecast, subbed, "/usr/bin/ffmpeg", castFFmpeg},
		{cast.KindChromecast, subbed, "", castRelay},
		{cast.KindChromecast, mkv, "/usr/bin/ffmpeg", castFFmpeg},
		{cast.KindDLNA, hls, "", castProgressive},
		{cast.KindDLNA, hls, "/usr/bin/ffmpeg", castFFmpeg},
		{cast.KindDLNA, mkv, "", castRelay},
		{cast.KindKodi, subbed, "/usr/bin/ffmpeg", castRelay},
		{cast.KindBrowser, subbed, "/usr/bin/ffmpeg", castRelay},
	}
	for _, tc := range cases {
		if got := pickMode(tc.kind, tc.req, tc.ffmpeg); got != tc.want {
			t.Errorf("%s %+v ffmpeg=%q: got %s want %s", tc.kind, tc.req, tc.ffmpeg, got, tc.want)
		}
	}
}

func TestSubtitlesToVTT(t *testing.T) {
	srt := "1\r\n00:00:01,500 --> 00:00:03,000\r\nHello\r\n"
	got, err := subtitlesToVTT([]byte(srt))
	if err != nil || string(got) != "WEBVTT\n\n1\n00:00:01.500 --> 00:00:03.000\nHello\n\n" {
		t.Fatalf("srt: %q %v", got, err)
	}
	ass := "[Script Info]\nTitle: x\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\nDialogue: 0,0:00:01.50,0:00:03.00,Default,,0,0,0,,{\\i1}Hi, <you>{\\i0}\\Nthere\n"
	got, err = subtitlesToVTT([]byte(ass))
	if err != nil || string(got) != "WEBVTT\n\n00:00:01.500 --> 00:00:03.000\nHi, &lt;you&gt;\nthere\n\n" {
		t.Fatalf("ass: %q %v", got, err)
	}
	vtt := "\uFEFFWEBVTT\n\n00:01.000 --> 00:02.000\nx\n"
	if got, _ := subtitlesToVTT([]byte(vtt)); !strings.HasPrefix(string(got), "WEBVTT") {
		t.Fatalf("vtt: %q", got)
	}
	if _, err := subtitlesToVTT([]byte("nothing here")); err == nil {
		t.Fatal("no cues accepted")
	}
	if subtitleExtension([]byte(ass)) != ".ass" || subtitleExtension([]byte(srt)) != ".srt" {
		t.Fatal("extension")
	}
}

func TestCastFirewallSaysWhetherItCanOpen(t *testing.T) {
	c := NewCore()
	defer c.Close()
	firewall := cast.Firewall
	defer func() { cast.Firewall = firewall }()
	for name, want := range map[string]bool{"ufw": true, "": false} {
		cast.Firewall = func() string { return name }
		out := run(t, c, `{"version":1,"operation":"cast_firewall"}`)
		if got := out["data"].(map[string]any)["can_allow"]; got != want {
			t.Fatalf("firewall %q: can_allow %v", name, got)
		}
	}
}

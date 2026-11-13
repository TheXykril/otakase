package mobilecore

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thexykril/otakase/internal/cast"
)

// fakeDevice records what it is told and reports a position.
type fakeDevice struct {
	mu       sync.Mutex
	played   []cast.Media
	seeks    []float64
	position float64
	idle     bool
	down     bool
}

func (f *fakeDevice) Play(url string) error { return f.PlayMedia(cast.Media{URL: url}) }
func (f *fakeDevice) PlayMedia(m cast.Media) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.played = append(f.played, m)
	return nil
}
func (f *fakeDevice) Progress() (cast.Progress, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return cast.Progress{}, errors.New("no answer")
	}
	return cast.Progress{Position: f.position, Idle: f.idle}, nil
}
func (f *fakeDevice) SeekToTime(s float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seeks = append(f.seeks, s)
	return nil
}
func (f *fakeDevice) Pause() error            { return nil }
func (f *fakeDevice) Unpause() error          { return nil }
func (f *fakeDevice) SetVolume(float64) error { return nil }
func (f *fakeDevice) Volume() float64         { return 0.5 }
func (f *fakeDevice) Stop() error             { return nil }
func (f *fakeDevice) last() (cast.Media, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.played) == 0 {
		return cast.Media{}, 0
	}
	return f.played[len(f.played)-1], len(f.played)
}

func fakeSession(t *testing.T, c *Core, kind cast.Kind, device *fakeDevice) *castSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s := &castSession{core: c, device: cast.Device{Name: "Test TV", UUID: "t", Kind: kind}, ctx: ctx, cancel: cancel, volume: -1, player: device}
	c.caster.session = s
	t.Cleanup(c.caster.closeCast)
	return s
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for " + what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func status(t *testing.T, c *Core) map[string]any {
	return run(t, c, `{"version":1,"operation":"cast_status"}`)["data"].(map[string]any)
}

func startOn(t *testing.T, c *Core, s *castSession, body string) {
	t.Helper()
	id := castDeviceID(s.device)
	c.caster.devices = map[string]cast.Device{id: s.device}
	if out := run(t, c, `{"version":1,"operation":"cast_start","cast":{"device":"`+id+`",`+body+`}}`); errorCode(out) != "" {
		t.Fatalf("start: %v", out)
	}
}

func TestCastDLNAJoinsPlaylistAndResumes(t *testing.T) {
	if cast.LocalAddress() == "" {
		t.Skip("no LAN address in this environment")
	}
	t.Setenv("PATH", "")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.m3u8":
			io.WriteString(w, "#EXTM3U\n#EXTINF:10,\na.ts\n#EXTINF:10,\nb.ts\n#EXT-X-ENDLIST\n")
		default:
			w.Write(make([]byte, 188))
		}
	}))
	defer upstream.Close()
	c := NewCore()
	defer c.Close()
	device := &fakeDevice{}
	s := fakeSession(t, c, cast.KindDLNA, device)
	startOn(t, c, s, `"url":"`+upstream.URL+`/index.m3u8","hls":true,"start":12,"title":"Ep 3"`)
	waitFor(t, "the device to be told", func() bool { _, n := device.last(); return n == 1 })
	media, _ := device.last()
	if !strings.Contains(media.URL, "/stream.ts?from=10.000") || media.ContentType != "video/mp2t" || media.Title != "Ep 3" {
		t.Fatalf("played %+v", media)
	}
	device.mu.Lock()
	device.position = 3
	device.mu.Unlock()
	waitFor(t, "duration and position", func() bool {
		st := status(t, c)
		return st["position"] == 13.0 && st["duration"] == 20.0 && st["state"] == "playing"
	})
	if note, ok := status(t, c)["note"]; ok {
		t.Fatalf("unexpected note %v", note)
	}
	resp, err := http.Get(media.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(body) != 188 {
		t.Fatalf("joined stream from 10s is %d bytes", len(body))
	}
	// A seek rebuilds the joined stream from the target.
	run(t, c, `{"version":1,"operation":"cast_control","cast":{"action":"seek","value":1}}`)
	waitFor(t, "the rebuilt stream", func() bool { _, n := device.last(); return n == 2 })
	if media, _ := device.last(); strings.Contains(media.URL, "from=") {
		t.Fatalf("seek to 1s played %s", media.URL)
	}
}

func TestCastKodiSeeksAfterStart(t *testing.T) {
	if cast.LocalAddress() == "" {
		t.Skip("no LAN address in this environment")
	}
	c := NewCore()
	defer c.Close()
	device := &fakeDevice{}
	s := fakeSession(t, c, cast.KindKodi, device)
	startOn(t, c, s, `"url":"https://cdn.example/ep.mp4","start":300`)
	waitFor(t, "the device to be told", func() bool { _, n := device.last(); return n == 1 })
	if media, _ := device.last(); !strings.HasSuffix(media.URL, "/video.mp4") || media.Start != 0 {
		t.Fatalf("played %+v", media)
	}
	device.mu.Lock()
	device.position = 1
	device.mu.Unlock()
	waitFor(t, "the resume seek", func() bool {
		status(t, c)
		device.mu.Lock()
		defer device.mu.Unlock()
		return len(device.seeks) == 1 && device.seeks[0] == 300
	})
	device.mu.Lock()
	device.idle = true
	device.mu.Unlock()
	waitFor(t, "the end", func() bool { return status(t, c)["state"] == "ended" })
}

func TestCastBurnsSubtitlesWithFFmpeg(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	if cast.LocalAddress() == "" {
		t.Skip("no LAN address in this environment")
	}
	src := t.TempDir()
	gen := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=320x240:rate=25:duration=12", "-f", "lavfi", "-i", "sine=duration=12",
		"-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-f", "hls", "-hls_time", "4", "-hls_playlist_type", "vod", filepath.Join(src, "index.m3u8"))
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg cannot make a test stream: %v %s", err, out)
	}
	os.WriteFile(filepath.Join(src, "subs.srt"), []byte("1\n00:00:00,500 --> 00:00:10,000\nHello\n"), 0o600)
	upstream := httptest.NewServer(http.FileServer(http.Dir(src)))
	defer upstream.Close()

	c := NewCore()
	defer c.Close()
	device := &fakeDevice{}
	s := fakeSession(t, c, cast.KindChromecast, device)
	startOn(t, c, s, `"url":"`+upstream.URL+`/index.m3u8","hls":true,"subtitle":"`+upstream.URL+`/subs.srt"`)
	waitFor(t, "the device to be told", func() bool {
		if st := status(t, c); st["state"] == "error" {
			t.Fatalf("cast failed: %v", st)
		}
		_, n := device.last()
		return n == 1
	})
	media, _ := device.last()
	if !strings.HasSuffix(media.URL, "/1/"+cast.PlaylistName) || media.ContentType != cast.HLSContentType {
		t.Fatalf("played %+v", media)
	}
	resp, err := http.Get(media.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "seg00000.ts") {
		t.Fatalf("playlist %s", body)
	}
	waitFor(t, "the length", func() bool { return status(t, c)["duration"].(float64) > 11 })
	// A seek rebuilds the stream from the target, and the position counts
	// from there.
	run(t, c, `{"version":1,"operation":"cast_control","cast":{"action":"seek","value":6}}`)
	waitFor(t, "the rebuilt stream", func() bool { _, n := device.last(); return n == 2 })
	if media, _ := device.last(); !strings.HasSuffix(media.URL, "/2/"+cast.PlaylistName) {
		t.Fatalf("seek played %s", media.URL)
	}
	device.mu.Lock()
	device.position = 2
	device.mu.Unlock()
	waitFor(t, "the offset position", func() bool { return status(t, c)["position"] == 8.0 })
}

func TestCastDeviceSilentForAWhilePlaysOn(t *testing.T) {
	c := NewCore()
	defer c.Close()
	device := &fakeDevice{position: 10}
	s := fakeSession(t, c, cast.KindDLNA, device)
	s.mu.Lock()
	s.state, s.seen, s.lastOK = "playing", true, time.Now().Add(-time.Minute)
	s.mu.Unlock()
	device.mu.Lock()
	device.down = true
	device.mu.Unlock()
	if got := status(t, c); got["state"] != "error" || got["error"] == nil {
		t.Fatalf("silent device: %v", got)
	}
	device.mu.Lock()
	device.down, device.position = false, 12
	device.mu.Unlock()
	got := status(t, c)
	if got["state"] != "playing" || got["error"] != nil || got["position"] != 12.0 {
		t.Fatalf("device back: %v", got)
	}
}

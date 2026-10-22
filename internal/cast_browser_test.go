package internal

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thexykril/otakase/internal/cast"
)

// The link is the same every run, so a TV can keep the page open.
func TestCastBrowserTokenIsKept(t *testing.T) {
	config := &Config{StoragePath: t.TempDir()}
	first := castBrowserToken(config)
	if first == "" || castBrowserToken(config) != first {
		t.Fatalf("token changed between runs: %q", first)
	}
	saved, err := os.ReadFile(filepath.Join(config.StoragePath, "cast-browser-token"))
	if err != nil || strings.TrimSpace(string(saved)) != first {
		t.Errorf("saved %q, %v", saved, err)
	}
}

// A press on the page reaches the episode as the same key would.
func TestCastBrowserCommandIsTheTerminalKey(t *testing.T) {
	castControls.mu.Lock()
	was := castControls.live
	live := make(chan castCommand, 8)
	castControls.live = live
	castControls.mu.Unlock()
	defer func() {
		castControls.mu.Lock()
		castControls.live = was
		castControls.mu.Unlock()
	}()

	for name, want := range map[string]castCommand{
		"toggle": castCmdPauseToggle, "back": castCmdSeekBack, "forward": castCmdSeekForward,
		"volup": castCmdVolumeUp, "voldown": castCmdVolumeDown, "stop": castCmdStop,
	} {
		castBrowserCommand(name)
		if got := <-live; got != want {
			t.Errorf("%s arrived as %d, want %d", name, got, want)
		}
	}
	castBrowserCommand("nonsense")
	select {
	case got := <-live:
		t.Errorf("an unknown press arrived as %d", got)
	default:
	}
}

// Without a terminal, a browser cast still takes its page's presses.
func TestCastControlsTakeTheBrowserWithoutATerminal(t *testing.T) {
	castControls.mu.Lock()
	begun, usable := castControls.begun, castControls.usable
	castControls.begun, castControls.usable = true, false
	castControls.mu.Unlock()
	defer func() {
		castControls.mu.Lock()
		castControls.begun, castControls.usable = begun, usable
		castControls.mu.Unlock()
	}()

	if _, release, ok := startCastControls(&Config{}); ok {
		release()
		t.Fatal("controls with neither a terminal nor a browser")
	}
	castRemoteControls.Store(true)
	defer castRemoteControls.Store(false)
	commands, release, ok := startCastControls(&Config{})
	defer release()
	if !ok || commands == nil {
		t.Fatal("a browser cast took no presses")
	}
}

func TestCastBrowserSoftSubtitlesSetting(t *testing.T) {
	for value, want := range map[string]bool{"": true, "soft": true, "burn": false, " Burn ": false} {
		if got := castBrowserSoftSubtitles(&Config{CastBrowserSubtitles: value}); got != want {
			t.Errorf("CastBrowserSubtitles=%q: soft %t, want %t", value, got, want)
		}
	}
}

func TestCastBrowserNamed(t *testing.T) {
	for name, want := range map[string]bool{"Browser": true, " browser ": true, "Living Room TV": false, "": false} {
		if got := castBrowserNamed(name); got != want {
			t.Errorf("castBrowserNamed(%q) = %t", name, got)
		}
	}
}

// CastDevice=Browser casts there without looking for devices at all.
func TestCastPickDeviceBrowserByName(t *testing.T) {
	defer func(was string) { castSessionDevice = was }(castSessionDevice)
	config := &Config{CastBrowser: true, CastDevice: "Browser", StoragePath: t.TempDir()}
	device, rescan, err := castPickDevice(config)
	if err != nil || rescan || !castIsBrowser(device) {
		t.Fatalf("got %+v, rescan %t, err %v", device, rescan, err)
	}
}

// A provider's ASS file is served to the browser as WebVTT.
func TestCastBrowserSubtitleFileConvertsToWebVTT(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	const ass = "[Script Info]\nScriptType: v4.00+\n\n[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\nStyle: Default,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,2,0,2,10,10,10,1\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\nDialogue: 0,0:00:01.00,0:00:03.50,Default,,0,0,0,,{\\i1}Hello{\\i0} there\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(ass))
	}))
	defer server.Close()

	dir := t.TempDir()
	name, err := castBrowserSubtitleFile(ffmpeg, server.URL+"/episode.ass", "", dir)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	vtt := string(body)
	if !strings.HasPrefix(vtt, "WEBVTT") || !strings.Contains(vtt, "00:01.000 --> 00:03.500") || !strings.Contains(vtt, "there") {
		t.Errorf("not WebVTT with the cue:\n%s", vtt)
	}
}

// The code to scan is drawn at full strength among dimmed text.
func TestCastPanelStatusDrawsTheCode(t *testing.T) {
	message := castBrowserLinkMessage("http://192.168.1.20:8012/abcde", true)
	lines := castPanelStatusLines("Show", 1, "Browser", message, 80, "")
	code := 0
	for _, line := range lines {
		if strings.Contains(line, "█") {
			code++
		}
	}
	if code < 10 {
		t.Errorf("only %d lines of the code drawn:\n%s", code, strings.Join(lines, "\n"))
	}
	if !strings.Contains(strings.Join(lines, "\n"), "http://192.168.1.20:8012/abcde") {
		t.Error("the link itself is not shown")
	}
	if strings.Contains(castBrowserLinkMessage("http://x/y", false), "█") {
		t.Error("a code drawn where there is no panel to show it")
	}
}

func TestCastPlayTellsTheBrowserWhereTheStreamStarts(t *testing.T) {
	recorder := &recordingPlayer{}
	if err := castPlay(recorder, "http://x/g0/playlist.m3u8", 42); err != nil || recorder.played != "http://x/g0/playlist.m3u8" {
		t.Errorf("a receiver was not simply played: %q, %v", recorder.played, err)
	}
}

type recordingPlayer struct {
	cast.Player
	played string
}

func (r *recordingPlayer) Play(url string) error { r.played = url; return nil }

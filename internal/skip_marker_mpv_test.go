package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// startTestMPV runs a real player on a generated tone, which is enough for
// time-pos and duration to mean something.
func startTestMPV(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("this drives a unix socket")
	}
	if _, err := exec.LookPath("mpv"); err != nil {
		t.Skip("mpv is not installed")
	}

	socket := filepath.Join(t.TempDir(), "mpv.sock")
	cmd := exec.Command("mpv",
		"--vo=null", "--ao=null", "--idle=yes", "--really-quiet",
		"--input-ipc-server="+socket,
		"av://lavfi:sine=duration=600")
	if err := cmd.Start(); err != nil {
		t.Skipf("could not start mpv: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socket); err == nil {
			if length, err := mpvFloatProperty(socket, "duration"); err == nil && length > 0 {
				return socket
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Skip("mpv did not come up in time")
	return ""
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// The whole feature against a real player: the keys are bound in mpv, pressing
// them reaches this program, the positions come from the player, and nothing is
// sent until the second press of alt+s.
func TestMarkingAndSubmittingThroughARealPlayer(t *testing.T) {
	socket := startTestMPV(t)

	var mu sync.Mutex
	received := []aniSkipSubmitRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var submission aniSkipSubmitRequest
		_ = json.Unmarshal(body, &submission)
		mu.Lock()
		received = append(received, submission)
		mu.Unlock()
		fmt.Fprint(w, `{"statusCode":201,"message":"Created","skipId":"new-id"}`)
	}))
	t.Cleanup(server.Close)
	restore := aniSkipWriteBaseForTest(t, server.URL)
	defer restore()

	anime := &Anime{MalId: 52991}
	anime.Ep.Number = 1
	previous := GetGlobalAnime()
	SetGlobalAnime(anime)
	t.Cleanup(func() { SetGlobalAnime(previous) })

	done := make(chan struct{})
	defer close(done)

	activeSkipMarkerMu.Lock()
	activeSkipMarker = nil
	activeSkipMarkerMu.Unlock()

	StartSkipMarker(&Config{ContributeSkipTimes: true, StoragePath: t.TempDir()},
		anime, socket, SkipIDs{}, done)

	press := func(key string) {
		t.Helper()
		if _, err := MPVSendCommand(socket, []interface{}{"keypress", key}); err != nil {
			t.Fatalf("pressing %s: %v", key, err)
		}
	}
	seek := func(seconds float64) {
		t.Helper()
		if _, err := MPVSendCommand(socket, []interface{}{"seek", seconds, "absolute"}); err != nil {
			t.Fatalf("seeking to %v: %v", seconds, err)
		}
		waitFor(t, fmt.Sprintf("the player to reach %vs", seconds), func() bool {
			position, err := mpvFloatProperty(socket, "time-pos")
			return err == nil && position >= seconds-1
		})
	}

	marker := func() *skipMarker {
		activeSkipMarkerMu.Lock()
		defer activeSkipMarkerMu.Unlock()
		return activeSkipMarker
	}
	if marker() == nil {
		t.Fatal("the marker did not take charge of the player")
	}

	seek(10)
	press("Alt+o")
	waitFor(t, "the opening's start to be marked", func() bool {
		m := marker()
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.opStart != nil
	})

	seek(100)
	press("Alt+o")
	waitFor(t, "the opening's end to be marked", func() bool {
		m := marker()
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.opEnd != nil
	})

	// The first alt+s asks rather than sends.
	press("Alt+s")
	waitFor(t, "the confirmation to be armed", func() bool {
		m := marker()
		m.mu.Lock()
		defer m.mu.Unlock()
		return !m.confirmAt.IsZero()
	})
	mu.Lock()
	sentEarly := len(received)
	mu.Unlock()
	if sentEarly != 0 {
		t.Fatalf("one keypress published %d skip times", sentEarly)
	}

	press("Alt+s")
	waitFor(t, "the submission to be sent", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(received) > 0
	})

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("sent %d submissions", len(received))
	}
	sent := received[0]
	if sent.SkipType != "op" {
		t.Errorf("filed as %q", sent.SkipType)
	}
	if sent.StartTime < 9 || sent.StartTime > 12 {
		t.Errorf("the start was taken as %vs, not where the viewer was", sent.StartTime)
	}
	if sent.EndTime < 99 || sent.EndTime > 102 {
		t.Errorf("the end was taken as %vs", sent.EndTime)
	}
	// The length has to be the player's own, whatever it says: AniSkip records
	// it with every entry, and a length invented here would describe a
	// different cut of the episode than the one that was watched.
	length, err := mpvFloatProperty(socket, "duration")
	if err != nil {
		t.Fatalf("reading the duration back: %v", err)
	}
	if difference := sent.EpisodeLength - length; difference > 2 || difference < -2 {
		t.Errorf("submitted a length of %v while the player says %v", sent.EpisodeLength, length)
	}
}

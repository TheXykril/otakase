package internal

import (
	"errors"
	"testing"
	"time"
)

func TestPlaybackStartWaitIsImmediateWithoutIPC(t *testing.T) {
	// A full timeout: if either case actually polled, the test would hang.
	wait := MpvPlaybackStartTimeoutDuration(nil)
	for _, socket := range []string{androidIntentSocket, ""} {
		if !WaitForMPVPlaybackStart(socket, wait) {
			t.Errorf("socket %q: want an immediate true", socket)
		}
	}
}

func TestPlaybackStartTimeoutFromConfig(t *testing.T) {
	if got := MpvPlaybackStartTimeoutDuration(nil); got != MpvStartWaitSeconds*time.Second {
		t.Errorf("nil config: %s", got)
	}
	for raw, want := range map[string]time.Duration{
		"45": 45 * time.Second,
		"0":  MpvStartWaitSeconds * time.Second,
	} {
		cfg := PopulateConfig(map[string]string{"MpvPlaybackStartTimeout": raw})
		if got := MpvPlaybackStartTimeoutDuration(&cfg); got != want {
			t.Errorf("MpvPlaybackStartTimeout=%s: got %s, want %s", raw, got, want)
		}
	}
}

func TestDefaultEpisodeTitle(t *testing.T) {
	show := &Anime{Title: AnimeTitle{English: "Show"}, Ep: Episode{Number: 7}}
	if got := defaultEpisodeTitle(show); got != "Show - Episode 7" {
		t.Fatalf("got %q", got)
	}
}

func TestMPVConnectionGoneErrors(t *testing.T) {
	gone := []string{
		"dial unix /tmp/otakase: connect: connection refused",
		"dial unix /tmp/otakase: connect: no such file or directory",
		`open \\.\pipe\otakase: The system cannot find the file specified.`,
		`read \\.\pipe\otakase: The pipe has been ended.`,
		`write \\.\pipe\otakase: The pipe is being closed.`,
		`read \\.\pipe\otakase: No process is on the other end of the pipe.`,
	}
	for _, msg := range gone {
		if !mpvConnectionLost(errors.New(msg)) {
			t.Errorf("%q should mean mpv is gone", msg)
		}
	}
	// A slow reply is not a dead player.
	if mpvConnectionLost(errors.New("i/o timeout")) {
		t.Error("i/o timeout taken for a closed connection")
	}
}

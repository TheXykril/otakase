package internal

import (
	"errors"
	"testing"
	"time"
)

func TestWaitForMPVPlaybackStartSkipsNonIPCPlayers(t *testing.T) {
	timeout := MpvPlaybackStartTimeoutDuration(nil)
	if !WaitForMPVPlaybackStart("android-intent", timeout) {
		t.Fatal("expected android-intent playback wait to succeed immediately")
	}
	if !WaitForMPVPlaybackStart("", timeout) {
		t.Fatal("expected empty socket playback wait to succeed immediately")
	}
}

func TestMpvPlaybackStartTimeoutDurationUsesConfig(t *testing.T) {
	config := PopulateConfig(map[string]string{
		"MpvPlaybackStartTimeout": "45",
	})
	if got := MpvPlaybackStartTimeoutDuration(&config); got != 45*time.Second {
		t.Fatalf("expected 45s timeout, got %s", got)
	}
}

func TestIsMPVConnectionGoneError(t *testing.T) {
	for _, message := range []string{
		"dial unix /tmp/otakase: connect: connection refused",
		"dial unix /tmp/otakase: connect: no such file or directory",
		"open \\\\.\\pipe\\otakase: The system cannot find the file specified.",
		"read \\\\.\\pipe\\otakase: The pipe has been ended.",
		"write \\\\.\\pipe\\otakase: The pipe is being closed.",
		"read \\\\.\\pipe\\otakase: No process is on the other end of the pipe.",
	} {
		if !isMPVConnectionGoneError(errors.New(message)) {
			t.Fatalf("expected gone error for %q", message)
		}
	}

	if isMPVConnectionGoneError(errors.New("i/o timeout")) {
		t.Fatal("did not expect timeout to be treated as closed connection")
	}
}

package internal

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/mpv"
)

const (
	mpvCommandAttempts      = 3
	mpvCommandRetryDelay    = 100 * time.Millisecond
	mpvPlaybackPollInterval = 500 * time.Millisecond
)

// ErrMPVNoSocket reports that no MPV IPC socket is available. Callers treat it
// like a closed connection: MPV is not reachable, so polling must stop.
var ErrMPVNoSocket = mpv.ErrNoSocket

var mpvCommand = mpv.Command

// MPVSendCommand sends one command to mpv, trying again a couple of times when
// the socket hiccups. An empty socket path and an unavailable property are
// answers, not hiccups, and return at once: retrying an empty path is what
// once turned a failed launch into a loop logging thousands of lines a second.
func MPVSendCommand(socket string, command []interface{}) (interface{}, error) {
	var err error
	for attempt := 1; attempt <= mpvCommandAttempts; attempt++ {
		var data interface{}
		if data, err = mpvCommand(socket, command...); err == nil {
			return data, nil
		}
		if errors.Is(err, mpv.ErrNoSocket) || mpv.IsUnavailable(err) {
			return nil, err
		}
		Log(fmt.Sprintf("MPV command error (attempt %d/%d): %v", attempt, mpvCommandAttempts, err))
		if attempt < mpvCommandAttempts {
			time.Sleep(mpvCommandRetryDelay)
		}
	}
	return nil, fmt.Errorf("command failed after %d attempts: %w", mpvCommandAttempts, err)
}

// MPVConnectionGone reports that the MPV session is over: the socket is missing,
// closed, or refusing connections. Polling loops must stop when it returns true,
// or they spin forever logging the same error.
func MPVConnectionGone(err error) bool {
	return mpv.IsGone(err)
}

func mpvConnectionLost(err error) bool {
	return mpv.IsGone(err)
}

type mpvCommandSender func(string, []interface{}) (interface{}, error)

// mpvNumber reads a numeric property, or -1 when the value is not a number.
// JSON numbers always decode as float64.
func mpvNumber(value interface{}) float64 {
	if number, ok := value.(float64); ok {
		return number
	}
	return -1
}

func SeekMPV(socket string, seconds int) (interface{}, error) {
	return MPVSendCommand(socket, []interface{}{"seek", seconds, "absolute"})
}

func ExitMPV(socket string) error {
	stopFlixPlaybackRelay()
	_, err := MPVSendCommand(socket, []interface{}{"quit"})
	if err != nil {
		Log("Error closing MPV: " + err.Error())
	}
	return err
}

func GetMPVPausedStatus(socket string) (bool, error) {
	value, err := MPVSendCommand(socket, []interface{}{"get_property", "pause"})
	if err != nil {
		return false, err
	}
	paused, _ := value.(bool)
	return paused, nil
}

func GetMPVPlaybackSpeed(socket string) (float64, error) {
	value, err := MPVSendCommand(socket, []interface{}{"get_property", "speed"})
	if err != nil {
		Log("Failed to get playback speed.")
		return 0, err
	}
	speed, _ := value.(float64)
	return speed, nil
}

func PercentageWatched(position, duration int) float64 {
	if duration <= 0 {
		return 0
	}
	return float64(position) / float64(duration) * 100
}

// HasActivePlayback reports whether mpv has a file playing. An unavailable
// time-pos means nothing is loaded, which is an answer rather than an error.
func HasActivePlayback(socket string) (bool, error) {
	position, err := MPVSendCommand(socket, []interface{}{"get_property", "time-pos"})
	if mpv.IsUnavailable(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check playback status: %w", err)
	}
	return position != nil, nil
}

// IsMPVRunning reports whether an mpv answers on the socket.
func IsMPVRunning(socket string) bool {
	if strings.TrimSpace(socket) == "" {
		return false
	}
	_, err := MPVSendCommand(socket, []interface{}{"get_property", "pid"})
	return err == nil
}

// clearMPVSubtitleFiles empties mpv's global external subtitle list, which the
// --sub-file this instance was started with filled. Call it before loading
// another episode into a running instance, then sub-add that episode's own.
func clearMPVSubtitleFiles(send mpvCommandSender, ipcSocketPath string) {
	if _, err := send(ipcSocketPath, []interface{}{"change-list", "sub-files", "clr", ""}); err != nil {
		Log(fmt.Sprintf("Failed to clear the previous subtitle files: %v", err))
	}
}

// resetMPVStreamHeaders makes a running instance send this episode's extra
// HTTP headers and no others.
//
// Like --sub-file, the headers an instance was started with are global: they go
// with every file loaded after, so the next episode -- possibly from another
// provider, whose CDN wants different ones or none -- was requested with the
// first episode's. Call it before loading another episode into a running
// instance, and only when the user has not set headers of their own.
func resetMPVStreamHeaders(send mpvCommandSender, ipcSocketPath string, headers map[string]string) {
	if _, err := send(ipcSocketPath, []interface{}{"change-list", "http-header-fields", "clr", ""}); err != nil {
		Log(fmt.Sprintf("Failed to clear the previous stream headers: %v", err))
		return
	}
	for _, line := range streamHeaderLines(headers) {
		if _, err := send(ipcSocketPath, []interface{}{"change-list", "http-header-fields", "append", line}); err != nil {
			Log(fmt.Sprintf("Failed to set a stream header: %v", err))
		}
	}
}

func waitForMPVFileReady(socket, expectedPath string, timeout time.Duration) error {
	return waitForMPVFileReadyWith(MPVSendCommand, socket, expectedPath, timeout, 100*time.Millisecond)
}

// waitForMPVFileReadyWith polls until mpv has opened expectedPath (any file
// when it is empty) far enough to report a duration or a position.
func waitForMPVFileReadyWith(send mpvCommandSender, socket, expectedPath string, timeout, poll time.Duration) error {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if poll <= 0 {
		poll = 100 * time.Millisecond
	}
	var lastErr error
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(poll) {
		value, err := send(socket, []interface{}{"get_property", "path"})
		if err != nil {
			lastErr = err
			continue
		}
		path, _ := value.(string)
		if strings.TrimSpace(path) == "" || (expectedPath != "" && path != expectedPath) {
			continue
		}
		if duration, err := send(socket, []interface{}{"get_property", "duration"}); err == nil && mpvNumber(duration) > 0 {
			return nil
		}
		if position, err := send(socket, []interface{}{"get_property", "time-pos"}); err == nil && mpvNumber(position) >= 0 {
			return nil
		}
	}
	if lastErr != nil {
		return fmt.Errorf("MPV file did not become ready: %w", lastErr)
	}
	return errors.New("MPV file did not become ready before timeout")
}

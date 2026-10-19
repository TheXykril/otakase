// Package mpv talks to a running mpv over its JSON IPC protocol.
//
// Every command opens its own connection. mpv also writes events to every
// connection, so replies are matched to their request by request_id and
// anything else on the line is skipped.
package mpv

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// ErrNoSocket is returned for an empty socket path. Nothing can be reached
// there, so callers should treat it like a player that has exited.
var ErrNoSocket = errors.New("no MPV IPC socket")

// Error is an error mpv itself answered a command with, such as
// "property unavailable".
type Error string

func (e Error) Error() string { return "mpv command error: " + string(e) }

// ReplyTimeout bounds the whole exchange of one command.
const ReplyTimeout = 3 * time.Second

var lastRequestID atomic.Int64

type request struct {
	Command   []any `json:"command"`
	RequestID int64 `json:"request_id"`
}

type message struct {
	Event     string  `json:"event"`
	RequestID *int64  `json:"request_id"`
	Error     *string `json:"error"`
	Data      any     `json:"data"`
}

// Command sends one command and returns the data mpv answered with, which is
// nil for commands that answer with none.
func Command(path string, command ...any) (any, error) {
	if strings.TrimSpace(path) == "" {
		return nil, ErrNoSocket
	}
	conn, err := Dial(path)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(ReplyTimeout))

	id := lastRequestID.Add(1)
	payload, err := json.Marshal(request{Command: command, RequestID: id})
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		var msg message
		if json.Unmarshal(scanner.Bytes(), &msg) != nil || msg.Event != "" {
			continue
		}
		if msg.RequestID != nil && *msg.RequestID != id {
			continue
		}
		if msg.RequestID == nil && msg.Error == nil {
			continue
		}
		return msg.result()
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	return nil, errors.New("read: connection closed before mpv answered")
}

func (m message) result() (any, error) {
	if m.Error != nil && *m.Error != "" && *m.Error != "success" {
		return nil, Error(*m.Error)
	}
	return m.Data, nil
}

// goneMessages are the ways a dial or read fails once mpv is not there,
// on Unix sockets and on Windows named pipes.
var goneMessages = []string{
	"connect: connection refused",
	"connect: no such file or directory",
	"cannot find the file specified",
	"pipe has been ended",
	"pipe is being closed",
	"no process is on the other end of the pipe",
	"missing address",
}

// IsGone reports whether err means mpv has exited or never started.
func IsGone(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNoSocket) {
		return true
	}
	text := strings.ToLower(err.Error())
	for _, gone := range goneMessages {
		if strings.Contains(text, gone) {
			return true
		}
	}
	return false
}

// IsUnavailable reports whether mpv answered that a property has no value
// right now, which happens between files and before playback starts.
func IsUnavailable(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "property unavailable")
}

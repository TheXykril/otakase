// Package stdio serves the mobile core over a pipe for the desktop app.
//
// The desktop app starts otakase-core as a helper process and speaks
// newline-delimited JSON with it. Each request carries the same JSON the
// Android binding passes to mobilecore.Core.Start:
//
//	app -> core  {"id":7,"op":"request","input":{"version":1,"operation":"search",...}}
//	app -> core  {"id":7,"op":"cancel"}
//	app -> core  {"op":"stop_playback"}
//	core -> app  {"event":"ready","version":1}
//	core -> app  {"id":7,"result":{"version":1,"data":{...}}}
//
// Results arrive in completion order, not request order. Every request id
// gets exactly one result line, including after a cancel. Closing the input
// cancels all outstanding work and ends Serve.
package stdio

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

// ProtocolVersion is announced in the ready event.
const ProtocolVersion = 1

// maxLine bounds one incoming message. Core requests are limited to 16 KiB.
const maxLine = 64 << 10

// Backend is the part of mobilecore.Core the server needs.
type Backend interface {
	Start(input string) int64
	Result(id int64) string
	Cancel(id int64)
	StopPlayback()
	Close()
}

type message struct {
	ID    int64           `json:"id"`
	Op    string          `json:"op"`
	Input json.RawMessage `json:"input"`
}

type reply struct {
	ID     int64           `json:"id,omitempty"`
	Event  string          `json:"event,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	// Version is only set on the ready event.
	Version int `json:"version,omitempty"`
}

type server struct {
	core     Backend
	writeMu  sync.Mutex
	out      *bufio.Writer
	writeErr error

	mu      sync.Mutex
	pending map[int64]int64 // request id -> core handle
	wg      sync.WaitGroup
}

// Serve reads requests from r and writes replies to w until r ends. It
// closes core before returning and waits for every outstanding result.
func Serve(core Backend, r io.Reader, w io.Writer) error {
	s := &server{core: core, out: bufio.NewWriter(w), pending: make(map[int64]int64)}
	s.write(reply{Event: "ready", Version: ProtocolVersion})

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), maxLine)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		s.handle(line)
	}
	err := scanner.Err()
	core.Close()
	s.wg.Wait()
	if errors.Is(err, bufio.ErrTooLong) {
		return errors.New("stdio: message exceeds 64 KiB")
	}
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.writeErr
}

func (s *server) handle(line []byte) {
	var m message
	if err := json.Unmarshal(line, &m); err != nil {
		s.fail(0, "invalid_request", "Invalid JSON message.")
		return
	}
	switch m.Op {
	case "request":
		s.start(m)
	case "cancel":
		s.mu.Lock()
		handle, ok := s.pending[m.ID]
		s.mu.Unlock()
		if ok {
			s.core.Cancel(handle)
		}
	case "stop_playback":
		s.core.StopPlayback()
	default:
		s.fail(m.ID, "invalid_request", "Unknown op.")
	}
}

func (s *server) start(m message) {
	if m.ID <= 0 {
		s.fail(0, "invalid_request", "A positive request id is required.")
		return
	}
	if len(m.Input) == 0 || m.Input[0] != '{' {
		s.fail(m.ID, "invalid_request", "Input must be a JSON object.")
		return
	}
	s.mu.Lock()
	if _, busy := s.pending[m.ID]; busy {
		s.mu.Unlock()
		s.fail(m.ID, "invalid_request", "Request id already in use.")
		return
	}
	handle := s.core.Start(string(m.Input))
	if handle == 0 {
		s.mu.Unlock()
		s.fail(m.ID, "busy", "Core closed or too many requests.")
		return
	}
	s.pending[m.ID] = handle
	s.wg.Add(1)
	s.mu.Unlock()

	go func() {
		defer s.wg.Done()
		result := s.core.Result(handle)
		s.mu.Lock()
		delete(s.pending, m.ID)
		s.mu.Unlock()
		if !json.Valid([]byte(result)) {
			s.fail(m.ID, "internal", "Core returned invalid JSON.")
			return
		}
		s.write(reply{ID: m.ID, Result: json.RawMessage(result)})
	}()
}

func (s *server) fail(id int64, code, text string) {
	b, _ := json.Marshal(map[string]any{
		"version": 1,
		"error":   map[string]any{"code": code, "message": text, "retryable": false},
	})
	s.write(reply{ID: id, Event: eventFor(id), Result: b})
}

// eventFor marks errors that belong to no request, so the app can log them.
func eventFor(id int64) string {
	if id == 0 {
		return "error"
	}
	return ""
}

func (s *server) write(r reply) {
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.writeErr != nil {
		return
	}
	b = append(b, '\n')
	if _, err := s.out.Write(b); err != nil {
		s.writeErr = err
		return
	}
	s.writeErr = s.out.Flush()
}

package stdio

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thexykril/otakase/mobilecore"
)

// fakeCore answers "echo" at once and blocks "wait" until cancelled.
type fakeCore struct {
	mu      sync.Mutex
	next    int64
	inputs  map[int64]string
	cancels map[int64]chan struct{}
	stopped int
	closed  bool
	limit   int
}

func newFake() *fakeCore {
	return &fakeCore{inputs: map[int64]string{}, cancels: map[int64]chan struct{}{}, limit: 16}
}

func (f *fakeCore) Start(input string) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed || len(f.inputs) >= f.limit {
		return 0
	}
	f.next++
	f.inputs[f.next] = input
	f.cancels[f.next] = make(chan struct{})
	return f.next
}

func (f *fakeCore) Result(id int64) string {
	f.mu.Lock()
	input, cancel := f.inputs[id], f.cancels[id]
	f.mu.Unlock()
	defer func() { f.mu.Lock(); delete(f.inputs, id); f.mu.Unlock() }()
	if strings.Contains(input, `"wait"`) {
		<-cancel
		return `{"version":1,"error":{"code":"cancelled","message":"Request cancelled.","retryable":false}}`
	}
	return `{"version":1,"data":{"echo":` + input + `}}`
}

func (f *fakeCore) Cancel(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// The channel stays in the map once closed: Result may not have read it
	// yet, and a missing one would leave it waiting forever.
	if c, ok := f.cancels[id]; ok {
		select {
		case <-c:
		default:
			close(c)
		}
	}
}

func (f *fakeCore) StopPlayback() { f.mu.Lock(); f.stopped++; f.mu.Unlock() }

func (f *fakeCore) Close() {
	f.mu.Lock()
	f.closed = true
	ids := make([]int64, 0, len(f.cancels))
	for id := range f.cancels {
		ids = append(ids, id)
	}
	f.mu.Unlock()
	for _, id := range ids {
		f.Cancel(id)
	}
}

type line struct {
	ID      int64           `json:"id"`
	Event   string          `json:"event"`
	Version int             `json:"version"`
	Result  json.RawMessage `json:"result"`
}

// session runs Serve on pipes and returns a writer for requests and a
// channel of decoded reply lines.
func session(t *testing.T, core Backend) (io.WriteCloser, <-chan line, <-chan error) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := Serve(core, inR, outW)
		outW.Close()
		done <- err
	}()
	lines := make(chan line, 32)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(outR)
		for sc.Scan() {
			var l line
			if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
				t.Errorf("reply is not JSON: %q", sc.Text())
				return
			}
			lines <- l
		}
	}()
	return inW, lines, done
}

func next(t *testing.T, lines <-chan line) line {
	t.Helper()
	select {
	case l, ok := <-lines:
		if !ok {
			t.Fatal("output closed")
		}
		return l
	case <-time.After(5 * time.Second):
		t.Fatal("no reply")
	}
	return line{}
}

func send(t *testing.T, w io.Writer, s string) {
	t.Helper()
	if _, err := io.WriteString(w, s+"\n"); err != nil {
		t.Fatal(err)
	}
}

func errorCode(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var r struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	if r.Error == nil {
		return ""
	}
	return r.Error.Code
}

func TestReadyThenResult(t *testing.T) {
	in, lines, done := session(t, newFake())
	if l := next(t, lines); l.Event != "ready" || l.Version != ProtocolVersion {
		t.Fatalf("first line = %+v, want ready", l)
	}
	send(t, in, `{"id":3,"op":"request","input":{"operation":"echo"}}`)
	l := next(t, lines)
	if l.ID != 3 || !strings.Contains(string(l.Result), `"operation":"echo"`) {
		t.Fatalf("got %+v", l)
	}
	in.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCancelAnswersThePendingRequest(t *testing.T) {
	in, lines, done := session(t, newFake())
	next(t, lines)
	send(t, in, `{"id":1,"op":"request","input":{"operation":"wait"}}`)
	send(t, in, `{"id":2,"op":"request","input":{"operation":"echo"}}`)
	if l := next(t, lines); l.ID != 2 {
		t.Fatalf("fast request should answer first, got %+v", l)
	}
	send(t, in, `{"id":1,"op":"cancel"}`)
	l := next(t, lines)
	if l.ID != 1 || errorCode(t, l.Result) != "cancelled" {
		t.Fatalf("got %+v", l)
	}
	in.Close()
	<-done
}

func TestClosingInputCancelsOutstandingWork(t *testing.T) {
	core := newFake()
	in, lines, done := session(t, core)
	next(t, lines)
	send(t, in, `{"id":9,"op":"request","input":{"operation":"wait"}}`)
	// Make sure the request started before closing.
	for {
		core.mu.Lock()
		n := len(core.inputs)
		core.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	in.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after input closed")
	}
	if l := next(t, lines); l.ID != 9 || errorCode(t, l.Result) != "cancelled" {
		t.Fatalf("got %+v", l)
	}
	if !core.closed {
		t.Fatal("core not closed")
	}
}

func TestBadMessages(t *testing.T) {
	core := newFake()
	core.limit = 1
	in, lines, done := session(t, core)
	next(t, lines)
	cases := []struct {
		msg   string
		id    int64
		event string
		code  string
	}{
		{`not json`, 0, "error", "invalid_request"},
		{`{"id":0,"op":"request","input":{}}`, 0, "error", "invalid_request"},
		{`{"id":4,"op":"request","input":"text"}`, 4, "", "invalid_request"},
		{`{"id":5,"op":"launch"}`, 5, "", "invalid_request"},
		{`{"id":6,"op":"request","input":{"operation":"wait"}}`, 0, "", ""},
		{`{"id":6,"op":"request","input":{"operation":"echo"}}`, 6, "", "invalid_request"},
		{`{"id":7,"op":"request","input":{"operation":"echo"}}`, 7, "", "busy"},
	}
	for _, c := range cases {
		send(t, in, c.msg)
		if c.code == "" {
			continue
		}
		l := next(t, lines)
		if l.ID != c.id || l.Event != c.event || errorCode(t, l.Result) != c.code {
			t.Errorf("%s: got id=%d event=%q code=%q", c.msg, l.ID, l.Event, errorCode(t, l.Result))
		}
	}
	send(t, in, `{"op":"stop_playback"}`)
	send(t, in, `{"id":6,"op":"cancel"}`)
	if l := next(t, lines); l.ID != 6 {
		t.Fatalf("got %+v", l)
	}
	in.Close()
	<-done
	if core.stopped != 1 {
		t.Fatalf("stop_playback calls = %d", core.stopped)
	}
}

func TestOversizedLine(t *testing.T) {
	in, lines, done := session(t, newFake())
	next(t, lines)
	go func() {
		io.WriteString(in, `{"id":1,"op":"request","input":{"q":"`+strings.Repeat("a", maxLine)+`"}}`+"\n")
		in.Close()
	}()
	if err := <-done; err == nil {
		t.Fatal("want error for oversized line")
	}
}

// The real core answers through the same server without network access.
func TestRealCore(t *testing.T) {
	in, lines, done := session(t, mobilecore.NewCore())
	next(t, lines)
	send(t, in, `{"id":1,"op":"request","input":{"version":1,"operation":"providers"}}`)
	l := next(t, lines)
	if l.ID != 1 || !strings.Contains(string(l.Result), `"providers":[`) {
		t.Fatalf("got %+v", l)
	}
	send(t, in, `{"id":2,"op":"request","input":{"version":2,"operation":"providers"}}`)
	if l := next(t, lines); errorCode(t, l.Result) != "invalid_request" {
		t.Fatalf("got %+v", l)
	}
	in.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

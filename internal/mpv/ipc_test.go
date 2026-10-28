//go:build !windows

package mpv

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// fakePlayer answers each request with reply(id, command), after writing an
// event and a reply meant for some other request, as a busy mpv would.
func fakePlayer(t *testing.T, reply func(id int64, command []any) string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("local player fixture: %v", err)
	}
	socket := listener.Addr().String()
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				line, err := bufio.NewReader(conn).ReadBytes('\n')
				if err != nil {
					return
				}
				var req request
				if json.Unmarshal(line, &req) != nil {
					return
				}
				fmt.Fprintf(conn, "{\"event\":\"property-change\",\"name\":\"time-pos\",\"data\":1}\n")
				fmt.Fprintf(conn, "{\"request_id\":%d,\"error\":\"success\",\"data\":\"wrong\"}\n", req.RequestID+1000)
				fmt.Fprintf(conn, "not json\n")
				fmt.Fprintln(conn, reply(req.RequestID, req.Command))
			}()
		}
	}()
	return socket
}

func TestCommandMatchesItsOwnReply(t *testing.T) {
	socket := fakePlayer(t, func(id int64, command []any) string {
		return fmt.Sprintf(`{"request_id":%d,"error":"success","data":%q}`, id, command[1])
	})
	got, err := commandWithDial(socket, dialFakePlayer, "get_property", "path")
	if err != nil {
		t.Fatal(err)
	}
	if got != "path" {
		t.Fatalf("got %#v, want the reply to this request", got)
	}
}

func TestCommandReturnsMPVErrors(t *testing.T) {
	socket := fakePlayer(t, func(id int64, _ []any) string {
		return fmt.Sprintf(`{"request_id":%d,"error":"property unavailable"}`, id)
	})
	_, err := commandWithDial(socket, dialFakePlayer, "get_property", "time-pos")
	var mpvErr Error
	if !errors.As(err, &mpvErr) || !IsUnavailable(err) {
		t.Fatalf("got %v, want mpv's own error", err)
	}
	if IsGone(err) {
		t.Fatal("an unavailable property does not mean mpv is gone")
	}
}

func TestCommandNumbersDecodeAsFloat(t *testing.T) {
	socket := fakePlayer(t, func(id int64, _ []any) string {
		return fmt.Sprintf(`{"request_id":%d,"error":"success","data":1440}`, id)
	})
	got, err := commandWithDial(socket, dialFakePlayer, "get_property", "duration")
	if err != nil {
		t.Fatal(err)
	}
	if got != float64(1440) {
		t.Fatalf("got %#v (%T), want float64 1440", got, got)
	}
}

func TestCommandWithoutPlayer(t *testing.T) {
	if _, err := Command("  ", "quit"); !errors.Is(err, ErrNoSocket) {
		t.Fatalf("got %v, want ErrNoSocket", err)
	}
	_, err := commandWithDial(filepath.Join(t.TempDir(), "missing.sock"), func(string) (net.Conn, error) {
		return nil, &net.OpError{Op: "dial", Net: "unix", Err: os.NewSyscallError("connect", syscall.ENOENT)}
	}, "quit")
	if !IsGone(err) {
		t.Fatalf("got %v, want a gone player", err)
	}
}

func dialFakePlayer(address string) (net.Conn, error) { return net.Dial("tcp", address) }

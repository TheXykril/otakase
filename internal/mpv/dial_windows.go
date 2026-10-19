//go:build windows

package mpv

import (
	"net"

	"github.com/Microsoft/go-winio"
)

// Dial connects to the named pipe mpv opened with --input-ipc-server.
func Dial(path string) (net.Conn, error) {
	return winio.DialPipe(path, nil)
}

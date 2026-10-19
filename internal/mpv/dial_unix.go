//go:build !windows

package mpv

import "net"

// Dial connects to the IPC server mpv opened with --input-ipc-server.
func Dial(path string) (net.Conn, error) {
	return net.Dial("unix", path)
}

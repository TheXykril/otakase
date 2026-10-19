//go:build windows

package internal

import "os"

// processAlive reports whether a process with this id is running: Windows
// only opens a handle to one that exists.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = p.Release()
	return true
}

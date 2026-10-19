//go:build !windows

package internal

import (
	"errors"
	"syscall"
)

// processAlive reports whether a process with this id is running.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

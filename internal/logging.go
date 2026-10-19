package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Log used to open and close the log file for every single line, costing an
// open/write/close syscall triple per entry. That is wasteful at any volume and
// becomes an amplifier whenever something starts logging in a tight loop.
//
// The handle is kept open instead, and writes stay unbuffered so a crash still
// loses nothing that a per-line open would have kept.

var (
	logMu     sync.Mutex
	logPath   string
	logHandle *os.File
)

// logFileLocked returns the open log handle, reopening it if the configured path
// has changed. Callers must hold logMu.
func logFileLocked() (*os.File, error) {
	path := GetGlobalLogFile()
	if path == "" {
		return nil, fmt.Errorf("no log file configured")
	}

	if logHandle != nil && logPath == path {
		return logHandle, nil
	}

	if logHandle != nil {
		logHandle.Close()
		logHandle = nil
	}

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create log directory: %w", err)
		}
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return nil, err
	}

	logHandle = file
	logPath = path
	return logHandle, nil
}

// writeLogLine appends one preformatted line to the log file.
func writeLogLine(line string) error {
	logMu.Lock()
	defer logMu.Unlock()

	file, err := logFileLocked()
	if err != nil {
		return err
	}
	if _, err := file.WriteString(line); err != nil {
		// A handle that has gone bad (log file deleted or rotated out from under
		// us) should not poison every later write.
		logHandle.Close()
		logHandle = nil
		return err
	}
	return nil
}

// CloseLogFile releases the log handle. Safe to call more than once.
func CloseLogFile() error {
	logMu.Lock()
	defer logMu.Unlock()

	if logHandle == nil {
		return nil
	}
	err := logHandle.Close()
	logHandle = nil
	logPath = ""
	return err
}

// resetLogHandleForTest drops the cached handle so a test can point the logger at
// a fresh path.
func resetLogHandleForTest() {
	_ = CloseLogFile()
}

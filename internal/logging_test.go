package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withTempLogFile(t *testing.T) string {
	t.Helper()

	previous := GetGlobalLogFile()
	path := filepath.Join(t.TempDir(), "debug.log")
	resetLogHandleForTest()
	SetGlobalLogFile(path)
	t.Cleanup(func() {
		resetLogHandleForTest()
		SetGlobalLogFile(previous)
	})
	return path
}

func TestLogWritesThroughAPersistentHandle(t *testing.T) {
	path := withTempLogFile(t)

	for _, line := range []string{"first", "second", "third"} {
		if err := Log(line); err != nil {
			t.Fatalf("Log(%q): %v", line, err)
		}
	}

	// Unbuffered writes mean the content is on disk without an explicit flush.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	content := string(raw)
	for _, want := range []string{"first", "second", "third"} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %q in log:\n%s", want, content)
		}
	}
	if lines := strings.Count(strings.TrimSpace(content), "\n") + 1; lines != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", lines, content)
	}
	// The caller's file and line must survive the move off the per-line open.
	if !strings.Contains(content, "logging_test.go:") {
		t.Fatalf("expected caller information in log:\n%s", content)
	}
}

// ClearLogFile truncates the file; a stale append handle would silently restore
// the old length on the next write.
func TestClearLogFileInvalidatesCachedHandle(t *testing.T) {
	path := withTempLogFile(t)

	if err := Log(strings.Repeat("noise", 200)); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if err := ClearLogFile(path); err != nil {
		t.Fatalf("ClearLogFile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("expected an empty log after clearing, got %d bytes", info.Size())
	}

	if err := Log("after clear"); err != nil {
		t.Fatalf("Log after clear: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if strings.Contains(string(raw), "noise") {
		t.Fatalf("truncated content came back:\n%s", raw)
	}
	if !strings.Contains(string(raw), "after clear") {
		t.Fatalf("expected the new line, got:\n%s", raw)
	}
}

func TestLogReopensWhenPathChanges(t *testing.T) {
	first := withTempLogFile(t)
	if err := Log("to first"); err != nil {
		t.Fatalf("Log: %v", err)
	}

	second := filepath.Join(t.TempDir(), "other.log")
	SetGlobalLogFile(second)
	if err := Log("to second"); err != nil {
		t.Fatalf("Log: %v", err)
	}

	firstRaw, _ := os.ReadFile(first)
	secondRaw, _ := os.ReadFile(second)
	if !strings.Contains(string(firstRaw), "to first") || strings.Contains(string(firstRaw), "to second") {
		t.Fatalf("first log has wrong content:\n%s", firstRaw)
	}
	if !strings.Contains(string(secondRaw), "to second") {
		t.Fatalf("second log missing its line:\n%s", secondRaw)
	}
}

func TestCloseLogFileIsIdempotent(t *testing.T) {
	withTempLogFile(t)
	if err := Log("x"); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if err := CloseLogFile(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := CloseLogFile(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	// Logging after a close must transparently reopen.
	if err := Log("y"); err != nil {
		t.Fatalf("Log after close: %v", err)
	}
}

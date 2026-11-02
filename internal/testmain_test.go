package internal

import (
	"os"
	"testing"
)

// TestMain points the desktop data directories at a scratch folder, so a test
// that sends a notification installs the app icon and menu entry there rather
// than in the home directory of whoever runs the tests.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "otakase-test-data-")
	if err == nil {
		os.Setenv("XDG_DATA_HOME", dir)
		os.Setenv("XDG_DATA_DIRS", dir)
	}
	code := m.Run()
	if dir != "" {
		os.RemoveAll(dir)
	}
	os.Exit(code)
}

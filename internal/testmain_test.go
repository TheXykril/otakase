package internal

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
)

type localTestTransport struct{ base http.RoundTripper }

func (t localTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	ip := net.ParseIP(r.URL.Hostname())
	if r.URL.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("tests require mocked HTTP for %s", r.URL.Hostname())
	}
	return t.base.RoundTrip(r)
}

// TestMain points the desktop data directories at a scratch folder, so a test
// that sends a notification installs the app icon and menu entry there rather
// than in the home directory of whoever runs the tests.
//
// It also keeps the clock out of the late-night extras, so a suite run at four
// in the morning sees the same menus as one run at noon.
func TestMain(m *testing.M) {
	http.DefaultTransport = localTestTransport{base: http.DefaultTransport}
	sharedHTTPClient.Transport = localTestTransport{base: sharedHTTPClient.Transport}
	lateNightNow = func() bool { return false }
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

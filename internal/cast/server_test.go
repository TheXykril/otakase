package cast

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The Chromecast fetches over the network, so the server has to be reachable
// at a LAN address -- not localhost, which means nothing to another device.
func TestServerURLIsNotLoopback(t *testing.T) {
	dir := t.TempDir()
	server, err := NewServer(dir)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { server.Close() })

	url := server.URL("playlist.m3u8")
	if strings.Contains(url, "127.0.0.1") || strings.Contains(url, "localhost") {
		t.Errorf("URL is only reachable from this machine: %q", url)
	}
	if !strings.HasSuffix(url, "/playlist.m3u8") {
		t.Errorf("URL does not name the file: %q", url)
	}
}

func TestServerServesFilesFromItsDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "playlist.m3u8"), []byte("#EXTM3U\n"), 0o644); err != nil {
		t.Fatalf("write playlist: %v", err)
	}

	server, err := NewServer(dir)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { server.Close() })

	resp, err := http.Get(server.URL("playlist.m3u8"))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "#EXTM3U\n" {
		t.Errorf("served %q", string(body))
	}
}

// A path climbing out of the directory would serve anything on the machine to
// anything on the network.
func TestServerRefusesPathsOutsideItsDirectory(t *testing.T) {
	dir := t.TempDir()
	server, err := NewServer(dir)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { server.Close() })

	resp, err := http.Get(server.URL("../../../etc/passwd"))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Error("a path outside the served directory was answered")
	}
}

// Closing has to actually stop the listener: the next episode starts another
// server, and a leaked one holds its port for the life of the process.
func TestServerCloseStopsServing(t *testing.T) {
	dir := t.TempDir()
	server, err := NewServer(dir)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	url := server.URL("anything")

	if err := server.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := http.Get(url); err == nil {
		t.Error("the server answered after Close")
	}
}

// A server that is closed deliberately has not failed, and Err must not
// invent a fault the caller would report to the viewer as a broken stream.
func TestServerErrIsNilAfterClose(t *testing.T) {
	dir := t.TempDir()
	server, err := NewServer(dir)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := server.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Serve returns on its own goroutine; give it a moment to record anything.
	time.Sleep(50 * time.Millisecond)
	if err := server.Err(); err != nil {
		t.Errorf("Err after a deliberate Close: %v", err)
	}
}

// The receiver fetches the manifest and every segment by XHR, so a response it
// cannot read cross-origin is a response it cannot play -- and it looks like a
// perfectly normal request in the server's own log.
func TestServerSendsCORSHeaders(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, PlaylistName), []byte("#EXTM3U\n"), 0o644); err != nil {
		t.Fatalf("writing the playlist: %v", err)
	}
	server, err := NewServer(dir)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()

	resp, err := http.Get(server.URL(PlaylistName))
	if err != nil {
		t.Fatalf("fetching the playlist: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "*")
	}
}

// The system mime database maps .ts to a text format and .m3u8 to audio on at
// least some Linux installs, so these are stated rather than inferred: a
// receiver told a transport stream is text will not play it.
func TestServerStatesHLSContentTypes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, PlaylistName), []byte("#EXTM3U\n"), 0o644); err != nil {
		t.Fatalf("writing the playlist: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "seg00000.ts"), []byte("not really a segment"), 0o644); err != nil {
		t.Fatalf("writing the segment: %v", err)
	}
	server, err := NewServer(dir)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()

	for name, want := range map[string]string{
		PlaylistName:  "application/vnd.apple.mpegurl",
		"seg00000.ts": "video/mp2t",
	} {
		resp, err := http.Get(server.URL(name))
		if err != nil {
			t.Fatalf("fetching %s: %v", name, err)
		}
		got := resp.Header.Get("Content-Type")
		resp.Body.Close()
		if got != want {
			t.Errorf("%s served as %q, want %q", name, got, want)
		}
	}
}

// A random port cannot be allowed through a firewall: the rule would have to
// cover the whole ephemeral range, or the whole LAN. A fixed port lets a
// viewer open exactly one.
func TestNewServerOnPortUsesTheRequestedPort(t *testing.T) {
	// Borrow a free port from the kernel, then hand that number to NewServer.
	probe, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()

	server, err := NewServerOnPort(t.TempDir(), port)
	if err != nil {
		t.Fatalf("NewServerOnPort: %v", err)
	}
	defer server.Close()

	if want := fmt.Sprintf(":%d/", port); !strings.Contains(server.URL("x.m3u8"), want) {
		t.Errorf("URL %q does not use the requested port %d", server.URL("x.m3u8"), port)
	}
}

// Zero keeps today's behaviour: the kernel picks, and nothing needs configuring.
func TestNewServerOnPortZeroPicksAFreePort(t *testing.T) {
	server, err := NewServerOnPort(t.TempDir(), 0)
	if err != nil {
		t.Fatalf("NewServerOnPort: %v", err)
	}
	defer server.Close()

	if strings.Contains(server.URL("x.m3u8"), ":0/") {
		t.Errorf("URL %q was not given a real port", server.URL("x.m3u8"))
	}
}

// Whether the device ever connected is the difference between "it refused what
// we served" and "nothing reached us", and only the second is a firewall. The
// caller cannot tell them apart without this.
func TestServerReportsWhetherItWasEverFetched(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, PlaylistName), []byte("#EXTM3U\n"), 0o644); err != nil {
		t.Fatalf("writing the playlist: %v", err)
	}
	server, err := NewServer(dir)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()

	if server.Fetched() {
		t.Error("a server nobody has asked for anything reports having been fetched")
	}

	resp, err := http.Get(server.URL(PlaylistName))
	if err != nil {
		t.Fatalf("fetching: %v", err)
	}
	resp.Body.Close()

	if !server.Fetched() {
		t.Error("a server that served the playlist reports never having been fetched")
	}
}

package cast

import (
	"io"
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

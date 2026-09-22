# Chromecast Casting Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let `otakase -cast` play the resolved episode on a Chromecast on the same network instead of in local mpv, while tracking progress as usual.

**Architecture:** A Chromecast cannot send the `Referer`/`Origin` headers otakase's providers require, so it can never fetch a provider URL directly. otakase therefore remuxes the stream locally with ffmpeg (`-c copy`, no re-encode) into an HLS directory, serves that directory over plain HTTP on the LAN, and tells the Chromecast to play that local URL. Progress comes back by polling the device's media status instead of from mpv's IPC socket.

**Tech Stack:** Go 1.26, `github.com/vishen/go-chromecast` (Default Media Receiver — no Google developer registration), ffmpeg (already an optional dependency for `-download`), stdlib `net/http`.

**Spec:** This document. The design decisions and their rationale are in "Design decisions" below; there is no separate spec file.

## Global Constraints

- **`internal/cast` must not import `internal`.** `internal` imports `internal/cast` for the bridge, so the reverse is an import cycle. Same discipline as `internal/providerhost`: the package takes plain values and returns plain values.
- **Never re-encode.** ffmpeg runs with `-c copy`. A cast must cost bandwidth and almost no CPU, exactly as `-download` does.
- **ffmpeg stays an optional dependency.** Casting fails with a clear message when ffmpeg is absent, the way `-download` already does via `ErrFFmpegMissing`. It is never a hard requirement for the rest of otakase.
- **No new required config.** Casting is opt-in per run via `-cast`. `CastDevice` is optional and only pre-selects a device by name.
- **Go version:** 1.26 (matches `go.mod`). Builds are vendored: `CGO_ENABLED=0 go build -mod=vendor`. Adding a dependency needs `GOFLAGS=-mod=mod go mod tidy && go mod vendor`.
- **Verification gate:** `./Build/ci-local --quick` (vet + `go test -short -race ./...`) must pass before every commit.
- **Commit messages carry no attribution trailers.** No `Co-authored-by:`, no `Claude-Session:`.

---

## Design decisions

These were settled before the plan was written. An implementer should not re-litigate them without new information.

**Why remux rather than proxy the provider's HLS directly.** A thin proxy that rewrote playlist URLs would have to re-solve every quirk `internal/download.go` already documents — most notably that "providers routinely disguise HLS segments as images (…/seg-1-f1-v1-a1.jpg)", which needed `-allowed_extensions ALL -extension_picky 0` to get past ffmpeg's demuxer. A Chromecast's player is a black box and takes no such flags. Remuxing reuses the exact invocation already proven against these providers and hands the device one clean, predictable stream.

**Why HLS output rather than a progressive MP4.** A growing MP4 served over HTTP cannot satisfy range requests for a length that is not yet known, so seeking breaks. `-hls_playlist_type event` appends segments and never removes them, so the device can seek anywhere already written.

**Why no subtitles in v1.** `go-chromecast`'s `Load` takes no subtitle track parameter, and the Default Media Receiver renders WebVTT only — while otakase carries ASS in places. v1 therefore prefers the provider's hardsub stream when casting (otakase already has a `SubStyle` preference of `"hard"`) and warns when casting a softsub-only stream that subtitles will not appear. Soft subtitle tracks are a v2 concern.

**Why polling rather than events.** `go-chromecast` exposes `Status()`; a one-second poll is enough to drive both the completion threshold and opening/ending skips, and avoids depending on its message-callback surface.

---

## File structure

**New package `internal/cast`** — everything that talks to a Chromecast or serves bytes to one. No knowledge of otakase's types.

- `internal/cast/device.go` — the `Device` value and conversion from the library's discovery entries.
- `internal/cast/discover.go` — network discovery.
- `internal/cast/server.go` — the LAN HTTP server that serves the remuxed directory.
- `internal/cast/remux.go` — ffmpeg argument construction and process lifecycle.
- `internal/cast/session.go` — connect, load, poll, seek, tear down. Pure decision helpers (`ShouldMarkComplete`, `NextSkip`) live here.

**Bridge, in package `internal`** — knows otakase's types, calls the package above.

- `internal/cast_playback.go` — `CastEpisode`: resolves the stream, starts remux + server + session, drives tracking.

**Modified**

- `internal/config.go` — add `CastDevice` field and default.
- `cmd/otakase/main.go` — add the `-cast` flag and branch.
- `internal/otakase.go:1440-1441` — branch in `StartPlayback` before `StartVideoWithProviderFallback`.
- `README.md`, `CHANGELOG.md` — document the flag.

---

### Task 1: Device discovery

**Files:**
- Create: `internal/cast/device.go`
- Create: `internal/cast/discover.go`
- Test: `internal/cast/device_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `type Device struct { Name, UUID string; Addr net.IP; Port int }`, `func (d Device) String() string`, `func devicesFromEntries(entries []dns.CastEntry) []Device`, `func Discover(ctx context.Context, timeout time.Duration) ([]Device, error)`.

- [ ] **Step 1: Add the dependency**

```bash
cd /home/xykril/Work/Otakase
GOFLAGS=-mod=mod go get github.com/vishen/go-chromecast@latest
GOFLAGS=-mod=mod go mod tidy
go mod vendor
```

- [ ] **Step 2: Write the failing test**

Create `internal/cast/device_test.go`:

```go
package cast

import (
	"net"
	"testing"

	"github.com/vishen/go-chromecast/dns"
)

// Discovery answers on every interface, so the same device arrives more than
// once. Offering it twice in a menu is a bug the user has to think about.
func TestDevicesFromEntriesDedupesByUUID(t *testing.T) {
	entries := []dns.CastEntry{
		{UUID: "abc", DeviceName: "Living Room", AddrV4: net.ParseIP("192.168.1.10"), Port: 8009},
		{UUID: "abc", DeviceName: "Living Room", AddrV4: net.ParseIP("192.168.1.10"), Port: 8009},
		{UUID: "def", DeviceName: "Bedroom", AddrV4: net.ParseIP("192.168.1.11"), Port: 8009},
	}

	got := devicesFromEntries(entries)
	if len(got) != 2 {
		t.Fatalf("got %d devices, want 2: %+v", len(got), got)
	}
}

// A device with no friendly name still has to be selectable, or it cannot be
// told apart from the next unnamed one.
func TestDevicesFromEntriesFallsBackToTheAddress(t *testing.T) {
	entries := []dns.CastEntry{
		{UUID: "abc", DeviceName: "", AddrV4: net.ParseIP("192.168.1.10"), Port: 8009},
	}

	got := devicesFromEntries(entries)
	if len(got) != 1 {
		t.Fatalf("got %d devices, want 1", len(got))
	}
	if got[0].Name != "192.168.1.10" {
		t.Errorf("unnamed device is called %q", got[0].Name)
	}
}

// An entry with no usable address cannot be connected to, so it must not be
// offered at all.
func TestDevicesFromEntriesSkipsAddresslessEntries(t *testing.T) {
	entries := []dns.CastEntry{
		{UUID: "abc", DeviceName: "Ghost", Port: 8009},
		{UUID: "def", DeviceName: "Real", AddrV4: net.ParseIP("192.168.1.11"), Port: 8009},
	}

	got := devicesFromEntries(entries)
	if len(got) != 1 || got[0].Name != "Real" {
		t.Fatalf("addressless entry was offered: %+v", got)
	}
}

func TestDeviceStringNamesTheDeviceAndAddress(t *testing.T) {
	device := Device{Name: "Living Room", Addr: net.ParseIP("192.168.1.10"), Port: 8009}
	if got := device.String(); got != "Living Room (192.168.1.10:8009)" {
		t.Errorf("String() = %q", got)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd /home/xykril/Work/Otakase && go test ./internal/cast/ -run Device -v`
Expected: FAIL — the package does not compile, `undefined: devicesFromEntries`.

- [ ] **Step 4: Write the implementation**

Create `internal/cast/device.go`:

```go
// Package cast plays a stream on a Chromecast on the local network.
//
// It deliberately knows nothing about otakase's types: internal imports this
// package for the bridge, so importing internal back would be a cycle. Values
// in, values out -- the same discipline internal/providerhost follows.
package cast

import (
	"fmt"
	"net"

	"github.com/vishen/go-chromecast/dns"
)

// Device is one Chromecast found on the network.
type Device struct {
	Name string
	UUID string
	Addr net.IP
	Port int
}

// String names a device the way a menu should show it: the friendly name, and
// the address to tell two rooms with the same name apart.
func (d Device) String() string {
	return fmt.Sprintf("%s (%s:%d)", d.Name, d.Addr, d.Port)
}

// devicesFromEntries turns discovery results into devices worth offering.
//
// Discovery answers per interface, so the same device arrives repeatedly; and
// an entry without an address cannot be connected to at all.
func devicesFromEntries(entries []dns.CastEntry) []Device {
	devices := make([]Device, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))

	for _, entry := range entries {
		addr := entry.AddrV4
		if addr == nil {
			addr = entry.AddrV6
		}
		if addr == nil {
			continue
		}
		if _, already := seen[entry.UUID]; already && entry.UUID != "" {
			continue
		}
		if entry.UUID != "" {
			seen[entry.UUID] = struct{}{}
		}

		name := entry.DeviceName
		if name == "" {
			name = addr.String()
		}

		devices = append(devices, Device{
			Name: name,
			UUID: entry.UUID,
			Addr: addr,
			Port: entry.Port,
		})
	}
	return devices
}
```

Create `internal/cast/discover.go`:

```go
package cast

import (
	"context"
	"fmt"
	"time"

	"github.com/vishen/go-chromecast/dns"
)

// DefaultDiscoveryTimeout is how long to listen for devices announcing
// themselves. Discovery is multicast and answers trickle in, so this is a
// deadline rather than a duration anything waits out in full.
const DefaultDiscoveryTimeout = 3 * time.Second

// Discover lists the Chromecasts on the local network.
func Discover(ctx context.Context, timeout time.Duration) ([]Device, error) {
	if timeout <= 0 {
		timeout = DefaultDiscoveryTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	found, err := dns.DiscoverCastDNSEntries(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("cast: discovery failed: %w", err)
	}

	entries := []dns.CastEntry{}
	for entry := range found {
		entries = append(entries, entry)
	}
	return devicesFromEntries(entries), nil
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `cd /home/xykril/Work/Otakase && go test ./internal/cast/ -run Device -v`
Expected: PASS, four tests.

- [ ] **Step 6: Commit**

```bash
cd /home/xykril/Work/Otakase
./Build/ci-local --quick
git add go.mod go.sum vendor internal/cast/device.go internal/cast/discover.go internal/cast/device_test.go
git commit -m "Add Chromecast device discovery

Wraps go-chromecast's mDNS discovery into a list worth putting in a menu:
deduped, since discovery answers once per interface, and without entries
that carry no address to connect to."
```

---

### Task 2: The LAN stream server

**Files:**
- Create: `internal/cast/server.go`
- Test: `internal/cast/server_test.go`

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces: `func NewServer(dir string) (*Server, error)`, `func (s *Server) URL(name string) string`, `func (s *Server) Close() error`, `func outboundIP() (net.IP, error)`.

- [ ] **Step 1: Write the failing test**

Create `internal/cast/server_test.go`:

```go
package cast

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /home/xykril/Work/Otakase && go test ./internal/cast/ -run Server -v`
Expected: FAIL — `undefined: NewServer`.

- [ ] **Step 3: Write the implementation**

Create `internal/cast/server.go`:

```go
package cast

import (
	"fmt"
	"net"
	"net/http"
	"time"
)

// Server hands the remuxed stream to the cast device over the LAN.
//
// The device fetches over the network, so this binds to every interface and
// advertises a routable address. Serving on localhost would be serving to
// nobody: the Chromecast is a different machine.
type Server struct {
	dir      string
	listener net.Listener
	server   *http.Server
	baseURL  string
}

// NewServer starts serving dir on a free port and returns immediately.
func NewServer(dir string) (*Server, error) {
	addr, err := outboundIP()
	if err != nil {
		return nil, err
	}

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, fmt.Errorf("cast: could not listen: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	server := &Server{
		dir:      dir,
		listener: listener,
		baseURL:  fmt.Sprintf("http://%s:%d", addr, port),
	}

	// http.FileServer resolves ".." itself before touching the filesystem, so a
	// path climbing out of dir is answered 404 rather than served.
	server.server = &http.Server{
		Handler:           http.FileServer(http.Dir(dir)),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		_ = server.server.Serve(listener)
	}()

	return server, nil
}

// URL is where the cast device should fetch name from.
func (s *Server) URL(name string) string {
	return s.baseURL + "/" + name
}

// Close stops serving. A leaked server holds its port for the life of the
// process, and every episode starts another one.
func (s *Server) Close() error {
	return s.server.Close()
}

// outboundIP finds the address this machine is reachable at from the LAN.
//
// It dials a UDP socket rather than scanning interfaces: no packet is sent,
// but the kernel picks the source address it would route from, which is the
// one the cast device can reach back on. Scanning interfaces means guessing
// between a VPN, a container bridge and the real network.
func outboundIP() (net.IP, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return nil, fmt.Errorf("cast: could not determine this machine's LAN address: %w", err)
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /home/xykril/Work/Otakase && go test ./internal/cast/ -run Server -v`
Expected: PASS, four tests. If `TestServerURLIsNotLoopback` fails because the machine has no route out, that machine cannot cast either — the test is correctly reporting it.

- [ ] **Step 5: Commit**

```bash
cd /home/xykril/Work/Otakase
./Build/ci-local --quick
git add internal/cast/server.go internal/cast/server_test.go
git commit -m "Serve the remuxed stream to the cast device over the LAN

The device is a different machine, so this binds every interface and
advertises the address the kernel would route from rather than localhost.
Close has a test because a leaked listener holds its port for the life of
the process and every episode starts another."
```

---

### Task 3: ffmpeg remux to HLS

**Files:**
- Create: `internal/cast/remux.go`
- Test: `internal/cast/remux_test.go`

**Interfaces:**
- Consumes: nothing from Tasks 1-2.
- Produces: `const PlaylistName = "playlist.m3u8"`, `func BuildRemuxArgs(streamURL, referrer, outDir string) []string`, `func StartRemux(ffmpegPath, streamURL, referrer, outDir string) (*Remux, error)`, `func (r *Remux) Stop()`, `func (r *Remux) Err() error`, `func WaitForPlaylist(outDir string, timeout time.Duration) error`.

- [ ] **Step 1: Write the failing test**

Create `internal/cast/remux_test.go`:

```go
package cast

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Casting must cost bandwidth and almost no CPU, exactly as a download does.
func TestBuildRemuxArgsCopiesStreams(t *testing.T) {
	args := BuildRemuxArgs("https://cdn.test/master.m3u8", "", "/tmp/cast")
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "-c copy") {
		t.Fatalf("expected a stream copy, got: %s", joined)
	}
	if !strings.Contains(joined, "-nostdin") {
		t.Fatalf("ffmpeg must not consume otakase's stdin, got: %s", joined)
	}
}

// Providers disguise HLS segments as images to slip past filters. Without
// these, a perfectly good stream fails with "not in allowed_segment_extensions"
// -- the same wall downloads hit before internal/download.go worked around it.
func TestBuildRemuxArgsToleratesDisguisedSegments(t *testing.T) {
	joined := strings.Join(BuildRemuxArgs("https://cdn.test/x.m3u8", "", "/tmp/cast"), " ")

	if !strings.Contains(joined, "-allowed_extensions ALL") {
		t.Errorf("expected -allowed_extensions ALL, got: %s", joined)
	}
	if !strings.Contains(joined, "-extension_picky 0") {
		t.Errorf("expected -extension_picky 0, got: %s", joined)
	}
}

// Hosts reject a request without the referrer their own player sends, and
// -headers is a per-input option: it must come before -i or it applies to
// nothing.
func TestBuildRemuxArgsPassesReferrerBeforeTheInput(t *testing.T) {
	args := BuildRemuxArgs("https://cdn.test/x.m3u8", "https://megaplay.buzz/", "/tmp/cast")

	headerIdx, inputIdx := -1, -1
	for i, arg := range args {
		if arg == "-headers" {
			headerIdx = i
		}
		if arg == "-i" && inputIdx == -1 {
			inputIdx = i
		}
	}
	if headerIdx == -1 {
		t.Fatalf("no referrer header: %v", args)
	}
	if headerIdx > inputIdx {
		t.Fatalf("-headers came after -i, so it applies to nothing: %v", args)
	}
}

// An event playlist appends and never drops segments, so the device can seek
// anywhere already written. A VOD playlist would need the whole duration known
// upfront, and a live one would offer no seek bar at all.
func TestBuildRemuxArgsWritesASeekableEventPlaylist(t *testing.T) {
	args := BuildRemuxArgs("https://cdn.test/x.m3u8", "", "/tmp/cast")
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "-hls_playlist_type event") {
		t.Errorf("expected an event playlist, got: %s", joined)
	}
	if args[len(args)-1] != filepath.Join("/tmp/cast", PlaylistName) {
		t.Errorf("playlist must be the final argument, got: %s", joined)
	}
}

// The session cannot tell the device to play a playlist that does not exist
// yet, and ffmpeg takes a moment to write the first segment.
func TestWaitForPlaylistReturnsOnceItExists(t *testing.T) {
	dir := t.TempDir()
	go func() {
		time.Sleep(50 * time.Millisecond)
		os.WriteFile(filepath.Join(dir, PlaylistName), []byte("#EXTM3U\n"), 0o644)
	}()

	if err := WaitForPlaylist(dir, 2*time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWaitForPlaylistGivesUp(t *testing.T) {
	if err := WaitForPlaylist(t.TempDir(), 100*time.Millisecond); err == nil {
		t.Error("waiting for a playlist that never appears should fail")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /home/xykril/Work/Otakase && go test ./internal/cast/ -run 'Remux|Playlist' -v`
Expected: FAIL — `undefined: BuildRemuxArgs`.

- [ ] **Step 3: Write the implementation**

Create `internal/cast/remux.go`:

```go
package cast

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// PlaylistName is the playlist ffmpeg writes and the device is pointed at.
const PlaylistName = "playlist.m3u8"

// BuildRemuxArgs renders the stream into HLS the cast device can play.
//
// The device cannot send the headers these providers require, so it never
// fetches the provider URL: ffmpeg does, with the headers, and writes a clean
// local stream the device fetches instead. Nothing is re-encoded -- this is a
// container change, so it costs bandwidth and almost no CPU.
func BuildRemuxArgs(streamURL, referrer, outDir string) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-stats_period", "1"}

	// Providers routinely disguise HLS segments as images (…/seg-1-f1-v1-a1.jpg)
	// to slip past filters. ffmpeg's HLS demuxer rejects any segment extension
	// it does not recognise, so without this a perfectly good stream fails with
	// "not in allowed_segment_extensions".
	args = append(args, "-allowed_extensions", "ALL", "-extension_picky", "0")

	// -headers is a per-input option: it applies only to the next -i.
	if referrer = strings.TrimSpace(referrer); referrer != "" {
		args = append(args, "-headers", "Referer: "+referrer+"\r\n")
	}
	args = append(args, "-i", streamURL)

	args = append(args, "-c", "copy", "-bsf:a", "aac_adtstoasc")

	// An event playlist appends and never drops a segment, so the device can
	// seek back through everything written so far. VOD would need the whole
	// duration known before the first segment; live would offer no seek bar.
	args = append(args,
		"-f", "hls",
		"-hls_time", "4",
		"-hls_playlist_type", "event",
		"-hls_flags", "independent_segments",
		"-hls_segment_filename", filepath.Join(outDir, "seg%05d.ts"),
	)

	args = append(args, "-nostdin", "-y", filepath.Join(outDir, PlaylistName))
	return args
}

// Remux is a running ffmpeg writing the stream into a directory.
type Remux struct {
	cmd    *exec.Cmd
	mu     sync.Mutex
	err    error
	done   chan struct{}
	stderr strings.Builder
}

// StartRemux begins remuxing and returns without waiting for the first segment.
func StartRemux(ffmpegPath, streamURL, referrer, outDir string) (*Remux, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("cast: could not create the stream directory: %w", err)
	}

	remux := &Remux{done: make(chan struct{})}
	remux.cmd = exec.Command(ffmpegPath, BuildRemuxArgs(streamURL, referrer, outDir)...)
	remux.cmd.Stderr = &remux.stderr

	if err := remux.cmd.Start(); err != nil {
		return nil, fmt.Errorf("cast: could not start ffmpeg: %w", err)
	}

	go func() {
		defer close(remux.done)
		err := remux.cmd.Wait()
		remux.mu.Lock()
		defer remux.mu.Unlock()
		// A killed process is how Stop ends this, not a failure worth reporting.
		if err != nil && remux.cmd.ProcessState != nil && !remux.cmd.ProcessState.Exited() {
			return
		}
		if err != nil {
			remux.err = fmt.Errorf("cast: ffmpeg failed: %s", strings.TrimSpace(remux.stderr.String()))
		}
	}()

	return remux, nil
}

// Stop ends the remux. It is safe to call more than once.
func (r *Remux) Stop() {
	if r.cmd.Process != nil {
		_ = r.cmd.Process.Kill()
	}
	<-r.done
}

// Err reports why the remux ended, or nil while it is still running or if it
// finished the stream.
func (r *Remux) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

// WaitForPlaylist blocks until ffmpeg has written the playlist.
//
// The device cannot be told to play a file that does not exist yet, and the
// first segment takes a moment.
func WaitForPlaylist(outDir string, timeout time.Duration) error {
	playlist := filepath.Join(outDir, PlaylistName)
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if info, err := os.Stat(playlist); err == nil && info.Size() > 0 {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("cast: the stream did not start within %s", timeout)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /home/xykril/Work/Otakase && go test ./internal/cast/ -run 'Remux|Playlist' -v`
Expected: PASS, six tests.

- [ ] **Step 5: Commit**

```bash
cd /home/xykril/Work/Otakase
./Build/ci-local --quick
git add internal/cast/remux.go internal/cast/remux_test.go
git commit -m "Remux the stream into HLS a cast device can fetch

A Chromecast cannot send the Referer these providers require, so it can
never fetch a provider URL directly. ffmpeg fetches it instead, with the
headers, and writes a clean local stream -- container change only, so it
costs bandwidth and almost no CPU.

The segment-extension flags are the same workaround downloads already
needed: providers disguise HLS segments as images, and ffmpeg's demuxer
refuses extensions it does not recognise. An event playlist is what makes
the result seekable."
```

---

### Task 4: The cast session

**Files:**
- Create: `internal/cast/session.go`
- Test: `internal/cast/session_test.go`

**Interfaces:**
- Consumes: `Device` (Task 1).
- Produces: `type Span struct { Start, End float64 }`, `type Progress struct { Position, Duration float64; Idle bool }`, `func ShouldMarkComplete(p Progress, thresholdPercent int) bool`, `func NextSkip(position float64, spans []Span) (float64, bool)`, `func Connect(d Device) (*Session, error)`, `func (s *Session) Play(url string) error`, `func (s *Session) Progress() (Progress, error)`, `func (s *Session) SeekToTime(seconds float64) error`, `func (s *Session) Stop() error`.

- [ ] **Step 1: Write the failing test**

Create `internal/cast/session_test.go`:

```go
package cast

import "testing"

// The completion threshold has to mean the same thing casting as it does in
// mpv, or an episode watched on the TV is tracked differently from one watched
// at the desk.
func TestShouldMarkCompleteAtTheThreshold(t *testing.T) {
	cases := []struct {
		name      string
		progress  Progress
		threshold int
		want      bool
	}{
		{"just started", Progress{Position: 10, Duration: 1400}, 85, false},
		{"most of the way", Progress{Position: 1200, Duration: 1400}, 85, true},
		{"exactly at it", Progress{Position: 1190, Duration: 1400}, 85, true},
		{"finished", Progress{Position: 1400, Duration: 1400}, 85, true},
		// The device reports zero duration before it has loaded the stream;
		// dividing by it would mark an episode watched the moment it starts.
		{"duration not known yet", Progress{Position: 0, Duration: 0}, 85, false},
		// A threshold of zero means "never mark from progress", not "always".
		{"threshold disabled", Progress{Position: 1400, Duration: 1400}, 0, false},
	}

	for _, test := range cases {
		if got := ShouldMarkComplete(test.progress, test.threshold); got != test.want {
			t.Errorf("%s: ShouldMarkComplete = %v, want %v", test.name, got, test.want)
		}
	}
}

// Skipping on a cast device is a seek, decided from the polled position rather
// than from mpv's own clock.
func TestNextSkipFindsTheSpanThePositionIsInside(t *testing.T) {
	spans := []Span{{Start: 90, End: 180}, {Start: 1300, End: 1390}}

	if target, ok := NextSkip(120, spans); !ok || target != 180 {
		t.Errorf("inside the opening: got %v, %v; want 180, true", target, ok)
	}
	if target, ok := NextSkip(1350, spans); !ok || target != 1390 {
		t.Errorf("inside the ending: got %v, %v; want 1390, true", target, ok)
	}
	if _, ok := NextSkip(600, spans); ok {
		t.Error("the middle of an episode is not a skip")
	}
}

// A span already passed must not pull the viewer backwards, and one that has
// not started yet must not pull them forwards.
func TestNextSkipIgnoresSpansNotBeingPlayed(t *testing.T) {
	spans := []Span{{Start: 90, End: 180}}

	if _, ok := NextSkip(10, spans); ok {
		t.Error("seeking before the opening has started jumps the viewer forward")
	}
	if _, ok := NextSkip(400, spans); ok {
		t.Error("seeking after the opening has ended jumps the viewer backward")
	}
	// The span's own end is where a skip lands. Treating it as inside would
	// seek to where the player already is, forever.
	if _, ok := NextSkip(180, spans); ok {
		t.Error("the end of a span is not inside it")
	}
}

// An empty or zero-width span means "not known", not "skip to the start".
func TestNextSkipIgnoresUnknownSpans(t *testing.T) {
	if _, ok := NextSkip(50, nil); ok {
		t.Error("no spans is not a skip")
	}
	if _, ok := NextSkip(0, []Span{{Start: 0, End: 0}}); ok {
		t.Error("a zero-width span is not a skip")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /home/xykril/Work/Otakase && go test ./internal/cast/ -run 'Complete|Skip' -v`
Expected: FAIL — `undefined: ShouldMarkComplete`.

- [ ] **Step 3: Write the implementation**

Create `internal/cast/session.go`:

```go
package cast

import (
	"fmt"

	"github.com/vishen/go-chromecast/application"
)

// Span is a stretch of an episode worth skipping, in seconds.
type Span struct {
	Start float64
	End   float64
}

// Progress is where the device is in the episode.
type Progress struct {
	Position float64
	Duration float64
	// Idle reports that the device has stopped playing -- the episode ended,
	// or something else took the device over.
	Idle bool
}

// ShouldMarkComplete reports whether enough of the episode has played to count
// as watched, by the same threshold mpv playback uses.
func ShouldMarkComplete(p Progress, thresholdPercent int) bool {
	if thresholdPercent <= 0 || p.Duration <= 0 {
		return false
	}
	return p.Position/p.Duration*100 >= float64(thresholdPercent)
}

// NextSkip reports where to seek to, if the position is inside a span.
//
// The end of a span is not inside it: seeking there when the player has just
// arrived would seek to where it already is, on every poll, forever.
func NextSkip(position float64, spans []Span) (float64, bool) {
	for _, span := range spans {
		if span.End <= span.Start {
			continue
		}
		if position >= span.Start && position < span.End {
			return span.End, true
		}
	}
	return 0, false
}

// Session is a connected cast device.
type Session struct {
	app *application.Application
}

// Connect opens a connection to a device and takes over its media receiver.
func Connect(d Device) (*Session, error) {
	app := application.NewApplication()
	if err := app.Start(d.Addr.String(), d.Port); err != nil {
		return nil, fmt.Errorf("cast: could not connect to %s: %w", d.Name, err)
	}
	return &Session{app: app}, nil
}

// Play loads a URL on the device and starts it.
//
// The content type is stated rather than guessed: the stream is served from a
// directory with no meaningful extension handling, and the Default Media
// Receiver picks its player from this.
func (s *Session) Play(url string) error {
	if err := s.app.Load(url, 0, "application/x-mpegURL", false, false, false); err != nil {
		return fmt.Errorf("cast: could not start playback: %w", err)
	}
	return nil
}

// Progress asks the device where it is.
func (s *Session) Progress() (Progress, error) {
	if err := s.app.Update(); err != nil {
		return Progress{}, fmt.Errorf("cast: could not read the device status: %w", err)
	}

	_, media, _ := s.app.Status()
	if media == nil {
		return Progress{Idle: true}, nil
	}
	return Progress{
		Position: float64(media.CurrentTime),
		Duration: float64(media.Media.Duration),
		Idle:     media.PlayerState == "IDLE",
	}, nil
}

// SeekToTime jumps to a position, which is how a skip happens on a device with
// no IPC socket to seek through.
func (s *Session) SeekToTime(seconds float64) error {
	if err := s.app.SeekToTime(float32(seconds)); err != nil {
		return fmt.Errorf("cast: could not seek: %w", err)
	}
	return nil
}

// Stop ends playback and disconnects, leaving the device on its home screen
// rather than holding a stream that is about to stop being served.
func (s *Session) Stop() error {
	return s.app.Close(true)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /home/xykril/Work/Otakase && go test ./internal/cast/ -run 'Complete|Skip' -v`
Expected: PASS, four tests.

- [ ] **Step 5: Verify the go-chromecast field names compile**

Run: `cd /home/xykril/Work/Otakase && go build ./internal/cast/`
Expected: builds. If `media.Media.Duration` or `media.PlayerState` do not exist under those names, correct them against `go doc github.com/vishen/go-chromecast/cast.Media` — the surrounding logic does not change.

- [ ] **Step 6: Commit**

```bash
cd /home/xykril/Work/Otakase
./Build/ci-local --quick
git add internal/cast/session.go internal/cast/session_test.go
git commit -m "Connect to a cast device, play, poll and seek

Progress comes from polling the device's media status rather than from
mpv's IPC socket, so the completion threshold and the opening/ending
skips both had to be re-expressed as decisions about a polled position.
Both are pure functions, and tested: an episode watched on the TV should
be tracked exactly as one watched at the desk."
```

---

### Task 5: Wire casting into playback

**Files:**
- Create: `internal/cast_playback.go`
- Create: `internal/cast_playback_test.go`
- Modify: `internal/config.go` (struct field near `SubStyle`, default in `defaultConfigMap`)
- Modify: `internal/otakase.go:1440-1441` (branch at the end of `StartPlayback`)
- Modify: `cmd/otakase/main.go` (the `-cast` flag, beside `imagePreview` at line ~103)
- Modify: `README.md`, `CHANGELOG.md`

**Interfaces:**
- Consumes: everything from Tasks 1-4.
- Produces: `func CastEpisode(config *Config, anime *Anime) error`, `func castSpansFor(times SkipTimes, config *Config) []cast.Span`.

- [ ] **Step 1: Write the failing test**

Create `internal/cast_playback_test.go`:

```go
package internal

import (
	"testing"

	"github.com/thexykril/otakase/internal/cast"
)

// Casting reuses the resolved skip times, but only the ones the user asked
// for: SkipOp and SkipEd are separate settings and a cast must honour both.
func TestCastSpansFollowTheSkipSettings(t *testing.T) {
	times := SkipTimes{Op: Skip{Start: 90, End: 180}, Ed: Skip{Start: 1300, End: 1390}}

	both := castSpansFor(times, &Config{SkipOp: true, SkipEd: true})
	if len(both) != 2 {
		t.Fatalf("with both enabled, got %d spans: %+v", len(both), both)
	}

	opOnly := castSpansFor(times, &Config{SkipOp: true})
	if len(opOnly) != 1 || opOnly[0].Start != 90 {
		t.Errorf("with only SkipOp, got %+v", opOnly)
	}

	if spans := castSpansFor(times, &Config{}); len(spans) != 0 {
		t.Errorf("with neither enabled, got %+v", spans)
	}
}

// A span otakase never resolved is zero, and zero means "not known". Offering
// it would seek the device to the start of the episode.
func TestCastSpansDropUnresolvedTimes(t *testing.T) {
	times := SkipTimes{Op: Skip{Start: 0, End: 0}, Ed: Skip{Start: 1300, End: 1390}}

	spans := castSpansFor(times, &Config{SkipOp: true, SkipEd: true})
	if len(spans) != 1 || spans[0] != (cast.Span{Start: 1300, End: 1390}) {
		t.Errorf("an unresolved opening was offered as a skip: %+v", spans)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /home/xykril/Work/Otakase && go test ./internal/ -run CastSpans -v`
Expected: FAIL — `undefined: castSpansFor`.

- [ ] **Step 3: Write the bridge**

Create `internal/cast_playback.go`:

```go
package internal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/thexykril/otakase/internal/cast"
)

// castPollInterval is how often the device is asked where it is. A second is
// enough to catch a skip window and to notice the episode ending, without
// making a conversation out of it.
const castPollInterval = time.Second

// castStartTimeout is how long to wait for ffmpeg to write the first segment
// before giving up on the stream.
const castStartTimeout = 30 * time.Second

// castSpansFor turns resolved skip times into spans to seek past, honouring
// the settings that decide whether each is wanted at all.
//
// A zero span is one otakase never resolved, not one that starts at the
// beginning: offering it would seek the device back to the start of the
// episode on the first poll.
func castSpansFor(times SkipTimes, config *Config) []cast.Span {
	spans := []cast.Span{}
	if config == nil {
		return spans
	}
	if config.SkipOp && times.Op.End > times.Op.Start {
		spans = append(spans, cast.Span{Start: float64(times.Op.Start), End: float64(times.Op.End)})
	}
	if config.SkipEd && times.Ed.End > times.Ed.Start {
		spans = append(spans, cast.Span{Start: float64(times.Ed.Start), End: float64(times.Ed.End)})
	}
	return spans
}

// CastEpisode plays the already-resolved episode on a Chromecast instead of in
// mpv, and keeps tracking it while it plays.
func CastEpisode(config *Config, anime *Anime) error {
	if config == nil || anime == nil {
		return fmt.Errorf("cast: nothing to play")
	}
	if len(anime.Ep.Links) == 0 {
		return fmt.Errorf("cast: no episode links")
	}

	ffmpeg, err := ffmpegPath()
	if err != nil {
		return err
	}

	device, err := chooseCastDevice(config)
	if err != nil {
		return err
	}

	// The stream is remuxed into a directory per cast and thrown away after:
	// it is a transcode buffer, not a download.
	streamDir, err := os.MkdirTemp("", "otakase-cast-")
	if err != nil {
		return fmt.Errorf("cast: could not create the stream directory: %w", err)
	}
	defer os.RemoveAll(streamDir)

	streamURL := PrioritizeLink(anime.Ep.Links)
	referrer := anime.Ep.StreamReferrer
	if referrer == "" {
		referrer = streamReferrer(CurrentAnimeProviderName(anime))
	}

	Out(fmt.Sprintf("Preparing the stream for %s...", device.Name))
	remux, err := cast.StartRemux(ffmpeg, streamURL, referrer, streamDir)
	if err != nil {
		return err
	}
	defer remux.Stop()

	if err := cast.WaitForPlaylist(streamDir, castStartTimeout); err != nil {
		if remuxErr := remux.Err(); remuxErr != nil {
			return remuxErr
		}
		return err
	}

	server, err := cast.NewServer(streamDir)
	if err != nil {
		return err
	}
	defer server.Close()

	session, err := cast.Connect(device)
	if err != nil {
		return err
	}
	defer session.Stop()

	if anime.Ep.SubtitleURL != "" {
		// The Default Media Receiver renders WebVTT only, and Load carries no
		// subtitle track. Say so rather than letting the episode arrive silently
		// without the subtitles the viewer was expecting.
		Out("Note: this stream's subtitles cannot be cast. Try SubStyle=hard for a hardsubbed stream.")
	}

	if err := session.Play(server.URL(cast.PlaylistName)); err != nil {
		return err
	}
	Out(fmt.Sprintf("Playing on %s.", device.Name))

	return watchCast(config, anime, session)
}

// watchCast follows the episode while the device plays it.
func watchCast(config *Config, anime *Anime, session *cast.Session) error {
	spans := castSpansFor(anime.Ep.SkipTimes, config)
	marked := false

	for {
		time.Sleep(castPollInterval)

		progress, err := session.Progress()
		if err != nil {
			Log(fmt.Sprintf("cast: lost contact with the device: %v", err))
			return nil
		}
		if progress.Idle {
			Out("Playback finished.")
			return nil
		}

		anime.Ep.Player.PlaybackTime = int(progress.Position)
		if progress.Duration > 0 {
			anime.Ep.Duration = int(progress.Duration)
		}

		if target, ok := cast.NextSkip(progress.Position, spans); ok {
			if err := session.SeekToTime(target); err != nil {
				Log(fmt.Sprintf("cast: skip failed: %v", err))
			}
			continue
		}

		if !marked && cast.ShouldMarkComplete(progress, config.PercentageToMarkComplete) {
			marked = true
			LocalUpdateAnime(
				filepath.Join(os.ExpandEnv(config.StoragePath), "curd_history.txt"),
				anime.AnilistId, anime.ProviderId, anime.Ep.Number,
				int(progress.Position), int(progress.Duration),
				GetAnimeName(*anime), CurrentAnimeProviderName(anime),
			)
			// GetGlobalUser returns nil when nothing signed in, and local-only
			// tracking is a supported mode -- dereferencing it here would panic
			// on the one path a local-only user reaches.
			if user := GetGlobalUser(); UsesRemoteTracking(config) && user != nil {
				if err := UpdateAnimeProgress(user.Token, anime.AnilistId, anime.Ep.Number); err != nil {
					Log(fmt.Sprintf("cast: could not update remote progress: %v", err))
				}
			}
			Out(fmt.Sprintf("Episode %d marked as watched.", anime.Ep.Number))
		}
	}
}

// chooseCastDevice finds the device to play on, asking only when the answer is
// not already obvious.
func chooseCastDevice(config *Config) (cast.Device, error) {
	Out("Looking for cast devices...")
	devices, err := cast.Discover(context.Background(), cast.DefaultDiscoveryTimeout)
	if err != nil {
		return cast.Device{}, err
	}
	if len(devices) == 0 {
		return cast.Device{}, fmt.Errorf("cast: no devices found on this network")
	}

	if configured := config.CastDevice; configured != "" {
		for _, device := range devices {
			if device.Name == configured {
				return device, nil
			}
		}
		Out(fmt.Sprintf("%q was not found; pick another device.", configured))
	}

	if len(devices) == 1 {
		return devices[0], nil
	}

	options := make([]SelectionOption, 0, len(devices))
	for _, device := range devices {
		options = append(options, SelectionOption{Key: device.UUID, Label: device.String()})
	}
	selected, err := DynamicSelectPreserveOrder(options)
	if err != nil {
		return cast.Device{}, err
	}
	for _, device := range devices {
		if device.UUID == selected.Key {
			return device, nil
		}
	}
	return cast.Device{}, fmt.Errorf("cast: no device chosen")
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /home/xykril/Work/Otakase && go test ./internal/ -run CastSpans -v`
Expected: PASS, two tests.

- [ ] **Step 5: Add the config option**

In `internal/config.go`, add the field beside the other strings in the `Config` struct:

```go
	CastDevice                 string   `config:"CastDevice"`
```

and its default in `defaultConfigMap()`:

```go
		"CastDevice":                 "",
```

- [ ] **Step 6: Add the flag and the playback branch**

In `cmd/otakase/main.go`, beside `imagePreview` (~line 103):

```go
	castFlag := flag.Bool("cast", false, "Play on a Chromecast on this network instead of locally")
```

and after the config is settled, before playback begins (~line 262, after the sub/dub flags are applied):

```go
	if *castFlag {
		userConfig.CastToDevice = true
	}
```

In `internal/config.go`, add the matching per-run field, untagged so it is never written to the file:

```go
	// CastToDevice records that -cast was given for this run. It is not a
	// setting, so it carries no config tag.
	CastToDevice bool `config:"-"`
```

In `internal/otakase.go`, replace the final line of `StartPlayback` (currently `return StartVideoWithProviderFallback(userConfig, anime, title)`):

```go
	if userConfig.CastToDevice {
		// Casting owns the episode until it ends, and there is no mpv socket to
		// hand back: the caller's playback loop has nothing to poll.
		if err := CastEpisode(userConfig, anime); err != nil {
			Out("Casting failed: " + err.Error())
			Log(fmt.Sprintf("cast: %v", err))
		}
		RestoreScreen()
		return ""
	}

	return StartVideoWithProviderFallback(userConfig, anime, title)
```

- [ ] **Step 7: Verify it builds and the whole suite passes**

```bash
cd /home/xykril/Work/Otakase
./Build/ci-local --quick
CGO_ENABLED=0 go build -mod=vendor -o /tmp/otakase-cast ./cmd/otakase
/tmp/otakase-cast -h 2>&1 | grep -A1 -- "-cast"
```

Expected: tests pass, and the help text lists `-cast`.

- [ ] **Step 8: Test against a real device**

This is the step no unit test replaces. With a Chromecast on the same network:

```bash
/tmp/otakase-cast -cast
```

Expected: a device list (or straight through if there is only one), then the episode playing on the TV within ~30 seconds. Check that seeking works from the TV remote, that an opening is skipped, and that the episode is marked watched at the threshold.

- [ ] **Step 9: Document it**

In `README.md`, add to the flag table beside `-download`:

```
| `-cast` | Play on a Chromecast on this network instead of locally | |
```

and to the config table:

```
| `CastDevice` | String | a device name | Cast to this device without asking, when `-cast` is given and the device is found. Empty asks each time. |
```

Add a `## Casting` section after `### Saving episodes`:

```markdown
## Casting

`otakase -cast` plays the episode on a Chromecast on the same network.

A Chromecast cannot send the headers these streaming hosts require, so it
cannot fetch a provider's URL directly. Otakase therefore remuxes the stream
locally with `ffmpeg` — no re-encoding, so it costs bandwidth and almost no
CPU — serves it from this machine, and points the device at that. `ffmpeg` is
required for the same reason `-download` needs it.

Openings and endings are still skipped, and progress is still tracked. Soft
subtitles are not carried: the Chromecast renders only WebVTT, so set
`SubStyle=hard` for a hardsubbed stream where the provider offers one.

Set `CastDevice` to a device's name to skip being asked which one each time.
```

- [ ] **Step 10: Changelog and commit**

Add to `CHANGELOG.md` under `## Unreleased`:

```markdown
### Added

- **`otakase -cast` plays an episode on a Chromecast.** A Chromecast cannot
  send the `Referer` these hosts require, so it can never fetch a provider URL
  itself. Otakase remuxes the stream locally with ffmpeg — a container change,
  not a re-encode, so it costs bandwidth and almost no CPU — serves it from
  this machine, and points the device at that. No Google developer
  registration: it uses the Default Media Receiver, the same one every other
  casting tool uses.

  Openings and endings are still skipped, by seeking the device rather than
  mpv, and progress is tracked by the same threshold local playback uses.
  Soft subtitles are not carried: the receiver renders WebVTT only, and
  otakase carries ASS in places, so `SubStyle=hard` is the answer where a
  provider offers a hardsubbed stream.
```

```bash
cd /home/xykril/Work/Otakase
./Build/ci-local --quick
git add internal/cast_playback.go internal/cast_playback_test.go internal/config.go internal/otakase.go cmd/otakase/main.go README.md CHANGELOG.md
git commit -m "Play an episode on a Chromecast with -cast

Branches at the end of StartPlayback: casting owns the episode until it
ends and returns no mpv socket, so the caller's playback loop has nothing
to poll and must not try.

Skips and progress both work, re-expressed as decisions about a polled
position rather than mpv's clock. Soft subtitles do not survive the trip
-- the Default Media Receiver renders WebVTT only -- so the user is told
rather than left wondering where the subtitles went."
```

---

## Out of scope

Listed so a later reader knows these were decided, not forgotten.

- **Soft subtitle tracks.** Needs either a cast library that exposes media tracks on `Load`, or muxing WebVTT into the HLS and selecting it. v1 tells the user and suggests `SubStyle=hard`.
- **Next-episode autoplay while casting.** The playlist controller is built around mpv's IPC socket; casting would need its own equivalent.
- **Casting to DLNA, Roku, AirPlay.** The remux and the LAN server are protocol-agnostic and would be reused, but each target needs its own discovery and control. Chromecast first because it is the device in hand.
- **Resuming a cast part-way.** `Session.Play` always starts at zero. `Load` takes a start time, so this is a small addition once the rest is proven.
- **Adaptive bitrate.** Remuxing collapses the provider's variants to whichever ffmpeg picks. A thin proxy would preserve them, at the cost of every quirk described under "Design decisions".

## Self-review

**Spec coverage.** Discovery (Task 1), serving (Task 2), remux (Task 3), connect/play/poll/seek (Task 4), wiring, config, flag, docs (Task 5). The header constraint about `internal/cast` not importing `internal` holds: the package imports only stdlib and go-chromecast; the bridge in Task 5 is in package `internal` and imports the cast package one way.

**Placeholders.** None: every step carries the code it needs. One step names a fallback if a library field turns out to be spelled differently (the `cast.Media` fields in Task 4) — that is a verification instruction against a real dependency, not a deferred decision. Every otakase function the plan calls was checked to exist with the signature used: `streamReferrer`, `PrioritizeLink`, `ffmpegPath`, `DynamicSelectPreserveOrder`, `LocalUpdateAnime`, `CurrentAnimeProviderName`, `UsesRemoteTracking`, `GetGlobalUser`.

**Type consistency.** `Device` (Task 1) is consumed by `Connect` (Task 4) and `chooseCastDevice` (Task 5). `PlaylistName` (Task 3) is used by `Server.URL` callers in Task 5. `Span`/`Progress` (Task 4) are produced by `castSpansFor` and consumed by `NextSkip`/`ShouldMarkComplete` in Task 5. `StartRemux`/`WaitForPlaylist`/`Remux.Stop`/`Remux.Err` (Task 3) all appear in Task 5 with the same signatures.

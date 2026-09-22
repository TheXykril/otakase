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

// Done is closed when ffmpeg has exited, whatever the reason. A caller
// waiting for the stream to start uses it to give up the moment there is
// nothing left to wait for.
func (r *Remux) Done() <-chan struct{} {
	return r.done
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

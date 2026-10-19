package cast

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PlaylistName is the playlist ffmpeg writes and the device is pointed at.
const PlaylistName = "playlist.m3u8"

// Log receives ffmpeg's stderr once it exits, whenever there is any -- even on
// a clean exit, since ffmpeg can leave the stream broken while still reporting
// success (Critical 1: "AAC bitstream not in ADTS format and extradata
// missing", 260 lines of it, exit status 0). It defaults to discarding: this
// package must not import internal to reach the real logger, so
// internal/cast_playback.go wires this to it at init.
var Log = func(string) {}

// BuildRemuxArgs renders the stream into HLS the cast device can play.
//
// The device cannot send the headers these providers require, so it never
// fetches the provider URL: ffmpeg does, with the headers, and writes a clean
// local stream the device fetches instead. Nothing is re-encoded -- this is a
// container change, so it costs bandwidth and almost no CPU.
func BuildRemuxArgs(streamURL, referrer, outDir string) []string {
	return BuildRemuxArgsFrom(streamURL, referrer, outDir, 0, nil, nil)
}

// BuildRemuxArgsFrom is BuildRemuxArgs starting the stream at an offset.
//
// A cast episode cannot be seeked on the device: the receiver accepts SEEK and
// ignores it, because an event playlist has no length it can seek within. The
// stream is therefore rebuilt from the target instead, and the offset belongs
// before -i, where ffmpeg seeks the input rather than decoding everything up to
// the target and throwing it away.
//
// Copying cannot start both tracks at an offset in sync, though. A copied
// track can only start on a whole packet -- video on a keyframe -- and a host
// that serves audio and video as separate HLS playlists cuts each on its own
// boundaries. Measured on anizone, a copy restarted at 476.8s began its audio
// at 471.2s and its video at 475.0s: timestamps still consistent, but a
// receiver that plays each track from its first sample runs the sound 3.8s
// behind the picture. So only a stream starting at zero is copied; every
// restart goes through BuildBurnArgsFrom, which trims both tracks at the
// target. See castStreamSource.start.
//
// maps are -map arguments from SelectTracks, or nil to leave the choice of
// streams to ffmpeg. headers are any the provider needs beyond the referrer.
func BuildRemuxArgsFrom(streamURL, referrer, outDir string, startAt float64, maps []string, headers map[string]string) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-stats_period", "1"}

	// Providers routinely disguise HLS segments as images (…/seg-1-f1-v1-a1.jpg)
	// to slip past filters. ffmpeg's HLS demuxer rejects any segment extension
	// it does not recognise, so without this a perfectly good stream fails with
	// "not in allowed_segment_extensions".
	args = append(args, HLSInputArgs()...)

	// -headers is a per-input option: it applies only to the next -i.
	args = append(args, inputHeaderArgs(referrer, headers)...)
	args = append(args, seekArgs(startAt)...)
	args = append(args, "-i", streamURL)
	args = append(args, maps...)

	// No -bsf:a aac_adtstoasc here, unlike download.go: that filter strips ADTS
	// headers for an MP4 container, and these segments are MPEG-TS, which needs
	// AAC *in* ADTS. With it, ffmpeg logs "AAC bitstream not in ADTS format and
	// extradata missing" for every frame, still exits 0, and the device plays
	// undecodable audio while otakase reports a healthy cast.
	args = append(args, "-c", "copy")

	// Rewrite the declared H.264 level, without touching a single frame.
	// Encoders routinely over-declare: a 1080p24 stream that fits inside
	// Level 4.0 arrives claiming Level 5.0, and a Chromecast refuses anything
	// above the 4.1 its decoder is specified for -- it fetches one segment and
	// drops the media session. This edits the level field in the SPS only, so
	// it is still -c copy and still costs no CPU.
	args = append(args, "-bsf:v", "h264_metadata=level=4.1")

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
	return StartFFmpeg(ffmpegPath, BuildRemuxArgs(streamURL, referrer, outDir), outDir)
}

// StartFFmpeg runs a prepared ffmpeg command and watches it the same way,
// whether it is copying the stream or burning subtitles into it.
func StartFFmpeg(ffmpegPath string, args []string, outDir string) (*Remux, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("cast: could not create the stream directory: %w", err)
	}

	remux := &Remux{done: make(chan struct{})}
	remux.cmd = exec.Command(ffmpegPath, args...)
	remux.cmd.Stderr = &remux.stderr

	if err := remux.cmd.Start(); err != nil {
		return nil, fmt.Errorf("cast: could not start ffmpeg: %w", err)
	}

	go func() {
		defer close(remux.done)
		err := remux.cmd.Wait()
		stderr := strings.TrimSpace(remux.stderr.String())
		// Logged unconditionally, not only on a non-zero exit: Critical 1 is
		// exactly a case where ffmpeg exits 0 with stderr explaining the stream
		// it just wrote is broken.
		if stderr != "" {
			Log(fmt.Sprintf("cast: ffmpeg stderr: %s", stderr))
		}
		remux.mu.Lock()
		defer remux.mu.Unlock()
		// A killed process is how Stop ends this, not a failure worth reporting.
		if err != nil && remux.cmd.ProcessState != nil && !remux.cmd.ProcessState.Exited() {
			return
		}
		if err != nil {
			remux.err = fmt.Errorf("cast: ffmpeg failed: %s", stderr)
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

// seekArgs is the input-side seek shared by the remux and the burn.
//
// Before -i and not after it: after -i ffmpeg decodes the whole episode up to
// the target before writing anything, which on a twenty-four minute episode is
// a wait the viewer reads as a broken seek.
func seekArgs(startAt float64) []string {
	if startAt <= 0 {
		return nil
	}
	return []string{"-ss", strconv.FormatFloat(startAt, 'f', 3, 64)}
}

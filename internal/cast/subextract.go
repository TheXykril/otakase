package cast

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// A release from a torrent carries its subtitles inside the file rather than at
// a URL, so there is nothing to download before burning them in: they have to
// be read out of the file first. A Matroska file interleaves its subtitle
// packets with the video, so all of them arrive only with the whole download.
//
// They do not have to arrive all at once to be useful, though. A torrent
// downloads well ahead of playback, so the subtitles read so far reach well
// ahead of it too, and a stream burned with them is correct up to that point.
// SubtitleReader reads them as the file arrives and says how far it has got;
// the cast burns what there is, and rebuilds the stream with more before
// playback outruns it.

// BuildSubtitleExtractArgs is the ffmpeg command that writes one embedded
// subtitle track out as ASS, and the release's fonts next to it.
//
// ASS whatever the track was: it is what the subtitles filter draws best, and
// ShiftSubtitles shifts it in place for a seek. Each line is flushed as it is
// read, so the file is usable before it is finished, and the first audio
// track is copied to nowhere alongside: subtitles are sparse, so their own
// timestamps say little about how far into the file ffmpeg has read, and the
// audio's say exactly that in the -progress report.
//
// Fonts are dumped under names of this program's choosing, never the
// attachment's own filename tag, which comes from the file and could name a
// path outside fontsDir.
func BuildSubtitleExtractArgs(streamURL, referrer string, headers map[string]string, track SubtitleTrack, fonts []int, fontsDir, outPath string) []string {
	args := []string{"-hide_banner", "-v", "error", "-nostdin", "-nostats", "-progress", "pipe:1"}
	args = append(args, InputArgs(streamURL)...)
	args = append(args, inputHeaderArgs(referrer, headers)...)
	if fontsDir != "" {
		for _, index := range fonts {
			args = append(args, "-dump_attachment:"+strconv.Itoa(index), filepath.Join(fontsDir, "font-"+strconv.Itoa(index)))
		}
	}
	args = append(args, "-i", streamURL,
		"-map", "0:"+strconv.Itoa(track.Index),
		"-c:s", "ass",
		"-flush_packets", "1",
		"-f", "ass",
		"-y", outPath,
		"-map", "0:a:0?",
		"-c:a", "copy",
		"-f", "null", "-",
	)
	return args
}

// SubtitleReader reads an embedded subtitle track out of the source while the
// cast plays.
type SubtitleReader struct {
	path     string
	fontsDir string
	done     chan struct{}

	mu      sync.Mutex
	covered float64
	err     error
}

// StartSubtitleReader starts reading track out of the source into
// dir/subtitles.ass, with the release's fonts in dir/fonts. Cancelling ctx
// stops it.
func StartSubtitleReader(ctx context.Context, ffmpegPath, streamURL, referrer string, headers map[string]string, track SubtitleTrack, fonts []int, dir string) (*SubtitleReader, error) {
	r := &SubtitleReader{path: filepath.Join(dir, "subtitles.ass"), done: make(chan struct{})}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("cast: could not create the subtitles directory: %w", err)
	}
	if len(fonts) > 0 {
		r.fontsDir = filepath.Join(dir, "fonts")
		if err := os.MkdirAll(r.fontsDir, 0o755); err != nil {
			return nil, fmt.Errorf("cast: could not create the fonts directory: %w", err)
		}
	}

	cmd := exec.CommandContext(ctx, ffmpegPath, BuildSubtitleExtractArgs(streamURL, referrer, headers, track, fonts, r.fontsDir, r.path)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	progress, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("cast: could not read the subtitles: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("cast: could not start reading the subtitles: %w", err)
	}

	go func() {
		defer close(r.done)
		r.follow(progress)
		err := cmd.Wait()
		r.mu.Lock()
		defer r.mu.Unlock()
		switch {
		case ctx.Err() != nil:
			r.err = ctx.Err()
		case err != nil:
			r.err = fmt.Errorf("cast: could not read the subtitles out of the file: %v: %s", err, strings.TrimSpace(stderr.String()))
		default:
			r.covered = math.Inf(1)
		}
	}()
	return r, nil
}

// follow reads ffmpeg's -progress report and keeps how far into the source it
// has read.
func (r *SubtitleReader) follow(progress io.Reader) {
	scanner := bufio.NewScanner(progress)
	for scanner.Scan() {
		if seconds, ok := parseProgressTime(scanner.Text()); ok {
			r.mu.Lock()
			if seconds > r.covered {
				r.covered = seconds
			}
			r.mu.Unlock()
		}
	}
}

// parseProgressTime reads the position from one line of a -progress report.
// Both out_time_us and out_time_ms are microseconds -- the second despite its
// name -- and either is "N/A" until something has been written.
func parseProgressTime(line string) (float64, bool) {
	key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
	if !ok || (key != "out_time_us" && key != "out_time_ms") {
		return 0, false
	}
	micros, err := strconv.ParseInt(value, 10, 64)
	if err != nil || micros < 0 {
		return 0, false
	}
	return float64(micros) / 1e6, true
}

// Covered is how far into the episode, in seconds, the subtitles read so far
// reach: +Inf once every one has been read.
func (r *SubtitleReader) Covered() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.covered
}

// Done is closed when the reader has stopped, finished or not.
func (r *SubtitleReader) Done() <-chan struct{} { return r.done }

// Err is why the reader stopped short, once it has.
func (r *SubtitleReader) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

// FontsDir is the directory holding the release's fonts, or "" when it ships
// none.
func (r *SubtitleReader) FontsDir() string { return r.fontsDir }

// Snapshot copies the subtitles read so far to dst and reports how far they
// reach.
//
// The reach is read before the file is, so the copy holds at least what it
// claims. A line ffmpeg is still writing is left out: the copy ends at the
// last complete line, and the part-written one is in the next snapshot.
func (r *SubtitleReader) Snapshot(dst string) (float64, error) {
	covered := r.Covered()
	raw, err := os.ReadFile(r.path)
	if err != nil {
		return 0, fmt.Errorf("cast: could not read the subtitles so far: %w", err)
	}
	if end := bytes.LastIndexByte(raw, '\n'); end >= 0 {
		raw = raw[:end+1]
	} else {
		raw = nil
	}
	if err := os.WriteFile(dst, raw, 0o644); err != nil {
		return 0, fmt.Errorf("cast: could not save the subtitles so far: %w", err)
	}
	return covered, nil
}

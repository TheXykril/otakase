package cast

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// The cast panel has no length to show while an episode is playing. The device
// reports dur=-1 for the whole episode, because the remux writes no
// EXT-X-ENDLIST until it finishes, so the receiver treats the playlist as live.
// The tracker's average episode length fills that gap but is only an average:
// AniList said 24 minutes for an episode that runs 24:40.
//
// The source playlist knows exactly, and ffprobe reads it without fetching a
// single segment, so the real length is available before playback starts.

// ProbeDurationArgs assembles the ffprobe command that reads a stream's length.
func ProbeDurationArgs(streamURL, referrer string) []string {
	args := []string{"-hide_banner", "-v", "error"}

	// The same two allowances the remux needs: providers disguise HLS segments
	// as images, and ffprobe rejects unrecognised extensions exactly as ffmpeg
	// does. Without these a probe fails on the streams most in need of it.
	args = append(args, "-allowed_extensions", "ALL", "-extension_picky", "0")

	// Per-input option, so it has to precede -i, the same as in BuildRemuxArgs.
	if referrer = strings.TrimSpace(referrer); referrer != "" {
		args = append(args, "-headers", "Referer: "+referrer+"\r\n")
	}

	args = append(args, "-show_entries", "format=duration", "-of", "default=nw=1:nk=1")
	return append(args, "-i", streamURL)
}

// ParseProbedDuration reads the seconds out of ffprobe's output.
//
// ffprobe prints "N/A" for a stream whose length it cannot work out, and an
// empty line for one it could not open at all. Both mean the same thing here --
// no length -- and neither is worth an error the caller has to distinguish,
// because every failure has the same answer: fall back to what we already had.
func ParseProbedDuration(output string) (float64, bool) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, "N/A") {
			continue
		}
		seconds, err := strconv.ParseFloat(line, 64)
		if err != nil || seconds <= 0 {
			continue
		}
		return seconds, true
	}
	return 0, false
}

// FFprobePathFor finds the ffprobe that belongs with a given ffmpeg.
//
// The two ship together, so the ffprobe beside the resolved ffmpeg is the one
// that matches it -- which matters for a user running a build from a directory
// that is not on PATH. PATH is the fallback, not the first choice.
func FFprobePathFor(ffmpegPath string) (string, error) {
	if dir := filepath.Dir(ffmpegPath); dir != "" && dir != "." {
		name := "ffprobe"
		if ext := filepath.Ext(ffmpegPath); ext != "" {
			name += ext
		}
		beside := filepath.Join(dir, name)
		if info, err := os.Stat(beside); err == nil && !info.IsDir() {
			return beside, nil
		}
	}
	return exec.LookPath("ffprobe")
}

// ProbeDuration asks ffprobe how long a stream is.
//
// Only the playlist is fetched, not the media, so this costs one request even
// on an episode that takes minutes to remux.
func ProbeDuration(ffprobePath, streamURL, referrer string) (float64, error) {
	out, err := exec.Command(ffprobePath, ProbeDurationArgs(streamURL, referrer)...).Output()
	if err != nil {
		return 0, fmt.Errorf("cast: could not read the stream's length: %w", err)
	}
	seconds, ok := ParseProbedDuration(string(out))
	if !ok {
		return 0, fmt.Errorf("cast: the stream did not report a length")
	}
	return seconds, nil
}

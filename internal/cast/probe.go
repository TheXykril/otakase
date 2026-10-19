package cast

import (
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

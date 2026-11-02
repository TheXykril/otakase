package internal

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/cast"
)

// Providers hand back HLS master playlists, which ffmpeg
// can remux without re-encoding, so a download is fast and lossless.
//
// Downloads are Matroska by default. MKV carries what anime releases actually
// ship -- ASS subtitles with their fonts, several audio tracks, FLAC or Opus
// audio -- where MP4 can hold none of it and a torrent release could not be
// saved at all. DownloadFormat=mp4 keeps the old behaviour for devices that
// only play MP4.

// ErrFFmpegMissing reports that ffmpeg is not installed.
var ErrFFmpegMissing = fmt.Errorf("ffmpeg is required to download episodes")

// invalidFilenameChars are stripped from anime titles used as filenames.
var invalidFilenameChars = regexp.MustCompile(`[/\\:*?"<>|\x00-\x1f]`)

// ffmpegProgressTime matches the out_time_ms field of ffmpeg's -progress output.
var ffmpegProgressTime = regexp.MustCompile(`out_time_ms=(\d+)`)

// DownloadResult reports what happened for one episode.
type DownloadResult struct {
	Episode int
	Path    string
	Err     error
}

// SanitizeFilename makes a title safe to use as a filename on every platform.
func SanitizeFilename(name string) string {
	name = invalidFilenameChars.ReplaceAllString(name, "")
	name = strings.TrimSpace(strings.Join(strings.Fields(name), " "))
	// Leading dots are dropped so a title such as "../.." cannot collapse into a
	// relative path component, and Windows rejects a trailing dot or space.
	name = strings.TrimLeft(name, ". ")
	name = strings.TrimRight(name, ". ")
	if name == "" {
		name = "episode"
	}
	if len(name) > 150 {
		name = strings.TrimSpace(name[:150])
	}
	return name
}

// Download containers.
const (
	DownloadFormatMKV = "mkv"
	DownloadFormatMP4 = "mp4"
)

// DownloadFormat reads the configured container, defaulting to MKV.
func DownloadFormat(config *Config) string {
	if config != nil && strings.EqualFold(strings.TrimSpace(config.DownloadFormat), DownloadFormatMP4) {
		return DownloadFormatMP4
	}
	return DownloadFormatMKV
}

// ResolveDownloadDir expands the configured download directory, defaulting to
// ~/Downloads/otakase.
func ResolveDownloadDir(config *Config) string {
	dir := ""
	if config != nil {
		dir = strings.TrimSpace(config.DownloadDir)
	}
	if dir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Downloads", AppName)
		}
		return AppName + "-downloads"
	}
	return os.ExpandEnv(dir)
}

// ffmpegPath locates ffmpeg, or reports that it is missing.
func ffmpegPath() (string, error) {
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", ErrFFmpegMissing
	}
	return path, nil
}

// ffmpegJob is one stream to save.
type ffmpegJob struct {
	Stream   string
	Referrer string
	// Headers are extra request headers the stream's CDN requires; see
	// providers.StreamPlaybackHint.
	Headers  map[string]string
	Subtitle string
	Output   string
	// File says the stream is one whole file whatever its address looks
	// like: movie hosts hand out links such as get_video?id=... with no
	// extension, which would otherwise be read as HLS.
	File bool
}

// wholeFile reports whether the stream is a single file rather than an HLS
// playlist; see cast.IsWholeFile.
func (job ffmpegJob) wholeFile() bool {
	return job.File || cast.IsWholeFile(job.Stream)
}

// mp4 reports whether the output is MP4 rather than Matroska.
func (job ffmpegJob) mp4() bool {
	return strings.EqualFold(filepath.Ext(job.Output), "."+DownloadFormatMP4)
}

// headerBlock renders the request headers for one input.
func (job ffmpegJob) headerBlock() string {
	lines := []string{}
	if referrer := strings.TrimSpace(job.Referrer); referrer != "" {
		// Many hosts reject a request without the referrer their player sends.
		lines = append(lines, "Referer: "+referrer)
	}
	keys := make([]string, 0, len(job.Headers))
	for key := range job.Headers {
		if strings.EqualFold(key, "Referer") && strings.TrimSpace(job.Referrer) != "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := strings.TrimSpace(job.Headers[key])
		// A header value cannot carry a line break without smuggling in a
		// header of its own.
		if value == "" || strings.ContainsAny(key+value, "\r\n") {
			continue
		}
		lines = append(lines, key+": "+value)
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\r\n") + "\r\n"
}

// args assembles the remux command.
//
// The stream is copied rather than re-encoded, so a download costs bandwidth and
// almost no CPU.
func (job ffmpegJob) args() []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-stats_period", "1"}

	subtitle := strings.TrimSpace(job.Subtitle)
	whole := job.wholeFile()
	mp4 := job.mp4()

	// -headers is a per-input option: it applies only to the next -i. Setting it
	// once covers the video but leaves the subtitle request unauthenticated, which
	// the CDNs answer with 403.
	headers := job.headerBlock()
	requestHeaders := func() {
		if headers != "" {
			args = append(args, "-headers", headers)
		}
	}

	if !whole {
		// Providers routinely disguise HLS segments as images (…/seg-1-f1-v1-a1.jpg)
		// to slip past filters. ffmpeg's HLS demuxer rejects any segment extension it
		// does not recognise, so without this a perfectly good stream fails with
		// "not in allowed_segment_extensions". These are HLS demuxer options and must
		// NOT be given for any other input -- a WebVTT file or a Matroska release --
		// which rejects them outright.
		args = append(args, cast.HLSInputArgs()...)
	}
	requestHeaders()
	args = append(args, "-i", job.Stream)

	if subtitle != "" {
		requestHeaders()
		args = append(args, "-i", subtitle)
	}

	args = append(args, "-c", "copy")
	if !whole {
		// HLS audio is ADTS, which MP4 cannot hold and Matroska stores badly. A
		// whole file may carry FLAC or Opus, which the filter rejects.
		args = append(args, "-bsf:a", "aac_adtstoasc")
	}

	switch {
	case whole && !mp4:
		// A release keeps everything: every audio track, its subtitles, and
		// the fonts its ASS subtitles are styled with.
		args = append(args, "-map", "0:v:0", "-map", "0:a?", "-map", "0:s?", "-map", "0:t?")
	case whole:
		// MP4 holds neither ASS nor fonts: keep one of each and convert the
		// subtitles to the only text format it has.
		args = append(args, "-map", "0:v:0", "-map", "0:a:0?", "-map", "0:s:0?")
		if subtitle == "" {
			args = append(args, "-c:s", "mov_text")
		}
	case subtitle != "":
		// Map one video, one audio and the subtitle explicitly. A bare
		// "-map 0" pulls in every variant of an HLS master playlist, which
		// produced a broken file and crashed ffmpeg outright.
		args = append(args, "-map", "0:v:0", "-map", "0:a:0")
	}
	if subtitle != "" {
		args = append(args, "-map", "1:0")
		if mp4 {
			args = append(args, "-c:s", "mov_text")
		}
	}

	args = append(args, "-progress", "pipe:1", "-nostdin", "-y", job.Output)
	return args
}

// runFFmpeg executes a download, reporting progress through onProgress.
func runFFmpeg(binary string, args []string, onProgress func(elapsed time.Duration)) error {
	cmd := exec.Command(binary, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		if onProgress == nil {
			continue
		}
		if match := ffmpegProgressTime.FindStringSubmatch(scanner.Text()); len(match) > 1 {
			micros, convErr := strconv.ParseInt(match[1], 10, 64)
			if convErr == nil {
				onProgress(time.Duration(micros) * time.Microsecond)
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			if len(message) > 400 {
				message = message[:400] + "..."
			}
			return fmt.Errorf("ffmpeg failed: %s", message)
		}
		return fmt.Errorf("ffmpeg failed: %w", err)
	}
	return nil
}

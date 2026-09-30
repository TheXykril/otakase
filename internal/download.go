package internal

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/cast"
	"github.com/thexykril/otakase/internal/providers"
)

// Downloading episodes is the most-requested missing feature upstream
// (Wraient/curd#104, #55). Providers hand back HLS master playlists, which ffmpeg
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

// downloadFormats lists every container a download may have been saved in, the
// default first.
var downloadFormats = []string{DownloadFormatMKV, DownloadFormatMP4}

// DownloadFormat reads the configured container, defaulting to MKV.
func DownloadFormat(config *Config) string {
	if config != nil && strings.EqualFold(strings.TrimSpace(config.DownloadFormat), DownloadFormatMP4) {
		return DownloadFormatMP4
	}
	return DownloadFormatMKV
}

// DownloadPath builds the output file path for one episode.
func DownloadPath(dir, title string, episode int, mode, format string) string {
	title = SanitizeFilename(title)
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "sub"
	}
	if format != DownloadFormatMP4 {
		format = DownloadFormatMKV
	}
	return filepath.Join(dir, fmt.Sprintf("%s - Episode %02d (%s).%s", title, episode, mode, format))
}

// existingDownload finds a finished download of an episode in any container,
// so switching DownloadFormat does not fetch every episode again.
func existingDownload(dir, title string, episode int, mode string) (string, bool) {
	for _, format := range downloadFormats {
		file := DownloadPath(dir, title, episode, mode, format)
		if info, err := os.Stat(file); err == nil && info.Size() > 0 {
			return file, true
		}
	}
	return "", false
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
}

// wholeFile reports whether the stream is a single file rather than an HLS
// playlist. The torrent provider serves the release itself from a local port,
// and a direct link names its container. A playlist is always named as one:
// anineko proxies its HLS through a local port too.
func (job ffmpegJob) wholeFile() bool {
	parsed, err := url.Parse(strings.TrimSpace(job.Stream))
	if err != nil {
		return false
	}
	switch strings.ToLower(path.Ext(parsed.Path)) {
	case ".m3u8", ".m3u":
		return false
	case ".mkv", ".mp4", ".webm", ".avi", ".m4v":
		return true
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
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

// DownloadEpisode fetches one episode to dir and returns the file written.
func DownloadEpisode(config Config, anime *Anime, episode int, dir string) (string, error) {
	binary, err := ffmpegPath()
	if err != nil {
		return "", err
	}
	if anime == nil {
		return "", fmt.Errorf("no anime selected")
	}
	if episode <= 0 {
		return "", fmt.Errorf("invalid episode number %d", episode)
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create download directory: %w", err)
	}

	title := GetAnimeName(*anime)

	// Check for a finished download before touching the network: an episode
	// already on disk should not cost a provider round trip.
	configuredMode := normalizeTranslationType(config.SubOrDub)
	if existing, found := existingDownload(dir, title, episode, configuredMode); found {
		Out(fmt.Sprintf("Episode %d already downloaded: %s", episode, existing))
		return existing, nil
	}

	// ResolveEpisodeURLForPlayback rather than GetEpisodeURLForPlayback: it walks
	// the whole provider stack with sub/dub fallback and, crucially, returns the
	// per-stream hints. anime.Ep.SubtitleURL is only populated during playback, so
	// reading it here would silently skip subtitles on every download.
	result, err := ResolveEpisodeURLForPlayback(config, anime, episode)
	if err != nil {
		return "", err
	}
	if len(result.Links) == 0 {
		return "", fmt.Errorf("no links found for episode %d", episode)
	}

	mode := result.Mode
	streamURL := PrioritizeLink(result.Links)
	hint := result.LinkHints[streamURL]
	output := DownloadPath(dir, title, episode, mode, DownloadFormat(&config))

	// The resolved mode can differ from the configured one when otakase falls back
	// between sub and dub, so re-check under the name actually being written.
	if mode != configuredMode {
		if existing, found := existingDownload(dir, title, episode, mode); found {
			Out(fmt.Sprintf("Episode %d already downloaded: %s", episode, existing))
			return existing, nil
		}
	}

	providerName := result.ProviderName
	if providerName == "" {
		providerName = providerNameForAnime(anime)
	}
	referrer := hint.Referrer
	if referrer == "" {
		referrer = providers.Referrer(providerName)
	}
	job := ffmpegJob{
		Stream:   streamURL,
		Referrer: referrer,
		Headers:  hint.Headers,
		// The same language the player would show, not only the provider's default.
		Subtitle: pickSubtitleForHint(&config, anime, hint),
		Output:   output,
	}

	Out(fmt.Sprintf("Downloading episode %d (%s)...", episode, mode))
	Log(fmt.Sprintf("Downloading episode %d from %s to %s", episode, streamURL, output))

	lastReport := time.Now()
	onProgress := func(elapsed time.Duration) {
		// ffmpeg emits progress every second; throttle the user-facing line so a
		// long download does not flood the terminal.
		if time.Since(lastReport) < 5*time.Second {
			return
		}
		lastReport = time.Now()
		Out(fmt.Sprintf("  episode %d: %s downloaded", episode, elapsed.Round(time.Second)))
	}

	progressErr := runFFmpeg(binary, job.args(), onProgress)
	if progressErr != nil && job.Subtitle != "" {
		// A subtitle track is a nicety; losing the episode over one is not. Retry
		// without it rather than failing the download outright.
		Log(fmt.Sprintf("Episode %d failed with subtitles (%v); retrying without them", episode, progressErr))
		Out(fmt.Sprintf("  episode %d: subtitles unavailable, downloading video only", episode))
		lastReport = time.Now()
		job.Subtitle = ""
		progressErr = runFFmpeg(binary, job.args(), onProgress)
	}

	if progressErr != nil {
		// A partial file is worse than none: it would be mistaken for a completed
		// download on the next run.
		os.Remove(output)
		return "", progressErr
	}

	writeDownloadInfo(config, anime, episode, mode, providerName, output)
	Out(fmt.Sprintf("Saved %s", output))
	return output, nil
}

// DownloadEpisodes fetches an inclusive range of episodes, continuing past a
// failure so one missing episode does not abandon the rest.
func DownloadEpisodes(config Config, anime *Anime, from, to int, dir string) []DownloadResult {
	if to < from {
		from, to = to, from
	}

	results := make([]DownloadResult, 0, to-from+1)
	for episode := from; episode <= to; episode++ {
		path, err := DownloadEpisode(config, anime, episode, dir)
		results = append(results, DownloadResult{Episode: episode, Path: path, Err: err})
		if err != nil {
			Out(fmt.Sprintf("Episode %d failed: %v", episode, err))
			Log(fmt.Sprintf("Download of episode %d failed: %v", episode, err))
			if err == ErrFFmpegMissing {
				break
			}
		}
	}
	return results
}

// ParseEpisodeRange reads "5" or "1-12" into an inclusive range.
func ParseEpisodeRange(raw string, fallback int) (from, to int, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if fallback <= 0 {
			return 0, 0, fmt.Errorf("no episode selected")
		}
		return fallback, fallback, nil
	}

	first, second, found := strings.Cut(raw, "-")
	from, err = strconv.Atoi(strings.TrimSpace(first))
	if err != nil || from <= 0 {
		return 0, 0, fmt.Errorf("invalid episode range %q", raw)
	}
	if !found {
		return from, from, nil
	}

	to, err = strconv.Atoi(strings.TrimSpace(second))
	if err != nil || to <= 0 {
		return 0, 0, fmt.Errorf("invalid episode range %q", raw)
	}
	if to < from {
		from, to = to, from
	}
	return from, to, nil
}

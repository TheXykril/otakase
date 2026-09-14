package internal

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/providers"
)

// Downloading episodes is the most-requested missing feature upstream
// (Wraient/curd#104, #55). Providers hand back HLS master playlists, which ffmpeg
// can remux to MP4 without re-encoding, so a download is fast and lossless.

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

// DownloadPath builds the output file path for one episode.
func DownloadPath(dir, title string, episode int, mode string) string {
	title = SanitizeFilename(title)
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "sub"
	}
	return filepath.Join(dir, fmt.Sprintf("%s - Episode %02d (%s).mp4", title, episode, mode))
}

// ResolveDownloadDir expands the configured download directory, defaulting to
// ~/Downloads/curd.
func ResolveDownloadDir(config *CurdConfig) string {
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

// buildFFmpegArgs assembles the remux command for one stream.
//
// The stream is copied rather than re-encoded, so a download costs bandwidth and
// almost no CPU. aac_adtstoasc is required to put ADTS audio from HLS into MP4.
func buildFFmpegArgs(streamURL, referrer, subtitleURL, output string) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-stats_period", "1"}

	referrer = strings.TrimSpace(referrer)
	subtitleURL = strings.TrimSpace(subtitleURL)

	// -headers is a per-input option: it applies only to the next -i. Setting it
	// once covers the video but leaves the subtitle request unauthenticated, which
	// the CDNs answer with 403.
	referrerHeader := func() {
		if referrer != "" {
			// Many hosts reject a request without the referrer their player sends.
			args = append(args, "-headers", "Referer: "+referrer+"\r\n")
		}
	}

	// Providers routinely disguise HLS segments as images (…/seg-1-f1-v1-a1.jpg)
	// to slip past filters. ffmpeg's HLS demuxer rejects any segment extension it
	// does not recognise, so without this a perfectly good stream fails with
	// "not in allowed_segment_extensions". These are HLS demuxer options and must
	// NOT be repeated before a WebVTT input, which rejects them outright.
	args = append(args, "-allowed_extensions", "ALL", "-extension_picky", "0")
	referrerHeader()
	args = append(args, "-i", streamURL)

	if subtitleURL != "" {
		referrerHeader()
		args = append(args, "-i", subtitleURL)
	}

	args = append(args, "-c", "copy", "-bsf:a", "aac_adtstoasc")
	if subtitleURL != "" {
		// Map one video, one audio and the subtitle explicitly. A bare
		// "-map 0" pulls in every variant of an HLS master playlist, which
		// produced a broken file and crashed ffmpeg outright.
		args = append(args, "-c:s", "mov_text", "-map", "0:v:0", "-map", "0:a:0", "-map", "1:0")
	}

	args = append(args, "-progress", "pipe:1", "-nostdin", "-y", output)
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
func DownloadEpisode(config CurdConfig, anime *Anime, episode int, dir string) (string, error) {
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
	expected := DownloadPath(dir, title, episode, normalizeTranslationType(config.SubOrDub))
	if info, statErr := os.Stat(expected); statErr == nil && info.Size() > 0 {
		CurdOut(fmt.Sprintf("Episode %d already downloaded: %s", episode, expected))
		return expected, nil
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
	output := DownloadPath(dir, title, episode, mode)

	// The resolved mode can differ from the configured one when curd falls back
	// between sub and dub, so re-check under the name actually being written.
	if output != expected {
		if info, statErr := os.Stat(output); statErr == nil && info.Size() > 0 {
			CurdOut(fmt.Sprintf("Episode %d already downloaded: %s", episode, output))
			return output, nil
		}
	}

	referrer := hint.Referrer
	if referrer == "" {
		providerName := result.ProviderName
		if providerName == "" {
			providerName = providerNameForAnime(anime)
		}
		referrer = providers.Referrer(providerName)
	}

	CurdOut(fmt.Sprintf("Downloading episode %d (%s)...", episode, mode))
	Log(fmt.Sprintf("Downloading episode %d from %s to %s", episode, streamURL, output))

	lastReport := time.Now()
	onProgress := func(elapsed time.Duration) {
		// ffmpeg emits progress every second; throttle the user-facing line so a
		// long download does not flood the terminal.
		if time.Since(lastReport) < 5*time.Second {
			return
		}
		lastReport = time.Now()
		CurdOut(fmt.Sprintf("  episode %d: %s downloaded", episode, elapsed.Round(time.Second)))
	}

	progressErr := runFFmpeg(binary, buildFFmpegArgs(streamURL, referrer, hint.Subtitle, output), onProgress)
	if progressErr != nil && hint.Subtitle != "" {
		// A subtitle track is a nicety; losing the episode over one is not. Retry
		// without it rather than failing the download outright.
		Log(fmt.Sprintf("Episode %d failed with subtitles (%v); retrying without them", episode, progressErr))
		CurdOut(fmt.Sprintf("  episode %d: subtitles unavailable, downloading video only", episode))
		lastReport = time.Now()
		progressErr = runFFmpeg(binary, buildFFmpegArgs(streamURL, referrer, "", output), onProgress)
	}

	if progressErr != nil {
		// A partial file is worse than none: it would be mistaken for a completed
		// download on the next run.
		os.Remove(output)
		return "", progressErr
	}

	CurdOut(fmt.Sprintf("Saved %s", output))
	return output, nil
}

// DownloadEpisodes fetches an inclusive range of episodes, continuing past a
// failure so one missing episode does not abandon the rest.
func DownloadEpisodes(config CurdConfig, anime *Anime, from, to int, dir string) []DownloadResult {
	if to < from {
		from, to = to, from
	}

	// StartCurd refreshes a stale provider id before playing; downloads must do
	// the same or they can resolve against an expired session id.
	if err := resolveRuntimeProviderID(&config, anime); err != nil {
		Log(fmt.Sprintf("Could not refresh provider id before download: %v", err))
	}

	results := make([]DownloadResult, 0, to-from+1)
	for episode := from; episode <= to; episode++ {
		path, err := DownloadEpisode(config, anime, episode, dir)
		results = append(results, DownloadResult{Episode: episode, Path: path, Err: err})
		if err != nil {
			CurdOut(fmt.Sprintf("Episode %d failed: %v", episode, err))
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

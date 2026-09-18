package internal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestDownloadEpisodeLive downloads a real episode end to end.
//
// Run with: CURD_LIVE_DOWNLOAD_TEST=1 go test -run Live ./internal/
func TestDownloadEpisodeLive(t *testing.T) {
	if os.Getenv("CURD_LIVE_DOWNLOAD_TEST") != "1" {
		t.Skip("set CURD_LIVE_DOWNLOAD_TEST=1")
	}
	if _, err := ffmpegPath(); err != nil {
		t.Skip("ffmpeg is not installed")
	}

	config := Config{Provider: "stacked", SubOrDub: "sub", SubStyle: "soft"}
	previous := GetGlobalConfig()
	SetGlobalConfig(&config)
	t.Cleanup(func() { SetGlobalConfig(previous) })

	anime := &Anime{
		Title:        AnimeTitle{English: "One Piece", Romaji: "One Piece"},
		ProviderName: "anipub",
		ProviderId:   "10",
	}

	dir := t.TempDir()
	path, err := DownloadEpisode(config, anime, 1, dir)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat downloaded file: %v", err)
	}
	if info.Size() < 1_000_000 {
		t.Fatalf("downloaded file is suspiciously small: %d bytes", info.Size())
	}
	if filepath.Ext(path) != ".mp4" {
		t.Fatalf("expected an mp4, got %q", path)
	}

	// Size alone proves nothing: a remux that dropped a stream or wrote a broken
	// container still produces a large file. Ask ffprobe what is actually in it.
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Logf("ffprobe not installed; skipping container validation")
		return
	}
	out, err := exec.Command(probe,
		"-v", "error",
		"-show_entries", "stream=codec_type",
		"-of", "csv=p=0",
		path,
	).Output()
	if err != nil {
		t.Fatalf("ffprobe rejected the download: %v", err)
	}

	streams := string(out)
	if !strings.Contains(streams, "video") {
		t.Fatalf("no video stream in the download:\n%s", streams)
	}
	if !strings.Contains(streams, "audio") {
		t.Fatalf("no audio stream in the download:\n%s", streams)
	}
	// Subtitles are only present when the provider supplied a soft track, so this
	// is reported rather than required -- but it proves the hint plumbing works
	// when there is something to mux.
	if strings.Contains(streams, "subtitle") {
		t.Logf("subtitle track was muxed in")
	} else {
		t.Logf("provider supplied no soft subtitle track for this episode")
	}

	t.Logf("downloaded %s (%.1f MB), streams: %s",
		filepath.Base(path), float64(info.Size())/(1024*1024), strings.ReplaceAll(strings.TrimSpace(streams), "\n", ", "))
}

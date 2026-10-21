package cast

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// A release from a torrent carries its subtitles inside the file rather than at
// a URL, so there is nothing to download before burning them in: they have to
// be read out of the file first. A Matroska file interleaves its subtitle
// packets with the video, so that read covers the whole file, and on a torrent
// it lasts as long as the download does. The cast therefore starts without
// them and has them drawn in once they are out -- see ExtractSubtitles.

// BuildSubtitleExtractArgs is the ffmpeg command that writes one embedded
// subtitle track out as ASS, and the release's fonts next to it.
//
// ASS whatever the track was: it is what the subtitles filter draws best, and
// ShiftSubtitles shifts it in place for a seek. Fonts are dumped under names
// of this program's choosing, never the attachment's own filename tag, which
// comes from the file and could name a path outside fontsDir.
func BuildSubtitleExtractArgs(streamURL, referrer string, headers map[string]string, track SubtitleTrack, fonts []int, fontsDir, outPath string) []string {
	args := []string{"-hide_banner", "-v", "error", "-nostdin"}
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
		"-f", "ass",
		"-y", outPath,
	)
	return args
}

// ExtractSubtitles reads an embedded subtitle track out of the source into
// dir/subtitles.ass, with its fonts in dir/fonts, and returns the file's path.
//
// The file only appears once it is complete: a burn started from half a file
// would show the first minutes and then nothing, with no sign anything was
// missing. Cancelling ctx stops ffmpeg.
func ExtractSubtitles(ctx context.Context, ffmpegPath, streamURL, referrer string, headers map[string]string, track SubtitleTrack, fonts []int, dir string) (string, error) {
	fontsDir := filepath.Join(dir, "fonts")
	if len(fonts) > 0 {
		if err := os.MkdirAll(fontsDir, 0o755); err != nil {
			return "", fmt.Errorf("cast: could not create the fonts directory: %w", err)
		}
	} else {
		fontsDir = ""
	}

	partial := filepath.Join(dir, "subtitles.part")
	args := BuildSubtitleExtractArgs(streamURL, referrer, headers, track, fonts, fontsDir, partial)
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("cast: could not read the subtitles out of the file: %v: %s", err, strings.TrimSpace(stderr.String()))
	}

	final := filepath.Join(dir, "subtitles.ass")
	if err := os.Rename(partial, final); err != nil {
		return "", fmt.Errorf("cast: could not save the subtitles: %w", err)
	}
	return final, nil
}

// SubtitleFontsDir is the directory ExtractSubtitles put the release's fonts
// in, or "" when it holds none.
func SubtitleFontsDir(subtitlePath string) string {
	dir := filepath.Join(filepath.Dir(subtitlePath), "fonts")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return ""
	}
	return dir
}

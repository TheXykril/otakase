package internal

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/cast"
)

// castSubtitleFetchTimeout bounds the subtitle download. A subtitle file is a
// few tens of kilobytes of text, and a cast should not wait on a slow host for
// something it can proceed without.
const castSubtitleFetchTimeout = 20 * time.Second

// fetchCastSubtitle downloads a subtitle track next to the stream it belongs
// to, and reports where it landed.
//
// ffmpeg's subtitles filter reads a file, not a URL: it has no way to send the
// referrer these hosts require, and no way to be told about redirects.
func fetchCastSubtitle(subtitleURL, referrer, outDir string) (string, error) {
	request, err := http.NewRequest(http.MethodGet, subtitleURL, nil)
	if err != nil {
		return "", fmt.Errorf("cast: bad subtitle URL: %w", err)
	}
	if referrer != "" {
		request.Header.Set("Referer", referrer)
	}

	client := &http.Client{Timeout: castSubtitleFetchTimeout}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("cast: could not fetch the subtitles: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("cast: subtitle host answered %s", response.Status)
	}

	// The extension is what tells ffmpeg which demuxer to use, so it is taken
	// from the URL rather than guessed.
	name := "subtitles" + castSubtitleExtension(subtitleURL)
	path := filepath.Join(outDir, name)
	file, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("cast: could not write the subtitles: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, response.Body); err != nil {
		return "", fmt.Errorf("cast: could not save the subtitles: %w", err)
	}
	return path, nil
}

// castSubtitleExtension is the subtitle format named by a URL, defaulting to
// .vtt when it names none.
func castSubtitleExtension(subtitleURL string) string {
	trimmed := subtitleURL
	if i := strings.IndexAny(trimmed, "?#"); i >= 0 {
		trimmed = trimmed[:i]
	}
	switch ext := strings.ToLower(filepath.Ext(trimmed)); ext {
	case ".ass", ".ssa", ".srt", ".vtt", ".sub":
		return ext
	default:
		return ".vtt"
	}
}

// castBurnEncoder picks the encoder for burning subtitles in, honouring a
// configured choice and otherwise preferring the GPU when it works.
func castBurnEncoder(config *Config, ffmpeg string) cast.Encoder {
	if config != nil {
		switch strings.ToLower(strings.TrimSpace(config.CastEncoder)) {
		case "software", "libx264":
			return cast.Encoder{Name: "libx264"}
		case "vaapi", "h264_vaapi":
			return cast.Encoder{Name: "h264_vaapi", Device: cast.DefaultRenderNode}
		}
	}
	return cast.ChooseEncoder(ffmpeg, cast.DefaultRenderNode,
		func(enc cast.Encoder) error { return cast.ProbeEncoder(ffmpeg, enc) },
		func(path string) bool { _, err := os.Stat(path); return err == nil })
}

// castShouldBurnSubtitles reports whether this cast has subtitles worth
// drawing into the picture.
//
// It is decided by the audio that plays, as the stream itself labels it:
// English needs nothing drawn over it, and anything else -- Japanese under a
// dub label included -- gets the subtitles. audioLanguage comes from
// cast.SelectTracks and is "" when the stream does not say.
//
// Only then does the requested mode decide, and it can be trusted now where it
// once could not: this gate was removed (85a7959) because anikoto answered a
// dub request for a show without a dub with the sub stream, labelled dub. The
// provider refuses that request since 1358628, so a dub label reaching here is
// a dub. Burning regardless put the host's full English dialogue track over
// English audio on every real dub.
func castShouldBurnSubtitles(config *Config, anime *Anime, audioLanguage string) bool {
	if config == nil || anime == nil || !config.CastBurnSubtitles {
		return false
	}
	if strings.TrimSpace(anime.Ep.SubtitleURL) == "" {
		return false
	}
	switch audioLanguage {
	case "en":
		return false
	case "":
		return playlistAudioMode(anime, config) != "dub"
	default:
		return true
	}
}

// castLanguageName is a normalised language code for a message.
func castLanguageName(code string) string {
	switch code {
	case "ja":
		return "Japanese"
	case "en":
		return "English"
	case "":
		return "the host's default audio"
	}
	return "the " + code + " audio"
}

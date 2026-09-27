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
// The subtitle URL is the whole question. It used to be gated on the audio not
// being a dub as well, on the reasoning that burning text onto a dub re-encodes
// an episode to draw something the viewer is not reading.
//
// That gate has been removed because it rested on a mode nothing verifies.
// ProviderEpisodeResult.Mode is the mode that was *requested*, never the one the
// host actually served (internal/provider.go, episodeModeResult), and a request
// for a dub that the show has no dub for comes back as sub audio still labelled
// dub. Observed on anikoto: a dub-category request returned Japanese audio with
// a subtitle track attached, so the one episode that most needed subtitles was
// the one that skipped them, silently.
//
// A stream that ships a subtitle track is treated as a stream worth burning it
// onto. The cost of being wrong is a re-encode on a genuine dub that also
// carries captions, and CastBurnSubtitles turns that off.
func castShouldBurnSubtitles(config *Config, anime *Anime) bool {
	if config == nil || anime == nil || !config.CastBurnSubtitles {
		return false
	}
	return strings.TrimSpace(anime.Ep.SubtitleURL) != ""
}

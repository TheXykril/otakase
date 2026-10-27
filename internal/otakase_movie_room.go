package internal

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/theme"
)

// The Discord bot's watch room plays the film too, when its host searches for
// "otakase" in the room's picker. The browser has no mpv to draw the picture
// live, so ffmpeg draws it once, the same lavfi graph and ASS subtitle mpv
// would use, into an HLS playlist the room relays like any episode.

// MovieRoomWidth and MovieRoomHeight are the size the room's copy is encoded
// at: the film is drawn on its 1080p canvas, then scaled to what the room
// plays everything else at.
const (
	MovieRoomWidth  = 1280
	MovieRoomHeight = 720
)

// MovieRoomFilm writes the film for the watch room into dir and returns the
// ffmpeg arguments, to be run in dir, that draw it to dir/playlist.m3u8 with
// four-second segments, and how many seconds it lasts. presenter is the
// host's name, used when AniList does not answer with one; anilistToken, when
// not empty, is the host's AniList sign-in, which makes the film about them.
func MovieRoomFilm(dir, presenter, anilistToken string) ([]string, float64, error) {
	var stats watchStats
	var extras movieExtras
	if anilistToken != "" {
		// The fetches read the sign-in from the storage folder, as they do
		// for the app.
		config := &Config{TrackingRemote: TrackingRemoteAniList, StoragePath: dir}
		if err := os.WriteFile(anilistTokenPath(config), []byte(anilistToken), 0o600); err != nil {
			return nil, 0, err
		}
		fetched, err := fetchWatchStats(config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "note movie stats unavailable: %v\n", err)
		} else {
			stats = fetched
		}
		if extras, err = fetchMovieExtras(config); err != nil {
			fmt.Fprintf(os.Stderr, "note movie extras unavailable: %v\n", err)
		}
		_ = os.Remove(anilistTokenPath(config))
	}
	name := strings.TrimSpace(extras.Name)
	if name == "" {
		name = strings.TrimSpace(presenter)
	}

	palette := theme.Builtin()
	rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 1))
	facts := gatherMovieFacts(stats, extras, name, 0, time.Now())
	script, timeline := movieScript(facts, "The movie", extras.Favourites, palette, "monospace", "sans-serif", rng)
	if err := os.WriteFile(filepath.Join(dir, "credits.ass"), []byte(script), 0o600); err != nil {
		return nil, 0, err
	}
	if err := os.WriteFile(filepath.Join(dir, "music.wav"), movieSoundtrack(timeline.Score), 0o600); err != nil {
		return nil, 0, err
	}
	return movieRoomArgs(palette, extras.Favourites, timeline), timeline.End, nil
}

// movieRoomArgs is the ffmpeg command line that draws the film: the
// background first, a cover per later input, the music last.
func movieRoomArgs(palette theme.Palette, wall []movieFavourite, timeline movieTimeline) []string {
	background := strings.TrimPrefix(movieBackground(palette, timeline.End), "av://lavfi:")
	args := []string{"-f", "lavfi", "-i", background}
	var images []string
	for i, favourite := range wall {
		args = append(args, "-i", favourite.Path)
		images = append(images, fmt.Sprintf("[%d:v]", i+1))
	}
	music := len(wall) + 1
	args = append(args, "-i", "music.wav")
	// The subtitle is named relative to dir, so its path needs no escaping
	// inside the graph.
	graph := movieFilterGraph("[0:v]", images, "[film]", timeline.Pictures) +
		fmt.Sprintf(";[film]subtitles=credits.ass,scale=%d:%d,format=yuv420p[vo]", MovieRoomWidth, MovieRoomHeight)
	// No B-frames, for the same reason as the room's torrent segments:
	// Chromium's built-in HLS player trips over the decode times they bring.
	return append(args,
		"-filter_complex", graph, "-map", "[vo]", "-map", strconv.Itoa(music)+":a",
		"-t", strconv.FormatFloat(timeline.End+0.3, 'f', 2, 64),
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "22", "-bf", "0", "-g", "120", "-keyint_min", "120", "-sc_threshold", "0",
		"-c:a", "aac", "-ac", "2", "-b:a", "160k",
		"-f", "hls", "-hls_time", "4", "-hls_playlist_type", "vod", "-hls_segment_filename", "seg%d.ts", "playlist.m3u8")
}

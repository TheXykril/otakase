package internal

import (
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/thexykril/otakase/internal/theme"
)

// The room's copy is drawn by ffmpeg from the files MovieRoomFilm writes:
// the background first, a cover per input after it, the music last, out to
// a four-second HLS playlist.
func TestMovieRoomArgs(t *testing.T) {
	palette := theme.Builtin()
	wall := []movieFavourite{{Title: "A", Path: "/c/a.jpg"}, {Title: "B", Path: "/c/b.jpg"}}
	facts := gatherMovieFacts(watchStats{}, movieExtras{}, "Host", 0, time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC))
	_, timeline := movieScript(facts, "The movie", wall, palette, "monospace", "sans-serif", rand.New(rand.NewPCG(1, 1)))
	args := strings.Join(movieRoomArgs(palette, wall, timeline), " ")
	for _, want := range []string{
		"-f lavfi -i color=c=0x", "-i /c/a.jpg -i /c/b.jpg -i music.wav",
		"[0:v]noise", "[1:v]scale", "[2:v]scale", "subtitles=credits.ass,scale=1280:720", "-map [vo] -map 3:a",
		"-bf 0", "-hls_time 4 -hls_playlist_type vod -hls_segment_filename seg%d.ts playlist.m3u8",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %q in\n%s", want, args)
		}
	}
	if strings.Contains(args, "av://") {
		t.Errorf("mpv source left in: %s", args)
	}
}

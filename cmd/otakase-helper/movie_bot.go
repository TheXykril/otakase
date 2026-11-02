package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/thexykril/otakase/internal"
)

// movieRenderWait bounds drawing the film; a small host draws it in a few
// minutes.
const movieRenderWait = 15 * time.Minute

// movieBotFile is what the film's server hands out: the playlist and its
// segments, nothing else in the folder.
var movieBotFile = regexp.MustCompile(`^/(playlist\.m3u8|seg[0-9]+\.ts)$`)

// serveMovieForBot draws "Otakase: The Movie" for the bot's watch room and
// serves it as HLS on a loopback port, the way -torrent-serve serves an
// episode: "progress <text>" lines while it works, then the playlist's
// address, then it serves until stdin closes. The host's AniList sign-in,
// when the bot has one, comes in OTAKASE_MOVIE_ANILIST_TOKEN.
func serveMovieForBot(presenter string) error {
	token := strings.TrimSpace(os.Getenv("OTAKASE_MOVIE_ANILIST_TOKEN"))
	_ = os.Unsetenv("OTAKASE_MOVIE_ANILIST_TOKEN")
	// The parent owns this private temporary root and passes its lock as fd 3.
	if root := os.Getenv("OTAKASE_ROOM_DIR"); root != "" && root == os.Getenv("TMPDIR") && strings.HasPrefix(filepath.Base(root), "otakase-room-torrent-") {
		defer os.RemoveAll(root)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() { io.Copy(io.Discard, os.Stdin); cancel() }()
	dir, err := os.MkdirTemp("", "otakase-room-movie-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	fmt.Println("progress Developing the film")
	args, seconds, err := internal.MovieRoomFilm(dir, presenter, token)
	if err != nil {
		return err
	}
	if err = renderBotMovie(ctx, dir, args, seconds); err != nil {
		return err
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	files := http.FileServer(http.Dir(dir))
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if (r.Method != "GET" && r.Method != "HEAD") || !movieBotFile.MatchString(r.URL.Path) {
				http.NotFound(w, r)
				return
			}
			if strings.HasSuffix(r.URL.Path, ".m3u8") {
				w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			} else {
				w.Header().Set("Content-Type", "video/mp2t")
			}
			files.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second,
	}
	defer server.Close()
	failed := make(chan error, 1)
	go func() { failed <- server.Serve(listener) }()
	fmt.Printf("http://127.0.0.1:%d/playlist.m3u8\n", listener.Addr().(*net.TCPAddr).Port)
	select {
	case <-ctx.Done():
		return nil
	case err := <-failed:
		return err
	}
}

// renderBotMovie runs ffmpeg in dir, printing how far it has got.
func renderBotMovie(ctx context.Context, dir string, args []string, seconds float64) error {
	run, done := context.WithTimeout(ctx, movieRenderWait)
	defer done()
	args = append([]string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-progress", "pipe:1", "-nostats"}, args...)
	cmd := exec.CommandContext(run, "ffmpeg", args...)
	cmd.Dir = dir
	stderr := &botLimitedOutput{limit: 2048}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}
	shown := -1
	lines := bufio.NewScanner(stdout)
	for lines.Scan() {
		if percent, ok := movieRenderPercent(lines.Text(), seconds); ok && percent > shown {
			shown = percent
			fmt.Printf("progress Developing the film %d%%\n", percent)
		}
	}
	if err = cmd.Wait(); err != nil {
		if detail := strings.TrimSpace(stderr.buffer.String()); detail != "" {
			lines := strings.Split(detail, "\n")
			err = fmt.Errorf("%w: %s", err, strings.TrimSpace(lines[len(lines)-1]))
		}
		return fmt.Errorf("draw the film: %w", err)
	}
	return nil
}

// movieRenderPercent reads one line of ffmpeg's -progress output: how much
// of a film seconds long is drawn, when the line says.
func movieRenderPercent(line string, seconds float64) (int, bool) {
	value, ok := strings.CutPrefix(strings.TrimSpace(line), "out_time_us=")
	if !ok || seconds <= 0 {
		return 0, false
	}
	micros, err := strconv.ParseInt(value, 10, 64)
	if err != nil || micros < 0 {
		return 0, false
	}
	return min(int(float64(micros)/1e6*100/seconds), 99), true
}

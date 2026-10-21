package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// A movie cast from rofi is handed to a terminal window, as an anime episode
// is, so it gets the same control panel. Unlike an episode, the process that
// opened the window stays: it keeps the movie history, Trakt and the next
// server to try, so the window only casts and reports where it got to in a
// small file beside the handoff file.

// movieCastReport is what a cast window tells the process that opened it.
type movieCastReport struct {
	Position float64 `json:"position"`
	Duration int     `json:"duration"`
	Watched  bool    `json:"watched"`
	// Done is set when the cast ended by itself or was stopped. A window
	// closed mid-movie never sets it; the report stops being touched instead.
	Done  bool   `json:"done"`
	Error string `json:"error,omitempty"`
}

// movieCastBeat is how often a cast window touches its report to say it is
// still open.
var movieCastBeat = 2 * time.Second

// movieCastStale is how long a report can go untouched before its window is
// taken to be gone.
var movieCastStale = 15 * time.Second

// movieCastPoll is how often the opening process reads the report.
var movieCastPoll = 500 * time.Millisecond

// movieCastStartup is how long a window has to open and write its first
// report.
var movieCastStartup = 30 * time.Second

// movieCastReporter writes a cast window's report.
type movieCastReporter struct {
	path   string
	mu     sync.Mutex
	report movieCastReport
}

func newMovieCastReporter(path string) *movieCastReporter {
	r := &movieCastReporter{path: path}
	r.write()
	return r
}

// progress is MoviePlayback.Progress for a cast window.
func (r *movieCastReporter) progress(position float64, duration int, watched bool) {
	r.mu.Lock()
	r.report.Position, r.report.Duration = position, duration
	r.report.Watched = r.report.Watched || watched
	r.mu.Unlock()
	r.write()
}

// finish marks the cast over, with the error that ended it if any.
func (r *movieCastReporter) finish(err error) {
	r.mu.Lock()
	r.report.Done = true
	if err != nil {
		r.report.Error = err.Error()
	}
	r.mu.Unlock()
	r.write()
}

// heartbeat touches the report until the returned function is called.
func (r *movieCastReporter) heartbeat() func() {
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(movieCastBeat)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-ticker.C:
				_ = os.Chtimes(r.path, now, now)
			}
		}
	}()
	return func() { close(stop) }
}

func (r *movieCastReporter) write() {
	if r.path == "" {
		return
	}
	r.mu.Lock()
	encoded, err := json.Marshal(r.report)
	r.mu.Unlock()
	if err == nil {
		err = writeFileAtomic(r.path, encoded, 0o600)
	}
	if err != nil {
		Log(fmt.Sprintf("movies: could not write the cast report: %v", err))
	}
}

// readMovieCastReport reads a report and when it was last touched.
func readMovieCastReport(path string) (movieCastReport, time.Time, bool) {
	var report movieCastReport
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		return report, time.Time{}, false
	}
	encoded, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(encoded, &report) != nil {
		return report, time.Time{}, false
	}
	return report, info.ModTime(), true
}

// errMovieCastWindow is a cast window that never opened, so the movie is
// cast from here instead.
var errMovieCastWindow = errors.New("cast: the cast window did not open")

// castMovieInWindow casts a movie from a terminal window of its own and waits
// for it to end, reporting whether the server it came from is done with, as
// castMovie does.
func castMovieInWindow(config *Config, anime *Anime) (bool, error) {
	sweepCastSessions(config, time.Hour)
	device, err := castHandOffDevice(config)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(castSessionDir(config), 0o700); err != nil {
		return false, err
	}
	reportFile, err := os.CreateTemp(castSessionDir(config), "movie-*.json")
	if err != nil {
		return false, fmt.Errorf("cast: could not create the report file: %w", err)
	}
	report := reportFile.Name()
	reportFile.Close()
	defer os.Remove(report)

	session := castSessionFor(anime, device)
	session.Movie = &castSessionMovie{Start: anime.Movie.Start, Report: report}
	path, err := writeCastSessionFile(config, session)
	if err != nil {
		return false, err
	}
	command, err := openCastWindow(config, path)
	if err != nil {
		return false, err
	}
	// Some terminals hand the window to a running instance and return at
	// once, so the window's end is read from the report rather than from the
	// terminal exiting; this only reaps it.
	go func() { _ = command.Wait() }()

	result, seen := waitForMovieCast(report)
	if !seen {
		os.Remove(path)
		return false, errMovieCastWindow
	}
	if result.Position >= 1 || result.Watched {
		anime.Movie.Progress(result.Position, result.Duration, result.Watched)
	}
	if result.Error != "" && !result.Watched {
		Log(fmt.Sprintf("movies: cast: %s", result.Error))
		Out("Casting failed: " + result.Error)
		return false, nil
	}
	return true, nil
}

// waitForMovieCast waits until the window's report says it is done, or the
// report stops being touched. It reports false when the window never wrote
// one.
func waitForMovieCast(report string) (movieCastReport, bool) {
	started := time.Now()
	var last movieCastReport
	seen := false
	ticker := time.NewTicker(movieCastPoll)
	defer ticker.Stop()
	for range ticker.C {
		result, touched, ok := readMovieCastReport(report)
		if ok {
			last, seen = result, true
			if result.Done || time.Since(touched) > movieCastStale {
				return last, true
			}
			continue
		}
		if !seen && time.Since(started) > movieCastStartup {
			return last, false
		}
	}
	return last, seen
}

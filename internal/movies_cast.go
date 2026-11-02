package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

// movieCastStale is how long a report can go untouched before its window is
// taken to be gone.
var movieCastStale = 15 * time.Second

// movieCastPoll is how often the opening process reads the report.
var movieCastPoll = 500 * time.Millisecond

// movieCastStartup is how long a window has to open and write its first
// report.
var movieCastStartup = 30 * time.Second

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

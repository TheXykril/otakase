package internal

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// fastMovieCastWait shortens the waits on a cast window for a test.
func fastMovieCastWait(t *testing.T) {
	t.Helper()
	poll, stale, startup := movieCastPoll, movieCastStale, movieCastStartup
	movieCastPoll, movieCastStale, movieCastStartup = 10*time.Millisecond, 300*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { movieCastPoll, movieCastStale, movieCastStartup = poll, stale, startup })
}

// fakeCastWindow stands in for the terminal: it reads the handoff file as the
// window would and plays the movie through window.
func fakeCastWindow(t *testing.T, window func(session *castSessionFile, report *movieCastReporter)) {
	t.Helper()
	open := openCastWindow
	openCastWindow = func(config *Config, path string) (*exec.Cmd, error) {
		session, err := readCastSession(path)
		if err != nil {
			t.Fatalf("readCastSession: %v", err)
		}
		go window(session, newMovieCastReporter(session.Movie.Report))
		command := exec.Command("true")
		return command, command.Start()
	}
	t.Cleanup(func() { openCastWindow = open })
}

type movieProgressCall struct {
	position float64
	duration int
	watched  bool
}

func testMovieCastAnime(calls *[]movieProgressCall) *Anime {
	anime := &Anime{Title: AnimeTitle{English: "Interstellar (2014)"}, TotalEpisodes: 1, Untracked: true}
	anime.Ep.Number = 1
	anime.Ep.Links = []string{"https://example.test/movie.m3u8"}
	anime.Movie = &MoviePlayback{Start: 120, Progress: func(position float64, duration int, watched bool) {
		*calls = append(*calls, movieProgressCall{position, duration, watched})
	}}
	return anime
}

// Under rofi a movie is cast from a terminal window, as an episode is, and
// where it got to comes back to the process keeping the movie history.
func TestMovieCastWindowReportsWhereItGot(t *testing.T) {
	fastMovieCastWait(t)
	config := testCastConfig(t)
	config.CastDevice = "Office TV"
	fakeCastWindow(t, func(session *castSessionFile, report *movieCastReporter) {
		anime := castSessionToAnime(session)
		if anime.Movie == nil || anime.Movie.Start != 120 || !anime.Untracked || session.Device != "Office TV" {
			t.Errorf("the window got %+v, device %q", anime.Movie, session.Device)
		}
		report.progress(600, 7000, false)
		report.finish(nil)
	})

	var calls []movieProgressCall
	done, err := castMovieInWindow(config, testMovieCastAnime(&calls))
	if err != nil || !done {
		t.Fatalf("done = %v, err = %v", done, err)
	}
	if len(calls) != 1 || calls[0] != (movieProgressCall{600, 7000, false}) {
		t.Errorf("progress = %+v", calls)
	}
}

// A server the device could not play leaves the next one to try.
func TestMovieCastWindowFailureTriesTheNextServer(t *testing.T) {
	fastMovieCastWait(t)
	config := testCastConfig(t)
	config.CastDevice = "Office TV"
	fakeCastWindow(t, func(_ *castSessionFile, report *movieCastReporter) {
		report.finish(errors.New("ffmpeg could not open the stream"))
	})

	var calls []movieProgressCall
	done, err := castMovieInWindow(config, testMovieCastAnime(&calls))
	if err != nil || done {
		t.Fatalf("done = %v, err = %v", done, err)
	}
	if len(calls) != 0 {
		t.Errorf("progress saved for a movie that never played: %+v", calls)
	}
}

// Closing the window mid-movie still keeps where it got to.
func TestMovieCastWindowClosedKeepsThePosition(t *testing.T) {
	fastMovieCastWait(t)
	config := testCastConfig(t)
	config.CastDevice = "Office TV"
	fakeCastWindow(t, func(_ *castSessionFile, report *movieCastReporter) {
		report.progress(1500, 7000, false)
	})

	var calls []movieProgressCall
	done, err := castMovieInWindow(config, testMovieCastAnime(&calls))
	if err != nil || !done || len(calls) != 1 || calls[0].position != 1500 {
		t.Fatalf("done = %v, err = %v, progress = %+v", done, err, calls)
	}
}

// A window that never opens leaves the movie to be cast from here.
func TestMovieCastWindowThatNeverOpens(t *testing.T) {
	fastMovieCastWait(t)
	config := testCastConfig(t)
	config.CastDevice = "Office TV"
	open := openCastWindow
	openCastWindow = func(*Config, string) (*exec.Cmd, error) {
		command := exec.Command("true")
		return command, command.Start()
	}
	t.Cleanup(func() { openCastWindow = open })

	var calls []movieProgressCall
	if _, err := castMovieInWindow(config, testMovieCastAnime(&calls)); !errors.Is(err, errMovieCastWindow) {
		t.Fatalf("err = %v", err)
	}
}

// A movie's panel has the anime's keys less the audio switch, and no episode
// number: the title already says which episode it is.
func TestCastPanelForAMovie(t *testing.T) {
	var calls []movieProgressCall
	episode, keys := castPanelLabels(testMovieCastAnime(&calls))
	if episode != 0 || strings.Contains(keys, "audio") || !strings.Contains(keys, "q stop") {
		t.Errorf("episode = %d, keys = %q", episode, keys)
	}
	state := testPanelState()
	state.Episode, state.Keys = episode, keys
	rendered := strings.Join(castPanelLines(state, 60), "\n")
	if strings.Contains(rendered, "Ep ") || strings.Contains(rendered, "audio") {
		t.Errorf("movie panel:\n%s", rendered)
	}
	if episode, keys := castPanelLabels(testCastAnime()); episode == 0 || keys != castPanelPlaybackKeys {
		t.Errorf("anime: episode = %d, keys = %q", episode, keys)
	}
}

// MovieSite is gone; a config that has it loses it instead of being warned
// about it on every run.
func TestRetiredMovieSiteIsDropped(t *testing.T) {
	onDisk := map[string]string{"MovieSite": "https://8filmai.eu", "Player": "mpv"}
	if !upgradeStoredValues(onDisk) {
		t.Error("nothing changed")
	}
	if _, ok := onDisk["MovieSite"]; ok || onDisk["Player"] != "mpv" {
		t.Errorf("config = %v", onDisk)
	}
	if len(UnknownConfigKeys(onDisk)) != 0 {
		t.Errorf("unknown keys left: %v", UnknownConfigKeys(onDisk))
	}
}

// rofi's Movies menu has the anime list's toolbar: the same buttons on the
// same keys, the cast one lit when casting is on, and Back where the main
// list has Quit.
func TestMoviesRofiToolbar(t *testing.T) {
	config := &Config{CastToDevice: true, MovieProvider: "all"}
	lib := &movieLibrary{config: config}
	bar := mainRofiToolbar(config, []Tab{{Key: movieContinueKey}, {Key: movieWatchlistKey}}, movieActions(config, lib))
	bar.back = true
	want := map[string]string{movieSearchKey: "Control+f", movieProviderKey: "Control+o", movieCastKey: "Control+k", movieTraktKey: "Control+t"}
	for _, button := range bar.buttons {
		if binding, ok := want[button.Key]; ok {
			if button.Binding != binding {
				t.Errorf("%s bound to %q, want %q", button.Key, button.Binding, binding)
			}
			if button.On != (button.Key == movieCastKey) {
				t.Errorf("%s on = %v", button.Key, button.On)
			}
			if strings.TrimSpace(button.Label) == "" {
				t.Errorf("%s has no label", button.Key)
			}
			delete(want, button.Key)
		}
	}
	if len(want) != 0 {
		t.Errorf("no button for %v", want)
	}
	if !strings.Contains(bar.lastLabel(), "Back") {
		t.Errorf("last button = %q", bar.lastLabel())
	}
	if args := strings.Join(bar.args([]string{"inputbar", "box-toolbar", "listview"}), " "); !strings.Contains(args, "-kb-move-char-forward Right") {
		t.Error("ctrl+f is left bound to rofi's cursor move, so rofi would refuse to start")
	}
}

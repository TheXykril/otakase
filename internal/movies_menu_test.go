package internal

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/thexykril/otakase/internal/movies"
)

// Movies is experimental: nothing of it shows until it is switched on, and
// then it shows without also having to be added to MenuActions.
func TestMoviesActionFollowsTheExperimentSwitch(t *testing.T) {
	config := PopulateConfig(map[string]string{})
	if config.ExperimentalMovies {
		t.Fatal("ExperimentalMovies should be off by default")
	}
	if config.MovieSite != "https://8filmai.eu" {
		t.Errorf("MovieSite default = %q", config.MovieSite)
	}

	hasMovies := func() bool {
		_, actions := mainMenuLayout(&config, AnimeList{})
		for _, action := range actions {
			if action.Key == "MOVIES" {
				return true
			}
		}
		return false
	}
	if hasMovies() {
		t.Error("MOVIES offered with the experiment off")
	}
	config.MenuActions += ",MOVIES"
	if hasMovies() {
		t.Error("MOVIES offered with the experiment off, even when MenuActions names it")
	}

	config.ExperimentalMovies = true
	config.MenuActions = "STATS"
	if !hasMovies() {
		t.Error("MOVIES not offered with the experiment on")
	}
	if toolbarButtonLabels["MOVIES"] == "" || categoryIcon("MOVIES") == 0 {
		t.Error("MOVIES needs a toolbar label and an icon")
	}
}

func TestExperimentalMoviesIsAddedToOldConfigs(t *testing.T) {
	config := map[string]string{"Player": "mpv"}
	added := injectMissingConfigOptions(config)
	found := map[string]bool{}
	for _, key := range added {
		found[key] = true
	}
	if !found["ExperimentalMovies"] || config["ExperimentalMovies"] != "false" {
		t.Errorf("ExperimentalMovies not added as false: %v", added)
	}
	if !found["MovieSite"] {
		t.Errorf("MovieSite not added: %v", added)
	}
}

func TestMovieDownloadPath(t *testing.T) {
	got := movieDownloadPath("/dl", movies.Movie{Title: "Alkis", Year: "2026"}, DownloadFormatMKV)
	if got != filepath.Join("/dl", "Alkis (2026).mkv") {
		t.Errorf("path = %q", got)
	}
}

// A movie host's link has no extension; marked as a file, it is copied as
// one rather than read as HLS.
func TestMovieDownloadIsAWholeFile(t *testing.T) {
	job := ffmpegJob{Stream: "https://streamtape.com/get_video?id=x&stream=1", Output: "/dl/a.mkv", File: true}
	args := strings.Join(job.args(), " ")
	if strings.Contains(args, "aac_adtstoasc") || strings.Contains(args, "allowed_extensions") {
		t.Errorf("treated as HLS: %s", args)
	}
}

// A cast of a movie starts where the movie history says, though it is
// untracked.
func TestCastResumesAMovie(t *testing.T) {
	anime := &Anime{Untracked: true, Movie: &MoviePlayback{Start: 600}}
	if got := castResumeAt(&Config{}, anime); got != 600 {
		t.Errorf("resume at %v", got)
	}
}

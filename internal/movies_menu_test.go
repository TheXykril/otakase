package internal

import "testing"

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

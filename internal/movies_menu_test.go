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

// The terminal menu opens on the first list holding something, with search
// always the first row and the footer's keys offered.
func TestMovieTabsOpenOnAFilledList(t *testing.T) {
	store, err := movies.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetWatchlist(movies.Movie{Path: "/filmas/a/", Title: "A"}, true); err != nil {
		t.Fatal(err)
	}
	var shown []SelectionOption
	var seen *SelectionRefreshConfig
	old := movieSelectWithTabs
	movieSelectWithTabs = func(options []SelectionOption, refresh *SelectionRefreshConfig) (SelectionOption, error) {
		shown, seen = options, refresh
		return options[1], nil
	}
	defer func() { movieSelectWithTabs = old }()

	picked, ok, active := pickFromMovieTabs(&Config{}, store, &movieLibrary{providers: map[string]movies.Provider{}}, "")
	if !ok || active != movieWatchlistKey || picked.Key != moviePathPrefix+"8filmai:/filmas/a/" {
		t.Fatalf("picked %+v ok=%v active=%q", picked, ok, active)
	}
	if shown[0].Key != movieSearchKey || len(seen.Categories) != 3 || len(seen.Actions) != 4 {
		t.Errorf("rows %+v, tabs %d, actions %d", shown, len(seen.Categories), len(seen.Actions))
	}
}

// The Trakt code stays on screen until something replaces it under the same
// tag: no expiry, critical urgency.
func TestTraktCodeNotificationStays(t *testing.T) {
	var sent [][]string
	old := notifySendRun
	notifySendRun = func(args []string) (string, error) {
		sent = append(sent, args)
		return "42\n", nil
	}
	defer func() { notifySendRun = old }()

	if err := sendLinuxStickyNotification(notifyTagTrakt, "enter ABC"); err != nil {
		t.Fatal(err)
	}
	if err := sendLinuxNotification(notifyTagTrakt, "", "signed in"); err != nil {
		t.Fatal(err)
	}
	first, second := strings.Join(sent[0], " "), strings.Join(sent[1], " ")
	if !strings.Contains(first, "-t 0") || !strings.Contains(first, "-u critical") {
		t.Errorf("code notification expires: %s", first)
	}
	if !strings.Contains(second, "-r 42") || strings.Contains(second, "critical") {
		t.Errorf("result does not replace it: %s", second)
	}
}

// A movie's HLS segments are named like web pages; mpv is told to accept
// them, and the anime audio preference does not apply.
func TestMovieMPVArgs(t *testing.T) {
	args := strings.Join(movieMPVArgs(movies.Stream{URL: "https://h/master.m3u8", HLS: true, Referrer: "https://h/"}), " ")
	for _, want := range []string{"--alang=en,eng", "extension_picky=0", "allowed_extensions=ALL"} {
		if !strings.Contains(args, want) {
			t.Errorf("%q missing from %s", want, args)
		}
	}
	if args := strings.Join(movieMPVArgs(movies.Stream{URL: "https://h/v.mp4", AudioLanguage: "lt"}), " "); args != "--alang=lt --referrer=" {
		t.Errorf("file args = %s", args)
	}
}

func TestMovieSourceLabel(t *testing.T) {
	for _, c := range []struct {
		entry movies.Entry
		want  string
	}{
		{movies.Entry{Movie: movies.Movie{Provider: "vidsrc"}, Server: "vidsrc 2"}, "vidsrc 2"},
		{movies.Entry{Movie: movies.Movie{Provider: "8filmai"}, Server: "imgsto.re"}, "8filmai, imgsto.re"},
		{movies.Entry{Movie: movies.Movie{Provider: "filmukas"}}, "filmukas"},
	} {
		if got := movieSourceLabel(c.entry); got != c.want {
			t.Errorf("%+v: %q, want %q", c.entry, got, c.want)
		}
	}
}

// Finishing a movie asks for a rating once; stopping part way keeps it on
// the watchlist.
func TestAfterMoviePlay(t *testing.T) {
	cases := []struct {
		name            string
		before, after   movies.Entry
		score           bool
		rate, watchlist bool
	}{
		{"finished", movies.Entry{}, movies.Entry{Watched: true}, true, true, false},
		{"finished, rating off", movies.Entry{}, movies.Entry{Watched: true}, false, false, false},
		{"finished, already rated", movies.Entry{}, movies.Entry{Watched: true, Rating: 7}, true, false, false},
		{"rewatched", movies.Entry{Watched: true}, movies.Entry{Watched: true}, true, false, false},
		{"stopped part way", movies.Entry{}, movies.Entry{Position: 600}, true, false, true},
		{"already on the watchlist", movies.Entry{}, movies.Entry{Position: 600, Watchlist: true}, true, false, false},
		{"closed at once", movies.Entry{}, movies.Entry{}, true, false, false},
	}
	for _, c := range cases {
		rate, watchlist := afterMoviePlay(c.before, c.after, c.score)
		if rate != c.rate || watchlist != c.watchlist {
			t.Errorf("%s: got rate=%v watchlist=%v, want %v %v", c.name, rate, watchlist, c.rate, c.watchlist)
		}
	}
}

// Searching all providers is the default, and an unknown name falls back to it.
func TestMovieSearchesAllByDefault(t *testing.T) {
	lib := &movieLibrary{providers: map[string]movies.Provider{}}
	for _, value := range []string{"", "all", "nonsense"} {
		if !lib.searchesAll(&Config{MovieProvider: value}) {
			t.Errorf("MovieProvider %q should search all", value)
		}
	}
	if lib.searchesAll(&Config{MovieProvider: "vidsrc"}) {
		t.Error("vidsrc should search vidsrc only")
	}
	if config := PopulateConfig(map[string]string{}); config.MovieProvider != "all" {
		t.Errorf("default MovieProvider = %q", config.MovieProvider)
	}
}

func TestMovieDetailsText(t *testing.T) {
	lines := movieDetailsText(movies.Details{
		Runtime: 169, Genres: []string{"science-fiction", "drama"}, Certification: "PG-13", Rating: 8.886,
		Overview: strings.Repeat("word ", 30),
	})
	if lines[0] != "2h 49m · Science fiction, Drama · PG-13 · 8.9/10 on Trakt" {
		t.Errorf("facts line = %q", lines[0])
	}
	if len(lines) != 3 {
		t.Errorf("overview should wrap into two lines: %q", lines)
	}
	if got := movieDetailsText(movies.Details{}); len(got) != 0 {
		t.Errorf("no details should give no lines: %q", got)
	}
}

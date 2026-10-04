package internal

import (
	"fmt"
	"time"

	"github.com/thexykril/otakase/internal/icons"
	"github.com/thexykril/otakase/internal/movies"
)

// Movies is an experimental section of its own, reached from the main menu's
// MOVIES action when ExperimentalMovies is on. Movies are not on AniList or
// MyAnimeList, so they are searched on their own site and their history is
// kept in a file beside the anime history, never synced anywhere.

const moviesSection = "Movies (experimental)"

// Keys of the Movies menus. Prefixed, so a movie path can never be mistaken
// for one of them.
const (
	movieSearchKey    = "MOVIE:SEARCH"
	movieContinueKey  = "MOVIE:CONTINUE"
	movieWatchlistKey = "MOVIE:WATCHLIST"
	movieHistoryKey   = "MOVIE:HISTORY"
	moviePlayKey      = "MOVIE:PLAY"
	movieRestartKey   = "MOVIE:RESTART"
	movieListOnKey    = "MOVIE:LIST_ON"
	movieListOffKey   = "MOVIE:LIST_OFF"
	movieForgetKey    = "MOVIE:FORGET"
	movieBackKey      = "back"
	moviePathPrefix   = "MOVIE_PATH:"
)

// movieSelect is the menu the Movies section draws with; tests replace it.
var movieSelect = DynamicSelectPreserveOrder

// WatchMovies runs the Movies section until the viewer backs out of it.
func WatchMovies(config *Config) {
	if config == nil || !config.ExperimentalMovies {
		return
	}
	store, err := movies.OpenStore(GetStoragePath())
	if err != nil {
		// A history that cannot be read is not a reason to refuse to play:
		// it starts empty, and the file is left alone until something is
		// saved.
		Log(fmt.Sprintf("movies: could not read the movie history: %v", err))
	}
	site := movies.NewSite(config.MovieSite, store.Site(), func(address string) {
		if err := store.SetSite(address); err != nil {
			Log(fmt.Sprintf("movies: could not remember the site address: %v", err))
		}
	})

	for {
		options := []SelectionOption{
			{Key: movieSearchKey, Label: "Search movies", Icon: icons.Search},
		}
		if n := len(store.Continue()); n > 0 {
			options = append(options, SelectionOption{Key: movieContinueKey, Label: fmt.Sprintf("Continue watching (%d)", n), Icon: icons.Play})
		}
		if n := len(store.Watchlist()); n > 0 {
			options = append(options, SelectionOption{Key: movieWatchlistKey, Label: fmt.Sprintf("Watchlist (%d)", n), Icon: icons.Planning})
		}
		if n := len(store.History()); n > 0 {
			options = append(options, SelectionOption{Key: movieHistoryKey, Label: fmt.Sprintf("History (%d)", n), Icon: icons.History})
		}
		options = append(options, SelectionOption{Key: movieBackKey, Label: "Back to menu", Icon: icons.Back})

		picked, ok := pickMovieOption(options)
		if !ok {
			return
		}
		switch picked.Key {
		case movieSearchKey:
			searchMovies(config, store, site)
		case movieContinueKey:
			pickFromMovieList(config, store, site, store.Continue)
		case movieWatchlistKey:
			pickFromMovieList(config, store, site, store.Watchlist)
		case movieHistoryKey:
			pickFromMovieList(config, store, site, store.History)
		default:
			return
		}
		ClearScreen()
	}
}

// pickMovieOption shows a menu and reports false when the viewer backs out.
// Quit quits, as it does everywhere.
func pickMovieOption(options []SelectionOption) (SelectionOption, bool) {
	picked, err := movieSelect(options)
	if err != nil {
		Log(fmt.Sprintf("movies: menu failed: %v", err))
		return SelectionOption{}, false
	}
	picked = NormalizeSelectionKey(picked)
	if SelectionMeansQuit(picked) {
		Exit(nil)
	}
	if picked.Key == "-2" || picked.Key == movieBackKey || picked.Key == "" {
		return SelectionOption{}, false
	}
	return picked, true
}

// searchMovies asks for a title and offers what the site finds, until the
// viewer backs out of the question.
func searchMovies(config *Config, store *movies.Store, site *movies.Site) {
	for {
		query, cancelled, err := promptCancelable(config, moviesSection,
			"Search for a movie", "enter to search · esc to go back")
		if err != nil || cancelled {
			return
		}
		found, err := site.Search(query)
		if err != nil {
			Log(fmt.Sprintf("movies: search for %q failed: %v", query, err))
			Out(fmt.Sprintf("Could not search for %q: %v", query, err))
			continue
		}
		if len(found) == 0 {
			Out(fmt.Sprintf("No movies found for %q.", query))
			continue
		}
		options := make([]SelectionOption, 0, len(found)+1)
		byKey := map[string]movies.Movie{}
		for _, movie := range found {
			key := moviePathPrefix + movie.Path
			byKey[key] = movie
			options = append(options, movieRow(movie, store))
		}
		options = append(options, SelectionOption{Key: movieBackKey, Label: "Back", Icon: icons.Back})
		picked, ok := pickMovieOption(options)
		if !ok {
			continue
		}
		if movie, ok := byKey[picked.Key]; ok {
			openMovie(config, store, site, movie)
			return
		}
	}
}

// movieRow is a movie as a menu row, saying where it was left when it was.
func movieRow(movie movies.Movie, store *movies.Store) SelectionOption {
	label := movie.Label()
	icon := icons.TV
	if entry, ok := store.Get(movie.Path); ok {
		switch {
		case entry.Started():
			label += " · stopped at " + formatClock(entry.Position)
			icon = icons.Play
		case entry.Watched:
			label += " · watched"
			icon = icons.Completed
		}
	}
	return SelectionOption{Key: moviePathPrefix + movie.Path, Label: label, Title: movie.Title, Thumbnail: movie.Poster, Icon: icon}
}

// pickFromMovieList offers one of the remembered lists.
func pickFromMovieList(config *Config, store *movies.Store, site *movies.Site, list func() []movies.Entry) {
	for {
		entries := list()
		if len(entries) == 0 {
			return
		}
		options := make([]SelectionOption, 0, len(entries)+1)
		byKey := map[string]movies.Movie{}
		for _, entry := range entries {
			row := movieRow(entry.Movie, store)
			byKey[row.Key] = entry.Movie
			options = append(options, row)
		}
		options = append(options, SelectionOption{Key: movieBackKey, Label: "Back", Icon: icons.Back})
		picked, ok := pickMovieOption(options)
		if !ok {
			return
		}
		if movie, ok := byKey[picked.Key]; ok {
			openMovie(config, store, site, movie)
		}
		ClearScreen()
	}
}

// openMovie offers what can be done with one movie: play it (or carry on
// where it stopped), keep it for later, or forget it.
func openMovie(config *Config, store *movies.Store, site *movies.Site, movie movies.Movie) {
	for {
		entry, known := store.Get(movie.Path)
		options := []SelectionOption{}
		if entry.Started() {
			options = append(options,
				SelectionOption{Key: moviePlayKey, Label: "Continue from " + formatClock(entry.Position), Icon: icons.Play},
				SelectionOption{Key: movieRestartKey, Label: "Play from the start", Icon: icons.Refresh})
		} else {
			options = append(options, SelectionOption{Key: moviePlayKey, Label: "Play", Icon: icons.Play})
		}
		if entry.Watchlist {
			options = append(options, SelectionOption{Key: movieListOffKey, Label: "Remove from watchlist", Icon: icons.No})
		} else if !entry.Watched {
			options = append(options, SelectionOption{Key: movieListOnKey, Label: "Add to watchlist", Icon: icons.Add})
		}
		if known {
			options = append(options, SelectionOption{Key: movieForgetKey, Label: "Remove from history", Icon: icons.Dropped})
		}
		options = append(options, SelectionOption{Key: movieBackKey, Label: "Back", Icon: icons.Back})

		ClearScreen()
		Out(movie.Label())
		picked, ok := pickMovieOption(options)
		if !ok {
			return
		}
		switch picked.Key {
		case moviePlayKey:
			playMovie(config, store, site, movie, entry.Position)
			return
		case movieRestartKey:
			playMovie(config, store, site, movie, 0)
			return
		case movieListOnKey, movieListOffKey:
			if err := store.SetWatchlist(movie, picked.Key == movieListOnKey); err != nil {
				Log(fmt.Sprintf("movies: could not save the watchlist: %v", err))
			}
		case movieForgetKey:
			if err := store.Remove(movie.Path); err != nil {
				Log(fmt.Sprintf("movies: could not forget %s: %v", movie.Path, err))
			}
			return
		}
	}
}

// playMovie plays a movie in mpv from start seconds, trying its servers in
// turn until one plays, and records where it stopped.
func playMovie(config *Config, store *movies.Store, site *movies.Site, movie movies.Movie, start int) {
	page, err := site.Page(movie.Path)
	if err != nil {
		Log(fmt.Sprintf("movies: %s: %v", movie.Path, err))
		Out(fmt.Sprintf("Could not open %s: %v", movie.Label(), err))
		awaitEnterNotice()
		return
	}
	title := page.Label()
	Out(fmt.Sprintf("Loading %s…", title))

	var anime Anime
	for _, source := range site.Sources(page) {
		stream, err := source.Resolve()
		if err != nil {
			Log(fmt.Sprintf("movies: %s: %s: %v", page.Path, source.Server, err))
			continue
		}
		Log(fmt.Sprintf("movies: %s: playing from %s", page.Path, stream.Server))
		anime.Ep.StreamReferrer = stream.Referrer
		args := []string{}
		if start > 0 {
			// A few seconds back, so the line it stopped on is heard again.
			args = append(args, fmt.Sprintf("--start=%d", max(start-5, 0)))
		}
		socket, err := StartVideo(stream.URL, args, title, &anime)
		if err != nil {
			Log(fmt.Sprintf("movies: %s: could not start the player: %v", stream.Server, err))
			continue
		}
		anime.Ep.Player.SocketPath = socket
		if socket == "android-intent" {
			Out("Opened the movie in mpv. Press Enter when you have finished watching...")
			AwaitEnter()
			return
		}
		if !WaitForMPVPlaybackStart(socket, MpvPlaybackStartTimeoutDuration(config)) {
			Log(fmt.Sprintf("movies: %s did not start playing", stream.Server))
			if IsMPVRunning(socket) {
				ExitMPV(socket)
			}
			anime.Ep.Player.SocketPath = ""
			continue
		}
		watchMoviePlayback(config, store, page.Movie, socket)
		return
	}
	Out(fmt.Sprintf("None of the servers for %s could be played. The movie may have been taken down; try again later.", title))
	awaitEnterNotice()
}

// watchMoviePlayback follows playback until the player closes or the movie
// ends, saving the position as it goes so a crash loses little.
func watchMoviePlayback(config *Config, store *movies.Store, movie movies.Movie, socket string) {
	position, duration := 0, 0
	lastSaved := time.Now()
	save := func() {
		watched := PercentageWatched(position, duration) >= float64(config.PercentageToMarkComplete)
		if err := store.SetProgress(movie, position, duration, watched); err != nil {
			Log(fmt.Sprintf("movies: could not save progress: %v", err))
		}
	}
	for {
		value, err := MPVSendCommand(socket, []interface{}{"get_property", "time-pos"})
		if err != nil {
			if isMPVConnectionGoneError(err) {
				save()
				return
			}
		} else if seconds, ok := value.(float64); ok {
			position = int(seconds + 0.5)
		}
		if duration == 0 {
			if value, err := MPVSendCommand(socket, []interface{}{"get_property", "duration"}); err == nil {
				if seconds, ok := value.(float64); ok {
					duration = int(seconds + 0.5)
				}
			}
		}
		// The player stays open at the end (--idle), so the end is it going
		// idle rather than it closing. Stopping without closing it idles it
		// too, so how far it got still decides whether it was watched.
		if idle, err := MPVSendCommand(socket, []interface{}{"get_property", "idle-active"}); err == nil {
			if done, ok := idle.(bool); ok && done {
				save()
				ExitMPV(socket)
				return
			}
		}
		if time.Since(lastSaved) > 15*time.Second && position > 0 {
			save()
			lastSaved = time.Now()
		}
		time.Sleep(time.Second)
	}
}

// awaitEnterNotice leaves a message on screen until the viewer has read it.
func awaitEnterNotice() {
	Out("Press Enter to go back.")
	AwaitEnter()
}

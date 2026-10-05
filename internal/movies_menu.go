package internal

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/icons"
	"github.com/thexykril/otakase/internal/movies"
	"github.com/thexykril/otakase/internal/providers"
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
	movieDownloadKey  = "MOVIE:DOWNLOAD"
	movieCastKey      = "MOVIE:CAST"
	movieWatchedKey   = "MOVIE:WATCHED"
	movieUnwatchedKey = "MOVIE:UNWATCHED"
	movieRateKey      = "MOVIE:RATE"
	movieProviderKey  = "MOVIE:PROVIDER"
	movieTraktKey     = "MOVIE:TRAKT"
	movieBackKey      = "back"
	moviePathPrefix   = "MOVIE_PATH:"
)

// movieSelect is the menu the Movies section draws with; tests replace it.
var movieSelect = DynamicSelectPreserveOrder

// movieSelectPreview draws the poster grid in rofi; tests replace it.
var movieSelectPreview = DynamicSelectPreview

// movieSelectWithTabs draws the terminal menu with tabs; tests replace it.
var movieSelectWithTabs = DynamicSelectWithRefresh

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
	lib := &movieLibrary{site: site, providers: map[string]movies.Provider{}, trakt: newMovieTrakt(config)}
	store.OnChange = func(before, after movies.Entry) { syncMovieToTrakt(lib, store, before, after) }

	active := ""
	for {
		var picked SelectionOption
		var ok bool
		if config.RofiSelection {
			picked, ok = pickMovieOption(movieHubOptions(config, store, lib))
		} else {
			picked, ok, active = pickFromMovieTabs(config, store, lib, active)
		}
		if !ok {
			return
		}
		switch picked.Key {
		case movieSearchKey:
			searchMovies(config, store, lib)
		case movieCastKey:
			toggleCastToDevice(config)
		case movieProviderKey:
			pickMovieProvider(config, lib)
		case movieTraktKey:
			manageMovieTrakt(config, lib.trakt)
		case movieContinueKey:
			pickFromMovieList(config, store, lib, store.Continue)
		case movieWatchlistKey:
			pickFromMovieList(config, store, lib, store.Watchlist)
		case movieHistoryKey:
			pickFromMovieList(config, store, lib, store.History)
		default:
			if path, found := strings.CutPrefix(picked.Key, moviePathPrefix); found {
				if entry, known := store.Get(path); known {
					openMovie(config, store, lib, entry.Movie)
				}
				break
			}
			return
		}
		ClearScreen()
	}
}

// movieLists are the remembered lists, in tab order.
func movieLists(store *movies.Store) []struct {
	tab  Tab
	list func() []movies.Entry
} {
	return []struct {
		tab  Tab
		list func() []movies.Entry
	}{
		{Tab{Key: movieContinueKey, Label: "Continue", Icon: icons.Play, Count: len(store.Continue())}, store.Continue},
		{Tab{Key: movieWatchlistKey, Label: "Watchlist", Icon: icons.Planning, Count: len(store.Watchlist())}, store.Watchlist},
		{Tab{Key: movieHistoryKey, Label: "History", Icon: icons.History, Count: len(store.History())}, store.History},
	}
}

// movieActions are the footer's keys in the terminal menu.
func movieActions(config *Config, lib *movieLibrary) []FooterAction {
	return []FooterAction{
		{Key: movieSearchKey, Label: "search", Hint: "ctrl+f"},
		{Key: movieProviderKey, Label: "provider: " + lib.current(config).Name(), Hint: "ctrl+o"},
		{Key: movieCastKey, Label: castActionCheckbox(config), Hint: "ctrl+k"},
		{Key: movieTraktKey, Label: traktActionLabel(lib.trakt), Hint: "ctrl+t"},
	}
}

// pickFromMovieTabs is the terminal's Movies menu: the remembered lists as
// tabs, as the anime lists are, with search and the cast switch in the
// footer. It reports the tab it was left on, so coming back opens it again.
func pickFromMovieTabs(config *Config, store *movies.Store, lib *movieLibrary, active string) (SelectionOption, bool, string) {
	lists := movieLists(store)
	tabs := make([]Tab, 0, len(lists))
	for _, list := range lists {
		tabs = append(tabs, list.tab)
	}
	if active == "" {
		// The first list with something in it.
		active = movieContinueKey
		for _, list := range lists {
			if list.tab.Count > 0 {
				active = list.tab.Key
				break
			}
		}
	}
	load := func(key string) []SelectionOption {
		active = key
		options := []SelectionOption{{Key: movieSearchKey, Label: "Search movies", Icon: icons.Search}}
		for _, list := range lists {
			if list.tab.Key != key {
				continue
			}
			for _, entry := range list.list() {
				options = append(options, movieRow(entry.Movie, store))
			}
		}
		return options
	}
	picked, err := movieSelectWithTabs(load(active), &SelectionRefreshConfig{
		Prompt:         moviesSection,
		Categories:     tabs,
		ActiveCategory: active,
		Actions:        movieActions(config, lib),
		LoadCategory:   load,
	})
	if err != nil {
		Log(fmt.Sprintf("movies: menu failed: %v", err))
		return SelectionOption{}, false, active
	}
	picked = NormalizeSelectionKey(picked)
	if SelectionMeansQuit(picked) {
		Exit(nil)
	}
	if picked.Key == "-2" || picked.Key == movieBackKey || picked.Key == "" {
		return SelectionOption{}, false, active
	}
	return picked, true, active
}

// movieHubOptions is the Movies menu under rofi, which has no tabs: the lists
// are rows, as is the cast switch.
func movieHubOptions(config *Config, store *movies.Store, lib *movieLibrary) []SelectionOption {
	options := []SelectionOption{{Key: movieSearchKey, Label: "Search movies", Icon: icons.Search}}
	labels := map[string]string{movieContinueKey: "Continue watching", movieWatchlistKey: "Watchlist", movieHistoryKey: "History"}
	for _, list := range movieLists(store) {
		if list.tab.Count > 0 {
			options = append(options, SelectionOption{Key: list.tab.Key, Label: fmt.Sprintf("%s (%d)", labels[list.tab.Key], list.tab.Count), Icon: list.tab.Icon})
		}
	}
	castIcon := icons.CastOff
	if config.CastToDevice {
		castIcon = icons.Cast
	}
	options = append(options,
		SelectionOption{Key: movieProviderKey, Label: "Provider: " + lib.current(config).Label(), Icon: icons.Provider},
		SelectionOption{Key: movieCastKey, Label: castActionLabel(config), Icon: castIcon},
		SelectionOption{Key: movieTraktKey, Label: "Trakt: " + strings.TrimPrefix(traktActionLabel(lib.trakt), "trakt: "), Icon: icons.Tracker})
	return options
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
func searchMovies(config *Config, store *movies.Store, lib *movieLibrary) {
	for {
		query, cancelled, err := promptCancelable(config, moviesSection,
			"Search for a movie", "enter to search · esc to go back")
		if err != nil || cancelled {
			return
		}
		provider := lib.current(config)
		found, err := provider.Search(query)
		if err != nil {
			Log(fmt.Sprintf("movies: %s: search for %q failed: %v", provider.Name(), query, err))
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
			key := moviePathPrefix + movie.Key()
			byKey[key] = movie
			options = append(options, movieRow(movie, store))
		}
		picked, ok := pickMovieRows(config, options)
		if !ok {
			continue
		}
		if movie, ok := byKey[picked.Key]; ok {
			openMovie(config, store, lib, movie)
			return
		}
	}
}

// movieRow is a movie as a menu row, saying where it was left when it was.
func movieRow(movie movies.Movie, store *movies.Store) SelectionOption {
	label := movie.Label()
	icon := icons.TV
	if entry, ok := store.Get(movie.Key()); ok {
		switch {
		case entry.Started():
			label += " · stopped at " + formatClock(entry.Position)
			icon = icons.Play
		case entry.Watched:
			label += " · watched"
			icon = icons.Completed
		}
		if entry.Rating > 0 {
			label += fmt.Sprintf(" · %d/10", entry.Rating)
		}
		// Where it was watched, since the same film can be on several
		// providers.
		if where := movieSourceLabel(entry); where != "" {
			label += " · " + where
		}
	}
	return SelectionOption{Key: moviePathPrefix + movie.Key(), Label: label, Title: movie.Title, Thumbnail: movie.Poster, Icon: icon}
}

// movieSourceLabel names a remembered movie's provider and, when it played,
// the server: "vidsrc 2", "8filmai, imgsto.re".
func movieSourceLabel(entry movies.Entry) string {
	provider := cmp.Or(entry.Provider, movies.FilmaiName)
	switch {
	case entry.Server == "":
		return provider
	case strings.HasPrefix(strings.ToLower(entry.Server), provider):
		return entry.Server
	}
	return provider + ", " + entry.Server
}

// pickMovieRows shows a list of movies, with their posters in rofi when
// ImagePreview is on, as the anime lists have.
func pickMovieRows(config *Config, options []SelectionOption) (SelectionOption, bool) {
	if !config.RofiSelection || !config.ImagePreview {
		return pickMovieOption(options)
	}
	previews := map[string]RofiSelectPreview{}
	for i, option := range options {
		previews[option.Key] = RofiSelectPreview{Title: option.Label, CoverImage: option.Thumbnail, Rank: i}
	}
	picked, err := movieSelectPreview(previews, false)
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

// pickMovieRating asks for a score out of 10, or 0 to clear the one given.
func pickMovieRating(current int) (int, bool) {
	options := []SelectionOption{}
	for score := 10; score >= 1; score-- {
		options = append(options, SelectionOption{Key: strconv.Itoa(score), Label: fmt.Sprintf("%d/10", score), Icon: icons.Star})
	}
	if current > 0 {
		options = append(options, SelectionOption{Key: "0", Label: "Clear rating", Icon: icons.No})
	}
	picked, ok := pickMovieOption(options)
	if !ok {
		return 0, false
	}
	score, err := strconv.Atoi(picked.Key)
	return score, err == nil
}

// pickFromMovieList offers one of the remembered lists.
func pickFromMovieList(config *Config, store *movies.Store, lib *movieLibrary, list func() []movies.Entry) {
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
		picked, ok := pickMovieRows(config, options)
		if !ok {
			return
		}
		if movie, ok := byKey[picked.Key]; ok {
			openMovie(config, store, lib, movie)
		}
		ClearScreen()
	}
}

// openMovie offers what can be done with one movie: play it (or carry on
// where it stopped), keep it for later, or forget it.
func openMovie(config *Config, store *movies.Store, lib *movieLibrary, movie movies.Movie) {
	for {
		entry, known := store.Get(movie.Key())
		options := []SelectionOption{}
		if entry.Started() {
			options = append(options,
				SelectionOption{Key: moviePlayKey, Label: "Continue from " + formatClock(entry.Position), Icon: icons.Play},
				SelectionOption{Key: movieRestartKey, Label: "Play from the start", Icon: icons.Refresh})
		} else {
			options = append(options, SelectionOption{Key: moviePlayKey, Label: "Play", Icon: icons.Play})
		}
		if entry.Watched {
			options = append(options, SelectionOption{Key: movieUnwatchedKey, Label: "Mark as not watched", Icon: icons.Undo})
		} else {
			options = append(options, SelectionOption{Key: movieWatchedKey, Label: "Mark as watched", Icon: icons.Completed})
		}
		rateLabel := "Rate"
		if entry.Rating > 0 {
			rateLabel = fmt.Sprintf("Rating: %d/10", entry.Rating)
		}
		options = append(options,
			SelectionOption{Key: movieRateKey, Label: rateLabel, Icon: icons.Star},
			SelectionOption{Key: movieDownloadKey, Label: "Download", Icon: icons.Download})
		if entry.Watchlist {
			options = append(options, SelectionOption{Key: movieListOffKey, Label: "Remove from watchlist", Icon: icons.No})
		} else if !entry.Watched {
			options = append(options, SelectionOption{Key: movieListOnKey, Label: "Add to watchlist", Icon: icons.Add})
		}
		if known {
			options = append(options, SelectionOption{Key: movieForgetKey, Label: "Remove from history", Icon: icons.Dropped})
		}

		ClearScreen()
		Out(movie.Label())
		picked, ok := pickMovieOption(options)
		if !ok {
			return
		}
		switch picked.Key {
		case moviePlayKey:
			playMovie(config, store, lib, movie, entry.Position)
			return
		case movieRestartKey:
			playMovie(config, store, lib, movie, 0)
			return
		case movieDownloadKey:
			downloadMovie(config, lib, movie)
		case movieWatchedKey, movieUnwatchedKey:
			if err := store.SetWatched(movie, picked.Key == movieWatchedKey); err != nil {
				Log(fmt.Sprintf("movies: could not save: %v", err))
			}
		case movieRateKey:
			if rating, ok := pickMovieRating(entry.Rating); ok {
				if err := store.SetRating(movie, rating); err != nil {
					Log(fmt.Sprintf("movies: could not save the rating: %v", err))
				}
			}
		case movieListOnKey, movieListOffKey:
			if err := store.SetWatchlist(movie, picked.Key == movieListOnKey); err != nil {
				Log(fmt.Sprintf("movies: could not save the watchlist: %v", err))
			}
		case movieForgetKey:
			if err := store.Remove(movie.Key()); err != nil {
				Log(fmt.Sprintf("movies: could not forget %s: %v", movie.Key(), err))
			}
			return
		}
	}
}

// playMovie plays a movie from start seconds, in mpv or on the cast device
// when casting is on, trying its servers in turn until one plays, and records
// where it stopped.
func playMovie(config *Config, store *movies.Store, lib *movieLibrary, movie movies.Movie, start int) {
	movie, sources, err := lib.open(config, movie)
	if err != nil {
		Out(fmt.Sprintf("Could not open %s: %v", movie.Label(), err))
		awaitEnterNotice()
		return
	}
	title := movie.Label()
	Out(fmt.Sprintf("Loading %s…", title))
	if entry, ok := store.Get(movie.Key()); ok {
		sources = movies.PreferServer(sources, entry.Server)
	}
	played := func(server string) {
		if err := store.SetServer(movie, server); err != nil {
			Log(fmt.Sprintf("movies: could not save the server: %v", err))
		}
		traktPaused(lib, store, movie)
	}

	for _, source := range sources {
		stream, err := source.Resolve()
		if err != nil {
			Log(fmt.Sprintf("movies: %s: %s: %v", movie.Key(), source.Server, err))
			continue
		}
		Log(fmt.Sprintf("movies: %s: playing from %s", movie.Key(), stream.Server))
		anime := movieAnime(config, store, movie, title, stream, start)
		if config.CastToDevice {
			if castMovie(config, &anime) {
				played(source.Server)
				return
			}
			continue
		}
		if playMovieInMPV(config, store, movie, title, stream, &anime, start) {
			played(source.Server)
			return
		}
	}
	Out(fmt.Sprintf("None of the servers for %s could be played. The movie may have been taken down; try again later.", title))
	awaitEnterNotice()
}

// movieAnime dresses a movie stream as the one-episode, untracked show the
// player and the cast code know how to play.
func movieAnime(config *Config, store *movies.Store, movie movies.Movie, title string, stream movies.Stream, start int) Anime {
	anime := Anime{
		Title:         AnimeTitle{English: title, Romaji: title},
		CoverImage:    movie.Poster,
		TotalEpisodes: 1,
		Untracked:     true,
		// Nothing about a movie goes to AniList or MyAnimeList.
		SkipRemoteSync: true,
	}
	anime.Ep.Number = 1
	anime.Ep.Links = []string{stream.URL}
	anime.Ep.StreamReferrer = stream.Referrer
	for _, subtitle := range stream.Subtitles {
		anime.Ep.SubtitleTracks = append(anime.Ep.SubtitleTracks, SubtitleTrack{URL: subtitle.URL, Language: subtitle.Language, Label: cmp.Or(subtitle.Label, subtitle.Language)})
	}
	if len(anime.Ep.SubtitleTracks) > 0 {
		// The provider lists the best match first.
		anime.Ep.SubtitleURL = providers.PickSubtitle(anime.Ep.SubtitleTracks, subtitleLanguageFor(config, nil), anime.Ep.SubtitleTracks[0].URL)
	}
	anime.Movie = &MoviePlayback{
		Start: float64(start),
		Progress: func(position float64, duration int, watched bool) {
			if err := store.SetProgress(movie, int(position), duration, watched); err != nil {
				Log(fmt.Sprintf("movies: could not save progress: %v", err))
			}
		},
	}
	return anime
}

// castMovie casts a movie, reporting whether the server it came from is done
// with: it played, or the viewer stopped it. A stream the device or ffmpeg
// could not play leaves the next server to try.
func castMovie(config *Config, anime *Anime) bool {
	RestoreScreen()
	// Cast here rather than handing it to a terminal window as an episode is
	// under rofi: the window rebuilds the show from a file, and a movie's
	// progress would be lost on the way.
	err := CastEpisode(config, anime)
	switch {
	case err == nil, errors.Is(err, ErrCastStopped):
		return true
	}
	Log(fmt.Sprintf("movies: cast: %v", err))
	Out("Casting failed: " + err.Error())
	return false
}

// playMovieInMPV plays one server's stream in mpv, reporting whether it
// played.
func playMovieInMPV(config *Config, store *movies.Store, movie movies.Movie, title string, stream movies.Stream, anime *Anime, start int) bool {
	args := movieMPVArgs(stream)
	if start > 0 {
		// A few seconds back, so the line it stopped on is heard again.
		args = append(args, fmt.Sprintf("--start=%d", max(start-5, 0)))
	}
	Log(fmt.Sprintf("movies: %s: mpv %v %s (referrer %q)", stream.Server, args, stream.URL, stream.Referrer))
	socket, err := StartVideo(stream.URL, args, title, anime)
	if err != nil {
		Log(fmt.Sprintf("movies: %s: could not start the player: %v", stream.Server, err))
		return false
	}
	anime.Ep.Player.SocketPath = socket
	if socket == "android-intent" {
		Out("Opened the movie in mpv. Press Enter when you have finished watching...")
		AwaitEnter()
		return true
	}
	if !WaitForMPVPlaybackStart(socket, MpvPlaybackStartTimeoutDuration(config)) {
		Log(fmt.Sprintf("movies: %s did not start playing", stream.Server))
		if IsMPVRunning(socket) {
			ExitMPV(socket)
		}
		anime.Ep.Player.SocketPath = ""
		return false
	}
	addAlternateSubtitles(MPVSendCommand, socket, anime.Ep.SubtitleURL, anime.Ep.SubtitleTracks)
	watchMoviePlayback(config, store, movie, socket)
	return true
}

// movieMPVArgs are the player options a movie stream needs.
func movieMPVArgs(stream movies.Stream) []string {
	// Always set: without it the anime's audio preference (Japanese) applies,
	// which would pick a Japanese dub on a stream that has one.
	language := stream.AudioLanguage
	if language == "" {
		language = "en,eng"
	}
	args := []string{"--alang=" + language}
	if stream.HLS {
		// Movie hosts name their HLS segments as web pages (page-1.html) and
		// images. FFmpeg 7.1 and later refuse segments whose extension is not
		// a media one unless extension_picky is off; an older FFmpeg does not
		// know the option, and mpv only warns about it there.
		args = append(args,
			"--demuxer-lavf-o-add=allowed_extensions=ALL",
			"--demuxer-lavf-o-add=extension_picky=0")
	}
	if stream.Referrer == "" {
		// Otherwise the anime provider's referrer would be sent.
		args = append(args, "--referrer=")
	}
	return args
}

// downloadMovie saves a movie to the download folder from the first server
// that hands over a file.
func downloadMovie(config *Config, lib *movieLibrary, movie movies.Movie) {
	binary, err := ffmpegPath()
	if err != nil {
		Out("Downloading needs ffmpeg, which was not found.")
		awaitEnterNotice()
		return
	}
	movie, sources, err := lib.open(config, movie)
	if err != nil {
		Out(fmt.Sprintf("Could not open %s: %v", movie.Label(), err))
		awaitEnterNotice()
		return
	}
	dir := ResolveDownloadDir(config)
	if err := os.MkdirAll(dir, 0755); err != nil {
		Out(fmt.Sprintf("Could not create %s: %v", dir, err))
		awaitEnterNotice()
		return
	}
	output := movieDownloadPath(dir, movie, DownloadFormat(config))
	if info, err := os.Stat(output); err == nil && info.Size() > 0 {
		Out("Already downloaded: " + output)
		awaitEnterNotice()
		return
	}

	for _, source := range sources {
		stream, err := source.Resolve()
		if err != nil {
			Log(fmt.Sprintf("movies: %s: %s: %v", movie.Key(), source.Server, err))
			continue
		}
		anime := movieAnime(config, nil, movie, movie.Label(), stream, 0)
		job := ffmpegJob{
			Stream:   stream.URL,
			Referrer: stream.Referrer,
			Subtitle: anime.Ep.SubtitleURL,
			Output:   output,
			File:     !stream.HLS,
		}
		Out(fmt.Sprintf("Downloading %s from %s…", movie.Label(), stream.Server))
		lastReport := time.Now()
		onProgress := func(elapsed time.Duration) {
			if time.Since(lastReport) < 5*time.Second {
				return
			}
			lastReport = time.Now()
			Out(fmt.Sprintf("  %s downloaded", elapsed.Round(time.Second)))
		}
		err = runFFmpeg(binary, job.args(), onProgress)
		if err != nil && job.Subtitle != "" {
			Log(fmt.Sprintf("movies: download with subtitles failed (%v); retrying without them", err))
			job.Subtitle = ""
			err = runFFmpeg(binary, job.args(), onProgress)
		}
		if err != nil {
			os.Remove(output)
			Log(fmt.Sprintf("movies: %s: download from %s failed: %v", movie.Key(), stream.Server, err))
			continue
		}
		Out("Saved " + output)
		awaitEnterNotice()
		return
	}
	Out(fmt.Sprintf("None of the servers for %s could be downloaded.", movie.Label()))
	awaitEnterNotice()
}

// movieDownloadPath names a downloaded movie "Title (Year).mkv".
func movieDownloadPath(dir string, movie movies.Movie, format string) string {
	name := movie.Title
	if movie.Year != "" {
		name += " (" + movie.Year + ")"
	}
	if format != DownloadFormatMP4 {
		format = DownloadFormatMKV
	}
	return filepath.Join(dir, SanitizeFilename(name)+"."+format)
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

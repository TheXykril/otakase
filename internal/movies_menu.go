package internal

import (
	"cmp"
	"errors"
	"fmt"
	"strconv"
	"strings"

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
	movieSubsKey      = "MOVIE:SUBTITLES"
	movieBackKey      = "back"
	moviePathPrefix   = "MOVIE_PATH:"
)

// movieSelect is the menu the Movies section draws with; tests replace it.
var movieSelect = DynamicSelectPreserveOrder

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
	site := movies.NewSite(movies.DefaultSiteURL, store.Site(), func(address string) {
		if err := store.SetSite(address); err != nil {
			Log(fmt.Sprintf("movies: could not remember the site address: %v", err))
		}
	})
	lib := &movieLibrary{config: config, store: store, site: site, providers: map[string]movies.Provider{}, trakt: newMovieTrakt(config)}
	store.OnChange = func(before, after movies.Entry) { syncMovieToTrakt(lib, store, before, after) }

	active := ""
	for {
		var picked SelectionOption
		var ok bool
		picked, ok, active = pickFromMovieTabs(config, store, lib, active)
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
		EnterAppScreen()
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

// movieActions are the Movies menu's keys: the terminal's footer, rofi's
// toolbar.
func movieActions(config *Config, lib *movieLibrary) []FooterAction {
	searching, _ := lib.searching(config)
	return []FooterAction{
		{Key: movieSearchKey, Label: "search", Hint: "ctrl+f"},
		{Key: movieProviderKey, Label: "provider: " + searching, Hint: "ctrl+o"},
		{Key: movieCastKey, Label: castActionCheckbox(config), Hint: "ctrl+k"},
		{Key: movieTraktKey, Label: traktActionLabel(lib.trakt), Hint: "ctrl+t"},
	}
}

// pickFromMovieTabs is the Movies menu, laid out as the anime lists are: the
// remembered lists as tabs, with search, provider, cast and Trakt in the
// terminal's footer or rofi's toolbar on the same keys. It reports the list
// it was left on, so coming back opens it again.
func pickFromMovieTabs(config *Config, store *movies.Store, lib *movieLibrary, active string) (SelectionOption, bool, string) {
	lists := movieLists(store)
	tabs := make([]Tab, 0, len(lists))
	labels := map[string]string{}
	for _, list := range lists {
		tabs = append(tabs, list.tab)
		labels[list.tab.Key] = list.tab.Label
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
		options := []SelectionOption{}
		if !config.RofiSelection || !config.ImagePreview {
			// The poster grid has no room for a row without a poster;
			// there, search is the toolbar's.
			options = append(options, SelectionOption{Key: movieSearchKey, Label: "Search movies", Icon: icons.Search})
		}
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
	for {
		options := load(active)
		prompt := moviesSection
		if config.RofiSelection {
			// rofi names the list in its search box, as it does the anime
			// lists.
			prompt = "Movies · " + labels[active]
		}
		var picked SelectionOption
		var err error
		if config.RofiSelection && config.ImagePreview {
			picked, err = movieSelectPreviewToolbar(moviePreviews(options), false, &PreviewSelectionRefreshConfig{
				Toolbar:    true,
				Prompt:     prompt,
				Categories: tabs,
				Actions:    movieActions(config, lib),
			})
		} else {
			picked, err = movieSelectWithTabs(options, &SelectionRefreshConfig{
				Toolbar:        true,
				Prompt:         prompt,
				Categories:     tabs,
				ActiveCategory: active,
				Actions:        movieActions(config, lib),
				LoadCategory:   load,
			})
		}
		if err != nil {
			Log(fmt.Sprintf("movies: menu failed: %v", err))
			return SelectionOption{}, false, active
		}
		picked = NormalizeSelectionKey(picked)
		if SelectionMeansQuit(picked) {
			Exit(nil)
		}
		switch picked.Key {
		case "-2", movieBackKey, "":
			return SelectionOption{}, false, active
		case nextListKey:
			// Tab, in rofi, which has no tabs to click.
			for i, tab := range tabs {
				if tab.Key == active {
					active = tabs[(i+1)%len(tabs)].Key
					break
				}
			}
			continue
		case listsMenuKey:
			options := make([]SelectionOption, 0, len(tabs))
			for _, tab := range tabs {
				options = append(options, SelectionOption{Key: listChoicePrefix + tab.Key, Label: fmt.Sprintf("%s (%d)", tab.Label, tab.Count), Icon: tab.Icon})
			}
			if chosen, ok := pickMovieOption(options); ok {
				active = strings.TrimPrefix(chosen.Key, listChoicePrefix)
			}
			continue
		}
		return picked, true, active
	}
}

// movieSelectPreviewToolbar draws the poster grid with rofi's toolbar; tests
// replace it.
var movieSelectPreviewToolbar = DynamicSelectPreviewWithRefresh

// moviePreviews are rows as poster grid tiles, in the rows' order.
func moviePreviews(options []SelectionOption) map[string]RofiSelectPreview {
	previews := map[string]RofiSelectPreview{}
	for i, option := range options {
		previews[option.Key] = RofiSelectPreview{Title: option.Label, CoverImage: option.Thumbnail, Rank: i}
	}
	return previews
}

// pickMovieOption shows a menu and reports false when the viewer backs out.
// Quit quits, as it does everywhere.
func pickMovieOption(options []SelectionOption) (SelectionOption, bool) {
	return movieChoice(movieSelect(options))
}

// movieChoice reads what was picked in a Movies menu, reporting false when
// the viewer backed out. Quit quits, as it does everywhere.
func movieChoice(picked SelectionOption, err error) (SelectionOption, bool) {
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

// movieSelectMessage draws a menu under a few lines of text: rofi's message
// bar, or the text printed above the terminal menu. Tests replace it.
var movieSelectMessage = func(config *Config, options []SelectionOption, message string) (SelectionOption, error) {
	if config.RofiSelection {
		lines := []string{}
		for _, line := range strings.Split(message, "\n") {
			lines = append(lines, wrapWords(line, 78)...)
		}
		return movieSelectRofiToolbar(options, movieCastActions(config), rofiEscape(strings.Join(lines, "\n")))
	}
	// The movie's details go in the pane beside the menu, where the anime
	// lists show theirs; printed above it they were drawn over.
	title, details, _ := strings.Cut(message, "\n")
	rows := make([]SelectionOption, len(options))
	for i, option := range options {
		option.Title = title
		if details != "" {
			option.Details = strings.Split(details, "\n")
		}
		rows[i] = option
	}
	return movieSelectActions(rows, movieCastActions(config))
}

// movieSelectActions draws a terminal menu, in the order given, with keys in
// its footer. Tests replace it.
var movieSelectActions = func(options []SelectionOption, actions []FooterAction) (SelectionOption, error) {
	return dynamicSelectInternal(options, &SelectionRefreshConfig{Prompt: moviesSection, Actions: actions}, true)
}

// movieCastActions is the cast switch in the footer of a movie's menu and of
// a list of movies, on ctrl+k as in every other menu.
func movieCastActions(config *Config) []FooterAction {
	return []FooterAction{{Key: movieCastKey, Label: castActionCheckbox(config), Hint: "ctrl+k"}}
}

// moviePlayIcon is the play row's icon: the cast one when it will be cast.
func moviePlayIcon(config *Config) icons.Icon {
	if config.CastToDevice {
		return icons.Cast
	}
	return icons.Play
}

// rofiEscape keeps text from being read as Pango markup by rofi.
func rofiEscape(text string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
}

// pickMovieOptionWithMessage is pickMovieOption with text above the menu.
func pickMovieOptionWithMessage(config *Config, options []SelectionOption, message string) (SelectionOption, bool) {
	return movieChoice(movieSelectMessage(config, options, message))
}

// searchMovies asks for a title and offers what the site finds, until the
// viewer backs out of the question.
func searchMovies(config *Config, store *movies.Store, lib *movieLibrary) {
	for {
		query, cancelled, err := promptCancelable(config, moviesSection,
			"Search for a movie or series", "enter to search · esc to go back")
		if err != nil || cancelled {
			return
		}
		found, err := lib.search(config, query)
		if err != nil {
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
	if movie.Series {
		label += " · series"
	}
	icon := icons.TV
	if entry, ok := store.Get(movie.Key()); ok {
		switch {
		case entry.Series && entry.Started():
			switch {
			case entry.EpisodeDone:
				label += " · watched " + entry.At().Label()
			case entry.Position > 0:
				label += fmt.Sprintf(" · %s stopped at %s", entry.At().Label(), formatClock(entry.Position))
			default:
				label += " · at " + entry.At().Label()
			}
			icon = icons.Play
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
	}
	// Where it is from, since the same film can be on several providers.
	entry, _ := store.Get(movie.Key())
	entry.Movie = movie
	label += " · " + movieSourceLabel(entry)
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
// ImagePreview is on, as the anime lists have, and the cast switch on ctrl+k.
func pickMovieRows(config *Config, options []SelectionOption) (SelectionOption, bool) {
	for {
		var picked SelectionOption
		var err error
		switch {
		case !config.RofiSelection:
			picked, err = movieSelectActions(options, movieCastActions(config))
		case config.ImagePreview:
			picked, err = movieSelectPreviewToolbar(moviePreviews(options), false, &PreviewSelectionRefreshConfig{
				Toolbar: true,
				Prompt:  moviesSection,
				Actions: movieCastActions(config),
			})
		default:
			picked, err = movieSelectRofiToolbar(options, movieCastActions(config), "")
		}
		picked, ok := movieChoice(picked, err)
		if ok && picked.Key == movieCastKey {
			toggleCastToDevice(config)
			continue
		}
		return picked, ok
	}
}

// movieSelectRofiToolbar draws a rofi menu with the toolbar holding actions,
// and message above the rows when there is one. Tests replace it.
var movieSelectRofiToolbar = func(options []SelectionOption, actions []FooterAction, message string) (SelectionOption, error) {
	return rofiSelectInternal(options, false, &SelectionRefreshConfig{Toolbar: true, Prompt: moviesSection, Actions: actions}, moviesSection, message)
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

// pickMovieSubtitleLanguage asks which language a movie's subtitles should be
// in. "" follows SubsLanguage.
func pickMovieSubtitleLanguage(config *Config, current string) (string, bool) {
	options := []SelectionOption{{Key: "", Label: "Same as SubsLanguage (" + titleCase(subtitleLanguageFor(config, nil)) + ")", Icon: icons.Undo}}
	for _, name := range providers.SubtitleLanguageNames() {
		icon := icons.Audio
		if name == providers.CanonicalLanguage(current) {
			icon = icons.Yes
		}
		options = append(options, SelectionOption{Key: "LANG:" + name, Label: titleCase(name), Icon: icon})
	}
	picked, ok := pickMovieOption(options)
	if !ok {
		return "", false
	}
	return strings.TrimPrefix(picked.Key, "LANG:"), true
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
		EnterAppScreen()
	}
}

// openMovie offers what can be done with one movie: play it (or carry on
// where it stopped), keep it for later, or forget it.
func openMovie(config *Config, store *movies.Store, lib *movieLibrary, movie movies.Movie) {
	if movie.Series {
		openSeries(config, store, lib, movie)
		return
	}
	for {
		entry, known := store.Get(movie.Key())
		options := []SelectionOption{}
		if entry.Started() {
			options = append(options,
				SelectionOption{Key: moviePlayKey, Label: "Continue from " + formatClock(entry.Position), Icon: moviePlayIcon(config)},
				SelectionOption{Key: movieRestartKey, Label: "Play from the start", Icon: icons.Refresh})
		} else {
			options = append(options, SelectionOption{Key: moviePlayKey, Label: "Play", Icon: moviePlayIcon(config)})
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
		if movie.Provider == movies.VidsrcName {
			// vidsrc's subtitles are fetched in any language; the others
			// carry their own.
			options = append(options, SelectionOption{Key: movieSubsKey, Label: "Subtitles: " + titleCase(movieSubtitleLanguage(config, store, movie)), Icon: icons.Audio})
		}
		if entry.Watchlist {
			options = append(options, SelectionOption{Key: movieListOffKey, Label: "Remove from watchlist", Icon: icons.No})
		} else if !entry.Watched {
			options = append(options, SelectionOption{Key: movieListOnKey, Label: "Add to watchlist", Icon: icons.Add})
		}
		if known {
			options = append(options, SelectionOption{Key: movieForgetKey, Label: "Remove from history", Icon: icons.Dropped})
		}

		EnterAppScreen()
		message := []string{movie.Label()}
		if details, found := movieDetails(lib, store, movie); found {
			message = append(message, movieDetailsText(details)...)
		}
		picked, ok := pickMovieOptionWithMessage(config, options, strings.Join(message, "\n"))
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
		case movieCastKey:
			toggleCastToDevice(config)
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
		case movieSubsKey:
			if language, ok := pickMovieSubtitleLanguage(config, entry.SubtitleLanguage); ok {
				if err := store.SetSubtitleLanguage(movie, language); err != nil {
					Log(fmt.Sprintf("movies: could not save the subtitle language: %v", err))
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

// castMovie casts a movie, reporting whether the server it came from is done
// with: it played, or the viewer stopped it. A stream the device or ffmpeg
// could not play leaves the next server to try.
func castMovie(config *Config, anime *Anime) bool {
	LeaveAppScreen()
	// Under rofi there is no terminal for the control panel, so the movie is
	// cast from a window of its own, as an anime episode is.
	if config.RofiSelection {
		done, err := castMovieInWindow(config, anime)
		switch {
		case err == nil:
			return done
		case errors.Is(err, ErrCastStopped):
			// The device menu was escaped.
			return true
		}
		Log(fmt.Sprintf("movies: cast window: %v", err))
		Out("Casting here instead of in a terminal: " + err.Error())
	}
	err := castMovieHere(config, anime)
	switch {
	case err == nil, errors.Is(err, ErrCastStopped):
		return true
	}
	Log(fmt.Sprintf("movies: cast: %v", err))
	Out("Casting failed: " + err.Error())
	return false
}

// castMovieHere casts in this process; tests replace it.
var castMovieHere = CastEpisode

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

// awaitEnterNotice leaves a message on screen until the viewer has read it.
func awaitEnterNotice() {
	Out("Press Enter to go back.")
	AwaitEnter()
}

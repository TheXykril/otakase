package internal

import (
	"cmp"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/pkg/browser"

	"github.com/thexykril/otakase/internal/icons"
	"github.com/thexykril/otakase/internal/movies"
)

// Trakt for movies: anime go to AniList or MyAnimeList, movies to Trakt.
// Nothing is synced until the viewer signs in from the Movies menu. otakase's
// own Trakt app is used unless TraktClientID names another.

// newMovieTrakt returns Trakt for the configured app, or nil when there is
// none.
func newMovieTrakt(config *Config) *movies.Trakt {
	clientID := config.TraktClientID
	if clientID == "" {
		clientID = movies.TraktClientID
	}
	trakt := movies.NewTrakt(clientID, config.TraktClientSecret, GetStoragePath())
	if !trakt.Configured() {
		return nil
	}
	return trakt
}

// syncMovieToTrakt is the store's OnChange: it tells Trakt what changed.
// Positions are not sent from here, since they are saved every few seconds;
// traktPaused sends the last one when playback stops.
func syncMovieToTrakt(lib *movieLibrary, store *movies.Store, before, after movies.Entry) {
	trakt := lib.trakt
	if trakt == nil || !trakt.SignedIn() {
		return
	}
	// An episode of a series is in Trakt's history once it is played to
	// the end; a series as a whole never is, or every episode would be.
	episodeDone := after.Series && after.EpisodeDone && after.Episode > 0 &&
		(!before.EpisodeDone || before.At() != after.At())
	filmWatched := !after.Series && before.Watched != after.Watched
	if !episodeDone && !filmWatched && before.Watchlist == after.Watchlist && before.Rating == after.Rating {
		return
	}
	movie := identifyMovie(lib, store, after.Movie)
	report := func(what string, err error) {
		if err != nil {
			Log(fmt.Sprintf("movies: trakt: %s %s: %v", what, after.Key(), err))
		}
	}
	playing := lib.isPlaying(after.Key())
	switch {
	case (episodeDone || (filmWatched && after.Watched)) && playing:
		// Ending the play Trakt shows as watching now adds it to the
		// history too.
		lib.setPlaying(after.Key(), false)
		report("history", trakt.Finished(movie, playingEpisode(after)))
	case episodeDone:
		report("history", trakt.EpisodeWatched(movie, after.At(), time.Now()))
	case filmWatched:
		report("history", trakt.Watched(movie, after.Watched, time.Now()))
	}
	if before.Watchlist != after.Watchlist {
		report("watchlist", trakt.Watchlist(movie, after.Watchlist))
	}
	if before.Rating != after.Rating {
		report("rating", trakt.Rate(movie, after.Rating))
	}
}

// playingEpisode is the episode an entry is at, zero for a film.
func playingEpisode(entry movies.Entry) movies.Episode {
	if !entry.Series {
		return movies.Episode{}
	}
	return entry.At()
}

// identifyMovie finds the IMDb id Trakt matches movies by, for a movie that
// has none yet: an 8Filmai movie only learns it from its page, and Filmukas
// gives none at all, so it is looked up on Trakt by its titles and year. What
// is found is kept, so it is looked up once.
func identifyMovie(lib *movieLibrary, store *movies.Store, movie movies.Movie) movies.Movie {
	if strings.HasPrefix(movie.IMDb, "tt") {
		return movie
	}
	if movie.Provider == "" || movie.Provider == movies.FilmaiName || movie.Provider == movies.FilmukasName {
		// The page knows the original title and year, and 8Filmai's the id.
		if provider, err := lib.provider(cmp.Or(movie.Provider, movies.FilmaiName)); err == nil {
			if opened, _, err := provider.Open(movie); err == nil {
				movie = opened
			}
		}
	}
	if !strings.HasPrefix(movie.IMDb, "tt") {
		movie.IMDb = lib.trakt.FindIMDb([]string{movie.Original, movie.Title}, movie.Year, movie.Series)
	}
	if movie.IMDb == "" {
		Log(fmt.Sprintf("movies: trakt: could not find %s on Trakt", movie.Label()))
		return movie
	}
	if err := store.SetIMDb(movie); err != nil {
		Log(fmt.Sprintf("movies: could not save the IMDb id: %v", err))
	}
	return movie
}

// traktPaused tells Trakt where a film or an episode was stopped, when it
// was stopped part way.
func traktPaused(lib *movieLibrary, store *movies.Store, movie movies.Movie) {
	if lib.trakt == nil || !lib.trakt.SignedIn() {
		return
	}
	lib.setPlaying(movie.Key(), false)
	entry, ok := store.Get(movie.Key())
	if !ok || !entry.Started() || entry.Position <= 0 {
		return
	}
	identified := identifyMovie(lib, store, entry.Movie)
	if err := lib.trakt.Paused(identified, playingEpisode(entry), entry.Position, entry.Duration); err != nil {
		Log(fmt.Sprintf("movies: trakt: progress %s: %v", movie.Key(), err))
	}
}

// traktStarted tells Trakt a film or an episode is playing now, in the
// background so the player is not kept waiting on it.
func traktStarted(lib *movieLibrary, store *movies.Store, play moviePlay) {
	if lib.trakt == nil || !lib.trakt.SignedIn() {
		return
	}
	entry, _ := store.Get(play.movie.Key())
	duration := entry.Duration
	if play.episode.Number > 0 && entry.At() != (movies.Episode{Season: play.episode.Season, Number: play.episode.Number}) {
		// The duration kept is another episode's.
		duration = 0
	}
	lib.setPlaying(play.movie.Key(), true)
	go func() {
		identified := identifyMovie(lib, store, play.movie)
		if err := lib.trakt.Started(identified, play.episode, play.start, duration); err != nil {
			Log(fmt.Sprintf("movies: trakt: watching %s: %v", play.movie.Key(), err))
		}
	}()
}

// traktActionLabel says whether Trakt is signed in.
func traktActionLabel(trakt *movies.Trakt) string {
	switch {
	case trakt == nil:
		return "trakt: not set up"
	case trakt.SignedIn():
		return "trakt: on"
	}
	return "trakt: sign in"
}

// manageMovieTrakt signs in to Trakt, or out.
func manageMovieTrakt(config *Config, trakt *movies.Trakt) {
	if trakt == nil {
		Out("Trakt is not set up: TraktClientID is empty.")
		awaitEnterNotice()
		return
	}
	if trakt.SignedIn() {
		picked, ok := pickMovieOption([]SelectionOption{
			{Key: "SIGN_OUT", Label: "Sign out of Trakt", Icon: icons.No},
		})
		if ok && picked.Key == "SIGN_OUT" {
			if err := trakt.SignOut(); err != nil {
				Log(fmt.Sprintf("movies: trakt: sign out: %v", err))
			}
		}
		return
	}
	code, err := trakt.StartSignIn()
	if err != nil {
		Out("Could not start the Trakt sign-in: " + err.Error())
		awaitEnterNotice()
		return
	}
	RestoreScreen()
	traktNotice(config, fmt.Sprintf("Trakt sign-in: go to %s and enter the code %s. Waiting for Trakt…", code.VerificationURL, code.UserCode), true)
	if err := browser.OpenURL(code.VerificationURL); err != nil {
		Log(fmt.Sprintf("movies: trakt: could not open %s: %v", code.VerificationURL, err))
	}
	if err := trakt.FinishSignIn(code); err != nil {
		traktNotice(config, "Trakt sign-in failed: "+err.Error(), false)
		awaitEnterNotice()
		return
	}
	traktNotice(config, "Signed in to Trakt. Watched movies, the watchlist and ratings are synced from now on.", false)
	awaitEnterNotice()
}

// traktNotice tells the viewer how the sign-in is going. Under rofi it is a
// notification, all of them under one tag so each replaces the last; the code
// stays up (sticky) until the sign-in ends, since the viewer has to read it
// off while typing it into Trakt.
func traktNotice(config *Config, message string, sticky bool) {
	if !config.RofiSelection || runtime.GOOS != "linux" {
		Out(message)
		return
	}
	Log(message)
	send := func() error { return sendLinuxNotification(notifyTagTrakt, "", message) }
	if sticky {
		send = func() error { return sendLinuxStickyNotification(notifyTagTrakt, message) }
	}
	if err := send(); err != nil {
		Log(fmt.Sprintf("Failed to send notification: %v", err))
		Out(message)
	}
}

// movieDetails is what Trakt says about a movie, looked up once per visit to
// the Movies section. A movie Trakt cannot match has none.
func movieDetails(lib *movieLibrary, store *movies.Store, movie movies.Movie) (movies.Details, bool) {
	if lib.trakt == nil {
		return movies.Details{}, false
	}
	if lib.details == nil {
		lib.details = map[string]*movies.Details{}
	}
	if details, ok := lib.details[movie.Key()]; ok {
		return derefDetails(details)
	}
	identified := identifyMovie(lib, store, movie)
	var found *movies.Details
	if strings.HasPrefix(identified.IMDb, "tt") {
		details, err := lib.trakt.Details(identified)
		if err != nil {
			Log(fmt.Sprintf("movies: trakt: details %s: %v", movie.Key(), err))
		} else {
			found = &details
		}
	}
	lib.details[movie.Key()] = found
	return derefDetails(found)
}

func derefDetails(details *movies.Details) (movies.Details, bool) {
	if details == nil {
		return movies.Details{}, false
	}
	return *details, true
}

// movieDetailsText is a movie's details as a few lines for the top of its
// menu: runtime, genres and rating on one line, then the plot.
func movieDetailsText(details movies.Details) []string {
	facts := []string{}
	if details.Runtime > 0 {
		if details.Runtime >= 60 {
			facts = append(facts, fmt.Sprintf("%dh %02dm", details.Runtime/60, details.Runtime%60))
		} else {
			facts = append(facts, fmt.Sprintf("%dm", details.Runtime))
		}
	}
	if details.Episodes > 0 {
		facts = append(facts, fmt.Sprintf("%d episodes", details.Episodes))
	}
	if len(details.Genres) > 0 {
		genres := make([]string, 0, len(details.Genres))
		for _, genre := range details.Genres {
			genre = strings.ReplaceAll(genre, "-", " ")
			if genre != "" {
				genre = strings.ToUpper(genre[:1]) + genre[1:]
			}
			genres = append(genres, genre)
		}
		facts = append(facts, strings.Join(genres, ", "))
	}
	if details.Certification != "" {
		facts = append(facts, details.Certification)
	}
	if details.Rating > 0 {
		facts = append(facts, fmt.Sprintf("%.1f/10 on Trakt", details.Rating))
	}
	lines := []string{}
	if len(facts) > 0 {
		lines = append(lines, strings.Join(facts, " · "))
	}
	if overview := strings.TrimSpace(details.Overview); overview != "" {
		lines = append(lines, wrapWords(overview, 78)...)
	}
	return lines
}

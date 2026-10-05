package internal

import (
	"fmt"
	"runtime"
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
func syncMovieToTrakt(trakt *movies.Trakt, before, after movies.Entry) {
	if trakt == nil || !trakt.SignedIn() {
		return
	}
	report := func(what string, err error) {
		if err != nil {
			Log(fmt.Sprintf("movies: trakt: %s %s: %v", what, after.Key(), err))
		}
	}
	if before.Watched != after.Watched {
		report("history", trakt.Watched(after.Movie, after.Watched, time.Now()))
	}
	if before.Watchlist != after.Watchlist {
		report("watchlist", trakt.Watchlist(after.Movie, after.Watchlist))
	}
	if before.Rating != after.Rating {
		report("rating", trakt.Rate(after.Movie, after.Rating))
	}
}

// traktPaused tells Trakt where a movie was stopped, when it was stopped
// part way.
func traktPaused(trakt *movies.Trakt, store *movies.Store, movie movies.Movie) {
	if trakt == nil || !trakt.SignedIn() {
		return
	}
	entry, ok := store.Get(movie.Key())
	if !ok || !entry.Started() {
		return
	}
	if err := trakt.Paused(entry.Movie, entry.Position, entry.Duration); err != nil {
		Log(fmt.Sprintf("movies: trakt: progress %s: %v", movie.Key(), err))
	}
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

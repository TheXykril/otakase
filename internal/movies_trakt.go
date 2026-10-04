package internal

import (
	"fmt"
	"time"

	"github.com/thexykril/otakase/internal/icons"
	"github.com/thexykril/otakase/internal/movies"
)

// Trakt for movies: anime go to AniList or MyAnimeList, movies to Trakt. It
// needs a Trakt app's credentials in TraktClientID and TraktClientSecret and
// a sign-in from the Movies menu; until then nothing is synced.

// newMovieTrakt returns Trakt for the configured app, or nil when there is
// none.
func newMovieTrakt(config *Config) *movies.Trakt {
	trakt := movies.NewTrakt(config.TraktClientID, config.TraktClientSecret, GetStoragePath())
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
		Out("Trakt needs an app: create one at https://trakt.tv/oauth/applications/new with the redirect URI urn:ietf:wg:oauth:2.0:oob, then set TraktClientID and TraktClientSecret in the config.")
		awaitEnterNotice()
		return
	}
	if trakt.SignedIn() {
		picked, ok := pickMovieOption([]SelectionOption{
			{Key: "SIGN_OUT", Label: "Sign out of Trakt", Icon: icons.No},
			{Key: movieBackKey, Label: "Back", Icon: icons.Back},
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
	Out(fmt.Sprintf("Go to %s and enter the code %s. Waiting for Trakt…", code.VerificationURL, code.UserCode))
	if err := trakt.FinishSignIn(code); err != nil {
		Out("Trakt sign-in failed: " + err.Error())
		awaitEnterNotice()
		return
	}
	Out("Signed in to Trakt. Watched movies, the watchlist and ratings are synced from now on.")
	awaitEnterNotice()
}

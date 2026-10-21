package movies

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The Trakt lists are read whole and the history follows them: what is on
// Trakt shows up here, what was taken off Trakt goes, and a play stopped on
// another device carries on from where it stopped.
func TestTraktLibraryIsApplied(t *testing.T) {
	answers := map[string]string{
		"/sync/watchlist": `[
			{"type":"movie","listed_at":"2026-10-01T10:00:00Z","movie":{"title":"Dune","year":2021,"ids":{"imdb":"tt1160419"}}},
			{"type":"season","listed_at":"2026-10-01T10:00:00Z","season":{"number":1}}]`,
		"/sync/watched/movies": `[{"last_watched_at":"2026-10-02T10:00:00Z","movie":{"title":"Interstellar","year":2014,"ids":{"imdb":"tt0816692"}}}]`,
		"/sync/watched/shows": `[{"last_watched_at":"2026-10-03T10:00:00Z","show":{"title":"Dark","year":2017,"ids":{"imdb":"tt5753856"}},
			"seasons":[{"number":0,"episodes":[{"number":9}]},{"number":1,"episodes":[{"number":1},{"number":2}]}]}]`,
		"/sync/ratings": `[{"type":"movie","rated_at":"2026-10-02T11:00:00Z","rating":9,"movie":{"title":"Interstellar","year":2014,"ids":{"imdb":"tt0816692"}}}]`,
		"/sync/playback": `[
			{"type":"movie","progress":50,"paused_at":"2026-10-04T10:00:00Z","movie":{"title":"Arrival","year":2016,"runtime":116,"ids":{"imdb":"tt2543164"}}},
			{"type":"episode","progress":25,"paused_at":"2026-10-04T11:00:00Z","episode":{"season":1,"number":3,"title":"Past and Present","runtime":60},"show":{"title":"Dark","year":2017,"ids":{"imdb":"tt5753856"}}}]`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		answer, ok := answers[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write([]byte(answer))
	}))
	defer server.Close()

	trakt := NewTrakt("id", "", t.TempDir())
	trakt.API = server.URL
	if err := trakt.saveToken(TraktToken{AccessToken: "tok", CreatedAt: time.Now().Unix(), ExpiresIn: 86400}); err != nil {
		t.Fatal(err)
	}
	lib, err := trakt.Library()
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Watchlist) != 1 || len(lib.Watched) != 2 || len(lib.Ratings) != 1 || len(lib.Playback) != 2 {
		t.Fatalf("library = %+v", lib)
	}
	if dark := lib.Watched[1]; dark.Episode != (Episode{Season: 1, Number: 2}) {
		t.Errorf("Dark is at %+v", dark.Episode)
	}

	store, _ := OpenStore(t.TempDir())
	// Interstellar from Filmukas, which has no IMDb id: the same film.
	if err := store.SetRating(Movie{Provider: FilmukasName, Path: "/interstellar", Title: "Interstellar", Year: "2014"}, 6); err != nil {
		t.Fatal(err)
	}
	// On the watchlist here, but no longer on Trakt's.
	if err := store.SetWatchlist(Movie{Provider: VidsrcName, Path: "tt0111161", IMDb: "tt0111161", Title: "Shawshank"}, true); err != nil {
		t.Fatal(err)
	}
	// Never reached Trakt: kept.
	if err := store.SetWatchlist(Movie{Provider: FilmukasName, Path: "/local", Title: "Local only"}, true); err != nil {
		t.Fatal(err)
	}
	notified := false
	store.OnChange = func(Entry, Entry) { notified = true }

	added := store.ApplyTrakt(lib)
	if notified {
		t.Error("changes from Trakt were sent back to it")
	}
	if len(added) != 3 {
		t.Errorf("added %d entries, want Dune, Dark and Arrival", len(added))
	}

	interstellar, _ := store.Get(Movie{Provider: FilmukasName, Path: "/interstellar"}.Key())
	if !interstellar.Watched || interstellar.Rating != 9 || interstellar.IMDb != "tt0816692" {
		t.Errorf("Interstellar = %+v", interstellar)
	}
	if shawshank, _ := store.Get(Movie{Provider: VidsrcName, Path: "tt0111161"}.Key()); shawshank.Watchlist {
		t.Error("Shawshank is still on the watchlist")
	}
	if local, _ := store.Get(Movie{Provider: FilmukasName, Path: "/local"}.Key()); !local.Watchlist {
		t.Error("a movie Trakt never knew about was taken off the watchlist")
	}
	if dune, _ := store.Get(Movie{Provider: VidsrcName, Path: "tt1160419"}.Key()); !dune.Watchlist || dune.Title != "Dune" {
		t.Errorf("Dune = %+v", dune)
	}
	arrival, _ := store.Get(Movie{Provider: VidsrcName, Path: "tt2543164"}.Key())
	if !arrival.Started() || arrival.Position != 116*60/2 || arrival.Duration != 116*60 {
		t.Errorf("Arrival = %+v", arrival)
	}
	dark, _ := store.Get(Movie{Provider: VidsrcName, Path: "tt5753856", Series: true}.Key())
	if dark.At() != (Episode{Season: 1, Number: 3}) || dark.EpisodeDone || dark.Position != 15*60 {
		t.Errorf("Dark = %+v", dark)
	}
	if continued := store.Continue(); len(continued) != 2 {
		t.Errorf("continue = %d entries", len(continued))
	}

	// A play here after Trakt's stop is kept.
	if err := store.SetProgress(arrival.Movie, 3000, 0, false); err != nil {
		t.Fatal(err)
	}
	store.ApplyTrakt(lib)
	if again, _ := store.Get(arrival.Key()); again.Position != 3000 {
		t.Errorf("Arrival moved back to %d", again.Position)
	}
}

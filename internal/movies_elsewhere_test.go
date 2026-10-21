package internal

import (
	"testing"

	"github.com/thexykril/otakase/internal/movies"
)

type stubMovieProvider struct {
	name  string
	found map[string][]movies.Movie
}

func (p stubMovieProvider) Name() string  { return p.name }
func (p stubMovieProvider) Label() string { return p.name }
func (p stubMovieProvider) Search(query string) ([]movies.Movie, error) {
	return p.found[query], nil
}
func (p stubMovieProvider) Open(movie movies.Movie) (movies.Movie, []movies.Source, error) {
	return movie, nil, nil
}

// A movie Trakt brought in as vidsrc's can be moved to 8Filmai: the other
// sites are searched by its titles, the same film first, and what is
// remembered about it goes with it.
func TestWatchElsewhereMovesTheMovie(t *testing.T) {
	dune := movies.Movie{Provider: movies.VidsrcName, Path: "tt1160419", IMDb: "tt1160419", Title: "Dune", Year: "2021"}
	filmai := movies.Movie{Provider: movies.FilmaiName, Path: "/filmas/kopa-online/", Title: "Kopa", Original: "Dune", Year: "2021"}
	namesake := movies.Movie{Provider: movies.FilmaiName, Path: "/filmas/dune-1984-online/", Title: "Dune", Year: "1984"}
	lib := &movieLibrary{providers: map[string]movies.Provider{
		movies.FilmaiName:   stubMovieProvider{movies.FilmaiName, map[string][]movies.Movie{"Dune": {namesake, filmai}}},
		movies.FilmukasName: stubMovieProvider{movies.FilmukasName, nil},
	}}
	found := searchElsewhere(lib, dune)
	if len(found) != 2 || found[0].Key() != filmai.Key() {
		t.Fatalf("found %+v", found)
	}

	store, _ := movies.OpenStore(t.TempDir())
	if err := store.SetWatchlist(dune, true); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRating(dune, 8); err != nil {
		t.Fatal(err)
	}
	store.OnChange = func(movies.Entry, movies.Entry) { t.Error("a move was sent to Trakt") }
	if err := store.Move(dune, filmai); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get(dune.Key()); ok {
		t.Error("the vidsrc entry is still there")
	}
	moved, ok := store.Get(filmai.Key())
	if !ok || !moved.Watchlist || moved.Rating != 8 || moved.IMDb != "tt1160419" || moved.Title != "Kopa" {
		t.Errorf("moved = %+v", moved)
	}
	// The next Trakt read finds it there, rather than adding vidsrc's again.
	added := store.ApplyTrakt(movies.TraktLibrary{Watchlist: []movies.TraktListed{{Movie: dune}}})
	if len(added) != 0 || len(store.Watchlist()) != 1 {
		t.Errorf("added %+v, watchlist %d", added, len(store.Watchlist()))
	}
}

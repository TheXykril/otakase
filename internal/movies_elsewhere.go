package internal

import (
	"fmt"
	"strings"
	"sync"

	"github.com/thexykril/otakase/internal/movies"
)

// A movie Trakt adds to the lists comes in as vidsrc's, since vidsrc plays
// anything with an IMDb id. Any movie can be moved to another provider, to
// watch it there: its lists, rating and where it was left go with it.

const movieElsewhereKey = "MOVIE:ELSEWHERE"

// searchElsewhere looks for a movie on every other provider, by each of its
// titles. The same film (SameMovie) comes first; namesakes follow, since a
// provider may not know the year or the original title.
func searchElsewhere(lib *movieLibrary, movie movies.Movie) []movies.Movie {
	queries := []string{}
	for _, title := range []string{movie.Title, movie.Original} {
		title = strings.TrimSpace(title)
		if title != "" && (len(queries) == 0 || !strings.EqualFold(queries[0], title)) {
			queries = append(queries, title)
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	var all []movies.Movie
	for _, name := range movies.ProviderNames {
		if name == movie.Provider {
			continue
		}
		provider, err := lib.provider(name)
		if err != nil {
			continue
		}
		for _, query := range queries {
			wg.Add(1)
			go func() {
				defer wg.Done()
				found, err := provider.Search(query)
				if err != nil {
					Log(fmt.Sprintf("movies: %s: search for %q failed: %v", name, query, err))
					return
				}
				mu.Lock()
				all = append(all, found...)
				mu.Unlock()
			}()
		}
	}
	wg.Wait()

	seen := map[string]bool{}
	var same, others []movies.Movie
	for _, name := range movies.ProviderNames {
		for _, found := range all {
			if found.Provider != name || found.Series != movie.Series || seen[found.Key()] {
				continue
			}
			seen[found.Key()] = true
			if movies.SameMovie(found, movie) {
				same = append(same, found)
			} else {
				others = append(others, found)
			}
		}
	}
	return append(same, others...)
}

// watchElsewhere offers the movie on the other providers and moves it to the
// one picked. It reports the movie as it now is.
func watchElsewhere(config *Config, store *movies.Store, lib *movieLibrary, movie movies.Movie) (movies.Movie, bool) {
	LeaveAppScreen()
	Out(fmt.Sprintf("Looking for %s on the other sites…", movie.Label()))
	found := searchElsewhere(lib, movie)
	EnterAppScreen()
	if len(found) == 0 {
		Out(fmt.Sprintf("No other site has %s.", movie.Label()))
		awaitEnterNotice()
		return movie, false
	}
	options := make([]SelectionOption, 0, len(found))
	byKey := map[string]movies.Movie{}
	for _, other := range found {
		row := movieRow(other, store)
		byKey[row.Key] = other
		options = append(options, row)
	}
	picked, ok := pickMovieRows(config, options)
	if !ok {
		return movie, false
	}
	other, ok := byKey[picked.Key]
	if !ok {
		return movie, false
	}
	if err := store.Move(movie, other); err != nil {
		Log(fmt.Sprintf("movies: could not move %s to %s: %v", movie.Key(), other.Key(), err))
	}
	if entry, ok := store.Get(other.Key()); ok {
		other = entry.Movie
	}
	return other, true
}

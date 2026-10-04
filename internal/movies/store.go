package movies

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Entry is what is remembered about one movie.
type Entry struct {
	Movie
	// Position is where playback stopped, in seconds.
	Position  int  `json:"position"`
	Duration  int  `json:"duration"`
	Watched   bool `json:"watched"`
	Watchlist bool `json:"watchlist"`
	// Rating is the viewer's score out of 10, 0 for none.
	Rating  int       `json:"rating,omitempty"`
	Updated time.Time `json:"updated"`
}

// Started reports whether the movie was stopped part way through.
func (e Entry) Started() bool {
	return !e.Watched && e.Position > 0
}

// Store is movies.json: the movie watch history, kept locally only. It lives
// beside the anime history and is a separate file so neither format has to
// know about the other.
type Store struct {
	path string

	mu   sync.Mutex
	data storeData
}

type storeData struct {
	// Site is the address the site was last found at.
	Site   string            `json:"site,omitempty"`
	Movies map[string]*Entry `json:"movies"`
}

// OpenStore reads movies.json in dir, or starts an empty one.
func OpenStore(dir string) (*Store, error) {
	store := &Store{path: filepath.Join(dir, "movies.json"), data: storeData{Movies: map[string]*Entry{}}}
	raw, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return store, err
	}
	if err := json.Unmarshal(raw, &store.data); err != nil {
		return store, err
	}
	if store.data.Movies == nil {
		store.data.Movies = map[string]*Entry{}
	}
	return store, nil
}

// save writes the file whole through a temporary one, so a crash mid-write
// leaves the old history rather than half of a new one. Called with mu held.
func (s *Store) save() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Site is the remembered site address.
func (s *Store) Site() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Site
}

// SetSite remembers the site address.
func (s *Store) SetSite(address string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Site = address
	return s.save()
}

// Get returns the entry for a movie, if there is one.
func (s *Store) Get(path string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.data.Movies[path]
	if !ok {
		return Entry{}, false
	}
	return *entry, true
}

// update changes a movie's entry, creating it from movie when missing, and
// saves.
func (s *Store) update(movie Movie, change func(*Entry)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.data.Movies[movie.Path]
	if !ok {
		entry = &Entry{}
		s.data.Movies[movie.Path] = entry
	}
	// The page knows more than a search result; keep what is known.
	merged := entry.Movie
	merged.Path = movie.Path
	for _, field := range []struct {
		dst *string
		src string
	}{
		{&merged.Title, movie.Title}, {&merged.Original, movie.Original},
		{&merged.Year, movie.Year}, {&merged.Poster, movie.Poster}, {&merged.IMDb, movie.IMDb},
	} {
		if field.src != "" {
			*field.dst = field.src
		}
	}
	entry.Movie = merged
	change(entry)
	entry.Updated = time.Now()
	return s.save()
}

// SetProgress records where playback stopped. A movie watched to the end is
// marked watched and starts from the beginning next time.
func (s *Store) SetProgress(movie Movie, position, duration int, watched bool) error {
	return s.update(movie, func(entry *Entry) {
		if duration > 0 {
			entry.Duration = duration
		}
		if watched {
			entry.Watched = true
			entry.Position = 0
			// Watched is what a watchlist is for.
			entry.Watchlist = false
			return
		}
		entry.Position = position
		if position > 0 {
			entry.Watched = false
		}
	})
}

// SetWatched marks a movie watched, or not watched and to be started over.
func (s *Store) SetWatched(movie Movie, watched bool) error {
	return s.update(movie, func(entry *Entry) {
		entry.Watched = watched
		entry.Position = 0
		if watched {
			entry.Watchlist = false
		}
	})
}

// SetRating records the viewer's score out of 10; 0 clears it.
func (s *Store) SetRating(movie Movie, rating int) error {
	return s.update(movie, func(entry *Entry) { entry.Rating = min(max(rating, 0), 10) })
}

// SetWatchlist adds a movie to the watchlist or takes it off.
func (s *Store) SetWatchlist(movie Movie, on bool) error {
	return s.update(movie, func(entry *Entry) { entry.Watchlist = on })
}

// Remove forgets a movie.
func (s *Store) Remove(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data.Movies, path)
	return s.save()
}

// List returns the entries keep accepts, most recently changed first.
func (s *Store) List(keep func(Entry) bool) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Entry{}
	for _, entry := range s.data.Movies {
		if keep == nil || keep(*entry) {
			out = append(out, *entry)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}

// Continue is the movies stopped part way through.
func (s *Store) Continue() []Entry { return s.List(Entry.Started) }

// Watchlist is the movies saved for later.
func (s *Store) Watchlist() []Entry {
	return s.List(func(e Entry) bool { return e.Watchlist })
}

// History is every movie played, finished or not.
func (s *Store) History() []Entry {
	return s.List(func(e Entry) bool { return e.Watched || e.Position > 0 })
}

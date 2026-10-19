package movies

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	Rating int `json:"rating,omitempty"`
	// Server is the provider's server the movie last played from, tried
	// first the next time.
	Server string `json:"server,omitempty"`
	// SubtitleLanguage is the subtitle language picked for this movie,
	// empty to follow SubsLanguage.
	SubtitleLanguage string `json:"subtitleLanguage,omitempty"`
	// Season and Episode are a series' episode last played; Position and
	// Duration are that episode's. EpisodeDone is it played to the end, so
	// the next one is up. Watched, for a series, is its last episode done.
	Season      int       `json:"season,omitempty"`
	Episode     int       `json:"episode,omitempty"`
	EpisodeDone bool      `json:"episodeDone,omitempty"`
	Updated     time.Time `json:"updated"`
}

// Started reports whether the movie was stopped part way through, or a
// series has episodes played and more to go.
func (e Entry) Started() bool {
	return !e.Watched && (e.Position > 0 || (e.Series && e.Episode > 0))
}

// At is the episode a series was left at.
func (e Entry) At() Episode {
	return Episode{Season: e.Season, Number: e.Episode}
}

// Store is movies.json: the movie watch history, kept locally only. It lives
// beside the anime history and is a separate file so neither format has to
// know about the other.
type Store struct {
	path string

	mu   sync.Mutex
	data storeData

	// OnChange, when set, is told about every saved change to an entry,
	// after the save, with the entry as it was and as it is. It is how Trakt
	// hears about changes.
	OnChange func(before, after Entry)
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
	// History from before there was a choice of provider is keyed by path
	// alone, and all of it is 8Filmai's.
	for key, entry := range store.data.Movies {
		if entry.Provider == "" {
			entry.Provider = FilmaiName
		}
		if entry.Key() != key {
			delete(store.data.Movies, key)
			store.data.Movies[entry.Key()] = entry
		}
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

// Get returns the entry for a movie, by its Key, if there is one.
func (s *Store) Get(key string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.data.Movies[key]
	if !ok {
		return Entry{}, false
	}
	return *entry, true
}

// update changes a movie's entry, creating it from movie when missing, and
// saves.
func (s *Store) update(movie Movie, change func(*Entry)) error {
	before, after, err := s.change(movie, change)
	if err == nil && s.OnChange != nil {
		s.OnChange(before, after)
	}
	return err
}

func (s *Store) change(movie Movie, change func(*Entry)) (Entry, Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if movie.Provider == "" {
		movie.Provider = FilmaiName
	}
	entry, ok := s.data.Movies[movie.Key()]
	if !ok {
		entry = &Entry{}
		s.data.Movies[movie.Key()] = entry
	}
	// The page knows more than a search result; keep what is known.
	merged := entry.Movie
	merged.Provider = movie.Provider
	merged.Path = movie.Path
	merged.Series = merged.Series || movie.Series
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
	before := *entry
	entry.Movie = merged
	change(entry)
	entry.Updated = time.Now()
	if entry.Watched && !before.Watched {
		s.clearCopies(*entry)
	}
	return before, *entry, s.save()
}

// clearCopies takes the same movie from other providers off Continue and the
// watchlist once it is watched from one of them.
func (s *Store) clearCopies(watched Entry) {
	for key, other := range s.data.Movies {
		if key == watched.Key() || !SameMovie(watched.Movie, other.Movie) {
			continue
		}
		other.Position = 0
		other.Watchlist = false
	}
}

// SameMovie reports whether two providers' movies are the same film: the same
// IMDb id when both know it, otherwise a title in common and the same year.
func SameMovie(a, b Movie) bool {
	if a.Series != b.Series {
		return false
	}
	if a.IMDb != "" && b.IMDb != "" {
		return a.IMDb == b.IMDb
	}
	if a.Year == "" || a.Year != b.Year {
		return false
	}
	for _, x := range []string{a.Title, a.Original} {
		for _, y := range []string{b.Title, b.Original} {
			if x != "" && strings.EqualFold(strings.TrimSpace(x), strings.TrimSpace(y)) {
				return true
			}
		}
	}
	return false
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

// SetEpisodeProgress records where a series' episode stopped. An episode
// watched to the end leaves the next one up, and the last one the series
// watched.
func (s *Store) SetEpisodeProgress(show Movie, episode Episode, position, duration int, watched, last bool) error {
	show.Series = true
	return s.update(show, func(entry *Entry) {
		entry.Season, entry.Episode = episode.Season, episode.Number
		if duration > 0 {
			entry.Duration = duration
		}
		if watched {
			entry.EpisodeDone = true
			entry.Position = 0
			if last {
				entry.Watched = true
				entry.Watchlist = false
			}
			return
		}
		entry.EpisodeDone = false
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
		} else if entry.Series {
			// Not watched is started over, from the first episode.
			entry.Season, entry.Episode, entry.EpisodeDone = 0, 0, false
		}
	})
}

// SetRating records the viewer's score out of 10; 0 clears it.
func (s *Store) SetRating(movie Movie, rating int) error {
	return s.update(movie, func(entry *Entry) { entry.Rating = min(max(rating, 0), 10) })
}

// SetIMDb keeps what was learned about a movie, its IMDb id above all,
// without changing anything else about its entry.
func (s *Store) SetIMDb(movie Movie) error {
	if _, ok := s.Get(movie.Key()); !ok {
		return nil
	}
	return s.update(movie, func(*Entry) {})
}

// SetServer records the server a movie played from.
func (s *Store) SetServer(movie Movie, server string) error {
	return s.update(movie, func(entry *Entry) { entry.Server = server })
}

// SetSubtitleLanguage keeps the subtitle language picked for a movie.
func (s *Store) SetSubtitleLanguage(movie Movie, language string) error {
	return s.update(movie, func(entry *Entry) { entry.SubtitleLanguage = language })
}

// SetWatchlist adds a movie to the watchlist or takes it off.
func (s *Store) SetWatchlist(movie Movie, on bool) error {
	return s.update(movie, func(entry *Entry) { entry.Watchlist = on })
}

// Remove forgets a movie, by its Key.
func (s *Store) Remove(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data.Movies, key)
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

// Continue is the movies stopped part way through, less those since watched
// from another provider.
func (s *Store) Continue() []Entry {
	watched := s.List(func(e Entry) bool { return e.Watched })
	return s.List(func(e Entry) bool {
		if !e.Started() {
			return false
		}
		for _, w := range watched {
			if w.Updated.After(e.Updated) && SameMovie(w.Movie, e.Movie) {
				return false
			}
		}
		return true
	})
}

// Watchlist is the movies saved for later.
func (s *Store) Watchlist() []Entry {
	return s.List(func(e Entry) bool { return e.Watchlist })
}

// History is every movie played, finished or not.
func (s *Store) History() []Entry {
	return s.List(func(e Entry) bool { return e.Watched || e.Started() })
}

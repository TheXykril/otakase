package movies

import (
	"strings"
	"time"
)

// ApplyTrakt brings the history in line with the viewer's Trakt lists, the
// way the anime lists follow AniList: what Trakt has wins, since every change
// made here was sent to it as it happened. Nothing is sent back for what
// changes, so OnChange is not told. It reports the entries it added, which
// know no poster yet.
func (s *Store) ApplyTrakt(lib TraktLibrary) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	added := map[string]bool{}
	// played is when each entry was last changed here, before any of this.
	played := map[*Entry]time.Time{}
	for _, entry := range s.data.Movies {
		played[entry] = entry.Updated
	}
	touch := func(entry *Entry, at time.Time) {
		changed = true
		if at.After(entry.Updated) {
			entry.Updated = at
		}
	}
	find := func(movie Movie, at time.Time) *Entry {
		entry := s.traktEntry(movie)
		if entry == nil {
			entry = &Entry{Movie: movie, Updated: at}
			s.data.Movies[movie.Key()] = entry
			added[movie.Key()] = true
			changed = true
		}
		return entry
	}

	listed := map[string]bool{}
	for _, item := range lib.Watchlist {
		listed[item.Movie.IMDb] = true
		entry := find(item.Movie, item.At)
		if !entry.Watchlist {
			entry.Watchlist = true
			touch(entry, item.At)
		}
	}
	rated := map[string]bool{}
	for _, item := range lib.Ratings {
		rated[item.Movie.IMDb] = true
		entry := find(item.Movie, item.At)
		if rating := min(max(item.Rating, 0), 10); entry.Rating != rating {
			entry.Rating = rating
			touch(entry, item.At)
		}
	}
	// Taken off the list or unrated on Trakt is taken off here too. Only
	// what Trakt can know about counts: a movie without an IMDb id never
	// reached it.
	for _, entry := range s.data.Movies {
		if !strings.HasPrefix(entry.IMDb, "tt") {
			continue
		}
		if entry.Watchlist && !listed[entry.IMDb] {
			entry.Watchlist = false
			changed = true
		}
		if entry.Rating > 0 && !rated[entry.IMDb] {
			entry.Rating = 0
			changed = true
		}
	}

	for _, item := range lib.Watched {
		entry := find(item.Movie, item.At)
		if !item.Movie.Series {
			// A film started again since it was watched is being rewatched.
			if entry.Watched || (entry.Position > 0 && entry.Updated.After(item.At)) {
				continue
			}
			entry.Watched, entry.Position, entry.Watchlist = true, 0, false
			touch(entry, item.At)
			continue
		}
		entry.Series = true
		at, ep := item.Episode, entry.At()
		further := at.Season > ep.Season || (at.Season == ep.Season && at.Number > ep.Number)
		if at.Number == 0 || !(further || (at == ep && !entry.EpisodeDone)) {
			continue
		}
		entry.Season, entry.Episode = at.Season, at.Number
		entry.EpisodeDone, entry.Position = true, 0
		touch(entry, item.At)
	}

	watchedAt := map[string]TraktWatched{}
	for _, item := range lib.Watched {
		watchedAt[item.Movie.IMDb] = item
	}
	for _, item := range lib.Playback {
		entry := find(item.Movie, item.At)
		if !item.At.After(played[entry]) {
			// Played here since.
			continue
		}
		episode := Episode{Season: item.Episode.Season, Number: item.Episode.Number}
		if watched, ok := watchedAt[item.Movie.IMDb]; ok {
			if !item.Movie.Series && watched.At.After(item.At) {
				// Watched to the end since it was stopped.
				continue
			}
			at := watched.Episode
			if item.Movie.Series && (at.Season > episode.Season || (at.Season == episode.Season && at.Number >= episode.Number)) {
				// A later episode is watched already.
				continue
			}
		}
		duration := item.Runtime
		if duration == 0 && (!item.Movie.Series || entry.At() == episode) {
			duration = entry.Duration
		}
		if duration == 0 {
			continue
		}
		if item.Movie.Series {
			entry.Series = true
			entry.Season, entry.Episode, entry.EpisodeDone = episode.Season, episode.Number, false
		}
		entry.Position = int(item.Progress / 100 * float64(duration))
		entry.Duration = duration
		entry.Watched = false
		touch(entry, item.At)
	}

	if changed {
		_ = s.save()
	}
	out := []Entry{}
	for key := range added {
		out = append(out, *s.data.Movies[key])
	}
	return out
}

// traktEntry is the entry for a movie from Trakt: the one with its IMDb id,
// else the same film from another provider, the latest played first. Called
// with mu held.
func (s *Store) traktEntry(movie Movie) *Entry {
	var found *Entry
	for _, entry := range s.data.Movies {
		if entry.Series != movie.Series {
			continue
		}
		exact := entry.IMDb == movie.IMDb
		if !exact && !SameMovie(entry.Movie, movie) {
			continue
		}
		if found == nil || (exact && found.IMDb != movie.IMDb) ||
			(exact == (found.IMDb == movie.IMDb) && entry.Updated.After(found.Updated)) {
			found = entry
		}
	}
	if found != nil && found.IMDb == "" {
		found.IMDb = movie.IMDb
	}
	return found
}

// SetPoster keeps a poster found for a movie, without telling OnChange.
func (s *Store) SetPoster(key, poster string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.data.Movies[key]
	if !ok || entry.Poster != "" || poster == "" {
		return nil
	}
	entry.Poster = poster
	return s.save()
}

// Move hands what is remembered about a movie over to the same movie on
// another provider: the lists it is on, its rating and where it was left.
// Nothing changes for Trakt, so OnChange is not told.
func (s *Store) Move(from, to Movie) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.data.Movies[from.Key()]
	if !ok || from.Key() == to.Key() {
		return nil
	}
	moved := *old
	if existing, ok := s.data.Movies[to.Key()]; ok {
		// Keep what the other provider's entry knew of itself.
		moved.Server, moved.SubtitleLanguage = existing.Server, existing.SubtitleLanguage
	} else {
		moved.Server, moved.SubtitleLanguage = "", ""
	}
	moved.Provider, moved.Path = to.Provider, to.Path
	for _, field := range []struct {
		dst *string
		src string
	}{
		{&moved.Title, to.Title}, {&moved.Original, to.Original},
		{&moved.Year, to.Year}, {&moved.Poster, to.Poster},
	} {
		if field.src != "" {
			*field.dst = field.src
		}
	}
	if moved.IMDb == "" {
		moved.IMDb = to.IMDb
	}
	delete(s.data.Movies, from.Key())
	s.data.Movies[to.Key()] = &moved
	return s.save()
}

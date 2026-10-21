package movies

import (
	"fmt"
	"net/http"
	"time"
)

// Trakt is the movie list the way AniList is the anime list: what is on it is
// read back from Trakt each time the Movies menu opens, so a film added to the
// watchlist on the Trakt site, or watched on another device, shows up here.
// What changes here is sent to Trakt as it happens (see the store's OnChange).

// TraktLibrary is the viewer's lists as Trakt has them.
type TraktLibrary struct {
	Watchlist []TraktListed
	Watched   []TraktWatched
	Ratings   []TraktRated
	Playback  []TraktPaused
}

// TraktListed is a film or series on the watchlist.
type TraktListed struct {
	Movie Movie
	At    time.Time
}

// TraktWatched is a film watched, or a series and the last episode of it
// watched.
type TraktWatched struct {
	Movie   Movie
	Episode Episode
	At      time.Time
}

// TraktRated is a film or series given a score out of 10.
type TraktRated struct {
	Movie  Movie
	Rating int
	At     time.Time
}

// TraktPaused is a film or an episode stopped part way, Progress percent in.
type TraktPaused struct {
	Movie    Movie
	Episode  Episode
	Progress float64
	// Runtime is the film's or episode's length in seconds, 0 when unknown.
	Runtime int
	At      time.Time
}

// traktMedia is a film, show or episode as Trakt describes it.
type traktMedia struct {
	Title   string `json:"title"`
	Year    int    `json:"year"`
	Season  int    `json:"season"`
	Number  int    `json:"number"`
	Runtime int    `json:"runtime"`
	IDs     struct {
		IMDb string `json:"imdb"`
	} `json:"ids"`
}

// movie is a film or show from Trakt as one of vidsrc's, which is keyed by
// IMDb id and so plays anything Trakt lists.
func (m *traktMedia) movie(series bool) (Movie, bool) {
	if m == nil || m.IDs.IMDb == "" {
		return Movie{}, false
	}
	movie := Movie{Provider: VidsrcName, Path: m.IDs.IMDb, IMDb: m.IDs.IMDb, Title: m.Title, Series: series}
	if m.Year > 0 {
		movie.Year = fmt.Sprint(m.Year)
	}
	return movie, true
}

// traktEntry is one row of any of the lists read here.
type traktEntry struct {
	Type          string      `json:"type"`
	ListedAt      time.Time   `json:"listed_at"`
	RatedAt       time.Time   `json:"rated_at"`
	PausedAt      time.Time   `json:"paused_at"`
	LastWatchedAt time.Time   `json:"last_watched_at"`
	Rating        int         `json:"rating"`
	Progress      float64     `json:"progress"`
	Movie         *traktMedia `json:"movie"`
	Show          *traktMedia `json:"show"`
	Episode       *traktMedia `json:"episode"`
	Seasons       []struct {
		Number   int `json:"number"`
		Episodes []struct {
			Number        int       `json:"number"`
			LastWatchedAt time.Time `json:"last_watched_at"`
		} `json:"episodes"`
	} `json:"seasons"`
}

// media is the row's film or show, and whether it is a show.
func (e traktEntry) media() (Movie, bool) {
	if e.Movie != nil {
		return e.Movie.movie(false)
	}
	return e.Show.movie(true)
}

func (t *Trakt) get(path string) ([]traktEntry, error) {
	bearer, err := t.bearer()
	if err != nil {
		return nil, err
	}
	var rows []traktEntry
	_, err = t.call(http.MethodGet, path, bearer, nil, &rows)
	return rows, err
}

// Library reads the viewer's watchlist, watched films and series, ratings
// and stopped plays from Trakt. Any list that cannot be read fails the whole,
// so a list is never taken to be empty when it was only unreachable.
func (t *Trakt) Library() (TraktLibrary, error) {
	var lib TraktLibrary

	rows, err := t.get("/sync/watchlist")
	if err != nil {
		return lib, err
	}
	for _, row := range rows {
		if row.Type != "movie" && row.Type != "show" {
			continue
		}
		if movie, ok := row.media(); ok {
			lib.Watchlist = append(lib.Watchlist, TraktListed{Movie: movie, At: row.ListedAt})
		}
	}

	for _, path := range []string{"/sync/watched/movies", "/sync/watched/shows"} {
		rows, err := t.get(path)
		if err != nil {
			return lib, err
		}
		for _, row := range rows {
			movie, ok := row.media()
			if !ok {
				continue
			}
			watched := TraktWatched{Movie: movie, At: row.LastWatchedAt}
			// The furthest episode watched is where the series is at.
			for _, season := range row.Seasons {
				if season.Number == 0 {
					continue
				}
				for _, episode := range season.Episodes {
					at := Episode{Season: season.Number, Number: episode.Number}
					if at.Season > watched.Episode.Season || (at.Season == watched.Episode.Season && at.Number > watched.Episode.Number) {
						watched.Episode = at
					}
				}
			}
			lib.Watched = append(lib.Watched, watched)
		}
	}

	rows, err = t.get("/sync/ratings")
	if err != nil {
		return lib, err
	}
	for _, row := range rows {
		if row.Type != "movie" && row.Type != "show" {
			continue
		}
		if movie, ok := row.media(); ok {
			lib.Ratings = append(lib.Ratings, TraktRated{Movie: movie, Rating: row.Rating, At: row.RatedAt})
		}
	}

	rows, err = t.get("/sync/playback?extended=full")
	if err != nil {
		return lib, err
	}
	for _, row := range rows {
		paused := TraktPaused{Progress: row.Progress, At: row.PausedAt}
		var ok bool
		switch {
		case row.Type == "movie" && row.Movie != nil:
			paused.Movie, ok = row.Movie.movie(false)
			paused.Runtime = row.Movie.Runtime * 60
		case row.Type == "episode" && row.Episode != nil:
			paused.Movie, ok = row.Show.movie(true)
			paused.Episode = Episode{Season: row.Episode.Season, Number: row.Episode.Number, Title: row.Episode.Title}
			paused.Runtime = row.Episode.Runtime * 60
		}
		if ok {
			lib.Playback = append(lib.Playback, paused)
		}
	}
	return lib, nil
}

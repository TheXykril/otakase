package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Films and TV series for /share-title, found on IMDb's public suggestion
// search, the same one the app's vidsrc source uses. It is free and needs
// no key. A title is kept as a show with a negative id made from its IMDb
// number, so it never mixes with AniList ids.

// watchShow is an anime from AniList or a film or series from IMDb.
type watchShow struct {
	ID       int    `json:"id"`
	MalID    int    `json:"mal_id,omitempty"`
	Title    string `json:"title"`
	Episodes int    `json:"episodes,omitempty"`
	Cover    string `json:"cover,omitempty"`
	Year     int    `json:"year,omitempty"`
	Format   string `json:"format,omitempty"`
	Adult    bool   `json:"adult,omitempty"`
}

// watchTitleBase offsets title ids below the film egg's -1.
const watchTitleBase = 1000

var imdbID = regexp.MustCompile(`^tt(\d{1,10})$`)

// watchTitleID turns tt0903747 into the room's id for it.
func watchTitleID(imdb string) int {
	m := imdbID.FindStringSubmatch(imdb)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return 0
	}
	return -(watchTitleBase + n)
}

// watchTitleIMDb is the IMDb id behind a title's room id. IMDb writes ids
// with at least seven digits.
func watchTitleIMDb(id int) string {
	if id > -watchTitleBase-1 {
		return ""
	}
	return fmt.Sprintf("tt%07d", -id-watchTitleBase)
}

// isTitle reports whether the show is a film or series from IMDb.
func (s watchShow) isTitle() bool {
	return s.ID <= -watchTitleBase-1
}

// series reports whether a title has episodes rather than being one film.
func (s watchShow) series() bool {
	return s.isTitle() && s.Format != "MOVIE"
}

var (
	watchIMDbSearch = "https://v3.sg.media-imdb.com/suggestion/x/"
	watchIMDbTitle  = "https://v3.sg.media-imdb.com/suggestion/t/"
)

type imdbSuggestion struct {
	ID    string `json:"id"`
	Title string `json:"l"`
	Kind  string `json:"qid"`
	Year  int    `json:"y"`
	Image struct {
		URL string `json:"imageUrl"`
	} `json:"i"`
}

func (s imdbSuggestion) show() (watchShow, bool) {
	id := watchTitleID(s.ID)
	if id == 0 || s.Title == "" {
		return watchShow{}, false
	}
	show := watchShow{ID: id, Title: s.Title, Year: s.Year, Cover: watchIMDbPoster(s.Image.URL)}
	switch s.Kind {
	case "movie", "tvMovie":
		show.Format, show.Episodes = "MOVIE", 1
	case "tvSeries", "tvMiniSeries":
		show.Format = "TV"
	default:
		return watchShow{}, false
	}
	return show, true
}

// watchIMDbPoster asks Amazon's image server for a poster small enough for
// a picker card instead of the full-size one.
func watchIMDbPoster(u string) string {
	if !strings.HasPrefix(u, "https://m.media-amazon.com/images/") {
		return ""
	}
	if i := strings.LastIndex(u, "._V1_"); i > 0 {
		return u[:i] + "._V1_QL75_UX300_.jpg"
	}
	return u
}

func watchGetJSON(ctx context.Context, u string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := activityClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return errWatchTitleMissing
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", u, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(into)
}

var errWatchTitleMissing = errors.New("no such title")

// watchTitleSearch finds films and series by name; a variable for tests.
var watchTitleSearch = func(q string) ([]watchShow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var r struct {
		D []imdbSuggestion `json:"d"`
	}
	if err := watchGetJSON(ctx, watchIMDbSearch+url.PathEscape(strings.ToLower(q))+".json", &r); err != nil {
		return nil, err
	}
	out := []watchShow{}
	for _, s := range r.D {
		if show, ok := s.show(); ok {
			out = append(out, show)
		}
	}
	return out, nil
}

// watchTitleByID reads one title, with a series' episode count; a variable
// for tests.
var watchTitleByID = func(id int) (watchShow, error) {
	imdb := watchTitleIMDb(id)
	if imdb == "" {
		return watchShow{}, errWatchTitleMissing
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var r struct {
		D []imdbSuggestion `json:"d"`
	}
	if err := watchGetJSON(ctx, watchIMDbTitle+imdb+".json", &r); err != nil {
		return watchShow{}, err
	}
	for _, s := range r.D {
		if s.ID != imdb {
			continue
		}
		show, ok := s.show()
		if !ok {
			break
		}
		return show, nil
	}
	return watchShow{}, errWatchTitleMissing
}

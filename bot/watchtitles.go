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
	"sync"
	"time"
)

// Films and TV series in the watch room. The picker finds them on IMDb's
// public suggestion search, the same one the app's vidsrc source uses, and
// a series' episodes come from TVmaze. Both are free and need no key. The
// helper plays them from vidsrc by IMDb id (`otakase-helper
// -title-stream-info`), as the app does.
//
// A title rides in the room as a show with a negative id made from its IMDb
// number, so the room's episode handling works unchanged: a film has one
// episode, and a series counts its aired episodes across seasons, in order.
// Titles are not on AniList, so watching one moves no lists.

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
	watchTVmaze     = "https://api.tvmaze.com"
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
		if show.series() {
			episodes, err := watchTitleEpisodes(ctx, imdb)
			if err != nil {
				return watchShow{}, err
			}
			show.Episodes = len(episodes)
			if show.Episodes == 0 {
				return watchShow{}, errors.New("no episodes out yet")
			}
		}
		return show, nil
	}
	return watchShow{}, errWatchTitleMissing
}

// watchTitleEpisode is one aired episode of a series.
type watchTitleEpisode struct {
	Season, Number int
	Name, Image    string
}

var watchTitleEpisodeCache = struct {
	sync.Mutex
	m map[string]struct {
		at       time.Time
		episodes []watchTitleEpisode
	}
}{}

// watchTitleEpisodes lists a series' aired regular episodes in order, from
// TVmaze; kept for six hours. A variable for tests.
var watchTitleEpisodes = func(ctx context.Context, imdb string) ([]watchTitleEpisode, error) {
	watchTitleEpisodeCache.Lock()
	cached, ok := watchTitleEpisodeCache.m[imdb]
	watchTitleEpisodeCache.Unlock()
	if ok && time.Since(cached.at) < 6*time.Hour {
		return cached.episodes, nil
	}
	var show struct {
		ID int `json:"id"`
	}
	if err := watchGetJSON(ctx, watchTVmaze+"/lookup/shows?imdb="+url.QueryEscape(imdb), &show); err != nil {
		return nil, err
	}
	if show.ID <= 0 {
		return nil, errWatchTitleMissing
	}
	var list []struct {
		Name     string    `json:"name"`
		Season   int       `json:"season"`
		Number   *int      `json:"number"`
		Type     string    `json:"type"`
		Airstamp time.Time `json:"airstamp"`
		Image    *struct {
			Medium string `json:"medium"`
		} `json:"image"`
	}
	if err := watchGetJSON(ctx, fmt.Sprintf("%s/shows/%d/episodes", watchTVmaze, show.ID), &list); err != nil {
		return nil, err
	}
	now := time.Now()
	episodes := []watchTitleEpisode{}
	for _, e := range list {
		if e.Number == nil || *e.Number < 1 || e.Season < 1 || e.Type != "" && e.Type != "regular" ||
			!e.Airstamp.IsZero() && e.Airstamp.After(now) {
			continue
		}
		ep := watchTitleEpisode{Season: e.Season, Number: *e.Number, Name: e.Name}
		if e.Image != nil && strings.HasPrefix(e.Image.Medium, "https://static.tvmaze.com/") {
			ep.Image = e.Image.Medium
		}
		episodes = append(episodes, ep)
	}
	watchTitleEpisodeCache.Lock()
	if watchTitleEpisodeCache.m == nil {
		watchTitleEpisodeCache.m = map[string]struct {
			at       time.Time
			episodes []watchTitleEpisode
		}{}
	}
	watchTitleEpisodeCache.m[imdb] = struct {
		at       time.Time
		episodes []watchTitleEpisode
	}{now, episodes}
	watchTitleEpisodeCache.Unlock()
	return episodes, nil
}

// watchTitlePreviews names a series' episodes for the picker, by their
// number in the room: "S2 E3 · Name".
func watchTitlePreviews(ctx context.Context, show watchShow) map[int]watchEpisodePreview {
	out := map[int]watchEpisodePreview{}
	if !show.series() {
		return out
	}
	episodes, err := watchTitleEpisodes(ctx, watchTitleIMDb(show.ID))
	if err != nil {
		return out
	}
	for k, e := range episodes {
		p := watchEpisodePreview{Title: fmt.Sprintf("S%d E%d · %s", e.Season, e.Number, e.Name)}
		if e.Image != "" {
			p.Thumbnail = "api/watch/cover?u=" + url.QueryEscape(e.Image)
		}
		out[k+1] = p
	}
	return out
}

// watchTitleArg is what the helper is asked for: the IMDb id, and for a
// series the season and episode of the room's episode number.
func watchTitleArg(ctx context.Context, id, ep int) (string, error) {
	imdb := watchTitleIMDb(id)
	if imdb == "" {
		return "", errWatchTitleMissing
	}
	if ep <= 1 {
		// A film, or the first episode: films need no lookup.
		if _, ok := watchTitleFilm.Load(imdb); ok {
			return imdb, nil
		}
	}
	episodes, err := watchTitleEpisodes(ctx, imdb)
	if errors.Is(err, errWatchTitleMissing) && ep <= 1 {
		return imdb, nil // not a series on TVmaze: played as a film
	}
	if err != nil {
		return "", err
	}
	if ep < 1 || ep > len(episodes) {
		return "", fmt.Errorf("episode %d isn't out", ep)
	}
	e := episodes[ep-1]
	return fmt.Sprintf("%s:%d:%d", imdb, e.Season, e.Number), nil
}

// watchTitleFilm remembers which titles were picked as films.
var watchTitleFilm sync.Map

// watchResolveTitle runs the helper for a title; a variable for tests.
var watchResolveTitle = func(parent context.Context, cli string, id, ep int) (streamInfo, error) {
	arg, err := watchTitleArg(parent, id, ep)
	if err != nil {
		return streamInfo{}, err
	}
	info, err := watchRunCLI(parent, cli, watchResolveFastWait, false, "-title-stream-info", arg)
	if err == nil {
		info.Episode = ep
	}
	return info, err
}

// watchAnyShowByID reads an anime from AniList, or a film or series.
func watchAnyShowByID(id int) (watchShow, error) {
	switch {
	case id == watchMovieID:
		return watchMovieShow(), nil
	case id <= -watchTitleBase-1:
		show, err := watchTitleByID(id)
		if err == nil && !show.series() {
			watchTitleFilm.Store(watchTitleIMDb(id), true)
		}
		return show, err
	}
	return watchShowByID(id)
}

// watchTitleAPI reads a film or series for the picker, with the number of
// episodes a series has out.
func (b *bot) watchTitleAPI(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	if !watchSearchLimit.allow(user, watchNow()) {
		apiError(w, http.StatusTooManyRequests, "slow down")
		return
	}
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || watchTitleIMDb(id) == "" {
		apiError(w, http.StatusBadRequest, "invalid title")
		return
	}
	show, err := watchAnyShowByID(id)
	if err != nil {
		apiError(w, http.StatusBadGateway, "couldn't look that title up, try again in a bit")
		return
	}
	if show.Cover != "" {
		show.Cover = "api/watch/cover?u=" + url.QueryEscape(show.Cover)
	}
	writeJSON(w, http.StatusOK, show)
}

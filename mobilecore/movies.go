package mobilecore

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/thexykril/otakase/internal/movies"
	"github.com/thexykril/otakase/internal/providers"
)

// Movie and series sources. The app finds a title on Trakt and asks the
// sources for it here, the way it asks anime sources for an AniList entry:
// match finds the title on one source, resolve turns it into a stream.

// movieSource describes one source to the app. Language is the language its
// films are dubbed or subtitled in, as an ISO 639-1 code.
type movieSource struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Language string `json:"language"`
	Series   bool   `json:"series"`
}

var movieSources = []movieSource{
	{Name: movies.VidsrcName, Label: "vidsrc", Language: "en", Series: true},
	{Name: movies.FilmaiName, Label: "8Filmai", Language: "lt", Series: true},
	{Name: movies.FilmukasName, Label: "Filmukas", Language: "lt"},
}

// movieTitle is a film or series on one source, as the app keeps it.
type movieTitle struct {
	Provider string `json:"provider"`
	Path     string `json:"path"`
	Title    string `json:"title"`
	Original string `json:"original,omitempty"`
	Year     string `json:"year,omitempty"`
	Poster   string `json:"poster,omitempty"`
	IMDb     string `json:"imdb,omitempty"`
	Series   bool   `json:"series,omitempty"`
}

func (m movieTitle) movie() movies.Movie {
	return movies.Movie{Provider: m.Provider, Path: m.Path, Title: m.Title, Original: m.Original, Year: m.Year, Poster: m.Poster, IMDb: m.IMDb, Series: m.Series}
}

func titleOf(m movies.Movie) movieTitle {
	return movieTitle{Provider: m.Provider, Path: m.Path, Title: m.Title, Original: m.Original, Year: m.Year, Poster: m.Poster, IMDb: m.IMDb, Series: m.Series}
}

type movieRequest struct {
	Version   int    `json:"version"`
	Operation string `json:"operation"`
	Provider  string `json:"provider"`
	// Titles, Year, IMDb and Series describe the Trakt entry match looks
	// for; Season picks the season page on sources with one per season.
	Titles []string `json:"titles"`
	Year   int      `json:"year"`
	IMDb   string   `json:"imdb"`
	Series bool     `json:"series"`
	Season int      `json:"season"`
	// Movie, Season and Episode name what resolve plays.
	Movie   *movieTitle `json:"movie"`
	Episode int         `json:"episode"`
	// Subtitles is the subtitle language name, such as "lithuanian".
	Subtitles string `json:"subtitles"`
	// Skip lists servers that already failed to play.
	Skip []string `json:"skip"`
	// Download fields; see downloads.go.
	ID      int64             `json:"id"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	HLS     bool              `json:"hls"`
	Path    string            `json:"path"`
	Quality int               `json:"quality"`
}

type movieStream struct {
	stream
	Server        string `json:"server"`
	AudioLanguage string `json:"audio_language,omitempty"`
}

// filmaiAddress is 8Filmai's last known address. The site moves; its domain
// redirects to wherever it is, and asking costs a request.
var filmaiAddress struct {
	sync.Mutex
	value string
}

var tempDirOnce sync.Once

// isExtraOperation reports whether an operation is served here rather than
// by the anime request handler.
func isExtraOperation(op string) bool {
	return strings.HasPrefix(op, "movie_") || strings.HasPrefix(op, "download_")
}

func peekOperation(input string) string {
	var peek struct {
		Operation string `json:"operation"`
	}
	if json.Unmarshal([]byte(input), &peek) != nil {
		return ""
	}
	return peek.Operation
}

func decodeMovieRequest(input string) (movieRequest, string) {
	var req movieRequest
	dec := json.NewDecoder(strings.NewReader(input))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, failure("invalid_request", "Invalid JSON request.", false)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return req, failure("invalid_request", "Expected one JSON object.", false)
	}
	if req.Version != 1 {
		return req, failure("invalid_request", "Use version 1.", false)
	}
	return req, ""
}

func (c *Core) executeExtra(ctx context.Context, input string) string {
	req, bad := decodeMovieRequest(input)
	if bad != "" {
		return bad
	}
	if strings.HasPrefix(req.Operation, "download_") {
		return c.executeDownload(req)
	}
	c.useCacheForSubtitles()
	transport := &requestTransport{ctx: ctx, base: c.transport}
	client := &http.Client{Transport: transport, Timeout: requestTimeout}
	var data any
	var err error
	switch req.Operation {
	case "movie_providers":
		return encode(response{Version: 1, Data: map[string]any{"providers": movieSources}})
	case "movie_match":
		if len(req.Titles) == 0 && !strings.HasPrefix(req.IMDb, "tt") {
			return failure("invalid_request", "A title or IMDb id is required.", false)
		}
		var found movieTitle
		found, err = matchMovie(client, req)
		if err == nil && found.Path == "" {
			return failure("not_found", "This source does not have the title.", false)
		}
		data = map[string]any{"movie": found}
	case "movie_resolve":
		if req.Movie == nil || req.Movie.Path == "" || len(req.Movie.Path) > 300 {
			return failure("invalid_request", "A movie is required.", false)
		}
		if req.Movie.Series && (req.Season <= 0 || req.Episode <= 0 || req.Episode > 10000) {
			return failure("invalid_request", "A season and episode are required.", false)
		}
		var found movieStream
		found, err = resolveMovie(client, req)
		if errors.Is(err, errNoMovieStream) {
			return failure("no_stream", "No server played this title.", true)
		}
		data = map[string]any{"stream": found}
	default:
		return failure("invalid_request", "Unknown operation.", false)
	}
	if ctx.Err() != nil {
		return contextFailure(ctx.Err())
	}
	if err != nil {
		if transport.rateLimited.Load() {
			return failure("rate_limited", "Source rate limit reached. Try again later.", true)
		}
		return failure("provider_error", "Source request failed. Try another source.", true)
	}
	return encode(response{Version: 1, Data: data})
}

// useCacheForSubtitles points the system temp dir at the app's cache once.
// vidsrc saves the subtitles it fetches there, and Android has no writable
// temp dir of its own.
func (c *Core) useCacheForSubtitles() {
	c.mu.Lock()
	dir := c.cacheDir
	c.mu.Unlock()
	if dir == "" || os.Getenv("TMPDIR") != "" {
		return
	}
	tempDirOnce.Do(func() { os.Setenv("TMPDIR", dir) })
}

// movieProvider builds a source whose requests go through client, so a
// cancelled request stops them.
func movieProvider(name string, client *http.Client, subtitles string) (movies.Provider, bool) {
	switch name {
	case movies.VidsrcName:
		v := movies.NewVidsrc()
		v.Client = client
		language := movies.English
		if name := providers.CanonicalLanguage(subtitles); name != "" {
			if code := providers.ThreeLetterCode(name); code != "" {
				language = movies.SubtitleLanguage{Code: code, Name: strings.ToUpper(name[:1]) + name[1:]}
			}
		}
		v.Subtitles = func(movies.Movie) movies.SubtitleLanguage { return language }
		return v, true
	case movies.FilmukasName:
		f := movies.NewFilmukas()
		f.Client = client
		return f, true
	case movies.FilmaiName:
		filmaiAddress.Lock()
		remembered := filmaiAddress.value
		filmaiAddress.Unlock()
		site := movies.NewSite(movies.DefaultSiteURL, remembered, func(address string) {
			filmaiAddress.Lock()
			filmaiAddress.value = address
			filmaiAddress.Unlock()
		})
		site.Client.Transport = client.Transport
		p, err := movies.NewProvider(movies.FilmaiName, site)
		return p, err == nil
	}
	return nil, false
}

var seasonPath = regexp.MustCompile(`-(\d+)-sezonas`)

// matchMovie finds a Trakt title on one source. vidsrc is keyed by IMDb id,
// so it needs no search. The others are searched by each title in turn
// (the Lithuanian one first, as the app sends them); a result with the same
// title and year wins. 8Filmai pages carry the IMDb id, which settles a
// match when Trakt knows it.
func matchMovie(client *http.Client, req movieRequest) (movieTitle, error) {
	source := slices.IndexFunc(movieSources, func(s movieSource) bool { return s.Name == req.Provider })
	if source < 0 {
		return movieTitle{}, errUnknownSource
	}
	if req.Series && !movieSources[source].Series {
		return movieTitle{}, nil
	}
	if req.Provider == movies.VidsrcName {
		if !strings.HasPrefix(req.IMDb, "tt") {
			return movieTitle{}, nil
		}
		return movieTitle{Provider: movies.VidsrcName, Path: req.IMDb, IMDb: req.IMDb, Title: firstTitle(req.Titles, req.IMDb), Year: yearText(req.Year), Series: req.Series}, nil
	}
	p, _ := movieProvider(req.Provider, client, "")
	type candidate struct {
		movie movies.Movie
		score int
	}
	var found []candidate
	seen := map[string]bool{}
	var lastErr error
	tried := 0
	for _, title := range req.Titles {
		title = strings.TrimSpace(title)
		if title == "" || len(title) > 300 || tried == 3 {
			continue
		}
		tried++
		results, err := p.Search(title)
		if err != nil {
			lastErr = err
			continue
		}
		for _, m := range results {
			if seen[m.Path] || m.Series != req.Series && req.Provider != movies.FilmaiName {
				continue
			}
			if req.Series {
				if !strings.HasPrefix(m.Path, "/serialai/") || seasonOfPath(m.Path) != max(req.Season, 1) {
					continue
				}
			} else if strings.HasPrefix(m.Path, "/serialai/") {
				continue
			}
			seen[m.Path] = true
			if score := movieScore(m, req); score >= 50 {
				found = append(found, candidate{m, score})
			}
		}
		if slices.ContainsFunc(found, func(c candidate) bool { return c.score >= 100 }) {
			break
		}
	}
	if len(found) == 0 {
		if m, ok := looseFilmaiMatch(p, req); ok {
			return m, nil
		}
		if lastErr != nil {
			return movieTitle{}, lastErr
		}
		return movieTitle{}, nil
	}
	slices.SortStableFunc(found, func(a, b candidate) int { return b.score - a.score })
	if req.Provider != movies.FilmaiName || !strings.HasPrefix(req.IMDb, "tt") {
		return titleOf(found[0].movie), nil
	}
	// The page names its IMDb id; check the best few, so a namesake from the
	// same year is not played instead.
	for i, c := range found {
		if i == 3 {
			break
		}
		opened, _, err := p.Open(c.movie)
		if err != nil {
			lastErr = err
			continue
		}
		if opened.IMDb == "" || opened.IMDb == req.IMDb {
			opened.Series = req.Series
			opened.Path = cmp.Or(opened.Path, c.movie.Path)
			return titleOf(opened), nil
		}
	}
	return movieTitle{}, lastErr
}

// looseFilmaiMatch finds a film 8Filmai lists under another name, like
// "Transporter 4" for The Transporter Refueled: it searches the first word of
// each title and opens the films from about the same year, taking one only
// when its page names the same IMDb id. Without that id nothing is taken.
func looseFilmaiMatch(p movies.Provider, req movieRequest) (movieTitle, bool) {
	if req.Provider != movies.FilmaiName || req.Series || !strings.HasPrefix(req.IMDb, "tt") {
		return movieTitle{}, false
	}
	// Results of the exact searches are looked at again: a low score there
	// may be a film under another name.
	searched, seen := map[string]bool{}, map[string]bool{}
	opened := 0
	for _, title := range req.Titles {
		if len(searched) == 3 || opened == 6 {
			break
		}
		query := looseQuery(title)
		if query == "" || searched[query] {
			continue
		}
		searched[query] = true
		results, err := p.Search(query)
		if err != nil {
			continue
		}
		for _, m := range results {
			if opened == 6 {
				break
			}
			if seen[m.Path] || m.Series || strings.HasPrefix(m.Path, "/serialai/") {
				continue
			}
			seen[m.Path] = true
			if year, err := strconv.Atoi(strings.TrimSpace(m.Year)); req.Year > 0 && err == nil && abs(year-req.Year) > 1 {
				continue
			}
			opened++
			page, _, err := p.Open(m)
			if err != nil || page.IMDb != req.IMDb {
				continue
			}
			page.Path = cmp.Or(page.Path, m.Path)
			return titleOf(page), true
		}
	}
	return movieTitle{}, false
}

// looseQuery is the first real word of a title ("The Transporter Refueled"
// gives "Transporter"), or "" when the title is that one word already.
func looseQuery(title string) string {
	words := strings.FieldsFunc(title, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\''
	})
	for len(words) > 1 && slices.Contains([]string{"the", "a", "an"}, strings.ToLower(words[0])) {
		words = words[1:]
	}
	if len(words) < 2 || len([]rune(words[0])) < 3 {
		return ""
	}
	return words[0]
}

var errUnknownSource = errors.New("unknown source")
var errNoMovieStream = errors.New("no movie stream")

func seasonOfPath(path string) int {
	if m := seasonPath.FindStringSubmatch(path); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 1
}

func yearText(year int) string {
	if year <= 0 {
		return ""
	}
	return strconv.Itoa(year)
}

// movieScore rates a search result against the wanted titles and year: 100
// is the same title from the same year, a year apart is a remake or a
// namesake and loses most of its score.
func movieScore(m movies.Movie, req movieRequest) int {
	names := []string{normalizeTitle(m.Title), normalizeTitle(m.Original)}
	score := 0
	for _, t := range req.Titles {
		want := normalizeTitle(t)
		if want == "" {
			continue
		}
		for _, name := range names {
			if name == "" {
				continue
			}
			s := 0
			switch {
			case name == want:
				s = 90
			case strings.HasPrefix(name, want) || strings.HasPrefix(want, name):
				s = 70 - min(20, abs(len(name)-len(want)))
			}
			score = max(score, s)
		}
	}
	if score == 0 || req.Year <= 0 || req.Series {
		return score
	}
	year, err := strconv.Atoi(strings.TrimSpace(m.Year))
	switch {
	case err != nil:
		return score - 15
	case year == req.Year:
		return score + 10
	case abs(year-req.Year) == 1:
		// Release dates differ by country.
		return score
	}
	return score - 60
}

// resolveMovie tries the title's servers in order until one gives a stream.
func resolveMovie(client *http.Client, req movieRequest) (movieStream, error) {
	p, ok := movieProvider(req.Movie.Provider, client, req.Subtitles)
	if !ok {
		return movieStream{}, errUnknownSource
	}
	movie := req.Movie.movie()
	var sources []movies.Source
	var err error
	if movie.Series {
		series, ok := p.(movies.SeriesProvider)
		if !ok {
			return movieStream{}, errNoMovieStream
		}
		sources, err = series.OpenEpisode(movie, movies.Episode{Season: req.Season, Number: req.Episode})
	} else {
		_, sources, err = p.Open(movie)
	}
	if err != nil {
		return movieStream{}, err
	}
	var lastErr error
	for _, source := range sources {
		if slices.Contains(req.Skip, source.Server) {
			continue
		}
		s, err := resolveSource(source)
		if err != nil {
			lastErr = err
			continue
		}
		headers := map[string]string{}
		if s.Referrer != "" {
			headers["Referer"] = s.Referrer
		}
		subs := make([]subtitle, 0, len(s.Subtitles))
		for _, sub := range s.Subtitles {
			subs = append(subs, subtitle{URL: sub.URL, Language: sub.Language, Label: sub.Label})
		}
		return movieStream{
			stream: stream{URL: s.URL, Headers: headers, Referrer: s.Referrer, HLS: s.HLS || strings.Contains(strings.ToLower(s.URL), ".m3u8"), Subtitles: subs},
			Server: cmp.Or(s.Server, source.Server), AudioLanguage: s.AudioLanguage,
		}, nil
	}
	if lastErr != nil {
		return movieStream{}, errors.Join(errNoMovieStream, lastErr)
	}
	return movieStream{}, errNoMovieStream
}

// resolveSource runs one server's resolve. Video host extractors make their
// own requests, so a slow host is given up on rather than holding the rest.
func resolveSource(source movies.Source) (movies.Stream, error) {
	type answer struct {
		stream movies.Stream
		err    error
	}
	done := make(chan answer, 1)
	go func() {
		s, err := source.Resolve()
		done <- answer{s, err}
	}()
	select {
	case a := <-done:
		if a.err == nil && a.stream.URL == "" {
			a.err = errNoMovieStream
		}
		return a.stream, a.err
	case <-time.After(45 * time.Second):
		return movies.Stream{}, errors.New("server timed out")
	}
}

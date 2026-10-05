// Package movies finds and resolves non-anime movies, an experimental feature
// switched on by ExperimentalMovies.
//
// The source is 8Filmai, a Lithuanian WordPress site on the DooPlay theme. It
// has no fixed address: it moves between bare IP addresses, and a domain in
// front of it (8filmai.eu) answers every request with a redirect to wherever
// it is now, path and all. So the site is reached through that domain once,
// and the address it points at is remembered until it stops answering.
//
// Nothing here runs JavaScript. A movie page carries its stream ids in a plain
// `var data='...'` line in front of an obfuscated script, and the script only
// turns those ids into links to two video hosts (see hosts.go).
package movies

import (
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/thexykril/otakase/internal/providerhost"
)

// DefaultSiteURL is the domain that redirects to the site's current address.
const DefaultSiteURL = "https://8filmai.eu"

// userAgent is a desktop browser's: the domain in front sits behind
// Cloudflare, which serves an ordinary browser and not a script.
const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36"

// Movie is one search result or movie page.
type Movie struct {
	// Provider names where the movie is from; see ProviderNames.
	Provider string
	// Path is the page's path on the site, /filmas/<slug>/. It is the movie's
	// key: the address in front of it changes, the path does not.
	Path  string
	Title string
	// Original is the title in its own language, when the site gives one.
	Original string
	Year     string
	Poster   string
	IMDb     string
	// Series is a show, played an episode at a time, rather than a film.
	Series bool `json:",omitempty"`
}

// Episode is one episode of a series.
type Episode struct {
	Season int
	Number int
	Title  string
}

// Label is how an episode reads in a menu: "S1E3 · Title".
func (e Episode) Label() string {
	label := fmt.Sprintf("S%dE%d", e.Season, e.Number)
	if e.Title != "" {
		label += " · " + e.Title
	}
	return label
}

// Key identifies the movie in the history: its provider and its path there.
func (m Movie) Key() string {
	provider := m.Provider
	if provider == "" {
		provider = FilmaiName
	}
	return provider + ":" + m.Path
}

// Label is how a movie reads in a menu.
func (m Movie) Label() string {
	label := m.Title
	if m.Original != "" && !strings.EqualFold(m.Original, m.Title) {
		label += " / " + m.Original
	}
	if m.Year != "" {
		label += " (" + m.Year + ")"
	}
	return label
}

// Page is a movie page: the movie and the ids of its streams.
type Page struct {
	Movie
	// Servers holds the stream ids by server number, as the page's data line
	// lists them; an empty string is a server the movie is not on.
	Servers []string
	// Subtitles is the data line's fifth field, empty for most movies.
	Subtitles string
	// Episodes are a series season's episodes, and EpisodeServers each
	// one's stream ids by episode number, as Servers are a film's.
	Episodes       []Episode
	EpisodeServers map[int][]string
}

// Site is the 8Filmai site, reached through its redirecting domain.
type Site struct {
	// Entry is the domain to ask for the current address.
	Entry string
	// Client makes the requests. Redirects are not followed by it: the site
	// resolver reads them itself.
	Client *http.Client

	mu      sync.Mutex
	current string
	// remember is called with every address the site is found at, so it can
	// be kept for the next run.
	remember func(string)
}

// NewSite returns the site behind entry, starting from a remembered address
// when there is one. remember may be nil.
func NewSite(entry, remembered string, remember func(string)) *Site {
	entry = strings.TrimRight(strings.TrimSpace(entry), "/")
	if entry == "" {
		entry = DefaultSiteURL
	}
	if !strings.Contains(entry, "://") {
		entry = "https://" + entry
	}
	return &Site{
		Entry: entry,
		Client: &http.Client{
			Timeout: 25 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		current:  strings.TrimRight(strings.TrimSpace(remembered), "/"),
		remember: remember,
	}
}

func logf(format string, args ...any) {
	if providerhost.Log != nil {
		providerhost.Log(fmt.Sprintf("movies: "+format, args...))
	}
}

// Base is the site's address as last found, asking the entry domain when
// none is known yet.
func (s *Site) Base() (string, error) {
	s.mu.Lock()
	current := s.current
	s.mu.Unlock()
	if current != "" {
		return current, nil
	}
	return s.relocate()
}

// relocate asks the entry domain where the site is now.
func (s *Site) relocate() (string, error) {
	req, err := http.NewRequest(http.MethodGet, s.Entry+"/", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := s.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not reach %s: %w", s.Entry, err)
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()

	base := s.Entry
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		location, err := resp.Location()
		if err != nil {
			return "", fmt.Errorf("%s redirected nowhere: %w", s.Entry, err)
		}
		base = location.Scheme + "://" + location.Host
	} else if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered %d", s.Entry, resp.StatusCode)
	}

	s.mu.Lock()
	changed := s.current != base
	s.current = base
	s.mu.Unlock()
	if changed {
		logf("site is at %s", base)
		if s.remember != nil {
			s.remember(base)
		}
	}
	return base, nil
}

// get fetches a path on the site. When the remembered address does not
// answer, the entry domain is asked where the site went and the request is
// tried once more there.
func (s *Site) get(path string) (string, string, error) {
	base, err := s.Base()
	if err != nil {
		return "", "", err
	}
	body, err := s.fetch(base + path)
	if err == nil {
		return body, base, nil
	}
	logf("%s%s failed (%v), asking %s where the site is", base, path, err, s.Entry)
	moved, relocateErr := s.relocate()
	if relocateErr != nil {
		return "", "", errors.Join(err, relocateErr)
	}
	if moved == base {
		return "", "", err
	}
	body, err = s.fetch(moved + path)
	return body, moved, err
}

func (s *Site) fetch(rawURL string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := s.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered %d", rawURL, resp.StatusCode)
	}
	return string(body), nil
}

// Search returns the films and series the site finds for query. A series
// is found a season at a time: each season has a page of its own.
func (s *Site) Search(query string) ([]Movie, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	body, _, err := s.get("/?s=" + url.QueryEscape(query))
	if err != nil {
		return nil, err
	}
	return parseListing(body), nil
}

// Page fetches a movie page.
func (s *Site) Page(path string) (Page, error) {
	body, _, err := s.get(path)
	if err != nil {
		return Page{}, err
	}
	page, err := parsePage(body)
	if err != nil {
		return Page{}, err
	}
	if page.Path == "" {
		page.Path = path
	}
	for i := range page.Episodes {
		page.Episodes[i].Season = seasonOf(page.Path)
	}
	return page, nil
}

var (
	articlePattern   = regexp.MustCompile(`(?s)<article\b.*?</article>`)
	moviePathInHref  = regexp.MustCompile(`href="(?:https?://[^/"]+)?(/(?:filmas|serialai)/[^"#?]+)`)
	seasonInPath     = regexp.MustCompile(`-(\d+)-sezonas`)
	pnamePattern     = regexp.MustCompile(`(?s)class="pname[^"]*"[^>]*>(.*?)</`)
	titleLinkPattern = regexp.MustCompile(`(?s)class="title"[^>]*>\s*<a[^>]*>(.*?)</a>`)
	altPattern       = regexp.MustCompile(`\balt="([^"]+)"`)
	yearPattern      = regexp.MustCompile(`(?s)class="(?:year|date)"[^>]*>\s*(\d{4})`)
	posterPattern    = regexp.MustCompile(`(?:data-src|src)="(https://image\.tmdb\.org/[^"]+)"`)
	tagPattern       = regexp.MustCompile(`<[^>]+>`)
)

// parseListing reads the movies off a listing page: the home page, a genre,
// or search results. Each is a DooPlay article holding a link to the movie and
// its title in one of a few places, depending on the template.
func parseListing(body string) []Movie {
	seen := map[string]bool{}
	movies := []Movie{}
	for _, article := range articlePattern.FindAllString(body, -1) {
		match := moviePathInHref.FindStringSubmatch(article)
		if match == nil {
			continue
		}
		path := match[1]
		if !strings.HasSuffix(path, "/") {
			path += "/"
		}
		if seen[path] {
			continue
		}
		title := firstGroup(article, pnamePattern, titleLinkPattern)
		if title == "" {
			if alt := firstGroup(article, altPattern); alt != "" {
				title = strings.TrimSuffix(alt, " online")
			}
		}
		if title == "" {
			continue
		}
		seen[path] = true
		movies = append(movies, Movie{
			Path:   path,
			Title:  title,
			Year:   firstGroup(article, yearPattern),
			Poster: firstGroup(article, posterPattern),
			Series: strings.HasPrefix(path, "/serialai/"),
		})
	}
	return movies
}

var (
	h1Pattern        = regexp.MustCompile(`(?s)<h1[^>]*class="h1e"[^>]*>(.*?)</h1>\s*<div[^>]*>\s*\(\s*(.*?)\s*\)\s*</div>`)
	h1OnlyPattern    = regexp.MustCompile(`(?s)<h1[^>]*>(.*?)</h1>`)
	dateSpanPattern  = regexp.MustCompile(`(?s)<span class="date"[^>]*>\s*(\d{4})`)
	imdbVarPattern   = regexp.MustCompile(`var\s+imid\s*=\s*'(tt\d+)'`)
	dataVarPattern   = regexp.MustCompile(`var\s+data\s*=\s*'([^']*)'`)
	canonicalPattern = regexp.MustCompile(`<link rel="canonical" href="(?:https?://[^/"]+)?(/[^"]*)"`)
	sheaderPoster    = regexp.MustCompile(`(?s)class="sheader".*?(?:data-src|src)="(https://image\.tmdb\.org/[^"]+)"`)
)

// errNoStreams is a page without a data line: a page that is not a movie, or
// one the site has not given any servers yet.
var errNoStreams = errors.New("the page lists no streams")

// parsePage reads a movie page.
func parsePage(body string) (Page, error) {
	var page Page
	if match := h1Pattern.FindStringSubmatch(body); match != nil {
		page.Title = cleanText(match[1])
		page.Original = cleanText(match[2])
	} else {
		page.Title = firstGroup(body, h1OnlyPattern)
	}
	page.Year = firstGroup(body, dateSpanPattern)
	page.IMDb = firstGroup(body, imdbVarPattern)
	page.Poster = firstGroup(body, sheaderPoster)
	page.Path = firstGroup(body, canonicalPattern)

	data := dataVarPattern.FindStringSubmatch(body)
	if data == nil {
		return page, errNoStreams
	}
	if isSeriesData(data[1]) {
		page.Series = true
		season := seasonOf(page.Path)
		page.EpisodeServers = parseSeriesData(data[1])
		for number := range page.EpisodeServers {
			page.Episodes = append(page.Episodes, Episode{Season: season, Number: number})
		}
		sort.Slice(page.Episodes, func(i, j int) bool { return page.Episodes[i].Number < page.Episodes[j].Number })
		if len(page.Episodes) == 0 {
			return page, errNoStreams
		}
		return page, nil
	}
	page.Servers, page.Subtitles = parseData(data[1])
	if !page.HasStreams() {
		return page, errNoStreams
	}
	return page, nil
}

// isSeriesData reports whether a data line is a series season's:
// episode=fields;episode=fields;...
func isSeriesData(data string) bool {
	return strings.Contains(data, "=")
}

// parseSeriesData splits a season's data line into each episode's stream ids
// by episode number. An episode's fields are as a film's: the ids of servers
// 0 to 3, then its language.
func parseSeriesData(data string) map[int][]string {
	episodes := map[int][]string{}
	for _, part := range strings.Split(data, ";") {
		number, fields, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(number))
		if err != nil || n <= 0 {
			continue
		}
		servers, _ := parseData(fields)
		if (Page{Servers: servers}).HasStreams() {
			episodes[n] = servers
		}
	}
	return episodes
}

// seasonOf reads the season number off a series page's path,
// /serialai/<name>-2-sezonas-online/, taking 1 when it has none.
func seasonOf(path string) int {
	if n, err := strconv.Atoi(firstGroup(path, seasonInPath)); err == nil && n > 0 {
		return n
	}
	return 1
}

// parseData splits a movie's data line. The fields are the stream ids of
// servers 0 to 3, then a subtitle field. A series uses a different shape
// (episode=fields;..., see parseSeriesData), which gives no servers here.
func parseData(data string) ([]string, string) {
	if isSeriesData(data) {
		return nil, ""
	}
	fields := strings.Split(data, "|")
	servers := make([]string, 4)
	for i := 0; i < 4 && i < len(fields); i++ {
		servers[i] = strings.TrimSpace(fields[i])
	}
	subtitles := ""
	if len(fields) > 4 {
		subtitles = strings.TrimSpace(fields[4])
	}
	return servers, subtitles
}

// HasStreams reports whether any server has the movie.
func (p Page) HasStreams() bool {
	for _, id := range p.Servers {
		if id != "" {
			return true
		}
	}
	return false
}

func firstGroup(text string, patterns ...*regexp.Regexp) string {
	for _, pattern := range patterns {
		if match := pattern.FindStringSubmatch(text); match != nil {
			if value := cleanText(match[1]); value != "" {
				return value
			}
		}
	}
	return ""
}

func cleanText(value string) string {
	value = tagPattern.ReplaceAllString(value, "")
	value = html.UnescapeString(value)
	value = strings.ReplaceAll(value, " ", " ")
	return strings.Join(strings.Fields(value), " ")
}

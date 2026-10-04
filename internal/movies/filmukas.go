package movies

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Filmukas is filmukas.com: children's, animated and family films, mostly
// dubbed in Lithuanian. Its player plays from the site's own CDN, an HLS
// playlist with the Lithuanian, English and Russian dubs as audio tracks.
//
// A film page loads its player as a script, /@videojs-v5.js?id=..&z=.., which
// holds the playlist's address with a short-lived token; it is fetched right
// before playing for that reason. The playlist wants the site as referrer.
type Filmukas struct {
	Base   string
	Client *http.Client
}

// NewFilmukas returns filmukas.com.
func NewFilmukas() *Filmukas {
	return &Filmukas{Base: "https://www.filmukas.com", Client: &http.Client{Timeout: 25 * time.Second}}
}

func (*Filmukas) Name() string  { return FilmukasName }
func (*Filmukas) Label() string { return "Filmukas (Lithuanian, family)" }

var (
	filmukasItemPattern   = regexp.MustCompile(`(?s)<li id="li_\d+">\s*<a href="(/[a-z-]+/[^"]+-\d+)".*?</li>`)
	filmukasImagePattern  = regexp.MustCompile(`data-src="([^"]+)"[^>]*alt="([^"]*)"`)
	filmukasYearPattern   = regexp.MustCompile(`-((?:19|20)\d\d)-\d+$`)
	filmukasTitlePattern  = regexp.MustCompile(`(?s)<h1[^>]*itemprop="name"[^>]*>(.*?)</h1>`)
	filmukasPosterPattern = regexp.MustCompile(`property="og:image" content="([^"]+)"`)
	filmukasPlayerPattern = regexp.MustCompile(`loadScript\('(/@videojs[^']+)'\)`)
	filmukasVideoPattern  = regexp.MustCompile(`videoSrc\s*=\s*'([^']+)'`)
)

func (f *Filmukas) fetch(path, referrer string) (string, error) {
	return fetchPage(f.Client, f.Base+path, referrer)
}

// Search finds films by their Lithuanian title. Series are left out.
func (f *Filmukas) Search(query string) ([]Movie, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	body, err := f.fetch("/visi?q="+url.QueryEscape(query), "")
	if err != nil {
		return nil, err
	}
	return f.parseListing(body), nil
}

func (f *Filmukas) parseListing(body string) []Movie {
	movies := []Movie{}
	seen := map[string]bool{}
	for _, match := range filmukasItemPattern.FindAllStringSubmatch(body, -1) {
		path := match[1]
		// A lock is a film only viewers who bought a code can play.
		if strings.HasPrefix(path, "/serialai/") || seen[path] || strings.Contains(match[0], "thumb_lock") {
			continue
		}
		seen[path] = true
		movie := Movie{Provider: FilmukasName, Path: path, Year: firstGroup(path, filmukasYearPattern)}
		if image := filmukasImagePattern.FindStringSubmatch(match[0]); image != nil {
			movie.Poster = f.absolute(image[1])
			movie.Title = cleanText(image[2])
		}
		if movie.Title == "" {
			continue
		}
		movies = append(movies, movie)
	}
	return movies
}

func (f *Filmukas) absolute(path string) string {
	if strings.HasPrefix(path, "/") {
		return f.Base + path
	}
	return path
}

// Open reads the film page; its one source fetches the player script when
// tried, so the token in it is fresh.
func (f *Filmukas) Open(movie Movie) (Movie, []Source, error) {
	movie.Provider = FilmukasName
	page := f.Base + movie.Path
	body, err := f.fetch(movie.Path, "")
	if err != nil {
		return movie, nil, err
	}
	if title := firstGroup(body, filmukasTitlePattern); title != "" {
		movie.Title = title
	}
	if poster := firstGroup(body, filmukasPosterPattern); poster != "" {
		movie.Poster = poster
	}
	if strings.Contains(body, "id='locked'") || strings.Contains(body, `id="locked"`) {
		return movie, nil, fmt.Errorf("filmukas locks this film: it plays only with a code bought on the site")
	}
	player := filmukasPlayerPattern.FindStringSubmatch(body)
	if player == nil {
		return movie, nil, fmt.Errorf("filmukas: no player on %s", page)
	}
	source := Source{Server: "Filmukas", Resolve: func() (Stream, error) {
		script, err := f.fetch(player[1], page)
		if err != nil {
			return Stream{}, fmt.Errorf("filmukas player: %w", err)
		}
		link := filmukasVideoPattern.FindStringSubmatch(script)
		if link == nil {
			return Stream{}, fmt.Errorf("filmukas: no video in the player")
		}
		youtube := strings.Contains(link[1], "youtube.com") || strings.Contains(link[1], "youtu.be")
		return Stream{
			URL:      link[1],
			Referrer: f.Base + "/",
			Server:   "Filmukas",
			HLS:      strings.Contains(link[1], ".m3u8"),
			// The Lithuanian dub, when the film has a choice.
			AudioLanguage: map[bool]string{false: "lt"}[youtube],
		}, nil
	}}
	return movie, []Source{source}, nil
}

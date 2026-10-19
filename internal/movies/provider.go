package movies

import (
	"fmt"
	"strings"
)

// Provider is a place movies are found and played from.
type Provider interface {
	// Name is the provider's key, kept in the config and in the history.
	Name() string
	// Label is how the provider reads in a menu, with its language.
	Label() string
	// Search returns the movies found for a title.
	Search(query string) ([]Movie, error)
	// Open fills in what the provider knows about a movie and lists its
	// servers, in the order to try them.
	Open(movie Movie) (Movie, []Source, error)
}

// SeriesProvider is a provider with series as well as films. A series is a
// Movie with Series set; it is played an episode at a time.
type SeriesProvider interface {
	Provider
	// Episodes lists a series' episodes in order, aired ones only.
	Episodes(show Movie) ([]Episode, error)
	// OpenEpisode lists one episode's servers, in the order to try them.
	OpenEpisode(show Movie, episode Episode) ([]Source, error)
}

// Provider names.
const (
	// FilmaiName is 8Filmai, in Lithuanian. History written before there
	// was a choice of provider is all from it.
	FilmaiName = "8filmai"
	// VidsrcName is vidsrc, in English.
	VidsrcName = "vidsrc"
	// FilmukasName is filmukas.com, family films in Lithuanian.
	FilmukasName = "filmukas"
)

// ProviderNames lists the providers, the default first.
var ProviderNames = []string{VidsrcName, FilmaiName, FilmukasName}

// NewProvider returns the named provider. site is 8Filmai's, which is built
// by the caller because it remembers its address between runs.
func NewProvider(name string, site *Site) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", VidsrcName:
		return NewVidsrc(), nil
	case FilmaiName:
		return filmai{site: site}, nil
	case FilmukasName:
		return NewFilmukas(), nil
	}
	return nil, fmt.Errorf("no movie provider called %q (have %s)", name, strings.Join(ProviderNames, ", "))
}

// filmai is 8Filmai as a Provider.
type filmai struct {
	site *Site
}

func (filmai) Name() string  { return FilmaiName }
func (filmai) Label() string { return "8Filmai (Lithuanian)" }

func (f filmai) Search(query string) ([]Movie, error) {
	found, err := f.site.Search(query)
	for i := range found {
		found[i].Provider = FilmaiName
	}
	return found, err
}

func (f filmai) Open(movie Movie) (Movie, []Source, error) {
	page, err := f.site.Page(movie.Path)
	if err != nil {
		return movie, nil, err
	}
	page.Provider = FilmaiName
	return page.Movie, f.site.Sources(page), nil
}

// Episodes lists the episodes of the season the page is for: 8Filmai has a
// page per season.
func (f filmai) Episodes(show Movie) ([]Episode, error) {
	page, err := f.site.Page(show.Path)
	if err != nil {
		return nil, err
	}
	return page.Episodes, nil
}

func (f filmai) OpenEpisode(show Movie, episode Episode) ([]Source, error) {
	page, err := f.site.Page(show.Path)
	if err != nil {
		return nil, err
	}
	servers, ok := page.EpisodeServers[episode.Number]
	if !ok {
		return nil, fmt.Errorf("8Filmai has no episode %d on %s", episode.Number, show.Path)
	}
	page.Servers = servers
	return f.site.Sources(page), nil
}

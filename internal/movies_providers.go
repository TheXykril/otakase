package internal

import (
	"fmt"
	"strings"

	"github.com/thexykril/otakase/internal/icons"
	"github.com/thexykril/otakase/internal/movies"
)

// movieLibrary holds the movie providers for one visit to the Movies
// section. A remembered movie is opened on the provider it came from, which
// is not always the one searched with now.
type movieLibrary struct {
	site      *movies.Site
	providers map[string]movies.Provider
}

// provider returns the named provider, made once.
func (l *movieLibrary) provider(name string) (movies.Provider, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = movies.ProviderNames[0]
	}
	if provider, ok := l.providers[name]; ok {
		return provider, nil
	}
	provider, err := movies.NewProvider(name, l.site)
	if err != nil {
		return nil, err
	}
	l.providers[name] = provider
	return provider, nil
}

// current is the provider searches go to: MovieProvider, or the default when
// that names none.
func (l *movieLibrary) current(config *Config) movies.Provider {
	provider, err := l.provider(config.MovieProvider)
	if err != nil {
		Log(fmt.Sprintf("movies: %v", err))
		provider, _ = l.provider("")
	}
	return provider
}

// open asks a movie's own provider for its details and servers.
func (l *movieLibrary) open(config *Config, movie movies.Movie) (movies.Movie, []movies.Source, error) {
	name := movie.Provider
	if name == "" {
		name = movies.FilmaiName
	}
	provider, err := l.provider(name)
	if err != nil {
		return movie, nil, err
	}
	opened, sources, err := provider.Open(movie)
	if err != nil {
		Log(fmt.Sprintf("movies: %s: %v", movie.Key(), err))
		return movie, nil, err
	}
	if len(sources) == 0 {
		return opened, nil, fmt.Errorf("%s lists no servers for it", provider.Label())
	}
	return opened, sources, nil
}

// pickMovieProvider lets the viewer choose where movies are searched, and
// keeps the choice in the config.
func pickMovieProvider(config *Config, lib *movieLibrary) {
	current := lib.current(config).Name()
	options := []SelectionOption{}
	for _, name := range movies.ProviderNames {
		provider, err := lib.provider(name)
		if err != nil {
			continue
		}
		icon := icons.Provider
		if name == current {
			icon = icons.Yes
		}
		options = append(options, SelectionOption{Key: name, Label: provider.Label(), Icon: icon})
	}
	options = append(options, SelectionOption{Key: movieBackKey, Label: "Back", Icon: icons.Back})
	picked, ok := pickMovieOption(options)
	if !ok || picked.Key == current {
		return
	}
	config.MovieProvider = picked.Key
	if err := persistMovieProvider(config); err != nil {
		Log(fmt.Sprintf("movies: could not save MovieProvider: %v", err))
	}
}

func persistMovieProvider(config *Config) error {
	if config == nil || GlobalConfigPath == "" {
		return nil
	}
	configMap, err := LoadConfigFromFile(GlobalConfigPath)
	if err != nil {
		return err
	}
	configMap["MovieProvider"] = config.MovieProvider
	return SaveConfigToFile(GlobalConfigPath, configMap)
}

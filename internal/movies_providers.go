package internal

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/thexykril/otakase/internal/icons"
	"github.com/thexykril/otakase/internal/movies"
	"github.com/thexykril/otakase/internal/providers"
)

// movieLibrary holds the movie providers for one visit to the Movies
// section. A remembered movie is opened on the provider it came from, which
// is not always the one searched with now.
type movieLibrary struct {
	config    *Config
	store     *movies.Store
	site      *movies.Site
	providers map[string]movies.Provider
	// trakt is nil when no Trakt app is configured.
	trakt *movies.Trakt
	// details holds what Trakt said about each movie opened, nil for one it
	// could not match, so each is looked up once.
	details map[string]*movies.Details

	mu sync.Mutex
	// playing holds the movies Trakt was told are playing now, by key.
	playing map[string]bool
}

// setPlaying records whether Trakt was told a movie is playing.
func (l *movieLibrary) setPlaying(key string, on bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.playing == nil {
		l.playing = map[string]bool{}
	}
	if on {
		l.playing[key] = true
	} else {
		delete(l.playing, key)
	}
}

func (l *movieLibrary) isPlaying(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.playing[key]
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
	if vidsrc, ok := provider.(*movies.Vidsrc); ok {
		vidsrc.Subtitles = l.subtitleLanguage
	}
	l.providers[name] = provider
	return provider, nil
}

// movieSubtitleLanguage is the subtitle language for a movie, as a canonical
// name: the one picked for it, else SubsLanguage.
func movieSubtitleLanguage(config *Config, store *movies.Store, movie movies.Movie) string {
	if store != nil {
		if entry, ok := store.Get(movie.Key()); ok && entry.SubtitleLanguage != "" {
			return providers.CanonicalLanguage(entry.SubtitleLanguage)
		}
	}
	return subtitleLanguageFor(config, nil)
}

// subtitleLanguage is the language vidsrc fetches a movie's subtitles in.
func (l *movieLibrary) subtitleLanguage(movie movies.Movie) movies.SubtitleLanguage {
	name := movieSubtitleLanguage(l.config, l.store, movie)
	code := providers.ThreeLetterCode(name)
	if code == "" {
		return movies.English
	}
	return movies.SubtitleLanguage{Code: code, Name: titleCase(name)}
}

// titleCase capitalises a language name: "lithuanian" reads "Lithuanian".
func titleCase(name string) string {
	if name == "" {
		return ""
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// allProviders is MovieProvider's value for searching every provider at once.
const allProviders = "all"

// searchesAll reports whether searches go to every provider: MovieProvider
// is "all", or empty, or names no provider.
func (l *movieLibrary) searchesAll(config *Config) bool {
	name := strings.ToLower(strings.TrimSpace(config.MovieProvider))
	if name == "" || name == allProviders {
		return true
	}
	_, err := l.provider(name)
	return err != nil
}

// searching names where searches go, for the menus: "all" or a provider.
func (l *movieLibrary) searching(config *Config) (name, label string) {
	if l.searchesAll(config) {
		return allProviders, "All providers"
	}
	provider, _ := l.provider(config.MovieProvider)
	return provider.Name(), provider.Label()
}

// search asks the chosen provider, or every provider at once, for a title.
// Results come grouped by provider, in ProviderNames order; a provider that
// fails is logged and left out, unless every one fails.
func (l *movieLibrary) search(config *Config, query string) ([]movies.Movie, error) {
	names := movies.ProviderNames
	if !l.searchesAll(config) {
		names = []string{strings.ToLower(strings.TrimSpace(config.MovieProvider))}
	}
	type answer struct {
		found []movies.Movie
		err   error
	}
	answers := make([]answer, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		provider, err := l.provider(name)
		if err != nil {
			answers[i].err = err
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			found, err := provider.Search(query)
			answers[i] = answer{found, err}
		}()
	}
	wg.Wait()
	found := []movies.Movie{}
	var failed []error
	for i, answer := range answers {
		if answer.err != nil {
			Log(fmt.Sprintf("movies: %s: search for %q failed: %v", names[i], query, answer.err))
			failed = append(failed, fmt.Errorf("%s: %w", names[i], answer.err))
			continue
		}
		found = append(found, answer.found...)
	}
	if len(found) == 0 && len(failed) == len(names) {
		return nil, errors.Join(failed...)
	}
	return found, nil
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
	current, _ := lib.searching(config)
	options := []SelectionOption{{Key: allProviders, Label: "All providers", Icon: icons.Provider}}
	if current == allProviders {
		options[0].Icon = icons.Yes
	}
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

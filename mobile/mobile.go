// Package mobile is the surface a mobile application would call.
//
// It exists because gomobile can only bind a narrow set of types: strings,
// integers, booleans, []byte, error, and types declared in the bound package.
// Slices of structs and maps -- which is what the provider API returns -- cannot
// cross the boundary at all. So everything here takes and returns strings, with
// structured results carried as JSON, and the caller decodes them in Kotlin.
//
// Keeping that translation in one package means the rest of the program is not
// bent into a shape that suits a binding tool.
package mobile

import (
	"encoding/json"
	"fmt"

	_ "github.com/thexykril/otakase/internal/loadproviders"
	"github.com/thexykril/otakase/internal/providers"
)

// Result is the envelope every call returns, so a caller has one shape to
// decode and one place to look for an error.
type result struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Data  any    `json:"data,omitempty"`
}

func respond(data any, err error) string {
	payload := result{OK: err == nil, Data: data}
	if err != nil {
		payload.Error = err.Error()
	}
	encoded, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		// A failure to encode the failure still has to return something the
		// caller can parse, or the app sees a blank string and cannot say why.
		return `{"ok":false,"error":"could not encode the result"}`
	}
	return string(encoded)
}

// Providers lists the registered provider names.
func Providers() string {
	return respond(providers.RegisteredNames(), nil)
}

func lookup(name string) (providers.Provider, error) {
	provider, err := providers.New(name)
	if err != nil {
		return nil, fmt.Errorf("no provider called %q: %w", name, err)
	}
	return provider, nil
}

// Search finds shows matching a query.
func Search(providerName, query, mode string) string {
	provider, err := lookup(providerName)
	if err != nil {
		return respond(nil, err)
	}
	options, err := provider.SearchAnime(query, mode)
	if err != nil {
		return respond(nil, err)
	}
	type match struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Label string `json:"label"`
	}
	matches := make([]match, 0, len(options))
	for _, option := range options {
		matches = append(matches, match{ID: option.Key, Title: option.Title, Label: option.Label})
	}
	return respond(matches, nil)
}

// Episodes lists the episode numbers a show has in a language.
func Episodes(providerName, showID, mode string) string {
	provider, err := lookup(providerName)
	if err != nil {
		return respond(nil, err)
	}
	episodes, err := provider.EpisodesList(showID, mode)
	if err != nil {
		return respond(nil, err)
	}
	return respond(episodes, nil)
}

// Stream resolves an episode to playable URLs, the HTTP headers its CDN
// requires, a subtitle track, and the intro and outro if the provider knows
// them.
//
// The headers matter more here than on the desktop: a player on Android is
// handed a URL and a header map, and a stream whose CDN rejects requests
// without a referrer simply fails with no explanation.
func Stream(providerName, showID, mode string, episode int) string {
	provider, err := lookup(providerName)
	if err != nil {
		return respond(nil, err)
	}

	type stream struct {
		URL      string            `json:"url"`
		Headers  map[string]string `json:"headers,omitempty"`
		Subtitle string            `json:"subtitle,omitempty"`
	}
	type playable struct {
		Streams []stream `json:"streams"`
		Intro   []int    `json:"intro,omitempty"`
		Outro   []int    `json:"outro,omitempty"`
	}

	resolver, ok := provider.(providers.HintResolver)
	if !ok {
		urls, err := provider.GetEpisodeURL(providers.PlaybackConfig{SubOrDub: mode}, showID, episode)
		if err != nil {
			return respond(nil, err)
		}
		out := playable{}
		for _, url := range urls {
			out.Streams = append(out.Streams, stream{URL: url})
		}
		return respond(out, nil)
	}

	urls, hints, err := resolver.GetEpisodeURLForModeWithHints(
		providers.PlaybackConfig{SubOrDub: mode}, showID, episode, mode)
	if err != nil {
		return respond(nil, err)
	}

	out := playable{}
	for _, url := range urls {
		hint := hints[url]
		out.Streams = append(out.Streams, stream{
			URL:      url,
			Headers:  hint.Headers,
			Subtitle: hint.Subtitle,
		})
	}
	if ranger, ok := provider.(providers.SkipRanger); ok {
		if intro, outro, err := ranger.SkipRange(showID, mode, episode); err == nil {
			out.Intro, out.Outro = intro, outro
		}
	}
	return respond(out, nil)
}

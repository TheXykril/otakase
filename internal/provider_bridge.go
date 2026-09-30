package internal

import (
	"net/http"

	_ "github.com/thexykril/otakase/internal/loadproviders"

	"github.com/thexykril/otakase/internal/providerhost"
	"github.com/thexykril/otakase/internal/providers"
)

func init() {
	providerhost.HTTPClient = func() *http.Client { return sharedHTTPClient }
	providerhost.Log = func(msg string) { _ = Log(msg) }
	providerhost.Out = func(msg string) { Out(msg) }
	providerhost.PromptSelect = func(options []providerhost.PromptOption) (providerhost.PromptOption, error) {
		mapped := make([]SelectionOption, 0, len(options))
		for _, option := range options {
			mapped = append(mapped, SelectionOption{
				Key:   option.Key,
				Label: option.Label,
			})
		}
		selected, err := promptSelect(mapped)
		if err != nil {
			return providerhost.PromptOption{}, err
		}
		return providerhost.PromptOption{Key: selected.Key, Label: selected.Label}, nil
	}
	providerhost.PersistSubStylePreference = persistSubStylePreference
	providerhost.CurrentSubStyle = func() string {
		if cfg := GetGlobalConfig(); cfg != nil {
			return cfg.SubStyle
		}
		return ""
	}
	providerhost.StoragePath = GetStoragePath
	providerhost.AnimeNameLanguage = func() string {
		if cfg := GetGlobalConfig(); cfg != nil {
			return cfg.AnimeNameLanguage
		}
		return "english"
	}
	providerhost.SetCookiesForAnimepahe = SetCookiesForAnimepahe
}

func normalizeTranslationType(mode string) string {
	return providers.NormalizeTranslationType(mode)
}

func alternateTranslationType(mode string) string {
	return providers.AlternateTranslationType(mode)
}

func toInternalSelectionOptions(options []providers.SelectionOption) []SelectionOption {
	if len(options) == 0 {
		return nil
	}
	result := make([]SelectionOption, 0, len(options))
	for _, option := range options {
		result = append(result, SelectionOption{
			Key:       option.Key,
			Label:     option.Label,
			Title:     option.Title,
			Thumbnail: option.Thumbnail,
			ExtraData: option.ExtraData,
		})
	}
	return result
}

func toProviderSelectionOptions(options []SelectionOption) []providers.SelectionOption {
	if len(options) == 0 {
		return nil
	}
	result := make([]providers.SelectionOption, 0, len(options))
	for _, option := range options {
		result = append(result, providers.SelectionOption{
			Key:       option.Key,
			Label:     option.Label,
			Title:     option.Title,
			Thumbnail: option.Thumbnail,
			ExtraData: option.ExtraData,
		})
	}
	return result
}

func toPlaybackConfig(config Config) providers.PlaybackConfig {
	return providers.PlaybackConfig{
		SubOrDub: config.SubOrDub,
		SubStyle: config.SubStyle,
	}
}

func fromStreamHints(hints map[string]providers.StreamPlaybackHint) map[string]StreamPlaybackHint {
	if len(hints) == 0 {
		return nil
	}
	result := make(map[string]StreamPlaybackHint, len(hints))
	for key, hint := range hints {
		result[key] = StreamPlaybackHint{
			Referrer:  hint.Referrer,
			Subtitle:  hint.Subtitle,
			Headers:   hint.Headers,
			Subtitles: hint.Subtitles,
		}
	}
	return result
}

type providerAdapter struct {
	inner providers.Provider
}

func (a *providerAdapter) Name() string {
	return a.inner.Name()
}

func (a *providerAdapter) SearchAnime(query, mode string) ([]SelectionOption, error) {
	options, err := a.inner.SearchAnime(query, mode)
	return toInternalSelectionOptions(options), err
}

func (a *providerAdapter) EpisodesList(showID, mode string) ([]string, error) {
	return a.inner.EpisodesList(showID, mode)
}

func (a *providerAdapter) GetEpisodeURL(config Config, id string, epNo int) ([]string, error) {
	return a.inner.GetEpisodeURL(toPlaybackConfig(config), id, epNo)
}

func (a *providerAdapter) GetEpisodeURLForMode(config Config, id string, epNo int, mode string) ([]string, error) {
	if resolver, ok := a.inner.(providers.ModeResolver); ok {
		return resolver.GetEpisodeURLForMode(toPlaybackConfig(config), id, epNo, mode)
	}
	return a.inner.GetEpisodeURL(toPlaybackConfig(config), id, epNo)
}

func (a *providerAdapter) GetEpisodeURLForModeWithHints(config Config, id string, epNo int, mode string) ([]string, map[string]StreamPlaybackHint, error) {
	if resolver, ok := a.inner.(providers.HintResolver); ok {
		links, hints, err := resolver.GetEpisodeURLForModeWithHints(toPlaybackConfig(config), id, epNo, mode)
		return links, fromStreamHints(hints), err
	}
	links, err := a.GetEpisodeURLForMode(config, id, epNo, mode)
	return links, nil, err
}

// SkipRange passes on the opening and ending a provider ships with its streams.
// Without it the adapter hid them: the skip lookup asks for this method, and
// no provider it was handed ever had it. A provider with nothing to say
// reports nothing, which the lookup reads as "ask the next source".
func (a *providerAdapter) SkipRange(id, mode string, epNo int) (intro, outro []int, err error) {
	if ranger, ok := a.inner.(providers.SkipRanger); ok {
		return ranger.SkipRange(id, mode, epNo)
	}
	return nil, nil, nil
}

func resolveProviderID(provider Provider, providerID, query string) (string, error) {
	inner := unwrapProvider(provider)
	if inner == nil {
		return providerID, nil
	}
	if resolver, ok := inner.(providers.IDResolver); ok {
		return resolver.ResolveProviderID(providerID, query)
	}
	return providerID, nil
}

func wrapProvider(provider providers.Provider) Provider {
	return &providerAdapter{inner: provider}
}

func unwrapProvider(provider Provider) providers.Provider {
	if adapter, ok := provider.(*providerAdapter); ok {
		return adapter.inner
	}
	return nil
}

// UsePrefetchedEpisode makes the prefetched next episode the one to play, if it
// is the episode anime is now on. It reports whether it did.
//
// The links and their playback hints are taken together. Taking the links
// alone left the subtitles unset, and the player went on showing the ones it
// was given for the previous episode.
func UsePrefetchedEpisode(anime *Anime) bool {
	if anime == nil {
		return false
	}
	next := anime.Ep.NextEpisode
	if next.Number != anime.Ep.Number || len(next.Links) == 0 {
		return false
	}
	anime.Ep.Links = next.Links
	applyStreamPlaybackHints(anime, next.Links, next.LinkHints)
	if next.Mode != "" {
		anime.Ep.Mode = next.Mode
	}
	if next.ProviderName != "" {
		anime.ProviderName = next.ProviderName
		anime.ProviderId = next.ProviderId
	}
	return true
}

func applyStreamPlaybackHints(anime *Anime, links []string, hints map[string]StreamPlaybackHint) {
	if anime == nil || len(links) == 0 {
		return
	}
	selected := PrioritizeLink(links)
	if hint, ok := hints[selected]; ok {
		anime.Ep.StreamReferrer = hint.Referrer
		anime.Ep.SubtitleURL = pickSubtitleForHint(GetGlobalConfig(), anime, hint)
		anime.Ep.SubtitleTracks = hint.Subtitles
		anime.Ep.StreamHeaders = hint.Headers
		return
	}
	anime.Ep.StreamReferrer = ""
	anime.Ep.SubtitleURL = ""
	anime.Ep.SubtitleTracks = nil
	anime.Ep.StreamHeaders = nil
}

package internal

import (
	"net/http"

	_ "github.com/thexykril/otakase/internal/loadproviders"

	"github.com/thexykril/otakase/internal/providerhost"
	"github.com/thexykril/otakase/internal/providers"
)

// The providers package cannot import this one, so what it needs from the
// app is handed to it here.
func init() {
	providerhost.HTTPClient = func() *http.Client { return sharedHTTPClient }
	providerhost.Log = func(msg string) { _ = Log(msg) }
	providerhost.Out = func(msg string) { Out(msg) }
	providerhost.PromptSelect = func(options []providerhost.PromptOption) (providerhost.PromptOption, error) {
		menu := make([]SelectionOption, len(options))
		for i, option := range options {
			menu[i] = SelectionOption{Key: option.Key, Label: option.Label}
		}
		picked, err := promptSelect(menu)
		if err != nil {
			return providerhost.PromptOption{}, err
		}
		return providerhost.PromptOption{Key: picked.Key, Label: picked.Label}, nil
	}
	providerhost.PersistSubStylePreference = persistSubStylePreference
	providerhost.CurrentSubStyle = func() string {
		var style string
		if config := GetGlobalConfig(); config != nil {
			style = config.SubStyle
		}
		return style
	}
}

// normalizeTranslationType is the canonical "sub" or "dub" for an audio value.
func normalizeTranslationType(audio string) string {
	return providers.NormalizeTranslationType(audio)
}

// alternateTranslationType is the other audio: dub for sub and vice versa.
func alternateTranslationType(audio string) string {
	return providers.AlternateTranslationType(audio)
}

func toPlaybackConfig(config Config) providers.PlaybackConfig {
	return providers.PlaybackConfig{SubOrDub: config.SubOrDub, SubStyle: config.SubStyle}
}

// fromStreamHints converts a provider's stream hints to this package's type.
// No hints come back as nil.
func fromStreamHints(in map[string]providers.StreamPlaybackHint) map[string]StreamPlaybackHint {
	if len(in) == 0 {
		return nil
	}
	converted := make(map[string]StreamPlaybackHint, len(in))
	for link, hint := range in {
		converted[link] = StreamPlaybackHint{
			Referrer:     hint.Referrer,
			Subtitle:     hint.Subtitle,
			Headers:      hint.Headers,
			FlixCloudKey: hint.FlixCloudKey,
			Subtitles:    hint.Subtitles,
			HLS:          hint.HLS,
		}
	}
	return converted
}

// providerAdapter presents a providers.Provider as this package's Provider,
// whose methods take the whole Config.
type providerAdapter struct {
	inner providers.Provider // the host being adapted
}

func wrapProvider(provider providers.Provider) Provider {
	return &providerAdapter{inner: provider}
}

func (a *providerAdapter) Name() string { return a.inner.Name() }

func (a *providerAdapter) SearchAnime(title, mode string) ([]SelectionOption, error) {
	found, err := a.inner.SearchAnime(title, mode)
	if len(found) == 0 {
		return nil, err
	}
	options := make([]SelectionOption, len(found))
	for i, option := range found {
		options[i] = SelectionOption{
			Key:       option.Key,
			Label:     option.Label,
			Title:     option.Title,
			Thumbnail: option.Thumbnail,
			ExtraData: option.ExtraData,
		}
	}
	return options, err
}

func (a *providerAdapter) EpisodesList(id, mode string) ([]string, error) {
	return a.inner.EpisodesList(id, mode)
}

func (a *providerAdapter) GetEpisodeURL(config Config, id string, epNo int) ([]string, error) {
	playback := toPlaybackConfig(config)
	return a.inner.GetEpisodeURL(playback, id, epNo)
}

// GetEpisodeURLForMode asks for mode even of a provider that only reads
// SubOrDub, by setting it to mode.
func (a *providerAdapter) GetEpisodeURLForMode(config Config, id string, epNo int, mode string) ([]string, error) {
	moded, ok := a.inner.(providers.ModeResolver)
	if !ok {
		config.SubOrDub = mode
		return a.GetEpisodeURL(config, id, epNo)
	}
	return moded.GetEpisodeURLForMode(toPlaybackConfig(config), id, epNo, mode)
}

func (a *providerAdapter) GetEpisodeURLForModeWithHints(config Config, id string, epNo int, mode string) ([]string, map[string]StreamPlaybackHint, error) {
	resolver, ok := a.inner.(providers.HintResolver)
	if !ok {
		links, err := a.GetEpisodeURLForMode(config, id, epNo, mode)
		return links, nil, err
	}
	links, hints, err := resolver.GetEpisodeURLForModeWithHints(toPlaybackConfig(config), id, epNo, mode)
	return links, fromStreamHints(hints), err
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

// applyStreamPlaybackHints sets the episode's stream details from the hint for
// the link that will be played, and clears them when that link has none, so
// nothing carries over from the previous episode.
func applyStreamPlaybackHints(anime *Anime, streams []string, hints map[string]StreamPlaybackHint) {
	if len(streams) == 0 || anime == nil {
		return
	}
	hint, ok := hints[BestLink(streams)]
	if !ok {
		anime.Ep.StreamReferrer = ""
		anime.Ep.SubtitleURL = ""
		anime.Ep.SubtitleTracks = nil
		anime.Ep.StreamHeaders = nil
		anime.Ep.StreamFlixKey = ""
		anime.Ep.StreamHLSBitrate = 0
		return
	}
	anime.Ep.StreamReferrer = hint.Referrer
	anime.Ep.SubtitleURL = pickSubtitleForHint(GetGlobalConfig(), anime, hint)
	anime.Ep.SubtitleTracks = hint.Subtitles
	anime.Ep.StreamHeaders = hint.Headers
	anime.Ep.StreamFlixKey = hint.FlixCloudKey
	anime.Ep.StreamHLSBitrate = hint.HLSBitrate
}

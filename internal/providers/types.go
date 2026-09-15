package providers

// SelectionOption is a provider search or menu result item.
type SelectionOption struct {
	Key       string
	Label     string
	Title     string
	Thumbnail string
	ExtraData any
}

// PlaybackConfig carries playback preferences passed into providers.
type PlaybackConfig struct {
	SubOrDub string
	// SubStyle controls subtitle delivery for sub-mode playback: ask, soft, or hard.
	SubStyle string
}

// StreamPlaybackHint carries MPV playback metadata for a resolved stream URL.
type StreamPlaybackHint struct {
	Referrer string
	Subtitle string
	// Headers are extra HTTP headers the stream's CDN requires. A referrer alone
	// is not always enough: one host rejects its own video segments with 403
	// unless Origin names the player's domain, and MPV's --referrer cannot set
	// Origin. Anything here is passed through to the player as-is.
	Headers map[string]string
}

// Provider resolves catalog search, episode lists, and stream URLs.
type Provider interface {
	Name() string
	SearchAnime(query, mode string) ([]SelectionOption, error)
	EpisodesList(showID, mode string) ([]string, error)
	GetEpisodeURL(config PlaybackConfig, id string, epNo int) ([]string, error)
}

// ModeResolver resolves episode URLs for an explicit sub/dub mode.
type ModeResolver interface {
	GetEpisodeURLForMode(config PlaybackConfig, id string, epNo int, mode string) ([]string, error)
}

// HintResolver resolves episode URLs with MPV playback hints.
type HintResolver interface {
	GetEpisodeURLForModeWithHints(config PlaybackConfig, id string, epNo int, mode string) ([]string, map[string]StreamPlaybackHint, error)
}

// SkipRanger reports the intro and outro of an episode as [start, end] second
// pairs. A provider that already knows them -- because its stream response
// carries them -- can answer without a second service being asked at all.
type SkipRanger interface {
	SkipRange(id, mode string, epNo int) (intro, outro []int, err error)
}

// IDResolver refreshes or validates a provider-specific show ID.
type IDResolver interface {
	ResolveProviderID(providerID, query string) (string, error)
}

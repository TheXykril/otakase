package providers

// SelectionOption is one show a provider's search found.
type SelectionOption struct {
	// Key is the provider's own id for the show, and Label is the line a
	// menu shows for it.
	Key, Label string
	Title      string
	Thumbnail  string
	// ExtraData is anything else the search returned that can tie the result
	// to a tracker entry. A provider that knows a result's MAL id or episode
	// count attaches a SearchHit.
	ExtraData any
}

// SearchHit is the ExtraData a provider attaches when its search result
// carries a MAL id or an episode count. Zero means unknown.
type SearchHit struct {
	MalID    int
	Episodes int
}

// PlaybackConfig is the part of the user's settings a provider reads.
type PlaybackConfig struct {
	SubOrDub string // "sub" or "dub"
	SubStyle string // sub mode only: "ask", "soft" or "hard" subtitles
}

// StreamPlaybackHint is what the player needs to know about one stream URL
// beyond the URL itself.
type StreamPlaybackHint struct {
	Referrer, Subtitle string
	// Headers are extra HTTP headers the stream's CDN requires. A referrer alone
	// is not always enough: one host rejects its own video segments with 403
	// unless Origin names the player's domain, and MPV's --referrer cannot set
	// Origin. Anything here is passed through to the player as-is.
	Headers map[string]string
	// Subtitles lists every subtitle track the host offers for the stream, so
	// the player can use a language other than the one Subtitle defaults to.
	// Empty when the host offers one track or none.
	Subtitles []SubtitleTrack
	// HLS marks a stream that is an HLS playlist although its address does
	// not say so (no .m3u8 in it). Players that choose by the address -- the
	// quality pick, the bot's watch room -- read this instead.
	HLS bool
}

// SubtitleTrack is one external subtitle file a host offers.
type SubtitleTrack struct {
	URL string
	// Language is whatever the host calls it: a code ("en", "por") or a name
	// ("English", "Portuguese - Brazilian"). LanguageMatches reads either.
	Language string
	// Label is the host's own name for the track, shown in the player.
	Label string
}

// Provider is a streaming host: it finds shows, lists their episodes and
// turns an episode into stream URLs.
type Provider interface {
	// Name is the canonical name the provider registered under.
	Name() string
	// SearchAnime looks a title up in the given audio.
	SearchAnime(title, mode string) ([]SelectionOption, error)
	// EpisodesList lists a show's episodes by the labels the host uses.
	EpisodesList(id, mode string) ([]string, error)
	// GetEpisodeURL resolves an episode in cfg.SubOrDub to stream URLs.
	GetEpisodeURL(cfg PlaybackConfig, id string, epNo int) ([]string, error)
}

// ModeResolver is a provider that can be asked for a given audio regardless
// of the user's SubOrDub setting, which the audio fallback needs.
type ModeResolver interface {
	GetEpisodeURLForMode(cfg PlaybackConfig, id string, epNo int, mode string) ([]string, error)
}

// HintResolver is a ModeResolver that also reports per-stream player hints.
type HintResolver interface {
	GetEpisodeURLForModeWithHints(cfg PlaybackConfig, id string, epNo int, mode string) (links []string, hints map[string]StreamPlaybackHint, err error)
}

// SkipRanger reports the intro and outro of an episode as [start, end] second
// pairs. A provider that already knows them -- because its stream response
// carries them -- can answer without a second service being asked at all.
type SkipRanger interface {
	SkipRange(id, mode string, epNo int) (intro, outro []int, err error)
}

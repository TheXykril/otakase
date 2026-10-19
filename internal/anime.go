package internal

// The show being watched and the episode playing, as the playback loop and
// the watch history see them.

// AnimeTitle is a show's name in the languages trackers give it in. The json
// names are the ones the watch history has always been written with.
type AnimeTitle struct {
	Romaji   string `json:"title_romanji"`
	English  string `json:"title"`
	Japanese string `json:"title_japanese"`
}

// Anime is the show being watched and where the viewer is in it.
type Anime struct {
	Title AnimeTitle `json:"title"`
	Ep    Episode    `json:"ep"`
	// CoverImage is a URL; kept under "url" from the history's first format.
	CoverImage string `json:"url"`
	// TotalEpisodes is 0 while it is not known, which for a show still
	// airing can be for good.
	TotalEpisodes int `json:"total_episodes"`
	MalId         int `json:"mal_id"`
	AnilistId     int `json:"anilist_id"`

	// Rewatching is a show the tracker has on Rewatching; it is never
	// scored at the end. Repeat and the dates are carried to write back
	// when the rewatch is completed.
	Rewatching  bool
	Repeat      int
	StartedAt   FuzzyDate
	CompletedAt FuzzyDate

	// ProviderId and ProviderName are where the episodes come from: the
	// show's id on that provider and the provider's name.
	ProviderId   string
	ProviderName string

	FillerEpisodes []int
	IsAiring       bool
	// IsAdult is the tracker's 18+ flag. With AdultContent on, such a show
	// is looked for only on adult providers, which are the only ones that
	// carry it.
	IsAdult bool `json:"-"`
	// SkipRemoteSync is set for a session that should leave the tracker
	// alone, such as watching a completed show again without a rewatch.
	SkipRemoteSync bool `json:"-"`
	// Untracked is an episode from Untracked Watching: played, never written
	// to the history file or a tracker. A cast reads it to skip every write.
	Untracked bool `json:"-"`
	// Movie is set when this is a movie from the Movies section, which is
	// Untracked too: a cast starts it where the movie history says and
	// reports where it got to there instead of to the anime history.
	Movie *MoviePlayback `json:"-"`
	// syncedResume is the position another device left in the tracker entry,
	// kept so a cast started after the lookup can still use it.
	syncedResume syncedResume
}

// MoviePlayback is what a cast needs to know about a movie.
type MoviePlayback struct {
	// Start is the position to start from, in seconds.
	Start float64
	// Progress is told the position and duration, in seconds, when the cast
	// stops, and whether the movie counts as watched.
	Progress func(position float64, duration int, watched bool)
}

// Episode is the episode being played and what is known about it.
type Episode struct {
	Title          AnimeTitle   `json:"title"`
	Number         int          `json:"number"`
	SkipTimes      SkipTimes    `json:"skip_times"`
	Player         playingVideo `json:"player"`
	Resume         bool         `json:"resume"`
	Started        bool         `json:"started"`
	Duration       int          `json:"duration"`
	Links          []string     `json:"links"`
	StreamReferrer string       `json:"-"`
	SubtitleURL    string       `json:"-"`
	// SubtitleTracks is every subtitle the provider offered for this episode,
	// SubtitleURL among them; the rest are offered to mpv to switch to.
	SubtitleTracks []SubtitleTrack   `json:"-"`
	StreamHeaders  map[string]string `json:"-"`
	// StreamHLSBitrate caps mpv at one variant of an HLS master whose audio
	// is a separate rendition; see applyQualityPreference. Zero is no cap.
	StreamHLSBitrate int `json:"-"`
	// Mode is the audio actually playing, which is not always the audio that was
	// asked for: AutoAudioFallback plays sub when a show has no dub. Anything
	// resolving a further episode has to follow what is playing rather than the
	// preference, or it asks for audio this show does not have and fails.
	Mode        string      `json:"-"`
	NextEpisode NextEpisode `json:"next_episode"`
	IsFiller    bool        `json:"filler"`
	IsRecap     bool        `json:"recap"`
	Aired       string      `json:"aired"`
	Synopsis    string      `json:"synopsis"`

	// ContinueLast picks the last watched show instead of showing the list
	// (-c, or Continue from the menu).
	ContinueLast bool
	// LastWasSkipped marks that filler or recap was skipped to reach this
	// episode, so the skipped ones are counted as watched once it plays.
	LastWasSkipped bool
	// IsCompleted is set once the episode counts as watched.
	IsCompleted bool
}

// NextEpisode is the following episode, resolved during playback so it can
// start without a wait.
type NextEpisode struct {
	Number       int
	Links        []string
	ProviderName string
	ProviderId   string
	Mode         string
	// LinkHints are the referrer, subtitles and headers each link was resolved
	// with. They belong to this episode's links, so they travel with them.
	LinkHints map[string]StreamPlaybackHint
}

// StreamPlaybackHint is what mpv needs besides the URL to play a resolved
// stream.
type StreamPlaybackHint struct {
	Referrer string
	Subtitle string
	// Headers are extra HTTP headers the stream's CDN requires; see the
	// providers package for why a referrer alone is sometimes not enough.
	Headers map[string]string
	// Subtitles is every subtitle track the provider offered; Subtitle is the
	// one its own default picked.
	Subtitles []SubtitleTrack
	// HLSBitrate is the bandwidth of the variant Quality chose, when the link
	// had to stay the master; zero when nothing was chosen.
	HLSBitrate int
}

// playingVideo is the mpv side of the episode.
type playingVideo struct {
	Url string
	// Speed is carried from one episode to the next under SaveMpvSpeed.
	Speed float64 `json:"speed"`
	// PlaybackTime is the position in whole seconds, saved as the resume
	// point.
	PlaybackTime int `json:"playback_time"`
	// SocketPath is mpv's IPC socket, or a sentinel such as "android-intent"
	// when nothing local is playing.
	SocketPath string
}

// Skip is a stretch of an episode in whole seconds; zero when there is none.
type Skip struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// SkipTimes are the parts of an episode that can be skipped.
type SkipTimes struct {
	Op Skip `json:"op"`
	Ed Skip `json:"ed"`
	// Recap is a "previously on" stretch inside an episode, where one is on
	// file. Skipped under SkipRecap, like a whole recap episode.
	Recap Skip `json:"recap,omitempty"`
}

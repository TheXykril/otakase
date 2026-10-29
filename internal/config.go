package internal

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/icons"
)

// Config is every setting in otakase.conf. A field's config tag is its key
// in the file; "-" marks per-run state that is never written.
type Config struct {
	Player                   string   `config:"Player"`
	MpvArgs                  []string `config:"MpvArgs"`
	MpvSkin                  string   `config:"MpvSkin"`
	MpvPlaybackStartTimeout  int      `config:"MpvPlaybackStartTimeout"`
	SubsLanguage             string   `config:"SubsLanguage"`
	SubOrDub                 string   `config:"SubOrDub"`
	SubStyle                 string   `config:"SubStyle"`
	StoragePath              string   `config:"StoragePath"`
	AnimeNameLanguage        string   `config:"AnimeNameLanguage"`
	Theme                    string   `config:"Theme"`
	ThemeOverrides           string   `config:"ThemeOverrides"`
	ThemeFile                string   `config:"ThemeFile"`
	Icons                    string   `config:"Icons"`
	DownloadDir              string   `config:"DownloadDir"`
	DownloadFormat           string   `config:"DownloadFormat"`
	MenuOrder                string   `config:"MenuOrder"`
	MenuActions              string   `config:"MenuActions"`
	ContinueWatchingRows     int      `config:"ContinueWatchingRows"`
	PercentageToMarkComplete int      `config:"PercentageToMarkComplete"`
	NextEpisodePrompt        bool     `config:"NextEpisodePrompt"`
	AutoAudioFallback        bool     `config:"AutoAudioFallback"`
	SkipOp                   bool     `config:"SkipOp"`
	SkipEd                   bool     `config:"SkipEd"`
	SkipFiller               bool     `config:"SkipFiller"`
	ImagePreview             bool     `config:"ImagePreview"`
	SkipRecap                bool     `config:"SkipRecap"`
	RofiSelection            bool     `config:"RofiSelection"`
	// SubOrDubFlag records that -sub or -dub was given for this run, which
	// outranks the audio remembered for a show. Not a setting either.
	SubOrDubFlag        bool   `config:"-"`
	ScoreOnCompletion   bool   `config:"ScoreOnCompletion"`
	SaveMpvSpeed        bool   `config:"SaveMpvSpeed"`
	AddMissingOptions   bool   `config:"AddMissingOptions"`
	AlternateScreen     bool   `config:"AlternateScreen"`
	DiscordPresence     bool   `config:"DiscordPresence"`
	DiscordClientId     string `config:"DiscordClientId"`
	VimKeys             bool   `config:"VimKeys"`
	AnimeSkipClientID   string `config:"AnimeSkipClientID"`
	ContributeSkipTimes bool   `config:"ContributeSkipTimes"`
	IntroDBSkipTimes    bool   `config:"IntroDBSkipTimes"`
	CheckUpdates        bool   `config:"CheckUpdates"`
	DevBuilds           bool   `config:"DevBuilds"`
	MpvEpisodePlaylist  bool   `config:"MpvEpisodePlaylist"`
	// Provider, DisabledProviders and ManualProviderSearch are no longer
	// settings: sources are always picked automatically from the whole stack
	// and never named to the viewer. PopulateConfig fixes their values; tests
	// and debugging may still set them in code.
	Provider                   string `config:"-"`
	DisabledProviders          string `config:"-"`
	ManualProviderSearch       bool   `config:"-"`
	TrackingLocal              bool   `config:"TrackingLocal"`
	TrackingRemote             string `config:"TrackingRemote"`
	TrackingConfigured         bool   `config:"TrackingConfigured"`
	MyAnimeListClientID        string `config:"MyAnimeListClientID"`
	MyAnimeListClientSecret    string `config:"MyAnimeListClientSecret"`
	MyAnimeListImported        bool   `config:"MyAnimeListImported"`
	MyAnimeListImportDismissed bool   `config:"MyAnimeListImportDismissed"`
	ShowNewEpisodes            bool   `config:"ShowNewEpisodes"`
	CastDevice                 string `config:"CastDevice"`
	// CastTerminal is the terminal emulator to open for a cast started from
	// rofi, which has no terminal of its own to show controls in. Empty means
	// $TERMINAL, then whatever is installed.
	CastTerminal string `config:"CastTerminal"`
	// LinkMenus is where an otakase:// link opened from the desktop shows
	// its menus: "rofi" or "terminal". rofi falls back to a terminal when it
	// is not installed.
	LinkMenus string `config:"LinkMenus"`
	// CastPort is the port the stream server listens on, or 0 for a random
	// free one. Set it when a firewall drops inbound connections by default:
	// a random port cannot be allowed through without opening the whole
	// ephemeral range, and a device that cannot fetch looks identical to one
	// that never started.
	CastPort int `config:"CastPort"`
	// CastDiscoveryPort is the UDP port discovery sends from and hears TVs
	// answer on, or 0 for a random one. Fixed by default so a firewall needs
	// one rule for it rather than every UDP port.
	CastDiscoveryPort int `config:"CastDiscoveryPort"`
	// KodiHost names Kodi instances to offer as cast devices when discovery
	// does not find them -- another subnet, or zeroconf turned off in Kodi.
	// host or host:port, several separated by commas.
	KodiHost     string `config:"KodiHost"`
	KodiUser     string `config:"KodiUser"`
	KodiPassword string `config:"KodiPassword"`
	// CastBurnSubtitles draws a stream's subtitles into the picture when
	// casting it. The Chromecast cannot render the subtitle files these
	// providers supply, so this is the only way to see them on a soft-subbed
	// stream -- at the cost of re-encoding the video, which the copy path
	// never does.
	CastBurnSubtitles bool `config:"CastBurnSubtitles"`
	// CastWaitForSubtitles holds a cast whose subtitles are inside the file (a
	// torrent release) until the whole file has downloaded, so they are drawn
	// from the first frame with no rebuilds. Off, the cast starts within
	// seconds and the stream is rebuilt a few times early on as more of them
	// are read.
	CastWaitForSubtitles bool `config:"CastWaitForSubtitles"`
	// CastSubtitleSize is the size of dialogue in burned ASS subtitles, as a
	// percentage of what the release wrote: they are sized for a screen up
	// close, and a TV is watched from across the room. Signs and typesetting
	// keep their size.
	CastSubtitleSize int `config:"CastSubtitleSize"`
	// CastBrowser lists "Browser" among the cast devices: otakase serves a
	// player page on the LAN, and any phone, TV or console with a browser
	// plays the episode by opening its link.
	CastBrowser bool `config:"CastBrowser"`
	// CastBrowserPort is the port that page is served on. Fixed so the link
	// stays the same between episodes and runs, and a firewall needs one rule.
	CastBrowserPort int `config:"CastBrowserPort"`
	// CastBrowserSubtitles is how a browser shows a provider's subtitles:
	// "soft" hands it the subtitle file to draw and switch on or off, plain
	// text without the release's styling; "burn" draws them into the picture,
	// styled, at the cost of re-encoding the video.
	CastBrowserSubtitles string `config:"CastBrowserSubtitles"`
	// CastEncoder forces the encoder used for that: "vaapi", "software", or
	// empty to detect what this machine can actually do.
	CastEncoder string `config:"CastEncoder"`
	// CastNextEpisode is what a cast does when an episode ends: "countdown"
	// counts down on the panel and plays the next one, "stop" ends the cast.
	// Local playback has NextEpisodePrompt for the same question.
	CastNextEpisode string `config:"CastNextEpisode"`
	// Quality is the picture height to play HLS streams at: "best" leaves
	// the master playlist as the provider gave it, a number of lines (1080,
	// 720, 480) picks the nearest variant at or below. A show can override
	// it in show_prefs.json.
	Quality string `config:"Quality"`
	// AdultContent shows 18+ titles in your lists and in search. Off hides
	// them; they stay on the tracker and keep syncing.
	AdultContent bool `config:"AdultContent"`
	// ContinueWatchingAdult lets 18+ shows into the continue-watching rows.
	// They open the home menu, so it is off by default and needs AdultContent.
	ContinueWatchingAdult bool `config:"ContinueWatchingAdult"`
	// CastToDevice records that -cast was given for this run. It is not a
	// setting, so it carries no config tag.
	CastToDevice bool `config:"-"`
	// CastNonInteractive records that this process is the terminal a rofi cast
	// was handed off to, and so has a viewer who is not at the keyboard.
	//
	// It is not a setting either. What it changes is that a prompt with no
	// answer available resolves to its declared default instead of drawing a
	// menu: the process-global cast reader is parked in os.Stdin.Read for the
	// whole run and would split the keystrokes with a Bubble Tea reader on the
	// same descriptor, and a menu nobody is at blocks the season outright.
	// See wiki/Developer-Cast-Window-Prompts.md.
	CastNonInteractive bool `config:"-"`
	// ExperimentalMovies turns on the Movies menu: non-anime movies from a
	// site outside AniList and MyAnimeList, tracked locally only. Off by
	// default while it is experimental.
	ExperimentalMovies bool `config:"ExperimentalMovies"`
	// MovieProvider is where movies are searched. No longer a setting:
	// PopulateConfig always sets "all" (every provider at once).
	MovieProvider string `config:"-"`
	// TraktClientID and TraktClientSecret name another Trakt app to sync
	// movies through than otakase's own, which is used when they are empty.
	TraktClientID     string `config:"TraktClientID"`
	TraktClientSecret string `config:"TraktClientSecret"`
	// DiscordBotURL is the Otakase Discord bot's API, for linking the app to
	// the server, news and reports. Empty turns all of that off.
	DiscordBotURL string `config:"DiscordBotURL"`
	// DiscordShareWatching lets a linked app tell the server what is playing.
	DiscordShareWatching bool `config:"DiscordShareWatching"`
	// DiscordWatchWithMe puts a Watch with me button in the Discord status
	// while an episode plays: it joins a synced party this app hosts.
	DiscordWatchWithMe bool `config:"DiscordWatchWithMe"`
	// DiscordShareRatings is "ask" to offer sharing a score given on
	// finishing a show in the Otakase Discord, "never" to not ask.
	DiscordShareRatings string `config:"DiscordShareRatings"`
}

// MpvPlaybackStartTimeout is in seconds: 20 unless set, never more than
// ten minutes.
const (
	MpvStartWaitSeconds        = 20
	maxMpvPlaybackStartTimeout = 10 * 60
)

// MpvPlaybackStartTimeoutDuration is how long to wait for mpv to start playing.
func MpvPlaybackStartTimeoutDuration(config *Config) time.Duration {
	if config == nil || config.MpvPlaybackStartTimeout <= 0 {
		return MpvStartWaitSeconds * time.Second
	}
	return time.Duration(config.MpvPlaybackStartTimeout) * time.Second
}

var globalConfig *Config

// ConfigFilePath is the config file this run loaded.
var ConfigFilePath string

func SetGlobalConfig(config *Config) { globalConfig = config }

func GetGlobalConfig() *Config { return globalConfig }

// GetStoragePath is the storage directory of the running config, or the
// default one before a config is loaded.
func GetStoragePath() string {
	if globalConfig != nil && globalConfig.StoragePath != "" {
		return os.ExpandEnv(globalConfig.StoragePath)
	}
	return filepath.Join(os.Getenv("HOME"), ".local", "share", AppName)
}

// VimKeysEnabled reports whether menus take vim-style motions.
func VimKeysEnabled(config *Config) bool {
	if config == nil {
		config = globalConfig
	}
	return config != nil && config.VimKeys
}

func useEnglishAnimeNames(config *Config) bool {
	return config == nil || strings.EqualFold(strings.TrimSpace(config.AnimeNameLanguage), "english")
}

// remoteTrackerAliases maps every accepted spelling of TrackingRemote to its
// canonical value.
var remoteTrackerAliases = map[string]string{
	"": TrackingRemoteNone, "none": TrackingRemoteNone, "disabled": TrackingRemoteNone,
	"off": TrackingRemoteNone, "local": TrackingRemoteNone,

	"anilist": TrackingRemoteAniList, "ani-list": TrackingRemoteAniList,

	"myanimelist": TrackingRemoteMyAnimeList, "my_anime_list": TrackingRemoteMyAnimeList,
	"my-anime-list": TrackingRemoteMyAnimeList, "mal": TrackingRemoteMyAnimeList,

	"both": TrackingRemoteBoth, "dual": TrackingRemoteBoth,
	"anilist+myanimelist": TrackingRemoteBoth, "anilist,myanimelist": TrackingRemoteBoth,
	"myanimelist+anilist": TrackingRemoteBoth, "myanimelist,anilist": TrackingRemoteBoth,
}

// canonicalTrackerName is the canonical TrackingRemote for value, or ""
// when it isn't one.
func canonicalTrackerName(value string) string {
	return remoteTrackerAliases[strings.ToLower(strings.TrimSpace(value))]
}

// normalizeTrackingConfig settles the tracking settings: local tracking is
// always on, and an unknown remote falls back to AniList and asks again.
func normalizeTrackingConfig(c *Config) {
	if c == nil {
		return
	}
	c.TrackingLocal = true // not optional
	remote := canonicalTrackerName(c.TrackingRemote)
	if remote == "" {
		// Unknown value: default to AniList and have the picker ask again.
		remote = TrackingRemoteAniList
		c.TrackingConfigured = false
	}
	c.TrackingRemote = remote
}

// categoryIcon is the icon for a main menu entry or a list category.
func categoryIcon(key string) icons.Icon {
	switch key {
	case "CURRENT":
		return icons.Watching
	case "ALL":
		return icons.AllShows
	case "UNTRACKED":
		return icons.Search
	case "UPDATE":
		return icons.Edit
	case "REMAP_PROVIDER":
		return icons.Remap
	case "CONTINUE_LAST":
		return icons.History
	case "SURPRISE":
		return icons.Surprise
	case "PLANNING":
		return icons.Planning
	case "COMPLETED":
		return icons.Completed
	case "PAUSED":
		return icons.Paused
	case "DROPPED":
		return icons.Dropped
	case "REWATCHING", "REPEATING":
		return icons.Rewatching
	case "TRACKER":
		return icons.Tracker
	case "CAST":
		return icons.Cast
	case "STATS":
		return icons.Stats
	case "MOVIES":
		return icons.TV
	case "COMMUNITY":
		return icons.Info
	}
	return 0
}

// menuKeys is every key the home menu names, in order: MenuOrder's, then
// MenuActions'. Configs written before MenuActions existed keep their actions
// in MenuOrder, so either setting may hold either kind; a key named twice
// counts where it first appears.
func menuKeys(config *Config) []string {
	if config == nil {
		return nil
	}
	keys := []string{}
	seen := map[string]bool{}
	for _, raw := range strings.Split(config.MenuOrder+","+config.MenuActions, ",") {
		key := strings.ToUpper(strings.TrimSpace(raw))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys
}

// defaultMenuKeys is the home menu of a fresh config.
func defaultMenuKeys() []string {
	defaults := defaultConfigMap()
	return menuKeys(&Config{MenuOrder: defaults["MenuOrder"], MenuActions: defaults["MenuActions"]})
}

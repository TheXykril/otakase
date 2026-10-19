package internal

// defaultConfigMap is the value every setting takes when the file leaves it
// out, as it would be written to the file.
func defaultConfigMap() map[string]string {
	return map[string]string{
		// Player
		"Player":                   "mpv",
		"MpvArgs":                  "[]",
		"MpvSkin":                  "true",
		"MpvPlaybackStartTimeout":  "20",
		"MpvEpisodePlaylist":       "true",
		"SaveMpvSpeed":             "true",
		"PercentageToMarkComplete": "85",
		"NextEpisodePrompt":        "false",
		"Quality":                  "best",

		// Audio and subtitles. A show that only exists in the other language
		// plays rather than asking a question with one useful answer.
		"SubOrDub":          "sub",
		"SubStyle":          "ask",
		"SubsLanguage":      "english",
		"AutoAudioFallback": "true",

		// Skipping
		"SkipOp":              "true",
		"SkipEd":              "true",
		"SkipFiller":          "true",
		"SkipRecap":           "true",
		"AnimeSkipClientID":   "",
		"ContributeSkipTimes": "true",
		"IntroDBSkipTimes":    "true",

		// Menus and look. The home menu is kept short: every entry is a rofi
		// row, and the lists left out are a tab away in Show All.
		"MenuOrder":            "CURRENT,PLANNING,ALL",
		"MenuActions":          "CONTINUE_LAST,UNTRACKED,UPDATE,CAST,SURPRISE,STATS,TRACKER,PROVIDER",
		"ContinueWatchingRows": "5",
		"AnimeNameLanguage":    "english",
		"RofiSelection":        "false",
		"ImagePreview":         "false",
		"AlternateScreen":      "true",
		"VimKeys":              "false",
		"Theme":                "auto",
		"ThemeOverrides":       "",
		"ThemeFile":            "",
		"Icons":                "auto",
		"LinkMenus":            "rofi",
		"ShowNewEpisodes":      "true",

		// Tracking
		"TrackingLocal":              "true",
		"TrackingRemote":             "anilist",
		"TrackingConfigured":         "false",
		"ScoreOnCompletion":          "true",
		"MyAnimeListClientID":        "46e39244e6635ceac2df59d37dbaa43f",
		"MyAnimeListClientSecret":    "",
		"MyAnimeListImported":        "false",
		"MyAnimeListImportDismissed": "false",

		// Providers
		"Provider":              "stacked",
		"DisabledProviders":     "[]",
		"ManualProviderSearch":  "false",
		"AdultContent":          "false",
		"ContinueWatchingAdult": "false",

		// Storage and downloads
		"StoragePath":    StoragePathDefault,
		"DownloadDir":    "$HOME/Downloads/" + AppName,
		"DownloadFormat": "mkv",

		// Updates
		"CheckUpdates":      "true",
		"DevBuilds":         "false",
		"AddMissingOptions": "true",

		// Discord
		"DiscordPresence":      "true",
		"DiscordClientId":      OtakaseDiscordAppID,
		"DiscordBotURL":        DefaultDiscordBotURL,
		"DiscordShareWatching": "false",
		"DiscordWatchWithMe":   "false",
		"DiscordShareRatings":  "ask",

		// Casting
		"CastDevice":        "",
		"CastTerminal":      "",
		"CastPort":          "0",
		"CastDiscoveryPort": "8011",
		"CastBurnSubtitles": "true",
		"CastEncoder":       "",
		"CastNextEpisode":   CastNextEpisodeCountdown,
		"KodiHost":          "",
		"KodiUser":          "",
		"KodiPassword":      "",

		// Movies
		"ExperimentalMovies": "false",
		"MovieSite":          "https://8filmai.eu",
		"MovieProvider":      "all",
		"TraktClientID":      "",
		"TraktClientSecret":  "",
	}
}

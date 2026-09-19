package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/thexykril/otakase/internal"
)

var version string // Will be set by ldflags during build

func resolvedVersion() string {
	if version == "" {
		return "2.0.7"
	}

	return version
}

func main() {
	var anime internal.Anime
	var user internal.User

	internal.SetGlobalAnime(&anime)
	internal.SetGlobalUser(&user)

	configDir, err := os.UserConfigDir()
	if err != nil {
		// Fallback if UserConfigDir fails
		if runtime.GOOS == "windows" {
			configDir = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		} else {
			configDir = filepath.Join(os.Getenv("HOME"), ".config")
		}
	}

	// An install from before the rename keeps its config, tokens and history
	// under the old name; carry them over before anything looks for them.
	if notes, migrateErr := internal.MigrateLegacyAppDirs(configDir, os.Getenv("HOME")); migrateErr != nil {
		fmt.Fprintf(os.Stderr, "Could not carry over the previous install: %v\n", migrateErr)
	} else {
		for _, note := range notes {
			fmt.Println(note)
		}
	}

	configFilePath := filepath.Join(configDir, internal.AppName, internal.ConfigFileName())

	// load userConfig
	userConfig, err := internal.LoadConfig(configFilePath)
	if err != nil {
		fmt.Println("Error loading config:", err)
		return
	}
	internal.SetVersion(resolvedVersion())
	if updated, migrateErr := internal.MigrateOnVersionUpgrade(configFilePath, &userConfig, resolvedVersion()); migrateErr != nil {
		fmt.Println("Error applying storage migration:", migrateErr)
		return
	} else if updated {
		fmt.Println("Updated config for this " + internal.DisplayName + " version (new options and/or migrations).")
	}
	internal.SetGlobalConfig(&userConfig)

	storageDir := os.ExpandEnv(userConfig.StoragePath)
	logFile := filepath.Join(storageDir, internal.LogFileName)
	internal.SetGlobalLogFile(logFile)
	internal.ClearLogFile(logFile)
	// The log used to be a bare debug.log. Leaving one behind would be a file
	// that looks current and never changes again, which is worth more confusion
	// than the byte it saves -- it is truncated at every launch anyway, so
	// nothing in it is wanted.
	internal.RemoveLegacyLogFile(storageDir)

	// Flags configured here cause userconfig needs to be changed.
	flag.StringVar(&userConfig.Player, "player", userConfig.Player, "Player binary for playback (mpv-compatible; falls back to mpv if unavailable)")
	flag.StringVar(&userConfig.StoragePath, "storage-path", userConfig.StoragePath, "Path to the storage directory")
	flag.StringVar(&userConfig.SubsLanguage, "subs-lang", userConfig.SubsLanguage, "Subtitles language")
	flag.IntVar(&userConfig.PercentageToMarkComplete, "percentage-to-mark-complete", userConfig.PercentageToMarkComplete, "Percentage to mark episode as complete")

	// Boolean flags that accept true/false
	flag.BoolVar(&userConfig.NextEpisodePrompt, "next-episode-prompt", userConfig.NextEpisodePrompt, "Prompt for the next episode (true/false)")
	flag.BoolVar(&userConfig.SkipOp, "skip-op", userConfig.SkipOp, "Skip opening (true/false)")
	flag.BoolVar(&userConfig.SkipEd, "skip-ed", userConfig.SkipEd, "Skip ending (true/false)")
	flag.BoolVar(&userConfig.SkipFiller, "skip-filler", userConfig.SkipFiller, "Skip filler episodes (true/false)")
	flag.BoolVar(&userConfig.SkipRecap, "skip-recap", userConfig.SkipRecap, "Skip recap (true/false)")
	flag.BoolVar(&userConfig.ScoreOnCompletion, "score-on-completion", userConfig.ScoreOnCompletion, "Score on episode completion (true/false)")
	flag.BoolVar(&userConfig.SaveMpvSpeed, "save-mpv-speed", userConfig.SaveMpvSpeed, "Save MPV speed setting (true/false)")
	flag.BoolVar(&userConfig.DiscordPresence, "discord-presence", userConfig.DiscordPresence, "Enable Discord presence (true/false)")
	flag.StringVar(&userConfig.DiscordClientId, "discord-client-id", userConfig.DiscordClientId, "Discord client ID for Rich Presence")
	flag.BoolVar(&userConfig.VimKeys, "vim-keys", userConfig.VimKeys, "Enable vim motions in selection menus (j/k/h/l, / search) (true/false)")
	flag.BoolVar(&userConfig.CheckUpdates, "check-updates", userConfig.CheckUpdates, "Check for updates in the background when idle (true/false)")
	flag.BoolVar(&userConfig.ShowNewEpisodes, "show-new-episodes", userConfig.ShowNewEpisodes, "Show new episode indicators in currently watching list (true/false)")
	continueLast := flag.Bool("c", false, "Continue last episode")
	addNewAnime := flag.Bool("new", false, "Add new anime")
	rofiSelection := flag.Bool("rofi", false, "Open selection in rofi")
	noRofi := flag.Bool("no-rofi", false, "No rofi")
	imagePreview := flag.Bool("image-preview", false, "Show image preview")
	noImagePreview := flag.Bool("no-image-preview", false, "No image preview")
	changeToken := flag.Bool("change-token", false, "Change token")
	setupAnimeSkip := flag.Bool("setup-anime-skip", false, "Create a personal Anime-Skip client id and save it")
	currentCategory := flag.Bool("current", false, "Current category")
	updateScript := flag.Bool("u", false, "Update the script")
	editConfig := flag.Bool("e", false, "Edit config")
	subFlag := flag.Bool("sub", false, "Watch sub version")
	dubFlag := flag.Bool("dub", false, "Watch dub version")
	softSubFlag := flag.Bool("softsub", false, "Prefer soft subtitles when available (anineko)")
	hardSubFlag := flag.Bool("hardsub", false, "Prefer hard subtitles when available (anineko)")
	versionFlag := flag.Bool("v", false, "Print version information")
	downloadFlag := flag.Bool("download", false, "Download episodes instead of playing them (requires ffmpeg)")
	downloadRange := flag.String("episodes", "", "Episodes to download, e.g. 5 or 1-12 (default: the selected episode)")
	flag.StringVar(&userConfig.DownloadDir, "download-dir", userConfig.DownloadDir, "Directory to save downloaded episodes into")
	providerStatus := flag.Bool("provider-status", false, "Probe every provider and report which ones work")
	installKeybind := flag.Bool("install-keybind", false, "Add a Super+Shift+A Hyprland binding that opens the rofi menu")
	removeKeybind := flag.Bool("remove-keybind", false, "Remove the Hyprland binding added by -install-keybind")
	refreshKeybind := flag.Bool("refresh-keybind", false, "Update an existing Hyprland binding, adding nothing if absent")
	forceKeybind := flag.Bool("force-keybind", false, "Let -install-keybind replace a binding something else owns")
	providerStatusQuery := flag.String("provider-status-query", "one piece", "Search query used by -provider-status")

	// Custom help/usage function
	flag.Usage = func() {
		internal.RestoreScreen()
		fmt.Fprintf(os.Stderr, "%s is a CLI tool to manage anime playback with advanced features like skipping intro, outro, filler, recap, tracking progress, and integrating with Discord.\n", internal.DisplayName)
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults() // This prints the default flag information
	}

	flag.Parse()

	// Validate PercentageToMarkComplete range (0-100) from CLI flag
	if userConfig.PercentageToMarkComplete < 0 {
		userConfig.PercentageToMarkComplete = 0
	} else if userConfig.PercentageToMarkComplete > 100 {
		userConfig.PercentageToMarkComplete = 100
	}

	// Check version before screen clearing
	if *versionFlag {
		fmt.Printf("%s version: %s\n", internal.DisplayName, resolvedVersion())
		os.Exit(0)
	}

	// Diagnostics run before any UI setup so the output stays plain and pipeable.
	// Editing the user's Hyprland config is a command they run deliberately,
	// never something an install does behind their back.
	if *installKeybind || *removeKeybind || *refreshKeybind {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not find your home directory: %v\n", err)
			os.Exit(1)
		}
		var notes []string
		switch {
		case *removeKeybind:
			notes, err = internal.RemoveHyprlandKeybind(home)
		case *refreshKeybind:
			notes, err = internal.RefreshHyprlandKeybind(home)
		default:
			notes, err = internal.InstallHyprlandKeybind(home, *forceKeybind)
		}
		for _, note := range notes {
			fmt.Println(note)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	if *providerStatus {
		report := internal.CheckProviders(&userConfig, *providerStatusQuery)
		fmt.Print(internal.FormatProviderStatus(report, *providerStatusQuery))
		for _, health := range report {
			if health.OK() {
				os.Exit(0)
			}
		}
		os.Exit(1)
	}

	anime.Ep.ContinueLast = *continueLast

	// Apply UI flags before -u so password prompt mode (terminal vs GTK) is correct.
	if *rofiSelection {
		userConfig.RofiSelection = true
	}
	if *noRofi || runtime.GOOS == "windows" {
		userConfig.RofiSelection = false
	}
	// `otakase -u` is a CLI operation: always use the terminal for sudo when stdin is a TTY,
	// even if RofiSelection is enabled in the config file. `otakase -e` is the same:
	// it runs an editor in this terminal, so its messages belong here too --
	// otherwise a failure is delivered as a desktop notification and the terminal
	// the user is looking at stays silent.
	if *updateScript || *editConfig {
		userConfig.RofiSelection = false
	}
	internal.SetGlobalConfig(&userConfig)

	if *updateScript {
		repo := internal.DefaultUpdateRepo
		fileName := internal.AppName

		if err := internal.SelfUpdate(repo, fileName); err != nil {
			fmt.Fprintf(os.Stderr, "Error updating executable: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Program Updated!")
		os.Exit(0)
	}

	if *currentCategory {
		userConfig.CurrentCategory = true
		userConfig.CurrentCategoryFlag = true
	}

	if *imagePreview {
		userConfig.ImagePreview = true
	}

	if *noImagePreview || runtime.GOOS == "windows" {
		userConfig.ImagePreview = false
	}

	if *editConfig {
		internal.EditConfig(configFilePath)
		return
	}

	// Resolve the colour palette before anything draws. On Omarchy this follows
	// the desktop theme; elsewhere it is otakase's own palette.
	internal.ApplyThemeFromConfig(&userConfig)

	if userConfig.RofiSelection {
		// Themes are rendered from the palette on every run, so a desktop theme
		// change is picked up without the user clearing anything. This also means
		// the menus no longer need the network before they can be shown.
		if err := internal.WriteRofiThemes(os.ExpandEnv(userConfig.StoragePath)); err != nil {
			internal.Log(fmt.Sprintf("Error writing rofi themes: %v", err))
			internal.Out(fmt.Sprintf("Error writing rofi themes: %v", err))
			internal.Exit(err)
		}
	}

	if err := internal.EnsureTrackingConfigured(&userConfig); err != nil {
		fmt.Println("Error configuring tracking:", err)
		return
	}
	internal.SetGlobalConfig(&userConfig)

	if *changeToken {
		internal.ChangeTrackingToken(&userConfig, &user)
		return
	}

	if *setupAnimeSkip {
		if err := internal.SetupAnimeSkipClientID(&userConfig); err != nil {
			fmt.Println("Anime-Skip setup failed:", err)
			os.Exit(1)
		}
		return
	}

	// Setup screen for interactive mode (only if not changing token)
	internal.ClearScreen()
	internal.InstallTerminalInterruptHandler()
	defer internal.RestoreScreen()

	// Set SubOrDub based on the flags
	if *subFlag {
		userConfig.SubOrDub = "sub"
	} else if *dubFlag {
		userConfig.SubOrDub = "dub"
	}
	if *softSubFlag {
		userConfig.SubStyle = "soft"
	} else if *hardSubFlag {
		userConfig.SubStyle = "hard"
	}

	// Show update found by a previous idle check (no network on the hot path).
	if internal.HandlePendingUpdatePrompt(&userConfig, resolvedVersion()) {
		return
	}

	// Idle background check — does not block startup; stores result for next launch.
	internal.StartBackgroundUpdateCheck(&userConfig, resolvedVersion())

	// From here on the launch can block on the network, with no terminal to show
	// it when otakase was started from a keybind.
	internal.BeginStartupProgress(&userConfig, "Otakase is starting")

	// Get the token from the token file for the configured remote tracker.
	if internal.UsesRemoteTracking(&userConfig) {
		internal.StartupStage("Signing in to your tracker")
		if err := internal.EnsureConfiguredTrackersReady(&userConfig, &user); err != nil {
			internal.Log("Error preparing trackers: " + err.Error())
			internal.Exit(err)
		}
	}

	// Load animes in database
	databaseFile := filepath.Join(os.ExpandEnv(userConfig.StoragePath), "curd_history.txt")
	databaseAnimes := internal.LocalGetAllAnime(databaseFile)

	if *addNewAnime {
		internal.AddNewAnime(&userConfig, &anime, &user, &databaseAnimes)
		// internal.Exit(fmt.Errorf("Added new anime!"))
	}

	internal.StartupStage("Loading your anime list")
	internal.Setup(&userConfig, &anime, &user, &databaseAnimes)

	temp_anime, err := internal.FindAnimeByAnilistID(user.AnimeList, strconv.Itoa(anime.AnilistId))
	if err != nil {
		internal.Log("Error finding anime by Anilist ID: " + err.Error())
	}

	if err == nil && temp_anime != nil && anime.TotalEpisodes == temp_anime.Progress && temp_anime.Status != "CURRENT" {
		internal.Log(temp_anime.Progress)
		internal.Log(anime.TotalEpisodes)
		internal.Log(user.AnimeList)
		internal.Log("Rewatching anime: " + internal.GetAnimeName(anime))
		anime.Rewatching = true
	}

	anime.Ep.Player.Speed = 1.0
	if userConfig.DiscordPresence {
		internal.Out("Starting Discord broadcast.")
	}

	// Get filler list concurrently
	go func() {
		// Get MAL ID first if not already set
		if anime.MalId == 0 {
			malID, err := internal.GetAnimeMalID(anime.AnilistId)
			if err != nil {
				internal.Log("Error getting MAL ID: " + err.Error())
				return
			}
			anime.MalId = malID
		}

		fillerList, err := internal.FetchFillerEpisodes(anime.MalId)
		if err != nil {
			internal.Log("Error getting filler list: " + err.Error())
		} else {
			anime.FillerEpisodes = fillerList
			internal.Log("Filler list fetched successfully")
			// fmt.Println("Filler episodes: ", anime.FillerEpisodes)
		}
	}()

	// Main loop (loop to keep starting new episodes)
	for {

		internal.Log(anime)

		// Create a channel to signal when to exit the skip loop
		var wg sync.WaitGroup
		skipLoopDone := make(chan struct{})
		skipLoopClosed := make(chan bool, 1) // Channel to track if skipLoopDone has been closed
		skipLoopClosed <- false              // Initialize to false (not closed yet)

		// Get MalId and CoverImage (only if discord presence is enabled)
		if userConfig.DiscordPresence {
			anime.MalId, anime.CoverImage, err = internal.GetAnimeIDAndImage(anime.AnilistId)
			if err != nil {
				internal.Log("Error getting anime ID and image: " + err.Error())
			}
			// Skip initial Discord presence - wait for MPV to provide real duration
			// This avoids showing the default 25-minute duration before the video starts
			internal.Log("Waiting for MPV to start to get actual video duration before showing Discord presence")
		} else if anime.MalId == 0 {
			anime.MalId, err = internal.GetAnimeMalID(anime.AnilistId)
			if err != nil {
				internal.Log("Error getting anime MAL ID: " + err.Error())
			}
		}

		// Start otakase (loop while episode is playing)
		for {
			// Check if current episode is filler/recap
			if episodeErr := internal.GetEpisodeData(anime.MalId, anime.Ep.Number, &anime); episodeErr != nil {
				internal.Log("Error getting episode data, assuming non-filler: " + episodeErr.Error())
				break // Break the loop and continue with playback
			}

			// Check if episode is filler
			anime.Ep.IsFiller = internal.IsEpisodeFiller(anime.FillerEpisodes, anime.Ep.Number)

			// If not filler/recap (or skip is disabled), break and continue with playback
			if !((anime.Ep.IsFiller && userConfig.SkipFiller) || (anime.Ep.IsRecap && userConfig.SkipRecap)) {
				if anime.Ep.LastWasSkipped {
					go internal.UpdateAnimeProgress(user.Token, anime.AnilistId, anime.Ep.Number-1)
				}
				break
			}

			// If it is filler/recap, log it and move to next episode
			if anime.Ep.IsFiller {
				internal.Out(fmt.Sprint("Filler episode, skipping: ", anime.Ep.Number))
				// Get next canon episode
				anime.Ep.Number = internal.GetNextCanonEpisode(anime.FillerEpisodes, anime.Ep.Number)
			} else {
				internal.Out(fmt.Sprint("Recap episode, skipping: ", anime.Ep.Number))
				anime.Ep.Number++
			}

			anime.Ep.LastWasSkipped = true
			anime.Ep.Started = false
			internal.LocalUpdateAnime(databaseFile, anime.AnilistId, anime.ProviderId, anime.Ep.Number, 0, 0, internal.GetAnimeName(anime), internal.CurrentAnimeProviderName(&anime))

			// Check if we've reached the end of the series
			if anime.TotalEpisodes > 0 && anime.Ep.Number > anime.TotalEpisodes {
				internal.Out("Reached end of series")
				internal.Exit(nil)
			}
		}

		// Downloading reuses everything above -- tracker selection, provider
		// mapping, episode resolution -- and simply saves the stream instead of
		// handing it to MPV.
		if *downloadFlag {
			from, to, rangeErr := internal.ParseEpisodeRange(*downloadRange, anime.Ep.Number)
			if rangeErr != nil {
				internal.Out(rangeErr.Error())
				internal.Exit(rangeErr)
			}

			dir := internal.ResolveDownloadDir(&userConfig)
			internal.Out(fmt.Sprintf("Downloading episodes %d-%d to %s", from, to, dir))

			results := internal.DownloadEpisodes(userConfig, &anime, from, to, dir)
			failed := 0
			for _, result := range results {
				if result.Err != nil {
					failed++
				}
			}
			internal.Out(fmt.Sprintf("Downloaded %d of %d episode(s).", len(results)-failed, len(results)))
			if failed > 0 {
				internal.Exit(fmt.Errorf("%d episode(s) failed to download", failed))
			}
			internal.Exit(nil)
			return
		}

		// Now start playback for the non-filler episode
		anime.Ep.Player.SocketPath = internal.StartPlayback(&userConfig, &anime)
		internal.Log(fmt.Sprint("Playback starting time: ", anime.Ep.Player.PlaybackTime))
		internal.Log(anime.Ep.Player.SocketPath)

		// StartPlayback reports "could not start playback" by returning an empty
		// socket path, having already said why. Continuing past it started the
		// playback watchers for a session that does not exist, and they then
		// polled a socket that would never answer -- once a second, forever.
		if anime.Ep.Player.SocketPath == "" {
			internal.Log("Playback did not start; no MPV socket")
			internal.Exit(nil)
			return
		}

		// After playback is running, lazily build MPV episode playlist / audio
		// options while idle (no startup cost, no mid-buffer stutter).
		if anime.Ep.Player.SocketPath != "" && anime.Ep.Player.SocketPath != "android-intent" {
			internal.StartMPVPlaylistController(&userConfig, &anime, anime.Ep.Player.SocketPath, skipLoopDone)
		}

		// Handle Android Intent external player
		if anime.Ep.Player.SocketPath == "android-intent" {
			internal.Out(fmt.Sprintf("\nOpened external player for Episode %d.", anime.Ep.Number))
			internal.Out("Press Enter when you have finished watching...")

			// Wait for user input to confirm completion. A stdin that has
			// ended is not someone pressing enter, and this loop starts the
			// next episode each time round.
			if !internal.AwaitEnter() {
				internal.Exit(nil)
			}

			// Mark as completed
			anime.Ep.IsCompleted = true

			// Update progress for the finished episode
			// Local update
			internal.LocalUpdateAnime(databaseFile, anime.AnilistId, anime.ProviderId, anime.Ep.Number, 0, 0, internal.GetAnimeName(anime), internal.CurrentAnimeProviderName(&anime))

			// Check if we should continue to next episode
			// On Android we always prompt because we don't know exactly when video ended
			shouldContinue := internal.NextEpisodePromptCLI(&userConfig)

			if shouldContinue {
				internal.StartNextEpisode(&anime, &userConfig, databaseFile, user.Token)
				continue
			} else {
				// Handle completion if this was the last episode
				if anime.Ep.Number == anime.TotalEpisodes {
					internal.HandleLastEpisodeCompletion(&userConfig, &anime, user.Token)
				}
				// Update progress for the just finished episode (StartNextEpisode usually does this for previous ep, but here we exit)
				if !anime.Rewatching {
					internal.UpdateAnimeProgress(user.Token, anime.AnilistId, anime.Ep.Number)
				}
				internal.Exit(nil)
			}
		}

		wg.Add(1)
		// Get episode data
		go func() {
			defer wg.Done()
			if episodeErr := internal.GetEpisodeData(anime.MalId, anime.Ep.Number, &anime); episodeErr != nil {
				internal.Log("Error getting episode data: " + episodeErr.Error())
			} else {
				internal.Log(anime)

				// if filler episode or recap episode and skip is enabled
				if (anime.Ep.IsFiller && userConfig.SkipFiller) || (anime.Ep.IsRecap && userConfig.SkipRecap) {
					if anime.Ep.IsFiller && userConfig.SkipFiller {
						internal.Out(fmt.Sprint("Filler Episode, starting next episode: ", anime.Ep.Number+1))
						internal.Log("Filler episode detected")
					} else if anime.Ep.IsRecap && userConfig.SkipRecap {
						internal.Out(fmt.Sprint("Recap Episode, starting next episode: ", anime.Ep.Number+1))
						internal.Log("Recap episode detected")
					}

					anime.Ep.IsCompleted = true
					if !userConfig.NextEpisodePrompt {
						// fmt.Println("[DEBUG] Starting next episode from filler/recap skip")
						internal.StartNextEpisode(&anime, &userConfig, databaseFile, user.Token)
					} else {
						// When NextEpisodePrompt is enabled, just call StartNextEpisode - it handles Rofi prompting internally
						internal.ExitMPV(anime.Ep.Player.SocketPath)
						internal.StartNextEpisode(&anime, &userConfig, databaseFile, user.Token)
						return
					}
					// Send command to close MPV
					_, err := internal.MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"quit"})
					if err != nil {
						internal.Log("Error closing MPV: " + err.Error())
					}
					// Exit the skip loop - only close if not already closed
					select {
					case isClosed := <-skipLoopClosed:
						if !isClosed {
							close(skipLoopDone)
							skipLoopClosed <- true // Mark as closed
						}
					default:
						// Channel is busy, another goroutine is handling closure
					}
				}
			}
		}()

		wg.Add(1)
		// Thread to update Discord presence with simple position-gap seek detection
		go func() {
			defer wg.Done()
			if userConfig.DiscordPresence {
				var lastKnownPauseState bool = false
				var lastKnownPosition int = 0
				var lastStateCheck time.Time
				var discordPresenceInitialized bool = false // Track if Discord presence has been set with real duration

				for {
					select {
					case <-skipLoopDone:
						return
					default:
						// Get current state from MPV
						isPaused, err := internal.MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"get_property", "pause"})
						if err != nil {
							// Without this the loop retried a dead socket every 5s
							// forever, logging the same error each time (see
							// Wraient/curd#58).
							if internal.MPVConnectionGone(err) {
								internal.Log("MPV is gone, stopping Discord presence updates")
								return
							}
							internal.Log("Error getting pause status: " + err.Error())
							time.Sleep(5 * time.Second)
							continue
						}

						if isPaused == nil {
							isPaused = true
						}

						// Get current time position
						currentPos := 0
						timePos, err := internal.MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"get_property", "time-pos"})
						if err == nil && timePos != nil {
							if pos, ok := timePos.(float64); ok {
								currentPos = int(pos + 0.5) // Round to nearest integer
							}
						}

						currentPauseState, ok := isPaused.(bool)
						if !ok {
							internal.Log(fmt.Sprintf("Error: pause state is not a bool (%T)", isPaused))
							currentPauseState = true
						}

						// Simple seek detection: position gap > 5 seconds
						hasSeekEvent := false
						if lastKnownPosition > 0 {
							positionDiff := currentPos - lastKnownPosition
							if positionDiff < -5 || positionDiff > 7 { // 5 sec backward or 7 sec forward (allowing normal playback + buffer)
								hasSeekEvent = true
							}
						}

						hasPlayPauseEvent := currentPauseState != lastKnownPauseState

						// Determine if we should update Discord presence
						shouldUpdate := false

						// Force update every 30 seconds for Discord keep-alive
						if lastStateCheck.IsZero() || time.Since(lastStateCheck) >= 30*time.Second {
							shouldUpdate = true
						}

						// Update on pause state change
						if hasPlayPauseEvent {
							shouldUpdate = true
						}

						// Update on seek events
						if hasSeekEvent {
							shouldUpdate = true
						}

						if shouldUpdate {
							// Only update Discord if we have real duration OR if presence was already initialized
							totalDuration := anime.Ep.Duration
							if totalDuration == 0 {
								// Skip Discord updates until we have real duration from MPV
								if !discordPresenceInitialized {
									lastKnownPauseState = currentPauseState
									lastKnownPosition = currentPos
									lastStateCheck = time.Now()
									time.Sleep(2 * time.Second)
									continue
								}
								totalDuration = currentPos + 1 // Small duration to avoid divide by zero
							} else {
								discordPresenceInitialized = true // Mark as initialized once we have real duration
							}

							// Force update on seek events to bypass Discord's internal filtering
							var presenceErr error
							if hasSeekEvent {
								presenceErr = internal.DiscordPresenceWithForce(anime, currentPauseState, currentPos, totalDuration, userConfig.DiscordClientId, true)
							} else {
								presenceErr = internal.DiscordPresence(anime, currentPauseState, currentPos, totalDuration, userConfig.DiscordClientId)
							}

							if presenceErr != nil {
								internal.Log("Error setting Discord presence: " + presenceErr.Error())
							}

							lastKnownPauseState = currentPauseState
							lastStateCheck = time.Now()
						}

						// Always update position for next comparison
						lastKnownPosition = currentPos

						time.Sleep(2 * time.Second) // Check every 2 seconds
					}
				}
			}
		}()

		// Get skip times Parallel.
		//
		// Through the whole chain -- the provider's own timings, AniSkip,
		// Anime-Skip -- rather than AniSkip alone. This episode used to ask
		// only AniSkip while every later one in the playlist asked everything,
		// so a show AniSkip does not cover started unskipped and then began
		// skipping an episode later.
		go func() {
			resolution := internal.ApplySkipTimes(&anime, anime.Ep.Number, &userConfig, internal.GetProvider())
			internal.Log(anime.Ep.SkipTimes)
			internal.StartSkipMarker(&userConfig, &anime, anime.Ep.Player.SocketPath, resolution.IDs, skipLoopDone)
		}()

		// Get video duration
		go func() {
			for {
				if anime.Ep.Started {
					if anime.Ep.Duration == 0 {
						// Get video duration
						durationPos, err := internal.MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"get_property", "duration"})
						if err != nil {
							internal.Log("Error getting video duration: " + err.Error())
						} else if durationPos != nil {
							if duration, ok := durationPos.(float64); ok {
								anime.Ep.Duration = int(duration + 0.5) // Round to nearest integer
								internal.Log(fmt.Sprintf("Video duration: %d seconds", anime.Ep.Duration))

								// Initialize Discord presence with correct duration (first time with real duration)
								if userConfig.DiscordPresence {
									isPaused, _ := internal.MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"get_property", "pause"})
									currentPos := 0
									if timePos, err := internal.MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"get_property", "time-pos"}); err == nil && timePos != nil {
										if pos, ok := timePos.(float64); ok {
											currentPos = int(pos + 0.5)
										}
									}
									pauseState := false
									if isPaused != nil {
										if value, ok := isPaused.(bool); ok {
											pauseState = value
										} else {
											internal.Log(fmt.Sprintf("Error: pause state is not a bool (%T)", isPaused))
										}
									}
									internal.Log("Initializing Discord presence with real video duration")
									if presenceErr := internal.DiscordPresence(anime, pauseState, currentPos, anime.Ep.Duration, userConfig.DiscordClientId); presenceErr != nil {
										internal.Log("Discord presence error: " + presenceErr.Error())
									}
								}
							} else {
								internal.Log("Error: duration is not a float64")
							}
						}
						break
					}
				}
				time.Sleep(1 * time.Second)
			}
		}()

		// Thread for continuous next episode prompt in CLI mode (throughout episode duration)
		go func() {
			if userConfig.NextEpisodePrompt && !userConfig.RofiSelection {
				internal.NextEpisodePromptContinuous(&userConfig, databaseFile, user.Token)
				// If the function returns, it means user made a decision
				// Exit the skip loop - only close if not already closed
				select {
				case isClosed := <-skipLoopClosed:
					if !isClosed {
						close(skipLoopDone)
						skipLoopClosed <- true // Mark as closed
					}
				default:
					// Channel is busy, another goroutine is handling closure
				}
			}
		}()

		wg.Add(1)
		// Thread to update playback time in database
		go func() {
			defer wg.Done()
			for {
				select {
				case <-skipLoopDone:
					return
				default:
					time.Sleep(1 * time.Second)

					// Get current playback time
					// internal.Log("Getting playback time "+anime.Ep.Player.SocketPath)
					timePos, err := internal.MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"get_property", "time-pos"})
					if err != nil {
						internal.Log("Error getting playback time: " + err.Error())

						// Nothing will ever answer a socket for an episode that never
						// began. The bail-out below is reached only in CLI mode, so a
						// rofi user whose playback failed to start polled forever.
						if !anime.Ep.Started && internal.MPVConnectionGone(err) &&
							!internal.IsMPVRunning(anime.Ep.Player.SocketPath) {
							internal.Log("MPV never started, stopping playback time updates")
							return
						}

						// For CLI mode with next episode prompt, let the continuous prompt handle everything
						if userConfig.NextEpisodePrompt && !userConfig.RofiSelection {
							// ...but only while MPV is still there. Once it has exited,
							// continuing just re-polls a dead socket every second.
							if internal.MPVConnectionGone(err) && !internal.IsMPVRunning(anime.Ep.Player.SocketPath) {
								internal.Log("MPV is gone, stopping playback time updates")
								return
							}
							continue
						}

						if anime.Ep.Started {
							percentageWatched := internal.PercentageWatched(anime.Ep.Player.PlaybackTime, anime.Ep.Duration)
							action := internal.ClassifyPlaybackLoss(
								anime.Ep.Player.SocketPath,
								anime.Ep.Started,
								percentageWatched,
								userConfig.PercentageToMarkComplete,
							)
							internal.Log(fmt.Sprintf("playback loss: pct=%.1f action=%d mpvRunning=%v switching=%v",
								percentageWatched, action,
								internal.IsMPVRunning(anime.Ep.Player.SocketPath),
								internal.MPVPlaylistIsSwitching()))

							switch action {
							case internal.PlaybackLossWait:
								// Playlist jump / demuxer reload / pause — MPV still open.
								continue
							case internal.PlaybackLossExit:
								internal.Log("Episode is not completed, exiting")
								internal.Exit(nil)
								return
							case internal.PlaybackLossComplete:
								// fall through to completion handling below
							}

							// Episode completed (threshold met, with or without MPV still idle).
							internal.Log(fmt.Sprint(percentageWatched))
							internal.Log(fmt.Sprint(anime.Ep.Player.Speed))
							internal.Log(fmt.Sprint(anime.Ep.Player.PlaybackTime))
							internal.Log(fmt.Sprint(anime.Ep.Duration))
							internal.Log(fmt.Sprint(userConfig.PercentageToMarkComplete))
							anime.Ep.IsCompleted = true
							if !userConfig.NextEpisodePrompt {
								internal.StartNextEpisode(&anime, &userConfig, databaseFile, user.Token)
							} else {
								// For Rofi mode, show prompt immediately after completion
								if userConfig.RofiSelection {
									shouldContinue := internal.NextEpisodePromptRofi(&userConfig)
									if shouldContinue {
										internal.StartNextEpisode(&anime, &userConfig, databaseFile, user.Token)
									} else {
										// Episode was already marked as completed above
										// Handle completion if this was the last episode
										if anime.Ep.Number == anime.TotalEpisodes {
											internal.HandleLastEpisodeCompletion(&userConfig, &anime, user.Token)
										}
										// Update local database with completed episode
										err := internal.LocalUpdateAnime(databaseFile, anime.AnilistId, anime.ProviderId, anime.Ep.Number, anime.Ep.Player.PlaybackTime, internal.ConvertSecondsToMinutes(anime.Ep.Duration), internal.GetAnimeName(anime), internal.CurrentAnimeProviderName(&anime))
										if err != nil {
											internal.Log("Error updating local database on quit: " + err.Error())
										}

										// Update Anilist progress if not rewatching
										if !anime.Rewatching {
											if progressErr := internal.UpdateAnimeProgress(user.Token, anime.AnilistId, anime.Ep.Number); progressErr != nil {
												internal.Log("Error updating Anilist progress on quit: " + progressErr.Error())
											} else {
												internal.Out(fmt.Sprintf("Episode completed! Progress updated: %d", anime.Ep.Number))
											}
										}

										internal.Exit(nil)
									}
								} else {
									// For CLI mode, let the continuous prompt handle it
									internal.Log("Episode completed, exiting monitoring to let CLI prompt handle next episode")
								}
								// Exit the skip loop - only close if not already closed
								select {
								case isClosed := <-skipLoopClosed:
									if !isClosed {
										close(skipLoopDone)
										skipLoopClosed <- true // Mark as closed
									}
								default:
									// Channel is busy, another goroutine is handling closure
								}
								return
							}
							// Exit the skip loop - only close if not already closed
							select {
							case isClosed := <-skipLoopClosed:
								if !isClosed {
									close(skipLoopDone)
									skipLoopClosed <- true // Mark as closed
								}
							default:
								// Channel is busy, another goroutine is handling closure
							}
							return
						}
					}

					// Convert timePos to integer
					if timePos != nil {
						if !anime.Ep.Started {
							anime.Ep.Started = true
							// Set the playback speed
							if userConfig.SaveMpvSpeed {
								speedCmd := []interface{}{"set_property", "speed", anime.Ep.Player.Speed}
								if _, speedErr := internal.MPVSendCommand(anime.Ep.Player.SocketPath, speedCmd); speedErr != nil {
									internal.Log("Error setting playback speed: " + speedErr.Error())
								}
							}

							// Apply OP/ED Chapters
							if skipErr := internal.SendSkipTimesToMPV(&anime); skipErr != nil {
								internal.Log("Error sending skip times to MPV: " + skipErr.Error())
							}
						}

						// If resume is true, seek to the playback time
						if anime.Ep.Resume {
							internal.SeekMPV(anime.Ep.Player.SocketPath, anime.Ep.Player.PlaybackTime)
							anime.Ep.Resume = false
						}

						animePosition, ok := timePos.(float64)
						if !ok {
							internal.Log("Error: timePos is not a float64")
							continue
						}

						anime.Ep.Player.PlaybackTime = int(animePosition + 0.5) // Round to nearest integer
						// Update Local Database
						if updateErr := internal.LocalUpdateAnime(databaseFile, anime.AnilistId, anime.ProviderId, anime.Ep.Number, anime.Ep.Player.PlaybackTime, internal.ConvertSecondsToMinutes(anime.Ep.Duration), internal.GetAnimeName(anime), internal.CurrentAnimeProviderName(&anime)); updateErr != nil {
							internal.Log("Error updating local database: " + updateErr.Error())
						}
					}

					// Check if anything is playing; if not and episode was started, classify the loss.
					hasPlayback, err := internal.HasActivePlayback(anime.Ep.Player.SocketPath)
					// A gone connection is not an inconclusive error: MPV has exited,
					// which is definitively "nothing is playing". Treating it as an
					// error left the loop re-polling a dead socket and logging the
					// same failure every iteration (see Wraient/curd#58).
					if err != nil && internal.MPVConnectionGone(err) {
						hasPlayback, err = false, nil
					}
					if err != nil {
						internal.Log("Error checking playback status: " + err.Error())
					} else if !hasPlayback && anime.Ep.Started {
						// Wait for a moment to allow playback to start / playlist switch to settle
						time.Sleep(2 * time.Second)

						hasPlayback, err = internal.HasActivePlayback(anime.Ep.Player.SocketPath)
						if err != nil && internal.MPVConnectionGone(err) {
							hasPlayback, err = false, nil
						}
						if err != nil {
							internal.Log("Error checking playback status: " + err.Error())
						} else if !hasPlayback {
							// For CLI mode with next episode prompt, let the continuous prompt handle everything
							if userConfig.NextEpisodePrompt && !userConfig.RofiSelection {
								continue
							}

							percentageWatched := internal.PercentageWatched(anime.Ep.Player.PlaybackTime, anime.Ep.Duration)
							action := internal.ClassifyPlaybackLoss(
								anime.Ep.Player.SocketPath,
								anime.Ep.Started,
								percentageWatched,
								userConfig.PercentageToMarkComplete,
							)
							internal.Log(fmt.Sprintf("no active playback: pct=%.1f action=%d", percentageWatched, action))

							switch action {
							case internal.PlaybackLossWait:
								continue
							case internal.PlaybackLossExit:
								internal.Log("Episode is not completed, exiting")
								internal.Exit(nil)
								return
							case internal.PlaybackLossComplete:
								// fall through
							}

							anime.Ep.IsCompleted = true
							if !userConfig.NextEpisodePrompt {
								internal.StartNextEpisode(&anime, &userConfig, databaseFile, user.Token)
							} else {
								// For Rofi mode, show prompt immediately after completion
								if userConfig.RofiSelection {
									shouldContinue := internal.NextEpisodePromptRofi(&userConfig)
									if shouldContinue {
										internal.StartNextEpisode(&anime, &userConfig, databaseFile, user.Token)
									} else {
										// Episode was already marked as completed above
										// Update local database with completed episode
										err := internal.LocalUpdateAnime(databaseFile, anime.AnilistId, anime.ProviderId, anime.Ep.Number, anime.Ep.Player.PlaybackTime, internal.ConvertSecondsToMinutes(anime.Ep.Duration), internal.GetAnimeName(anime), internal.CurrentAnimeProviderName(&anime))
										if err != nil {
											internal.Log("Error updating local database on quit: " + err.Error())
										}

										// Update Anilist progress if not rewatching
										if !anime.Rewatching {
											if progressErr := internal.UpdateAnimeProgress(user.Token, anime.AnilistId, anime.Ep.Number); progressErr != nil {
												internal.Log("Error updating Anilist progress on quit: " + progressErr.Error())
											} else {
												internal.Out(fmt.Sprintf("Episode completed! Progress updated: %d", anime.Ep.Number))
											}
										}

										internal.Exit(nil)
									}
								} else {
									// For CLI mode, update progress immediately since episode is 85%+ complete
									// Update local database with completed episode
									err := internal.LocalUpdateAnime(databaseFile, anime.AnilistId, anime.ProviderId, anime.Ep.Number, anime.Ep.Player.PlaybackTime, internal.ConvertSecondsToMinutes(anime.Ep.Duration), internal.GetAnimeName(anime), internal.CurrentAnimeProviderName(&anime))
									if err != nil {
										internal.Log("Error updating local database on completion: " + err.Error())
									}

									// Update Anilist progress if not rewatching
									if !anime.Rewatching {
										if progressErr := internal.UpdateAnimeProgress(user.Token, anime.AnilistId, anime.Ep.Number); progressErr != nil {
											internal.Log("Error updating Anilist progress on completion: " + progressErr.Error())
										} else {
											internal.Out(fmt.Sprintf("Episode completed! Progress updated: %d", anime.Ep.Number))
										}
									}

									internal.Log("Episode completed, updated progress, exiting monitoring to let CLI prompt handle next episode")
								}
								// Exit the skip loop - only close if not already closed
								select {
								case isClosed := <-skipLoopClosed:
									if !isClosed {
										close(skipLoopDone)
										skipLoopClosed <- true // Mark as closed
									}
								default:
									// Channel is busy, another goroutine is handling closure
								}
								return
							}
							// Exit the skip loop - only close if not already closed
							select {
							case isClosed := <-skipLoopClosed:
								if !isClosed {
									close(skipLoopDone)
									skipLoopClosed <- true // Mark as closed
								}
							default:
								// Channel is busy, another goroutine is handling closure
							}
							return
						}
					}

				}
			}
		}()

		// Skip OP and ED and Save MPV Speed
	skipLoop:
		for {
			select {
			case <-skipLoopDone:
				// Exit signal received, break out of the skipLoop
				break skipLoop
			default:
				if userConfig.SkipOp {
					if anime.Ep.Player.PlaybackTime > anime.Ep.SkipTimes.Op.Start && anime.Ep.Player.PlaybackTime < anime.Ep.SkipTimes.Op.Start+2 && anime.Ep.SkipTimes.Op.Start != anime.Ep.SkipTimes.Op.End {
						internal.SeekMPV(anime.Ep.Player.SocketPath, anime.Ep.SkipTimes.Op.End)
					}
				}
				if userConfig.SkipEd {
					if anime.Ep.Player.PlaybackTime > anime.Ep.SkipTimes.Ed.Start && anime.Ep.Player.PlaybackTime < anime.Ep.SkipTimes.Ed.Start+2 && anime.Ep.SkipTimes.Ed.Start != anime.Ep.SkipTimes.Ed.End {
						internal.SeekMPV(anime.Ep.Player.SocketPath, anime.Ep.SkipTimes.Ed.End)
					}
				}
				if _, positionErr := internal.MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"get_property", "time-pos"}); positionErr == nil && anime.Ep.Started {
					speed, speedErr := internal.GetMPVPlaybackSpeed(anime.Ep.Player.SocketPath)
					if speedErr != nil {
						internal.Log("Failed to get mpv speed " + speedErr.Error())
					} else {
						anime.Ep.Player.Speed = speed
					}
				}
			}

			time.Sleep(1 * time.Second) // Wait before checking again
		}

		// Wait for all goroutines to finish before starting the next iteration
		wg.Wait()

		// Reset the WaitGroup for the next loop
		wg = sync.WaitGroup{}

		// Exit the program if we're starting an episode beyond the total episodes
		if anime.Ep.Number > anime.TotalEpisodes && anime.TotalEpisodes > 0 {
			internal.Out("Reached end of series")
			internal.Exit(nil)
		}

		if anime.Ep.IsCompleted && !anime.Rewatching {
			// Update progress for both regular episodes and skipped fillers
			if anime.TotalEpisodes > 0 && anime.Ep.Number-1 != anime.TotalEpisodes {
				progressEpisode := anime.Ep.Number - 1
				go func(episode int) {
					// Update progress for regular episodes
					if progressErr := internal.UpdateAnimeProgress(user.Token, anime.AnilistId, episode); progressErr != nil {
						internal.Log("Error updating Anilist progress: " + progressErr.Error())
					}
				}(progressEpisode)
			} else {
				// Update progress for last episode

				// Exit MPV
				internal.ExitMPV(anime.Ep.Player.SocketPath)

				if progressErr := internal.UpdateAnimeProgress(user.Token, anime.AnilistId, anime.Ep.Number-1); progressErr != nil {
					internal.Log("Error updating Anilist progress: " + progressErr.Error())
				}
			}

			anime.Ep.IsCompleted = false
			// Only mark as complete and prompt for rating if we've reached the total episodes
			// AND the anime is not currently airing (total episodes > 0)
			if anime.Ep.Number-1 == anime.TotalEpisodes && userConfig.ScoreOnCompletion && anime.TotalEpisodes > 0 {

				// Get updated anime data to check if it's still airing
				updatedAnime, err := internal.GetAnimeDataByID(anime.AnilistId, user.Token)
				if err != nil {
					internal.Log("Error getting updated anime data: " + err.Error())
				} else if !updatedAnime.IsAiring {
					anime.Ep.Number = anime.Ep.Number - 1
					internal.Out("Completed anime.")
					if rateErr := internal.RateAnime(user.Token, anime.AnilistId); rateErr != nil {
						internal.Log("Error rating anime: " + rateErr.Error())
						internal.Out("Error rating anime: " + rateErr.Error())
					}
					internal.LocalDeleteAnime(databaseFile, anime.AnilistId, anime.ProviderId)
					internal.Exit(nil)
				}
			}
		}
		if anime.Rewatching && anime.Ep.IsCompleted && anime.Ep.Number-1 == anime.TotalEpisodes {
			anime.Ep.Number = anime.Ep.Number - 1
			internal.Out("Completed anime. (Rewatching so no scoring)")
			internal.LocalDeleteAnime(databaseFile, anime.AnilistId, anime.ProviderId)
			internal.Exit(nil)
		}

		// Handle next episode logic based on config
		if anime.Ep.IsCompleted {
			if userConfig.NextEpisodePrompt {
				if !userConfig.RofiSelection {
					// For CLI mode, the continuous prompt handles everything
					internal.Out("CLI mode: continuous prompt handling next episode logic")
				}
				// For both modes, if we reach here, it means the monitoring thread exited
				// and the episode should transition. Let the normal flow continue.
			} else {
				// When NextEpisodePrompt is off, continue automatically
				internal.StartNextEpisode(&anime, &userConfig, databaseFile, user.Token)
				continue
			}
		}

		// Wait for up to 5 seconds for prefetched links to become available
		for i := 0; i < 5; i++ {
			if anime.Ep.NextEpisode.Number == anime.Ep.Number && len(anime.Ep.NextEpisode.Links) > 0 {
				internal.Log("Using prefetched next episode link")
				anime.Ep.Links = anime.Ep.NextEpisode.Links
				break
			}
			time.Sleep(1 * time.Second)
		}

		// If we still don't have links, get them now
		if len(anime.Ep.Links) == 0 {
			links, _, err := internal.GetEpisodeURLForPlayback(userConfig, anime.ProviderId, anime.Ep.Number)
			if err != nil {
				internal.Log("Failed to get episode links: " + err.Error())
				internal.Out("Failed to get episode links. Try again later.")
				internal.Exit(fmt.Errorf("failed to get episode links: %v", err))
				return
			}
			anime.Ep.Links = links
		}

		// Verify that we have links before starting
		if len(anime.Ep.Links) == 0 {
			internal.Out("No episode links found. Try again later.")
			internal.Exit(fmt.Errorf("no episode links found"))
			return
		}

	}
}

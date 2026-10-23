package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/thexykril/otakase/internal"
	"github.com/thexykril/otakase/internal/appicon"
)

// version is stamped in at build time with -ldflags -X main.version=...
var version string

// buildVersion is the stamped version, or "dev" for a plain `go build`.
// Don't put a real version number here instead: versionOrder ranks "dev"
// above every release, whereas a hard-coded number would look out of date
// forever and keep offering to reinstall the same build.
func buildVersion() string {
	if version == "" {
		return "dev"
	}
	return version
}

func main() {
	anime, user := new(internal.Anime), new(internal.User)
	internal.SetGlobalAnime(anime)
	internal.SetGlobalUser(user)

	configPath := filepath.Join(userConfigDir(), internal.AppName, internal.ConfigFileName())
	loaded, err := internal.LoadConfig(configPath)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}
	// Shared with the rest of the program through SetGlobalConfig, so every
	// change below is seen everywhere.
	cfg := &loaded
	if !prepareStorage(configPath, cfg) {
		return
	}

	flags := parseFlags(cfg)
	openLink, partyArg := launchLinks(flags.openLink, flags.party, flag.Args())
	cfg.PercentageToMarkComplete = clampPercent(cfg.PercentageToMarkComplete)

	// Checked before the screen is touched, so the output stays plain.
	if flags.version {
		fmt.Printf("%s version: %s\n", internal.DisplayName, buildVersion())
		return
	}
	runCommandAndExit(flags, cfg)

	anime.Ep.ContinueLast = flags.continueLast
	cfg.RofiSelection = useRofi(cfg.RofiSelection, flags, runtime.GOOS)

	// A link opened from the desktop has no terminal: its menus go to rofi,
	// or (LinkMenus=terminal, or no rofi) otakase reopens in a terminal.
	if openLink != "" || partyArg != "" {
		if flags.noRofi {
			cfg.LinkMenus = internal.LinkMenusTerminal
		}
		if internal.PrepareLinkLaunch(cfg) {
			os.Exit(0)
		}
	}
	internal.SetGlobalConfig(cfg)

	if flags.update {
		os.Exit(selfUpdate(cfg))
	}

	cfg.ImagePreview = useImagePreview(cfg.ImagePreview, flags, runtime.GOOS)
	if flags.editConfig {
		internal.EditConfig(configPath)
		return
	}

	prepareDisplay(cfg)

	if err := internal.EnsureTrackingConfigured(cfg); err != nil {
		fmt.Printf("Error configuring tracking: %v\n", err)
		return
	}
	internal.SetGlobalConfig(cfg)

	if flags.changeToken {
		internal.ChangeTrackingToken(cfg, user)
		return
	}
	if flags.setupAnimeSkip {
		if err := internal.SetupAnimeSkipClientID(cfg); err != nil {
			fmt.Printf("Anime-Skip setup failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Everything from here on is interactive.
	internal.EnterAppScreen()
	internal.HandleTerminalInterrupts()
	defer internal.LeaveAppScreen()
	// Notices a run that crashed, to offer a report at the next launch.
	internal.StartCrashWatch(cfg)
	defer internal.CrashGuard()

	applyAudioFlags(cfg, flags)
	if flags.cast {
		cfg.CastToDevice = true
		internal.ApplyCastSubStyle(cfg, flags.softSub)
	}

	if flags.castSession != "" {
		runCastSession(cfg, flags.castSession)
		return
	}

	// Opened from a link: go straight to that show. A link that cannot be
	// used is said so, and the launch carries on as usual.
	if openLink != "" {
		if link, linkErr := internal.ParseOpenLink(openLink); linkErr != nil {
			internal.Log(fmt.Sprintf("Link %q: %v", openLink, linkErr))
			internal.OpenLinkNotice(cfg, internal.OpenLinkError(linkErr))
		} else {
			internal.Log(fmt.Sprintf("Opening from link: AniList %d, episode %d", link.AnilistID, link.Episode))
			anime.Ep.ContinueLast = false
			internal.SetOpenLink(link)
		}
	}

	s := &session{cfg: cfg, anime: anime, user: user, download: flags.download, episodes: flags.downloadEpisodes}
	if s.start(partyArg, flags.addNew) {
		s.run()
	}
}

// userConfigDir is where the config folder lives. os.UserConfigDir only fails
// when the environment is missing the variable it reads, so the usual place
// is spelled out by hand.
func userConfigDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return dir
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
	}
	return filepath.Join(os.Getenv("HOME"), ".config")
}

// prepareStorage brings the storage folder and config up to this version and
// starts a fresh log. It reports false when the run cannot go on.
func prepareStorage(configPath string, cfg *internal.Config) bool {
	internal.SetVersion(buildVersion())
	updated, migrateErr := internal.MigrateOnVersionUpgrade(configPath, cfg, buildVersion())
	if migrateErr != nil {
		fmt.Println("Error applying storage migration:", migrateErr)
		return false
	}
	if updated {
		fmt.Println("Updated config for this " + internal.DisplayName + " version (new options and/or migrations).")
	}
	internal.SetGlobalConfig(cfg)

	storageDir := os.ExpandEnv(cfg.StoragePath)
	logFile := filepath.Join(storageDir, internal.LogFileName)
	internal.SetGlobalLogFile(logFile)
	// -diag shows the previous run's log, so it runs before the log is cleared.
	if internal.WantsDiagnostics(os.Args[1:]) {
		fmt.Print(internal.Diagnostics(buildVersion(), configPath, logFile))
		os.Exit(0)
	}
	internal.UseDiagnosticsFor(buildVersion(), configPath)
	internal.PreservePreviousLog(logFile)
	internal.ClearLogFile(logFile)
	// The log used to be a bare debug.log. Leaving one behind would be a file
	// that looks current and never changes again, which is worth more confusion
	// than the byte it saves -- it is truncated at every launch anyway, so
	// nothing in it is wanted.
	internal.RemoveLegacyLogFile(storageDir)
	return true
}

// selfUpdate replaces this binary with the latest release (-u) and returns
// the exit code.
func selfUpdate(cfg *internal.Config) int {
	if err := internal.SelfUpdate(internal.DefaultUpdateRepo, internal.UpdateReleaseTag(cfg)); err != nil {
		fmt.Fprintln(os.Stderr, "Error updating executable:", err)
		return 1
	}
	fmt.Println("Program Updated!")
	return 0
}

// runCommandAndExit handles the flags that do one job and exit. They run
// before any UI setup so the output stays plain and pipeable.
func runCommandAndExit(f *cliFlags, cfg *internal.Config) {
	// Editing the user's Hyprland config is a command they run deliberately,
	// never something an install does behind their back.
	if f.installKeybind || f.removeKeybind || f.refreshKeybind {
		os.Exit(runKeybindCommand(f))
	}

	if f.castSetup {
		if err := internal.CastSetup(cfg); err != nil {
			fmt.Fprintln(os.Stderr, "Cast setup failed:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	if f.providerStatus {
		report := internal.CheckProviders(cfg, f.providerStatusQuery, f.providerStatusAdult)
		fmt.Print(internal.FormatProviderStatus(report, f.providerStatusQuery))
		// Success as long as anything can be watched at all.
		for _, health := range report {
			if health.OK() {
				os.Exit(0)
			}
		}
		os.Exit(1)
	}

	if f.streamInfo != "" {
		anilistID, episode, err := internal.ParseStreamInfoArg(f.streamInfo)
		if err == nil {
			cfg.AdultContent = f.streamAdult
			var info internal.StreamInfo
			info, err = internal.ResolveStreamInfo(*cfg, anilistID, episode)
			if err == nil {
				fmt.Println(internal.FormatStreamInfo(info))
				os.Exit(0)
			}
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if f.discordLink || f.discordUnlink || f.report {
		var err error
		switch {
		case f.discordLink:
			err = internal.RunDiscordLinkCLI(cfg)
		case f.discordUnlink:
			err = internal.RunDiscordUnlinkCLI(cfg)
		default:
			internal.RunReportCLI(cfg)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
}

// runKeybindCommand installs, removes or refreshes the Hyprland binding and
// returns the exit code.
func runKeybindCommand(f *cliFlags) int {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not find your home directory: %v\n", err)
		return 1
	}
	var notes []string
	switch {
	case f.removeKeybind:
		notes, err = internal.RemoveHyprlandKeybind(home)
	case f.refreshKeybind:
		notes, err = internal.RefreshHyprlandKeybind(home)
	default:
		notes, err = internal.InstallHyprlandKeybind(home, f.forceKeybind)
	}
	for _, note := range notes {
		fmt.Println(note)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	return 0
}

// prepareDisplay sets up what the menus are drawn with.
func prepareDisplay(cfg *internal.Config) {
	// Resolve the colour palette before anything draws. On Omarchy this follows
	// the desktop theme; elsewhere it is otakase's own palette.
	internal.ApplyThemeFromConfig(cfg)

	// Line icons in the menus, where the font for them can be relied on.
	internal.SetupIcons(cfg)

	// The app icon and, on Linux, the app-menu entry. Off the main path: it
	// is a few small files at most, and nothing waits on them.
	go appicon.Path()

	// On Windows, make otakase:// links open this otakase when nothing has
	// claimed them yet (the installer normally has).
	go internal.RegisterURLScheme()

	if !cfg.RofiSelection {
		return
	}
	// Themes are rendered from the palette on every run, so a desktop theme
	// change is picked up without the user clearing anything. This also means
	// the menus no longer need the network before they can be shown.
	if err := internal.WriteRofiThemes(os.ExpandEnv(cfg.StoragePath)); err != nil {
		internal.Log(fmt.Sprintf("Error writing rofi themes: %v", err))
		internal.Out(fmt.Sprintf("Error writing rofi themes: %v", err))
		internal.Exit(err)
	}
}

// runCastSession is the spawned terminal's entry point: main has loaded the
// config, and everything after this is the picking flow this process does not
// need -- the episode was already resolved by the process that spawned it.
func runCastSession(cfg *internal.Config, handoffFile string) {
	internal.LeaveAppScreen()
	// Only rofi hands a cast to a window, and with the window gone nothing
	// of otakase is left running.
	internal.ArmCloseNotice()
	if err := internal.RunCastSession(cfg, handoffFile); err != nil {
		internal.Out("Casting failed: " + err.Error())
		internal.Log(fmt.Sprintf("cast: %v", err))
		internal.WaitBeforeClosingCastWindow(cfg)
	}
	// Exit rather than return: the cast controls restore the terminal from a
	// cleanup that only runExitCleanups reaches, and a plain return does not
	// reach it. Returning here left the tty raw after q or a finished season,
	// which is invisible while the spawned window closes itself and very
	// visible when the session was started from an ordinary terminal.
	internal.Exit(nil)
}

// rewatchingFinishedShow reports whether the chosen show is one the list
// already has as fully watched, which makes this run a rewatch.
func rewatchingFinishedShow(entry *internal.Entry, totalEpisodes int) bool {
	return entry != nil && entry.Progress == totalEpisodes && entry.Status != "CURRENT"
}

// noteRewatch marks the show as a rewatch when the list says it was finished.
func noteRewatch(anime *internal.Anime, user *internal.User) {
	entry, err := internal.FindAnimeByAnilistID(user.AnimeList, strconv.Itoa(anime.AnilistId))
	if err != nil {
		internal.Log("Error finding anime by Anilist ID: " + err.Error())
		return
	}
	if !rewatchingFinishedShow(entry, anime.TotalEpisodes) {
		return
	}
	internal.Log(entry.Progress)
	internal.Log(anime.TotalEpisodes)
	internal.Log(user.AnimeList)
	internal.Log("Rewatching anime: " + internal.GetAnimeName(*anime))
	anime.Rewatching = true
}

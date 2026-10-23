package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/thexykril/otakase/internal"
)

// cliFlags holds the switches that pick what this run does. Flags that only
// override a config value are bound straight to the config instead, so the
// rest of the program sees one set of settings whichever way they were given.
type cliFlags struct {
	continueLast   bool
	addNew         bool
	rofi           bool
	noRofi         bool
	imagePreview   bool
	noImagePreview bool
	cast           bool
	castSetup      bool
	castSession    string
	changeToken    bool
	setupAnimeSkip bool
	update         bool
	editConfig     bool
	sub            bool
	dub            bool
	softSub        bool
	hardSub        bool
	version        bool

	download         bool
	downloadEpisodes string

	providerStatus      bool
	providerStatusQuery string
	providerStatusAdult string

	streamInfo  string
	streamAdult bool

	installKeybind bool
	removeKeybind  bool
	refreshKeybind bool
	forceKeybind   bool

	openLink      string
	party         string
	discordLink   bool
	discordUnlink bool
	report        bool
}

// parseFlags registers every flag against the loaded config and parses the
// command line. The config has to be loaded first: its values are the
// defaults the flags show and fall back to.
func parseFlags(cfg *internal.Config) *cliFlags {
	f := &cliFlags{}

	flag.StringVar(&cfg.Player, "player", cfg.Player, "Player binary for playback (mpv-compatible; falls back to mpv if unavailable)")
	flag.StringVar(&cfg.StoragePath, "storage-path", cfg.StoragePath, "Path to the storage directory")
	flag.StringVar(&cfg.SubsLanguage, "subs-lang", cfg.SubsLanguage, "Subtitles language")
	flag.IntVar(&cfg.PercentageToMarkComplete, "percentage-to-mark-complete", cfg.PercentageToMarkComplete, "Percentage to mark episode as complete")
	flag.BoolVar(&cfg.NextEpisodePrompt, "next-episode-prompt", cfg.NextEpisodePrompt, "Prompt for the next episode (true/false)")
	flag.BoolVar(&cfg.SkipOp, "skip-op", cfg.SkipOp, "Skip opening (true/false)")
	flag.BoolVar(&cfg.SkipEd, "skip-ed", cfg.SkipEd, "Skip ending (true/false)")
	flag.BoolVar(&cfg.SkipFiller, "skip-filler", cfg.SkipFiller, "Skip filler episodes (true/false)")
	flag.BoolVar(&cfg.SkipRecap, "skip-recap", cfg.SkipRecap, "Skip recap (true/false)")
	flag.BoolVar(&cfg.ScoreOnCompletion, "score-on-completion", cfg.ScoreOnCompletion, "Score on episode completion (true/false)")
	flag.BoolVar(&cfg.SaveMpvSpeed, "save-mpv-speed", cfg.SaveMpvSpeed, "Save MPV speed setting (true/false)")
	flag.BoolVar(&cfg.DiscordPresence, "discord-presence", cfg.DiscordPresence, "Enable Discord presence (true/false)")
	flag.StringVar(&cfg.DiscordClientId, "discord-client-id", cfg.DiscordClientId, "Discord client ID for Rich Presence")
	flag.BoolVar(&cfg.VimKeys, "vim-keys", cfg.VimKeys, "Enable vim motions in selection menus (j/k/h/l, / search) (true/false)")
	flag.BoolVar(&cfg.CheckUpdates, "check-updates", cfg.CheckUpdates, "Check for updates in the background at launch (true/false)")
	flag.BoolVar(&cfg.ShowNewEpisodes, "show-new-episodes", cfg.ShowNewEpisodes, "Show new episode indicators in currently watching list (true/false)")
	flag.StringVar(&cfg.DownloadDir, "download-dir", cfg.DownloadDir, "Directory to save downloaded episodes into")
	flag.StringVar(&cfg.DownloadFormat, "download-format", cfg.DownloadFormat, "Container for downloaded episodes: mkv or mp4")

	flag.BoolVar(&f.continueLast, "c", false, "Continue last episode")
	flag.BoolVar(&f.addNew, "new", false, "Add new anime")
	flag.BoolVar(&f.rofi, "rofi", false, "Open selection in rofi")
	flag.BoolVar(&f.noRofi, "no-rofi", false, "No rofi")
	flag.BoolVar(&f.imagePreview, "image-preview", false, "Show image preview")
	flag.BoolVar(&f.noImagePreview, "no-image-preview", false, "No image preview")
	flag.BoolVar(&f.cast, "cast", false, "Play on a Chromecast, DLNA TV or Kodi on this network instead of locally")
	flag.BoolVar(&f.castSetup, "cast-setup", false, "Let casting through this machine's firewall: fix the cast ports, allow them (asks first), then search for devices")
	flag.StringVar(&f.castSession, "cast-session", "", "Cast the episode described by a handoff file (used internally when casting from rofi)")
	flag.BoolVar(&f.changeToken, "change-token", false, "Change token")
	flag.BoolVar(&f.setupAnimeSkip, "setup-anime-skip", false, "Create a personal Anime-Skip client id and save it")
	flag.BoolVar(&f.update, "u", false, "Update the script")
	flag.BoolVar(&f.editConfig, "e", false, "Edit config")
	flag.BoolVar(&f.sub, "sub", false, "Watch sub version")
	flag.BoolVar(&f.dub, "dub", false, "Watch dub version")
	flag.BoolVar(&f.softSub, "softsub", false, "Prefer soft subtitles when available (anineko)")
	flag.BoolVar(&f.hardSub, "hardsub", false, "Prefer hard subtitles when available (anineko)")
	flag.BoolVar(&f.version, "v", false, "Print version information")
	// Read from os.Args before parsing (see main); registered so it is
	// accepted and listed in the help.
	flag.Bool("diag", false, "Print version, system, mpv and the end of the log to paste in a support post")
	flag.BoolVar(&f.download, "download", false, "Download episodes instead of playing them (requires ffmpeg)")
	flag.StringVar(&f.downloadEpisodes, "episodes", "", "Episodes to download, e.g. 5 or 1-12 (default: the selected episode)")
	flag.BoolVar(&f.providerStatus, "provider-status", false, "Probe every provider and report which ones work")
	flag.StringVar(&f.providerStatusQuery, "provider-status-query", internal.DefaultStatusQuery, "Search query used by -provider-status")
	flag.StringVar(&f.providerStatusAdult, "provider-status-adult-query", internal.DefaultAdultStatusQuery, "Search query -provider-status uses for 18+ providers")
	flag.StringVar(&f.streamInfo, "stream-info", "", "Print an episode's stream links as JSON without asking anything, e.g. 154587:1 (AniList id:episode)")
	flag.BoolVar(&f.streamAdult, "stream-adult", false, "Let -stream-info use the 18+ sources for 18+ titles")
	flag.BoolVar(&f.installKeybind, "install-keybind", false, "Add a Super+Shift+A Hyprland binding that opens the rofi menu")
	flag.BoolVar(&f.removeKeybind, "remove-keybind", false, "Remove the Hyprland binding added by -install-keybind")
	flag.BoolVar(&f.refreshKeybind, "refresh-keybind", false, "Update an existing Hyprland binding, adding nothing if absent")
	flag.BoolVar(&f.forceKeybind, "force-keybind", false, "Let -install-keybind replace a binding something else owns")
	flag.StringVar(&f.openLink, "open", "", "Open an otakase:// link, e.g. otakase://play/anilist/154587?ep=1 (what the site's Play in Otakase buttons send)")
	flag.StringVar(&f.party, "party", "", "Join a synced watch party by its event id or link (link the app with -discord-link first)")
	flag.BoolVar(&f.discordLink, "discord-link", false, "Link this app to your account on the Otakase Discord")
	flag.BoolVar(&f.discordUnlink, "discord-unlink", false, "Unlink this app from the Otakase Discord")
	flag.BoolVar(&f.report, "report", false, "Report a problem to the Otakase support forum, with the app details and log")

	flag.Usage = printUsage
	flag.Parse()
	return f
}

func printUsage() {
	internal.LeaveAppScreen()
	fmt.Fprintf(os.Stderr, "%s is a CLI tool to manage anime playback with advanced features like skipping intro, outro, filler, recap, tracking progress, and integrating with Discord.\n", internal.DisplayName)
	fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
	flag.PrintDefaults()
}

// launchLinks works out which otakase:// link this run was started with, if
// any, and whether it joins a watch party rather than opening a show.
//
// A desktop opening an otakase:// link starts `otakase <link>` (the .desktop
// entry's %u, the Windows registry's "%1"), so a bare first argument counts
// as much as -open does.
func launchLinks(openFlag, partyFlag string, args []string) (openLink, party string) {
	openLink = strings.TrimSpace(openFlag)
	if openLink == "" && len(args) > 0 && internal.IsOpenLinkArg(args[0]) {
		openLink = args[0]
	}
	party = strings.TrimSpace(partyFlag)
	if internal.IsPartyLink(openLink) {
		party, openLink = openLink, ""
	}
	return openLink, party
}

// clampPercent keeps -percentage-to-mark-complete within 0-100.
func clampPercent(p int) int {
	return min(max(p, 0), 100)
}

// useRofi decides whether the menus go to rofi for this run.
//
// The command-line flags are applied before -u runs so its password prompt
// (terminal or GTK) matches. -u and -e always stay in the terminal they were
// typed in, even with RofiSelection on in the config: -e runs an editor there,
// and a failure from either would otherwise arrive as a desktop notification
// while the terminal the user is looking at stays silent.
func useRofi(configured bool, f *cliFlags, goos string) bool {
	if f.update || f.editConfig || f.noRofi || goos == "windows" {
		return false
	}
	return configured || f.rofi
}

// useImagePreview is the same decision for the cover images.
func useImagePreview(configured bool, f *cliFlags, goos string) bool {
	if f.noImagePreview || goos == "windows" {
		return false
	}
	return configured || f.imagePreview
}

// applyAudioFlags lets -sub/-dub and -softsub/-hardsub override the config for
// this run. SubOrDubFlag records that the choice was explicit, so a show's
// remembered audio does not replace it.
func applyAudioFlags(cfg *internal.Config, f *cliFlags) {
	switch {
	case f.sub:
		cfg.SubOrDub, cfg.SubOrDubFlag = "sub", true
	case f.dub:
		cfg.SubOrDub, cfg.SubOrDubFlag = "dub", true
	}
	switch {
	case f.softSub:
		cfg.SubStyle = "soft"
	case f.hardSub:
		cfg.SubStyle = "hard"
	}
}

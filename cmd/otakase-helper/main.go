// Command otakase-helper is the headless program the Discord bot runs for its
// watch room and source check. It never asks anything and never draws a menu:
// each mode does one job, prints plain output and exits (or, for the serve
// modes, serves until stdin closes).
//
//	otakase-helper -v
//	otakase-helper -provider-status
//	otakase-helper -stream-info <anilist id>:<episode> [-stream-info-fast] [-stream-adult] [-dub|-sub]
//	otakase-helper -stream-languages <provider>:<id>:<episode>
//	otakase-helper -title-stream-info <imdb id>[:<season>:<episode>]
//	otakase-helper -torrent-serve <info hash> <mode>
//	otakase-helper -movie-serve <presenter>
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/thexykril/otakase/internal"
)

// version is stamped in at build time with -ldflags -X main.version=...
var version string

func buildVersion() string {
	if version == "" {
		return "dev"
	}
	return version
}

type helperFlags struct {
	version             bool
	sub, dub            bool
	providerStatus      bool
	providerStatusQuery string
	providerStatusAdult string
	streamInfo          string
	streamInfoFast      bool
	streamAdult         bool
	streamLangs         string
	titleInfo           string
}

func main() {
	// The serve modes take positional arguments and need no config.
	if len(os.Args) == 4 && os.Args[1] == "-torrent-serve" {
		if err := serveTorrentForBot(os.Args[2], os.Args[3]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "-movie-serve" {
		if err := serveMovieForBot(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	var f helperFlags
	flag.BoolVar(&f.version, "v", false, "Print version information")
	flag.BoolVar(&f.sub, "sub", false, "Prefer the subbed release")
	flag.BoolVar(&f.dub, "dub", false, "Prefer the dubbed release")
	flag.BoolVar(&f.providerStatus, "provider-status", false, "Check the sources and report how many work")
	flag.StringVar(&f.providerStatusQuery, "provider-status-query", internal.DefaultStatusQuery, "Search query used by -provider-status")
	flag.StringVar(&f.providerStatusAdult, "provider-status-adult-query", internal.DefaultAdultStatusQuery, "Search query -provider-status uses for 18+ providers")
	flag.StringVar(&f.streamInfo, "stream-info", "", "Print an episode's stream links as JSON, e.g. 154587:1 (AniList id:episode)")
	flag.BoolVar(&f.streamInfoFast, "stream-info-fast", false, "Skip optional audio-release discovery for -stream-info")
	flag.BoolVar(&f.streamAdult, "stream-adult", false, "Let -stream-info use the 18+ sources for 18+ titles")
	flag.StringVar(&f.streamLangs, "stream-languages", "", "Print the other audio-language releases of an episode, as JSON, e.g. kickassanime:<provider id>:1")
	flag.StringVar(&f.titleInfo, "title-stream-info", "", "Print a film's or series episode's stream link as JSON, e.g. tt0903747:1:2 (IMDb id[:season:episode])")
	flag.Parse()

	if f.version {
		fmt.Printf("%s version: %s\n", internal.DisplayName, buildVersion())
		return
	}

	switch {
	case f.providerStatus, f.streamInfo != "":
		cfg, ok := loadConfig()
		if !ok {
			os.Exit(1)
		}
		if f.providerStatus {
			os.Exit(providerStatus(cfg, f))
		}
		os.Exit(streamInfo(cfg, f))
	case f.titleInfo != "":
		info, err := titleStreamInfo(f.titleInfo)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(internal.FormatStreamInfo(info))
	case f.streamLangs != "":
		info, err := streamLanguages(f.streamLangs)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(internal.FormatStreamInfo(info))
	default:
		flag.Usage()
		os.Exit(2)
	}
}

// loadConfig reads (or creates) the config the lookups use. The bot runs the
// helper with a fresh HOME each time, so this is normally the defaults.
func loadConfig() (*internal.Config, bool) {
	anime, user := new(internal.Anime), new(internal.User)
	internal.SetGlobalAnime(anime)
	internal.SetGlobalUser(user)

	configPath := filepath.Join(userConfigDir(), internal.AppName, internal.ConfigFileName())
	loaded, err := internal.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		return nil, false
	}
	cfg := &loaded
	internal.SetVersion(buildVersion())
	if _, err := internal.MigrateOnVersionUpgrade(configPath, cfg, buildVersion()); err != nil {
		fmt.Fprintln(os.Stderr, "Error applying storage migration:", err)
		return nil, false
	}
	internal.SetGlobalConfig(cfg)
	logFile := filepath.Join(os.ExpandEnv(cfg.StoragePath), internal.LogFileName)
	internal.SetGlobalLogFile(logFile)
	internal.ClearLogFile(logFile)
	return cfg, true
}

func providerStatus(cfg *internal.Config, f helperFlags) int {
	report := internal.CheckProviders(cfg, f.providerStatusQuery, f.providerStatusAdult)
	if os.Getenv(internal.ProviderStatusDetailsEnv) == "1" {
		fmt.Print(internal.FormatProviderStatus(report, f.providerStatusQuery))
	} else {
		fmt.Print(internal.FormatProviderStatusSummary(report, cfg.AdultContent))
	}
	// Success as long as anything can be watched at all.
	for _, health := range report {
		if health.OK() {
			return 0
		}
	}
	return 1
}

func streamInfo(cfg *internal.Config, f helperFlags) int {
	anilistID, episode, err := internal.ParseStreamInfoArg(f.streamInfo)
	if err == nil {
		switch {
		case f.sub:
			cfg.SubOrDub, cfg.SubOrDubFlag = "sub", true
		case f.dub:
			cfg.SubOrDub, cfg.SubOrDubFlag = "dub", true
		}
		cfg.AdultContent = f.streamAdult
		var info internal.StreamInfo
		if f.streamInfoFast {
			info, err = internal.ResolveStreamInfoFast(*cfg, anilistID, episode)
		} else {
			info, err = internal.ResolveStreamInfo(*cfg, anilistID, episode)
		}
		if err == nil {
			fmt.Println(internal.FormatStreamInfo(info))
			return 0
		}
	}
	fmt.Fprintln(os.Stderr, err)
	return 1
}

// streamLanguages answers -stream-languages provider:id:episode. The id
// may itself contain colons, so the provider and episode are cut off the ends.
func streamLanguages(arg string) (internal.StreamInfo, error) {
	provider, rest, ok := strings.Cut(arg, ":")
	cut := strings.LastIndex(rest, ":")
	if !ok || cut <= 0 {
		return internal.StreamInfo{}, fmt.Errorf("-stream-languages wants provider:id:episode, got %q", arg)
	}
	episode, err := strconv.Atoi(rest[cut+1:])
	if err != nil {
		return internal.StreamInfo{}, fmt.Errorf("-stream-languages wants provider:id:episode, got %q", arg)
	}
	return internal.ResolveStreamLanguages(provider, rest[:cut], episode)
}

// userConfigDir is where the config folder lives.
func userConfigDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return dir
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
	}
	return filepath.Join(os.Getenv("HOME"), ".config")
}

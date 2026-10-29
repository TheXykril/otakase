package internal

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/mpv"
	"github.com/thexykril/otakase/internal/torrentstream"
)

const (
	// androidIntentSocket stands in for a socket when the episode was handed
	// to mpv-android, which otakase cannot talk to.
	androidIntentSocket = "android-intent"

	// mpvSocketWait is how long a freshly started player gets to open its
	// IPC socket, polled every mpvSocketPoll.
	mpvSocketWait = 3 * time.Second
	mpvSocketPoll = 300 * time.Millisecond

	// subtitleAttachWait bounds the wait for a reused player to open the next
	// file before its subtitles can be added.
	subtitleAttachWait = 12 * time.Second
)

// StartVideo plays link and returns the socket of the player it went to. A
// player that is still open from the last episode loads it in the same window;
// otherwise a new one starts.
func StartVideo(link string, args []string, title string, anime *Anime) (string, error) {
	config := GetGlobalConfig()
	if config == nil {
		defaults := PopulateConfig(map[string]string{})
		config = &defaults
	}
	if anime == nil {
		return "", fmt.Errorf("missing anime playback state")
	}
	if strings.TrimSpace(link) == "" {
		return "", fmt.Errorf("empty video link")
	}

	if anime.Ep.StreamFlixKey != "" && runtime.GOOS != "android" {
		var err error
		link, err = startFlixPlaybackRelay(link, anime.Ep.StreamFlixKey)
		if err != nil {
			return "", err
		}
	} else {
		stopFlixPlaybackRelay()
	}
	playback := buildPlaybackArgs(config, anime, link, args)

	if socket := anime.Ep.Player.SocketPath; socket != "" && IsMPVRunning(socket) {
		if err := loadInRunningMPV(config, anime, socket, link, title, playback); err != nil {
			return "", err
		}
		return socket, nil
	}

	if runtime.GOOS == "android" {
		return startAndroidIntent(link)
	}

	socket := anime.Ep.Player.SocketPath
	if socket == "" {
		var err error
		if socket, err = newMPVSocketPath(); err != nil {
			return "", err
		}
	}
	if err := launchMPV(config, anime, socket, link, title, playback.args); err != nil {
		stopFlixPlaybackRelay()
		return "", err
	}
	return socket, nil
}

func newMPVSocketPath() (string, error) {
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		Log("Failed to generate random number")
		return "", fmt.Errorf("failed to generate random number: %w", err)
	}
	if runtime.GOOS == "windows" {
		return `\\.\pipe\otakase_mpv_` + hex.EncodeToString(suffix), nil
	}
	return "/tmp/otakase_mpvsocket_" + hex.EncodeToString(suffix), nil
}

// loadInRunningMPV plays the next episode in a player that is already open.
//
// Several options a player starts with are global and stay with every file
// loaded after: the referrer, the extra headers, --sub-file, the HLS bitrate
// cap and the preferred audio and subtitle languages. Each is set again for
// this episode, unless the user's own MpvArgs fixed it.
func loadInRunningMPV(config *Config, anime *Anime, socket, link, title string, playback playbackArgs) error {
	setProperty := func(name string, value interface{}, what string) {
		if _, err := MPVSendCommand(socket, []interface{}{"set_property", name, value}); err != nil {
			Log(fmt.Sprintf("Failed to set the %s: %v", what, err))
		}
	}

	if playback.referrer != "" {
		setProperty("referrer", normalizeReferrerValue(playback.referrer), "referrer")
	}
	// The next episode may resolve in the other mode after a fallback.
	if len(mpvAudioLanguageArgs("", config.MpvArgs)) > 0 {
		setProperty("alang", mpvAudioLanguages(playlistAudioMode(anime, config)), "audio language")
	}
	// And it may be another show, in another language.
	if slang := mpvSubtitleLanguageArgs(subtitleLanguageFor(config, anime), config.MpvArgs); len(slang) > 0 {
		setProperty("slang", strings.TrimPrefix(slang[0], "--slang="), "subtitle language")
	}
	if !playback.ownSubtitles {
		clearMPVSubtitleFiles(MPVSendCommand, socket)
	}
	if !hasMPVHeaderArg(config.MpvArgs) {
		resetMPVStreamHeaders(MPVSendCommand, socket, anime.Ep.StreamHeaders)
	}
	if !hasMPVHLSBitrateArg(config.MpvArgs) {
		resetMPVHLSBitrate(MPVSendCommand, socket, anime.Ep.StreamHLSBitrate)
	}

	if _, err := MPVSendCommand(socket, []interface{}{"loadfile", link}); err != nil {
		return fmt.Errorf("failed to load file in existing MPV instance: %w", err)
	}

	if playback.subtitleURL != "" {
		if err := waitForMPVFileReady(socket, link, subtitleAttachWait); err != nil {
			Log(fmt.Sprintf("Timed out waiting to attach subtitle track: %v", err))
		}
		if _, err := MPVSendCommand(socket, []interface{}{"sub-add", playback.subtitleURL, "select"}); err != nil {
			Log(fmt.Sprintf("Failed to load subtitle track: %v", err))
		}
	}

	setProperty("force-media-title", title, "title")
	setProperty("title", title, "window title")
	return nil
}

// startAndroidIntent hands the link to mpv-android, which takes no IPC.
func startAndroidIntent(link string) (string, error) {
	am, err := findBinary("/system/bin/am")
	if err != nil {
		if am, err = findBinary("am"); err != nil {
			Out("Error: Android activity manager binary not found")
			return "", fmt.Errorf("failed to locate android activity manager binary: %w", err)
		}
	}
	intent := exec.Command(am, "start", "--user", "0",
		"-a", "android.intent.action.VIEW",
		"-d", link,
		"-n", "is.xyz.mpv/.MPVActivity")
	if err := intent.Start(); err != nil {
		Out("Error: Failed to start android intent")
		return "", fmt.Errorf("failed to start android intent: %w", err)
	}
	return androidIntentSocket, nil
}

// launchMPV starts a new player on socket. The window stays open when an
// episode ends, so the next one plays in it.
func launchMPV(config *Config, anime *Anime, socket, link, title string, args []string) error {
	binary, player, err := resolvePlayer(config.Player)
	if err != nil {
		Out("Error: Failed to resolve media player")
		Log(fmt.Sprintf("Player resolution failed for '%s': %v", config.Player, err))
		return err
	}

	playerArgs := []string{"--no-terminal", "--really-quiet", "--input-ipc-server=" + socket}
	playerArgs = append(playerArgs, args...)
	playerArgs = append(playerArgs, "--force-window=yes", "--idle=yes", "--title="+title, "--force-media-title="+title, link)

	if isIINAPlayer(player, binary) {
		playerArgs = append(translateMPVArgsForIINA(playerArgs), "--no-stdin")
	} else if skin := mpvSkinArgs(config, binary, anime.Movie == nil); len(skin) > 0 {
		// First, so the user's MpvArgs after them still have the last word.
		playerArgs = append(skin, playerArgs...)
		markMPVSkinSocket(socket)
	}
	if window := mpvWindowArgs(config, binary); len(window) > 0 {
		playerArgs = append(window, playerArgs...)
	}

	if err := exec.Command(binary, playerArgs...).Start(); err != nil {
		Out(fmt.Sprintf("Error: Failed to start %s process", player))
		return fmt.Errorf("failed to start %s: %w", player, err)
	}

	// Commands sent before the socket opens retry on their own, so a slow
	// start is only logged.
	if !waitForMPVSocket(socket) {
		Log(fmt.Sprintf("MPV socket %s did not open within %s", socket, mpvSocketWait))
	}
	return nil
}

// waitForMPVSocket reports whether socket accepted a connection within
// mpvSocketWait.
func waitForMPVSocket(socket string) bool {
	deadline := time.Now().Add(mpvSocketWait)
	for time.Now().Before(deadline) {
		time.Sleep(mpvSocketPoll)
		conn, err := mpv.Dial(socket)
		if err != nil {
			continue
		}
		_ = conn.Close()
		return true
	}
	return false
}

// WaitForMPVPlaybackStart blocks until mpv reports a time-pos, i.e. the first
// frame is up. It gives up early if mpv goes away and returns false on
// timeout. Players without IPC count as started.
func WaitForMPVPlaybackStart(socket string, timeout time.Duration) bool {
	switch socket {
	case "", androidIntentSocket:
		return true
	}
	Log(fmt.Sprintf("Waiting up to %s for MPV playback to start at %s", timeout, socket))
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pos, err := MPVSendCommand(socket, []interface{}{"get_property", "time-pos"})
		if _, isNum := pos.(float64); err == nil && isNum {
			Log("MPV playback started")
			return true
		}
		if mpv.IsGone(err) {
			Log("MPV exited before playback could start")
			return false
		}
		time.Sleep(mpvPlaybackPollInterval)
	}
	Log("MPV playback did not start within " + timeout.String())
	return false
}

// torrentPlaybackStartTimeout is the least a torrent link gets to start. Its
// first frame waits on peers and on pieces nobody has fetched yet; 45 seconds
// was measured on a well-seeded release, which the regular wait cuts off.
const torrentPlaybackStartTimeout = 120 * time.Second

func playbackStartTimeout(config *Config, links []string) time.Duration {
	timeout := MpvPlaybackStartTimeoutDuration(config)
	if len(links) > 0 && torrentstream.IsStreamURL(BestLink(links)) && timeout < torrentPlaybackStartTimeout {
		return torrentPlaybackStartTimeout
	}
	return timeout
}

// StartVideoWithProviderFallback plays the episode's links and, when mpv
// never gets a frame up, works through the other providers. Every provider in
// the configured audio is tried first; only then is the other audio offered,
// behind a prompt. Returns the socket of the player that started and exits
// the program when nothing plays.
func StartVideoWithProviderFallback(config *Config, anime *Anime, title string) string {
	// Nothing upstream can do anything useful once this gives up.
	giveUp := func(logLine, outLine string) {
		if logLine != "" {
			Log(logLine)
		}
		if outLine != "" {
			Out(outLine)
		}
		restoreAndExit(1)
	}
	if config == nil || anime == nil {
		giveUp("StartVideoWithProviderFallback: missing config or anime", "")
	}
	if len(strings.Fields(title)) == 0 {
		title = defaultEpisodeTitle(anime)
	}

	wantAudio := normalizeTranslationType(config.SubOrDub)
	audio := wantAudio
	var skip []string // providers that already failed in this audio
	askedOtherAudio := false

	for {
		if links := anime.Ep.Links; len(links) < 1 {
			giveUp("", "No episode links found")
		}
		socket, startErr := StartVideo(BestLink(anime.Ep.Links), nil, title, anime)
		if startErr != nil {
			giveUp("Failed to start mpv", "")
		}
		wait := playbackStartTimeout(config, anime.Ep.Links)
		if socket == androidIntentSocket || WaitForMPVPlaybackStart(socket, wait) {
			return socket
		}

		bad := CurrentAnimeProviderName(anime)
		badLinks := slices.Clone(anime.Ep.Links)
		Log(fmt.Sprintf("Playback did not start with provider %s/%s within %s", bad, audio, wait))
		noteProviderResult(bad, "playback", fmt.Errorf("playback did not start within %s", wait))
		Out("Playback failed to start. Trying another copy...")
		if socket != "" {
			ExitMPV(socket)
		}
		anime.Ep.Player.SocketPath = "" // a fresh player for the next try
		skip = append(skip, bad)

		alt, err := ResolveEpisodeURLExcludingProvidersMode(*config, anime, anime.Ep.Number, skip, audio)
		if err == nil && len(alt.Links) > 0 {
			useFallbackLinks(anime, alt)
			Log(fmt.Sprintf("Retrying playback with %s/%s: %+v", alt.ProviderName, alt.Mode, alt.Links))
			Out("Retrying with another copy...")
			continue
		}

		// Out of providers in this audio. The other audio is offered once,
		// and starts from a clean slate: every provider is fair game again.
		if !askedOtherAudio && audio == wantAudio {
			askedOtherAudio = true
			alt, err = ResolveEpisodeURLAlternateModeAfterFailure(*config, anime, anime.Ep.Number, nil, badLinks)
			if err == nil && len(alt.Links) > 0 {
				skip = nil
				audio = normalizeTranslationType(alt.Mode)
				// mpv's alang follows Ep.Mode.
				anime.Ep.Mode = alt.Mode
				useFallbackLinks(anime, alt)
				Log(fmt.Sprintf("Retrying playback with %s/%s after audio fallback: %+v", alt.ProviderName, alt.Mode, alt.Links))
				Out(fmt.Sprintf("Retrying with another copy (%s)...", alt.Mode))
				continue
			}
		}

		reason := ""
		if err != nil {
			reason = "Provider fallback failed: " + err.Error()
		}
		giveUp(reason, "No other copy could start playback for this episode.")
	}
}

// defaultEpisodeTitle is "<show> - Episode <n>", the player title used when
// the caller has none.
func defaultEpisodeTitle(anime *Anime) string {
	return fmt.Sprintf("%s - Episode %d", GetAnimeName(*anime), anime.Ep.Number)
}

func useFallbackLinks(anime *Anime, result ProviderEpisodeResult) {
	anime.Ep.Links = result.Links
	applyStreamPlaybackHints(anime, anime.Ep.Links, result.LinkHints)
}

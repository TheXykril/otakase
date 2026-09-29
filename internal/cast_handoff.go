package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// castSessionVersion is the schema of the handoff file. A child reading a
// version newer than this refuses the file rather than guessing at a tokened
// URL inside it; an older one is read, because every field added since is
// additive and its zero value is what that build meant.
//
// 2 added the fields the spawned process needs once it can finish a season:
// TotalEpisodes and Rewatching, and the completion state Repeat, StartedAt,
// CompletedAt, IsAiring and SkipRemoteSync that TotalEpisodes makes reachable.
//
// No further bump is needed as fields are added at this version: the reader
// accepts anything up to it, missing fields decode to zero, and a version 1
// file carries no TotalEpisodes -- so it cannot reach the completion path that
// reads the rest at all.
const castSessionVersion = 2

// castSessionFile is one resolved episode, handed from the process that picked
// it to the process that casts it.
//
// It names every field explicitly rather than embedding Anime, because
// Episode.StreamReferrer, SubtitleURL, StreamHeaders and Mode are tagged
// json:"-": marshalling an Anime would drop the referrer, and without the
// referrer the remux cannot fetch the stream at all.
type castSessionFile struct {
	Version int `json:"version"`

	AnilistID    int    `json:"anilist_id"`
	MalID        int    `json:"mal_id"`
	ProviderID   string `json:"provider_id"`
	ProviderName string `json:"provider_name"`
	Title        string `json:"title"`

	EpisodeNumber  int               `json:"episode_number"`
	Duration       int               `json:"duration"`
	Links          []string          `json:"links"`
	StreamReferrer string            `json:"stream_referrer"`
	StreamHeaders  map[string]string `json:"stream_headers"`
	SubtitleURL    string            `json:"subtitle_url"`
	Mode           string            `json:"mode"`
	SkipTimes      SkipTimes         `json:"skip_times"`
	Resume         bool              `json:"resume"`
	PlaybackTime   int               `json:"playback_time"`

	// TotalEpisodes is what tells the spawned process the season has an end:
	// without it advanceDecision's atEnd is permanently false there, so the
	// series never finishes and HandleLastEpisodeCompletion never fires.
	TotalEpisodes int `json:"total_episodes"`
	// Rewatching stops the spawned process writing progress over the completed
	// entry a rewatch already has.
	Rewatching bool `json:"rewatching"`

	// The fields below belong to the same completion path that TotalEpisodes
	// made reachable in this process for the first time. Carrying TotalEpisodes
	// without them is worse than carrying neither: the completion runs, and runs
	// on zero values.
	//
	// Repeat and StartedAt are the ones that lose data. CompleteAniListAnimeRewatch
	// writes anime.Repeat+1, so a viewer finishing their fourth rewatch from a
	// rofi cast would have AniList told "repeat 1", and with StartedAt zero it
	// stamps today over the date they actually started. Both are silent and
	// neither is recoverable.
	Repeat    int       `json:"repeat"`
	StartedAt FuzzyDate `json:"started_at"`
	// CompletedAt is not read by the completion path -- that dates the finished
	// entry from today, not from this field. It is carried because it is the
	// rest of the entry's completion state, and because a path that reconciles
	// against the existing entry rather than overwriting it would otherwise
	// silently see zero. Do not read the carrying as the field mattering today.
	CompletedAt FuzzyDate `json:"completed_at"`
	// IsAiring gates both the score prompt and the COMPLETED write. AniList
	// commonly still reports RELEASING for hours after a finale, and main's
	// process does neither in that window; false here made the spawned one do
	// both.
	IsAiring bool `json:"is_airing"`
	// SkipRemoteSync is the viewer having chosen "Continue without updating
	// tracker" on a completed show. It is tagged json:"-" on Anime, so nothing
	// but an explicit field here carries it, and without it their status is
	// written to COMPLETED anyway.
	SkipRemoteSync bool `json:"skip_remote_sync"`
	// Untracked is tagged json:"-" on Anime for the same reason, and without
	// it here the spawned window would write history and push progress for a
	// show the viewer asked not to track.
	Untracked bool `json:"untracked"`

	Device string `json:"device"`
}

// castSessionDir is where handoff files live, under the storage path.
func castSessionDir(config *Config) string {
	return filepath.Join(os.ExpandEnv(config.StoragePath), "cast-session")
}

// writeCastSession records a resolved episode for another process to cast, and
// returns the file's path.
func writeCastSession(config *Config, anime *Anime, device string) (string, error) {
	dir := castSessionDir(config)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("cast: could not create the session directory: %w", err)
	}

	session := castSessionFile{
		Version:        castSessionVersion,
		AnilistID:      anime.AnilistId,
		MalID:          anime.MalId,
		ProviderID:     anime.ProviderId,
		ProviderName:   CurrentAnimeProviderName(anime),
		Title:          GetAnimeName(*anime),
		EpisodeNumber:  anime.Ep.Number,
		Duration:       anime.Ep.Duration,
		Links:          anime.Ep.Links,
		StreamReferrer: anime.Ep.StreamReferrer,
		StreamHeaders:  anime.Ep.StreamHeaders,
		SubtitleURL:    anime.Ep.SubtitleURL,
		Mode:           anime.Ep.Mode,
		SkipTimes:      anime.Ep.SkipTimes,
		Resume:         anime.Ep.Resume,
		PlaybackTime:   anime.Ep.Player.PlaybackTime,
		TotalEpisodes:  anime.TotalEpisodes,
		Rewatching:     anime.Rewatching,
		Repeat:         anime.Repeat,
		StartedAt:      anime.StartedAt,
		CompletedAt:    anime.CompletedAt,
		IsAiring:       anime.IsAiring,
		SkipRemoteSync: anime.SkipRemoteSync,
		Untracked:      anime.Untracked,
		Device:         device,
	}

	encoded, err := json.Marshal(session)
	if err != nil {
		return "", fmt.Errorf("cast: could not encode the session: %w", err)
	}

	file, err := os.CreateTemp(dir, "session-*.json")
	if err != nil {
		return "", fmt.Errorf("cast: could not create the session file: %w", err)
	}
	path := file.Name()
	// The stream URL carries an authentication token, so this is never group
	// or world readable. CreateTemp already makes it 0600; stated here so a
	// later change to the creation call cannot quietly widen it.
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		os.Remove(path)
		return "", fmt.Errorf("cast: could not secure the session file: %w", err)
	}
	if _, err := file.Write(encoded); err != nil {
		file.Close()
		os.Remove(path)
		return "", fmt.Errorf("cast: could not write the session file: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("cast: could not close the session file: %w", err)
	}
	return path, nil
}

// readCastSession loads a handoff file and deletes it.
//
// It is deleted whatever the outcome: the file has served its purpose the
// moment it is opened, and leaving a tokened URL on disk after a failed read
// is worse than losing the episode.
func readCastSession(path string) (*castSessionFile, error) {
	defer os.Remove(path)

	encoded, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cast: could not read the session file: %w", err)
	}

	var session castSessionFile
	if err := json.Unmarshal(encoded, &session); err != nil {
		return nil, fmt.Errorf("cast: could not decode the session file: %w", err)
	}
	// Only a file from a newer build is refused. One from an older build is
	// missing fields added since, and their zero values are exactly what that
	// build meant -- refusing it would strand a viewer who upgraded between the
	// rofi pick and the window opening.
	if session.Version < 1 || session.Version > castSessionVersion {
		return nil, fmt.Errorf("cast: session file version %d, this build understands up to %d", session.Version, castSessionVersion)
	}
	return &session, nil
}

// sweepCastSessions deletes handoff files older than olderThan.
//
// A spawn that succeeded but whose terminal died before reading leaves a file
// holding a tokened URL, and nothing else would ever remove it.
func sweepCastSessions(config *Config, olderThan time.Duration) {
	dir := castSessionDir(config)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-olderThan)
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
}

// castSessionToAnime rebuilds the episode the picking process resolved.
func castSessionToAnime(session *castSessionFile) *Anime {
	anime := &Anime{
		AnilistId:    session.AnilistID,
		MalId:        session.MalID,
		ProviderId:   session.ProviderID,
		ProviderName: session.ProviderName,
	}
	anime.Title.Romaji = session.Title
	anime.Ep.Number = session.EpisodeNumber
	anime.Ep.Duration = session.Duration
	anime.Ep.Links = session.Links
	anime.Ep.StreamReferrer = session.StreamReferrer
	anime.Ep.StreamHeaders = session.StreamHeaders
	anime.Ep.SubtitleURL = session.SubtitleURL
	anime.Ep.Mode = session.Mode
	anime.Ep.SkipTimes = session.SkipTimes
	anime.Ep.Resume = session.Resume
	anime.Ep.Player.PlaybackTime = session.PlaybackTime
	anime.TotalEpisodes = session.TotalEpisodes
	anime.Rewatching = session.Rewatching
	anime.Repeat = session.Repeat
	anime.StartedAt = session.StartedAt
	anime.CompletedAt = session.CompletedAt
	anime.IsAiring = session.IsAiring
	anime.SkipRemoteSync = session.SkipRemoteSync
	anime.Untracked = session.Untracked
	return anime
}

// castEpisodeForSession is the cast castSessionEpisode runs. A variable so the
// spawned loop can be tested without a device.
var castEpisodeForSession = CastEpisode

// castSessionEpisode casts one episode for the spawned session, resolving its
// stream first when it has none.
//
// The handoff file carries the links for the first episode only. Every episode
// after it arrives from StartNextEpisode, which clears Ep.Links "to force
// fetching new ones" -- a fetch main's loop performs inside StartPlayback and
// this process never reaches. So it is performed here, through the same helper.
func castSessionEpisode(config *Config, anime *Anime) error {
	if anime == nil {
		return fmt.Errorf("cast: nothing to play")
	}
	if len(anime.Ep.Links) == 0 && !ResolveEpisodeLinks(config, anime) {
		return fmt.Errorf("cast: could not find a stream for episode %d", anime.Ep.Number)
	}
	return castEpisodeForSession(config, anime)
}

// castTerminalCandidates are the emulators tried when nothing is configured,
// in order. Every one of them accepts "-e command args".
var castTerminalCandidates = []string{"ghostty", "kitty", "alacritty", "foot", "wezterm", "xterm"}

// resolveCastTerminal decides which terminal emulator to spawn.
//
// CastTerminal wins, then $TERMINAL, then whatever is installed. The value is
// split on whitespace before being looked up, because both routinely carry
// arguments ("flatpak run org.x.Term"): handing the whole string to
// exec.Command looks for a binary whose name contains spaces, the same trap
// internal/editor.go documents for EDITOR.
//
// lookPath is exec.LookPath in production and a stub in tests.
func resolveCastTerminal(config *Config, lookPath func(string) (string, error)) ([]string, error) {
	preferences := []string{}
	if config != nil && strings.TrimSpace(config.CastTerminal) != "" {
		preferences = append(preferences, config.CastTerminal)
	}
	if fromEnv := strings.TrimSpace(os.Getenv("TERMINAL")); fromEnv != "" {
		preferences = append(preferences, fromEnv)
	}
	preferences = append(preferences, castTerminalCandidates...)

	for _, preference := range preferences {
		fields := strings.Fields(preference)
		if len(fields) == 0 {
			continue
		}
		binary, err := lookPath(fields[0])
		if err != nil {
			continue
		}
		return append([]string{binary}, fields[1:]...), nil
	}
	return nil, fmt.Errorf("cast: no terminal emulator found; set CastTerminal in the config")
}

// handOffCastToTerminal starts the cast in a terminal of its own and returns.
//
// A rofi launch has no terminal, so a cast started from one has nowhere to show
// its controls or its progress. Rather than casting here, the resolved episode
// is written out and a terminal is opened to do the casting: that process owns
// ffmpeg, the HTTP server, the device session and all tracking, so closing its
// window ends the episode and exactly one process ever writes history.
func handOffCastToTerminal(config *Config, anime *Anime) error {
	sweepCastSessions(config, time.Hour)

	terminal, err := resolveCastTerminal(config, exec.LookPath)
	if err != nil {
		return err
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cast: could not find this program on disk: %w", err)
	}

	// The device is resolved here, in the process that still has a viewer at the
	// keyboard, rather than in the window that does not. The spawned session runs
	// with CastNonInteractive set, so with two or more devices on the network and
	// no CastDevice configured its own picker refuses, CastEpisode returns the
	// refusal, and the window closes on "interactive menu unavailable" without
	// naming what to set. Asking now turns that dead end into one menu.
	device := config.CastDevice
	if device == "" {
		chosen, err := chooseCastDevice(config)
		if err != nil {
			return fmt.Errorf("cast: no device to cast to -- set CastDevice in the config to skip this: %w", err)
		}
		device = chosen.Name
	}

	path, err := writeCastSession(config, anime, device)
	if err != nil {
		return err
	}

	args := append(terminal[1:], "-e", self, "-cast-session", path)
	command := exec.Command(terminal[0], args...)
	if err := command.Start(); err != nil {
		os.Remove(path)
		return fmt.Errorf("cast: could not open a terminal: %w", err)
	}

	// Not waited on: this process is done, and the cast belongs to the window
	// now. Release ties off the child so it is not left a zombie.
	if err := command.Process.Release(); err != nil {
		Log(fmt.Sprintf("cast: could not release the terminal process: %v", err))
	}
	return nil
}

// prepareCastSessionUser gives the spawned session the user every later step
// needs, signing in to the configured trackers when there are any.
//
// The user is set whatever TrackingRemote says. AdvanceAfterEpisode refuses a
// nil user, so with tracking off the spawned cast used to finish episode 1 and
// exit -- after the viewer had watched a ten-second countdown promising the
// next one -- and the LocalUpdateAnime write behind the same guard, which needs
// no tracker at all, was skipped with it.
//
// A sign-in failure is reported rather than fatal: local history still works,
// and losing the episode over it would be worse.
func prepareCastSessionUser(config *Config) *User {
	user := GetGlobalUser()
	if user == nil {
		user = &User{}
		SetGlobalUser(user)
	}

	// This process reaches casting above the point where main signs the viewer
	// in, so without this the spawned window would write curd_history.txt,
	// report the episode watched, and never move AniList or MyAnimeList -- the
	// tracker silently stops following the one launch path this feature makes
	// primary.
	if UsesRemoteTracking(config) {
		if err := EnsureConfiguredTrackersReady(config, user); err != nil {
			Out("Remote tracking is unavailable for this cast: " + err.Error())
			Log(fmt.Sprintf("cast: could not ready trackers in the spawned session: %v", err))
		}
	}
	return user
}

// RunCastSession casts the episode named by a handoff file.
//
// This is the spawned terminal's entry point. RofiSelection is cleared because
// this process does have a terminal: Out must print here rather than raise a
// desktop notification.
// prepareCastSession turns a handoff file into the state the spawned process
// needs before it can cast: the episode, the process globals the tracking code
// reads, and the history file's path.
//
// Split out of RunCastSession so the wiring is testable without a Chromecast.
// Every line here was a defect at some point -- a missing user stopped the
// season advancing, and a zero global anime sent tracking writes to the wrong
// entry -- and a test that calls RunCastSession cannot reach any of it.
func prepareCastSession(config *Config, session *castSessionFile) (*Anime, string) {
	config.RofiSelection = false
	config.CastToDevice = true
	// The viewer is across the room. A menu opened here blocks the season on an
	// answer nobody is there to give, and its keystrokes would be split with the
	// process-global cast reader anyway. Prompts on this path take a declared
	// default and report it -- see docs/cast-window-prompts.md.
	config.CastNonInteractive = true
	if session.Device != "" {
		config.CastDevice = session.Device
	}

	anime := castSessionToAnime(session)

	prepareCastSessionUser(config)

	// The terminal is taken once for the whole cast rather than per episode, so
	// the panel does not blink between episodes and the season-end prompts have
	// the same frame to draw in that the episodes did. Held after RofiSelection
	// is forced off above, because that is what castControlsPossible reads.
	if beginCastSessionScreen(config) {
		defer endCastSessionScreen()
	}

	// ShouldWriteRemoteTracking reads the global anime, which in this process
	// is still main's zero value rather than the episode being cast.
	SetGlobalAnime(anime)

	return anime, filepath.Join(os.ExpandEnv(config.StoragePath), "curd_history.txt")
}

func RunCastSession(config *Config, path string) error {
	session, err := readCastSession(path)
	if err != nil {
		return err
	}

	anime, databaseFile := prepareCastSession(config, session)

	// The terminal is taken once for the whole cast rather than per episode, so
	// the panel does not blink between episodes and the season-end prompts have
	// the same frame to draw in that the episodes did. Held after
	// prepareCastSession has forced RofiSelection off, because that is what
	// castControlsPossible reads.
	if beginCastSessionScreen(config) {
		defer endCastSessionScreen()
	}

	var lastErr error
	runCastLoop(
		func() error {
			lastErr = castSessionEpisode(config, anime)
			return lastErr
		},
		func() bool {
			// The countdown in CastEpisode already asked, and the loop only
			// reaches here when it said advance: asking again would put an
			// interactive menu in front of a viewer across the room.
			if anime.Untracked {
				advanceUntrackedCast(anime)
				return true
			}
			return AdvanceAfterEpisode(config, anime, GetGlobalUser(), databaseFile, func() bool { return true })
		},
	)

	// Released before the summary prints, so the summary is the last thing in
	// the scrollback rather than a notification fired from behind a panel that
	// is on its way out.
	endCastSessionScreen()
	if summary := takeCastDeferredSummary(); summary != "" {
		Out(summary)
	}

	if errors.Is(lastErr, ErrCastStopped) {
		return nil
	}
	return lastErr
}

// WaitBeforeClosingCastWindow holds a failed cast's window open until the
// viewer presses q or enter.
//
// The spawned window closes the moment this process exits, so a failure
// printed just before the exit was on screen for a frame: a firewall that
// blocked the device read as a window that simply vanished, with the fix it
// named never seen. It waits only where there is a keyboard to answer it;
// without one it returns at once rather than leave a process nobody can end.
func WaitBeforeClosingCastWindow(config *Config) {
	commands, release, ok := startCastControls(config)
	if !ok {
		return
	}
	defer release()
	Out("Press q or enter to close this window.")
	for command := range commands {
		if command == castCmdStop || command == castCmdSelect {
			return
		}
	}
}

// advanceUntrackedCast moves an untracked cast on to the next episode.
//
// AdvanceAfterEpisode is the tracked path's: it writes the finished episode to
// the history file and the tracker, which is exactly what Untracked Watching
// promises not to do. An untracked show has no episode count to stop at, so
// the next episode is simply asked for; when the provider has none, resolving
// its stream fails and the season ends there.
func advanceUntrackedCast(anime *Anime) {
	anime.Ep.Number++
	anime.Ep.Links = nil
	anime.Ep.StreamReferrer = ""
	anime.Ep.StreamHeaders = nil
	anime.Ep.SubtitleURL = ""
	anime.Ep.Duration = 0
	anime.Ep.SkipTimes = SkipTimes{}
	anime.Ep.Resume = false
	anime.Ep.Player.PlaybackTime = 0
}

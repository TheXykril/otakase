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
// version it does not know refuses the file rather than guessing at a
// tokened URL inside it.
const castSessionVersion = 1

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
	if session.Version != castSessionVersion {
		return nil, fmt.Errorf("cast: session file version %d, this build understands %d", session.Version, castSessionVersion)
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
	return anime
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

	path, err := writeCastSession(config, anime, config.CastDevice)
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

// RunCastSession casts the episode named by a handoff file.
//
// This is the spawned terminal's entry point. RofiSelection is cleared because
// this process does have a terminal: Out must print here rather than raise a
// desktop notification.
func RunCastSession(config *Config, path string) error {
	session, err := readCastSession(path)
	if err != nil {
		return err
	}

	config.RofiSelection = false
	config.CastToDevice = true
	if session.Device != "" {
		config.CastDevice = session.Device
	}

	anime := castSessionToAnime(session)

	// This process reaches casting above the point where main signs the viewer
	// in, so without this the spawned window would write curd_history.txt,
	// report the episode watched, and never move AniList or MyAnimeList -- the
	// tracker silently stops following the one launch path this feature makes
	// primary. A sign-in failure is reported rather than fatal: local history
	// still works, and losing the episode over it would be worse.
	if UsesRemoteTracking(config) {
		user := GetGlobalUser()
		if user == nil {
			user = &User{}
			SetGlobalUser(user)
		}
		if err := EnsureConfiguredTrackersReady(config, user); err != nil {
			Out("Remote tracking is unavailable for this cast: " + err.Error())
			Log(fmt.Sprintf("cast: could not ready trackers in the spawned session: %v", err))
		}
	}

	// ShouldWriteRemoteTracking reads the global anime, which in this process
	// is still main's zero value rather than the episode being cast.
	SetGlobalAnime(anime)

	// The spawned process has no show to select, so it cannot enter main's
	// loop at the top. It runs the same advance instead, so a rofi cast and a
	// local playback continue through one implementation.
	databaseFile := filepath.Join(os.ExpandEnv(config.StoragePath), "curd_history.txt")
	var lastErr error
	runCastLoop(
		func() error {
			lastErr = CastEpisode(config, anime)
			return lastErr
		},
		func() bool {
			return AdvanceAfterEpisode(config, anime, GetGlobalUser(), databaseFile)
		},
	)

	if errors.Is(lastErr, ErrCastStopped) {
		return nil
	}
	return lastErr
}

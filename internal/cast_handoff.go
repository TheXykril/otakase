package internal

import (
	"encoding/json"
	"fmt"
	"net/url"
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

	// Movie is set for a film or series episode from the Movies section. The
	// window casts it the way an inline cast does, from Start, and writes
	// where it got to into the report file for the process that opened it,
	// which keeps the movie history and is still waiting.
	Movie *castSessionMovie `json:"movie,omitempty"`
}

// castSessionMovie is what a cast window needs to play a movie.
type castSessionMovie struct {
	Start  float64 `json:"start"`
	Report string  `json:"report"`
}

// castSessionDir is where handoff files live, under the storage path.
func castSessionDir(config *Config) string {
	return filepath.Join(os.ExpandEnv(config.StoragePath), "cast-session")
}

// writeCastSession records a resolved episode for another process to cast, and
// returns the file's path.
func writeCastSession(config *Config, anime *Anime, device string) (string, error) {
	return writeCastSessionFile(config, castSessionFor(anime, device))
}

// castSessionFor is the handoff record of a resolved episode.
func castSessionFor(anime *Anime, device string) castSessionFile {
	return castSessionFile{
		Version:        castSessionVersion,
		AnilistID:      anime.AnilistId,
		MalID:          anime.MalId,
		ProviderID:     anime.ProviderId,
		ProviderName:   CurrentAnimeProviderName(anime),
		Title:          GetAnimeName(*anime),
		EpisodeNumber:  anime.Ep.Number,
		Duration:       anime.Ep.Duration,
		Links:          handOffLinks(anime.Ep.Links),
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
}

// writeCastSessionFile writes a handoff record and returns the file's path.
func writeCastSessionFile(config *Config, session castSessionFile) (string, error) {
	dir := castSessionDir(config)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("cast: could not create the session directory: %w", err)
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

// isProcessLocalLink reports a link only this process can serve. A torrent is
// streamed from a server inside otakase, which closes when this process exits
// -- which, on a handoff, is straight after spawning the window. Any loopback
// link is one of those, since no provider hands out a link to the viewer's own
// machine.
func isProcessLocalLink(link string) bool {
	parsed, err := url.Parse(link)
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

// handOffLinks is the links the spawned window can still open. Without the
// ones served from this process it has none, and resolves the episode itself
// the way it does every episode after the first.
func handOffLinks(links []string) []string {
	kept := make([]string, 0, len(links))
	for _, link := range links {
		if !isProcessLocalLink(link) {
			kept = append(kept, link)
		}
	}
	return kept
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

	device, err := castHandOffDevice(config)
	if err != nil {
		return err
	}

	path, err := writeCastSession(config, anime, device)
	if err != nil {
		return err
	}
	command, err := openCastWindow(config, path)
	if err != nil {
		return err
	}

	// The window announces the close when the cast ends; this process exiting
	// now is not otakase closing.
	disarmCloseNotice()

	// Not waited on: this process is done, and the cast belongs to the window
	// now. Release ties off the child so it is not left a zombie.
	if err := command.Process.Release(); err != nil {
		Log(fmt.Sprintf("cast: could not release the terminal process: %v", err))
	}
	return nil
}

// castHandOffDevice is the device a cast window should use.
func castHandOffDevice(config *Config) (string, error) {
	// The device is resolved here, in the process that still has a viewer at the
	// keyboard, rather than in the window that does not. The spawned session runs
	// with CastNonInteractive set, so with two or more devices on the network and
	// no CastDevice configured its own picker refuses, CastEpisode returns the
	// refusal, and the window closes on "interactive menu unavailable" without
	// naming what to set. Asking now turns that dead end into one menu.
	if device := config.CastDevice; device != "" {
		return device, nil
	}
	chosen, err := chooseCastDevice(config)
	if err != nil {
		return "", fmt.Errorf("cast: no device to cast to -- set CastDevice in the config to skip this: %w", err)
	}
	return chosen.Name, nil
}

// openCastWindow starts a terminal that casts the handoff file at path. The
// file is removed when no terminal could be started.
var openCastWindow = func(config *Config, path string) (*exec.Cmd, error) {
	terminal, err := resolveCastTerminal(config, exec.LookPath)
	if err != nil {
		os.Remove(path)
		return nil, err
	}

	self, err := os.Executable()
	if err != nil {
		os.Remove(path)
		return nil, fmt.Errorf("cast: could not find this program on disk: %w", err)
	}

	args := append(terminal[1:], "-e", self, "-cast-session", path)
	command := exec.Command(terminal[0], args...)
	if err := command.Start(); err != nil {
		os.Remove(path)
		return nil, fmt.Errorf("cast: could not open a terminal: %w", err)
	}
	return command, nil
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

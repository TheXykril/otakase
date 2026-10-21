package internal

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/thexykril/otakase/internal/cast"
)

// A cast device cannot seek. The receiver accepts SEEK, answers without an
// error, and goes on playing where it was: an event playlist has no length for
// it to seek within, and it reports dur=-1.0 for the whole episode. Nine seeks
// on real hardware confirmed it, every one err=<nil>, the reported position
// walking forward in real time throughout.
//
// So seeking means rebuilding the stream. ffmpeg restarts at the target, into a
// directory of its own, and the device is handed that as a new stream -- which
// it always plays from zero, because for the device it is a new stream.
//
// Two adapters carry that into the watch loop, which otherwise assumes one
// stream from beginning to end:
//
//   - castSeekingSession translates between the device's clock and the
//     episode's. Everything above it -- the panel, the progress written to the
//     trackers, the completion threshold, the skip spans -- keeps working in
//     episode time, which is the only timeline that means anything to a viewer.
//   - castRemuxSwitch keeps the loop watching whichever ffmpeg is current. The
//     loop reads Err() to tell a dead remux from a finished episode, and a
//     remux stopped on purpose must not look like either.

// castStreamSource rebuilds the stream at an offset.
type castStreamSource struct {
	ffmpeg    string
	streamURL string
	referrer  string
	// headers are any the provider needs beyond the referrer, as mpv is sent
	// them: one host 403s its segments without Origin.
	headers map[string]string
	// rootDir holds one directory per generation. The device is given a new URL
	// each time, because a device handed the same URL twice may serve the
	// episode out of its own cache and never ask for the new segments.
	rootDir string
	// subtitlePath is the file as the provider wrote it, always the original:
	// every shift is taken from it, so offsets stay absolute rather than
	// compounding with each seek.
	subtitlePath string
	// liveSubs reads a release's own subtitles out of the file while the cast
	// plays. Each generation burns what it has read by then, and coveredUntil
	// is how far into the episode that reaches: +Inf when the generation has
	// all of them, or none are coming.
	liveSubs     castSubtitleSource
	coveredUntil float64
	// encoder re-encodes the video, whenever it is drawn on or restarted at an
	// offset. Left empty, chooseEncoder picks it the first time it is needed,
	// so a cast that is never re-encoded never pays for the hardware probe.
	encoder       cast.Encoder
	chooseEncoder func() cast.Encoder
	// maps names the video and audio to play, from cast.SelectTracks; nil
	// leaves the choice to ffmpeg. The same every generation, so a seek keeps
	// the language the cast began in.
	maps []string

	generation int

	// launch starts the process. It is a field so a test can stand in for ffmpeg
	// and exercise the generation bookkeeping, which is where a wrong answer is
	// silent: a reused URL leaves the device playing from its own cache.
	launch func(ffmpeg string, args []string, outDir string) (*cast.Remux, error)
}

// castSubtitleSource is what a cast needs of cast.SubtitleReader, so the
// rebuild rule can be tested without ffmpeg.
type castSubtitleSource interface {
	Covered() float64
	Snapshot(dst string) (float64, error)
	FontsDir() string
}

// stream is one generation of the stream: a running ffmpeg, the path the device
// should fetch, and the directory holding both.
type stream struct {
	remux *cast.Remux
	path  string
	dir   string
}

// start begins a generation of the stream at startAt seconds into the episode.
func (s *castStreamSource) start(startAt float64) (stream, error) {
	dir := filepath.Join(s.rootDir, "g"+strconv.Itoa(s.generation))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return stream{}, fmt.Errorf("cast: could not create the stream directory: %w", err)
	}
	s.generation++

	subtitlePath, fontsDir := s.subtitlePath, ""
	s.coveredUntil = math.Inf(1)
	if s.liveSubs != nil {
		subtitlePath, s.coveredUntil = s.subtitlesSoFar(dir)
		fontsDir = s.liveSubs.FontsDir()
	}

	args := cast.BuildRemuxArgsFrom(s.streamURL, s.referrer, dir, startAt, s.maps, s.headers)
	if subtitlePath == "" && startAt > 0 {
		// Copying starts each track on its own packet boundary, seconds apart
		// on a host that serves audio and video separately, so a stream that
		// starts anywhere but zero is re-encoded to start both at the target.
		// See cast.BuildRemuxArgsFrom.
		args = cast.BuildBurnArgsFrom(s.streamURL, s.referrer, "", dir, s.reencoder(), startAt, s.maps, s.headers)
	}
	if subtitlePath != "" {
		// Shifted into this generation's own directory, from the original, so a
		// second seek does not shift an already-shifted file. The original's
		// extension is kept: it names the format, and an ASS file written out
		// as subtitles.vtt says the wrong one.
		shifted := filepath.Join(dir, "subtitles"+filepath.Ext(subtitlePath))
		if err := cast.ShiftSubtitles(subtitlePath, shifted, startAt); err != nil {
			// Subtitles that cannot be shifted are worth less than the seek:
			// burn the original and let them run late rather than refusing to
			// move at all.
			Log(fmt.Sprintf("cast: %v", err))
			shifted = subtitlePath
		}
		args = cast.BuildBurnArgsWithFonts(s.streamURL, s.referrer, shifted, fontsDir, dir, s.reencoder(), startAt, s.maps, s.headers)
	}

	launch := s.launch
	if launch == nil {
		launch = cast.StartFFmpeg
	}
	remux, err := launch(s.ffmpeg, args, dir)
	if err != nil {
		return stream{}, err
	}
	if err := awaitPlaylist(remux, dir); err != nil {
		remux.Stop()
		return stream{}, err
	}

	return stream{
		remux: remux,
		dir:   dir,
		// Forward slashes, not filepath: this is a URL path, and the device is
		// not necessarily on the same kind of machine as the one serving it.
		path: "g" + strconv.Itoa(s.generation-1) + "/" + cast.PlaylistName,
	}, nil
}

// subtitlesSoFar copies what the subtitle reader has read into dir for one
// generation, and says how far into the episode it reaches. A copy holding no
// events yet -- the reader has only just started -- is no file at all: the
// filter would draw nothing from it, and an empty one stops it opening.
func (s *castStreamSource) subtitlesSoFar(dir string) (string, float64) {
	path := filepath.Join(dir, "subtitles-read.ass")
	covered, err := s.liveSubs.Snapshot(path)
	if err != nil {
		Log(fmt.Sprintf("cast: %v", err))
		return "", 0
	}
	raw, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(raw, []byte("[Events]")) {
		return "", 0
	}
	if math.IsInf(covered, 1) {
		Log("cast: burning all of the file's subtitles")
	} else {
		Log(fmt.Sprintf("cast: burning the file's subtitles up to %.0fs", covered))
	}
	return path, covered
}

// castSubtitleMargin is how close playback gets to the end of the subtitles
// burned into the stream before it is rebuilt with more. A rebuild takes a
// few seconds, and the line at the edge should not be the one it costs.
const castSubtitleMargin = 30.0

// castSubtitleMinGain is the least a rebuild has to add. Every rebuild is a
// few seconds of buffering on the TV, so one that adds a few lines is not
// worth it. A minute keeps a stretch without subtitles short -- after a
// resume the first stream may hold only seconds of them -- while a healthy
// download, reading several minutes for every one played, still needs only a
// handful of rebuilds before it has them all.
const castSubtitleMinGain = 60.0

// subtitlesBehind reports whether playback at position is about to run past
// the subtitles burned into the stream, and enough more have been read to be
// worth rebuilding it for.
func (s *castStreamSource) subtitlesBehind(position float64) bool {
	if s.liveSubs == nil || math.IsInf(s.coveredUntil, 1) {
		return false
	}
	if position < s.coveredUntil-castSubtitleMargin {
		return false
	}
	covered := s.liveSubs.Covered()
	return math.IsInf(covered, 1) || covered >= math.Max(s.coveredUntil, position)+castSubtitleMinGain
}

// reencoder is the encoder for a re-encoded generation, chosen once.
func (s *castStreamSource) reencoder() cast.Encoder {
	if s.encoder.Name == "" {
		s.encoder = cast.Encoder{Name: "libx264"}
		if s.chooseEncoder != nil {
			s.encoder = s.chooseEncoder()
		}
		Log(fmt.Sprintf("cast: re-encoding with %s (hardware=%t)", s.encoder.Name, s.encoder.Hardware()))
	}
	return s.encoder
}

// castRemuxSwitch is the remux the watch loop watches, which changes under it
// every time the viewer seeks.
//
// Only the current generation is reachable through it. That is what keeps a seek
// from ending the episode: stopping ffmpeg makes it exit non-zero, the loop reads
// a remux error as a stream that died under the device, and the stopped one is
// no longer the one being read. Suppressing errors after a seek instead would
// also hide a rebuilt stream that genuinely failed to start.
type castRemuxSwitch struct {
	mu      sync.Mutex
	current *cast.Remux
	// swapped records that at least one seek has happened, so the caller can
	// tell a finished rebuilt stream from a finished episode.
	swapped bool
}

func newCastRemuxSwitch(current *cast.Remux) *castRemuxSwitch {
	return &castRemuxSwitch{current: current}
}

// swap stops the running remux and takes up the new one.
func (r *castRemuxSwitch) swap(next *cast.Remux) *cast.Remux {
	// The new generation is installed before the old one is stopped, so a poll
	// landing in between reads the stream that is taking over rather than the
	// one being killed.
	r.mu.Lock()
	previous := r.current
	r.current = next
	r.swapped = true
	r.mu.Unlock()

	if previous != nil {
		previous.Stop()
	}
	return previous
}

func (r *castRemuxSwitch) Err() error {
	r.mu.Lock()
	current := r.current
	r.mu.Unlock()

	if current == nil {
		return nil
	}
	return current.Err()
}

func (r *castRemuxSwitch) Done() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current == nil {
		// A nil channel blocks forever, which is what "not finished" means to
		// the select this feeds.
		return nil
	}
	return r.current.Done()
}

// seeked reports whether any seek has happened.
func (r *castRemuxSwitch) seeked() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.swapped
}

// castSeekingSession is the device, seen in episode time.
type castSeekingSession struct {
	inner castSession

	mu sync.Mutex
	// base is where in the episode the current stream starts, so the episode
	// position is base plus whatever the device reports.
	base float64
	// duration is the episode's real length, read from the source before
	// playback. The device reports dur=-1.0 throughout and, after a seek, would
	// report the length of a stream that begins in the middle.
	duration float64

	// restart rebuilds the stream at a target and hands it to the device.
	restart func(target float64) error

	// behind reports whether the stream should be rebuilt where it is to burn
	// subtitles read since it was built; nil when none are being read.
	behind func(position float64) bool
}

// castRollingSubtitles is a session whose subtitles are still being read out
// of the file. The watch loop asks for it rather than taking it as an
// argument, so every session without any stays exactly as it was.
type castRollingSubtitles interface {
	subtitlesBehind(position float64) bool
}

func (s *castSeekingSession) subtitlesBehind(position float64) bool {
	return s.behind != nil && s.behind(position)
}

func (s *castSeekingSession) Progress() (cast.Progress, error) {
	progress, err := s.inner.Progress()

	s.mu.Lock()
	base, duration := s.base, s.duration
	s.mu.Unlock()

	progress.Position += base
	if duration > 0 {
		progress.Duration = duration
	}
	return progress, err
}

// SeekToTime rebuilds the stream at seconds instead of asking the device, which
// would accept the request and ignore it.
func (s *castSeekingSession) SeekToTime(seconds float64) error {
	if seconds < 0 {
		seconds = 0
	}

	s.mu.Lock()
	duration := s.duration
	s.mu.Unlock()

	// Seeking past the end has nowhere to go: ffmpeg would produce an empty
	// stream and the device would sit on a playlist that never gains a segment.
	// The last few seconds are left out of reach deliberately, because a stream
	// starting inside them is over before it buffers.
	if duration > 0 && seconds > duration-castSeekTailGuard {
		seconds = duration - castSeekTailGuard
		if seconds < 0 {
			seconds = 0
		}
	}

	if err := s.restart(seconds); err != nil {
		return err
	}

	s.mu.Lock()
	s.base = seconds
	s.mu.Unlock()
	return nil
}

func (s *castSeekingSession) Pause() error              { return s.inner.Pause() }
func (s *castSeekingSession) Unpause() error            { return s.inner.Unpause() }
func (s *castSeekingSession) SetVolume(l float64) error { return s.inner.SetVolume(l) }
func (s *castSeekingSession) Volume() float64           { return s.inner.Volume() }

// castSeekTailGuard is how much of the end a seek will not land in.
//
// A stream that starts two seconds before the credits end has to launch, buffer
// and reach the device before it is over, and the receiver's own idle timeout
// arrives first. Five seconds is enough to be worth playing and short enough
// that seeking "to the end" still lands in the last moments.
const castSeekTailGuard = 5.0

// awaitPlaylist waits for ffmpeg to write something the device can fetch.
//
// It watches the remux as well as the file, because a stream short enough to
// finish before the wait notices exits without complaint -- and because a remux
// that failed has a reason worth reporting, where the wait only knows that no
// playlist appeared.
func awaitPlaylist(remux *cast.Remux, dir string) error {
	written := make(chan error, 1)
	go func() {
		written <- cast.WaitForPlaylist(dir, castStartTimeout)
	}()

	select {
	case err := <-written:
		if err != nil {
			if remuxErr := remux.Err(); remuxErr != nil {
				return remuxErr
			}
			return err
		}
		return nil
	case <-remux.Done():
		if remuxErr := remux.Err(); remuxErr != nil {
			return remuxErr
		}
		// ffmpeg exited without complaint. The playlist was written either way,
		// so the wait's own answer is the one that counts.
		return <-written
	}
}

// castResumeAt answers where in the episode a cast should begin.
//
// Resuming is the first seek, made before playback starts: the stream is built
// from the saved position, so the device is handed the rest of the episode as a
// stream of its own. Until the stream could be rebuilt at an offset this was not
// possible at all, and a part-watched episode was disclosed and then started over.
//
// The position is read from the history file here rather than taken from the
// anime as it arrives, because which episode is playing can change after the
// position was attached to it -- and does, routinely. A cast launched from rofi
// runs in a second process that never sees the picking flow at all: it rebuilds
// the anime from a handoff file, so whatever position that file carries was
// resolved against whichever episode the first process had in mind. Casting
// episode 10 with a position belonging to episode 11 is how this last failed:
// one second, rejected as too early to be worth a restart, silently.
//
// formatResumePosition is the gate, the same one the list rows use: it rejects a
// position too early to be worth anything and one near enough to the end that
// resuming is pointless. Its duration argument is minutes, and Ep.Duration is
// seconds in memory.
func castResumeAt(config *Config, anime *Anime) float64 {
	if anime != nil && anime.Movie != nil {
		return anime.Movie.Start
	}
	// An untracked episode has no history row to resume from.
	if anime == nil || anime.Untracked {
		return 0
	}
	// Nothing intends to resume: either the episode was never started, or the
	// viewer said to start it over -- a rewatch, or the tracker's episode chosen
	// over the local one. Both clear these deliberately, and both must stay
	// cleared however tempting the row in the history file looks.
	if !anime.Ep.Resume && anime.Ep.Player.PlaybackTime <= 0 {
		return 0
	}

	playbackTime := anime.Ep.Player.PlaybackTime
	if config != nil {
		historyPath := HistoryFile(config.StoragePath)
		provider := CurrentAnimeProviderName(anime)
		if row := LocalFindEpisode(LocalGetAllAnime(historyPath), anime.AnilistId, anime.Ep.Number, provider); row != nil {
			playbackTime = row.Ep.Player.PlaybackTime
		} else {
			// A row for this episode is what a resume needs. Without one the
			// carried position belongs to a different episode, and using it
			// would resume one the viewer has not started.
			playbackTime = 0
		}
		// A position another device left for this episode, when it got further.
		if synced := syncedResumeFor(anime); synced > playbackTime {
			playbackTime = synced
		}
		Log(fmt.Sprintf("cast: resume position for episode %d on %s: %ds", anime.Ep.Number, provider, playbackTime))
	}

	if playbackTime <= 0 {
		return 0
	}
	if formatResumePosition(playbackTime, ConvertSecondsToMinutes(anime.Ep.Duration)) == "" {
		Log(fmt.Sprintf("cast: not resuming at %ds: too early or too near the end to be worth rebuilding the stream", playbackTime))
		return 0
	}
	return float64(playbackTime)
}

package internal

import (
	"fmt"
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
	encoder      cast.Encoder
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

	args := cast.BuildRemuxArgsFrom(s.streamURL, s.referrer, dir, startAt, s.maps, s.headers)
	if s.subtitlePath != "" {
		// Shifted into this generation's own directory, from the original, so a
		// second seek does not shift an already-shifted file. The original's
		// extension is kept: it names the format, and an ASS file written out
		// as subtitles.vtt says the wrong one.
		shifted := filepath.Join(dir, "subtitles"+filepath.Ext(s.subtitlePath))
		if err := cast.ShiftSubtitles(s.subtitlePath, shifted, startAt); err != nil {
			// Subtitles that cannot be shifted are worth less than the seek:
			// burn the original and let them run late rather than refusing to
			// move at all.
			Log(fmt.Sprintf("cast: %v", err))
			shifted = s.subtitlePath
		}
		args = cast.BuildBurnArgsFrom(s.streamURL, s.referrer, shifted, dir, s.encoder, startAt, s.maps, s.headers)
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
		historyPath := filepath.Join(os.ExpandEnv(config.StoragePath), "curd_history.txt")
		provider := CurrentAnimeProviderName(anime)
		if row := LocalFindEpisode(LocalGetAllAnime(historyPath), anime.AnilistId, anime.Ep.Number, provider); row != nil {
			playbackTime = row.Ep.Player.PlaybackTime
		} else {
			// A row for this episode is what a resume needs. Without one the
			// carried position belongs to a different episode, and using it
			// would resume one the viewer has not started.
			playbackTime = 0
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

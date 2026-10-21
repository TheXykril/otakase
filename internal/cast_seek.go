package internal

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

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
	// readSubsFrom starts a new read of the subtitles at a point in the
	// episode, for a seek the current read does not reach.
	readSubsFrom func(from float64) (castSubtitleSource, error)
	// encoder re-encodes the video, whenever it is drawn on or restarted at an
	// offset. Left empty, chooseEncoder picks it the first time it is needed;
	// the cast starts that choice in the background, so the first seek does
	// not wait on the hardware probe.
	// subtitleSize is CastSubtitleSize: how much larger ASS dialogue is drawn,
	// as a percentage.
	subtitleSize  int
	encoder       cast.Encoder
	chooseEncoder func() cast.Encoder
	// maps names the video and audio to play, from cast.SelectTracks; nil
	// leaves the choice to ffmpeg. The same every generation, so a seek keeps
	// the language the cast began in.
	maps []string

	generation int

	// jumps counts the jumps made within a stream already written, so each
	// is handed to the device under a URL of its own. See cast.JumpPath.
	jumps int

	// waiting is told how long a rebuilt stream has kept the viewer waiting,
	// once a second, so a slow one says so rather than looking frozen.
	waiting func(elapsed time.Duration)
	// startTimeout is how long a rebuilt stream may take to write its first
	// segment; zero is castSeekTimeout.
	startTimeout time.Duration

	// launch starts the process. It is a field so a test can stand in for ffmpeg
	// and exercise the generation bookkeeping, which is where a wrong answer is
	// silent: a reused URL leaves the device playing from its own cache.
	launch func(ffmpeg string, args []string, outDir string) (*cast.Remux, error)
}

// castSubtitleSource is what a cast needs of cast.SubtitleReader, so the
// rebuild rule can be tested without ffmpeg.
type castSubtitleSource interface {
	Covered() float64
	From() float64
	Snapshot(dst string) (float64, error)
	FontsDir() string
	Stop()
}

// stream is one generation of the stream: a running ffmpeg, the path the device
// should fetch, and the directory holding both.
type stream struct {
	remux *cast.Remux
	path  string
	dir   string
	// start is where in the episode the stream begins.
	start float64
}

// start begins a generation of the stream at startAt seconds into the episode.
func (s *castStreamSource) start(startAt float64) (stream, error) {
	dir := filepath.Join(s.rootDir, "g"+strconv.Itoa(s.generation))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return stream{}, fmt.Errorf("cast: could not create the stream directory: %w", err)
	}
	// The first stream gets the long wait: a TV waking up, a burn's first
	// segment, a torrent finding its peers. A rebuild happens while the
	// episode is already playing, and the viewer is better told it did not
	// work than left watching the old stream for a minute and a half.
	timeout, waiting := castStartTimeout, func(time.Duration) {}
	if s.generation > 0 {
		timeout = s.startTimeout
		if timeout <= 0 {
			timeout = castSeekTimeout
		}
		if s.waiting != nil {
			waiting = s.waiting
		}
	}
	s.generation++

	subtitlePath, fontsDir := s.subtitlePath, ""
	s.coveredUntil = math.Inf(1)
	s.rereadSubtitles(startAt)
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
		} else if err := cast.ScaleDialogueStyles(shifted, s.subtitleSize); err != nil {
			// Resized in this generation's copy only, never the original, so
			// every generation grows them once.
			Log(fmt.Sprintf("cast: %v", err))
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
	if err := awaitPlaylist(remux, dir, timeout, waiting); err != nil {
		remux.Stop()
		if s.generation > 1 {
			// Nothing will ever fetch from it: the device is still on the
			// stream this one was meant to replace.
			_ = os.RemoveAll(dir)
		}
		return stream{}, err
	}

	return stream{
		remux: remux,
		dir:   dir,
		start: startAt,
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

// rereadSubtitles starts the subtitle read over at startAt when the current
// one cannot serve it: it began after startAt, so the lines there were never
// read, or it is so far behind startAt that catching up would leave the
// viewer without them for minutes.
func (s *castStreamSource) rereadSubtitles(startAt float64) {
	if s.liveSubs == nil || s.readSubsFrom == nil {
		return
	}
	before := startAt < s.liveSubs.From()-1
	behind := startAt > s.liveSubs.Covered()+castSubtitleMinGain*2
	if !before && !behind {
		return
	}
	next, err := s.readSubsFrom(startAt)
	if err != nil {
		Log(fmt.Sprintf("cast: could not read the subtitles again from %.0fs: %v", startAt, err))
		return
	}
	Log(fmt.Sprintf("cast: reading the subtitles again from %.0fs", startAt))
	s.liveSubs.Stop()
	s.liveSubs = next
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

// castJumpLead is how much of the stream has to be written beyond a jump's
// landing point for the jump to be worth it: the device buffers a few seconds
// before it plays, and a jump to the very edge would leave it waiting on
// ffmpeg, which is the wait a jump exists to avoid.
const castJumpLead = 12.0

// jumpPoint finds where in gen a seek to target can land without a new
// stream: the segment boundary nearest the target, in what ffmpeg has already
// written. It reports the segment, and how far into the stream it starts.
//
// minOffset rules out a landing that starts less than that far into the
// stream, other than its very start. See castSeekingSession.clock.
func (s *castStreamSource) jumpPoint(gen stream, target, minOffset float64) (segment int, offset float64, ok bool) {
	if gen.dir == "" || target < gen.start {
		return 0, 0, false
	}
	// Burned subtitles end where the read had reached when the stream was
	// built. Landing past that edge would play on without them until the
	// next rebuild; a rebuild at the target burns what has been read since.
	if s.liveSubs != nil && !math.IsInf(s.coveredUntil, 1) && target >= s.coveredUntil-castSubtitleMargin {
		return 0, 0, false
	}
	segments, finished := cast.ReadPlaylist(filepath.Join(gen.dir, cast.PlaylistName))
	if len(segments) == 0 {
		return 0, 0, false
	}
	want := target - gen.start
	best, bestStart, at := -1, 0.0, 0.0
	for i, seg := range segments {
		if best < 0 || math.Abs(at-want) < math.Abs(bestStart-want) {
			best, bestStart = i, at
		}
		at += seg.Duration
	}
	written := at
	// The nearest boundary may still be a whole segment away from a target
	// past the last one written: that is a seek ffmpeg has not reached.
	if want > written || (!finished && bestStart+castJumpLead > written) {
		return 0, 0, false
	}
	if bestStart > 0 && bestStart < minOffset {
		return 0, 0, false
	}
	return best, bestStart, true
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

	// jump moves the device within the stream already written, if the target
	// is in it, landing on the segment boundary nearest the target. It
	// reports where in the episode the device now starts and how far into
	// its generation that is. nil, or ok false, leaves the seek to restart.
	jump func(target, minOffset float64) (landed, offset float64, ok bool, err error)

	// streamStart is where in the episode the stream the device was last
	// handed begins, which is not always base: see clock.
	streamStart float64

	// clock is how this device counts the position of a jump. A jump hands
	// it a stream that starts partway into a generation, and a receiver
	// may count from where that stream starts or from where the generation
	// does -- from the segment's own timestamps. Which one is learnt from the
	// first jump that can tell them apart, and kept for the cast.
	clock castJumpClock

	// settling is set for the first moments after the device is handed a
	// stream: until a reading belongs to the new stream, the position is
	// held at where it starts. See settle.
	settling     bool
	settleSince  time.Time
	settleOffset float64
}

// castJumpClock is how a device counts the position of a jump.
type castJumpClock int

const (
	castClockUnknown castJumpClock = iota
	// castClockFromLoad counts from the start of the stream it was handed.
	castClockFromLoad
	// castClockFromGeneration counts from the start of the generation.
	castClockFromGeneration
)

// castJumpLearnOffset is the least a jump must land into its generation
// while the device's clock is not yet known. Below it the two ways of
// counting are too close to tell apart from a reading or two -- a device
// still buffering one counts as little as one playing the other -- so such a
// jump is left to a rebuild, whose position means the same either way.
const castJumpLearnOffset = 30.0

// castSettleSlack is how far a first reading may run ahead of the time since
// the load and still be the new stream's: the receiver's own clock, a
// muxer's start offset and a poll's latency between them.
const castSettleSlack = 6.0

// castSettleEarly is how far below a generation-counted jump's own offset a
// reading may sit: the segment's timestamps start a moment off its boundary.
const castSettleEarly = 3.0

// castSettleNow is the clock the settling reads, so a test can move it on
// rather than wait.
var castSettleNow = time.Now

// castSettleLimit is how long the position is held waiting for a reading
// from the new stream before whatever the device says is believed again.
const castSettleLimit = 20 * time.Second

// beginSettle starts holding the position for a newly loaded stream that
// begins offset seconds into its generation. Called with mu held.
func (s *castSeekingSession) beginSettle(offset float64) {
	s.settling = true
	s.settleSince = castSettleNow()
	s.settleOffset = offset
}

// settle decides what a reading taken just after a load means, adjusting it
// to count from base. Called with mu held.
//
// A reading still describing the stream before -- the receiver answers for
// a moment or two about what it was told to leave -- would put the panel,
// the skips and the progress written to the trackers wherever that stream
// had got to. So until a reading fits the new stream, the position is held at
// where it starts.
func (s *castSeekingSession) settle(progress *cast.Progress) {
	if progress.Settling {
		// The device says itself that this is the old stream.
		progress.Position, progress.Idle = s.streamStart-s.base, false
		return
	}
	if !s.settling || progress.Idle {
		return
	}
	elapsed := castSettleNow().Sub(s.settleSince).Seconds()
	if elapsed > castSettleLimit.Seconds() {
		Log(fmt.Sprintf("cast: no reading fit the new stream in %s, believing the device again", castSettleLimit))
		s.settling = false
		return
	}
	reading, offset := progress.Position, s.settleOffset
	fromLoad := reading >= 0 && reading <= elapsed+castSettleSlack
	fromGeneration := offset > 0 && reading >= offset-castSettleEarly && reading <= offset+elapsed+castSettleSlack

	hold := func() { progress.Position = s.streamStart - s.base }
	switch {
	case reading < 0.5:
		// Nothing played yet: buffering, where either clock reads zero.
		hold()
		return
	case offset == 0 && fromLoad:
		// A stream from the start of its generation: both clocks agree.
	case s.clock == castClockFromLoad && fromLoad, s.clock == castClockFromGeneration && fromGeneration:
	case s.clock == castClockUnknown && offset >= castJumpLearnOffset && fromGeneration:
		s.clock = castClockFromGeneration
		s.base = s.streamStart - offset
		Log(fmt.Sprintf("cast: the device counts a jump from the start of its stream's generation (read %.1f, %.1f in)", reading, offset))
	case s.clock == castClockUnknown && offset >= castJumpLearnOffset && fromLoad:
		s.clock = castClockFromLoad
		Log(fmt.Sprintf("cast: the device counts a jump from where it was handed the stream (read %.1f, %.1f in)", reading, offset))
	default:
		Log(fmt.Sprintf("cast: ignoring a reading of %.1f, %.1fs after the load: still the stream before", reading, elapsed))
		hold()
		return
	}
	s.settling = false
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
	if err == nil {
		s.settle(&progress)
	}
	base, duration, streamStart := s.base, s.duration, s.streamStart
	s.mu.Unlock()

	progress.Position += base
	if duration > 0 {
		progress.Duration = duration
	} else if progress.Duration > 0 {
		// The device's length is the stream's, which starts partway in.
		progress.Duration += streamStart
	}
	progress.Settling = false
	return progress, err
}

// SeekToTime rebuilds the stream at seconds instead of asking the device, which
// would accept the request and ignore it.
func (s *castSeekingSession) SeekToTime(seconds float64) error {
	seconds = s.reachable(seconds)
	if err := s.restart(seconds); err != nil {
		return err
	}

	s.mu.Lock()
	s.base, s.streamStart = seconds, seconds
	s.beginSettle(0)
	s.mu.Unlock()
	return nil
}

// reachable limits a seek target to where a stream can start.
func (s *castSeekingSession) reachable(seconds float64) float64 {
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
	return seconds
}

// seekNear moves the episode to about seconds, reporting where it landed: a
// jump within the stream already written when the target is in it, which
// lands on the nearest segment boundary -- a few seconds either side -- and
// takes only the device's own reload; otherwise a rebuild at the target
// itself, as SeekToTime.
//
// Only for a viewer's seek. A skip has to land at or past the end of what it
// skips, or the next poll reads the position as still inside it, and a
// rebuild for more subtitles needs a new stream to burn them into.
func (s *castSeekingSession) seekNear(seconds float64) (float64, error) {
	seconds = s.reachable(seconds)
	if s.jump != nil {
		s.mu.Lock()
		clock := s.clock
		s.mu.Unlock()
		minOffset := 0.0
		if clock == castClockUnknown {
			minOffset = castJumpLearnOffset
		}

		landed, offset, ok, err := s.jump(seconds, minOffset)
		if err != nil {
			return 0, err
		}
		if ok {
			s.mu.Lock()
			s.streamStart, s.base = landed, landed
			if s.clock == castClockFromGeneration {
				s.base = landed - offset
			}
			s.beginSettle(offset)
			s.mu.Unlock()
			return landed, nil
		}
	}
	return seconds, s.SeekToTime(seconds)
}

func (s *castSeekingSession) Pause() error              { return s.inner.Pause() }
func (s *castSeekingSession) Unpause() error            { return s.inner.Unpause() }
func (s *castSeekingSession) SetVolume(l float64) error { return s.inner.SetVolume(l) }
func (s *castSeekingSession) Volume() float64           { return s.inner.Volume() }

// castSeekTimeout is how long a rebuild at a seek target may take to write its
// first segment before the seek is given up, and the episode left playing
// where it was. Well past the few seconds a healthy rebuild takes, burn
// included; short of castStartTimeout, which allows for a TV waking up.
const castSeekTimeout = 30 * time.Second

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
//
// waiting is told every second how long the wait has lasted, so a slow one can
// be shown as slow rather than frozen.
func awaitPlaylist(remux *cast.Remux, dir string, timeout time.Duration, waiting func(time.Duration)) error {
	written := make(chan error, 1)
	go func() {
		written <- cast.WaitForPlaylist(dir, timeout)
	}()

	began := time.Now()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
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
			// ffmpeg exited without complaint. The playlist was written either
			// way, so the wait's own answer is the one that counts.
			return <-written
		case <-tick.C:
			if waiting != nil {
				waiting(time.Since(began))
			}
		}
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

package internal

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thexykril/otakase/internal/cast"
)

// Casting reuses the resolved skip times, but only the ones the user asked
// for: SkipOp and SkipEd are separate settings and a cast must honour both.
func TestCastSpansFollowTheSkipSettings(t *testing.T) {
	times := SkipTimes{Op: Skip{Start: 90, End: 180}, Ed: Skip{Start: 1300, End: 1390}}

	both := castSpansFor(times, &Config{SkipOp: true, SkipEd: true})
	if len(both) != 2 {
		t.Fatalf("with both enabled, got %d spans: %+v", len(both), both)
	}

	opOnly := castSpansFor(times, &Config{SkipOp: true})
	if len(opOnly) != 1 || opOnly[0].Start != 90 {
		t.Errorf("with only SkipOp, got %+v", opOnly)
	}

	if spans := castSpansFor(times, &Config{}); len(spans) != 0 {
		t.Errorf("with neither enabled, got %+v", spans)
	}
}

// A span otakase never resolved is zero, and zero means "not known". Offering
// it would seek the device to the start of the episode.
func TestCastSpansDropUnresolvedTimes(t *testing.T) {
	times := SkipTimes{Op: Skip{Start: 0, End: 0}, Ed: Skip{Start: 1300, End: 1390}}

	spans := castSpansFor(times, &Config{SkipOp: true, SkipEd: true})
	if len(spans) != 1 || spans[0] != (cast.Span{Start: 1300, End: 1390}) {
		t.Errorf("an unresolved opening was offered as a skip: %+v", spans)
	}
}

// --- watchCast: fakes and table tests ---
//
// watchCast is where every bug found on this branch has lived, and until now
// it had no coverage at all -- it talks to a real device and a real ffmpeg
// process, neither of which exists in a test. The castSession/castServer/
// castRemux interfaces (internal/cast_playback.go) narrow what it actually
// needs down to something a fake can satisfy.

// fakeStep is one poll's worth of scripted device status.
type fakeStep struct {
	progress cast.Progress
	err      error
}

// fakeSession scripts a sequence of Progress() results, one per call,
// repeating the last one if watchCast polls past the end of the script. A
// hook runs after each call's result is decided but before it is returned, so
// a test can flip the remux or server fake at an exact poll -- deterministic
// without a fake clock.
type fakeSession struct {
	mu     sync.Mutex
	steps  []fakeStep
	calls  int
	seeks  []float64
	paused bool
	volume float64
	onCall func(call int)
}

func (s *fakeSession) Pause() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = true
	return nil
}

func (s *fakeSession) Unpause() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = false
	return nil
}

func (s *fakeSession) SetVolume(level float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if level < 0 {
		level = 0
	}
	if level > 1 {
		level = 1
	}
	s.volume = level
	return nil
}

func (s *fakeSession) Volume() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.volume
}

func (s *fakeSession) Progress() (cast.Progress, error) {
	s.mu.Lock()
	call := s.calls
	if call >= len(s.steps) {
		call = len(s.steps) - 1
	}
	s.calls++
	step := s.steps[call]
	s.mu.Unlock()

	if s.onCall != nil {
		s.onCall(call)
	}
	return step.progress, step.err
}

func (s *fakeSession) SeekToTime(seconds float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seeks = append(s.seeks, seconds)
	return nil
}

// fakeRemux is a castRemux double whose error and done state a test flips
// mid-run, typically from a fakeSession's onCall hook.
type fakeRemux struct {
	mu        sync.Mutex
	err       error
	done      chan struct{}
	closeOnce sync.Once
}

func newFakeRemux() *fakeRemux {
	return &fakeRemux{done: make(chan struct{})}
}

func (r *fakeRemux) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

func (r *fakeRemux) Done() <-chan struct{} {
	return r.done
}

// fail marks the remux as having ended with err. Safe to call more than once,
// and safe to call after succeed(): the error still takes effect even though
// the channel was already closed.
func (r *fakeRemux) fail(err error) {
	r.mu.Lock()
	r.err = err
	r.mu.Unlock()
	r.closeOnce.Do(func() { close(r.done) })
}

// succeed marks the remux as having ended cleanly.
func (r *fakeRemux) succeed() {
	r.closeOnce.Do(func() { close(r.done) })
}

// fakeServer is a castServer double.
type fakeServer struct {
	mu           sync.Mutex
	err          error
	neverFetched bool
}

func (s *fakeServer) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// fetched defaults true: most tests are about a device that did reach us and
// then misbehaved, and the one test that cares about the firewall path sets it
// false explicitly.
func (s *fakeServer) Fetched() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.neverFetched
}

func (s *fakeServer) URL(name string) string { return "http://192.168.0.115:8010/" + name }

// withFastCastTimings shrinks the timing watchCast polls against so its table
// tests run in milliseconds instead of the real 30-second/2-minute windows,
// and restores the originals on cleanup so no other test in the package
// observes the change.
func withFastCastTimings(t *testing.T) {
	t.Helper()
	prevPoll, prevGrace, prevStall := castPollInterval, castStartupGrace, castStallTimeout
	castPollInterval = 2 * time.Millisecond
	castStartupGrace = time.Millisecond
	// Wide enough that a test doing real file I/O between two polls (writing
	// curd_history.txt on a mark) cannot trip the bound by accident, and still
	// short enough that the stall test finishes in well under its own timeout.
	castStallTimeout = 500 * time.Millisecond
	t.Cleanup(func() {
		castPollInterval, castStartupGrace, castStallTimeout = prevPoll, prevGrace, prevStall
	})
}

func testCastAnime() *Anime {
	return &Anime{
		AnilistId:    424242,
		ProviderId:   "test-provider-id",
		ProviderName: "test-provider",
		Title:        AnimeTitle{English: "Test Show"},
		Ep:           Episode{Number: 5},
	}
}

func testCastConfig(t *testing.T) *Config {
	t.Helper()
	return &Config{
		StoragePath:              t.TempDir(),
		PercentageToMarkComplete: 85,
	}
}

// animeMarked reports whether the local history has an entry for anilistID.
func animeMarked(entries []Anime, anilistID int) bool {
	for _, e := range entries {
		if e.AnilistId == anilistID {
			return true
		}
	}
	return false
}

// Critical 2: a remux that fails partway through must not mark the episode
// watched against a truncated live-edge duration, and must report the remux
// failure rather than a clean finish. remuxDone() alone would have let a
// remux that died two minutes into a 24-minute episode mark it watched at
// 8:30 and push episode-complete to a tracker; remuxSucceeded() is the fix.
func TestWatchCastRemuxFailureDoesNotMarkWatched(t *testing.T) {
	withFastCastTimings(t)

	remux := newFakeRemux()
	server := &fakeServer{}
	failure := errors.New("cast: ffmpeg failed: 403 on segment 42")

	session := &fakeSession{
		steps: []fakeStep{
			{progress: cast.Progress{Position: 100, Duration: 600}}, // playing; started latches
			{progress: cast.Progress{Position: 550, Duration: 600}}, // 91.6%: would mark if remux were healthy
			{progress: cast.Progress{Idle: true}},                   // device runs out of segments
		},
	}
	session.onCall = func(call int) {
		if call == 1 {
			remux.fail(failure)
		}
	}

	anime := testCastAnime()
	anime.Ep.Duration = 1440 // a real duration; must survive untouched, not the live edge
	config := testCastConfig(t)

	var err error
	out := captureStdout(t, func() {
		err = watchCast(config, anime, session, server, remux, cast.Device{Name: "Living Room"}, false)
	})

	if !errors.Is(err, failure) {
		t.Fatalf("expected the remux failure, got %v", err)
	}
	if anime.Ep.Duration != 1440 {
		t.Errorf("duration was overwritten by a truncated live edge: got %d", anime.Ep.Duration)
	}
	// The history entry savePartial writes here (Important 4: a resume point
	// for the next launch) is correct and expected -- it is not itself
	// evidence of a false mark. The signal that matters is this message,
	// which a real completion would have printed instead of the returned
	// error.
	if strings.Contains(out, "marked as watched") {
		t.Error("a failed remux reported the episode as marked watched")
	}
}

// BUFFERING is not idle, and is not evidence of playback either: a device
// that accepted the load but cannot reach this machine sits there for as
// long as the connection takes to fail. started must not latch on it, so the
// idle that eventually follows reports the startup failure -- not "Playback
// finished." on a black screen.
func TestWatchCastBufferingDoesNotLatchStarted(t *testing.T) {
	withFastCastTimings(t)

	remux := newFakeRemux()
	server := &fakeServer{}
	session := &fakeSession{
		steps: []fakeStep{
			{progress: cast.Progress{Position: 0, Idle: false}}, // BUFFERING
			{progress: cast.Progress{Position: 0, Idle: false}}, // still BUFFERING
			{progress: cast.Progress{Idle: true}},               // gives up
		},
	}

	anime := testCastAnime()
	config := testCastConfig(t)

	err := watchCast(config, anime, session, server, remux, cast.Device{Name: "Guest VLAN TV"}, false)

	if err == nil {
		t.Fatal(`expected a startup-failure error, got nil (would print "Playback finished.")`)
	}
	if !strings.Contains(err.Error(), "never started playing") {
		t.Errorf("expected a startup-failure message, got: %v", err)
	}
}

// A remembered remux failure outranks losing the device: if ffmpeg died and
// the device then stops answering, ffmpeg is the cause worth reporting, and
// returning nil here would bury it under a silent exit 0.
func TestWatchCastRemuxFailureOutranksLostDevice(t *testing.T) {
	withFastCastTimings(t)

	remux := newFakeRemux()
	server := &fakeServer{}
	failure := errors.New("cast: ffmpeg failed: connection reset")
	lostContact := errors.New("cast: could not read the device status: EOF")

	session := &fakeSession{
		steps: []fakeStep{
			{progress: cast.Progress{Position: 50, Duration: 600}}, // started latches
			{err: lostContact}, // device stops answering
		},
	}
	session.onCall = func(call int) {
		if call == 0 {
			remux.fail(failure)
		}
	}

	anime := testCastAnime()
	config := testCastConfig(t)

	err := watchCast(config, anime, session, server, remux, cast.Device{Name: "Living Room"}, false)

	if !errors.Is(err, failure) {
		t.Fatalf("expected the remembered remux failure to outrank the lost device, got %v", err)
	}
}

// Important 4: a cast that ends without ever marking the episode complete
// must still offer a resume point next time. The mpv path gets this from its
// own playback loop; a cast has no loop left once watchCast returns.
func TestWatchCastSavesPartialProgressOnAnUnmarkedExit(t *testing.T) {
	withFastCastTimings(t)

	remux := newFakeRemux()
	server := &fakeServer{}
	lostContact := errors.New("cast: could not read the device status: EOF")

	session := &fakeSession{
		steps: []fakeStep{
			{progress: cast.Progress{Position: 200, Duration: 1440}}, // well under the mark threshold
			{err: lostContact},
		},
	}

	anime := testCastAnime()
	config := testCastConfig(t)

	// Losing the device mid-episode is a failure, not a clean finish: the
	// caller must not advance to the next episode on it, only resume from
	// where it left off.
	if err := watchCast(config, anime, session, server, remux, cast.Device{Name: "Living Room"}, false); err == nil {
		t.Fatal("expected an error on an unremembered lost-device exit, got nil")
	}

	entries := LocalGetAllAnime(filepath.Join(config.StoragePath, "curd_history.txt"))
	saved := false
	for _, e := range entries {
		if e.AnilistId == anime.AnilistId && e.Ep.Player.PlaybackTime == 200 {
			saved = true
		}
	}
	if !saved {
		t.Error("expected a resume point at position 200 to have been saved")
	}
}

// An episode already marked watched is one the viewer saw through: a remux
// failure discovered afterwards, in the episode's tail, is cosmetic by then
// and must be reported as a clean finish -- not "Casting failed" about an
// episode the viewer just finished watching.
func TestWatchCastMarkedSuppressesALaterRemuxFailure(t *testing.T) {
	withFastCastTimings(t)

	remux := newFakeRemux()
	remux.succeed() // finished cleanly, well before playback catches up to it
	server := &fakeServer{}
	failure := errors.New("cast: ffmpeg failed: truncated tail")

	session := &fakeSession{
		steps: []fakeStep{
			{progress: cast.Progress{Position: 530, Duration: 600}}, // 88.3%: marks
			{progress: cast.Progress{Position: 530, Duration: 600}}, // steady; failure discovered here
			{progress: cast.Progress{Idle: true}},                   // finishes
		},
	}
	session.onCall = func(call int) {
		if call == 1 {
			remux.fail(failure)
		}
	}

	anime := testCastAnime()
	config := testCastConfig(t)

	err := watchCast(config, anime, session, server, remux, cast.Device{Name: "Living Room"}, false)

	if err != nil {
		t.Fatalf("a failure discovered after the episode was marked watched should read as a clean finish, got %v", err)
	}
	entries := LocalGetAllAnime(filepath.Join(config.StoragePath, "curd_history.txt"))
	if !animeMarked(entries, anime.AnilistId) {
		t.Error("expected the episode to have been marked watched before the remux failed")
	}
}

// Important 3 (part 2): a frozen position, with the device never reporting
// idle, must not be polled forever holding ffmpeg, the scratch directory and
// the HTTP server open. The vendored library never clears a.application when
// the receiver reports no applications at all, so idle alone cannot be
// relied on to end this -- run in a goroutine with its own timeout so a
// regression here fails this test in two seconds instead of hanging it.
func TestWatchCastStallBoundFiresWhenPositionStopsAdvancing(t *testing.T) {
	withFastCastTimings(t)

	remux := newFakeRemux()
	server := &fakeServer{}
	session := &fakeSession{
		steps: []fakeStep{
			{progress: cast.Progress{Position: 30, Duration: 600}}, // starts
			{progress: cast.Progress{Position: 30, Duration: 600}}, // frozen from here on
		},
	}

	anime := testCastAnime()
	config := testCastConfig(t)

	// The frozen position sits inside an enabled skip span, which is the case
	// that actually holds this fix closed: NextSkip returns true for as long as
	// the position is within a span, so a stall-clock reset in the skip branch
	// would restart the bound every poll and it could never fire. A freeze
	// during the opening or the ending is also when a viewer is most likely to
	// have stopped the cast from the device.
	config.SkipOp = true
	anime.Ep.SkipTimes = SkipTimes{Op: Skip{Start: 20, End: 100}}

	done := make(chan error, 1)
	go func() {
		done <- watchCast(config, anime, session, server, remux, cast.Device{Name: "Living Room"}, false)
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a stall error, got nil")
		}
		if !strings.Contains(err.Error(), "stopped reporting progress") {
			t.Errorf("expected a stall message, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchCast did not return: the stall bound did not fire")
	}
}

// Pausing must suspend the stall bound. The bound exists to catch a position
// that has stopped advancing, which is exactly what a paused device looks
// like -- without this, pausing for two minutes kills the episode.
func TestWatchCastPauseSuspendsTheStallBound(t *testing.T) {
	withFastCastTimings(t)

	commands := make(chan castCommand, 4)
	commands <- castCmdPauseToggle

	remux := newFakeRemux()
	server := &fakeServer{}
	session := &fakeSession{
		steps: []fakeStep{
			{progress: cast.Progress{Position: 30, Duration: 600}},
			{progress: cast.Progress{Position: 30, Duration: 600}}, // frozen: paused
		},
	}

	anime := testCastAnime()
	config := testCastConfig(t)

	done := make(chan error, 1)
	go func() {
		done <- watchCastWithControls(config, anime, session, server, remux, cast.Device{Name: "Living Room"}, commands, false)
	}()

	select {
	case err := <-done:
		t.Fatalf("watchCast returned while paused: %v", err)
	case <-time.After(700 * time.Millisecond):
		// castStallTimeout is 500ms here and the position has not moved;
		// still running is the pass.
	}

	session.mu.Lock()
	paused := session.paused
	session.mu.Unlock()
	if !paused {
		t.Error("the device was never paused")
	}

	commands <- castCmdStop
	<-done
}

// q ends the cast without marking an episode the viewer did not finish.
func TestWatchCastStopCommandEndsWithoutMarking(t *testing.T) {
	withFastCastTimings(t)

	commands := make(chan castCommand, 1)
	commands <- castCmdStop

	remux := newFakeRemux()
	server := &fakeServer{}
	session := &fakeSession{steps: []fakeStep{{progress: cast.Progress{Position: 30, Duration: 600}}}}

	anime := testCastAnime()
	config := testCastConfig(t)

	captureStdout(t, func() {
		if err := watchCastWithControls(config, anime, session, server, remux, cast.Device{Name: "Living Room"}, commands, false); !errors.Is(err, ErrCastStopped) {
			t.Fatalf("stopping should report ErrCastStopped, got: %v", err)
		}
	})

	history := LocalGetAllAnime(filepath.Join(config.StoragePath, "curd_history.txt"))
	for _, entry := range history {
		if entry.AnilistId == anime.AnilistId && entry.Ep.Player.PlaybackTime == 0 {
			t.Error("stopping wrote a completed entry rather than a partial one")
		}
	}
}

// Review Focus 4. Seeking back in the first seconds must clamp at zero rather
// than asking the device for a negative position.
func TestWatchCastSeekBackClampsAtZero(t *testing.T) {
	withFastCastTimings(t)

	commands := make(chan castCommand, 4)
	commands <- castCmdSeekBack

	remux := newFakeRemux()
	server := &fakeServer{}
	session := &fakeSession{steps: []fakeStep{{progress: cast.Progress{Position: 3, Duration: 600}}}}

	anime := testCastAnime()
	config := testCastConfig(t)

	done := make(chan error, 1)
	captureStdout(t, func() {
		go func() {
			done <- watchCastWithControls(config, anime, session, server, remux, cast.Device{Name: "Living Room"}, commands, false)
		}()
		time.Sleep(100 * time.Millisecond)
		commands <- castCmdStop
		<-done
	})

	session.mu.Lock()
	seeks := append([]float64(nil), session.seeks...)
	session.mu.Unlock()

	if len(seeks) == 0 {
		t.Fatal("no seek was issued")
	}
	for _, s := range seeks {
		if s < 0 {
			t.Errorf("seeked to %v, which is before the start of the episode", s)
		}
	}
}

// Review Focus 3, at the loop level: holding the volume key settles at 1.0.
func TestWatchCastVolumeClampsAtTheTop(t *testing.T) {
	withFastCastTimings(t)

	commands := make(chan castCommand, 64)
	for i := 0; i < 30; i++ {
		commands <- castCmdVolumeUp
	}

	remux := newFakeRemux()
	server := &fakeServer{}
	session := &fakeSession{
		volume: 0.9,
		steps:  []fakeStep{{progress: cast.Progress{Position: 30, Duration: 600}}},
	}

	anime := testCastAnime()
	config := testCastConfig(t)

	done := make(chan error, 1)
	captureStdout(t, func() {
		go func() {
			done <- watchCastWithControls(config, anime, session, server, remux, cast.Device{Name: "Living Room"}, commands, false)
		}()
		time.Sleep(200 * time.Millisecond)
		commands <- castCmdStop
		<-done
	})

	if got := session.Volume(); got > 1 {
		t.Errorf("volume reached %v, above the receiver's maximum of 1", got)
	}
}

// Repeated presses must each act on where the last one left the episode.
// applyCastCommand works from lastPosition, which only the poll branch
// refreshed -- so a viewer pressing right six times to skip a recap sent six
// seeks to the same timestamp and the episode moved ten seconds once.
func TestWatchCastRepeatedSeeksCompound(t *testing.T) {
	withFastCastTimings(t)

	commands := make(chan castCommand, 8)
	commands <- castCmdSeekForward
	commands <- castCmdSeekForward
	commands <- castCmdSeekForward

	remux := newFakeRemux()
	server := &fakeServer{}
	// The device never reports the new position, which is the point: the
	// seeks must compound from each other, not from the last poll.
	session := &fakeSession{steps: []fakeStep{{progress: cast.Progress{Position: 100, Duration: 600}}}}

	anime := testCastAnime()
	config := testCastConfig(t)

	done := make(chan error, 1)
	captureStdout(t, func() {
		go func() {
			done <- watchCastWithControls(config, anime, session, server, remux, cast.Device{Name: "Living Room"}, commands, false)
		}()
		time.Sleep(150 * time.Millisecond)
		commands <- castCmdStop
		<-done
	})

	session.mu.Lock()
	seeks := append([]float64(nil), session.seeks...)
	session.mu.Unlock()

	if len(seeks) < 3 {
		t.Fatalf("expected three seeks, got %v", seeks)
	}
	if seeks[0] == seeks[1] || seeks[1] == seeks[2] {
		t.Errorf("repeated seeks did not compound: %v -- each press acted on a stale position", seeks)
	}
}

// The receiver resumes playback on any seek -- SeekToTime sends
// ResumeState "PLAYBACK_START" -- so a seek while paused starts the TV again.
// If watchCast keeps thinking it is paused, the status line lies, space needs
// two presses to pause again, and the stall bound is gated off !paused for the
// rest of the episode: a device that stops answering then holds ffmpeg, the
// scratch directory and the server open forever.
//
// The observable difference is the stall bound. With a frozen position, a
// watchCast that knows the seek resumed playback gives up after
// castStallTimeout; one that still believes it is paused never does.
func TestWatchCastSeekingWhilePausedClearsPaused(t *testing.T) {
	withFastCastTimings(t)

	commands := make(chan castCommand, 8)
	commands <- castCmdPauseToggle
	commands <- castCmdSeekForward

	remux := newFakeRemux()
	server := &fakeServer{}
	session := &fakeSession{steps: []fakeStep{{progress: cast.Progress{Position: 100, Duration: 600}}}}

	anime := testCastAnime()
	config := testCastConfig(t)

	done := make(chan error, 1)
	captureStdout(t, func() {
		go func() {
			done <- watchCastWithControls(config, anime, session, server, remux, cast.Device{Name: "Living Room"}, commands, false)
		}()

		select {
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), "stopped reporting progress") {
				t.Errorf("expected the stall bound to fire after the seek resumed playback, got %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("the stall bound never fired: watchCast still believes the device is paused after a seek resumed it")
			commands <- castCmdStop
			<-done
		}
	})
}

// Closing the spawned window is the intended way to stop a rofi cast, and the
// README and the hardware checklist both promise it saves your position. That
// path is a signal, so it runs the exit cleanups and never returns through
// watchCast -- the position has to be written from a cleanup, not from the
// function's own exits.
func TestWatchCastSavesPartialProgressOnASignal(t *testing.T) {
	withFastCastTimings(t)
	resetExitCleanupsForTest()
	t.Cleanup(resetExitCleanupsForTest)

	commands := make(chan castCommand, 4)
	remux := newFakeRemux()
	server := &fakeServer{}
	session := &fakeSession{steps: []fakeStep{{progress: cast.Progress{Position: 321, Duration: 1440}}}}

	anime := testCastAnime()
	config := testCastConfig(t)

	done := make(chan error, 1)
	var history []Anime
	captureStdout(t, func() {
		go func() {
			done <- watchCastWithControls(config, anime, session, server, remux, cast.Device{Name: "Living Room"}, commands, false)
		}()
		time.Sleep(120 * time.Millisecond) // let it poll a position

		// What the signal handler does, without exiting the test process.
		runExitCleanups()

		// Read before stopping: watchCast's own stop path also saves, so
		// checking afterwards would pass whether or not the cleanup did
		// anything.
		history = LocalGetAllAnime(filepath.Join(config.StoragePath, "curd_history.txt"))

		commands <- castCmdStop
		<-done
	})

	if !animeMarked(history, anime.AnilistId) {
		t.Fatal("closing the window wrote no history row at all")
	}
	for _, entry := range history {
		if entry.AnilistId != anime.AnilistId {
			continue
		}
		if entry.Ep.Player.PlaybackTime == 0 {
			t.Errorf("the position was not saved: row has PlaybackTime 0, wanted about 321")
		}
	}
}

// A device that never fetched anything did not refuse the stream -- nothing
// reached this machine. Those need different messages, because only one of
// them is fixed by a firewall rule.
func TestWatchCastDistinguishesNeverFetchedFromRefused(t *testing.T) {
	withFastCastTimings(t)

	anime := testCastAnime()
	config := testCastConfig(t)
	config.CastPort = 8010

	for _, tc := range []struct {
		name         string
		neverFetched bool
		want         string
	}{
		{"nothing reached us", true, "never fetched the stream"},
		{"fetched but would not play", false, "would not play it"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := &fakeSession{steps: []fakeStep{{progress: cast.Progress{Idle: true}}}}
			var err error
			captureStdout(t, func() {
				err = watchCast(config, anime, session, &fakeServer{neverFetched: tc.neverFetched},
					newFakeRemux(), cast.Device{Name: "Living Room"}, false)
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("expected a message containing %q, got %v", tc.want, err)
			}
		})
	}
}

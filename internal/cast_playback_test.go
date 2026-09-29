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
	mu    sync.Mutex
	steps []fakeStep
	calls int
	seeks []float64
	// seekErr is what every SeekToTime returns, for a rebuild that fails;
	// seekErrs fails only the listed targets.
	seekErr  error
	seekErrs map[float64]error
	paused   bool
	volume   float64
	onCall   func(call int)
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
	if err, ok := s.seekErrs[seconds]; ok {
		return err
	}
	return s.seekErr
}

// slowProgressSession is a castSession whose Progress call sleeps for a
// caller-controlled duration -- it exists only to test progressWithin's
// bound, not the wider watchCast loop, which is why it does not join
// fakeSession's scripted-steps machinery.
type slowProgressSession struct {
	delay time.Duration
}

func (s *slowProgressSession) Progress() (cast.Progress, error) {
	time.Sleep(s.delay)
	return cast.Progress{Position: 42}, nil
}
func (s *slowProgressSession) SeekToTime(seconds float64) error { return nil }
func (s *slowProgressSession) Pause() error                     { return nil }
func (s *slowProgressSession) Unpause() error                   { return nil }
func (s *slowProgressSession) SetVolume(level float64) error    { return nil }
func (s *slowProgressSession) Volume() float64                  { return 0 }

// A device that stops acknowledging traffic without closing the connection
// must not be allowed to hold the poll loop hostage for however long the OS
// takes to notice -- see castProgressTimeout's doc comment for why the
// vendored library cannot be trusted to bound this on its own.
func TestProgressWithinGivesUpWhenTheDeviceNeverAnswers(t *testing.T) {
	session := &slowProgressSession{delay: 200 * time.Millisecond}

	start := time.Now()
	_, err := progressWithin(session, 20*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed >= 200*time.Millisecond {
		t.Fatalf("progressWithin waited for the slow call (%s) instead of giving up at its own bound", elapsed)
	}
}

func TestProgressWithinReturnsAFastResult(t *testing.T) {
	session := &fakeSession{steps: []fakeStep{{progress: cast.Progress{Position: 7}}}}

	progress, err := progressWithin(session, time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if progress.Position != 7 {
		t.Fatalf("got position %v, want 7", progress.Position)
	}
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
	prevPoll, prevGrace, prevStall, prevEnd := castPollInterval, castStartupGrace, castStallTimeout, castEndStallTimeout
	castPollInterval = 2 * time.Millisecond
	castStartupGrace = time.Millisecond
	// Wide enough that a test doing real file I/O between two polls (writing
	// curd_history.txt on a mark) cannot trip the bound by accident, and still
	// short enough that the stall test finishes in well under its own timeout.
	castStallTimeout = 500 * time.Millisecond
	castEndStallTimeout = 50 * time.Millisecond
	t.Cleanup(func() {
		castPollInterval, castStartupGrace, castStallTimeout, castEndStallTimeout = prevPoll, prevGrace, prevStall, prevEnd
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

// Confirmed on hardware: some receivers stop answering status polls right at
// the true end of a stream they have already fully received. When that
// happens after the episode is both marked watched and fully delivered, it
// must read as a finish, not a failure -- the viewer should get the normal
// next-episode countdown, not an error.
func TestWatchCastTreatsLostContactAsFinishedOnceMarkedAndFullyDelivered(t *testing.T) {
	withFastCastTimings(t)

	remux := newFakeRemux()
	server := &fakeServer{}
	lostContact := errors.New("cast: device did not respond to a status check within 10s")

	session := &fakeSession{
		steps: []fakeStep{
			// Crosses the 85% mark-complete threshold while the remux has
			// already finished successfully.
			{progress: cast.Progress{Position: 1230, Duration: 1440}},
			{err: lostContact},
		},
	}
	session.onCall = func(call int) {
		if call == 0 {
			remux.succeed()
		}
	}

	anime := testCastAnime()
	config := testCastConfig(t)

	if err := watchCast(config, anime, session, server, remux, cast.Device{Name: "Office TV"}, false); err != nil {
		t.Fatalf("expected a finish, got an error: %v", err)
	}
}

// The same lost-contact error before the episode is marked (or before the
// remux has finished delivering it) must still fail hard: nothing here
// proves the episode is actually done.
func TestWatchCastStillFailsOnLostContactBeforeMarked(t *testing.T) {
	withFastCastTimings(t)

	remux := newFakeRemux()
	server := &fakeServer{}
	lostContact := errors.New("cast: device did not respond to a status check within 10s")

	session := &fakeSession{
		steps: []fakeStep{
			{progress: cast.Progress{Position: 200, Duration: 1440}}, // well under the mark threshold
			{err: lostContact},
		},
	}
	session.onCall = func(call int) {
		if call == 0 {
			remux.succeed()
		}
	}

	anime := testCastAnime()
	config := testCastConfig(t)

	err := watchCast(config, anime, session, server, remux, cast.Device{Name: "Office TV"}, false)
	if err == nil {
		t.Fatal("expected an error: the episode was never marked watched")
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
// Seeking a cast episode restarts ffmpeg at the target, so a burst of presses
// must arrive as one jump: one restart, to the destination all the presses add up
// to. The bug this still guards is the original one -- presses acting on a stale
// position, so that six presses moved the episode ten seconds once.
func TestWatchCastRepeatedSeeksCompoundIntoOneJump(t *testing.T) {
	withFastCastTimings(t)
	withFastSeekDebounce(t)

	commands := make(chan castCommand, 8)
	commands <- castCmdSeekForward
	commands <- castCmdSeekForward
	commands <- castCmdSeekForward

	remux := newFakeRemux()
	server := &fakeServer{}
	// The device never reports the new position, which is the point: the
	// destination must come from the presses, not from the last poll.
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

	if len(seeks) != 1 {
		t.Fatalf("a burst of three presses produced %d seeks (%v), want one", len(seeks), seeks)
	}
	// Three steps of ten seconds. A seek to 10 would be the original bug: every
	// press acting on the same stale position, so the burst moved ten seconds.
	if want := 3 * castSeekStep; seeks[0] != want {
		t.Fatalf("three presses sought to %.0f, want %.0f", seeks[0], want)
	}
}

// A viewer who seeks and then immediately stops must still stop: the stop
// arrives in the same burst as the seeks, and dropping it would leave them
// holding a key that no longer ends anything.
func TestAStopInsideASeekBurstStillStops(t *testing.T) {
	withFastCastTimings(t)
	withFastSeekDebounce(t)

	commands := make(chan castCommand, 8)
	commands <- castCmdSeekForward
	commands <- castCmdSeekForward
	commands <- castCmdStop

	session := &fakeSession{steps: []fakeStep{{progress: cast.Progress{Position: 100, Duration: 600}}}}

	var err error
	captureStdout(t, func() {
		err = watchCastWithControls(testCastConfig(t), testCastAnime(), session, &fakeServer{},
			newFakeRemux(), cast.Device{Name: "Living Room"}, commands, false)
	})

	if !errors.Is(err, ErrCastStopped) {
		t.Fatalf("a stop inside a seek burst returned %v", err)
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

// A receiver that plays the last segment of a fully delivered episode and then
// sits on the final frame reporting PLAYING must be read as a finished episode,
// promptly -- not as a stall two minutes later. Seen on hardware: the panel
// froze at 23:41 / 23:41 and the cast then ended with "stopped reporting
// progress".
func TestWatchCastFrozenAtTheEndIsAFinish(t *testing.T) {
	withFastCastTimings(t)
	// Longer than the test waits: a finish must come from the end-of-episode
	// check, not the general stall bound.
	castStallTimeout = time.Minute

	remux := newFakeRemux()
	remux.succeed()
	session := &fakeSession{
		steps: []fakeStep{
			{progress: cast.Progress{Position: 1400, Duration: 1421}},
			{progress: cast.Progress{Position: 1421.1, Duration: 1421}}, // frozen from here on
		},
	}
	anime := testCastAnime()
	config := testCastConfig(t)

	done := make(chan error, 1)
	go func() {
		done <- watchCast(config, anime, session, &fakeServer{}, remux, cast.Device{Name: "Office TV"}, false)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected a clean finish, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchCast did not return on a device frozen at the end")
	}
	entries := LocalGetAllAnime(filepath.Join(config.StoragePath, "curd_history.txt"))
	if !animeMarked(entries, anime.AnilistId) {
		t.Error("the episode was not marked watched")
	}
}

// A frozen position well short of the end is still a stall, not a finish.
func TestWatchCastFrozenBeforeTheEndIsStillAStall(t *testing.T) {
	withFastCastTimings(t)

	remux := newFakeRemux()
	remux.succeed()
	session := &fakeSession{
		steps: []fakeStep{
			{progress: cast.Progress{Position: 600, Duration: 1421}},
			{progress: cast.Progress{Position: 700, Duration: 1421}},
		},
	}

	err := watchCast(testCastConfig(t), testCastAnime(), session, &fakeServer{}, remux, cast.Device{Name: "Office TV"}, false)
	if err == nil || !strings.Contains(err.Error(), "stopped reporting progress") {
		t.Fatalf("expected a stall error, got: %v", err)
	}
}

// An ending that stops a few seconds short of the end is still skipped with a
// rebuild: what follows it (a post-credits line, a preview) is worth seeing.
func TestWatchCastSkippingAnEndingNearTheEndStillSeeks(t *testing.T) {
	withFastCastTimings(t)

	remux := newFakeRemux()
	remux.succeed()
	session := &fakeSession{
		steps: []fakeStep{
			{progress: cast.Progress{Position: 1328, Duration: 1421}},
			{progress: cast.Progress{Idle: true}},
		},
	}
	anime := testCastAnime()
	anime.Ep.SkipTimes = SkipTimes{Ed: Skip{Start: 1327, End: 1416}}
	config := testCastConfig(t)
	config.SkipEd = true

	if err := watchCast(config, anime, session, &fakeServer{}, remux, cast.Device{Name: "Office TV"}, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(session.seeks) != 1 || session.seeks[0] != 1416 {
		t.Errorf("expected one seek to 1416, got %v", session.seeks)
	}
}

// When that rebuild fails -- seen on hardware: too few frames left for the
// encoder -- nothing but the skipped ending is left to play, so the episode
// finishes rather than retrying every poll or playing the ending out.
func TestWatchCastFailedSkipNearTheEndFinishes(t *testing.T) {
	withFastCastTimings(t)

	remux := newFakeRemux()
	remux.succeed()
	session := &fakeSession{
		seekErr: errors.New("ffmpeg failed"),
		steps: []fakeStep{
			{progress: cast.Progress{Position: 1300, Duration: 1421}},
			{progress: cast.Progress{Position: 1328, Duration: 1421}},
		},
	}
	anime := testCastAnime()
	anime.Ep.SkipTimes = SkipTimes{Ed: Skip{Start: 1327, End: 1416}}
	config := testCastConfig(t)
	config.SkipEd = true

	if err := watchCast(config, anime, session, &fakeServer{}, remux, cast.Device{Name: "Office TV"}, false); err != nil {
		t.Fatalf("expected a clean finish, got: %v", err)
	}
	// The skip itself, then each earlier retry.
	if len(session.seeks) != 1+castSkipBackOffTries {
		t.Errorf("expected %d seek attempts, got %v", 1+castSkipBackOffTries, session.seeks)
	}
	if !session.paused {
		t.Error("the device was left playing the ending it was told to skip")
	}
	entries := LocalGetAllAnime(filepath.Join(config.StoragePath, "curd_history.txt"))
	if !animeMarked(entries, anime.AnilistId) {
		t.Error("the episode was not marked watched")
	}
}

// A rebuild that fails near the end is retried a little earlier, and the first
// start that works is kept.
func TestWatchCastFailedSkipNearTheEndRetriesEarlier(t *testing.T) {
	withFastCastTimings(t)

	remux := newFakeRemux()
	remux.succeed()
	session := &fakeSession{
		seekErrs: map[float64]error{1416: errors.New("no frames")},
		steps: []fakeStep{
			{progress: cast.Progress{Position: 1328, Duration: 1421}},
			// Still inside the ending, where the back-off put it on purpose:
			// seen on hardware, this used to trigger the skip again, fail, and
			// finish the episode before the last seconds played.
			{progress: cast.Progress{Position: 1414, Duration: 1421}},
			{progress: cast.Progress{Position: 1415, Duration: 1421}},
			{progress: cast.Progress{Idle: true}},
		},
	}
	anime := testCastAnime()
	anime.Ep.SkipTimes = SkipTimes{Ed: Skip{Start: 1327, End: 1416}}
	config := testCastConfig(t)
	config.SkipEd = true

	if err := watchCast(config, anime, session, &fakeServer{}, remux, cast.Device{Name: "Office TV"}, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []float64{1416, 1414}
	if len(session.seeks) != len(want) || session.seeks[0] != want[0] || session.seeks[1] != want[1] {
		t.Errorf("expected seeks %v, got %v", want, session.seeks)
	}
	if session.paused {
		t.Error("the device was paused although the retry worked")
	}
}

// An ending that runs to the last second has nothing after it: finish without
// a rebuild.
func TestWatchCastSkippingAnEndingThatRunsToTheEndFinishes(t *testing.T) {
	withFastCastTimings(t)

	remux := newFakeRemux()
	remux.succeed()
	session := &fakeSession{
		steps: []fakeStep{
			{progress: cast.Progress{Position: 1328, Duration: 1421}},
		},
	}
	anime := testCastAnime()
	anime.Ep.SkipTimes = SkipTimes{Ed: Skip{Start: 1327, End: 1421}}
	config := testCastConfig(t)
	config.SkipEd = true

	if err := watchCast(config, anime, session, &fakeServer{}, remux, cast.Device{Name: "Office TV"}, false); err != nil {
		t.Fatalf("expected a clean finish, got: %v", err)
	}
	if len(session.seeks) != 0 {
		t.Errorf("expected no seek, got %v", session.seeks)
	}
	if !session.paused {
		t.Error("the device was left playing the ending it was told to skip")
	}
}

// A skip whose rebuild fails is tried once, not on every poll.
func TestWatchCastFailedSkipIsNotRetried(t *testing.T) {
	withFastCastTimings(t)

	remux := newFakeRemux()
	session := &fakeSession{
		seekErr: errors.New("ffmpeg failed"),
		steps: []fakeStep{
			{progress: cast.Progress{Position: 10, Duration: 1421}},
			{progress: cast.Progress{Position: 11, Duration: 1421}},
			{progress: cast.Progress{Position: 12, Duration: 1421}},
			{progress: cast.Progress{Position: 13, Duration: 1421}},
			{progress: cast.Progress{Idle: true}},
		},
	}
	anime := testCastAnime()
	anime.Ep.SkipTimes = SkipTimes{Op: Skip{Start: 0, End: 90}}
	config := testCastConfig(t)
	config.SkipOp = true

	if err := watchCast(config, anime, session, &fakeServer{}, remux, cast.Device{Name: "Office TV"}, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(session.seeks) != 1 {
		t.Errorf("expected one seek attempt, got %d", len(session.seeks))
	}
}

// The receiver stops answering as the last segment plays out, so the episode is
// finished on the last poll before the end rather than after a 10s timeout.
func TestWatchCastFinishesJustBeforeTheEnd(t *testing.T) {
	withFastCastTimings(t)

	remux := newFakeRemux()
	remux.succeed()
	session := &fakeSession{
		steps: []fakeStep{
			{progress: cast.Progress{Position: 1419, Duration: 1421}},
			{progress: cast.Progress{Position: 1420.4, Duration: 1421}},
			{err: errors.New("device did not respond")},
		},
	}
	anime := testCastAnime()
	config := testCastConfig(t)

	if err := watchCast(config, anime, session, &fakeServer{}, remux, cast.Device{Name: "Office TV"}, false); err != nil {
		t.Fatalf("expected a clean finish, got: %v", err)
	}
	if session.calls != 2 {
		t.Errorf("expected to finish on the second poll, took %d", session.calls)
	}
	entries := LocalGetAllAnime(filepath.Join(config.StoragePath, "curd_history.txt"))
	if !animeMarked(entries, anime.AnilistId) {
		t.Error("the episode was not marked watched")
	}
}

// Near the live edge of an unfinished remux is ffmpeg running behind, not the end.
func TestWatchCastNearTheLiveEdgeIsNotTheEnd(t *testing.T) {
	withFastCastTimings(t)

	remux := newFakeRemux()
	session := &fakeSession{
		steps: []fakeStep{
			{progress: cast.Progress{Position: 100, Duration: 200}},
			{progress: cast.Progress{Position: 199.5, Duration: 200}},
			{progress: cast.Progress{Position: 201, Duration: 300}},
			{progress: cast.Progress{Idle: true}},
		},
	}

	if err := watchCast(testCastConfig(t), testCastAnime(), session, &fakeServer{}, remux, cast.Device{Name: "Office TV"}, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.calls != 4 {
		t.Errorf("finished early at the live edge: %d polls", session.calls)
	}
}

// Passing the watched threshold -- by seeking past it, typically -- must not
// mark an episode that is still playing. It is marked when the cast ends, as
// the mpv path does.
func TestWatchCastMarksWhenTheEpisodeEndsNotWhenTheThresholdIsPassed(t *testing.T) {
	withFastCastTimings(t)

	remux := newFakeRemux()
	remux.succeed()
	anime := testCastAnime()
	config := testCastConfig(t)
	historyPath := filepath.Join(config.StoragePath, "curd_history.txt")

	var markedMidPlay bool
	session := &fakeSession{
		steps: []fakeStep{
			{progress: cast.Progress{Position: 100, Duration: 1000}},
			{progress: cast.Progress{Position: 900, Duration: 1000}}, // a seek past 85%
			{progress: cast.Progress{Position: 910, Duration: 1000}},
			{progress: cast.Progress{Position: 920, Duration: 1000}},
			{progress: cast.Progress{Idle: true}},
		},
	}
	session.onCall = func(call int) {
		if call == 3 && animeMarked(LocalGetAllAnime(historyPath), anime.AnilistId) {
			markedMidPlay = true
		}
	}

	if err := watchCast(config, anime, session, &fakeServer{}, remux, cast.Device{Name: "Office TV"}, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if markedMidPlay {
		t.Error("the episode was marked while it was still playing")
	}
	if !animeMarked(LocalGetAllAnime(historyPath), anime.AnilistId) {
		t.Error("the episode was not marked when it ended")
	}
}

// An untracked cast plays like any other but writes nothing: no history row
// when it finishes, and none for a partial position when it is stopped.
func TestWatchCastUntrackedWritesNoHistory(t *testing.T) {
	withFastCastTimings(t)

	remux := newFakeRemux()
	remux.succeed()
	session := &fakeSession{
		steps: []fakeStep{
			{progress: cast.Progress{Position: 100, Duration: 1000}},
			{progress: cast.Progress{Position: 950, Duration: 1000}},
			{progress: cast.Progress{Idle: true}},
		},
	}
	anime := testCastAnime()
	anime.Untracked = true
	config := testCastConfig(t)

	if err := watchCast(config, anime, session, &fakeServer{}, remux, cast.Device{Name: "Office TV"}, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries := LocalGetAllAnime(filepath.Join(config.StoragePath, "curd_history.txt")); len(entries) != 0 {
		t.Errorf("an untracked cast wrote history: %+v", entries)
	}
}

// A device that has fetched nothing is told about well before the grace gives
// up: the failure used to be the first the viewer heard of a firewall, ninety
// seconds in, and the window closed on it.
func TestWatchCastHintsAtAFirewallWhileStillWaiting(t *testing.T) {
	withFastCastTimings(t)
	castStartupGrace = 200 * time.Millisecond
	prevHint := castFetchHintDelay
	castFetchHintDelay = time.Millisecond
	t.Cleanup(func() { castFetchHintDelay = prevHint })

	previous := clipboardWriteAll
	clipboardWriteAll = func(string) error { return nil }
	t.Cleanup(func() { clipboardWriteAll = previous })

	config := testCastConfig(t)
	config.CastPort = 8010
	session := &fakeSession{steps: []fakeStep{{progress: cast.Progress{Idle: true}}}}

	out := captureStdout(t, func() {
		_ = watchCast(config, testCastAnime(), session, &fakeServer{neverFetched: true},
			newFakeRemux(), cast.Device{Name: "Living Room"}, false)
	})

	if strings.Count(out, "firewall may be blocking port 8010") != 1 {
		t.Errorf("expected the hint exactly once while waiting, got:\n%s", out)
	}
}

// A device that did fetch the stream is not held up as a firewall problem.
func TestWatchCastNoFirewallHintOnceFetched(t *testing.T) {
	withFastCastTimings(t)
	castStartupGrace = 50 * time.Millisecond
	prevHint := castFetchHintDelay
	castFetchHintDelay = time.Millisecond
	t.Cleanup(func() { castFetchHintDelay = prevHint })

	config := testCastConfig(t)
	config.CastPort = 8010
	session := &fakeSession{steps: []fakeStep{{progress: cast.Progress{Idle: true}}}}

	out := captureStdout(t, func() {
		_ = watchCast(config, testCastAnime(), session, &fakeServer{},
			newFakeRemux(), cast.Device{Name: "Living Room"}, false)
	})

	if strings.Contains(out, "firewall") {
		t.Errorf("hinted at a firewall for a device that fetched the stream:\n%s", out)
	}
}

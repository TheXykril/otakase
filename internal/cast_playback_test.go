package internal

import (
	"errors"
	"math"
	"strings"
	"sync"
	"sync/atomic"
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
	mu   sync.Mutex
	err  error
	done chan struct{}
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
	// the history file on a mark) cannot trip the bound by accident, and still
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

	history := LocalGetAllAnime(HistoryFile(config.StoragePath))
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

// rollingSubtitleSession is a fakeSession whose subtitles are still being
// read: behind answers as the source would.
type rollingSubtitleSession struct {
	*fakeSession
	behind func(position float64) bool
}

func (s *rollingSubtitleSession) subtitlesBehind(position float64) bool { return s.behind(position) }

// Playback nearing the end of the subtitles burned in rebuilds the stream
// where the device is, with more -- at the reported position, not at zero.
func TestWatchCastRebuildsTheStreamForMoreSubtitles(t *testing.T) {
	withFastCastTimings(t)

	var asked atomic.Int32
	session := &rollingSubtitleSession{
		fakeSession: &fakeSession{steps: []fakeStep{
			{progress: cast.Progress{Position: 100, Duration: 1440}},
			{progress: cast.Progress{Position: 101, Duration: 1440}},
		}},
		// Behind once, as a source is until the rebuild moves its edge on.
		behind: func(float64) bool { return asked.Add(1) == 2 },
	}
	commands := make(chan castCommand, 1)

	done := make(chan error, 1)
	captureStdout(t, func() {
		go func() {
			done <- watchCastWithControls(testCastConfig(t), testCastAnime(), session, &fakeServer{}, newFakeRemux(), cast.Device{Name: "Living Room"}, commands, false)
		}()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) && asked.Load() < 4 {
			time.Sleep(5 * time.Millisecond)
		}
		commands <- castCmdStop
		<-done
	})

	session.mu.Lock()
	defer session.mu.Unlock()
	if len(session.seeks) != 1 || (session.seeks[0] != 100 && session.seeks[0] != 101) {
		t.Errorf("rebuilds %v, want exactly one at the device's position", session.seeks)
	}
}

// The edge of the burned subtitles, and whether more have been read than a
// rebuild is worth.
func TestCastSubtitlesBehind(t *testing.T) {
	for _, tc := range []struct {
		name       string
		edge, read float64
		position   float64
		want       bool
	}{
		{"far from the edge", 300, 900, 100, false},
		{"near the edge with plenty more read", 300, 900, 280, true},
		{"near the edge with too little more", 300, 350, 280, false},
		{"everything read", 300, math.Inf(1), 280, true},
		{"already holding everything", math.Inf(1), math.Inf(1), 280, false},
		{"past the edge, read only just past playback", 300, 400, 350, false},
		{"past the edge, read well past playback", 300, 500, 350, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &castStreamSource{liveSubs: fakeSubtitleSource(tc.read), coveredUntil: tc.edge}
			if got := source.subtitlesBehind(tc.position); got != tc.want {
				t.Errorf("subtitlesBehind(%v) = %v, want %v", tc.position, got, tc.want)
			}
		})
	}
}

// fakeSubtitleSource is a subtitle read that has got this far, from 0.
type fakeSubtitleSource float64

func (f fakeSubtitleSource) Covered() float64                 { return float64(f) }
func (f fakeSubtitleSource) From() float64                    { return 0 }
func (f fakeSubtitleSource) Snapshot(string) (float64, error) { return float64(f), nil }
func (f fakeSubtitleSource) FontsDir() string                 { return "" }
func (f fakeSubtitleSource) Stop()                            {}

// fakeSubtitleRead is a read that began somewhere and has got somewhere.
type fakeSubtitleRead struct {
	from, covered float64
	stopped       bool
}

func (f *fakeSubtitleRead) Covered() float64                 { return f.covered }
func (f *fakeSubtitleRead) From() float64                    { return f.from }
func (f *fakeSubtitleRead) Snapshot(string) (float64, error) { return f.covered, nil }
func (f *fakeSubtitleRead) FontsDir() string                 { return "" }
func (f *fakeSubtitleRead) Stop()                            { f.stopped = true }

// A seek the read cannot serve starts it over at the target: back before it
// began, or so far ahead that catching up would take minutes. One it can
// serve keeps it.
func TestCastSubtitlesAreReadAgainForASeekTheReadCannotServe(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target float64
		again  bool
	}{
		{"inside what was read", 400, false},
		{"just ahead of what was read", 700, false},
		{"back before the read began", 100, true},
		{"far ahead of what was read", 1200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := &fakeSubtitleRead{from: 300, covered: 600}
			var startedAt float64 = -1
			source := &castStreamSource{liveSubs: current, readSubsFrom: func(from float64) (castSubtitleSource, error) {
				startedAt = from
				return &fakeSubtitleRead{from: from, covered: from}, nil
			}}
			source.rereadSubtitles(tc.target)
			if again := startedAt >= 0; again != tc.again {
				t.Fatalf("read again = %v, want %v", again, tc.again)
			}
			if tc.again && (startedAt != tc.target || !current.stopped || source.liveSubs.From() != tc.target) {
				t.Errorf("read again from %v (stopped old: %v, now from %v), want from %v", startedAt, current.stopped, source.liveSubs.From(), tc.target)
			}
		})
	}
}

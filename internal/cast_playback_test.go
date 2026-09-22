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
	onCall func(call int)
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
	mu  sync.Mutex
	err error
}

func (s *fakeServer) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// withFastCastTimings shrinks the timing watchCast polls against so its table
// tests run in milliseconds instead of the real 30-second/2-minute windows,
// and restores the originals on cleanup so no other test in the package
// observes the change.
func withFastCastTimings(t *testing.T) {
	t.Helper()
	prevPoll, prevGrace := castPollInterval, castStartupGrace
	castPollInterval = 2 * time.Millisecond
	castStartupGrace = time.Millisecond
	t.Cleanup(func() {
		castPollInterval, castStartupGrace = prevPoll, prevGrace
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

	err := watchCast(config, anime, session, server, remux, cast.Device{Name: "Living Room"})

	if !errors.Is(err, failure) {
		t.Fatalf("expected the remux failure, got %v", err)
	}
	if anime.Ep.Duration != 1440 {
		t.Errorf("duration was overwritten by a truncated live edge: got %d", anime.Ep.Duration)
	}
	entries := LocalGetAllAnime(filepath.Join(config.StoragePath, "curd_history.txt"))
	if animeMarked(entries, anime.AnilistId) {
		t.Error("a failed remux marked the episode watched")
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
			{progress: cast.Progress{Idle: true}},                // gives up
		},
	}

	anime := testCastAnime()
	config := testCastConfig(t)

	err := watchCast(config, anime, session, server, remux, cast.Device{Name: "Guest VLAN TV"})

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
			{err: lostContact},                                     // device stops answering
		},
	}
	session.onCall = func(call int) {
		if call == 0 {
			remux.fail(failure)
		}
	}

	anime := testCastAnime()
	config := testCastConfig(t)

	err := watchCast(config, anime, session, server, remux, cast.Device{Name: "Living Room"})

	if !errors.Is(err, failure) {
		t.Fatalf("expected the remembered remux failure to outrank the lost device, got %v", err)
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

	err := watchCast(config, anime, session, server, remux, cast.Device{Name: "Living Room"})

	if err != nil {
		t.Fatalf("a failure discovered after the episode was marked watched should read as a clean finish, got %v", err)
	}
	entries := LocalGetAllAnime(filepath.Join(config.StoragePath, "curd_history.txt"))
	if !animeMarked(entries, anime.AnilistId) {
		t.Error("expected the episode to have been marked watched before the remux failed")
	}
}

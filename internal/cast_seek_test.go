package internal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thexykril/otakase/internal/cast"
)

// fakeInnerSession is the device as the seeking wrapper sees it: it reports the
// position of whichever stream is playing, which after a seek starts at zero
// again.
type fakeInnerSession struct {
	position float64
	duration float64
	seeks    []float64
	paused   bool
	volume   float64
	err      error
}

func (f *fakeInnerSession) Progress() (cast.Progress, error) {
	return cast.Progress{Position: f.position, Duration: f.duration}, f.err
}

func (f *fakeInnerSession) SeekToTime(seconds float64) error {
	f.seeks = append(f.seeks, seconds)
	return nil
}
func (f *fakeInnerSession) Pause() error              { f.paused = true; return nil }
func (f *fakeInnerSession) Unpause() error            { f.paused = false; return nil }
func (f *fakeInnerSession) SetVolume(l float64) error { f.volume = l; return nil }
func (f *fakeInnerSession) Volume() float64           { return f.volume }

func seekingSession(inner *fakeInnerSession, duration float64, restart func(float64) error) *castSeekingSession {
	return &castSeekingSession{inner: inner, duration: duration, restart: restart}
}

// The device plays a rebuilt stream from zero, so its position means nothing to
// a viewer on its own: 30 seconds into a stream that began at 12:00 is 12:30 of
// the episode. Everything above this -- the panel, the progress written to the
// trackers, the completion threshold -- works in episode time.
func TestThePositionIsReportedInEpisodeTimeAfterASeek(t *testing.T) {
	inner := &fakeInnerSession{position: 0, duration: -1}
	restarted := []float64{}
	session := seekingSession(inner, 1480, func(target float64) error {
		restarted = append(restarted, target)
		// A new stream starts at zero, which is the whole reason this wrapper
		// exists.
		inner.position = 0
		return nil
	})

	if err := session.SeekToTime(720); err != nil {
		t.Fatal(err)
	}
	inner.position = 30

	progress, err := session.Progress()
	if err != nil {
		t.Fatal(err)
	}
	if progress.Position != 750 {
		t.Fatalf("position reported as %.1f, want 750", progress.Position)
	}
	if len(restarted) != 1 || restarted[0] != 720 {
		t.Fatalf("restarts: %v", restarted)
	}
}

// The device never learns the length: it reports dur=-1.0 throughout, and after
// a seek its own idea of the stream is the tail, not the episode.
func TestTheDurationStaysTheEpisodesOwn(t *testing.T) {
	inner := &fakeInnerSession{duration: -1}
	session := seekingSession(inner, 1480, func(float64) error { return nil })

	progress, err := session.Progress()
	if err != nil {
		t.Fatal(err)
	}
	if progress.Duration != 1480 {
		t.Fatalf("duration reported as %.1f, want the episode's 1480", progress.Duration)
	}
}

func TestSeekingIsNotAskedOfTheDevice(t *testing.T) {
	inner := &fakeInnerSession{}
	session := seekingSession(inner, 1480, func(float64) error { return nil })

	if err := session.SeekToTime(600); err != nil {
		t.Fatal(err)
	}
	// Asking the device is exactly what does not work: it accepts the request,
	// answers without an error and goes on playing where it was.
	if len(inner.seeks) != 0 {
		t.Fatalf("the device was asked to seek: %v", inner.seeks)
	}
}

func TestSeekingBackBeforeTheStartLandsAtTheStart(t *testing.T) {
	restarted := []float64{}
	session := seekingSession(&fakeInnerSession{}, 1480, func(target float64) error {
		restarted = append(restarted, target)
		return nil
	})

	if err := session.SeekToTime(-30); err != nil {
		t.Fatal(err)
	}
	if len(restarted) != 1 || restarted[0] != 0 {
		t.Fatalf("a seek before the start restarted at %v", restarted)
	}
}

// A stream starting two seconds before the end has to launch, buffer and reach
// the device before it is over. ffmpeg would write almost nothing and the device
// would sit on a playlist that never gains a segment.
func TestSeekingPastTheEndLandsShortOfIt(t *testing.T) {
	restarted := []float64{}
	session := seekingSession(&fakeInnerSession{}, 1480, func(target float64) error {
		restarted = append(restarted, target)
		return nil
	})

	if err := session.SeekToTime(2000); err != nil {
		t.Fatal(err)
	}
	if len(restarted) != 1 {
		t.Fatalf("restarts: %v", restarted)
	}
	if got := restarted[0]; got != 1480-castSeekTailGuard {
		t.Fatalf("a seek past the end landed at %.1f, want %.1f", got, 1480-castSeekTailGuard)
	}
}

// Without a measured length there is nothing to guard against: the tracker's
// average would refuse the last minutes of a longer episode and allow a seek
// past the end of a shorter one.
func TestWithNoKnownLengthASeekIsNotSecondGuessed(t *testing.T) {
	restarted := []float64{}
	session := seekingSession(&fakeInnerSession{}, 0, func(target float64) error {
		restarted = append(restarted, target)
		return nil
	})

	if err := session.SeekToTime(4000); err != nil {
		t.Fatal(err)
	}
	if len(restarted) != 1 || restarted[0] != 4000 {
		t.Fatalf("restarts: %v", restarted)
	}
}

// A seek that could not rebuild the stream must leave the wrapper reporting the
// stream that is actually playing. Moving the base anyway would add the failed
// seek's offset to every later position.
func TestAFailedSeekDoesNotMoveTheClock(t *testing.T) {
	inner := &fakeInnerSession{position: 40}
	session := seekingSession(inner, 1480, func(float64) error {
		return errors.New("ffmpeg could not start")
	})

	if err := session.SeekToTime(600); err == nil {
		t.Fatal("a failed seek was reported as success")
	}

	progress, err := session.Progress()
	if err != nil {
		t.Fatal(err)
	}
	if progress.Position != 40 {
		t.Fatalf("position moved to %.1f after a failed seek", progress.Position)
	}
}

func TestSeeksCompoundFromWhereTheEpisodeIs(t *testing.T) {
	inner := &fakeInnerSession{}
	session := seekingSession(inner, 1480, func(target float64) error {
		inner.position = 0
		return nil
	})

	// Two presses of the same key, as the watch loop issues them: each target is
	// absolute episode time, so the second must not be taken relative to the
	// first stream.
	if err := session.SeekToTime(100); err != nil {
		t.Fatal(err)
	}
	if err := session.SeekToTime(110); err != nil {
		t.Fatal(err)
	}
	inner.position = 5

	progress, _ := session.Progress()
	if progress.Position != 115 {
		t.Fatalf("position reported as %.1f, want 115", progress.Position)
	}
}

// The watch loop reads Err() to tell a stream that died under the device from a
// finished episode. Stopping ffmpeg on purpose makes it exit non-zero, so a seek
// must not leave that error where the loop can read it.
func TestAStreamStoppedByASeekIsNotReadAsAFailure(t *testing.T) {
	stopped := startedRemux(t, "sleep 30")
	replacement := startedRemux(t, "sleep 30")

	switched := newCastRemuxSwitch(stopped)
	previous := switched.swap(replacement)
	if previous != stopped {
		t.Fatal("swap did not hand back the stream it replaced")
	}

	// The stopped generation really did fail, which is exactly why it must no
	// longer be the one being read.
	<-stopped.Done()
	if stopped.Err() == nil {
		t.Skip("this platform does not report a killed process as an error")
	}
	if err := switched.Err(); err != nil {
		t.Fatalf("a stream stopped by a seek was read as a failure: %v", err)
	}
}

// The opposite failure, and the reason the switch does not simply suppress
// errors once a seek has happened: a rebuilt stream that cannot start is a real
// failure, and the viewer is left watching nothing while the device plays out
// what is already on disk.
func TestARebuiltStreamThatFailsIsStillReported(t *testing.T) {
	switched := newCastRemuxSwitch(startedRemux(t, "sleep 30"))
	switched.swap(startedRemux(t, "exit 1"))

	<-switched.Done()
	if switched.Err() == nil {
		t.Fatal("a rebuilt stream that failed to run was reported as healthy")
	}
}

func TestADyingStreamIsReportedBeforeAnySeek(t *testing.T) {
	switched := newCastRemuxSwitch(startedRemux(t, "exit 1"))

	<-switched.Done()
	if switched.Err() == nil {
		t.Fatal("a remux that failed before any seek was reported as healthy")
	}
	if switched.seeked() {
		t.Fatal("a switch that never swapped reported a seek")
	}
}

func TestTheSwitchWaitsOnTheCurrentStreamNotTheReplacedOne(t *testing.T) {
	switched := newCastRemuxSwitch(startedRemux(t, "exit 0"))
	// The replaced stream has already finished. Waiting on it would report the
	// episode over the moment the viewer seeks.
	<-switched.Done()

	switched.swap(startedRemux(t, "sleep 30"))
	select {
	case <-switched.Done():
		t.Fatal("the switch reported the new stream finished")
	default:
	}
	if !switched.seeked() {
		t.Fatal("a swapped switch did not report a seek")
	}
}

// startedRemux runs a shell command through the real remux plumbing, so the
// switch is exercised against the type it actually wraps.
func startedRemux(t *testing.T, script string) *cast.Remux {
	t.Helper()
	remux, err := cast.StartFFmpeg("/bin/sh", []string{"-c", script}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(remux.Stop)
	return remux
}

// Each seek writes a new generation into its own directory, because a device
// handed the same URL twice may play the episode out of its own cache and never
// fetch the new segments at all.
func TestEachRebuiltStreamGetsItsOwnURL(t *testing.T) {
	root := t.TempDir()
	source := &castStreamSource{ffmpeg: "unused", streamURL: "https://host/master.m3u8", rootDir: root}
	// Standing in for ffmpeg: writes the playlist the wait requires, in whatever
	// directory the source chose, and exits.
	source.launch = func(_ string, _ []string, outDir string) (*cast.Remux, error) {
		playlist := filepath.Join(outDir, cast.PlaylistName)
		return cast.StartFFmpeg("/bin/sh", []string{"-c", fmt.Sprintf("printf '#EXTM3U\\n' > %q", playlist)}, outDir)
	}

	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		got, err := source.start(float64(i * 60))
		if err != nil {
			t.Fatal(err)
		}
		if seen[got.path] {
			t.Fatalf("seek %d reused the URL path %q", i, got.path)
		}
		seen[got.path] = true
		if !strings.HasSuffix(got.path, cast.PlaylistName) {
			t.Fatalf("path %q does not end in the playlist name", got.path)
		}
		// Forward slashes: this is a URL, whatever the separator on this machine.
		if strings.Contains(got.path, "\\") {
			t.Fatalf("path %q is a file path, not a URL path", got.path)
		}
		if _, err := os.Stat(filepath.Join(got.dir, cast.PlaylistName)); err != nil {
			t.Fatalf("generation %d has no playlist on disk: %v", i, err)
		}
	}
}

// The offset reaches ffmpeg, and the subtitles are shifted to match it. A
// mismatch here is the failure that shows every subtitle early by exactly the
// seek distance.
func TestARebuiltStreamCarriesTheOffsetAndTheShiftedSubtitles(t *testing.T) {
	root := t.TempDir()
	subtitles := filepath.Join(root, "original.vtt")
	if err := os.WriteFile(subtitles, []byte("WEBVTT\n\n1\n00:10:10.000 --> 00:10:12.000\nLine.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	source := &castStreamSource{
		ffmpeg:       "unused",
		streamURL:    "https://host/master.m3u8",
		rootDir:      root,
		subtitlePath: subtitles,
		encoder:      cast.Encoder{Name: "libx264"},
	}

	var got []string
	source.launch = func(_ string, args []string, outDir string) (*cast.Remux, error) {
		got = args
		playlist := filepath.Join(outDir, cast.PlaylistName)
		return cast.StartFFmpeg("/bin/sh", []string{"-c", fmt.Sprintf("printf '#EXTM3U\\n' > %q", playlist)}, outDir)
	}

	generation, err := source.start(600)
	if err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "-ss 600.000") {
		t.Fatalf("the offset did not reach ffmpeg:\n%s", joined)
	}
	if !strings.Contains(joined, "subtitles=") {
		t.Fatalf("the subtitles were not burned:\n%s", joined)
	}

	shifted, err := os.ReadFile(filepath.Join(generation.dir, "subtitles.vtt"))
	if err != nil {
		t.Fatalf("no shifted subtitle file: %v", err)
	}
	if !strings.Contains(string(shifted), "00:00:10.000") {
		t.Fatalf("the subtitles were not shifted by the seek:\n%s", shifted)
	}
	// The original is never overwritten: every shift is taken from it, so two
	// seeks in a row must not compound.
	original, err := os.ReadFile(subtitles)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(original), "00:10:10.000") {
		t.Fatalf("the original subtitle file was modified:\n%s", original)
	}
}

// castResumeFixture writes a history file the way a cast does, and returns a
// config pointing at it.
func castResumeFixture(t *testing.T, rows string) *Config {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "curd_history.txt"), []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	return &Config{StoragePath: dir}
}

// The history your own file held: three providers, one of them an episode ahead.
const castResumeHistory = `201514,"Saijo no Osewa",11,1,0,nyaa,Rich Girl Caretaker
201514,201514,10,548,0,anikoto,Rich Girl Caretaker
201514,lpams7m8,10,387,24,anizone,Rich Girl Caretaker
`

func castResumeAnime(episode, carried int, provider string) *Anime {
	anime := &Anime{AnilistId: 201514, ProviderName: provider}
	anime.Ep.Number = episode
	anime.Ep.Resume = true
	anime.Ep.Player.PlaybackTime = carried
	anime.Ep.Duration = 1480
	return anime
}

// Casting used to start a part-watched episode over and say so, because the
// device cannot seek. Rebuilding the stream at an offset is what makes a resume
// possible: it is the first seek, made before playback begins.
func TestACastResumesWhereTheEpisodeWasLeft(t *testing.T) {
	config := castResumeFixture(t, castResumeHistory)

	if got := castResumeAt(config, castResumeAnime(10, 387, "anizone")); got != 387 {
		t.Fatalf("a resume at 6:27 produced %.0f", got)
	}
}

// How this last failed. A cast launched from rofi runs in a second process that
// never sees the picking flow: it rebuilds the anime from a handoff file, so the
// position it carries was resolved against whichever episode the first process
// had in mind. Episode 10 arrived carrying episode 11's one second, which the
// gate then rejected as too early to bother with, silently.
func TestACastIgnoresAPositionBelongingToAnotherEpisode(t *testing.T) {
	config := castResumeFixture(t, castResumeHistory)

	if got := castResumeAt(config, castResumeAnime(10, 1, "anizone")); got != 387 {
		t.Fatalf("episode 10 carrying episode 11's position resumed at %.0f, want 387", got)
	}
}

func TestACastResumesFromTheProviderAboutToPlay(t *testing.T) {
	config := castResumeFixture(t, castResumeHistory)

	// Two providers hold a position in this episode, in different encodes of it.
	if got := castResumeAt(config, castResumeAnime(10, 1, "anikoto")); got != 548 {
		t.Fatalf("anikoto resumed at %.0f, want its own 548", got)
	}
}

func TestAnEpisodeWithNoSavedPositionStartsAtTheBeginning(t *testing.T) {
	config := castResumeFixture(t, castResumeHistory)

	// Episode 12 has no row. Falling back to another episode's position would
	// resume one the viewer never started.
	if got := castResumeAt(config, castResumeAnime(12, 548, "anizone")); got != 0 {
		t.Fatalf("an episode with no saved position resumed at %.0f", got)
	}
}

func TestAnEpisodeNotBeingResumedStartsAtTheBeginning(t *testing.T) {
	config := castResumeFixture(t, castResumeHistory)

	anime := castResumeAnime(10, 0, "anizone")
	// Both cleared is what a rewatch and "use the tracker's episode" leave
	// behind, and both mean start over however good the row in the file looks.
	anime.Ep.Resume = false

	if got := castResumeAt(config, anime); got != 0 {
		t.Fatalf("an episode being started over resumed at %.0f", got)
	}
}

func TestAResumePointTooEarlyToMatterIsIgnored(t *testing.T) {
	config := castResumeFixture(t, "201514,201514,10,30,0,anikoto,Rich Girl Caretaker\n")

	// Rebuilding the stream costs seconds, which is more than this would save.
	if got := castResumeAt(config, castResumeAnime(10, 30, "anikoto")); got != 0 {
		t.Fatalf("a resume point of thirty seconds produced %.0f", got)
	}
}

func TestAResumePointNearTheEndIsIgnored(t *testing.T) {
	// The episode is effectively over: a stream starting in the credits is done
	// before it buffers, the same reason a seek will not land there.
	config := castResumeFixture(t, "201514,201514,10,1470,0,anikoto,Rich Girl Caretaker\n")

	if got := castResumeAt(config, castResumeAnime(10, 1470, "anikoto")); got != 0 {
		t.Fatalf("a resume point in the credits produced %.0f", got)
	}
}

// A resumed cast reports episode time from its first poll. Without the base the
// panel would show 0:00 for an episode playing at 6:27, and the progress written
// back to the trackers would walk the episode backwards.
func TestAResumedCastReportsEpisodeTimeImmediately(t *testing.T) {
	inner := &fakeInnerSession{position: 4, duration: -1}
	session := &castSeekingSession{inner: inner, base: 387, duration: 1480}

	progress, err := session.Progress()
	if err != nil {
		t.Fatal(err)
	}
	if progress.Position != 391 {
		t.Fatalf("a resumed cast reported %.1f, want 391", progress.Position)
	}
}

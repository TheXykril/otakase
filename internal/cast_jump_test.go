package internal

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/thexykril/otakase/internal/cast"
)

// writtenGeneration writes a playlist of segments of the given lengths into a
// generation directory, as ffmpeg would have so far.
func writtenGeneration(t *testing.T, start float64, finished bool, lengths ...float64) stream {
	t.Helper()
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:6\n#EXT-X-TARGETDURATION:10\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:EVENT\n")
	for i, length := range lengths {
		b.WriteString("#EXTINF:" + strconv.FormatFloat(length, 'f', -1, 64) + ",\n")
		b.WriteString("seg" + string(rune('a'+i)) + ".ts\n")
	}
	if finished {
		b.WriteString("#EXT-X-ENDLIST\n")
	}
	if err := os.WriteFile(filepath.Join(dir, cast.PlaylistName), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return stream{dir: dir, path: "g0/" + cast.PlaylistName, start: start}
}

func TestAJumpLandsOnTheNearestSegmentAlreadyWritten(t *testing.T) {
	// A stream that began at 600 of the episode, with 60 seconds written.
	gen := writtenGeneration(t, 600, false, 10, 10, 10, 10, 10, 10)
	source := &castStreamSource{}

	segment, offset, ok := source.jumpPoint(gen, 623, 0)
	if !ok || segment != 2 || offset != 20 {
		t.Fatalf("jump to 623: segment %d at %.1f ok=%t, want segment 2 at 20", segment, offset, ok)
	}
	if segment, offset, ok = source.jumpPoint(gen, 627, 0); !ok || segment != 3 || offset != 30 {
		t.Fatalf("jump to 627: segment %d at %.1f ok=%t, want segment 3 at 30", segment, offset, ok)
	}
}

func TestAJumpIsNotMadeWhereFFmpegHasNotReached(t *testing.T) {
	gen := writtenGeneration(t, 600, false, 10, 10, 10, 10, 10, 10)
	source := &castStreamSource{}

	// Before the stream begins: it does not reach back there at all.
	if _, _, ok := source.jumpPoint(gen, 590, 0); ok {
		t.Fatal("jumped to before the stream begins")
	}
	// Past what is written, and too near the edge to play from without
	// waiting on ffmpeg.
	for _, target := range []float64{700, 655} {
		if _, _, ok := source.jumpPoint(gen, target, 0); ok {
			t.Fatalf("jumped to %.0f, at or past the edge of a stream written to 660", target)
		}
	}
	// A finished stream has no edge to wait on.
	done := writtenGeneration(t, 600, true, 10, 10, 10, 10, 10, 10)
	if _, _, ok := source.jumpPoint(done, 655, 0); !ok {
		t.Fatal("no jump into a finished stream")
	}
}

func TestAJumpStaysInsideTheSubtitlesBurned(t *testing.T) {
	gen := writtenGeneration(t, 0, true, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10)
	source := &castStreamSource{liveSubs: fakeSubtitleSource(60), coveredUntil: 60}
	if _, _, ok := source.jumpPoint(gen, 45, 0); ok {
		t.Fatal("jumped past the subtitles the stream was burned with")
	}
	if _, _, ok := source.jumpPoint(gen, 20, 0); !ok {
		t.Fatal("no jump within the subtitles burned")
	}
	source.coveredUntil = math.Inf(1)
	if _, _, ok := source.jumpPoint(gen, 80, 0); !ok {
		t.Fatal("no jump with every subtitle burned")
	}
}

func TestAJumpLeavesTooSmallAnOffsetToARebuild(t *testing.T) {
	gen := writtenGeneration(t, 0, true, 10, 10, 10, 10, 10, 10)
	source := &castStreamSource{}
	if _, _, ok := source.jumpPoint(gen, 20, castJumpLearnOffset); ok {
		t.Fatal("jumped 20s in while the device's clock is still to be learnt")
	}
	if _, offset, ok := source.jumpPoint(gen, 2, castJumpLearnOffset); !ok || offset != 0 {
		t.Fatal("no jump to the very start, where both clocks agree")
	}
}

// jumpingSession is a seeking session whose jumps land where told.
func jumpingSession(inner *fakeInnerSession, landAt float64, offset float64) (*castSeekingSession, *[]float64) {
	restarts := []float64{}
	session := seekingSession(inner, 1480, func(target float64) error {
		restarts = append(restarts, target)
		return nil
	})
	session.jump = func(target, minOffset float64) (float64, float64, bool, error) {
		if offset > 0 && offset < minOffset {
			return 0, 0, false, nil
		}
		return landAt, offset, true, nil
	}
	return session, &restarts
}

func TestAJumpNeedsNoRebuild(t *testing.T) {
	inner := &fakeInnerSession{}
	session, restarts := jumpingSession(inner, 640, 40)

	landed, err := session.seekNear(642)
	if err != nil || landed != 640 {
		t.Fatalf("landed at %.1f err=%v", landed, err)
	}
	if len(*restarts) != 0 {
		t.Fatalf("rebuilt for a jump: %v", *restarts)
	}
}

// The device may count a jump from where it was handed the stream, or from
// where its generation starts; either way the panel shows the episode.
func TestAJumpIsReportedInEpisodeTimeEitherWayTheDeviceCounts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		reading float64
	}{
		{"from the load", 5},
		{"from the generation", 45},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inner := &fakeInnerSession{duration: -1}
			session, _ := jumpingSession(inner, 640, 40)
			session.base, session.streamStart = 600, 600

			if _, err := session.seekNear(642); err != nil {
				t.Fatal(err)
			}
			settleIn(t, 5*time.Second)
			inner.position = tc.reading
			progress, err := session.Progress()
			if err != nil {
				t.Fatal(err)
			}
			if progress.Position != 645 {
				t.Fatalf("reported %.1f, want 645", progress.Position)
			}
			// Learnt once: later readings count the same way.
			inner.position = tc.reading + 10
			if progress, _ = session.Progress(); progress.Position != 655 {
				t.Fatalf("then reported %.1f, want 655", progress.Position)
			}
		})
	}
}

// Just after a load the receiver can still answer about the stream it was
// told to leave. Believed, that reading puts the position wherever the old
// stream had got to.
func TestAReadingFromTheStreamBeforeIsHeld(t *testing.T) {
	inner := &fakeInnerSession{}
	session := seekingSession(inner, 1480, func(float64) error { return nil })
	if err := session.SeekToTime(900); err != nil {
		t.Fatal(err)
	}
	// Two seconds after the load, the old stream's 300.
	settleIn(t, 2*time.Second)
	inner.position = 300
	if progress, _ := session.Progress(); progress.Position != 900 {
		t.Fatalf("reported %.1f, want the seek's 900 held", progress.Position)
	}
	inner.position = 1
	if progress, _ := session.Progress(); progress.Position != 901 {
		t.Fatalf("reported %.1f, want 901 once the new stream reports", progress.Position)
	}
}

func TestADeviceSayingItIsStillOnTheOldStreamIsHeld(t *testing.T) {
	inner := &settlingSession{}
	session := &castSeekingSession{inner: inner, duration: 1480, restart: func(float64) error { return nil }}
	if err := session.SeekToTime(900); err != nil {
		t.Fatal(err)
	}
	progress, err := session.Progress()
	if err != nil {
		t.Fatal(err)
	}
	if progress.Position != 900 || progress.Idle || progress.Settling {
		t.Fatalf("reported %+v, want 900 and playing", progress)
	}
}

// settlingSession is a device still describing the stream it was told to
// leave, which has gone idle.
type settlingSession struct{ fakeInnerSession }

func (s *settlingSession) Progress() (cast.Progress, error) {
	return cast.Progress{Position: 300, Idle: true, Settling: true}, nil
}

func TestASeekWithNoJumpRebuilds(t *testing.T) {
	inner := &fakeInnerSession{}
	session, restarts := jumpingSession(inner, 0, 0)
	session.jump = func(float64, float64) (float64, float64, bool, error) { return 0, 0, false, nil }

	landed, err := session.seekNear(700)
	if err != nil || landed != 700 || len(*restarts) != 1 || (*restarts)[0] != 700 {
		t.Fatalf("landed %.1f err=%v restarts=%v", landed, err, *restarts)
	}
}

func TestAFailedJumpIsReported(t *testing.T) {
	inner := &fakeInnerSession{}
	session, _ := jumpingSession(inner, 0, 0)
	session.jump = func(float64, float64) (float64, float64, bool, error) {
		return 0, 0, false, errors.New("device refused")
	}
	if _, err := session.seekNear(700); err == nil {
		t.Fatal("a refused jump reported success")
	}
}

package cast

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func shiftFixture(t *testing.T, body string, seconds float64) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "in.vtt")
	dst := filepath.Join(dir, "out.vtt")
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ShiftWebVTT(src, dst, seconds); err != nil {
		t.Fatal(err)
	}
	shifted, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	return string(shifted)
}

// A seek restarts the stream at the target, so the video the device plays begins
// at 0 again while the subtitle file is still stamped against the whole episode.
// Without the shift, a line written for 12:00 appears twelve minutes early.
func TestSubtitlesMoveWithASeek(t *testing.T) {
	got := shiftFixture(t, `WEBVTT

1
00:12:00.000 --> 00:12:04.500
Kept me waiting, huh.
`, 600)

	if !strings.Contains(got, "00:02:00.000 --> 00:02:04.500") {
		t.Fatalf("a cue at 12:00 was not moved to 2:00 by a ten minute seek:\n%s", got)
	}
	if !strings.Contains(got, "Kept me waiting, huh.") {
		t.Fatalf("the cue text was lost:\n%s", got)
	}
}

func TestACueBeforeTheSeekTargetIsDropped(t *testing.T) {
	got := shiftFixture(t, `WEBVTT

1
00:00:10.000 --> 00:00:12.000
Before the seek.

2
00:10:10.000 --> 00:10:12.000
After the seek.
`, 600)

	// It belongs to a part of the episode this stream no longer contains.
	if strings.Contains(got, "Before the seek.") {
		t.Fatalf("a cue from before the seek target survived:\n%s", got)
	}
	if !strings.Contains(got, "After the seek.") {
		t.Fatalf("the cue after the target was dropped too:\n%s", got)
	}
	// A dangling cue number with no timing line is a parse error for the filter,
	// so the whole cue has to go, not just its timing.
	if strings.Contains(got, "\n1\n") {
		t.Fatalf("the dropped cue left its number behind:\n%s", got)
	}
}

func TestACueAlreadyOnScreenWhenTheViewerSeeksStaysOnScreen(t *testing.T) {
	got := shiftFixture(t, `WEBVTT

1
00:09:58.000 --> 00:10:03.000
Straddling the target.
`, 600)

	// Clamped rather than dropped: the line was visible at the moment the viewer
	// sought, and it should still be visible when the stream resumes.
	if !strings.Contains(got, "00:00:00.000 --> 00:00:03.000") {
		t.Fatalf("a cue straddling the target was not clamped to zero:\n%s", got)
	}
}

func TestCueSettingsSurviveTheShift(t *testing.T) {
	got := shiftFixture(t, `WEBVTT

1
00:10:10.000 --> 00:10:12.000 line:90% align:center
Positioned at the bottom.
`, 600)

	// Signs and captions carry placement, and a line that jumps to the middle of
	// the picture is a visible regression even when the timing is right.
	if !strings.Contains(got, "line:90% align:center") {
		t.Fatalf("cue settings were lost:\n%s", got)
	}
}

func TestTheShortTimestampFormIsRead(t *testing.T) {
	// MM:SS.mmm is valid WebVTT and providers do emit it.
	got := shiftFixture(t, `WEBVTT

1
10:10.000 --> 10:12.000
Short form.
`, 600)

	if !strings.Contains(got, "00:00:10.000 --> 00:00:12.000") {
		t.Fatalf("a short-form timestamp was not shifted:\n%s", got)
	}
}

func TestTheHeaderAndNotesAreLeftAlone(t *testing.T) {
	got := shiftFixture(t, `WEBVTT
Kind: captions

NOTE this file came from the provider

1
00:10:10.000 --> 00:10:12.000
Line.
`, 600)

	for _, want := range []string{"WEBVTT", "Kind: captions", "NOTE this file came from the provider"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q was lost:\n%s", want, got)
		}
	}
}

func TestAnUnreadableTimingLineKeepsItsCue(t *testing.T) {
	// Losing synchronisation on a strange file is a bad outcome; losing the
	// subtitles entirely is a worse one.
	got := shiftFixture(t, `WEBVTT

1
not a timestamp --> either
Still here.
`, 600)

	if !strings.Contains(got, "Still here.") {
		t.Fatalf("a cue with an unreadable timing line was dropped:\n%s", got)
	}
}

func TestShiftingByNothingChangesNothingThatMatters(t *testing.T) {
	// The no-seek case goes through the same code, so it has to be harmless.
	got := shiftFixture(t, `WEBVTT

1
00:10:10.000 --> 00:10:12.000
Line.
`, 0)

	if !strings.Contains(got, "00:10:10.000 --> 00:10:12.000") {
		t.Fatalf("a zero shift moved the cue:\n%s", got)
	}
}

func TestASecondShiftOfTheSameFileCarriesFromWhereItWas(t *testing.T) {
	// Every seek shifts from the original file, never from an already-shifted
	// one: shifting twice from the original is how the offsets stay absolute.
	dir := t.TempDir()
	src := filepath.Join(dir, "in.vtt")
	if err := os.WriteFile(src, []byte("WEBVTT\n\n1\n00:20:00.000 --> 00:20:02.000\nLine.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		seek float64
		want string
	}{
		{600, "00:10:00.000 --> 00:10:02.000"},
		{900, "00:05:00.000 --> 00:05:02.000"},
	} {
		dst := filepath.Join(dir, "out.vtt")
		if err := ShiftWebVTT(src, dst, tc.seek); err != nil {
			t.Fatal(err)
		}
		shifted, err := os.ReadFile(dst)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(shifted), tc.want) {
			t.Fatalf("a seek to %.0fs produced:\n%s", tc.seek, shifted)
		}
	}
}

func TestRoundingDoesNotProduceAnImpossibleTimestamp(t *testing.T) {
	// 59.9996 rounds to 60.000 seconds, which must carry into the minute rather
	// than being written as :60.
	if got := formatWebVTTTime(59.9996); got != "00:01:00.000" {
		t.Fatalf("59.9996 formatted as %q", got)
	}
	if got := formatWebVTTTime(3599.9999); got != "01:00:00.000" {
		t.Fatalf("3599.9999 formatted as %q", got)
	}
}

// The direction matters, and it was documented backwards at first. A cue drawn
// against the rebuilt stream's own clock lands *after* the dialogue it belongs
// to, by the seek distance -- so the shift moves cues earlier, never later.
//
// Measured with ffmpeg on a black test video: a cue at 0:20 burned into a stream
// restarted at 0:15 appears at output 0:20 unshifted, and at output 0:05 shifted.
func TestTheShiftMovesCuesEarlierNotLater(t *testing.T) {
	got := shiftFixture(t, `WEBVTT

1
00:00:20.000 --> 00:00:25.000
MARK
`, 15)

	if !strings.Contains(got, "00:00:05.000 --> 00:00:10.000") {
		t.Fatalf("a cue at 0:20 seeked past 0:15 did not become 0:05:\n%s", got)
	}
	// 0:35 would be the wrong direction, and the one that leaves subtitles
	// trailing the dialogue by the seek distance.
	if strings.Contains(got, "00:00:35.000") {
		t.Fatalf("the shift moved the cue later instead of earlier:\n%s", got)
	}
}

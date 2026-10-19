package cast

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const assFixture = `[Script Info]
ScriptType: v4.00+

[V4+ Styles]
Format: Name, Fontname, Fontsize
Style: Default,Arial,20

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:05.00,0:00:08.00,Default,,0,0,0,,Gone before the seek.
Dialogue: 0,0:01:28.50,0:01:32.00,Default,,0,0,0,,On screen at the seek.
Dialogue: 0,0:12:00.00,0:12:04.50,Default,,0,0,0,,Kept me waiting, huh.
Comment: 0,0:12:10.00,0:12:11.00,Default,,0,0,0,,A note, not a line.
`

func shiftSubtitleFixture(t *testing.T, name, body string, seconds float64) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, name)
	dst := filepath.Join(dir, "out"+filepath.Ext(name))
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ShiftSubtitles(src, dst, seconds); err != nil {
		t.Fatal(err)
	}
	shifted, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	return string(shifted)
}

// ASS carries its timings in Dialogue lines, not "-->" lines. Shifting it as
// WebVTT left every line late by the seek distance -- including the automatic
// opening skip -- which read as subtitles that were out of sync, or that did
// not appear at all until the first late line came round.
func TestASSSubtitlesMoveWithASeek(t *testing.T) {
	got := shiftSubtitleFixture(t, "in.ass", assFixture, 90)

	if !strings.Contains(got, "Dialogue: 0,0:10:30.00,0:10:34.50,Default,,0,0,0,,Kept me waiting, huh.") {
		t.Fatalf("a line at 12:00 was not moved to 10:30 by a 90s seek:\n%s", got)
	}
	if strings.Contains(got, "Gone before the seek.") {
		t.Errorf("a line that ended before the seek was kept:\n%s", got)
	}
	if !strings.Contains(got, "Dialogue: 0,0:00:00.00,0:00:02.00,Default,,0,0,0,,On screen at the seek.") {
		t.Errorf("a line straddling the seek was not clamped to zero:\n%s", got)
	}
	if !strings.Contains(got, "[V4+ Styles]") || !strings.Contains(got, "Style: Default,Arial,20") {
		t.Errorf("the header and styles did not survive:\n%s", got)
	}
}

// The text of a line may itself hold commas; only the timing fields move.
func TestASSShiftLeavesTheTextAlone(t *testing.T) {
	got := shiftSubtitleFixture(t, "in.ass", assFixture, 60)
	if !strings.Contains(got, ",,Kept me waiting, huh.") {
		t.Fatalf("the text of a line was changed:\n%s", got)
	}
}

// The Format line decides where Start and End sit, and a file is free to order
// its fields differently.
func TestASSShiftFollowsTheFormatLine(t *testing.T) {
	body := "[Events]\nFormat: Layer, Style, Start, End, Text\nDialogue: 0,Default,0:02:00.00,0:02:03.00,Hello\n"
	got := shiftSubtitleFixture(t, "in.ass", body, 60)
	if !strings.Contains(got, "Dialogue: 0,Default,0:01:00.00,0:01:03.00,Hello") {
		t.Fatalf("the shift ignored the Format line:\n%s", got)
	}
}

// The format is read from the content, since the URL a provider hands over is
// not always honest about it.
func TestShiftSubtitlesDetectsASSByContent(t *testing.T) {
	got := shiftSubtitleFixture(t, "in.vtt", assFixture, 90)
	if !strings.Contains(got, "0:10:30.00") {
		t.Fatalf("ASS content in a .vtt file was not shifted:\n%s", got)
	}
}

func TestShiftSubtitlesStillShiftsWebVTT(t *testing.T) {
	got := shiftSubtitleFixture(t, "in.vtt", "WEBVTT\n\n00:12:00.000 --> 00:12:04.500\nLine.\n", 600)
	if !strings.Contains(got, "00:02:00.000 --> 00:02:04.500") {
		t.Fatalf("WebVTT was not shifted:\n%s", got)
	}
}

// The styles section has a Format line too, and it must not decide where an
// event's timings sit when [Events] has none of its own.
func TestASSShiftIgnoresTheStylesFormatLine(t *testing.T) {
	body := "[V4+ Styles]\nFormat: Start, End, Name\n\n[Events]\nDialogue: 0,0:02:00.00,0:02:03.00,Default,,0,0,0,,Hello\n"
	got := shiftSubtitleFixture(t, "in.ass", body, 60)
	if !strings.Contains(got, "Dialogue: 0,0:01:00.00,0:01:03.00,Default,,0,0,0,,Hello") {
		t.Fatalf("the styles Format line moved the timing fields:\n%s", got)
	}
}

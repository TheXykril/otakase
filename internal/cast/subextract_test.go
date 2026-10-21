package cast

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubtitleExtractWritesTheChosenTrackAsASS(t *testing.T) {
	args := BuildSubtitleExtractArgs("http://127.0.0.1:4000/abc", "", nil, SubtitleTrack{Index: 3}, nil, "", "/tmp/out/subtitles.part")
	joined := strings.Join(args, " ")
	for _, want := range []string{"-map 0:3", "-c:s ass", "-f ass", "/tmp/out/subtitles.part"} {
		if !strings.Contains(joined, want) {
			t.Errorf("extract args %q are missing %q", joined, want)
		}
	}
	if strings.Contains(joined, "-dump_attachment") {
		t.Errorf("fonts were dumped with no fonts directory: %q", joined)
	}
}

// Fonts are named here, never by the attachment's own filename tag, which
// comes from the file and could point anywhere.
func TestSubtitleExtractDumpsFontsUnderItsOwnNamesBeforeTheInput(t *testing.T) {
	args := BuildSubtitleExtractArgs("http://127.0.0.1:4000/abc", "", nil, SubtitleTrack{Index: 3}, []int{5, 6}, "/tmp/out/fonts", "/tmp/out/subtitles.part")

	input := -1
	var dumps []string
	for i, arg := range args {
		if arg == "-i" {
			input = i
		}
		if strings.HasPrefix(arg, "-dump_attachment:") {
			if input >= 0 {
				t.Fatalf("%s comes after -i, where ffmpeg does not read it as an input option", arg)
			}
			dumps = append(dumps, arg+" "+args[i+1])
		}
	}
	want := []string{
		"-dump_attachment:5 " + filepath.Join("/tmp/out/fonts", "font-5"),
		"-dump_attachment:6 " + filepath.Join("/tmp/out/fonts", "font-6"),
	}
	if strings.Join(dumps, "|") != strings.Join(want, "|") {
		t.Errorf("font dumps %v, want %v", dumps, want)
	}
}

func TestBurningWithFontsPointsTheFilterAtThem(t *testing.T) {
	args := strings.Join(BuildBurnArgsWithFonts("http://127.0.0.1:4000/abc", "", "/tmp/s.ass", "/tmp/fonts", "/tmp/out", Encoder{Name: "libx264"}, 0, nil, nil), " ")
	if !strings.Contains(args, "subtitles='/tmp/s.ass':fontsdir='/tmp/fonts'") {
		t.Errorf("burn args %q do not hand the filter the fonts", args)
	}
	without := strings.Join(BuildBurnArgsFrom("http://127.0.0.1:4000/abc", "", "/tmp/s.ass", "/tmp/out", Encoder{Name: "libx264"}, 0, nil, nil), " ")
	if strings.Contains(without, "fontsdir") {
		t.Errorf("burn args with no fonts name a fonts directory: %q", without)
	}
}

// Subtitles are sparse, so how far the read has got is told by an audio copy
// sent nowhere, reported on stdout.
func TestSubtitleExtractReportsHowFarItHasRead(t *testing.T) {
	joined := strings.Join(BuildSubtitleExtractArgs("http://127.0.0.1:4000/abc", "", nil, SubtitleTrack{Index: 3}, nil, "", "/tmp/out/subtitles.ass"), " ")
	for _, want := range []string{"-progress pipe:1", "-flush_packets 1", "-map 0:a:0? -c:a copy -f null -"} {
		if !strings.Contains(joined, want) {
			t.Errorf("extract args %q are missing %q", joined, want)
		}
	}
}

func TestParseProgressTime(t *testing.T) {
	for _, tc := range []struct {
		line string
		want float64
		ok   bool
	}{
		{"out_time_us=48855000", 48.855, true},
		{"out_time_ms=1500000", 1.5, true},
		{"out_time_us=N/A", 0, false},
		{"out_time=00:00:48.855000", 0, false},
		{"progress=continue", 0, false},
	} {
		got, ok := parseProgressTime(tc.line)
		if ok != tc.ok || got != tc.want {
			t.Errorf("parseProgressTime(%q) = %v, %t; want %v, %t", tc.line, got, ok, tc.want, tc.ok)
		}
	}
}

// A line ffmpeg is halfway through writing stays out of the copy.
func TestSnapshotStopsAtTheLastCompleteLine(t *testing.T) {
	dir := t.TempDir()
	r := &SubtitleReader{path: filepath.Join(dir, "subtitles.ass"), covered: 42}
	body := "[Events]\nDialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,One\nDialogue: 0,0:00:0"
	if err := os.WriteFile(r.path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "copy.ass")
	covered, err := r.Snapshot(dst)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dst)
	if covered != 42 || string(got) != "[Events]\nDialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,One\n" {
		t.Errorf("snapshot reached %v with %q", covered, got)
	}
}

// The whole path against a real ffmpeg, on a release-shaped file: subtitles in
// the file and a font attached. Skipped where there is no ffmpeg.
func TestSubtitleReaderReadsARealFile(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	dir := t.TempDir()
	ass := filepath.Join(dir, "in.ass")
	if err := os.WriteFile(ass, []byte("[Script Info]\nScriptType: v4.00+\n\n[V4+ Styles]\nFormat: Name, Fontname, Fontsize\nStyle: Default,Arial,20\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\nDialogue: 0,0:00:00.50,0:00:01.50,Default,,0,0,0,,Hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	font := filepath.Join(dir, "font.ttf")
	if err := os.WriteFile(font, []byte("not really a font"), 0o644); err != nil {
		t.Fatal(err)
	}
	mkv := filepath.Join(dir, "ep.mkv")
	build := exec.Command(ffmpeg, "-v", "error", "-y",
		"-f", "lavfi", "-i", "color=size=64x64:rate=10:duration=2",
		"-f", "lavfi", "-i", "sine=duration=2",
		"-i", ass, "-map", "0", "-map", "1", "-map", "2",
		"-c:v", "mpeg4", "-c:a", "mp2", "-c:s", "ass",
		"-attach", font, "-metadata:s:t:0", "mimetype=application/x-truetype-font", mkv)
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("could not build a test file: %v %s", err, out)
	}

	reader, err := StartSubtitleReader(context.Background(), ffmpeg, mkv, "", nil, SubtitleTrack{Index: 2}, []int{3}, filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	<-reader.Done()
	if err := reader.Err(); err != nil {
		t.Fatal(err)
	}
	if !math.IsInf(reader.Covered(), 1) {
		t.Errorf("a finished read reaches %v, want +Inf", reader.Covered())
	}
	copyPath := filepath.Join(dir, "copy.ass")
	if _, err := reader.Snapshot(copyPath); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(copyPath)
	if !strings.Contains(string(got), "Hello") {
		t.Errorf("the line was not read out: %q", got)
	}
	if _, err := os.Stat(filepath.Join(reader.FontsDir(), "font-3")); err != nil {
		t.Errorf("the font was not dumped: %v", err)
	}
}

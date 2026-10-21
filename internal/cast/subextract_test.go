package cast

import (
	"os"
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

func TestSubtitleFontsDirOnlyWhenThereAreFonts(t *testing.T) {
	dir := t.TempDir()
	subs := filepath.Join(dir, "subtitles.ass")
	if got := SubtitleFontsDir(subs); got != "" {
		t.Errorf("no fonts directory, got %q", got)
	}
	if err := os.MkdirAll(filepath.Join(dir, "fonts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := SubtitleFontsDir(subs); got != "" {
		t.Errorf("an empty fonts directory was offered: %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "fonts", "font-5"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := SubtitleFontsDir(subs); got != filepath.Join(dir, "fonts") {
		t.Errorf("fonts directory %q, want %q", got, filepath.Join(dir, "fonts"))
	}
}

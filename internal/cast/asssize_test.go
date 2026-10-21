package cast

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const releaseASS = `[Script Info]
PlayResY: 1080

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Roboto,60,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,3,1.5,2,10,10,10,1
Style: Speech,Roboto,50,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,2,1,2,10,10,10,1
Style: Sign-Board,Gandhi,40,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,7,10,10,10,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:02.00,Speech,,0,0,0,,Hello, there
Dialogue: 0,0:00:03.00,0:00:04.00,Speech,,0,0,0,,Again
Dialogue: 0,0:00:05.00,0:00:06.00,Sign-Board,,0,0,0,,{\pos(100,200)}CAFE
Dialogue: 0,0:00:05.00,0:00:06.00,Sign-Board,,0,0,0,,{\pos(100,300)}OPEN
Dialogue: 0,0:00:05.00,0:00:06.00,Sign-Board,,0,0,0,,{\pos(100,400)}MENU
`

// Dialogue grows -- the named style and the one most unplaced lines use --
// and the sign stays exactly as the release sized it.
func TestScaleDialogueStylesLeavesSignsAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subtitles.ass")
	if err := os.WriteFile(path, []byte(releaseASS), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ScaleDialogueStyles(path, 120); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	got := string(raw)
	for _, want := range []string{
		"Style: Default,Roboto,72,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,3.6,1.8,2,",
		"Style: Speech,Roboto,60,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,2.4,1.2,2,",
		"Style: Sign-Board,Gandhi,40,",
		"Hello, there",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("resized file is missing %q:\n%s", want, got)
		}
	}
}

func TestScaleDialogueStylesAtOneHundredChangesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subtitles.ass")
	if err := os.WriteFile(path, []byte(releaseASS), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ScaleDialogueStyles(path, 100); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != releaseASS {
		t.Error("100% rewrote the file")
	}
}

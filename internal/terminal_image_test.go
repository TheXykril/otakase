package internal

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePoster makes a file shaped like a real cover: taller than it is wide.
func writePoster(t *testing.T, width, height int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 0x80, A: 0xff})
		}
	}
	encoded, err := EncodePNG(img)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "poster.png")
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// Each protocol has to produce something the terminal will actually act on.
func TestTerminalImageRendersForEveryProtocol(t *testing.T) {
	path := writePoster(t, 460, 650)

	for _, protocol := range []TerminalImageProtocol{TerminalImageKitty, TerminalImageIterm, TerminalImageSixel} {
		rendered, err := RenderTerminalImage(path, protocol, 200, 300)
		if err != nil {
			t.Errorf("%s: %v", protocol, err)
			continue
		}
		if rendered == "" {
			t.Errorf("%s produced nothing", protocol)
			continue
		}
		// Every one of these protocols is carried by an escape sequence.
		if !strings.Contains(rendered, "\x1b") {
			t.Errorf("%s produced no escape sequence: %q", protocol, rendered[:min(40, len(rendered))])
		}
	}
}

// A terminal that cannot draw pictures is not an error, it is the common case.
func TestTerminalImageWithoutSupportIsEmptyNotAnError(t *testing.T) {
	path := writePoster(t, 100, 140)
	rendered, err := RenderTerminalImage(path, TerminalImageNone, 80, 120)
	if err != nil {
		t.Fatalf("no-protocol should not be an error: %v", err)
	}
	if rendered != "" {
		t.Errorf("expected nothing to draw, got %d bytes", len(rendered))
	}
}

// A poster is taller than it is wide. Squashing it to fill the box would be
// worse than leaving space, so the proportions have to survive.
func TestScaleKeepsProportionsAndNeverEnlarges(t *testing.T) {
	tall := image.NewRGBA(image.Rect(0, 0, 460, 650))
	scaled := scaleToFit(tall, 100, 100)
	got := scaled.Bounds()
	if got.Dx() > 100 || got.Dy() > 100 {
		t.Errorf("scaled image %dx%d escapes the box", got.Dx(), got.Dy())
	}
	sourceRatio := 460.0 / 650.0
	scaledRatio := float64(got.Dx()) / float64(got.Dy())
	if diff := sourceRatio - scaledRatio; diff > 0.02 || diff < -0.02 {
		t.Errorf("proportions changed: %.3f became %.3f", sourceRatio, scaledRatio)
	}

	// Already small enough: enlarging only blurs it.
	small := image.NewRGBA(image.Rect(0, 0, 40, 60))
	if out := scaleToFit(small, 400, 600).Bounds(); out.Dx() != 40 || out.Dy() != 60 {
		t.Errorf("a small image was resized to %dx%d", out.Dx(), out.Dy())
	}
}

// A missing or unreadable file must be reported, not drawn as garbage.
func TestTerminalImageReportsBadInput(t *testing.T) {
	if _, err := RenderTerminalImage(filepath.Join(t.TempDir(), "absent.png"), TerminalImageKitty, 10, 10); err == nil {
		t.Error("a missing file should be an error")
	}

	notAnImage := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(notAnImage, []byte("this is not a poster"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RenderTerminalImage(notAnImage, TerminalImageKitty, 10, 10); err == nil {
		t.Error("a file that is not an image should be an error")
	}

	path := writePoster(t, 10, 10)
	if _, err := RenderTerminalImage(path, TerminalImageKitty, 0, 10); err == nil {
		t.Error("a zero-width box should be an error")
	}
}

// tmux and screen multiplex the escape stream; an image sent through them lands
// in the wrong place or wedges the terminal, so detection must refuse.
func TestTerminalImageDetectionRefusesInsideTmux(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,1234,0")
	if got := DetectTerminalImageProtocol(); got != TerminalImageNone {
		t.Errorf("inside tmux the answer must be none, got %s", got)
	}
}

// The upstream helper only matches TERM=screen*, so each way of being inside a
// multiplexer has to be checked separately.
func TestMultiplexerDetectionCoversTmuxAndScreen(t *testing.T) {
	cases := []struct {
		name   string
		env    map[string]string
		inside bool
	}{
		{"modern tmux", map[string]string{"TMUX": "/tmp/tmux-1000/default,1,0", "TERM": "tmux-256color"}, true},
		{"tmux passing the outer TERM through", map[string]string{"TMUX": "/tmp/tmux-1000/default,1,0", "TERM": "xterm-kitty"}, true},
		{"tmux by TERM alone", map[string]string{"TERM": "tmux-256color"}, true},
		{"GNU screen", map[string]string{"STY": "1234.pts-0.host", "TERM": "screen"}, true},
		{"a plain terminal", map[string]string{"TERM": "xterm-kitty"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TMUX", "")
			t.Setenv("STY", "")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if got := insideMultiplexer(); got != tc.inside {
				t.Errorf("expected inside=%v, got %v", tc.inside, got)
			}
		})
	}
}

package internal

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cachedCover(t *testing.T, url string) string {
	t.Helper()
	path, err := coverCachePath(url)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodePNG(image.NewRGBA(image.Rect(0, 0, 60, 90)))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
	return path
}

// A cover already on disk is drawn, and the file is read only once: decoding
// and transmitting on every keystroke is what made the first attempts unusable.
func TestPosterSourceReadsACoverOnlyOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	url := "https://example.invalid/cover-rendered-once.jpg"
	path := cachedCover(t, url)

	source := NewPosterSource(TerminalImageKitty, 20, 15)
	option := SelectionOption{Key: "1", Thumbnail: url}

	first := source.Poster(option)
	if first == "" {
		t.Fatal("a cached cover was not drawn")
	}

	// Removing the file must not stop it being drawn, which proves the second
	// call came from memory rather than from disk.
	os.Remove(path)
	second := source.Poster(option)
	if second == "" {
		t.Error("the poster was re-read from disk instead of being remembered")
	}
	// The two differ on purpose: the first carries the image, the rest refer
	// to it.
	if second == first {
		t.Error("every frame resent the image instead of referring to it")
	}
}

// Nothing here may reach the network on the drawing path. The URL is
// unresolvable, so a synchronous fetch would hang or error; returning promptly
// with nothing is the required behaviour.
func TestPosterSourceDoesNotBlockOnAMissingCover(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	source := NewPosterSource(TerminalImageKitty, 20, 15)

	got := source.Poster(SelectionOption{Key: "1", Thumbnail: "https://unresolvable.invalid/none.jpg"})
	if got != "" {
		t.Errorf("expected nothing to draw yet, got %d bytes", len(got))
	}
}

// A terminal that cannot draw pictures, and an entry with no cover, are both
// ordinary and must cost nothing.
func TestPosterSourceIsQuietWhenThereIsNothingToDo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	url := "https://example.invalid/unsupported.jpg"
	cachedCover(t, url)

	if got := NewPosterSource(TerminalImageNone, 20, 15).Poster(SelectionOption{Thumbnail: url}); got != "" {
		t.Error("a terminal without image support should draw nothing")
	}
	if got := NewPosterSource(TerminalImageKitty, 20, 15).Poster(SelectionOption{}); got != "" {
		t.Error("an entry with no cover should draw nothing")
	}
	var nilSource *PosterSource
	if got := nilSource.Poster(SelectionOption{Thumbnail: url}); got != "" {
		t.Error("a nil source should be usable and draw nothing")
	}
}

// The cost of a redraw is what broke this twice. A poster is redrawn every time
// the cursor moves, so the sequence sent each frame has to be small; the first
// attempt sent the whole image, 464 kB at a time, and the terminal could not
// keep up.
func TestRedrawCostStaysSmall(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	url := "https://example.invalid/redraw-cost.jpg"
	cachedCover(t, url)

	source := NewPosterSource(TerminalImageKitty, 22, 16)
	option := SelectionOption{Thumbnail: url}

	first := source.Poster(option)
	if first == "" {
		t.Fatal("the cover was not drawn")
	}
	later := source.Poster(option)
	if later == "" {
		t.Fatal("the cover stopped being drawn after the first frame")
	}

	// The transmit happens once; every frame after it must be a reference.
	if len(later) > 200 {
		t.Errorf("each redraw costs %d bytes; it should reference an "+
			"already-transmitted image, not resend the image", len(later))
	}
	if len(later) >= len(first) {
		t.Errorf("the later frame (%d bytes) is no cheaper than the first (%d)", len(later), len(first))
	}
	// And it still has to erase what was there.
	if !strings.Contains(later, "a=d") {
		t.Errorf("a redraw that does not delete leaves the previous poster behind: %q", later)
	}
}

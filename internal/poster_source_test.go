package internal

import (
	"image"
	"os"
	"path/filepath"
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

// A cover already on disk should be drawn, and drawn only once: re-encoding on
// every keystroke is the difference between a smooth list and a stuttering one.
func TestPosterSourceRendersCachedCoversOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	url := "https://example.invalid/cover-rendered-once.jpg"
	path := cachedCover(t, url)

	source := NewPosterSource(TerminalImageKitty, 100, 150)
	option := SelectionOption{Key: "1", Thumbnail: url}

	first := source.Poster(option)
	if first == "" {
		t.Fatal("a cached cover was not drawn")
	}

	// Removing the file must not change the answer, which proves the second
	// call came from memory rather than from disk.
	os.Remove(path)
	if second := source.Poster(option); second != first {
		t.Error("the poster was re-rendered instead of being remembered")
	}
}

// Nothing here may reach the network on the drawing path. The URL is
// unresolvable, so a synchronous fetch would hang or error; returning promptly
// with nothing is the required behaviour.
func TestPosterSourceDoesNotBlockOnAMissingCover(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	source := NewPosterSource(TerminalImageKitty, 100, 150)

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

	if got := NewPosterSource(TerminalImageNone, 100, 150).Poster(SelectionOption{Thumbnail: url}); got != "" {
		t.Error("a terminal without image support should draw nothing")
	}
	if got := NewPosterSource(TerminalImageKitty, 100, 150).Poster(SelectionOption{}); got != "" {
		t.Error("an entry with no cover should draw nothing")
	}
	var nilSource *PosterSource
	if got := nilSource.Poster(SelectionOption{Thumbnail: url}); got != "" {
		t.Error("a nil source should be usable and draw nothing")
	}
}

package theme

import "testing"

// A dimmed colour is darker than it was, and black stays black.
func TestDimmedPullsTowardBlack(t *testing.T) {
	p := Palette{Foreground: "#ffffff", Background: "#000000", Accent: "#ff0000"}
	d := Dimmed(p, 0.25)
	if d.Foreground != "#bfbfbf" {
		t.Errorf("foreground = %s, want #bfbfbf", d.Foreground)
	}
	if d.Background != "#000000" {
		t.Errorf("background = %s, want #000000", d.Background)
	}
	if d.Accent != "#bf0000" {
		t.Errorf("accent = %s, want #bf0000", d.Accent)
	}
	if d.Red != "" {
		t.Errorf("an unset colour became %q", d.Red)
	}
}

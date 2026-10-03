package internal

import (
	"strings"
	"testing"

	"github.com/thexykril/otakase/internal/icons"
)

// The entry has to say whether casting is on, because it is the only place
// that says so: once a show is picked the menu is gone, and a viewer who
// toggled it earlier has nothing else to check.
func TestCastActionLabelCarriesItsState(t *testing.T) {
	off := castActionLabel(&Config{CastToDevice: false})
	on := castActionLabel(&Config{CastToDevice: true})

	if !strings.Contains(strings.ToLower(off), "off") {
		t.Errorf("the off label does not say so: %q", off)
	}
	if !strings.Contains(strings.ToLower(on), "on") {
		t.Errorf("the on label does not say so: %q", on)
	}
	if off == on {
		t.Errorf("both states render identically as %q", off)
	}
}

// With a device configured, naming it is more useful than "on": it tells the
// viewer where the episode is about to go.
func TestCastActionLabelNamesTheConfiguredDevice(t *testing.T) {
	label := castActionLabel(&Config{CastToDevice: true, CastDevice: "Office TV"})

	if !strings.Contains(label, "Office TV") {
		t.Errorf("the label does not name the device: %q", label)
	}
}

// The terminal menu marks it like the checkbox it is, so the state reads at a
// glance next to the other actions rather than needing to be parsed.
func TestCastActionLabelIsACheckboxInTheTerminal(t *testing.T) {
	if got := castActionCheckbox(&Config{CastToDevice: true}); !strings.HasPrefix(got, "[x]") {
		t.Errorf("an enabled cast is not ticked: %q", got)
	}
	if got := castActionCheckbox(&Config{CastToDevice: false}); !strings.HasPrefix(got, "[ ]") {
		t.Errorf("a disabled cast is not shown unticked: %q", got)
	}
}

// Toggling has to move the subtitle preference with it: casting asks for the
// hardsubbed stream, and turning casting back off must not leave that behind
// for the local playback that follows.
func TestToggleCastRestoresTheSubtitlePreference(t *testing.T) {
	config := &Config{SubStyle: "soft"}

	toggleCastToDevice(config)
	if !config.CastToDevice || config.SubStyle != "hard" {
		t.Fatalf("enabling did not take: cast=%v substyle=%q", config.CastToDevice, config.SubStyle)
	}

	toggleCastToDevice(config)
	if config.CastToDevice {
		t.Error("disabling did not take")
	}
	if config.SubStyle != "soft" {
		t.Errorf("SubStyle stayed %q after turning casting off, want the original %q", config.SubStyle, "soft")
	}
}

// The category menu is the one menu rofi and the terminal share. The state is
// in the words, so it reads with icons off, and in the icon, which is crossed
// out while casting is off. No checkbox: next to the icon it read as clutter.
func TestCastMenuLabelReadsInBothUIs(t *testing.T) {
	off := castMenuLabel(&Config{})
	on := castMenuLabel(&Config{CastToDevice: true, CastDevice: "Office TV"})

	if off != "Cast: Off" {
		t.Errorf("the off entry reads %q, want %q", off, "Cast: Off")
	}
	if on != "Cast: On | Office TV" {
		t.Errorf("the on entry reads %q, want %q", on, "Cast: On | Office TV")
	}
	if strings.Contains(off+on, "[") {
		t.Errorf("the entries still carry a checkbox: %q, %q", off, on)
	}
	if castMenuIcon(&Config{}) != icons.CastOff || castMenuIcon(&Config{CastToDevice: true}) != icons.Cast {
		t.Error("the cast icon does not follow the state")
	}
}

// CAST has to survive a MenuOrder that names it, or the entry never appears.
func TestCastAppearsInTheOrderedCategories(t *testing.T) {
	config := &Config{MenuOrder: "CURRENT,TRACKER,CAST"}

	found := false
	for _, option := range getOrderedCategories(config) {
		if option.Key == "CAST" {
			found = true
			if !strings.Contains(option.Label, "Cast") {
				t.Errorf("the CAST entry is labelled %q", option.Label)
			}
		}
	}
	if !found {
		t.Error("CAST was named in MenuOrder but produced no menu entry")
	}
}

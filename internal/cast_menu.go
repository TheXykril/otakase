package internal

import (
	"fmt"

	"github.com/thexykril/otakase/internal/icons"
)

// castSubStyleBeforeCast remembers the subtitle preference casting overrode,
// so turning casting back off restores it rather than leaving local playback
// with a preference the viewer never chose.
var castSubStyleBeforeCast string

// castActionLabel is the menu entry's text, which carries the state because it
// is the only thing that reports it: once a show is chosen the menu is gone,
// and a viewer who toggled casting earlier has nothing else to look at.
func castActionLabel(config *Config) string {
	if config == nil || !config.CastToDevice {
		return "cast: off"
	}
	if device := config.CastDevice; device != "" {
		return "cast: " + device
	}
	return "cast: on"
}

// castActionCheckbox is the same state for the terminal menu, where the other
// actions sit in a row and a tick reads faster than a word.
func castActionCheckbox(config *Config) string {
	if config != nil && config.CastToDevice {
		return "[x] " + castActionLabel(config)
	}
	return "[ ] " + castActionLabel(config)
}

// toggleCastToDevice turns casting on or off for this run.
//
// The subtitle preference moves with it. Casting asks for a provider's
// hardsubbed stream, because the device renders no subtitle format any
// provider here supplies; local playback has no such limit, so the preference
// the viewer actually set is put back when casting is turned off.
func toggleCastToDevice(config *Config) {
	if config == nil {
		return
	}

	if config.CastToDevice {
		config.CastToDevice = false
		config.SubStyle = castSubStyleBeforeCast
		Log(fmt.Sprintf("cast: disabled from the menu, SubStyle restored to %q", config.SubStyle))
		return
	}

	castSubStyleBeforeCast = config.SubStyle
	config.CastToDevice = true
	ApplyCastSubStyle(config, false)
	Log("cast: enabled from the menu")
}

// castMenuLabel is the category menu's entry for the cast toggle.
//
// The terminal menu and rofi share this menu, so both get the same words. The
// words carry the state on their own, so the entry still reads right with
// icons off; with them on, castMenuIcon shows it as well.
func castMenuLabel(config *Config) string {
	if config != nil && config.CastToDevice {
		if device := config.CastDevice; device != "" {
			return "Cast: " + device
		}
		return "Cast: On"
	}
	return "Cast: Off"
}

// castMenuIcon is the cast entry's icon, crossed out while casting is off.
func castMenuIcon(config *Config) icons.Icon {
	if config != nil && config.CastToDevice {
		return icons.Cast
	}
	return icons.CastOff
}

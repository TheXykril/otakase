package internal

import (
	"fmt"

	"github.com/thexykril/otakase/internal/rofitheme"
	"github.com/thexykril/otakase/internal/theme"
)

// ApplyThemeFromConfig resolves the colour palette named by the config and
// installs it into the UI. A theme that cannot be read is not fatal: otakase falls
// back to its own palette and logs why.
func ApplyThemeFromConfig(config *Config) theme.Palette {
	mode := theme.ModeAuto
	if config != nil {
		mode = theme.ParseMode(config.Theme)
	}

	palette, err := theme.Resolve(mode)
	if err != nil {
		Log(fmt.Sprintf("Falling back to the builtin palette: %v", err))
	}

	// Hand-picked colours sit on top of whatever was resolved, so following the
	// desktop theme and replacing one colour in it are not exclusive.
	if config != nil {
		var problems []error
		palette, problems = theme.ApplyOverrides(palette, config.ThemeOverrides)
		for _, problem := range problems {
			Log(fmt.Sprintf("ThemeOverrides: %v", problem))
		}
		// Whatever reads the active palette later -- the rofi themes are
		// written from it -- must see the same colours the menus use.
		theme.SetActive(palette)
	}

	ApplyTheme(palette)
	Log(fmt.Sprintf("Using %s colour theme %q", palette.Source, palette.Name))
	return palette
}

// WriteRofiThemes renders the rofi menu themes for the active palette, telling
// the user about any hand-edited theme it had to set aside.
func WriteRofiThemes(storagePath string) error {
	backups, err := rofitheme.WriteAllWithBackups(storagePath, theme.Active())
	for _, backup := range backups {
		Out(fmt.Sprintf("Saved your customised rofi theme to %s", backup))
		Log(fmt.Sprintf("Backed up a hand-edited rofi theme to %s", backup))
	}
	return err
}

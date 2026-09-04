package internal

import (
	"fmt"

	"github.com/wraient/curd/internal/rofitheme"
	"github.com/wraient/curd/internal/theme"
)

// ApplyThemeFromConfig resolves the colour palette named by the config and
// installs it into the UI. A theme that cannot be read is not fatal: Curd falls
// back to its own palette and logs why.
func ApplyThemeFromConfig(config *CurdConfig) theme.Palette {
	mode := theme.ModeAuto
	if config != nil {
		mode = theme.ParseMode(config.Theme)
	}

	palette, err := theme.Resolve(mode)
	if err != nil {
		Log(fmt.Sprintf("Falling back to the builtin palette: %v", err))
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
		CurdOut(fmt.Sprintf("Saved your customised rofi theme to %s", backup))
		Log(fmt.Sprintf("Backed up a hand-edited rofi theme to %s", backup))
	}
	return err
}

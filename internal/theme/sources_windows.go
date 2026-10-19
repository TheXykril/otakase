//go:build windows

package theme

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// readWindowsTheme reads whether apps use the light theme and the accent
// colour from the current user's registry.
func readWindowsTheme() (bool, string, error) {
	dark := true
	personalize, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err == nil {
		if light, _, err := personalize.GetIntegerValue("AppsUseLightTheme"); err == nil {
			dark = light == 0
		}
		personalize.Close()
	}

	dwm, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\DWM`, registry.QUERY_VALUE)
	if err != nil {
		return dark, "", fmt.Errorf("read Windows accent colour: %w", err)
	}
	defer dwm.Close()
	value, _, err := dwm.GetIntegerValue("AccentColor")
	if err != nil {
		// No accent set: Windows' default blue.
		return dark, "#0078d4", nil
	}
	return dark, windowsAccent(uint32(value)), nil
}

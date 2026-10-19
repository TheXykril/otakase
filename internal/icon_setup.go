package internal

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/thexykril/otakase/internal/icons"
)

// Icons setting values. auto draws them where the glyphs can be relied on.
const (
	IconsAuto = "auto"
	IconsOn   = "true"
	IconsOff  = "false"
)

func iconsMode(config *Config) string {
	if config == nil {
		return IconsAuto
	}
	switch strings.ToLower(strings.TrimSpace(config.Icons)) {
	case "true", "on", "yes":
		return IconsOn
	case "false", "off", "no":
		return IconsOff
	default:
		return IconsAuto
	}
}

// SetupIcons decides whether menus draw icons, installing the bundled icon
// font first where fontconfig can use it. It runs in the background: the font
// check shells out to fontconfig, and a launch should not wait on that. The
// first menu waits briefly for the answer (see icons.Enabled).
func SetupIcons(config *Config) {
	mode := iconsMode(config)
	if mode == IconsOff {
		icons.Resolve(false)
		return
	}
	rofi := config != nil && config.RofiSelection
	go func() {
		if wrote, err := icons.InstallFont(); err != nil {
			Log(fmt.Sprintf("Icons: installing the icon font: %v", err))
		} else if wrote {
			Log("Icons: installed the icon font")
		}
		if mode == IconsOn {
			icons.Resolve(true)
			return
		}
		on := iconsAutoAllowed(rofi) && icons.FontAvailable()
		Log(fmt.Sprintf("Icons: auto -> %t", on))
		icons.Resolve(on)
	}()
}

// iconsAutoAllowed rules out the places a font check here says nothing about
// what will draw the text.
//
// rofi draws with this machine's fontconfig, so the check holds. A terminal
// is only trustworthy when it is a local one: over SSH, or in WSL, the glyphs
// are drawn by another machine's (or Windows') fonts, and the Linux console
// has no way to show them at all.
func iconsAutoAllowed(rofi bool) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return false
	}
	if rofi {
		return true
	}
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" {
		return false
	}
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return false
	}
	if os.Getenv("TERM") == "linux" {
		return false
	}
	return true
}

package internal

import (
	"runtime"

	"github.com/thexykril/otakase/internal/appicon"
	"github.com/thexykril/otakase/internal/mpvskin"
)

// mpvWindowArgs name the player window after otakase on Linux, so the
// desktop matches it to otakase's app-menu entry and shows otakase's icon on
// it instead of mpv's. Wayland reads the app id; X11 the window's instance
// name. Each flag is passed only when this mpv has it, as a build without
// Wayland or X11 refuses to start on the other's option, and never over one
// the user set in MpvArgs.
//
// Windows and macOS take a window's icon from the program file, which here
// is mpv's own, so there is nothing to pass there.
func mpvWindowArgs(config *Config, binary string) []string {
	if runtime.GOOS != "linux" || !mpvskin.IsMPVBinary(binary) {
		return nil
	}
	var userArgs []string
	if config != nil {
		userArgs = config.MpvArgs
	}
	var args []string
	for _, name := range []string{"wayland-app-id", "x11-name"} {
		if hasMPVFlag(userArgs, "--"+name) || !mpvskin.HasOption(binary, name) {
			continue
		}
		args = append(args, "--"+name+"="+appicon.Name)
	}
	return args
}

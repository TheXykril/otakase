package internal

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"golang.org/x/term"
)

// A desktop opens otakase:// links through otakase-link.desktop
// (internal/appicon): `otakase -rofi %u` with no terminal, so a Play button
// does not leave an empty terminal window behind. LinkMenus picks where the
// link's menus go: "rofi" (the default) or "terminal". With "terminal", or
// without rofi installed, otakase reopens itself in a terminal.

// linkLaunchHasTerminal reports whether this process can draw its menus where
// it was started. Swapped in tests.
var linkLaunchHasTerminal = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// LinkMenusTerminal is the LinkMenus value that opens links in a terminal.
const LinkMenusTerminal = "terminal"

// PrepareLinkLaunch readies a link launch that has no terminal: its menus go
// to rofi, or, when LinkMenus asks for the terminal or rofi is not
// installed, otakase reopens in a terminal. It reports whether it reopened;
// this process is then done and should exit. In a terminal nothing changes.
func PrepareLinkLaunch(config *Config) bool {
	return prepareLinkLaunch(config, runtime.GOOS, exec.LookPath, startInTerminal)
}

func prepareLinkLaunch(config *Config, goos string, lookPath func(string) (string, error), relaunch func(*Config) error) bool {
	if goos != "linux" || linkLaunchHasTerminal() {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(config.LinkMenus), LinkMenusTerminal) {
		if _, err := lookPath("rofi"); err == nil {
			config.RofiSelection = true
			return false
		}
	}
	if err := relaunch(config); err != nil {
		Log(fmt.Sprintf("Opening a link in a terminal: %v", err))
		return false
	}
	return true
}

// startInTerminal runs this otakase again, with the same arguments and the
// terminal menus, in a terminal of its own.
func startInTerminal(config *Config) error {
	terminal, err := resolveCastTerminal(config, exec.LookPath)
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	// -no-rofi goes first: flags after the link would be read as arguments.
	args := append(append(terminal[1:], "-e", self, "-no-rofi"), os.Args[1:]...)
	command := exec.Command(terminal[0], args...)
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

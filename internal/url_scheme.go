package internal

import "strings"

// The otakase:// registration on Windows lives under
// HKCU\Software\Classes\otakase; the installer writes it, and the app writes
// it itself when it is missing (url_scheme_windows.go). Linux gets it from the
// desktop entry (internal/appicon). macOS needs an app bundle to own a URL
// scheme, which a plain command-line binary is not, so it has none.

// urlSchemeCommand is the open command registered for otakase:// links: the
// link arrives as the first argument.
func urlSchemeCommand(exe string) string {
	return `"` + exe + `" "%1"`
}

// commandExecutable is the program a registered open command starts, quoted
// or not.
func commandExecutable(command string) string {
	command = strings.TrimSpace(command)
	if strings.HasPrefix(command, `"`) {
		if end := strings.Index(command[1:], `"`); end >= 0 {
			return command[1 : end+1]
		}
		return ""
	}
	if i := strings.Index(command, " "); i >= 0 {
		return command[:i]
	}
	return command
}

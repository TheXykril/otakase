package internal

import (
	"os/exec"
	"runtime"
)

// The update prompt is a short summary over the choices, with the full
// changelog one row away. The summary is a handful of headlines so the
// choices stay on screen however many releases were skipped; reading
// everything is a choice of its own, on a page that scrolls.

const ()

// openInBrowser opens a link with the desktop's default handler.
func openInBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

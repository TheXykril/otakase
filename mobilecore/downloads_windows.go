package mobilecore

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps ffmpeg from opening a console window over the app.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

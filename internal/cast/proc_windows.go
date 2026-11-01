package cast

import (
	"os/exec"
	"syscall"
)

// quiet keeps ffmpeg from opening a console window when the caller is a
// windowed app with no console of its own.
func quiet(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return cmd
}

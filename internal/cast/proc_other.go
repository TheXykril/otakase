//go:build !windows

package cast

import "os/exec"

// quiet is a no-op off Windows; see proc_windows.go.
func quiet(cmd *exec.Cmd) *exec.Cmd { return cmd }

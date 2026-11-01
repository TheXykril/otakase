//go:build !windows

package mobilecore

import "os/exec"

func hideWindow(*exec.Cmd) {}

//go:build !windows

package theme

import "fmt"

func readWindowsTheme() (bool, string, error) {
	return true, "", fmt.Errorf("the Windows theme can only be read on Windows")
}

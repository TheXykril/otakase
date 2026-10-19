package internal

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// GetUserInputFromRofi asks for a line of text in rofi, with question shown
// above the entry. Dismissing rofi is an error, as is any other failure to run it.
func GetUserInputFromRofi(question string) (string, error) {
	// A menu on screen answers "is it still working" by itself.
	defer suspendBusy()()

	theme := filepath.Join(GetStoragePath(), "userinput.rasi")
	answer, err := exec.Command("rofi", "-dmenu", "-theme", theme, "-p", "Input", "-mesg", question).Output()
	if err != nil {
		return "", fmt.Errorf("rofi input: %w", err)
	}
	return strings.TrimSpace(string(answer)), nil
}

package internal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"
)

// errPasswordCancelled is a password dialog the user closed. That is their
// answer, so nothing asks again in another way.
var errPasswordCancelled = errors.New("password dialog cancelled")

func hasDisplay() bool {
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

func stdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func globalRofiSelection() bool {
	cfg := GetGlobalConfig()
	return cfg != nil && cfg.RofiSelection
}

// preferGUIPasswordPrompt decides whether to open a desktop password dialog:
// always in rofi mode, never in a terminal session (`otakase -u` asks there
// even with a display around), and otherwise when there is a display, as for
// a desktop launcher started without a terminal.
func preferGUIPasswordPrompt() bool {
	if globalRofiSelection() {
		return true
	}
	return !stdinIsTerminal() && hasDisplay()
}

// passwordDialog is a desktop tool that can ask for a password.
type passwordDialog struct {
	name string
	args []string
}

// passwordDialogs are tried in order: zenity and yad for GTK desktops,
// kdialog for KDE.
func passwordDialogs(title, prompt string) []passwordDialog {
	return []passwordDialog{
		{"zenity", []string{"--password", "--title=" + title, "--text=" + prompt}},
		{"yad", []string{"--entry", "--hide-text", "--title=" + title, "--text=" + prompt, "--button=OK:0", "--button=Cancel:1"}},
		{"kdialog", []string{"--title", title, "--password", prompt}},
	}
}

// promptSudoPasswordGUI asks with the first password dialog installed.
// title is the dialog's window title.
func promptSudoPasswordGUI(title, prompt string) (string, error) {
	for _, dialog := range passwordDialogs(title, prompt) {
		bin, err := exec.LookPath(dialog.name)
		if err != nil {
			continue
		}
		out, err := exec.Command(bin, dialog.args...).Output()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// All three exit non-zero when the user cancels.
			return "", errPasswordCancelled
		}
		if err != nil {
			Log("password dialog " + dialog.name + " failed: " + err.Error())
			continue
		}
		password := strings.TrimRight(string(out), "\r\n")
		if password == "" {
			return "", errors.New("empty password")
		}
		Log("Collected sudo password via " + dialog.name + " dialog")
		return password, nil
	}
	return "", errors.New("no GUI password dialog available (tried zenity, yad, kdialog)")
}

// promptSudoPasswordTitled asks for the sudo password, in a desktop dialog
// titled title when preferGUIPasswordPrompt says so and in the terminal
// otherwise, or when no dialog could be shown.
func promptSudoPasswordTitled(title, prompt string) (string, error) {
	if preferGUIPasswordPrompt() {
		password, err := promptSudoPasswordGUI(title, prompt)
		if err == nil {
			return password, nil
		}
		Log(fmt.Sprintf("GUI password prompt unavailable (%v)", err))
		if errors.Is(err, errPasswordCancelled) {
			return "", err
		}
	}

	if !stdinIsTerminal() {
		return "", errors.New("no terminal available for password entry; install zenity/yad/kdialog for a GUI prompt")
	}
	fmt.Fprint(os.Stderr, strings.TrimSuffix(prompt, " ")+" ")
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	// The Enter that ended the password is not echoed.
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(password), nil
}

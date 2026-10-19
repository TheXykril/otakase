package internal

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// EDITOR almost always carries arguments -- "code --wait", "subl -w",
// "omarchy-launch-editor --inline" -- but the whole string was being passed to
// exec.Command as the executable name, so `otakase -e` failed with:
//
//	exec: "omarchy-launch-editor --inline": executable file not found in $PATH
//
// The value has to be split the way a shell would before it can be run.

// splitEditorCommand splits an EDITOR/VISUAL value into a command and its
// arguments, honouring single and double quotes so a path containing spaces
// survives.
func splitEditorCommand(value string) []string {
	var (
		parts   []string
		current strings.Builder
		quote   rune
		escaped bool
		started bool
	)

	flush := func() {
		if started {
			parts = append(parts, current.String())
			current.Reset()
			started = false
		}
	}

	for _, r := range value {
		switch {
		case escaped:
			current.WriteRune(r)
			started = true
			escaped = false
		case r == '\\' && quote != '\'':
			escaped = true
			started = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
			started = true
		case r == '\'' || r == '"':
			quote = r
			started = true
		case r == ' ' || r == '\t':
			flush()
		default:
			current.WriteRune(r)
			started = true
		}
	}
	flush()

	return parts
}

// defaultEditor picks an editor when neither VISUAL nor EDITOR is set.
func defaultEditor() string {
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("notepad++"); err == nil {
			return "notepad++"
		}
		return "notepad.exe"
	}
	for _, candidate := range []string{"nvim", "vim", "nano"} {
		if _, err := exec.LookPath(candidate); err == nil {
			return candidate
		}
	}
	return "nano"
}

// resolveEditorCommand returns the command and arguments to open path with.
func resolveEditorCommand(path string) (string, []string, error) {
	// VISUAL wins over EDITOR by convention: EDITOR may name a line editor,
	// VISUAL a full-screen one.
	value := strings.TrimSpace(os.Getenv("VISUAL"))
	if value == "" {
		value = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if value == "" {
		value = defaultEditor()
	}

	parts := splitEditorCommand(value)
	// A quoted-empty value such as EDITOR='""' splits to a single empty string,
	// which would otherwise become an empty executable name.
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		parts = splitEditorCommand(defaultEditor())
	}
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return "", nil, fmt.Errorf("no editor configured; set $EDITOR or $VISUAL")
	}

	return parts[0], append(parts[1:], path), nil
}

// EditConfig opens the config file in the user's editor.
func EditConfig(configFilePath string) {
	name, args, err := resolveEditorCommand(configFilePath)
	if err != nil {
		Out(fmt.Sprintf("Error opening config file: %v", err))
		return
	}

	if _, err := exec.LookPath(name); err != nil {
		Out(fmt.Sprintf("Editor %q not found. Set $EDITOR or $VISUAL to an editor on your PATH.", name))
		Log(fmt.Sprintf("Editor lookup failed for %q: %v", name, err))
		return
	}

	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		Out(fmt.Sprintf("Error opening config file: %v", err))
		return
	}

	Out("Config file edited successfully.")
}

//go:build windows

package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const urlSchemeKey = `Software\Classes\` + OpenLinkScheme

// RegisterURLScheme makes otakase:// links open this otakase.exe, for the
// current user only, when they are not registered yet or point at a program
// that is gone. A working registration -- the installer's, or another copy's
// -- is left as it is. Best effort: a failure only means links do not open.
func RegisterURLScheme() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	// `go run` and tests run from a temporary build that is about to vanish.
	if tmp := os.TempDir(); tmp != "" && strings.HasPrefix(strings.ToLower(exe), strings.ToLower(filepath.Clean(tmp)+string(filepath.Separator))) {
		return
	}
	if current := registeredSchemeExecutable(); current != "" {
		if info, err := os.Stat(current); err == nil && !info.IsDir() {
			return
		}
	}
	if err := writeURLScheme(exe); err != nil {
		Log(fmt.Sprintf("Registering otakase:// links: %v", err))
		return
	}
	Log("Registered otakase:// links for " + exe)
}

func registeredSchemeExecutable() string {
	k, err := registry.OpenKey(registry.CURRENT_USER, urlSchemeKey+`\shell\open\command`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	command, _, err := k.GetStringValue("")
	if err != nil {
		return ""
	}
	return commandExecutable(command)
}

func writeURLScheme(exe string) error {
	set := func(path string, values map[string]string) error {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer k.Close()
		for name, value := range values {
			if err := k.SetStringValue(name, value); err != nil {
				return err
			}
		}
		return nil
	}
	if err := set(urlSchemeKey, map[string]string{"": "URL:Otakase", "URL Protocol": ""}); err != nil {
		return err
	}
	if err := set(urlSchemeKey+`\DefaultIcon`, map[string]string{"": `"` + exe + `",0`}); err != nil {
		return err
	}
	return set(urlSchemeKey+`\shell\open\command`, map[string]string{"": urlSchemeCommand(exe)})
}

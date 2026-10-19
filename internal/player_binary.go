package internal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// iinaAppBinary is IINA's own binary, for when its command-line helper is not
// on PATH.
const iinaAppBinary = "/Applications/IINA.app/Contents/MacOS/IINA"

// getBundledMPVPath is where the Windows build ships mpv: bin\mpv.exe next
// to otakase.exe.
func getBundledMPVPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "bin", "mpv.exe"), nil
}

// findBinary looks a bare name up on PATH and checks that a path exists.
func findBinary(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("empty binary name")
	}
	if !filepath.IsAbs(name) && !strings.ContainsAny(name, `/\`) {
		return exec.LookPath(name)
	}
	if _, err := os.Stat(name); err != nil {
		return "", fmt.Errorf("binary path not found: %s", name)
	}
	return name, nil
}

// findMPV prefers the bundled mpv on Windows, then whatever is on PATH.
func findMPV() (string, error) {
	if runtime.GOOS == "windows" {
		if bundled, err := getBundledMPVPath(); err == nil {
			if _, err := os.Stat(bundled); err == nil {
				return bundled, nil
			}
		}
	}
	return findBinary("mpv")
}

// resolvePlayer finds the configured player and returns its binary and the
// player name in effect. A player that cannot be found falls back to mpv,
// with a warning when the user asked for something else.
func resolvePlayer(configured string) (binary, name string, err error) {
	name = strings.TrimSpace(configured)
	if name == "" {
		name = "mpv"
	}

	candidates := []string{name}
	if strings.EqualFold(name, "iina") {
		candidates = append(candidates, "iina")
		if runtime.GOOS == "darwin" {
			candidates = append(candidates, iinaAppBinary)
		}
	}
	for _, candidate := range candidates {
		if path, err := findBinary(candidate); err == nil {
			return path, name, nil
		}
	}

	mpvPath, err := findMPV()
	if err != nil {
		return "", "", fmt.Errorf("configured player %q was not found and fallback to mpv failed: %w", name, err)
	}
	if !strings.EqualFold(name, "mpv") {
		warning := fmt.Sprintf("Configured player '%s' was not found. Falling back to mpv.", name)
		Out(warning)
		Log(warning)
	}
	return mpvPath, "mpv", nil
}

func isIINAPlayer(name, binary string) bool {
	base := strings.TrimSuffix(filepath.Base(binary), filepath.Ext(binary))
	return strings.EqualFold(strings.TrimSpace(name), "iina") || strings.EqualFold(base, "iina")
}

// translateMPVArgsForIINA rewrites --name options as --mpv-name, which is how
// IINA passes options through to the mpv inside it.
func translateMPVArgsForIINA(args []string) []string {
	out := make([]string, len(args))
	for i, arg := range args {
		if rest, ok := strings.CutPrefix(arg, "--"); ok && !strings.HasPrefix(rest, "mpv-") {
			arg = "--mpv-" + rest
		}
		out[i] = arg
	}
	return out
}

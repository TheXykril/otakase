package icons

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// fontData is Symbols Nerd Font Mono cut down to the glyphs in All, renamed
// "Otakase Symbols" so it never stands in for a full Nerd Font a user
// installed. Licence: font/LICENSE (MIT). Regenerate with Build/icon-font.py.
//
//go:embed font/OtakaseSymbols.ttf
var fontData []byte

const fontFileName = "OtakaseSymbols.ttf"

// fontconfigSystem reports whether this OS finds fonts through fontconfig,
// which is what lets rofi and most terminals fall back to the bundled font for
// a glyph their own font lacks.
func fontconfigSystem() bool {
	switch runtime.GOOS {
	case "linux", "freebsd", "openbsd", "netbsd", "dragonfly":
		return true
	}
	return false
}

// userFontDir is where fontconfig looks for a user's own fonts.
func userFontDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); dir != "" {
		return filepath.Join(dir, "fonts"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "fonts"), nil
}

// InstallFont puts the bundled font in the user's font directory, replacing
// an older copy, and refreshes fontconfig's cache for it. It reports whether
// anything was written; a copy already current is left alone, so this costs a
// file read on every launch after the first.
func InstallFont() (bool, error) {
	if !fontconfigSystem() {
		return false, nil
	}
	base, err := userFontDir()
	if err != nil {
		return false, err
	}
	dir := filepath.Join(base, "otakase")
	path := filepath.Join(dir, fontFileName)
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, fontData) {
		return false, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, fontData, 0o644); err != nil {
		return false, err
	}
	// Only this directory: a full fc-cache can take seconds on a large font
	// collection. A missing fc-cache is not fatal; fontconfig rescans on its
	// own when it notices the directory changed.
	if fcCache, err := exec.LookPath("fc-cache"); err == nil {
		if out, err := exec.Command(fcCache, "-f", dir).CombinedOutput(); err != nil {
			return true, fmt.Errorf("fc-cache: %v: %s", err, strings.TrimSpace(string(out)))
		}
	}
	return true, nil
}

// FontAvailable reports whether fontconfig has a font with the icon glyphs:
// the bundled one, or any Nerd Font the user has. Without fontconfig there is
// no way to ask, so it reports false.
func FontAvailable() bool {
	if !fontconfigSystem() {
		return false
	}
	fcList, err := exec.LookPath("fc-list")
	if err != nil {
		return false
	}
	// Asking for the first icon is enough: every font that has one of these
	// glyphs is a Nerd Font build, and they all carry the whole set.
	query := fmt.Sprintf(":charset=%x", rune(All[0]))
	out, err := exec.Command(fcList, query, "family").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

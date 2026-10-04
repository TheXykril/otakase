// Package theme resolves the colour palette otakase draws its menus with.
//
// On Omarchy the active theme is a directory of generated config fragments under
// ~/.local/state/omarchy/current/theme, with colors.toml as the canonical source
// every other fragment is rendered from. Reading that file lets otakase match the
// rest of the desktop instead of shipping its own fixed palette.
//
// Nothing here writes to the Omarchy theme; it is read-only.
package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Source says where a palette came from.
type Source string

const (
	// SourceBuiltin is otakase's own palette, used off Omarchy or when disabled.
	SourceBuiltin Source = "builtin"
	// SourceOmarchy is the user's active Omarchy theme.
	SourceOmarchy Source = "omarchy"
	// SourceWal is a pywal colors.json (also written by wallust and matugen).
	SourceWal Source = "wal"
	// SourceBase16 is a base16 scheme, from tinty, stylix or a file.
	SourceBase16 Source = "base16"
	// SourceKDE is the KDE Plasma colour scheme.
	SourceKDE Source = "kde"
	// SourceGNOME is GNOME's dark style and accent colour.
	SourceGNOME Source = "gnome"
	// SourceMacOS is macOS's appearance and accent colour.
	SourceMacOS Source = "macos"
	// SourceWindows is Windows' app mode and accent colour.
	SourceWindows Source = "windows"
	// SourceFile is a palette file named by ThemeFile.
	SourceFile Source = "file"
)

// Palette is the resolved set of colours the UI draws with. Every field is a
// "#rrggbb" string so it can be handed straight to lipgloss or a rofi theme.
type Palette struct {
	Name   string
	Source Source
	// Dark reports whether the palette is a dark theme.
	Dark bool

	Background        string
	DarkBackground    string
	LighterBackground string
	Foreground        string
	DarkForeground    string
	BrightForeground  string

	Accent    string
	Selection string
	Muted     string

	Red     string
	Green   string
	Yellow  string
	Blue    string
	Magenta string
	Cyan    string
}

// Builtin is otakase's original palette, kept as the fallback so behaviour off
// Omarchy is unchanged.
func Builtin() Palette {
	return Palette{
		Name:   "otakase",
		Source: SourceBuiltin,
		Dark:   true,

		Background:        "#000000",
		DarkBackground:    "#000000",
		LighterBackground: "#333333",
		Foreground:        "#E6E6FA",
		DarkForeground:    "#9A9A9A",
		BrightForeground:  "#FFFFFF",

		Accent:    "#7CB9E8",
		Selection: "#4A90E2",
		Muted:     "#9A9A9A",

		Red:     "#FF6B6B",
		Green:   "#98FB98",
		Yellow:  "#FFD700",
		Blue:    "#7CB9E8",
		Magenta: "#FF69B4",
		Cyan:    "#6EC6FF",
	}
}

// BuiltinLight is the light counterpart of Builtin, used when the desktop is in
// light mode and only gives an accent colour, and to fill gaps in light themes.
func BuiltinLight() Palette {
	return Palette{
		Name:   "otakase light",
		Source: SourceBuiltin,
		Dark:   false,

		Background:        "#fafafa",
		DarkBackground:    "#eeeeee",
		LighterBackground: "#e2e2e2",
		Foreground:        "#2b2b3a",
		DarkForeground:    "#6b6b6b",
		BrightForeground:  "#000000",

		Accent:    "#1f6fd1",
		Selection: "#1f6fd1",
		Muted:     "#6b6b6b",

		Red:     "#c4302b",
		Green:   "#2e7d32",
		Yellow:  "#9a6700",
		Blue:    "#1f6fd1",
		Magenta: "#b0358f",
		Cyan:    "#0f7a94",
	}
}

// withAccent is the builtin palette for a brightness with the desktop's accent
// colour in place of otakase's blue. Desktops that only expose an accent and a
// dark/light switch (GNOME, macOS, Windows) resolve through it.
func withAccent(dark bool, accent, name string, source Source) Palette {
	palette := Builtin()
	if !dark {
		palette = BuiltinLight()
	}
	palette.Name = name
	palette.Source = source
	if isHexColor(accent) {
		palette.Accent = normalizeHex(accent)
		palette.Selection = normalizeHex(accent)
	}
	return palette
}

// OmarchyThemePath is the directory Omarchy links the active theme into.
func OmarchyThemePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "omarchy", "current", "theme")
}

// omarchyThemeName reads the active theme's slug, e.g. "solitude".
func omarchyThemeName() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(home, ".local", "state", "omarchy", "current", "theme.name"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// Available reports whether an Omarchy theme can be read on this machine.
func Available() bool {
	path := OmarchyThemePath()
	if path == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(path, "colors.toml"))
	return err == nil && !info.IsDir()
}

// parseColorsTOML reads the flat `key = "value"` pairs from an Omarchy
// colors.toml. The file is a simple key/value list with no tables or arrays, so
// this avoids taking on a TOML dependency for a dozen lines of text.
func parseColorsTOML(content string) map[string]string {
	values := make(map[string]string, 32)

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		// Trim a trailing comment, but not a '#' inside a quoted colour.
		key, rawValue, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		rawValue = strings.TrimSpace(rawValue)
		if key == "" || rawValue == "" {
			continue
		}
		if unquoted, err := strconv.Unquote(rawValue); err == nil {
			rawValue = unquoted
		} else {
			// Unquoted values (rare) may carry a trailing comment.
			if idx := strings.Index(rawValue, "#"); idx > 0 {
				rawValue = strings.TrimSpace(rawValue[:idx])
			}
			rawValue = strings.Trim(rawValue, `"'`)
		}
		values[key] = rawValue
	}

	return values
}

// LoadOmarchy reads the active Omarchy theme. Any colour the theme omits falls
// back to otakase's builtin palette, so a sparse or unusual theme still renders.
func LoadOmarchy() (Palette, error) {
	path := OmarchyThemePath()
	if path == "" {
		return Builtin(), fmt.Errorf("cannot locate the Omarchy theme directory")
	}

	raw, err := os.ReadFile(filepath.Join(path, "colors.toml"))
	if err != nil {
		return Builtin(), fmt.Errorf("read Omarchy colors.toml: %w", err)
	}

	name := omarchyThemeName()
	if name == "" {
		name = "omarchy"
	}
	return paletteFromValues(parseColorsTOML(string(raw)), name, SourceOmarchy), nil
}

// paletteFromValues builds a palette from Omarchy-style colour names
// (background, accent, red, ...). Every other source is translated into these
// names first, so this is the one place a role is chosen. Anything missing
// falls back to the builtin palette of the same brightness.
func paletteFromValues(values map[string]string, name string, source Source) Palette {
	dark := true
	switch strings.ToLower(strings.TrimSpace(values["mode"])) {
	case "light":
		dark = false
	case "dark":
	default:
		if bg := values["background"]; isHexColor(bg) {
			dark = relativeLuminance(bg) < 0.4
		}
	}
	fallback := Builtin()
	if !dark {
		fallback = BuiltinLight()
	}

	pick := func(fallbackValue string, keys ...string) string {
		for _, key := range keys {
			if color, ok := values[key]; ok && isHexColor(color) {
				return normalizeHex(color)
			}
		}
		return fallbackValue
	}

	return Palette{
		Name:   name,
		Source: source,
		Dark:   dark,

		Background:        pick(fallback.Background, "background"),
		DarkBackground:    pick(fallback.DarkBackground, "dark_background", "darker_background", "background"),
		LighterBackground: pick(fallback.LighterBackground, "lighter_background", "selection", "background"),
		Foreground:        pick(fallback.Foreground, "foreground"),
		DarkForeground:    pick(fallback.DarkForeground, "dark_foreground", "muted"),
		BrightForeground:  pick(fallback.BrightForeground, "bright_foreground", "light_foreground", "foreground"),

		Accent:    pick(fallback.Accent, "accent", "blue"),
		Selection: pick(fallback.Selection, "selection", "accent"),
		Muted:     pick(fallback.Muted, "muted", "dark_foreground"),

		Red:     pick(fallback.Red, "bright_red", "red"),
		Green:   pick(fallback.Green, "green", "bright_green"),
		Yellow:  pick(fallback.Yellow, "yellow", "bright_yellow"),
		Blue:    pick(fallback.Blue, "blue", "bright_blue"),
		Magenta: pick(fallback.Magenta, "magenta", "bright_magenta"),
		Cyan:    pick(fallback.Cyan, "cyan", "bright_cyan"),
	}
}

var (
	activeMu sync.RWMutex
	active   = Builtin()
)

// Mode selects which palette to resolve.
type Mode string

const (
	// ModeAuto follows the first desktop theme it finds, see autoOrder.
	ModeAuto Mode = "auto"
	// ModeOmarchy forces the Omarchy theme, falling back if it cannot be read.
	ModeOmarchy Mode = "omarchy"
	// ModeWal forces pywal's colors.json (wallust and matugen can write it too).
	ModeWal Mode = "wal"
	// ModeBase16 forces the current base16 scheme from tinty or stylix.
	ModeBase16 Mode = "base16"
	// ModeKDE forces the KDE Plasma colour scheme.
	ModeKDE Mode = "kde"
	// ModeGNOME forces GNOME's dark style and accent colour.
	ModeGNOME Mode = "gnome"
	// ModeMacOS forces macOS's appearance and accent colour.
	ModeMacOS Mode = "macos"
	// ModeWindows forces Windows' app mode and accent colour.
	ModeWindows Mode = "windows"
	// ModeBuiltin forces otakase's own palette.
	ModeBuiltin Mode = "builtin"
)

// ParseMode maps a config value onto a Mode, defaulting to auto.
func ParseMode(raw string) Mode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "builtin", "curd", "none", "off", "false":
		return ModeBuiltin
	case "omarchy":
		return ModeOmarchy
	case "wal", "pywal", "wallust", "matugen":
		return ModeWal
	case "base16", "tinty", "stylix":
		return ModeBase16
	case "kde", "plasma":
		return ModeKDE
	case "gnome", "gtk", "adwaita":
		return ModeGNOME
	case "macos", "mac", "darwin":
		return ModeMacOS
	case "windows", "win":
		return ModeWindows
	default:
		return ModeAuto
	}
}

// Resolve picks the palette for a mode and installs it as the active one. A
// non-empty file is read instead of auto-detecting. The returned error is
// advisory: a palette is always returned.
func Resolve(mode Mode, file string) (Palette, error) {
	palette, err := resolve(mode, file)

	activeMu.Lock()
	active = palette
	activeMu.Unlock()
	return palette, err
}

func resolve(mode Mode, file string) (Palette, error) {
	if mode == ModeBuiltin {
		return Builtin(), nil
	}
	if mode == ModeAuto && strings.TrimSpace(file) != "" {
		return LoadFile(file)
	}
	if source, ok := sources[mode]; ok {
		palette, err := source.load()
		if err != nil {
			return Builtin(), err
		}
		return palette, nil
	}
	// A source that looks present but will not read is worth a log line, but
	// the next one down the list is still a better match than the builtin.
	var firstErr error
	for _, candidate := range autoOrder {
		source := sources[candidate]
		if !source.available() {
			continue
		}
		palette, err := source.load()
		if err == nil {
			return palette, firstErr
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return Builtin(), firstErr
}

// Active returns the palette currently installed.
func Active() Palette {
	activeMu.RLock()
	defer activeMu.RUnlock()
	return active
}

// SetActive installs a palette directly. Intended for tests and previews.
func SetActive(palette Palette) {
	activeMu.Lock()
	active = palette
	activeMu.Unlock()
}

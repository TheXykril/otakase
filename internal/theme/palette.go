// Package theme resolves the colour palette Curd draws its menus with.
//
// On Omarchy the active theme is a directory of generated config fragments under
// ~/.local/state/omarchy/current/theme, with colors.toml as the canonical source
// every other fragment is rendered from. Reading that file lets Curd match the
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
	// SourceBuiltin is Curd's own palette, used off Omarchy or when disabled.
	SourceBuiltin Source = "builtin"
	// SourceOmarchy is the user's active Omarchy theme.
	SourceOmarchy Source = "omarchy"
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

// Builtin is Curd's original palette, kept as the fallback so behaviour off
// Omarchy is unchanged.
func Builtin() Palette {
	return Palette{
		Name:   "curd",
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
// back to Curd's builtin palette, so a sparse or unusual theme still renders.
func LoadOmarchy() (Palette, error) {
	path := OmarchyThemePath()
	if path == "" {
		return Builtin(), fmt.Errorf("cannot locate the Omarchy theme directory")
	}

	raw, err := os.ReadFile(filepath.Join(path, "colors.toml"))
	if err != nil {
		return Builtin(), fmt.Errorf("read Omarchy colors.toml: %w", err)
	}

	values := parseColorsTOML(string(raw))
	fallback := Builtin()

	pick := func(fallbackValue string, keys ...string) string {
		for _, key := range keys {
			if color, ok := values[key]; ok && isHexColor(color) {
				return normalizeHex(color)
			}
		}
		return fallbackValue
	}

	name := omarchyThemeName()
	if name == "" {
		name = "omarchy"
	}

	palette := Palette{
		Name:   name,
		Source: SourceOmarchy,
		Dark:   !strings.EqualFold(strings.TrimSpace(values["mode"]), "light"),

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

	return palette, nil
}

var (
	activeMu sync.RWMutex
	active   = Builtin()
)

// Mode selects which palette to resolve.
type Mode string

const (
	// ModeAuto uses the Omarchy theme when one is present.
	ModeAuto Mode = "auto"
	// ModeOmarchy forces the Omarchy theme, falling back if it cannot be read.
	ModeOmarchy Mode = "omarchy"
	// ModeBuiltin forces Curd's own palette.
	ModeBuiltin Mode = "builtin"
)

// ParseMode maps a config value onto a Mode, defaulting to auto.
func ParseMode(raw string) Mode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "builtin", "curd", "none", "off", "false":
		return ModeBuiltin
	case "omarchy", "system":
		return ModeOmarchy
	default:
		return ModeAuto
	}
}

// Resolve picks the palette for a mode and installs it as the active one. The
// returned error is advisory: a palette is always returned.
func Resolve(mode Mode) (Palette, error) {
	var (
		palette = Builtin()
		err     error
	)

	switch mode {
	case ModeBuiltin:
	case ModeOmarchy:
		palette, err = LoadOmarchy()
	default:
		if Available() {
			palette, err = LoadOmarchy()
		}
	}

	activeMu.Lock()
	active = palette
	activeMu.Unlock()
	return palette, err
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

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

// Builtin is otakase's own palette, Shu: warm near-black, cream text and a
// vermilion accent. It is the fallback whenever no desktop theme is found.
func Builtin() Palette {
	return Palette{
		Name:   "otakase",
		Source: SourceBuiltin,
		Dark:   true,

		Background:        "#0e0d0c",
		DarkBackground:    "#080707",
		LighterBackground: "#2a2724",
		Foreground:        "#ece4d6",
		DarkForeground:    "#8a8276",
		BrightForeground:  "#fff8ec",

		Accent:    "#d2492f",
		Selection: "#a83a22",
		Muted:     "#8a8276",

		Red:     "#e0604a",
		Green:   "#9bb67a",
		Yellow:  "#e3b45a",
		Blue:    "#7fa2c4",
		Magenta: "#c47e9c",
		Cyan:    "#72b5a9",
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

// Active returns the palette currently installed.
func Active() Palette {
	activeMu.RLock()
	defer activeMu.RUnlock()
	return active
}

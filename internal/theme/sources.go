package theme

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Desktop theme sources beyond Omarchy. Each one is translated into the
// Omarchy colour names (background, accent, red, ...) and handed to
// paletteFromValues, so the roles are chosen in one place. Desktops that only
// expose an accent and a dark/light switch go through withAccent instead.
//
// Like the Omarchy reader, all of these are read-only.

// source is one place a palette can come from.
type source struct {
	// available reports cheaply whether this source applies on this machine,
	// for auto detection. A forced mode skips it and just loads.
	available func() bool
	load      func() (Palette, error)
}

var sources = map[Mode]source{
	ModeOmarchy: {available: Available, load: LoadOmarchy},
	ModeWal:     {available: func() bool { return fileExists(walPath()) }, load: LoadWal},
	ModeBase16:  {available: func() bool { return base16Path() != "" }, load: LoadBase16},
	ModeKDE: {
		available: func() bool { return desktopIs("KDE") && fileExists(kdeglobalsPath()) },
		load:      LoadKDE,
	},
	ModeGNOME: {
		available: func() bool { return desktopIs("GNOME") && commandExists("gsettings") },
		load:      LoadGNOME,
	},
	ModeMacOS: {
		available: func() bool { return runtime.GOOS == "darwin" },
		load:      LoadMacOS,
	},
	ModeWindows: {
		available: func() bool { return runtime.GOOS == "windows" },
		load:      LoadWindows,
	},
}

// autoOrder is the order auto detection tries sources in. Generated palettes
// come first: someone running pywal or a base16 manager chose those colours on
// purpose, often on top of a desktop whose own scheme they never touched.
var autoOrder = []Mode{ModeOmarchy, ModeWal, ModeBase16, ModeKDE, ModeGNOME, ModeMacOS, ModeWindows}

// Seams for tests.
var (
	homeDir    = os.UserHomeDir
	getenv     = os.Getenv
	runCommand = func(name string, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, name, args...).Output()
		return strings.TrimSpace(string(out)), err
	}
)

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// desktopIs reports whether XDG_CURRENT_DESKTOP names the desktop, which may be
// a colon-separated list such as "ubuntu:GNOME".
func desktopIs(name string) bool {
	for _, part := range strings.Split(getenv("XDG_CURRENT_DESKTOP"), ":") {
		if strings.EqualFold(strings.TrimSpace(part), name) {
			return true
		}
	}
	return false
}

// xdgDir returns $env or ~/fallback.
func xdgDir(env, fallback string) string {
	if dir := getenv(env); dir != "" {
		return dir
	}
	home, err := homeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, fallback)
}

// --- pywal / wallust / matugen ---

func walPath() string {
	dir := xdgDir("XDG_CACHE_HOME", ".cache")
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "wal", "colors.json")
}

// LoadWal reads pywal's colors.json. wallust and matugen write the same file
// when set up with their pywal template.
func LoadWal() (Palette, error) {
	raw, err := os.ReadFile(walPath())
	if err != nil {
		return Builtin(), fmt.Errorf("read pywal colors.json: %w", err)
	}
	values, err := parseWalJSON(raw)
	if err != nil {
		return Builtin(), err
	}
	return paletteFromValues(values, "pywal", SourceWal), nil
}

type walJSON struct {
	Special map[string]string `json:"special"`
	Colors  map[string]string `json:"colors"`
}

// parseWalJSON maps a 16-colour terminal palette onto the Omarchy names.
func parseWalJSON(raw []byte) (map[string]string, error) {
	var file walJSON
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse pywal colors.json: %w", err)
	}
	if len(file.Colors) == 0 {
		return nil, fmt.Errorf("pywal colors.json has no colours")
	}
	color := func(n int) string { return file.Colors["color"+strconv.Itoa(n)] }

	background := file.Special["background"]
	if background == "" {
		background = color(0)
	}
	foreground := file.Special["foreground"]
	if foreground == "" {
		foreground = color(7)
	}

	values := map[string]string{
		"background":        background,
		"foreground":        foreground,
		"bright_foreground": color(15),
		"muted":             color(8),
		"dark_foreground":   color(8),
		"accent":            color(4),
		"red":               color(1),
		"green":             color(2),
		"yellow":            color(3),
		"blue":              color(4),
		"magenta":           color(5),
		"cyan":              color(6),
	}
	if isHexColor(background) && isHexColor(foreground) {
		// The 16 colours have no raised background; color0 is usually the
		// background itself, so mix one instead.
		values["lighter_background"] = Mix(background, foreground, 0.15)
	}
	return values, nil
}

// --- base16 (tinty, stylix, any scheme file) ---

// base16Path finds the active base16 scheme: tinty's current scheme, then
// stylix's generated palette.
func base16Path() string {
	data := xdgDir("XDG_DATA_HOME", filepath.Join(".local", "share"))
	if data != "" {
		tinty := filepath.Join(data, "tinted-theming", "tinty")
		if raw, err := os.ReadFile(filepath.Join(tinty, "current_scheme")); err == nil {
			// "base16-ocean" lives at repos/schemes/base16/ocean.yaml.
			system, slug, ok := strings.Cut(strings.TrimSpace(string(raw)), "-")
			if ok {
				for _, repo := range []string{"schemes", "tinted-schemes"} {
					for _, ext := range []string{".yaml", ".yml"} {
						path := filepath.Join(tinty, "repos", repo, system, slug+ext)
						if fileExists(path) {
							return path
						}
					}
				}
			}
		}
	}

	config := xdgDir("XDG_CONFIG_HOME", ".config")
	for _, path := range []string{filepath.Join(config, "stylix", "palette.json"), "/etc/stylix/palette.json"} {
		if config != "" && fileExists(path) {
			return path
		}
	}
	return ""
}

// LoadBase16 reads the active base16 scheme from tinty or stylix.
func LoadBase16() (Palette, error) {
	path := base16Path()
	if path == "" {
		return Builtin(), fmt.Errorf("no tinty or stylix base16 scheme found")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Builtin(), fmt.Errorf("read base16 scheme: %w", err)
	}
	values, name := parseBase16(raw)
	if len(values) == 0 {
		return Builtin(), fmt.Errorf("%s has no base16 colours", path)
	}
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return paletteFromValues(values, name, SourceBase16), nil
}

// yamlPair reads one `key: value` line of YAML or JSON, without the quotes,
// trailing comma or comment around it.
func yamlPair(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	key, value, ok := strings.Cut(line, ":")
	if !ok {
		return "", "", false
	}
	key = strings.ToLower(strings.Trim(strings.TrimSpace(key), `"'`))
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, `"`) || strings.HasPrefix(value, "'") {
		quote := value[:1]
		if end := strings.Index(value[1:], quote); end >= 0 {
			return key, value[1 : end+1], true
		}
		return key, strings.Trim(value, `"',`), true
	}
	if idx := strings.Index(value, " #"); idx >= 0 {
		value = value[:idx]
	}
	return key, strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), ",")), true
}

// parseBase16 reads a base16 scheme in YAML (old flat or new palette:
// layout) or stylix's JSON, mapping the sixteen slots onto Omarchy names.
func parseBase16(raw []byte) (map[string]string, string) {
	slots := map[string]string{}
	var name, variant string

	lines := strings.Split(string(raw), "\n")
	// stylix writes its palette as one line of JSON.
	var object map[string]any
	if json.Unmarshal(raw, &object) == nil {
		lines = lines[:0]
		for key, value := range object {
			if text, ok := value.(string); ok {
				lines = append(lines, key+": "+strconv.Quote(text))
			}
		}
	}

	for _, line := range lines {
		key, value, ok := yamlPair(line)
		if !ok {
			continue
		}
		switch key {
		case "scheme", "name":
			if name == "" {
				name = value
			}
		case "variant":
			variant = strings.ToLower(value)
		default:
			if len(key) != 6 || !strings.HasPrefix(key, "base0") {
				continue
			}
			if !strings.HasPrefix(value, "#") {
				value = "#" + value
			}
			if isHexColor(value) {
				slots[key] = value
			}
		}
	}
	if len(slots) == 0 {
		return nil, name
	}

	values := map[string]string{}
	for role, slot := range map[string]string{
		"background":         "base00",
		"lighter_background": "base01",
		"selection":          "base02",
		"muted":              "base03",
		"dark_foreground":    "base04",
		"foreground":         "base05",
		"bright_foreground":  "base07",
		"red":                "base08",
		"yellow":             "base0a",
		"green":              "base0b",
		"cyan":               "base0c",
		"blue":               "base0d",
		"accent":             "base0d",
		"magenta":            "base0e",
	} {
		if color, ok := slots[slot]; ok {
			values[role] = color
		}
	}
	if variant == "light" || variant == "dark" {
		values["mode"] = variant
	}
	return values, name
}

// --- KDE Plasma ---

func kdeglobalsPath() string {
	dir := xdgDir("XDG_CONFIG_HOME", ".config")
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "kdeglobals")
}

// LoadKDE reads the Plasma colour scheme from kdeglobals.
func LoadKDE() (Palette, error) {
	raw, err := os.ReadFile(kdeglobalsPath())
	if err != nil {
		return Builtin(), fmt.Errorf("read kdeglobals: %w", err)
	}
	values, name := parseKDEGlobals(string(raw))
	if values["background"] == "" {
		return Builtin(), fmt.Errorf("kdeglobals has no colour scheme")
	}
	if name == "" {
		name = "plasma"
	}
	return paletteFromValues(values, name, SourceKDE), nil
}

// parseKDEGlobals maps the View, Window and Selection colour sets onto the
// Omarchy names. KDE's link, negative, positive and neutral text colours make a
// full palette, not just an accent.
func parseKDEGlobals(content string) (map[string]string, string) {
	sections := map[string]map[string]string{}
	current := ""
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = line[1 : len(line)-1]
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if sections[current] == nil {
			sections[current] = map[string]string{}
		}
		sections[current][strings.TrimSpace(key)] = strings.TrimSpace(value)
	}

	get := func(section, key string) string {
		return kdeColor(sections[section][key])
	}

	values := map[string]string{
		"background":         get("Colors:View", "BackgroundNormal"),
		"lighter_background": get("Colors:View", "BackgroundAlternate"),
		"dark_background":    get("Colors:Window", "BackgroundNormal"),
		"foreground":         get("Colors:View", "ForegroundNormal"),
		"muted":              get("Colors:View", "ForegroundInactive"),
		"selection":          get("Colors:Selection", "BackgroundNormal"),
		"red":                get("Colors:View", "ForegroundNegative"),
		"green":              get("Colors:View", "ForegroundPositive"),
		"yellow":             get("Colors:View", "ForegroundNeutral"),
		"blue":               get("Colors:View", "ForegroundLink"),
		"magenta":            get("Colors:View", "ForegroundVisited"),
		"cyan":               get("Colors:View", "DecorationFocus"),
	}
	accent := get("General", "AccentColor")
	if accent == "" {
		accent = values["selection"]
	}
	values["accent"] = accent
	for key, value := range values {
		if value == "" {
			delete(values, key)
		}
	}
	return values, sections["General"]["ColorScheme"]
}

// kdeColor turns KDE's "r,g,b" (or "r,g,b,a") into #rrggbb.
func kdeColor(value string) string {
	parts := strings.Split(value, ",")
	if len(parts) < 3 {
		if isHexColor(value) {
			return normalizeHex(value)
		}
		return ""
	}
	var channels [3]int
	for i := range channels {
		n, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err != nil || n < 0 || n > 255 {
			return ""
		}
		channels[i] = n
	}
	return fmt.Sprintf("#%02x%02x%02x", channels[0], channels[1], channels[2])
}

// --- GNOME ---

// gnomeAccents are libadwaita's accent colours (GNOME 47+).
var gnomeAccents = map[string]string{
	"blue":   "#3584e4",
	"teal":   "#2190a4",
	"green":  "#3a944a",
	"yellow": "#c88800",
	"orange": "#ed5b00",
	"red":    "#e62d42",
	"pink":   "#d56199",
	"purple": "#9141ac",
	"slate":  "#6f8396",
}

// LoadGNOME reads GNOME's dark style switch and accent colour.
func LoadGNOME() (Palette, error) {
	scheme, err := runCommand("gsettings", "get", "org.gnome.desktop.interface", "color-scheme")
	if err != nil {
		return Builtin(), fmt.Errorf("read GNOME colour scheme: %w", err)
	}
	dark := strings.Contains(scheme, "dark")
	if !dark && !strings.Contains(scheme, "light") {
		// "default" defers to the GTK theme, which may itself be dark.
		if gtk, err := runCommand("gsettings", "get", "org.gnome.desktop.interface", "gtk-theme"); err == nil {
			dark = strings.Contains(strings.ToLower(gtk), "dark")
		}
	}

	// Older GNOME has no accent setting; libadwaita's default is blue.
	accent := gnomeAccents["blue"]
	if raw, err := runCommand("gsettings", "get", "org.gnome.desktop.interface", "accent-color"); err == nil {
		if color, ok := gnomeAccents[strings.Trim(raw, "'\" ")]; ok {
			accent = color
		}
	}
	return withAccent(dark, accent, "gnome", SourceGNOME), nil
}

// --- macOS ---

// macAccents are macOS's accent colours by AppleAccentColor value, as
// dark-mode and light-mode system colours.
var macAccents = map[string][2]string{
	"-1": {"#98989d", "#8e8e93"}, // graphite
	"0":  {"#ff453a", "#ff3b30"}, // red
	"1":  {"#ff9f0a", "#ff9500"}, // orange
	"2":  {"#ffd60a", "#ffcc00"}, // yellow
	"3":  {"#32d74b", "#28cd41"}, // green
	"4":  {"#0a84ff", "#007aff"}, // blue
	"5":  {"#bf5af2", "#af52de"}, // purple
	"6":  {"#ff375f", "#ff2d55"}, // pink
}

// LoadMacOS reads the system appearance and accent colour.
func LoadMacOS() (Palette, error) {
	if !commandExists("defaults") {
		return Builtin(), fmt.Errorf("macOS defaults command not found")
	}
	// The key only exists in dark mode; reading it fails in light mode.
	style, _ := runCommand("defaults", "read", "-g", "AppleInterfaceStyle")
	dark := strings.EqualFold(style, "dark")

	// Missing means the default multicolour accent, which is blue.
	key, err := runCommand("defaults", "read", "-g", "AppleAccentColor")
	if err != nil {
		key = "4"
	}
	colors, ok := macAccents[key]
	if !ok {
		colors = macAccents["4"]
	}
	accent := colors[1]
	if dark {
		accent = colors[0]
	}
	return withAccent(dark, accent, "macos", SourceMacOS), nil
}

// --- Windows ---

// windowsAccent turns the DWM AccentColor DWORD, stored as 0xAABBGGRR, into
// #rrggbb.
func windowsAccent(abgr uint32) string {
	r := abgr & 0xff
	g := (abgr >> 8) & 0xff
	b := (abgr >> 16) & 0xff
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// LoadWindows reads the app light/dark mode and accent colour.
func LoadWindows() (Palette, error) {
	dark, accent, err := readWindowsTheme()
	if err != nil {
		return Builtin(), err
	}
	return withAccent(dark, accent, "windows", SourceWindows), nil
}

// --- ThemeFile ---

// LoadFile reads a palette file of any format otakase knows: pywal
// colors.json, a base16 scheme (YAML or JSON), or an Omarchy-style
// colors.toml. The format is told from the content, not the extension.
func LoadFile(path string) (Palette, error) {
	path = strings.TrimSpace(path)
	if strings.HasPrefix(path, "~/") || path == "~" {
		if home, err := homeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Builtin(), fmt.Errorf("read ThemeFile: %w", err)
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	values, schemeName := parsePaletteFile(raw)
	if len(values) == 0 {
		return Builtin(), fmt.Errorf("ThemeFile %s: no colours found (expected pywal colors.json, a base16 scheme or a colors.toml)", path)
	}
	if schemeName != "" {
		name = schemeName
	}
	return paletteFromValues(values, name, SourceFile), nil
}

func parsePaletteFile(raw []byte) (map[string]string, string) {
	text := strings.TrimSpace(string(raw))

	if strings.HasPrefix(text, "{") {
		var probe map[string]json.RawMessage
		if json.Unmarshal(raw, &probe) == nil {
			if _, ok := probe["colors"]; ok {
				if values, err := parseWalJSON(raw); err == nil {
					return values, ""
				}
			}
		}
	}
	if values, name := parseBase16(raw); len(values) > 0 {
		return values, name
	}

	values := parseColorsTOML(text)
	for _, key := range []string{"background", "foreground", "accent"} {
		if isHexColor(values[key]) {
			return values, ""
		}
	}
	return nil, ""
}

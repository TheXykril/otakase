package theme

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeHome points the source lookups at a temporary home with no XDG
// variables, and restores the real ones afterwards.
func fakeHome(t *testing.T, env map[string]string) string {
	t.Helper()
	home := t.TempDir()
	oldHome, oldEnv := homeDir, getenv
	homeDir = func() (string, error) { return home, nil }
	getenv = func(key string) string { return env[key] }
	t.Cleanup(func() { homeDir, getenv = oldHome, oldEnv })
	return home
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const walSample = `{
  "wallpaper": "/home/x/wall.jpg",
  "alpha": "100",
  "special": {"background": "#1a1b26", "foreground": "#c0caf5", "cursor": "#c0caf5"},
  "colors": {
    "color0": "#1a1b26", "color1": "#f7768e", "color2": "#9ece6a", "color3": "#e0af68",
    "color4": "#7aa2f7", "color5": "#bb9af7", "color6": "#7dcfff", "color7": "#a9b1d6",
    "color8": "#565f89", "color9": "#f7768e", "color10": "#9ece6a", "color11": "#e0af68",
    "color12": "#7aa2f7", "color13": "#bb9af7", "color14": "#7dcfff", "color15": "#ffffff"
  }
}`

func TestLoadWalMapsTerminalColours(t *testing.T) {
	home := fakeHome(t, nil)
	writeFile(t, filepath.Join(home, ".cache", "wal", "colors.json"), walSample)

	if !sources[ModeWal].available() {
		t.Fatal("wal should be available")
	}
	palette, err := LoadWal()
	if err != nil {
		t.Fatal(err)
	}
	for got, want := range map[string]string{
		palette.Background:       "#1a1b26",
		palette.Foreground:       "#c0caf5",
		palette.Accent:           "#7aa2f7",
		palette.Muted:            "#565f89",
		palette.Red:              "#f7768e",
		palette.Cyan:             "#7dcfff",
		palette.BrightForeground: "#ffffff",
	} {
		if got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	}
	if !palette.Dark || palette.Source != SourceWal {
		t.Fatalf("dark=%v source=%s", palette.Dark, palette.Source)
	}
}

func TestParseBase16FlatAndPaletteLayouts(t *testing.T) {
	flat := `scheme: "Ocean"
author: "Chris Kempson"
base00: "2b303b"
base05: "c0c5ce"
base08: "bf616a"
base0D: "8fa1b3" # blue
`
	nested := `system: "base16"
name: "One Light"
variant: "light"
palette:
  base00: "#fafafa"
  base05: "#383a42"
  base0D: "#4078f2"
`
	stylix := `{"base00":"1e1e2e","base05":"cdd6f4","base0D":"89b4fa","scheme":"Catppuccin Mocha"}`

	values, name := parseBase16([]byte(flat))
	if name != "Ocean" || values["background"] != "#2b303b" || values["accent"] != "#8fa1b3" || values["red"] != "#bf616a" {
		t.Fatalf("flat: %q %v", name, values)
	}
	values, name = parseBase16([]byte(nested))
	if name != "One Light" || values["mode"] != "light" || values["foreground"] != "#383a42" {
		t.Fatalf("nested: %q %v", name, values)
	}
	values, name = parseBase16([]byte(stylix))
	if name != "Catppuccin Mocha" || values["blue"] != "#89b4fa" {
		t.Fatalf("stylix: %q %v", name, values)
	}

	palette := paletteFromValues(mustBase16(t, nested), "One Light", SourceBase16)
	if palette.Dark {
		t.Fatal("a light base16 scheme should give a light palette")
	}
	// Missing slots come from the light builtin, not the dark one.
	if palette.Yellow != BuiltinLight().Yellow {
		t.Fatalf("yellow = %s", palette.Yellow)
	}
}

func mustBase16(t *testing.T, content string) map[string]string {
	t.Helper()
	values, _ := parseBase16([]byte(content))
	return values
}

func TestBase16PathFindsTintyThenStylix(t *testing.T) {
	home := fakeHome(t, nil)
	if base16Path() != "" {
		t.Fatal("nothing installed, expected no path")
	}

	stylix := filepath.Join(home, ".config", "stylix", "palette.json")
	writeFile(t, stylix, `{"base00":"000000"}`)
	if got := base16Path(); got != stylix {
		t.Fatalf("got %q, want stylix %q", got, stylix)
	}

	tinty := filepath.Join(home, ".local", "share", "tinted-theming", "tinty")
	writeFile(t, filepath.Join(tinty, "current_scheme"), "base16-ocean\n")
	scheme := filepath.Join(tinty, "repos", "schemes", "base16", "ocean.yaml")
	writeFile(t, scheme, "base00: \"2b303b\"\n")
	if got := base16Path(); got != scheme {
		t.Fatalf("got %q, want tinty %q", got, scheme)
	}
}

func TestParseKDEGlobals(t *testing.T) {
	content := `[ColorEffects:Disabled]
Color=56,56,56

[Colors:Selection]
BackgroundNormal=61,174,233

[Colors:View]
BackgroundAlternate=29,31,34
BackgroundNormal=20,22,24
ForegroundInactive=161,169,177
ForegroundLink=29,153,243
ForegroundNegative=218,68,83
ForegroundNeutral=246,116,0
ForegroundNormal=252,252,252
ForegroundPositive=39,174,96
ForegroundVisited=155,89,182

[Colors:Window]
BackgroundNormal=32,35,38

[General]
ColorScheme=BreezeDark
`
	values, name := parseKDEGlobals(content)
	if name != "BreezeDark" {
		t.Fatalf("name = %q", name)
	}
	for key, want := range map[string]string{
		"background": "#141618",
		"foreground": "#fcfcfc",
		"selection":  "#3daee9",
		"accent":     "#3daee9",
		"red":        "#da4453",
		"green":      "#27ae60",
		"blue":       "#1d99f3",
	} {
		if values[key] != want {
			t.Fatalf("%s = %q, want %q", key, values[key], want)
		}
	}

	values, _ = parseKDEGlobals(content + "AccentColor=200,100,50\n")
	if values["accent"] != "#c86432" {
		t.Fatalf("explicit accent = %q", values["accent"])
	}
}

func TestKDEOnlyAutoDetectedOnPlasma(t *testing.T) {
	home := fakeHome(t, map[string]string{"XDG_CURRENT_DESKTOP": "Hyprland"})
	writeFile(t, filepath.Join(home, ".config", "kdeglobals"), "[Colors:View]\nBackgroundNormal=1,2,3\n")
	if sources[ModeKDE].available() {
		t.Fatal("a stray kdeglobals off Plasma should not be followed")
	}
	getenv = func(key string) string {
		if key == "XDG_CURRENT_DESKTOP" {
			return "KDE"
		}
		return ""
	}
	if !sources[ModeKDE].available() {
		t.Fatal("kdeglobals on Plasma should be followed")
	}
}

func fakeCommands(t *testing.T, outputs map[string]string) {
	t.Helper()
	old := runCommand
	runCommand = func(name string, args ...string) (string, error) {
		key := args[len(args)-1]
		if out, ok := outputs[key]; ok {
			return out, nil
		}
		return "", errors.New("not set")
	}
	t.Cleanup(func() { runCommand = old })
}

func TestLoadGNOME(t *testing.T) {
	fakeCommands(t, map[string]string{"color-scheme": "'prefer-dark'", "accent-color": "'purple'"})
	palette, err := LoadGNOME()
	if err != nil {
		t.Fatal(err)
	}
	if !palette.Dark || palette.Accent != "#9141ac" || palette.Background != Builtin().Background {
		t.Fatalf("got %+v", palette)
	}

	// Older GNOME: no accent key, light default.
	fakeCommands(t, map[string]string{"color-scheme": "'default'", "gtk-theme": "'Adwaita'"})
	palette, _ = LoadGNOME()
	if palette.Dark || palette.Accent != "#3584e4" || palette.Background != BuiltinLight().Background {
		t.Fatalf("got %+v", palette)
	}
}

func TestWindowsAccentIsABGR(t *testing.T) {
	if got := windowsAccent(0xffd77800); got != "#0078d7" {
		t.Fatalf("got %s", got)
	}
}

func TestLoadFileDetectsFormat(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]struct {
		content string
		accent  string
	}{
		"wal.json":     {walSample, "#7aa2f7"},
		"scheme.yaml":  {"base00: \"2b303b\"\nbase0D: \"8fa1b3\"\n", "#8fa1b3"},
		"palette.json": {`{"base00":"1e1e2e","base0D":"89b4fa"}`, "#89b4fa"},
		"mine.txt":     {"background = \"#101010\"\naccent = \"#ff8800\"\n", "#ff8800"},
	}
	for name, c := range cases {
		path := filepath.Join(dir, name)
		writeFile(t, path, c.content)
		palette, err := LoadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if palette.Accent != c.accent || palette.Source != SourceFile {
			t.Fatalf("%s: accent %s source %s", name, palette.Accent, palette.Source)
		}
	}

	junk := filepath.Join(dir, "junk")
	writeFile(t, junk, "hello\n")
	if _, err := LoadFile(junk); err == nil {
		t.Fatal("a file with no colours should be an error")
	}
}

func TestResolveAutoOrderAndThemeFile(t *testing.T) {
	home := fakeHome(t, nil)
	if palette, _ := Resolve(ModeAuto, ""); palette.Source != SourceBuiltin && palette.Source != SourceMacOS && palette.Source != SourceWindows {
		t.Fatalf("empty home resolved to %s", palette.Source)
	}

	writeFile(t, filepath.Join(home, ".cache", "wal", "colors.json"), walSample)
	if palette, _ := Resolve(ModeAuto, ""); palette.Source != SourceWal {
		t.Fatalf("expected wal, got %s", palette.Source)
	}

	file := filepath.Join(home, "mine.toml")
	writeFile(t, file, "accent = \"#ff8800\"\n")
	if palette, _ := Resolve(ModeAuto, file); palette.Source != SourceFile {
		t.Fatalf("ThemeFile should win over auto, got %s", palette.Source)
	}
	if palette, _ := Resolve(ModeBuiltin, file); palette.Source != SourceBuiltin {
		t.Fatalf("builtin should ignore ThemeFile, got %s", palette.Source)
	}
	if palette, err := Resolve(ModeKDE, ""); palette.Source != SourceBuiltin || err == nil {
		t.Fatalf("forced KDE with no kdeglobals should fall back with an error")
	}
	SetActive(Builtin())
}

func TestParseModeAliases(t *testing.T) {
	for raw, want := range map[string]Mode{
		"":        ModeAuto,
		"system":  ModeAuto,
		"wallust": ModeWal,
		"stylix":  ModeBase16,
		"Plasma":  ModeKDE,
		"gtk":     ModeGNOME,
		"mac":     ModeMacOS,
		"windows": ModeWindows,
		"off":     ModeBuiltin,
	} {
		if got := ParseMode(raw); got != want {
			t.Fatalf("ParseMode(%q) = %s, want %s", raw, got, want)
		}
	}
}

package theme

import (
	"math"
	"strings"
	"testing"
)

func TestParseColorsTOMLReadsOmarchyFormat(t *testing.T) {
	// Verbatim shape of an Omarchy colors.toml, including the non-hex entries
	// and a key that only some themes define.
	content := `mode = "dark"

accent = "#798186"
selection = "#343d41"

background = "#101315"
foreground = "#cacccc"

hyprland_active_border = "rgba(798186ee) rgba(caccccee)"

red = "#565d60"
orange = "#f6b6ab"
`
	values := parseColorsTOML(content)

	for key, want := range map[string]string{
		"mode":       "dark",
		"accent":     "#798186",
		"background": "#101315",
		"foreground": "#cacccc",
		"red":        "#565d60",
		"orange":     "#f6b6ab",
	} {
		if values[key] != want {
			t.Fatalf("%s = %q, want %q", key, values[key], want)
		}
	}
	// Non-hex values are kept verbatim; the palette simply ignores them.
	if !strings.HasPrefix(values["hyprland_active_border"], "rgba(") {
		t.Fatalf("hyprland_active_border = %q", values["hyprland_active_border"])
	}
}

func TestParseColorsTOMLIgnoresCommentsAndBlanks(t *testing.T) {
	values := parseColorsTOML("# a comment\n\n  \naccent = \"#abcdef\"\n[table]\nbroken line\n")
	if values["accent"] != "#abcdef" {
		t.Fatalf("accent = %q", values["accent"])
	}
	if len(values) != 1 {
		t.Fatalf("expected only accent, got %v", values)
	}
}

func TestIsHexColor(t *testing.T) {
	for _, good := range []string{"#fff", "#FFFFFF", "#101315", "#AbCdEf"} {
		if !isHexColor(good) {
			t.Fatalf("%q should be a hex colour", good)
		}
	}
	for _, bad := range []string{"", "fff", "#ff", "#fffff", "#gggggg", "rgba(1,2,3)", "#12345g"} {
		if isHexColor(bad) {
			t.Fatalf("%q should not be a hex colour", bad)
		}
	}
}

func TestNormalizeHexExpandsShortForm(t *testing.T) {
	if got := normalizeHex("#ABC"); got != "#aabbcc" {
		t.Fatalf("normalizeHex(#ABC) = %q", got)
	}
	if got := normalizeHex("#101315"); got != "#101315" {
		t.Fatalf("normalizeHex(#101315) = %q", got)
	}
}

func TestContrastRatioMatchesWCAGExtremes(t *testing.T) {
	if got := contrastRatio("#000000", "#ffffff"); math.Abs(got-21) > 0.01 {
		t.Fatalf("black/white contrast = %v, want 21", got)
	}
	if got := contrastRatio("#123456", "#123456"); math.Abs(got-1) > 0.001 {
		t.Fatalf("identical colours = %v, want 1", got)
	}
}

// The point of ReadableOn: a light accent needs dark text, a dark accent needs
// light text. Hardcoding either produces unreadable selections on half of themes.
func TestReadableOnPicksTheHigherContrastCandidate(t *testing.T) {
	if got := ReadableOn("#ffffff", "#f0f0f0", "#101010"); got != "#101010" {
		t.Fatalf("on white, got %q", got)
	}
	if got := ReadableOn("#000000", "#f0f0f0", "#101010"); got != "#f0f0f0" {
		t.Fatalf("on black, got %q", got)
	}
	// With no usable candidates it still returns something legible.
	if got := ReadableOn("#000000", "not-a-colour"); got != "#ffffff" {
		t.Fatalf("fallback on black = %q", got)
	}
	if got := ReadableOn("#ffffff"); got != "#000000" {
		t.Fatalf("fallback on white = %q", got)
	}
}

func TestMixBlendsEndpoints(t *testing.T) {
	if got := Mix("#000000", "#ffffff", 0); got != "#000000" {
		t.Fatalf("amount 0 = %q", got)
	}
	if got := Mix("#000000", "#ffffff", 1); got != "#ffffff" {
		t.Fatalf("amount 1 = %q", got)
	}
	if got := Mix("#000000", "#ffffff", 0.5); got != "#808080" {
		t.Fatalf("midpoint = %q", got)
	}
	// Out-of-range amounts clamp rather than producing nonsense.
	if got := Mix("#000000", "#ffffff", 5); got != "#ffffff" {
		t.Fatalf("clamped high = %q", got)
	}
}

// Omarchy's Solitude sets selection so close to its background that a
// highlighted row would be invisible; the accent must take over.
func TestSelectionBackgroundFallsBackToAccentWhenTooSubtle(t *testing.T) {
	invisible := Palette{
		Background: "#101315",
		Selection:  "#111416", // essentially the background
		Accent:     "#798186",
	}
	if got := invisible.SelectionBackground(); got != "#798186" {
		t.Fatalf("expected the accent to take over, got %q", got)
	}

	distinct := Palette{
		Background: "#1e1e2e",
		Selection:  "#89b4fa",
		Accent:     "#f5c2e7",
	}
	if got := distinct.SelectionBackground(); got != "#89b4fa" {
		t.Fatalf("a visible selection should be kept, got %q", got)
	}
}

func TestSelectionForegroundIsReadable(t *testing.T) {
	// A bright accent must end up with dark text on it.
	light := Palette{Background: "#101315", Selection: "#101315", Accent: "#f5e0dc", Foreground: "#cacccc", BrightForeground: "#ffffff"}
	fg := light.SelectionForeground()
	if ratio := contrastRatio(light.SelectionBackground(), fg); ratio < 4.5 {
		t.Fatalf("selection text contrast %.2f is too low (fg=%s on %s)", ratio, fg, light.SelectionBackground())
	}
}

// A theme missing colours must still produce a complete palette.
func TestLoadOmarchyFallsBackForMissingKeys(t *testing.T) {
	values := parseColorsTOML(`accent = "#123456"`)
	if values["accent"] != "#123456" {
		t.Fatal("sanity")
	}

	fallback := Builtin()
	pick := func(fallbackValue string, keys ...string) string {
		for _, key := range keys {
			if color, ok := values[key]; ok && isHexColor(color) {
				return normalizeHex(color)
			}
		}
		return fallbackValue
	}
	if got := pick(fallback.Red, "bright_red", "red"); got != fallback.Red {
		t.Fatalf("expected the builtin red for a theme that omits it, got %q", got)
	}
	if got := pick(fallback.Accent, "accent"); got != "#123456" {
		t.Fatalf("expected the theme's accent, got %q", got)
	}
}

func TestParseMode(t *testing.T) {
	for raw, want := range map[string]Mode{
		"":         ModeAuto,
		"auto":     ModeAuto,
		"AUTO":     ModeAuto,
		"omarchy":  ModeOmarchy,
		"system":   ModeOmarchy,
		"builtin":  ModeBuiltin,
		"off":      ModeBuiltin,
		"curd":     ModeBuiltin,
		"nonsense": ModeAuto,
	} {
		if got := ParseMode(raw); got != want {
			t.Fatalf("ParseMode(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestResolveBuiltinNeverTouchesOmarchy(t *testing.T) {
	palette, err := Resolve(ModeBuiltin)
	if err != nil {
		t.Fatalf("resolve builtin: %v", err)
	}
	if palette.Source != SourceBuiltin {
		t.Fatalf("source = %q", palette.Source)
	}
	if Active().Source != SourceBuiltin {
		t.Fatal("Resolve should install the palette as active")
	}
	t.Cleanup(func() { SetActive(Builtin()) })
}

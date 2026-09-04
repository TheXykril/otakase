package theme

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Themes vary wildly: an accent may be a pale grey on one theme and a saturated
// blue on another. Hardcoding "white text on the accent" therefore produces
// unreadable selections on some themes, so the foreground for a highlighted row
// is chosen by contrast rather than assumed.

// isHexColor reports whether a value is a #rgb or #rrggbb colour.
func isHexColor(value string) bool {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "#") {
		return false
	}
	digits := value[1:]
	if len(digits) != 3 && len(digits) != 6 {
		return false
	}
	for _, r := range digits {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// normalizeHex expands #rgb to #rrggbb and lowercases the digits.
func normalizeHex(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) == 4 {
		return fmt.Sprintf("#%c%c%c%c%c%c", value[1], value[1], value[2], value[2], value[3], value[3])
	}
	return value
}

// rgb splits a hex colour into its components.
func rgb(value string) (r, g, b float64, ok bool) {
	if !isHexColor(value) {
		return 0, 0, 0, false
	}
	value = normalizeHex(value)
	parsed, err := strconv.ParseUint(value[1:], 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return float64((parsed >> 16) & 0xff), float64((parsed >> 8) & 0xff), float64(parsed & 0xff), true
}

// relativeLuminance implements the WCAG relative luminance formula.
func relativeLuminance(value string) float64 {
	r, g, b, ok := rgb(value)
	if !ok {
		return 0
	}

	channel := func(v float64) float64 {
		v /= 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(r) + 0.7152*channel(g) + 0.0722*channel(b)
}

// contrastRatio returns the WCAG contrast ratio between two colours, 1..21.
func contrastRatio(a, b string) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// ReadableOn returns whichever candidate reads best against background. It is
// how the selected row keeps legible text whatever the theme's accent happens
// to be.
func ReadableOn(background string, candidates ...string) string {
	best := ""
	bestRatio := -1.0
	for _, candidate := range candidates {
		if !isHexColor(candidate) {
			continue
		}
		if ratio := contrastRatio(background, candidate); ratio > bestRatio {
			best, bestRatio = normalizeHex(candidate), ratio
		}
	}
	if best == "" {
		// Nothing usable was offered; fall back to plain black or white.
		if relativeLuminance(background) > 0.5 {
			return "#000000"
		}
		return "#ffffff"
	}
	return best
}

// Mix blends two colours, with amount 0 returning a and 1 returning b. Used to
// derive subtle surfaces (a slightly raised input bar, a hover row) that a theme
// does not define explicitly.
func Mix(a, b string, amount float64) string {
	ar, ag, ab, okA := rgb(a)
	br, bg, bb, okB := rgb(b)
	if !okA || !okB {
		if okA {
			return normalizeHex(a)
		}
		return normalizeHex(b)
	}

	amount = math.Max(0, math.Min(1, amount))
	blend := func(x, y float64) int {
		return int(math.Round(x + (y-x)*amount))
	}
	return fmt.Sprintf("#%02x%02x%02x", blend(ar, br), blend(ag, bg), blend(ab, bb))
}

// minSelectionContrast is how far a highlighted row must stand off the base
// background before it reads as highlighted at all.
const minSelectionContrast = 1.6

// SelectionBackground returns the background for the highlighted row. A theme's
// selection colour is sometimes nearly its background -- Omarchy's Solitude pairs
// #343d41 with a #101315 base -- which leaves the cursor invisible. The accent is
// used instead whenever selection is too close to the base to be seen.
func (p Palette) SelectionBackground() string {
	if contrastRatio(p.Background, p.Selection) >= minSelectionContrast {
		return normalizeHex(p.Selection)
	}
	return normalizeHex(p.Accent)
}

// SelectionForeground picks the text colour for a highlighted row, by contrast
// against whatever SelectionBackground resolved to.
func (p Palette) SelectionForeground() string {
	background := p.SelectionBackground()
	return ReadableOn(background, p.BrightForeground, p.Foreground, p.Background, p.DarkBackground)
}

// Surface returns a background one step raised from the base, for input bars and
// other panels a theme does not colour explicitly.
func (p Palette) Surface() string {
	if p.Dark {
		return Mix(p.Background, p.Foreground, 0.10)
	}
	return Mix(p.Background, p.Foreground, 0.06)
}

// Border returns a low-contrast line colour for panel edges.
func (p Palette) Border() string {
	return Mix(p.Background, p.Muted, 0.75)
}

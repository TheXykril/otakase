// Package rofitheme renders otakase's rofi menu themes from the active colour
// palette.
//
// The .rasi files used to be downloaded from GitHub on first run and then left
// alone forever, which meant otakase needed the network before it could show a menu,
// and any change to a theme never reached an existing install. They are embedded
// and rendered locally instead, so the menus work offline and follow the desktop
// theme.
package rofitheme

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/thexykril/otakase/internal/theme"
)

//go:embed templates/*.rasi
var templates embed.FS

// GridLabelCapacity is how many monospace characters fit under one cover in the
// poster grid. Measured against the three-column layout in
// selectanimepreview.rasi by rendering a character ruler, not derived from the
// geometry: a first estimate from column arithmetic was off by a third.
const GridLabelCapacity = 37

// Names are the theme files otakase invokes rofi with.
var Names = []string{
	"selectanime.rasi",
	"selectanimepreview.rasi",
	"contextselect.rasi",
	"userinput.rasi",
}

// templateData is the value the .rasi templates are rendered against.
//
// The type scale is set by role rather than decoration: one family, sized so the
// thing you are typing into is larger than the things you are choosing between.
type templateData struct {
	Font        string
	EntryFont   string
	PromptFont  string
	HeadingFont string
	TitleFont   string

	Background       string
	Scrim            string
	Surface          string
	Border           string
	Foreground       string
	BrightForeground string
	Muted            string
	Accent           string
	// CardBorder, HairRule, SelectFill and SelectEdge are the foreground tinted
	// at the alphas Omarchy's shell.toml uses for menu chrome: a card outline, an
	// interior rule, a selected row's fill, and that row's own outline.
	CardBorder string
	HairRule   string
	SelectFill string
	SelectEdge string
	// OnAccent is text drawn on top of the accent colour.
	OnAccent string
	// SelectionBand is the filled band behind the cursor row. It is mixed from
	// the accent rather than taken from the theme's selection colour, which on
	// some themes sits so close to the background that the cursor disappears.
	SelectionBand       string
	SelectionBackground string
	SelectionForeground string
	Red                 string
	Green               string
}

// alpha renders a palette colour at the given opacity as #rrggbbaa.
func alpha(color string, opacity float64) string {
	color = strings.TrimSpace(color)
	if opacity < 0 {
		opacity = 0
	}
	if opacity > 1 {
		opacity = 1
	}
	return fmt.Sprintf("%s%02x", color, int(opacity*255+0.5))
}

func newTemplateData(palette theme.Palette) templateData {
	accent := palette.Accent
	mono := theme.MonospaceFont()
	return templateData{
		// A modular scale, 11 -> 13 -> 16 at roughly 1.2x, rather than the ad-hoc
		// 11/13/13/14/15 it replaces. Three steps is all this UI needs, and a
		// consistent ratio is what makes hierarchy read as deliberate.
		// Monospace throughout: the grid budgets a title against a fixed column
		// width, which only holds when every glyph is the same width. Omarchy's
		// own menus are monospace too, so this matches the desktop.
		Font:        mono + " 10",
		EntryFont:   mono + " 13",
		PromptFont:  mono + " Bold 13",
		HeadingFont: mono + " Bold 13",
		TitleFont:   mono + " 13",

		Background: palette.Background,
		// rofi accepts #rrggbbaa. The poster grid is fullscreen, so it dims the
		// desktop rather than blacking it out -- but it has to be opaque enough
		// that whatever is behind it does not compete with the covers.
		// Omarchy dims the desktop to 0.5 behind its menus rather than blacking it
		// out; that works here because the grid now sits on an opaque card.
		Scrim:            alpha(palette.Background, 0.50),
		Surface:          palette.Surface(),
		Border:           palette.Border(),
		Foreground:       palette.Foreground,
		BrightForeground: palette.BrightForeground,
		Muted:            palette.Muted,
		Accent:           accent,
		// The prompt pill is filled with the accent, so its text has to be chosen
		// by contrast or it disappears on light accents.
		OnAccent: theme.ReadableOn(accent, palette.Background, palette.BrightForeground, palette.Foreground),
		// Alphas from Omarchy's shell.toml: menu border, interior rules, and a
		// selected row at 8% fill with a 25% outline.
		CardBorder:          alpha(palette.Foreground, 0.40),
		HairRule:            alpha(palette.Foreground, 0.15),
		SelectFill:          alpha(palette.Foreground, 0.08),
		SelectEdge:          alpha(palette.Foreground, 0.25),
		SelectionBand:       palette.SelectionBand(),
		SelectionBackground: palette.SelectionBackground(),
		SelectionForeground: palette.SelectionForeground(),
		Red:                 palette.Red,
		Green:               palette.Green,
	}
}

// Render returns one rendered theme file.
func Render(name string, palette theme.Palette) (string, error) {
	raw, err := templates.ReadFile("templates/" + name)
	if err != nil {
		return "", fmt.Errorf("unknown rofi theme %q: %w", name, err)
	}

	parsed, err := template.New(name).Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("parse rofi theme %q: %w", name, err)
	}

	var out strings.Builder
	if err := parsed.Execute(&out, newTemplateData(palette)); err != nil {
		return "", fmt.Errorf("render rofi theme %q: %w", name, err)
	}
	return out.String(), nil
}

// generatedMarker identifies a file this package wrote. Anything without it was
// hand-edited by the user (or downloaded by an older release) and is backed up
// before being replaced, so no one silently loses a customised menu.
const generatedMarker = "Generated by otakase from the active colour theme"

// legacyGeneratedMarker is what otakase, and otakase up to 1.0.0, wrote. A file
// carrying it is still one this package wrote, so it is recognised too --
// otherwise the rename alone would look like every theme had been hand-edited,
// and upgrading would back up and replace the lot.
const legacyGeneratedMarker = "Generated by curd from the active colour theme"

// BackupSuffix is appended to a hand-edited theme that had to be replaced.
const BackupSuffix = ".user-backup"

// isGenerated reports whether an existing theme file was written by otakase.
func isGenerated(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		// Missing counts as generated: there is nothing to preserve.
		return true
	}
	defer file.Close()

	header := make([]byte, 256)
	n, _ := file.Read(header)
	head := string(header[:n])
	return strings.Contains(head, generatedMarker) || strings.Contains(head, legacyGeneratedMarker)
}

// WriteAll renders every theme into dir. Generated files are rewritten on every
// run so a desktop theme change is picked up without the user clearing anything.
// A hand-edited theme is copied aside first and its backup path returned.
func WriteAll(dir string, palette theme.Palette) error {
	_, err := WriteAllWithBackups(dir, palette)
	return err
}

// WriteAllWithBackups is WriteAll, reporting any files it had to preserve.
func WriteAllWithBackups(dir string, palette theme.Palette) (backups []string, err error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("no storage path for rofi themes")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create rofi theme directory: %w", err)
	}

	for _, name := range Names {
		rendered, err := Render(name, palette)
		if err != nil {
			return backups, err
		}

		path := filepath.Join(dir, name)
		if !isGenerated(path) {
			backupPath := path + BackupSuffix
			// Only back up once: a later run must not overwrite the original
			// customisation with an already-generated file.
			if _, statErr := os.Stat(backupPath); os.IsNotExist(statErr) {
				existing, readErr := os.ReadFile(path)
				if readErr != nil {
					return backups, fmt.Errorf("read %s: %w", path, readErr)
				}
				if err := os.WriteFile(backupPath, existing, 0644); err != nil {
					return backups, fmt.Errorf("back up %s: %w", path, err)
				}
				backups = append(backups, backupPath)
			}
		}

		if err := os.WriteFile(path, []byte(rendered), 0644); err != nil {
			return backups, fmt.Errorf("write %s: %w", path, err)
		}
	}
	return backups, nil
}

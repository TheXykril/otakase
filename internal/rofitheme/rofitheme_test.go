package rofitheme

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/wraient/curd/internal/theme"
)

func TestRenderSubstitutesPaletteColors(t *testing.T) {
	palette := theme.Palette{
		Name:       "test",
		Dark:       true,
		Background: "#101315",
		Foreground: "#cacccc",
		Muted:      "#4b4e55",
		Accent:     "#798186",
		Selection:  "#343d41",
		Red:        "#de6145",
		Green:      "#9fa5a9",
	}

	for _, name := range Names {
		rendered, err := Render(name, palette)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.Contains(rendered, "{{") || strings.Contains(rendered, "}}") {
			t.Fatalf("%s still contains template markers:\n%s", name, rendered)
		}
		if !strings.Contains(rendered, palette.Background) {
			t.Fatalf("%s does not use the palette background", name)
		}
		if !strings.Contains(rendered, palette.Accent) {
			t.Fatalf("%s does not use the palette accent", name)
		}
	}
}

func TestRenderRejectsUnknownTheme(t *testing.T) {
	if _, err := Render("nope.rasi", theme.Builtin()); err == nil {
		t.Fatal("expected an error for an unknown theme")
	}
}

func TestWriteAllWritesEveryTheme(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "storage")
	if err := WriteAll(dir, theme.Builtin()); err != nil {
		t.Fatalf("WriteAll: %v", err)
	}

	for _, name := range Names {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if info.Size() == 0 {
			t.Fatalf("%s is empty", name)
		}
	}
}

// Themes are rewritten every run so a desktop theme change is picked up without
// the user clearing anything.
func TestWriteAllOverwritesStaleThemes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "selectanime.rasi")
	if err := os.WriteFile(path, []byte("stale content"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := WriteAll(dir, theme.Builtin()); err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "stale content") {
		t.Fatal("expected the stale theme to be overwritten")
	}
}

func TestWriteAllRejectsEmptyDir(t *testing.T) {
	if err := WriteAll("   ", theme.Builtin()); err == nil {
		t.Fatal("expected an error for an empty storage path")
	}
}

// The prompt pill is filled with the accent colour, so its text must be picked by
// contrast -- a light accent needs dark text or the prompt vanishes.
func TestPromptTextContrastsWithAccent(t *testing.T) {
	light := theme.Palette{Background: "#101315", Accent: "#f5e0dc", Foreground: "#cacccc", BrightForeground: "#ffffff"}
	data := newTemplateData(light)
	if data.OnAccent != "#101315" {
		t.Fatalf("expected dark prompt text on a light accent, got %q", data.OnAccent)
	}

	dark := theme.Palette{Background: "#ffffff", Accent: "#1a1a2e", Foreground: "#333333", BrightForeground: "#000000"}
	data = newTemplateData(dark)
	if data.OnAccent != "#ffffff" {
		t.Fatalf("expected light prompt text on a dark accent, got %q", data.OnAccent)
	}
}

// Regression guard for an invalid property (`width: island`) that rendered and
// wrote out fine but made rofi silently fall back to its default theme. Only a
// real parse catches that class of mistake.
func TestRenderedThemesParseInRofi(t *testing.T) {
	rofiPath, err := exec.LookPath("rofi")
	if err != nil {
		t.Skip("rofi is not installed")
	}

	dir := t.TempDir()
	for _, palette := range []theme.Palette{
		theme.Builtin(),
		{Name: "light", Background: "#ffffff", Foreground: "#1a1a1a", Muted: "#777777", Accent: "#0057b7", Selection: "#cfe3ff", Red: "#c62828", Green: "#2e7d32", BrightForeground: "#000000"},
	} {
		if err := WriteAll(dir, palette); err != nil {
			t.Fatalf("WriteAll: %v", err)
		}

		for _, name := range Names {
			path := filepath.Join(dir, name)

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			cmd := exec.CommandContext(ctx, rofiPath, "-no-config", "-theme", path, "-dump-theme")
			var stderr strings.Builder
			cmd.Stderr = &stderr
			runErr := cmd.Run()
			cancel()

			if runErr != nil {
				t.Fatalf("%s (%s): rofi failed: %v\n%s", name, palette.Name, runErr, stderr.String())
			}
			// rofi reports a bad theme as a warning and falls back silently, so the
			// exit status alone proves nothing.
			if strings.Contains(stderr.String(), "Failed to parse theme") {
				t.Fatalf("%s (%s) is not valid rofi syntax:\n%s", name, palette.Name, stderr.String())
			}
		}
	}
}

// The user may have hand-edited their .rasi files (or an older Curd downloaded
// them). Replacing those without a copy would silently destroy real work.
func TestWriteAllBacksUpHandEditedThemes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "selectanime.rasi")
	custom := "* { background: #123456; /* my careful hand-tuned theme */ }"
	if err := os.WriteFile(path, []byte(custom), 0644); err != nil {
		t.Fatal(err)
	}

	backups, err := WriteAllWithBackups(dir, theme.Builtin())
	if err != nil {
		t.Fatalf("WriteAllWithBackups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one backup, got %v", backups)
	}

	saved, err := os.ReadFile(path + BackupSuffix)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(saved) != custom {
		t.Fatalf("backup does not match the original:\n%s", saved)
	}

	// The live file is the generated one.
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current), generatedMarker) {
		t.Fatal("expected the generated theme to be installed")
	}
}

// A second run must not overwrite the preserved original with a generated file.
func TestWriteAllBacksUpOnlyOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "selectanime.rasi")
	custom := "* { background: #123456; }"
	if err := os.WriteFile(path, []byte(custom), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := WriteAllWithBackups(dir, theme.Builtin()); err != nil {
		t.Fatal(err)
	}
	backups, err := WriteAllWithBackups(dir, theme.Builtin())
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatalf("expected no further backups on a second run, got %v", backups)
	}

	saved, err := os.ReadFile(path + BackupSuffix)
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != custom {
		t.Fatalf("the preserved original was overwritten:\n%s", saved)
	}
}

// Curd's own generated files are refreshed silently -- they are not user work.
func TestWriteAllDoesNotBackUpItsOwnOutput(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteAllWithBackups(dir, theme.Builtin()); err != nil {
		t.Fatal(err)
	}

	backups, err := WriteAllWithBackups(dir, theme.Builtin())
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatalf("expected no backups for generated files, got %v", backups)
	}
	for _, name := range Names {
		if _, err := os.Stat(filepath.Join(dir, name+BackupSuffix)); !os.IsNotExist(err) {
			t.Fatalf("unexpected backup for %s", name)
		}
	}
}

// GridLabelCapacity is measured against the poster grid's column count and card
// width. If either changes, the budget is stale and titles will clip the counts
// again, so this fails loudly rather than silently drifting.
func TestGridLayoutMatchesTheMeasuredCapacity(t *testing.T) {
	rendered, err := Render("selectanimepreview.rasi", theme.Builtin())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"columns:          3;", "width:            1100px;", "size:             240px;"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("GridLabelCapacity (%d) was measured against a layout with %q; "+
				"re-measure it before changing the grid", GridLabelCapacity, want)
		}
	}
}

// The menus should be monospace: the grid budgets a title in characters, which
// only maps to width in a fixed-pitch face.
func TestMenusUseAMonospaceFont(t *testing.T) {
	data := newTemplateData(theme.Builtin())
	if !strings.Contains(data.Font, theme.MonospaceFont()) {
		t.Fatalf("Font = %q, expected the system monospace family", data.Font)
	}
}

func TestQuattroTokensAreRendered(t *testing.T) {
	rendered, err := Render("selectanime.rasi", theme.Builtin())
	if err != nil {
		t.Fatal(err)
	}
	// Foreground-tinted chrome at Omarchy's alphas, not accent-coloured bands.
	for _, want := range []string{"sel-fill:", "sel-edge:", "edge:", "rule:"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected %q in the rendered theme", want)
		}
	}
	if strings.Contains(rendered, "{{") {
		t.Fatal("unrendered template markers remain")
	}
}

// Rofi draws -mesg only if the theme lists `message` among mainbox children.
// Leaving it out fails silently and invisibly: the menu still appears, still
// works, and simply loses the sentence explaining why it is on screen -- why
// playback failed, or when the next episode airs -- so the user sees a bare
// list of choices with no context.
func TestSelectAnimeThemeRendersMessages(t *testing.T) {
	rendered, err := Render("selectanime.rasi", theme.Builtin())
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	children := regexp.MustCompile(`mainbox\s*\{[^}]*children:\s*\[([^\]]*)\]`).FindStringSubmatch(rendered)
	if children == nil {
		t.Fatal("could not find mainbox children in the rendered theme")
	}
	if !strings.Contains(children[1], "message") {
		t.Fatalf("mainbox children %q must include message, or -mesg is never drawn", children[1])
	}

	// A message must also be styled, or it inherits defaults that clash with the
	// rest of the menu.
	if !strings.Contains(rendered, "message textbox") {
		t.Error("expected the message textbox to be styled")
	}
}

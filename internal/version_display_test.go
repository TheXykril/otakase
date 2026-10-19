package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/thexykril/otakase/internal/rofitheme"
)

func TestDisplayVersion(t *testing.T) {
	previous := appVersion
	defer func() { appVersion = previous }()

	for _, tc := range []struct{ set, want string }{
		{"26.1.0", "v26.1.0"},
		{"v26.1.1", "v26.1.1"},
		{"dev", "dev"},
	} {
		appVersion = tc.set
		if got := DisplayVersion(); got != tc.want {
			t.Errorf("DisplayVersion() with %q = %q, want %q", tc.set, got, tc.want)
		}
	}
}

func TestRenderHeaderShowsVersionWhenItFits(t *testing.T) {
	previous := appVersion
	defer func() { appVersion = previous }()
	appVersion = "26.1.0"

	wide := renderHeader("Watching", 60)
	if !strings.Contains(wide, "v26.1.0") {
		t.Fatalf("wide header %q is missing the version", wide)
	}
	if got := lipgloss.Width(wide); got != 60 {
		t.Fatalf("wide header is %d cells, want the full 60", got)
	}

	narrow := renderHeader("Watching", 20)
	if strings.Contains(narrow, "v26.1.0") {
		t.Fatalf("narrow header %q should drop the version", narrow)
	}
}

func TestRofiVersionThemeArgs(t *testing.T) {
	previous := appVersion
	defer func() { appVersion = previous }()
	appVersion = "26.1.0"

	args := rofiVersionThemeArgs()
	if len(args) != 2 || args[0] != "-theme-str" {
		t.Fatalf("unexpected args %q", args)
	}
	if !strings.Contains(args[1], `content: "v26.1.0"`) {
		t.Fatalf("theme string %q does not carry the version", args[1])
	}
}

// The version override names the input bar's children, so it has to keep the
// app icon the theme draws in front of the search box.
func TestRofiVersionThemeArgsKeepsIcon(t *testing.T) {
	previous := globalConfig
	defer func() { globalConfig = previous }()
	dir := t.TempDir()
	globalConfig = &Config{StoragePath: dir}

	if args := rofiVersionThemeArgs(); strings.Contains(args[1], "icon-app") {
		t.Fatalf("icon named with no icon written: %q", args[1])
	}
	if err := os.WriteFile(filepath.Join(dir, rofitheme.IconFile), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if args := rofiVersionThemeArgs(); !strings.Contains(args[1], "children: [ icon-app, entry, textbox-version ]") {
		t.Fatalf("icon dropped from the input bar: %q", args[1])
	}
}

package appicon

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallLinuxWritesUserCopies(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_DATA_DIRS", t.TempDir())
	var registered []string
	defer func(prev func(string)) { registerScheme = prev }(registerScheme)
	registerScheme = func(apps string) { registered = append(registered, apps) }

	path, ok := installLinux()
	if !ok {
		t.Fatal("nothing installed")
	}
	if want := filepath.Join(data, "icons", "hicolor", "256x256", "apps", "otakase.png"); path != want {
		t.Fatalf("icon at %s, want %s", path, want)
	}
	for _, rel := range []string{
		"icons/hicolor/256x256/apps/otakase.png",
		"icons/hicolor/scalable/apps/otakase.svg",
		"applications/otakase.desktop",
		"applications/otakase-link.desktop",
	} {
		if _, err := os.Stat(filepath.Join(data, rel)); err != nil {
			t.Fatalf("%s not written: %v", rel, err)
		}
	}

	// A second run with nothing changed rewrites nothing.
	entry := filepath.Join(data, "applications", "otakase.desktop")
	before, _ := os.Stat(entry)
	if _, ok := installLinux(); !ok {
		t.Fatal("second run failed")
	}
	after, _ := os.Stat(entry)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("unchanged desktop entry was rewritten")
	}
	// The link default is checked every run; the desktop database is only
	// refreshed when the entry was written.
	if want := filepath.Join(data, "applications"); len(registered) != 2 || registered[0] != want || registered[1] != "" {
		t.Fatalf("scheme registered %v, want %s then a check only", registered, want)
	}
}

// A package's copies win, and the user's data directory is left alone.
func TestInstallLinuxDefersToPackage(t *testing.T) {
	system := t.TempDir()
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_DATA_DIRS", system)
	png := filepath.Join(system, "icons", "hicolor", "256x256", "apps", "otakase.png")
	for _, path := range []string{png, filepath.Join(system, "applications", "otakase.desktop"), filepath.Join(system, "applications", LinkEntry)} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	path, ok := installLinux()
	if !ok || path != png {
		t.Fatalf("got %q, want the package's %q", path, png)
	}
	if entries, _ := os.ReadDir(data); len(entries) != 0 {
		t.Fatal("wrote user copies beside a package install")
	}
}

// User copies left from before the package, among them a menu entry that
// still claims otakase:// links, are removed so a link offers one app.
func TestInstallLinuxDropsStaleUserCopies(t *testing.T) {
	system := t.TempDir()
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_DATA_DIRS", system)
	var registered []string
	defer func(prev func(string)) { registerScheme = prev }(registerScheme)
	registerScheme = func(apps string) { registered = append(registered, apps) }
	write := func(path, body string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(system, "icons", "hicolor", "256x256", "apps", "otakase.png"), "x")
	write(filepath.Join(system, "applications", "otakase.desktop"), "x")
	write(filepath.Join(system, "applications", LinkEntry), "x")
	apps := filepath.Join(data, "applications")
	write(filepath.Join(apps, "otakase.desktop"), "[Desktop Entry]\nExec=otakase %u\nMimeType="+SchemeMime+";\n")
	write(filepath.Join(apps, LinkEntry), string(linkEntry))

	installLinux()
	for _, name := range []string{"otakase.desktop", LinkEntry} {
		if fileExists(filepath.Join(apps, name)) {
			t.Fatalf("stale %s left beside the package's", name)
		}
	}
	if len(registered) != 1 || registered[0] != apps {
		t.Fatalf("scheme registered %v, want a refresh of %s", registered, apps)
	}

	// A menu entry the user made without the scheme is theirs to keep.
	write(filepath.Join(apps, "otakase.desktop"), "[Desktop Entry]\nExec=otakase\n")
	installLinux()
	if !fileExists(filepath.Join(apps, "otakase.desktop")) {
		t.Fatal("removed a menu entry that does not claim the links")
	}
}

func TestDesktopEntry(t *testing.T) {
	entry := string(desktopEntry)
	for _, line := range []string{"Icon=" + Name, "Exec=otakase", "Terminal=true", "StartupWMClass=" + Name} {
		if !strings.Contains(entry, "\n"+line+"\n") {
			t.Fatalf("desktop entry is missing %q", line)
		}
	}
	// Links open in their own entry, without a terminal.
	if strings.Contains(entry, "MimeType=") {
		t.Fatal("the app-menu entry still claims otakase:// links")
	}
	link := string(linkEntry)
	for _, line := range []string{"Icon=" + Name, "Exec=otakase -rofi %u", "Terminal=false", "NoDisplay=true", "MimeType=" + SchemeMime + ";", "StartupWMClass=" + Name} {
		if !strings.Contains(link, "\n"+line+"\n") {
			t.Fatalf("link entry is missing %q", line)
		}
	}
	if !bytes.HasPrefix(pngData, []byte("\x89PNG")) || !bytes.HasPrefix(menuData, []byte("\x89PNG")) {
		t.Fatal("embedded icons are not PNGs")
	}
}

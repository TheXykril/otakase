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
}

// A package's copies win, and the user's data directory is left alone.
func TestInstallLinuxDefersToPackage(t *testing.T) {
	system := t.TempDir()
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_DATA_DIRS", system)
	png := filepath.Join(system, "icons", "hicolor", "256x256", "apps", "otakase.png")
	for _, path := range []string{png, filepath.Join(system, "applications", "otakase.desktop")} {
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

func TestDesktopEntry(t *testing.T) {
	entry := string(desktopEntry)
	for _, line := range []string{"Icon=" + Name, "Exec=otakase", "StartupWMClass=" + Name} {
		if !strings.Contains(entry, "\n"+line+"\n") {
			t.Fatalf("desktop entry is missing %q", line)
		}
	}
	if !bytes.HasPrefix(pngData, []byte("\x89PNG")) || !bytes.HasPrefix(menuData, []byte("\x89PNG")) {
		t.Fatal("embedded icons are not PNGs")
	}
}

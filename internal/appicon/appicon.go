// Package appicon carries the app icon and puts it where the desktop looks.
//
// The images are written by Build/app-icon from site/img/icon.svg; regenerate
// them there rather than editing these copies.
//
// On Linux the icon and an app-menu entry are installed for the user under
// ~/.local/share, the way a package would under /usr/share. Notifications,
// the rofi menus and the player window then all show it. Where a package has
// already installed them system-wide, nothing is written.
package appicon

import (
	"bytes"
	"context"
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// SchemeMime is the MIME type a desktop looks up to find the app that opens
// otakase:// links, which the site's Play in Otakase page hands over.
const SchemeMime = "x-scheme-handler/otakase"

// Name is the icon's name in the icon theme and the desktop entry's file
// name. The player window's app id is the same, which is how a desktop knows
// the window belongs to the entry.
const Name = "otakase"

//go:embed otakase.png
var pngData []byte

//go:embed otakase-menu.png
var menuData []byte

//go:embed otakase.svg
var svgData []byte

// LinkEntry is the desktop entry that opens otakase:// links. It is its own
// entry, without a terminal, so a link opens straight into the rofi menus
// instead of a terminal window; the app-menu entry keeps the terminal.
const LinkEntry = Name + "-link.desktop"

//go:embed otakase.desktop
var desktopEntry []byte

//go:embed otakase-link.desktop
var linkEntry []byte

// PNG is the 256 px icon.
func PNG() []byte { return pngData }

// MenuPNG is the icon at 112 px, the size rofi's 56 px icon takes on a
// screen scaled 2x.
func MenuPNG() []byte { return menuData }

// SVG is the scalable icon.
func SVG() []byte { return svgData }

var (
	pathOnce sync.Once
	pngPath  string
)

// Path returns a PNG of the icon on disk, writing it on first use, or "" when
// no copy could be written. It is what notifications and rofi are handed.
func Path() string {
	pathOnce.Do(func() {
		if runtime.GOOS == "linux" {
			if path, ok := installLinux(); ok {
				pngPath = path
				return
			}
		}
		dir, err := os.UserCacheDir()
		if err != nil {
			return
		}
		path := filepath.Join(dir, Name, "otakase-icon.png")
		if writeIfChanged(path, pngData) == nil {
			pngPath = path
		}
	})
	return pngPath
}

// systemDataDirs are where a package installs desktop files.
func systemDataDirs() []string {
	dirs := filepath.SplitList(os.Getenv("XDG_DATA_DIRS"))
	if len(dirs) == 0 {
		dirs = []string{"/usr/local/share", "/usr/share"}
	}
	return dirs
}

func userDataDir() string {
	if dir := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(dir) {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "share")
}

// installLinux makes sure the icon theme has the icon and the app menu has an
// entry, and returns the PNG's path. A package's copy wins: when one exists the
// user copies are left alone and the package's PNG is returned.
func installLinux() (string, bool) {
	png, ok, dropped := installMenuEntry()
	installLinkEntry(dropped)
	return png, ok
}

// installMenuEntry also says whether it changed the user's copy of the entry,
// so the desktop database gets refreshed.
func installMenuEntry() (string, bool, bool) {
	for _, dir := range systemDataDirs() {
		entry := filepath.Join(dir, "applications", Name+".desktop")
		png := filepath.Join(dir, "icons", "hicolor", "256x256", "apps", Name+".png")
		if fileExists(entry) && fileExists(png) {
			return png, true, dropStaleMenuEntry()
		}
	}
	data := userDataDir()
	if data == "" {
		return "", false, false
	}
	icons := filepath.Join(data, "icons", "hicolor")
	png := filepath.Join(icons, "256x256", "apps", Name+".png")
	if writeIfChanged(png, pngData) != nil {
		return "", false, false
	}
	_ = writeIfChanged(filepath.Join(icons, "scalable", "apps", Name+".svg"), svgData)
	wrote, _ := writeFile(filepath.Join(data, "applications", Name+".desktop"), desktopEntry)
	return png, true, wrote
}

// installLinkEntry puts the otakase:// entry in place, unless a package has,
// and makes it the default for the links. refresh asks for the user's desktop
// database to be refreshed even when nothing is written here.
func installLinkEntry(refresh bool) {
	data := userDataDir()
	apps := ""
	if data != "" {
		apps = filepath.Join(data, "applications")
	}
	for _, dir := range systemDataDirs() {
		if fileExists(filepath.Join(dir, "applications", LinkEntry)) {
			// A user copy left from before the package shadows it.
			if apps != "" && os.Remove(filepath.Join(apps, LinkEntry)) == nil {
				refresh = true
			}
			if !refresh {
				apps = ""
			}
			registerScheme(apps)
			return
		}
	}
	if apps == "" {
		return
	}
	wrote, err := writeFile(filepath.Join(apps, LinkEntry), linkEntry)
	if err != nil {
		return
	}
	if !wrote && !refresh {
		apps = ""
	}
	registerScheme(apps)
}

// dropStaleMenuEntry removes a user copy of the app-menu entry written before
// the link entry existed, and says whether it did. That copy shadows the
// package's, and still claims otakase:// links, so a link offered two apps.
func dropStaleMenuEntry() bool {
	data := userDataDir()
	if data == "" {
		return false
	}
	path := filepath.Join(data, "applications", Name+".desktop")
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(current, []byte("MimeType="+SchemeMime)) {
		return false
	}
	return os.Remove(path) == nil
}

// registerScheme makes the link entry the default for otakase:// links,
// refreshing the desktop database of apps first when an entry was just
// written there. Checked on every launch rather than only when the entry is
// written: an install from before the link entry existed left otakase.desktop
// as the default, and a package install writes nothing for the user. Best
// effort: a desktop without xdg-utils just goes without.
var registerScheme = func(apps string) {
	// Tests write the entry into scratch folders, and mimeapps.list lives in
	// the real config directory.
	if testing.Testing() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if apps != "" {
		_ = exec.CommandContext(ctx, "update-desktop-database", apps).Run()
	}
	current, err := exec.CommandContext(ctx, "xdg-mime", "query", "default", SchemeMime).Output()
	if err == nil && strings.TrimSpace(string(current)) == LinkEntry {
		return
	}
	_ = exec.CommandContext(ctx, "xdg-mime", "default", LinkEntry, SchemeMime).Run()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// writeIfChanged writes data to path unless the file already holds exactly
// that, so a run that changes nothing touches nothing.
func writeIfChanged(path string, data []byte) error {
	_, err := writeFile(path, data)
	return err
}

// writeFile is writeIfChanged that also says whether it wrote.
func writeFile(path string, data []byte) (bool, error) {
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, data) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return false, err
	}
	return true, os.Rename(tmp, path)
}

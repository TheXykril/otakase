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
	_ "embed"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// Name is the icon's name in the icon theme and the desktop entry's file
// name. The player window's app id is the same, which is how a desktop knows
// the window belongs to the entry.
const Name = "otakase"

//go:embed otakase.png
var pngData []byte

//go:embed otakase-small.png
var smallData []byte

//go:embed otakase.svg
var svgData []byte

//go:embed otakase.desktop
var desktopEntry []byte

// PNG is the 256 px icon.
func PNG() []byte { return pngData }

// SmallPNG is 任 alone at 64 px, for where the full icon's lettering would be
// too small to read, like beside a search box.
func SmallPNG() []byte { return smallData }

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
	for _, dir := range systemDataDirs() {
		entry := filepath.Join(dir, "applications", Name+".desktop")
		png := filepath.Join(dir, "icons", "hicolor", "256x256", "apps", Name+".png")
		if fileExists(entry) && fileExists(png) {
			return png, true
		}
	}
	data := userDataDir()
	if data == "" {
		return "", false
	}
	icons := filepath.Join(data, "icons", "hicolor")
	png := filepath.Join(icons, "256x256", "apps", Name+".png")
	if writeIfChanged(png, pngData) != nil {
		return "", false
	}
	_ = writeIfChanged(filepath.Join(icons, "scalable", "apps", Name+".svg"), svgData)
	_ = writeIfChanged(filepath.Join(data, "applications", Name+".desktop"), desktopEntry)
	return png, true
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// writeIfChanged writes data to path unless the file already holds exactly
// that, so a run that changes nothing touches nothing.
func writeIfChanged(path string, data []byte) error {
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

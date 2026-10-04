package mpvskin

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MinMPV is the oldest mpv the skin is passed to. --osd-fonts-dir, which the
// icons depend on, arrived in 0.37, and an mpv that does not know a flag
// refuses to start at all.
var MinMPV = [2]int{0, 37}

var mpvVersionPattern = regexp.MustCompile(`(?i)\bmpv\s+v?(\d+)\.(\d+)`)

// ParseMPVVersion reads major and minor from `mpv --version` output.
func ParseMPVVersion(output string) (major, minor int, ok bool) {
	match := mpvVersionPattern.FindStringSubmatch(output)
	if match == nil {
		return 0, 0, false
	}
	major, errMajor := strconv.Atoi(match[1])
	minor, errMinor := strconv.Atoi(match[2])
	if errMajor != nil || errMinor != nil {
		return 0, 0, false
	}
	return major, minor, true
}

// VersionSupported reports whether major.minor is at least MinMPV.
func VersionSupported(major, minor int) bool {
	if major != MinMPV[0] {
		return major > MinMPV[0]
	}
	return minor >= MinMPV[1]
}

var (
	versionMu    sync.Mutex
	versionCache = map[string]bool{}
)

// BinarySupported asks the mpv at path for its version, once per path. A
// version that cannot be read counts as too old: guessing wrong would stop mpv
// from starting.
func BinarySupported(path string) bool {
	versionMu.Lock()
	defer versionMu.Unlock()
	if supported, ok := versionCache[path]; ok {
		return supported
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--no-config", "--version").Output()
	supported := false
	if err == nil {
		if major, minor, ok := ParseMPVVersion(string(out)); ok {
			supported = VersionSupported(major, minor)
		}
	}
	versionCache[path] = supported
	return supported
}

// IsMPVBinary reports whether path is mpv itself rather than another player
// that takes mpv's flags.
func IsMPVBinary(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	name = strings.TrimSuffix(strings.TrimSuffix(name, ".exe"), ".com")
	return name == "mpv"
}

// UserConfig is what the user's own mpv configuration says that matters here.
type UserConfig struct {
	// OwnSkin names the script or setting that shows the user already has a
	// look of their own, or is empty.
	OwnSkin string
	// SetsOSDFont is true when mpv.conf picks an OSD font, which otakase then
	// leaves alone.
	SetsOSDFont bool
}

// skinScripts are the starts of script names that replace mpv's controls.
var skinScripts = []string{"uosc", "modernx", "modernz", "mfpbar", "mpv-osc", "osc", "tethys", "mordenx"}

// ConfigDirs are the directories mpv reads its configuration from, for the mpv
// binary at mpvPath, most specific first.
func ConfigDirs(mpvPath string) []string {
	var dirs []string
	if home := strings.TrimSpace(os.Getenv("MPV_HOME")); home != "" {
		dirs = append(dirs, home)
	}
	if runtime.GOOS == "windows" {
		if mpvPath != "" {
			dirs = append(dirs, filepath.Join(filepath.Dir(mpvPath), "portable_config"))
		}
		if appData := os.Getenv("APPDATA"); appData != "" {
			dirs = append(dirs, filepath.Join(appData, "mpv"))
		}
		return dirs
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		dirs = append(dirs, filepath.Join(xdg, "mpv"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".config", "mpv"), filepath.Join(home, ".mpv"))
	}
	return dirs
}

// ReadUserConfig looks through dirs for a skin script or osc=no, and for an
// osd-font setting.
func ReadUserConfig(dirs []string) UserConfig {
	var found UserConfig
	for _, dir := range dirs {
		if found.OwnSkin == "" {
			if entries, err := os.ReadDir(filepath.Join(dir, "scripts")); err == nil {
				for _, entry := range entries {
					if name := skinScriptName(entry.Name()); name != "" {
						found.OwnSkin = name
						break
					}
				}
			}
		}
		settings := readConf(filepath.Join(dir, "mpv.conf"))
		if found.OwnSkin == "" && (settings["osc"] == "no" || settings["no-osc"] == "yes") {
			found.OwnSkin = "osc=no in mpv.conf"
		}
		if _, ok := settings["osd-font"]; ok {
			found.SetsOSDFont = true
		}
	}
	return found
}

func skinScriptName(file string) string {
	if strings.HasPrefix(file, ".") {
		return ""
	}
	name := strings.ToLower(file)
	if strings.HasSuffix(name, ".disable") || strings.HasSuffix(name, ".disabled") {
		return "" // switched off by renaming
	}
	name = strings.TrimSuffix(strings.TrimSuffix(name, ".lua"), ".js")
	for _, prefix := range skinScripts {
		if strings.HasPrefix(name, prefix) {
			return file
		}
	}
	return ""
}

// readConf reads the top-level options of an mpv.conf, before any [profile]
// section. A bare "name" means yes, as mpv reads it.
func readConf(path string) map[string]string {
	settings := map[string]string{}
	file, err := os.Open(path)
	if err != nil {
		return settings
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			break
		}
		line = strings.TrimPrefix(line, "--")
		key, value, hasValue := strings.Cut(line, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if !hasValue {
			value = "yes"
		}
		settings[key] = strings.ToLower(value)
	}
	return settings
}

// ArgsOverride reports whether the user's own MpvArgs already decide what the
// controls look like, which otakase then leaves to them.
func ArgsOverride(args []string) bool {
	for _, arg := range args {
		flag := strings.ToLower(strings.TrimSpace(arg))
		switch {
		case flag == "--osc=no", flag == "--no-osc", flag == "--osc=yes", flag == "--osc":
			return true
		case strings.HasPrefix(flag, "--script=") || strings.HasPrefix(flag, "--scripts="):
			return true
		case flag == "--no-config" || flag == "--load-scripts=no" || flag == "--no-load-scripts":
			return true
		}
	}
	return false
}

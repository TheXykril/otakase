// Package mpvskin gives mpv otakase's own look while otakase plays in it.
//
// The look is otakase_skin.lua, an on-screen controller drawn in the active
// palette: a top bar with the title and chips, a seek bar with openings and
// endings marked, the control row with track and episode menus, a skip button
// for openings and endings, the undo notice after an automatic skip and the
// next-episode card. Its icons are Material Icons Round (Apache-2.0).
//
// Nothing is installed into the user's mpv. The script and font are embedded
// in the binary, written to otakase's storage directory, and handed to the one
// mpv process otakase starts with --script and --osd-fonts-dir. Their mpv.conf,
// input.conf and scripts still load as always. Someone who already runs a skin
// of their own keeps it: otakase steps aside rather than stacking a second one.
package mpvskin

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ScriptName is the name mpv gives otakase_skin.lua, which script-message-to
// addresses it by.
const ScriptName = "otakase_skin"

//go:embed all:assets
var assets embed.FS

// Mode is the MpvSkin setting.
type Mode string

const (
	// ModeAuto uses the skin unless the user's mpv already has one.
	ModeAuto Mode = "auto"
	// ModeOn uses it regardless.
	ModeOn Mode = "true"
	// ModeOff leaves mpv's look alone.
	ModeOff Mode = "false"
)

// ParseMode reads the MpvSkin value. Anything unrecognised is auto.
func ParseMode(raw string) Mode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "false", "off", "no", "0":
		return ModeOff
	case "true", "on", "yes", "1":
		return ModeOn
	default:
		return ModeAuto
	}
}

// Colors are the palette roles the skin draws with, each "#rrggbb".
type Colors struct {
	Background string
	Surface    string // menus and cards, a step up from Background
	Foreground string
	Bright     string // the title, hovered buttons
	Dim        string
	Accent     string
	// AccentText is readable on Accent: the play button's icon.
	AccentText string
	Highlight  string // openings and endings on the seek bar
}

// Options is everything Args needs to know about this launch.
type Options struct {
	Dir    string // where Install wrote the files
	Colors Colors
	// Font is the family for the skin's text, set as mpv's OSD font; empty
	// keeps the one mpv has.
	Font string
	// SkipOp and SkipEd say otakase skips these by itself, so the skin offers
	// an undo rather than a skip button.
	SkipOp bool
	SkipEd bool
}

// hideOthers are the script options that keep skins the user runs themselves
// from drawing their controls under this one. They are always passed: such a
// script can load from places otakase does not look (a system-wide scripts
// folder, a package), and a script that is not loaded never reads them.
var hideOthers = []string{
	"uosc-disable_elements=timeline,controls,volume,top_bar,speed,idle_indicator,audio_indicator,buffering_indicator,pause_indicator",
	"modernz-visibility=never",
	"modernx-visibility=never",
}

var (
	digestOnce sync.Once
	digest     string
)

// assetDigest names the installed copy after its contents, so an otakase
// update with a different otakase_skin.lua never runs against files a
// previous version left behind.
func assetDigest() string {
	digestOnce.Do(func() {
		hash := sha256.New()
		var paths []string
		_ = fs.WalkDir(assets, "assets", func(path string, entry fs.DirEntry, err error) error {
			if err == nil && !entry.IsDir() {
				paths = append(paths, path)
			}
			return nil
		})
		sort.Strings(paths)
		for _, path := range paths {
			data, err := assets.ReadFile(path)
			if err != nil {
				continue
			}
			fmt.Fprintf(hash, "%s\x00%d\x00", path, len(data))
			hash.Write(data)
		}
		digest = hex.EncodeToString(hash.Sum(nil))[:12]
	})
	return digest
}

// Install writes the skin under storage and returns the directory it is in.
// A copy already there from this same build is reused; copies from other
// builds are removed.
func Install(storage string) (string, error) {
	if strings.TrimSpace(storage) == "" {
		return "", fmt.Errorf("no storage directory")
	}
	root := filepath.Join(storage, "mpv-skin")
	dir := filepath.Join(root, assetDigest())
	marker := filepath.Join(dir, ".complete")

	if _, err := os.Stat(marker); err != nil {
		// Written to a temporary sibling and renamed into place, so a crash or
		// a second otakase starting at the same moment never leaves mpv a half
		// written script to load.
		if err := os.MkdirAll(root, 0o755); err != nil {
			return "", err
		}
		tmp, err := os.MkdirTemp(root, ".install-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(tmp)
		if err := writeAssets(tmp); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(tmp, ".complete"), []byte(assetDigest()+"\n"), 0o644); err != nil {
			return "", err
		}
		// A copy without its marker is one an older otakase left half written.
		_ = os.RemoveAll(dir)
		if err := os.Rename(tmp, dir); err != nil {
			// Another otakase got there first; its copy is the same files.
			if _, statErr := os.Stat(marker); statErr != nil {
				return "", err
			}
		}
	}

	if entries, err := os.ReadDir(root); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if name == filepath.Base(dir) || strings.HasPrefix(name, ".") {
				continue
			}
			_ = os.RemoveAll(filepath.Join(root, name))
		}
	}
	return dir, nil
}

func writeAssets(target string) error {
	return fs.WalkDir(assets, "assets", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("assets", filepath.FromSlash(path))
		if err != nil {
			return err
		}
		out := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		data, err := assets.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o644)
	})
}

// Args are the mpv flags that load the skin.
//
// Script options go in one --script-opts-append each, which takes the whole
// value as one option. Passed on the command line, they override the user's
// script-opts/otakase_skin.conf for this process only.
func Args(opts Options) []string {
	c := opts.Colors
	hex := func(color string) string { return strings.TrimPrefix(strings.TrimSpace(color), "#") }

	skin := [][2]string{
		{"background", hex(c.Background)},
		{"surface", hex(c.Surface)},
		{"foreground", hex(c.Foreground)},
		{"bright", hex(c.Bright)},
		{"dim", hex(c.Dim)},
		{"accent", hex(c.Accent)},
		{"accent_text", hex(c.AccentText)},
		{"highlight", hex(c.Highlight)},
		{"skip_op", yesNo(opts.SkipOp)},
		{"skip_ed", yesNo(opts.SkipEd)},
		{"logo", filepath.Join(opts.Dir, "logo", "otakase.ass")},
	}

	args := []string{
		"--osc=no",
		// mpv's own bar for seeks, volume and speed would show under the
		// skin's controls, which flash up for those already.
		"--osd-bar=no",
		"--osd-on-seek=no",
		"--script=" + filepath.Join(opts.Dir, "scripts", ScriptName+".lua"),
		"--osd-fonts-dir=" + filepath.Join(opts.Dir, "fonts"),
	}
	if font := strings.TrimSpace(opts.Font); font != "" {
		// The skin draws its text in mpv's OSD font.
		args = append(args, "--osd-font="+font)
	}
	for _, kv := range skin {
		if kv[1] == "" {
			continue
		}
		args = append(args, "--script-opts-append="+ScriptName+"-"+kv[0]+"="+kv[1])
	}
	for _, kv := range hideOthers {
		args = append(args, "--script-opts-append="+kv)
	}
	return args
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

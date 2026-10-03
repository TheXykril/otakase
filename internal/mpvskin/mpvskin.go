// Package mpvskin gives mpv otakase's own look while otakase plays in it.
//
// The look is uosc (https://github.com/tomasklaen/uosc, LGPL-2.1), coloured
// from the active palette, plus otakase_skin.lua for what is otakase's own: a
// skip button for openings and endings, the undo notice after an automatic
// skip, the next-episode card and the chips under the top bar.
//
// Nothing is installed into the user's mpv. The scripts and fonts are embedded
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

// UOSCVersion is the uosc release embedded under assets/scripts/uosc.
const UOSCVersion = "5.13.0"

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
	Foreground string
	Dim        string
	Accent     string
	// AccentText is readable on Accent: the play button's icon, a hovered
	// button's label.
	AccentText string
	Highlight  string // openings and endings on the timeline
	Success    string
	Error      string
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

var (
	digestOnce sync.Once
	digest     string
)

// assetDigest names the installed copy after its contents, so an otakase
// update with a different uosc or otakase_skin.lua never runs against files a
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
		if err := os.WriteFile(filepath.Join(tmp, ".complete"), []byte(UOSCVersion+"\n"), 0o644); err != nil {
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

// uoscControls is uosc's default control bar without the controls an anime
// episode has no use for (shuffle, loop, speed).
const uoscControls = "menu,gap,<video,audio>subtitles,<has_many_audio>audio,<has_many_video>video," +
	"<has_many_edition>editions,<stream>stream-quality,gap,space,prev,items,next,gap,fullscreen"

// Args are the mpv flags that load the skin.
//
// Script options go in one --script-opts-append each: that form takes the
// whole value as one option, so the commas inside uosc's lists survive.
// Passed on the command line, they override the user's script-opts/uosc.conf
// for this process only.
func Args(opts Options) []string {
	c := opts.Colors
	hex := func(color string) string { return strings.TrimPrefix(strings.TrimSpace(color), "#") }

	uosc := [][2]string{
		{"top_bar", "always"},
		{"timeline_style", "bar"},
		{"controls", uoscControls},
		{"border_radius", "6"},
		// The updater and the clipboard need uosc's helper binary, which is
		// not shipped: it is 18 MB for features otakase does not use.
		{"disable_elements", "updater"},
		{"color", strings.Join([]string{
			"foreground=" + hex(c.Accent),
			"foreground_text=" + hex(c.AccentText),
			"background=" + hex(c.Background),
			"background_text=" + hex(c.Foreground),
			"curtain=" + hex(c.Background),
			"success=" + hex(c.Success),
			"error=" + hex(c.Error),
			"match=" + hex(c.Accent),
			"heatmap=" + hex(c.Accent),
		}, ",")},
		{"chapter_ranges", fmt.Sprintf("openings:%sbb,endings:%sbb,ads:%s80", hex(c.Highlight), hex(c.Highlight), hex(c.Error))},
	}
	skin := [][2]string{
		{"background", hex(c.Background)},
		{"foreground", hex(c.Foreground)},
		{"dim", hex(c.Dim)},
		{"accent", hex(c.Accent)},
		{"accent_text", hex(c.AccentText)},
		{"skip_op", yesNo(opts.SkipOp)},
		{"skip_ed", yesNo(opts.SkipEd)},
	}

	scripts := filepath.Join(opts.Dir, "scripts")
	args := []string{
		"--osc=no",
		"--script=" + filepath.Join(scripts, "uosc"),
		"--script=" + filepath.Join(scripts, ScriptName+".lua"),
		"--osd-fonts-dir=" + filepath.Join(opts.Dir, "fonts"),
	}
	if font := strings.TrimSpace(opts.Font); font != "" {
		// uosc draws with mpv's OSD font, so this is how its text is set too.
		args = append(args, "--osd-font="+font)
	}
	for _, kv := range uosc {
		args = append(args, "--script-opts-append=uosc-"+kv[0]+"="+kv[1])
	}
	for _, kv := range skin {
		args = append(args, "--script-opts-append="+ScriptName+"-"+kv[0]+"="+kv[1])
	}
	return args
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

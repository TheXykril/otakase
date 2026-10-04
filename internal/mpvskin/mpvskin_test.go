package mpvskin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMode(t *testing.T) {
	cases := map[string]Mode{
		"":      ModeAuto,
		"auto":  ModeAuto,
		"junk":  ModeAuto,
		"true":  ModeOn,
		" On ":  ModeOn,
		"false": ModeOff,
		"no":    ModeOff,
	}
	for raw, want := range cases {
		if got := ParseMode(raw); got != want {
			t.Errorf("ParseMode(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestParseMPVVersion(t *testing.T) {
	cases := []struct {
		out          string
		major, minor int
		ok           bool
	}{
		{"mpv 0.37.0 Copyright © 2000-2023 mpv/MPlayer/mplayer2 projects", 0, 37, true},
		{"mpv v0.41.0 Copyright © 2000-2025 mpv/MPlayer/mplayer2 projects", 0, 41, true},
		{"mpv 0.38.0-473-g6f4a2c1 Copyright", 0, 38, true},
		{"mpv v0.36.0-dirty", 0, 36, true},
		{"IINA 1.3", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, c := range cases {
		major, minor, ok := ParseMPVVersion(c.out)
		if ok != c.ok || major != c.major || minor != c.minor {
			t.Errorf("ParseMPVVersion(%q) = %d.%d %t, want %d.%d %t", c.out, major, minor, ok, c.major, c.minor, c.ok)
		}
	}
}

func TestVersionSupported(t *testing.T) {
	if VersionSupported(0, 36) {
		t.Error("0.36 has no --osd-fonts-dir and must be refused")
	}
	for _, v := range [][2]int{{0, 37}, {0, 41}, {1, 0}} {
		if !VersionSupported(v[0], v[1]) {
			t.Errorf("%d.%d should be supported", v[0], v[1])
		}
	}
}

func TestIsMPVBinary(t *testing.T) {
	for _, path := range []string{"/usr/bin/mpv", filepath.Join("bin", "mpv.exe"), "MPV.COM"} {
		if !IsMPVBinary(path) {
			t.Errorf("%s is mpv", path)
		}
	}
	for _, path := range []string{"/usr/bin/celluloid", "/Applications/IINA.app/Contents/MacOS/iina-cli", "mpvnet"} {
		if IsMPVBinary(path) {
			t.Errorf("%s is not mpv", path)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadUserConfigFindsOwnSkin(t *testing.T) {
	dir := t.TempDir()
	if got := ReadUserConfig([]string{dir}); got.OwnSkin != "" || got.SetsOSDFont {
		t.Fatalf("empty config dir reported %+v", got)
	}

	if err := os.MkdirAll(filepath.Join(dir, "scripts", "uosc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ReadUserConfig([]string{dir}); got.OwnSkin != "uosc" {
		t.Errorf("uosc directory not found as a skin: %+v", got)
	}

	other := t.TempDir()
	writeFile(t, filepath.Join(other, "scripts", "ModernZ.lua"), "")
	if got := ReadUserConfig([]string{other}); got.OwnSkin != "ModernZ.lua" {
		t.Errorf("ModernZ.lua not found as a skin: %+v", got)
	}

	disabled := t.TempDir()
	writeFile(t, filepath.Join(disabled, "scripts", "uosc.lua.disabled"), "")
	writeFile(t, filepath.Join(disabled, "scripts", "thumbfast.lua"), "")
	if got := ReadUserConfig([]string{disabled}); got.OwnSkin != "" {
		t.Errorf("a renamed-off skin and thumbfast are not skins: %+v", got)
	}
}

func TestReadUserConfigMPVConf(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mpv.conf"), "# osc=no\nvolume=80\nosd-font = 'Inter'\n\n[anime]\nosc=no\n")
	got := ReadUserConfig([]string{dir})
	if got.OwnSkin != "" {
		t.Errorf("osc=no in a comment or a profile must not count: %+v", got)
	}
	if !got.SetsOSDFont {
		t.Error("osd-font in mpv.conf not seen")
	}

	off := t.TempDir()
	writeFile(t, filepath.Join(off, "mpv.conf"), "osc = no\n")
	if got := ReadUserConfig([]string{off}); got.OwnSkin == "" {
		t.Error("osc=no in mpv.conf should step aside")
	}
	bare := t.TempDir()
	writeFile(t, filepath.Join(bare, "mpv.conf"), "no-osc\n")
	if got := ReadUserConfig([]string{bare}); got.OwnSkin == "" {
		t.Error("no-osc in mpv.conf should step aside")
	}
}

func TestArgsOverride(t *testing.T) {
	for _, args := range [][]string{
		{"--osc=no"}, {"--no-osc"}, {"--script=/home/me/uosc"}, {"--no-config"},
	} {
		if !ArgsOverride(args) {
			t.Errorf("%v should leave the controls to the user", args)
		}
	}
	for _, args := range [][]string{nil, {"--volume=50", "--sub-scale=1.2"}, {"--script-opts=foo=bar"}} {
		if ArgsOverride(args) {
			t.Errorf("%v does not touch the controls", args)
		}
	}
}

func TestInstallWritesReusesAndCleans(t *testing.T) {
	storage := t.TempDir()
	stale := filepath.Join(storage, "mpv-skin", "000000000000")
	writeFile(t, filepath.Join(stale, "scripts", "otakase_skin.lua"), "old")

	dir, err := Install(storage)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"scripts/otakase_skin.lua",
		"fonts/MaterialIconsRound.otf",
		"LICENSE.MaterialIcons",
		".complete",
	} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s missing: %v", rel, err)
		}
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("an older copy should be removed")
	}

	again, err := Install(storage)
	if err != nil || again != dir {
		t.Fatalf("second install = %q, %v; want %q", again, err, dir)
	}
	entries, _ := os.ReadDir(filepath.Join(storage, "mpv-skin"))
	if len(entries) != 1 {
		t.Errorf("want one installed copy, have %d", len(entries))
	}
}

func TestArgs(t *testing.T) {
	args := Args(Options{
		Dir: "/store/mpv-skin/abc",
		Colors: Colors{
			Background: "#1a1b26", Surface: "#24283b", Foreground: "#a9b1d6", Bright: "#c0caf5",
			Dim: "#787c99", Accent: "#7aa2f7", AccentText: "#1a1b26", Highlight: "#e0af68",
		},
		Font:   "JetBrainsMono Nerd Font",
		SkipOp: true,
	})
	joined := strings.Join(args, "\n")
	for _, want := range []string{
		"--osc=no",
		"--script=" + filepath.Join("/store/mpv-skin/abc", "scripts", "otakase_skin.lua"),
		"--osd-fonts-dir=" + filepath.Join("/store/mpv-skin/abc", "fonts"),
		"--osd-font=JetBrainsMono Nerd Font",
		"--script-opts-append=otakase_skin-background=1a1b26",
		"--script-opts-append=otakase_skin-surface=24283b",
		"--script-opts-append=otakase_skin-accent=7aa2f7",
		"--script-opts-append=otakase_skin-highlight=e0af68",
		"--script-opts-append=otakase_skin-skip_op=yes",
		"--script-opts-append=otakase_skin-skip_ed=no",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q:\n%s", want, joined)
		}
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "--script-opts=") {
			t.Errorf("%q: options go in --script-opts-append", arg)
		}
		if strings.Contains(arg, "#") {
			t.Errorf("%q: the script wants colours without #", arg)
		}
		if strings.Contains(arg, "uosc") {
			t.Errorf("%q: uosc is not shipped", arg)
		}
	}

	got := strings.Join(Args(Options{Dir: "/x"}), "\n")
	if strings.Contains(got, "--osd-font=") {
		t.Error("no font given should keep mpv's own OSD font")
	}
	if strings.Contains(got, "-background=") {
		t.Error("an empty colour should keep the script's default")
	}
}

package internal

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeMPV writes an executable named mpv that answers --version like mpv
// version would.
func fakeMPV(t *testing.T, version string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell script standing in for mpv")
	}
	path := filepath.Join(t.TempDir(), "mpv")
	script := "#!/bin/sh\necho 'mpv " + version + " Copyright (C) 2000-2025 mpv/MPlayer/mplayer2 projects'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// isolateMPVConfig points mpv's config lookup at an empty directory, so a
// developer's own skin does not decide the test.
func isolateMPVConfig(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("MPV_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("HOME", home)
	return home
}

func TestMPVSkinArgsGates(t *testing.T) {
	mpvHome := isolateMPVConfig(t)
	binary := fakeMPV(t, "0.38.0")
	config := &Config{MpvSkin: "auto", StoragePath: t.TempDir()}

	args := mpvSkinArgs(config, binary, true)
	if len(args) == 0 || args[0] != "--osc=no" {
		t.Fatalf("auto with no skin of the user's own should load ours, got %v", args)
	}

	off := *config
	off.MpvSkin = "false"
	if got := mpvSkinArgs(&off, binary, true); got != nil {
		t.Errorf("MpvSkin=false still added %v", got)
	}

	if got := mpvSkinArgs(config, filepath.Join(filepath.Dir(binary), "celluloid"), true); got != nil {
		t.Errorf("a player that is not mpv got %v", got)
	}

	withArgs := *config
	withArgs.MpvArgs = []string{"--osc=no"}
	if got := mpvSkinArgs(&withArgs, binary, true); got != nil {
		t.Errorf("MpvArgs deciding the controls still got %v", got)
	}

	if got := mpvSkinArgs(config, fakeMPV(t, "0.36.0"), true); got != nil {
		t.Errorf("mpv 0.36 cannot take --osd-fonts-dir and must not get %v", got)
	}

	if err := os.MkdirAll(filepath.Join(mpvHome, "scripts", "uosc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := mpvSkinArgs(config, binary, true); got != nil {
		t.Errorf("auto with the user's own uosc should step aside, got %v", got)
	}
	forced := *config
	forced.MpvSkin = "true"
	if got := mpvSkinArgs(&forced, binary, true); len(got) == 0 {
		t.Error("MpvSkin=true should load the skin even beside the user's own")
	}
}

func TestMPVSkinArgsLeaveTheUsersOSDFont(t *testing.T) {
	mpvHome := isolateMPVConfig(t)
	binary := fakeMPV(t, "0.41.0")
	if err := os.WriteFile(filepath.Join(mpvHome, "mpv.conf"), []byte("osd-font=Inter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	args := mpvSkinArgs(&Config{MpvSkin: "auto", StoragePath: t.TempDir()}, binary, true)
	if len(args) == 0 {
		t.Fatal("skin not loaded")
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "--osd-font=") {
			t.Errorf("mpv.conf picks an OSD font, yet otakase set %q", arg)
		}
	}
}

func TestMPVSkinChips(t *testing.T) {
	anime := &Anime{}
	chips := mpvSkinChips(&Config{TrackingRemote: TrackingRemoteAniList}, anime)
	if len(chips) == 0 || chips[0].Text != "AniList" {
		t.Errorf("AniList tracking chip missing: %+v", chips)
	}
	chips = mpvSkinChips(&Config{TrackingRemote: TrackingRemoteBoth}, anime)
	if len(chips) == 0 || chips[0].Text != "AniList + MAL" {
		t.Errorf("both trackers chip missing: %+v", chips)
	}
	chips = mpvSkinChips(&Config{TrackingRemote: TrackingRemoteNone}, anime)
	for _, chip := range chips {
		if chip.Icon == "sync" {
			t.Errorf("no tracker, yet a tracker chip: %+v", chips)
		}
	}
	untracked := &Anime{SkipRemoteSync: true}
	for _, chip := range mpvSkinChips(&Config{TrackingRemote: TrackingRemoteAniList}, untracked) {
		if chip.Icon == "sync" {
			t.Errorf("a show kept off the tracker shows a tracker chip: %+v", chip)
		}
	}
}

func TestMPVSkinEpisodeTitle(t *testing.T) {
	anime := &Anime{}
	if got := mpvSkinEpisodeTitle(anime); got != "" {
		t.Errorf("no title known = %q, want empty", got)
	}
	anime.Ep.Title = AnimeTitle{Romaji: "Doukyou no Kyoudai", English: " Aversion Between Same-Sex Siblings "}
	if got := mpvSkinEpisodeTitle(anime); got != "Aversion Between Same-Sex Siblings" {
		t.Errorf("English title = %q", got)
	}
	anime.Ep.Title.English = ""
	if got := mpvSkinEpisodeTitle(anime); got != "Doukyou no Kyoudai" {
		t.Errorf("romaji fallback = %q", got)
	}
	if got := mpvSkinEpisodeTitle(nil); got != "" {
		t.Errorf("nil anime = %q", got)
	}
}

// Films and series have no skip times: the skin offers no skip times menu and
// lists none of their keys, whatever ContributeSkipTimes says.
func TestMPVSkinArgsWithoutSkipTimes(t *testing.T) {
	isolateMPVConfig(t)
	binary := fakeMPV(t, "0.38.0")
	config := &Config{MpvSkin: "true", ContributeSkipTimes: true, StoragePath: t.TempDir()}

	anime := strings.Join(mpvSkinArgs(config, binary, true), "\n")
	for _, want := range []string{"otakase_skin-contribute=yes", "otakase_skin-skips=yes"} {
		if !strings.Contains(anime, want) {
			t.Errorf("anime: args missing %q:\n%s", want, anime)
		}
	}
	film := strings.Join(mpvSkinArgs(config, binary, false), "\n")
	for _, want := range []string{"otakase_skin-contribute=no", "otakase_skin-skips=no"} {
		if !strings.Contains(film, want) {
			t.Errorf("film: args missing %q:\n%s", want, film)
		}
	}
}

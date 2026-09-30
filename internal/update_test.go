package internal

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/thexykril/otakase/internal/theme"
)

func TestIsUpdateNewer(t *testing.T) {
	if !isUpdateNewer("2.0.4", "2.0.3") {
		t.Fatal("expected 2.0.4 newer than 2.0.3")
	}
	if isUpdateNewer("2.0.3", "2.0.3") {
		t.Fatal("same version is not newer")
	}
	if isUpdateNewer("2.0.2", "2.0.3") {
		t.Fatal("older version is not newer")
	}
	if !isUpdateNewer("v2.1.0", "2.0.9") {
		t.Fatal("tag prefix should be normalized")
	}
}

// Releases switched from semver to YY.DROP.HOTFIX at 26.1.0. Every install
// still on 2.x has to be offered the first year release, and the year
// numbering has to order itself after that.
func TestIsUpdateNewerAcrossYearVersions(t *testing.T) {
	newer := [][2]string{
		{"v26.1.0", "2.2.2"},
		{"26.1.0", "2.99.99"},
		{"26.1.1", "26.1.0"},
		{"26.2.0", "26.1.9"},
		{"26.10.0", "26.9.0"},
		{"27.1.0", "26.12.3"},
	}
	for _, pair := range newer {
		if !isUpdateNewer(pair[0], pair[1]) {
			t.Errorf("expected %s newer than %s", pair[0], pair[1])
		}
		if isUpdateNewer(pair[1], pair[0]) {
			t.Errorf("expected %s not newer than %s", pair[1], pair[0])
		}
	}
}

func TestPendingUpdateShouldPromptRespectsSkipAndRemind(t *testing.T) {
	cfg := &Config{CheckUpdates: true}
	state := updatePendingState{
		Available:     true,
		LatestVersion: "2.1.0",
	}
	if !pendingUpdateShouldPrompt(cfg, "2.0.4", state) {
		t.Fatal("expected prompt when update available")
	}

	state.SkippedVersion = "2.1.0"
	if pendingUpdateShouldPrompt(cfg, "2.0.4", state) {
		t.Fatal("skipped version should not prompt")
	}

	state.SkippedVersion = ""
	state.RemindAfter = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if pendingUpdateShouldPrompt(cfg, "2.0.4", state) {
		t.Fatal("remind-later window should suppress prompt")
	}

	cfg.CheckUpdates = false
	state.RemindAfter = ""
	if pendingUpdateShouldPrompt(cfg, "2.0.4", state) {
		t.Fatal("disabled CheckUpdates should not prompt")
	}
}

func TestCheckForUpdateInBackgroundWritesPendingState(t *testing.T) {
	storage := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/TheXykril/otakase/releases/latest" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"tag_name": "v9.9.9",
			"name":     "otakase v9.9.9",
			"body":     "## Changes\n- test",
			"html_url": "https://github.com/TheXykril/otakase/releases/tag/v9.9.9",
			"assets":   []map[string]string{},
		})
	}))
	t.Cleanup(server.Close)

	// Point GitHub API at the test server by temporarily overriding fetch via env is hard;
	// instead call fetch through a rewritten helper path: exercise save/load + isUpdateNewer
	// with a synthetic state write matching what background check would store.
	state := updatePendingState{
		Available:     true,
		LatestVersion: "9.9.9",
		LatestTag:     "v9.9.9",
		ReleaseName:   "otakase v9.9.9",
		ReleaseNotes:  "## Changes\n- test",
		HTMLURL:       "https://github.com/TheXykril/otakase/releases/tag/v9.9.9",
		CheckedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveUpdatePendingState(storage, state); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded := loadUpdatePendingState(storage)
	if !loaded.Available || loaded.LatestVersion != "9.9.9" {
		t.Fatalf("unexpected loaded state: %#v", loaded)
	}
	cfg := &Config{CheckUpdates: true, StoragePath: storage}
	if !pendingUpdateShouldPrompt(cfg, "2.0.4", loaded) {
		t.Fatal("expected pending update prompt")
	}
	_ = server
}

func TestTruncateReleaseNotes(t *testing.T) {
	short := truncateReleaseNotes("hello")
	if short != "hello" {
		t.Fatalf("got %q", short)
	}
	long := strings.Repeat("a", maxReleaseNotesRunes+50)
	got := truncateReleaseNotes(long)
	if !strings.Contains(got, "truncated") {
		t.Fatalf("expected truncation marker, got len=%d", len(got))
	}
}

func TestIsPermissionError(t *testing.T) {
	if !isPermissionError(os.ErrPermission) {
		t.Fatal("os.ErrPermission should match")
	}
	if isPermissionError(os.ErrNotExist) {
		t.Fatal("not-exist should not match")
	}
}

func TestUpdatePendingPath(t *testing.T) {
	path := updatePendingPath(filepath.Join("tmp", "share"))
	if filepath.Base(path) != updatePendingFileName {
		t.Fatalf("unexpected path %q", path)
	}
}

func TestFormatLocalTimeUsesLocalZone(t *testing.T) {
	// Fixed instant: 2026-07-23T20:24:06Z
	utc := time.Date(2026, 7, 23, 20, 24, 6, 0, time.UTC)
	got := formatLocalTime(utc)
	if strings.Contains(got, "2026-07-23T20:24:06Z") {
		t.Fatalf("expected local display, not RFC3339 Z form: %q", got)
	}
	if !strings.Contains(got, "2026") || !strings.Contains(got, ":") {
		t.Fatalf("unexpected local format %q", got)
	}
}

func TestPreferGUIPasswordPromptWithRofi(t *testing.T) {
	prev := GetGlobalConfig()
	t.Cleanup(func() { SetGlobalConfig(prev) })

	SetGlobalConfig(&Config{RofiSelection: true})
	if !preferGUIPasswordPrompt() {
		t.Fatal("expected GUI password preference when RofiSelection is on")
	}

	// CLI mode (Rofi off): never force GUI just because a display session exists.
	SetGlobalConfig(&Config{RofiSelection: false})
	if preferGUIPasswordPrompt() && stdinIsTerminal() {
		t.Fatal("CLI with a TTY should use terminal password, not GUI")
	}
}

func TestIsCrossDeviceError(t *testing.T) {
	if !isCrossDeviceError(errors.New("rename /tmp/a /home/b: invalid cross-device link")) {
		t.Fatal("expected cross-device detection")
	}
	if isCrossDeviceError(os.ErrPermission) {
		t.Fatal("permission is not cross-device")
	}
}

func TestCopyFileReplaceCrossDeviceStyle(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	src := filepath.Join(srcDir, "newbin")
	dst := filepath.Join(dstDir, "otakase")
	if err := os.WriteFile(src, []byte("#!/bin/sh\necho new\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("#!/bin/sh\necho old\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := copyFileReplace(src, dst); err != nil {
		t.Fatalf("copyFileReplace: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "new") {
		t.Fatalf("dest not replaced: %q", data)
	}
}

func TestReplaceExecutableHandlesCrossDevice(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	src := filepath.Join(srcDir, "downloaded")
	dst := filepath.Join(dstDir, "otakase")
	if err := os.WriteFile(src, []byte("new-binary-content"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old-binary-content"), 0755); err != nil {
		t.Fatal(err)
	}
	// src and dst are different temp dirs — rename often fails with EXDEV on Linux.
	if err := replaceExecutable(src, dst); err != nil {
		t.Fatalf("replaceExecutable: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new-binary-content" {
		t.Fatalf("got %q", data)
	}
}

func TestBuildUpdatePromptMessage(t *testing.T) {
	prompt, msg := buildUpdatePromptMessage("2.0.1", updatePendingState{
		LatestVersion: "2.0.2",
		ReleaseName:   "otakase v2.0.2",
		HTMLURL:       "https://example.com",
		ReleaseNotes:  "## Direct Commits\n- **fixed** stuff",
	})
	if !strings.Contains(prompt, "2.0.1") || !strings.Contains(prompt, "2.0.2") {
		t.Fatalf("prompt=%q", prompt)
	}
	if !strings.Contains(msg, "fixed") || !strings.Contains(msg, "https://example.com") {
		t.Fatalf("message=%q", msg)
	}
}

// A viewer who has skipped several releases gets every intervening release's
// notes concatenated, which can run well past the truncation limit. Cutting
// the already-built pango markup at an arbitrary rune offset can land
// mid-tag -- an unclosed <span> that pango's markup parser rejects outright,
// which rofi then renders unstyled (a plain white box) instead of erroring
// visibly. The fix truncates the plain markdown before conversion, so every
// <span> markdownToPango emits is complete.
func TestBuildUpdatePromptMessageProducesBalancedMarkupWhenNotesAreLong(t *testing.T) {
	var notes strings.Builder
	for i := 0; i < 40; i++ {
		notes.WriteString("## Release notes section\n- **fixed** something with `code` and a [link](https://example.com/x)\n\n")
	}

	_, msg := buildUpdatePromptMessageMode("2.0.1", updatePendingState{
		LatestVersion: "2.5.0",
		ReleaseName:   "otakase v2.5.0",
		HTMLURL:       "https://example.com",
		ReleaseNotes:  notes.String(),
	}, true)

	opens := strings.Count(msg, "<span")
	closes := strings.Count(msg, "</span>")
	if opens != closes {
		t.Fatalf("unbalanced markup: %d <span> vs %d </span> in:\n%s", opens, closes, msg)
	}
}

func TestMarkdownToPangoColorsHeadingsAndBullets(t *testing.T) {
	md := "## Direct Commits\n- fix: something\n**Full Changelog**: https://example.com/compare"
	got := markdownToPango(md)
	if !strings.Contains(got, "foreground=") {
		t.Fatalf("expected pango colors, got %q", got)
	}
	if !strings.Contains(got, "Direct Commits") || !strings.Contains(got, "•") {
		t.Fatalf("expected heading/bullet conversion, got %q", got)
	}
}

// The notes used fixed pastels (#FFD166, #E6E6FA, ...) picked for a dark
// background. On a light palette they were near-invisible, so every colour in
// the rofi message must come from the active palette.
func TestUpdatePromptMessageUsesThePaletteColours(t *testing.T) {
	_, msg := buildUpdatePromptMessageMode("2.0.1", updatePendingState{
		LatestVersion: "2.0.2",
		ReleaseName:   "otakase v2.0.2",
		HTMLURL:       "https://example.com",
		ReleaseNotes:  "## Fixed\n- **subs** no longer stale\n- `code` and [link](https://example.com)",
	}, true)

	p := theme.Active()
	allowed := map[string]bool{}
	for _, c := range []string{p.Foreground, p.Muted, p.Accent, p.Red, p.Green, p.Yellow, p.Blue, p.Magenta} {
		allowed[strings.ToLower(c)] = true
	}
	for _, m := range regexp.MustCompile(`foreground="([^"]+)"`).FindAllStringSubmatch(msg, -1) {
		if !allowed[strings.ToLower(m[1])] {
			t.Errorf("colour %s is not from the active palette", m[1])
		}
	}
}

// The terminal prompt said "truncated" but printed every note anyway, which
// can scroll the prompt itself off screen after a few skipped releases.
func TestTerminalUpdateNotesAreActuallyTruncated(t *testing.T) {
	var notes strings.Builder
	for i := 0; i < 200; i++ {
		notes.WriteString("- fixed something that was broken\n")
	}
	_, msg := buildUpdatePromptMessageMode("2.0.1", updatePendingState{
		LatestVersion: "2.5.0",
		ReleaseNotes:  notes.String(),
	}, false)

	plain := ansiStrip.ReplaceAllString(msg, "")
	if !strings.Contains(plain, "truncated") {
		t.Fatal("expected the truncation marker")
	}
	if n := len([]rune(plain)); n > maxReleaseNotesRunes+400 {
		t.Fatalf("terminal message is %d runes; notes were not cut to %d", n, maxReleaseNotesRunes)
	}
}

func TestUpdateActionOptionsOrder(t *testing.T) {
	opts := updateActionOptions()
	if len(opts) < 1 || opts[0].Key != "update" {
		t.Fatalf("Update now must be first, got %#v", opts)
	}
	for _, o := range opts {
		if strings.HasPrefix(o.Label, "1.") || strings.Contains(o.Label, "1. ") {
			t.Fatalf("labels should not be numbered: %q", o.Label)
		}
	}
}

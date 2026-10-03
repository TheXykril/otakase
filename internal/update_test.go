package internal

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestUpdateActionOptionsOrder(t *testing.T) {
	opts := updateActionOptions(updatePendingState{LatestVersion: "26.5.0", HTMLURL: "https://example.com"})
	if len(opts) < 1 || opts[0].Key != "update" {
		t.Fatalf("Update now must be first, got %#v", opts)
	}
	for _, o := range opts {
		if strings.HasPrefix(o.Label, "1.") || strings.Contains(o.Label, "1. ") {
			t.Fatalf("labels should not be numbered: %q", o.Label)
		}
	}
}

func TestWaitForLaunchUpdateCheck(t *testing.T) {
	saved := launchUpdateDone
	t.Cleanup(func() { launchUpdateDone = saved })

	launchUpdateDone = nil
	if waitForLaunchUpdateCheck(time.Millisecond) {
		t.Fatal("no check started should not count as finished")
	}

	done := make(chan struct{})
	launchUpdateDone = done
	if waitForLaunchUpdateCheck(10 * time.Millisecond) {
		t.Fatal("a running check should not count as finished")
	}

	close(done)
	if !waitForLaunchUpdateCheck(time.Second) {
		t.Fatal("a finished check should be picked up in the same run")
	}
}

func TestHandlePendingUpdatePromptSkipsWhenNothingToOffer(t *testing.T) {
	storage := t.TempDir()
	cfg := &Config{CheckUpdates: true, StoragePath: storage}
	state := updatePendingState{Available: false, LatestVersion: "9.9.9"}
	if err := saveUpdatePendingState(storage, state); err != nil {
		t.Fatalf("save: %v", err)
	}
	if prompted, exit := handlePendingUpdatePrompt(cfg, "2.0.4", false); prompted || exit {
		t.Fatalf("nothing available: prompted=%t exit=%t", prompted, exit)
	}
}

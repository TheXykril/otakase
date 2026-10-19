package internal

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// serveReleaseDownloads serves assets by path the way github.com does and
// points releaseDownloadURL at it.
func serveReleaseDownloads(t *testing.T, assets map[string]string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := assets[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	saved := githubDownloadBase
	githubDownloadBase = server.URL
	t.Cleanup(func() { githubDownloadBase = saved })
}

func TestInstallReleaseBinaryReplacesExecutable(t *testing.T) {
	serveReleaseDownloads(t, map[string]string{
		"/TheXykril/otakase/releases/download/dev/otakase-linux-x86_64": "dev-binary",
	})
	dir := t.TempDir()
	executable := filepath.Join(dir, "otakase")
	writeTestFile(t, executable, "old-binary")

	url := releaseDownloadURL(DefaultUpdateRepo, "dev", "otakase-linux-x86_64")
	if err := installReleaseBinary(url, executable, "otakase-linux-x86_64"); err != nil {
		t.Fatalf("install: %v", err)
	}
	if got := readTestFile(t, executable); got != "dev-binary" {
		t.Fatalf("got %q", got)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(executable); err != nil || info.Mode().Perm()&0100 == 0 {
			t.Fatalf("the new binary should be executable: %v %v", info, err)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("the download should not leave files behind: %v", entries)
	}
}

func TestInstallReleaseBinaryKeepsOldOnFailedDownload(t *testing.T) {
	serveReleaseDownloads(t, map[string]string{})
	dir := t.TempDir()
	executable := filepath.Join(dir, "otakase")
	writeTestFile(t, executable, "old-binary")

	url := releaseDownloadURL(DefaultUpdateRepo, "", "otakase-linux-x86_64")
	err := installReleaseBinary(url, executable, "otakase-linux-x86_64")
	if err == nil || err.Error() != "failed to download file: received status code 404" {
		t.Fatalf("got %v", err)
	}
	if got := readTestFile(t, executable); got != "old-binary" {
		t.Fatalf("the old binary should stay, got %q", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("the failed download should be removed: %v", entries)
	}
}

func TestCopyFileReplace(t *testing.T) {
	src := filepath.Join(t.TempDir(), "newbin")
	dstDir := t.TempDir()
	dst := filepath.Join(dstDir, "otakase")
	writeTestFile(t, src, "#!/bin/sh\necho new\n")
	writeTestFile(t, dst, "#!/bin/sh\necho old\n")

	if err := copyFileReplace(src, dst); err != nil {
		t.Fatalf("copyFileReplace: %v", err)
	}
	if got := readTestFile(t, dst); got != "#!/bin/sh\necho new\n" {
		t.Fatalf("dest not replaced: %q", got)
	}
	if entries, _ := os.ReadDir(dstDir); len(entries) != 1 {
		t.Fatalf("staged copy left behind: %v", entries)
	}
	if err := copyFileReplace(filepath.Join(t.TempDir(), "missing"), dst); err == nil {
		t.Fatal("a missing source should fail")
	}
}

// The source and target are in different temp dirs, which on Linux are often
// different filesystems, so the plain rename may fail with EXDEV.
func TestReplaceExecutableAcrossDirectories(t *testing.T) {
	src := filepath.Join(t.TempDir(), "downloaded")
	dst := filepath.Join(t.TempDir(), "otakase")
	writeTestFile(t, src, "new-binary-content")
	writeTestFile(t, dst, "old-binary-content")

	if err := replaceExecutable(src, dst); err != nil {
		t.Fatalf("replaceExecutable: %v", err)
	}
	if got := readTestFile(t, dst); got != "new-binary-content" {
		t.Fatalf("got %q", got)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("the download should be gone: %v", err)
	}
}

func TestSwapIntoPlaceWithoutExistingTarget(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "staged")
	dst := filepath.Join(dir, "otakase")
	writeTestFile(t, src, "new")
	if err := swapIntoPlace(src, dst); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, dst); got != "new" {
		t.Fatalf("got %q", got)
	}
}

func TestIsPermissionError(t *testing.T) {
	permission := []error{
		os.ErrPermission,
		&os.PathError{Op: "open", Path: "/usr/bin/otakase", Err: syscall.EACCES},
		errors.New("Access is denied."),
		errors.New("rename: operation not permitted"),
	}
	for _, err := range permission {
		if !isPermissionError(err) {
			t.Errorf("%v should be a permission error", err)
		}
	}
	for _, err := range []error{nil, os.ErrNotExist} {
		if isPermissionError(err) {
			t.Errorf("%v is not a permission error", err)
		}
	}
}

func TestIsCrossDeviceError(t *testing.T) {
	if !isCrossDeviceError(errors.New("rename /tmp/a /home/b: invalid cross-device link")) {
		t.Error("expected cross-device detection")
	}
	if !isCrossDeviceError(&os.LinkError{Op: "rename", Old: "/tmp/a", New: "/b", Err: errors.New("EXDEV")}) {
		t.Error("expected the cause of a LinkError to be read")
	}
	// A path that happens to say exdev is not the cause.
	if isCrossDeviceError(&os.LinkError{Op: "rename", Old: "/tmp/exdev", New: "/b", Err: os.ErrPermission}) {
		t.Error("only the cause should count")
	}
	if isCrossDeviceError(nil) || isCrossDeviceError(os.ErrPermission) {
		t.Error("permission is not cross-device")
	}
}

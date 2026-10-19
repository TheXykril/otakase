package internal

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// githubDownloadBase serves release assets; tests point it at a local server.
var githubDownloadBase = "https://github.com"

// SelfUpdate replaces the running binary with the one from the release
// tagged tag, or from the latest release when tag is empty.
func SelfUpdate(repo, tag string) error {
	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("unable to find current executable: %v", err)
	}
	// Resolve symlinks so we replace the real binary (e.g. /usr/local/bin/otakase).
	if resolved, err := filepath.EvalSymlinks(executablePath); err == nil && resolved != "" {
		executablePath = resolved
	}
	binaryName, err := releaseBinaryName()
	if err != nil {
		return err
	}
	return installReleaseBinary(releaseDownloadURL(repo, tag, binaryName), executablePath, binaryName)
}

// releaseDownloadURL is where GitHub serves asset from the release tagged
// tag, or from the latest release when tag is empty.
func releaseDownloadURL(repo, tag, asset string) string {
	repo = orDefaultUpdateRepo(repo)
	if tag = strings.TrimSpace(tag); tag != "" {
		return fmt.Sprintf("%s/%s/releases/download/%s/%s", githubDownloadBase, repo, tag, asset)
	}
	return fmt.Sprintf("%s/%s/releases/latest/download/%s", githubDownloadBase, repo, asset)
}

// installReleaseBinary downloads url and puts it in place of executablePath.
// Nothing is left behind when either step fails.
func installReleaseBinary(url, executablePath, binaryName string) error {
	tmpPath, out, err := createUpdateTempFile(executablePath, binaryName)
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %v", err)
	}
	if err := downloadReleaseBinary(out, url); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	// Asks for sudo when the install directory is not writable.
	if err := replaceExecutable(tmpPath, executablePath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

// downloadReleaseBinary writes url to out, marks it executable and closes it.
func downloadReleaseBinary(out *os.File, url string) error {
	err := fetchInto(out, url)
	if err == nil {
		if chmodErr := out.Chmod(0755); chmodErr != nil {
			err = fmt.Errorf("failed to set file permissions: %v", chmodErr)
		}
	}
	if closeErr := out.Close(); err == nil && closeErr != nil {
		err = fmt.Errorf("failed to close temporary file: %v", closeErr)
	}
	return err
}

func fetchInto(w io.Writer, url string) error {
	resp, err := updateHTTPClient(5 * time.Minute).Get(url)
	if err != nil {
		return fmt.Errorf("failed to download file: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download file: received status code %d", resp.StatusCode)
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("failed to write to temporary file: %v", err)
	}
	return nil
}

// createUpdateTempFile makes the file the download goes into. Next to the
// executable the final move is a plain rename; when that directory is not
// writable (/usr/bin) it goes to the system temp dir instead, and
// replaceExecutable deals with the move across filesystems.
func createUpdateTempFile(executablePath, binaryName string) (string, *os.File, error) {
	if f, err := os.CreateTemp(filepath.Dir(executablePath), ".otakase-download-*"); err == nil {
		return f.Name(), f, nil
	}
	if binaryName == "" {
		binaryName = "bin"
	}
	path := filepath.Join(os.TempDir(), "otakase-update-"+binaryName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return "", nil, err
	}
	return path, f, nil
}

// replaceExecutable moves the downloaded binary at tmpPath over
// executablePath, falling back to a copy when the two are on different
// filesystems and to sudo when the user may not write there.
func replaceExecutable(tmpPath, executablePath string) error {
	renameErr := os.Rename(tmpPath, executablePath)
	switch {
	case renameErr == nil:
		return nil
	case isPermissionError(renameErr):
		return sudoReplaceExecutable(tmpPath, executablePath)
	case !isCrossDeviceError(renameErr):
		// Not the usual /tmp-on-tmpfs case, but a copy may still work.
		Log(fmt.Sprintf("rename to %s failed (%v); trying copy replace", executablePath, renameErr))
	}

	copyErr := copyFileReplace(tmpPath, executablePath)
	switch {
	case copyErr == nil:
		_ = os.Remove(tmpPath)
		return nil
	case isPermissionError(copyErr):
		if err := sudoReplaceExecutable(tmpPath, executablePath); err != nil {
			return fmt.Errorf("failed to replace executable: %w", err)
		}
		return nil
	case isCrossDeviceError(copyErr) || runtime.GOOS != "windows":
		// Odd mount layouts can fail in other ways; root may still manage.
		Log(fmt.Sprintf("copy replace failed (%v); trying sudo", copyErr))
		if sudoReplaceExecutable(tmpPath, executablePath) == nil {
			return nil
		}
	}
	return fmt.Errorf("failed to replace executable: %w", copyErr)
}

func sudoReplaceExecutable(tmpPath, executablePath string) error {
	updateUserMessage(GetGlobalConfig(), "Update needs elevated permissions to replace the installed binary.")
	if err := installExecutableWithSudo(tmpPath, executablePath); err != nil {
		return err
	}
	_ = os.Remove(tmpPath)
	return nil
}

// copyFileReplace copies src over dst. The copy is staged next to dst, so
// the last step is still a rename within one filesystem.
func copyFileReplace(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	staged, err := os.CreateTemp(filepath.Dir(dst), ".otakase-update-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(staged.Name())
		}
	}()
	if err := writeExecutable(staged, in); err != nil {
		return err
	}
	return swapIntoPlace(staged.Name(), dst)
}

// writeExecutable fills f from r, synced and marked executable, and closes it.
func writeExecutable(f *os.File, r io.Reader) error {
	err := f.Chmod(0755)
	if err == nil {
		_, err = io.Copy(f, r)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

// swapIntoPlace renames path over dst. Windows will not replace a running
// executable but does let it be renamed, so when the plain rename fails dst
// is moved to dst.old first, and put back if path still cannot take its place.
func swapIntoPlace(path, dst string) error {
	if os.Rename(path, dst) == nil {
		return nil
	}
	old := dst + ".old"
	_ = os.Remove(old)
	if err := os.Rename(dst, old); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(path, dst); err != nil {
		_ = os.Rename(old, dst)
		return err
	}
	// Fails on Windows while the old binary still runs; it is only a leftover.
	_ = os.Remove(old)
	return nil
}

// installExecutableWithSudo installs src at dest as root, asking for the
// password first.
func installExecutableWithSudo(src, dest string) error {
	password, err := promptSudoPasswordTitled(DisplayName+" Update", "Administrator password (sudo) to install update: ")
	if err != nil {
		return fmt.Errorf("read sudo password: %w", err)
	}
	if strings.TrimSpace(password) == "" {
		return errors.New("empty sudo password")
	}
	sudo := func(args ...string) error {
		// -S reads the password from stdin; -p "" stops sudo printing its own prompt.
		cmd := exec.Command("sudo", append([]string{"-S", "-p", ""}, args...)...)
		cmd.Stdin = strings.NewReader(password + "\n")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		return cmd.Run()
	}

	// install(1) sets the mode in the same step; cp and chmod are for
	// systems without it.
	installErr := sudo("install", "-m", "755", src, dest)
	if installErr == nil {
		return nil
	}
	Log("sudo install failed: " + installErr.Error())
	if cpErr := sudo("cp", src, dest); cpErr != nil {
		return fmt.Errorf("sudo install failed: %w", cpErr)
	}
	_ = sudo("chmod", "755", dest)
	return nil
}

func isPermissionError(err error) bool {
	return errors.Is(err, os.ErrPermission) ||
		errorMentions(err, "permission denied", "access is denied", "operation not permitted")
}

// isCrossDeviceError reports a rename that failed because src and dest are on
// different filesystems (e.g. /tmp on tmpfs, ~/.local/bin on disk).
func isCrossDeviceError(err error) bool {
	// Only the cause is checked: the paths in a LinkError could match too.
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		err = linkErr.Err
	}
	return errorMentions(err, "cross-device", "cross device", "exdev")
}

// errorMentions reports whether err's text contains any of words, ignoring case.
func errorMentions(err error, words ...string) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, word := range words {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

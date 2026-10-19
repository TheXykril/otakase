package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"
)

// githubReleaseAPI is the part of a GitHub release the updater reads. The
// assets are not needed: each build is downloaded by its fixed name.
type githubReleaseAPI struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
}

// githubAPIBase is where release lookups go; tests point it at a local server.
var githubAPIBase = "https://api.github.com"

// orDefaultUpdateRepo lets callers pass "" for the fork otakase updates from.
func orDefaultUpdateRepo(repo string) string {
	if strings.TrimSpace(repo) == "" {
		return DefaultUpdateRepo
	}
	return repo
}

// updateHTTPClient is the app's shared client, or one with timeout when that
// has not been set up.
func updateHTTPClient(timeout time.Duration) *http.Client {
	if sharedHTTPClient != nil {
		return sharedHTTPClient
	}
	return &http.Client{Timeout: timeout}
}

// fetchGitHubRelease reads one release: which is "latest" or "tags/<tag>".
func fetchGitHubRelease(repo, which string) (githubReleaseAPI, error) {
	var release githubReleaseAPI
	url := fmt.Sprintf("%s/repos/%s/releases/%s", githubAPIBase, orDefaultUpdateRepo(repo), which)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return release, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", AppName+"-update-check")

	resp, err := updateHTTPClient(15 * time.Second).Do(req)
	if err != nil {
		return release, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// GitHub explains rate limits and missing releases in the body.
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return release, fmt.Errorf("github releases API status %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return githubReleaseAPI{}, err
	}
	if strings.TrimSpace(release.TagName) == "" {
		return githubReleaseAPI{}, fmt.Errorf("release %s has no tag", which)
	}
	return release, nil
}

// normalizeReleaseVersion drops the v a release tag starts with.
func normalizeReleaseVersion(tag string) string {
	return strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(tag), "v"), "V")
}

// isUpdateNewer reports whether candidate is a newer release than running.
// With either one unknown there is nothing to compare, so no update.
func isUpdateNewer(candidate, running string) bool {
	candidate, running = normalizeReleaseVersion(candidate), normalizeReleaseVersion(running)
	if candidate == "" || running == "" {
		return false
	}
	return compareUpdateVersions(running, candidate) < 0
}

// truncateReleaseNotes shortens notes for the state file.
func truncateReleaseNotes(notes string) string {
	if notes = strings.TrimSpace(notes); notes == "" {
		return "(No release notes provided.)"
	}
	if runes := []rune(notes); len(runes) > releaseNotesLimit {
		return string(runes[:releaseNotesLimit]) + "\n… (truncated)"
	}
	return notes
}

func releaseBinaryName() (string, error) {
	return releaseAssetName(runtime.GOOS, runtime.GOARCH)
}

// releaseArchNames maps GOARCH to the suffix the release builds use.
var releaseArchNames = map[string]string{"amd64": "x86_64", "arm64": "arm64"}

// releaseAssetName is the release build for goos/goarch. Windows only has
// arm64 and x86_64 builds, so any other architecture is given x86_64; a Mac
// that is neither gets the universal build.
func releaseAssetName(goos, goarch string) (string, error) {
	arch, known := releaseArchNames[goarch]
	switch goos {
	case "windows":
		if goarch == "arm64" {
			return "otakase-windows-arm64.exe", nil
		}
		return "otakase-windows-x86_64.exe", nil
	case "darwin":
		if !known {
			return "otakase-macos-universal", nil
		}
		return "otakase-macos-" + arch, nil
	case "linux":
		if !known {
			return "", fmt.Errorf("unsupported Linux architecture: %s", goarch)
		}
		return "otakase-linux-" + arch, nil
	}
	return "", fmt.Errorf("unsupported operating system: %s", goos)
}

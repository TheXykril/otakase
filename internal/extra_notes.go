package internal

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// extraNotesAssetName is an optional file attached to a release (see the
	// Extra Notes workflow) holding more detailed notes than the release body.
	extraNotesAssetName = "notes-extra.md"
	maxExtraNotesBytes  = 16 << 10
)

// withExtraNotes puts the release's extra notes asset, when it has one, ahead
// of the release body. It is only fetched when enabled is true; a missing or
// unreadable file leaves the release untouched.
func withExtraNotes(release githubReleaseAPI, enabled bool) githubReleaseAPI {
	if !enabled {
		return release
	}
	extra, err := fetchExtraNotes(release)
	if err != nil {
		Log(fmt.Sprintf("No extra release notes: %v", err))
		return release
	}
	if extra == "" {
		return release
	}
	release.Body = strings.TrimSpace(extra + "\n\n" + release.Body)
	return release
}

func fetchExtraNotes(release githubReleaseAPI) (string, error) {
	url := ""
	for _, asset := range release.Assets {
		if asset.Name == extraNotesAssetName {
			url = asset.BrowserDownloadURL
			break
		}
	}
	if url == "" {
		return "", nil
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", AppName+"-update-check")
	client := sharedHTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxExtraNotesBytes))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

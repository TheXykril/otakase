package internal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func releaseWithExtraNotes(url string) githubReleaseAPI {
	release := githubReleaseAPI{TagName: "v26.1.1", Body: "## Changelog\n- Stability improvements"}
	release.Assets = append(release.Assets, struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	}{Name: extraNotesAssetName, BrowserDownloadURL: url})
	return release
}

func TestWithExtraNotesPrependsWhenEnabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("### Details\n- Detailed line\n"))
	}))
	defer server.Close()

	got := withExtraNotes(releaseWithExtraNotes(server.URL), true)
	if !strings.HasPrefix(got.Body, "### Details\n- Detailed line") {
		t.Fatalf("extra notes not first: %q", got.Body)
	}
	if !strings.Contains(got.Body, "- Stability improvements") {
		t.Fatalf("release body lost: %q", got.Body)
	}
}

func TestWithExtraNotesNeverFetchedWhenDisabled(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("- Detailed line"))
	}))
	defer server.Close()

	release := releaseWithExtraNotes(server.URL)
	got := withExtraNotes(release, false)
	if got.Body != release.Body {
		t.Fatalf("body changed while disabled: %q", got.Body)
	}
	if hits.Load() != 0 {
		t.Fatalf("extra notes fetched while disabled")
	}
}

func TestWithExtraNotesMissingOrFailingLeavesBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	release := releaseWithExtraNotes(server.URL)
	if got := withExtraNotes(release, true); got.Body != release.Body {
		t.Fatalf("failed fetch changed body: %q", got.Body)
	}
	plain := githubReleaseAPI{TagName: "v26.1.1", Body: "notes"}
	if got := withExtraNotes(plain, true); got.Body != "notes" {
		t.Fatalf("release without asset changed: %q", got.Body)
	}
}

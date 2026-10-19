package internal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func pointReleaseAPIAt(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	saved := githubAPIBase
	githubAPIBase = server.URL
	t.Cleanup(func() { githubAPIBase = saved })
}

func TestFetchGitHubReleaseReadsRelease(t *testing.T) {
	pointReleaseAPIAt(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/TheXykril/otakase/releases/tags/v26.1.0" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("User-Agent") != AppName+"-update-check" {
			t.Errorf("unexpected headers: %v", r.Header)
		}
		_, _ = w.Write([]byte(`{"tag_name":"v26.1.0","name":"Otakase v26.1.0","body":"notes","html_url":"https://x","assets":[{"name":"a"}]}`))
	})

	// An empty repo means the default one.
	release, err := fetchGitHubRelease("", "tags/v26.1.0")
	if err != nil {
		t.Fatal(err)
	}
	want := githubReleaseAPI{TagName: "v26.1.0", Name: "Otakase v26.1.0", Body: "notes", HTMLURL: "https://x"}
	if release != want {
		t.Fatalf("got %#v, want %#v", release, want)
	}
}

func TestFetchGitHubReleaseErrors(t *testing.T) {
	pointReleaseAPIAt(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/TheXykril/otakase/releases/latest":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(" API rate limit exceeded \n"))
		case "/repos/TheXykril/otakase/releases/tags/untagged":
			_, _ = w.Write([]byte(`{"tag_name":" ","name":"draft"}`))
		default:
			_, _ = w.Write([]byte(`not json`))
		}
	})

	_, err := fetchGitHubRelease(DefaultUpdateRepo, "latest")
	if err == nil || err.Error() != "github releases API status 403: API rate limit exceeded" {
		t.Errorf("rate limit: got %v", err)
	}
	_, err = fetchGitHubRelease(DefaultUpdateRepo, "tags/untagged")
	if err == nil || !strings.Contains(err.Error(), "has no tag") {
		t.Errorf("untagged: got %v", err)
	}
	if _, err = fetchGitHubRelease(DefaultUpdateRepo, "tags/broken"); err == nil {
		t.Error("broken JSON: expected an error")
	}
}

func TestReleaseAssetName(t *testing.T) {
	cases := []struct{ goos, goarch, want string }{
		{"windows", "amd64", "otakase-windows-x86_64.exe"},
		{"windows", "arm64", "otakase-windows-arm64.exe"},
		{"windows", "386", "otakase-windows-x86_64.exe"},
		{"darwin", "amd64", "otakase-macos-x86_64"},
		{"darwin", "arm64", "otakase-macos-arm64"},
		{"darwin", "ppc64", "otakase-macos-universal"},
		{"linux", "amd64", "otakase-linux-x86_64"},
		{"linux", "arm64", "otakase-linux-arm64"},
	}
	for _, c := range cases {
		got, err := releaseAssetName(c.goos, c.goarch)
		if err != nil || got != c.want {
			t.Errorf("%s/%s: got %q %v, want %q", c.goos, c.goarch, got, err, c.want)
		}
	}
	if _, err := releaseAssetName("linux", "riscv64"); err == nil {
		t.Error("linux/riscv64 has no build")
	}
	if _, err := releaseAssetName("freebsd", "amd64"); err == nil {
		t.Error("freebsd has no build")
	}
}

func TestReleaseDownloadURL(t *testing.T) {
	if got := releaseDownloadURL("", "", "otakase-linux-x86_64"); got != "https://github.com/TheXykril/otakase/releases/latest/download/otakase-linux-x86_64" {
		t.Errorf("latest: got %q", got)
	}
	if got := releaseDownloadURL("someone/fork", " dev ", "otakase-macos-arm64"); got != "https://github.com/someone/fork/releases/download/dev/otakase-macos-arm64" {
		t.Errorf("tagged: got %q", got)
	}
}

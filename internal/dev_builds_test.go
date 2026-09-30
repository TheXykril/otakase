package internal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompareUpdateVersionsOrdersDevBuilds(t *testing.T) {
	newer := [][2]string{
		{"26.1.0", "26.1.0-dev.12"},
		{"26.1.0-dev.12", "26.1.0-dev.3"},
		{"26.1.0-dev.1", "26.1.0-dev.cff83f3"},
		{"26.1.0-dev.1", "2.2.2"},
		{"26.2.0-dev.1", "26.1.0"},
		{"v26.1.1", "26.1.0"},
	}
	for _, pair := range newer {
		if !isUpdateNewer(pair[0], pair[1]) {
			t.Errorf("expected %s newer than %s", pair[0], pair[1])
		}
		if isUpdateNewer(pair[1], pair[0]) {
			t.Errorf("expected %s not newer than %s", pair[1], pair[0])
		}
	}
	if isUpdateNewer("26.1.0-dev.4", "26.1.0-dev.4") {
		t.Error("same dev build is not newer")
	}
	// An unflagged local build stays out of the update prompt.
	if isUpdateNewer("26.1.0-dev.4", "dev") {
		t.Error("a dev build must not be offered over an unflagged build")
	}
}

// A dev build of 26.1.0 must still get 26.1.0's new settings and menu rows.
func TestMigrationsTreatDevBuildAsItsVersion(t *testing.T) {
	if compareVersions("26.1.0-dev.4", "26.1.0") != 0 {
		t.Fatal("migrations should count 26.1.0-dev.4 as 26.1.0")
	}
}

func TestReleaseVersionReadsDevVersionFromName(t *testing.T) {
	cases := []struct {
		release githubReleaseAPI
		want    string
	}{
		{githubReleaseAPI{TagName: "v26.1.0"}, "26.1.0"},
		{githubReleaseAPI{TagName: "dev", Name: "Otakase 26.1.0-dev.7"}, "26.1.0-dev.7"},
		{githubReleaseAPI{TagName: "dev", Name: "Dev build", Body: "version `26.1.0-dev.8`."}, "26.1.0-dev.8"},
		{githubReleaseAPI{TagName: "dev", Name: "Dev build"}, ""},
	}
	for _, c := range cases {
		if got := releaseVersion(c.release); got != c.want {
			t.Errorf("releaseVersion(%+v) = %q, want %q", c.release, got, c.want)
		}
	}
}

func serveReleases(t *testing.T, releases map[string]map[string]interface{}) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		release, ok := releases[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(release)
	}))
	t.Cleanup(server.Close)
	previous := githubAPIBase
	githubAPIBase = server.URL
	t.Cleanup(func() { githubAPIBase = previous })
}

func TestFetchUpdateReleasePicksNewerChannel(t *testing.T) {
	const latestPath = "/repos/TheXykril/otakase/releases/latest"
	const devPath = "/repos/TheXykril/otakase/releases/tags/dev"
	stable := map[string]interface{}{"tag_name": "v2.2.2", "name": "Otakase v2.2.2"}
	dev := map[string]interface{}{"tag_name": "dev", "name": "Otakase 26.1.0-dev.3"}

	serveReleases(t, map[string]map[string]interface{}{latestPath: stable, devPath: dev})

	release, version, err := fetchUpdateRelease(DefaultUpdateRepo, false)
	if err != nil || release.TagName != "v2.2.2" || version != "2.2.2" {
		t.Fatalf("DevBuilds off: got %q %q %v, want the latest release", release.TagName, version, err)
	}
	release, version, err = fetchUpdateRelease(DefaultUpdateRepo, true)
	if err != nil || release.TagName != "dev" || version != "26.1.0-dev.3" {
		t.Fatalf("DevBuilds on: got %q %q %v, want the dev build", release.TagName, version, err)
	}

	// Once the release it led up to is out, the release wins.
	serveReleases(t, map[string]map[string]interface{}{
		latestPath: {"tag_name": "v26.1.0", "name": "Otakase v26.1.0"},
		devPath:    dev,
	})
	release, version, err = fetchUpdateRelease(DefaultUpdateRepo, true)
	if err != nil || release.TagName != "v26.1.0" || version != "26.1.0" {
		t.Fatalf("got %q %q %v, want v26.1.0 over its dev build", release.TagName, version, err)
	}

	// No dev release published: fall back to the latest release.
	serveReleases(t, map[string]map[string]interface{}{latestPath: stable})
	release, _, err = fetchUpdateRelease(DefaultUpdateRepo, true)
	if err != nil || release.TagName != "v2.2.2" {
		t.Fatalf("got %q %v, want the latest release when there is no dev build", release.TagName, err)
	}
}

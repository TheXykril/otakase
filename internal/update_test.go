package internal

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testLatestPath = "/repos/TheXykril/otakase/releases/latest"
	testDevPath    = "/repos/TheXykril/otakase/releases/tags/dev"
	// testNewerTag is a release newer than anything that will ship soon.
	testNewerTag = "v9.9.9"
)

// noChangelogFile points CHANGELOG.md lookups at a server without one, so
// the changelog falls back to the release notes without leaving the machine.
func noChangelogFile(t *testing.T) {
	t.Helper()
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	saved := githubRawBase
	githubRawBase = server.URL
	t.Cleanup(func() { githubRawBase = saved })
}

// failOnReleaseLookup makes any release lookup fail the test.
func failOnReleaseLookup(t *testing.T) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected release lookup: %s", r.URL.Path)
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	saved := githubAPIBase
	githubAPIBase = server.URL
	t.Cleanup(func() { githubAPIBase = saved })
}

func TestIsUpdateNewerSemver(t *testing.T) {
	cases := []struct {
		candidate, running string
		want               bool
	}{
		{"2.0.4", "2.0.3", true},
		{"v2.1.0", "2.0.9", true},
		{"2.0.3", "2.0.3", false},
		{"2.0.2", "2.0.3", false},
		{"", "2.0.3", false},
		{"2.0.4", "", false},
	}
	for _, c := range cases {
		if got := isUpdateNewer(c.candidate, c.running); got != c.want {
			t.Errorf("isUpdateNewer(%q, %q) = %t, want %t", c.candidate, c.running, got, c.want)
		}
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

func TestPendingUpdateShouldPrompt(t *testing.T) {
	on := &Config{CheckUpdates: true}
	offered := pendingUpdate{Available: true, LatestVersion: "2.1.0"}
	later := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	earlier := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)

	cases := []struct {
		name   string
		config *Config
		edit   func(*pendingUpdate)
		want   bool
	}{
		{"newer release", on, func(*pendingUpdate) {}, true},
		{"no config", nil, func(*pendingUpdate) {}, false},
		{"checks off", &Config{}, func(*pendingUpdate) {}, false},
		{"not available", on, func(s *pendingUpdate) { s.Available = false }, false},
		{"already running it", on, func(s *pendingUpdate) { s.LatestVersion = "2.0.4" }, false},
		{"skipped", on, func(s *pendingUpdate) { s.SkippedVersion = "v2.1.0" }, false},
		{"reminder pending", on, func(s *pendingUpdate) { s.RemindAfter = later }, false},
		{"reminder over", on, func(s *pendingUpdate) { s.RemindAfter = earlier }, true},
		{"unreadable reminder", on, func(s *pendingUpdate) { s.RemindAfter = "soon" }, true},
	}
	for _, c := range cases {
		state := offered
		c.edit(&state)
		if got := pendingUpdateShouldPrompt(c.config, "2.0.4", state); got != c.want {
			t.Errorf("%s: got %t, want %t", c.name, got, c.want)
		}
	}
}

func TestUpdatePendingStateRoundTrip(t *testing.T) {
	storage := t.TempDir()
	if path := pendingUpdatePath(storage); path != filepath.Join(storage, updatePendingFileName) {
		t.Fatalf("unexpected path %q", path)
	}
	if got := readPendingUpdate(storage); got.Available || got.LatestVersion != "" {
		t.Fatalf("no file should load as nothing known, got %#v", got)
	}

	want := pendingUpdate{Available: true, LatestVersion: "9.9.9", LatestTag: "v9.9.9", ReleaseNotes: "## Changes\n- test"}
	if err := writePendingUpdate(storage, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := readPendingUpdate(storage)
	if !got.Available || got.LatestTag != want.LatestTag || got.ReleaseNotes != want.ReleaseNotes {
		t.Fatalf("got %#v, want %#v", got, want)
	}

	if err := os.WriteFile(pendingUpdatePath(storage), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := readPendingUpdate(storage); got.Available {
		t.Fatalf("a damaged file should load as nothing known, got %#v", got)
	}
}

func TestCheckForUpdateInBackgroundStoresNewerRelease(t *testing.T) {
	noChangelogFile(t)
	serveReleases(t, map[string]map[string]interface{}{
		testLatestPath: {
			"tag_name": testNewerTag,
			"body":     "## What's Changed\n* feat: something new by @x in https://x\n",
			"html_url": "https://github.com/TheXykril/otakase/releases/tag/v9.9.9",
		},
	})
	cfg := &Config{CheckUpdates: true, StoragePath: t.TempDir()}
	earlier := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if err := writePendingUpdate(cfg.StoragePath, pendingUpdate{RemindAfter: earlier}); err != nil {
		t.Fatal(err)
	}

	if err := checkForUpdateInBackground(cfg, "2.0.4"); err != nil {
		t.Fatalf("check: %v", err)
	}
	state := readPendingUpdate(cfg.StoragePath)
	if !state.Available || state.LatestVersion != "9.9.9" || state.LatestTag != "v9.9.9" {
		t.Fatalf("expected v9.9.9 to be offered, got %#v", state)
	}
	if state.ReleaseName != DisplayName+" 9.9.9" {
		t.Errorf("a release without a name should be named after the app, got %q", state.ReleaseName)
	}
	if len(state.Changelog) == 0 || state.CheckedAt == "" {
		t.Errorf("expected a changelog and a check time, got %#v", state)
	}
	if state.RemindAfter != "" {
		t.Errorf("a reminder that ran out should be cleared, got %q", state.RemindAfter)
	}
	if !pendingUpdateShouldPrompt(cfg, "2.0.4", state) {
		t.Fatal("the stored update should be offered")
	}

	// Running it already: stored, but nothing to offer.
	if err := checkForUpdateInBackground(cfg, "9.9.9"); err != nil {
		t.Fatalf("check: %v", err)
	}
	if state := readPendingUpdate(cfg.StoragePath); state.Available {
		t.Fatalf("the running version should not be offered, got %#v", state)
	}
}

func TestCheckForUpdateInBackgroundRespectsSkip(t *testing.T) {
	noChangelogFile(t)
	serveReleases(t, map[string]map[string]interface{}{testLatestPath: {"tag_name": testNewerTag}})
	cfg := &Config{CheckUpdates: true, StoragePath: t.TempDir()}
	if err := writePendingUpdate(cfg.StoragePath, pendingUpdate{SkippedVersion: "v9.9.9"}); err != nil {
		t.Fatal(err)
	}
	if err := checkForUpdateInBackground(cfg, "2.0.4"); err != nil {
		t.Fatalf("check: %v", err)
	}
	if state := readPendingUpdate(cfg.StoragePath); state.Available || state.LatestVersion != "9.9.9" {
		t.Fatalf("a skipped version should be recorded but not offered, got %#v", state)
	}
}

func TestCheckForUpdateInBackgroundWaitsOutReminder(t *testing.T) {
	failOnReleaseLookup(t)
	cfg := &Config{CheckUpdates: true, StoragePath: t.TempDir()}
	later := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if err := writePendingUpdate(cfg.StoragePath, pendingUpdate{RemindAfter: later}); err != nil {
		t.Fatal(err)
	}
	if err := checkForUpdateInBackground(cfg, "2.0.4"); err != nil {
		t.Fatalf("check: %v", err)
	}
}

func TestCheckForUpdateInBackgroundOffersDevBuild(t *testing.T) {
	noChangelogFile(t)
	serveReleases(t, map[string]map[string]interface{}{
		testLatestPath: {"tag_name": "v2.2.2", "name": "Otakase v2.2.2"},
		testDevPath:    {"tag_name": "dev", "name": "Otakase 26.1.0-dev.3", "body": "Dev build"},
	})

	stable := &Config{CheckUpdates: true, StoragePath: t.TempDir()}
	if err := checkForUpdateInBackground(stable, "2.2.2"); err != nil {
		t.Fatalf("check: %v", err)
	}
	if state := readPendingUpdate(stable.StoragePath); state.Available {
		t.Fatalf("DevBuilds off: nothing newer than 2.2.2 to offer, got %#v", state)
	}

	dev := &Config{CheckUpdates: true, DevBuilds: true, StoragePath: t.TempDir()}
	if err := checkForUpdateInBackground(dev, "2.2.2"); err != nil {
		t.Fatalf("check: %v", err)
	}
	state := readPendingUpdate(dev.StoragePath)
	if !state.Available || state.LatestTag != "dev" || state.LatestVersion != "26.1.0-dev.3" {
		t.Fatalf("DevBuilds on: expected the dev build, got %#v", state)
	}
}

func TestRefreshUpdateStateKeepsStateWhenOffline(t *testing.T) {
	serveReleases(t, map[string]map[string]interface{}{})
	state := pendingUpdate{Available: true, LatestVersion: "9.9.9", ReleaseNotes: "stored"}
	refreshUpdateStateFromGitHub(&state, false, "2.0.4")
	if state.LatestVersion != "9.9.9" || state.ReleaseNotes != "stored" {
		t.Fatalf("a failed refresh should leave the state alone, got %#v", state)
	}
}

func TestRefreshUpdateStateReadsFullNotes(t *testing.T) {
	noChangelogFile(t)
	long := strings.Repeat("a", releaseNotesLimit+10)
	serveReleases(t, map[string]map[string]interface{}{testLatestPath: {"tag_name": "v9.9.10", "body": long}})
	state := pendingUpdate{Available: false, LatestVersion: "9.9.9", ReleaseNotes: "short"}
	refreshUpdateStateFromGitHub(&state, false, "2.0.4")
	if !state.Available || state.LatestVersion != "9.9.10" || state.ReleaseNotes != long {
		t.Fatalf("expected the newest release with its full notes, got %#v", state)
	}
}

func TestTruncateReleaseNotes(t *testing.T) {
	if got := truncateReleaseNotes("  hello \n"); got != "hello" {
		t.Fatalf("got %q", got)
	}
	if got := truncateReleaseNotes(" "); got != "(No release notes provided.)" {
		t.Fatalf("got %q", got)
	}
	got := truncateReleaseNotes(strings.Repeat("é", releaseNotesLimit+50))
	if !strings.HasSuffix(got, "\n… (truncated)") || strings.Count(got, "é") != releaseNotesLimit {
		t.Fatalf("expected %d runes and a marker, got %d runes", releaseNotesLimit, len([]rune(got)))
	}
}

func TestFormatLocalTimeUsesLocalZone(t *testing.T) {
	saved := time.Local
	time.Local = time.FixedZone("TST", 2*60*60)
	t.Cleanup(func() { time.Local = saved })

	got := localClock(time.Date(2026, 7, 23, 20, 24, 6, 0, time.UTC))
	if got != "Thu Jul 23 2026, 10:24 PM TST" {
		t.Fatalf("got %q", got)
	}
}

func TestUpdateActionOptionsOrder(t *testing.T) {
	options := updateActionOptions(pendingUpdate{LatestVersion: "26.5.0", HTMLURL: "https://example.com"})
	if len(options) == 0 || options[0].Key != "update" {
		t.Fatalf("Update now must be first, got %#v", options)
	}
	for _, option := range options {
		if strings.HasPrefix(option.Label, "1.") {
			t.Fatalf("labels should not be numbered: %q", option.Label)
		}
	}
}

func TestApplyUpdateChoiceRemembersTheAnswer(t *testing.T) {
	cfg := &Config{CheckUpdates: true, StoragePath: t.TempDir()}

	state := pendingUpdate{Available: true, LatestVersion: "9.9.9"}
	if applyUpdateChoice(cfg, &state, SelectionOption{Key: "remind"}) {
		t.Fatal("remind later should not exit")
	}
	stored := readPendingUpdate(cfg.StoragePath)
	until, err := time.Parse(time.RFC3339, stored.RemindAfter)
	if err != nil || !stored.Available || until.Before(time.Now().Add(23*time.Hour)) {
		t.Fatalf("expected a reminder a day out, got %#v", stored)
	}

	if applyUpdateChoice(cfg, &state, SelectionOption{Key: "skip-version"}) {
		t.Fatal("skip should not exit")
	}
	stored = readPendingUpdate(cfg.StoragePath)
	if stored.Available || stored.SkippedVersion != "9.9.9" || stored.RemindAfter != "" {
		t.Fatalf("expected 9.9.9 skipped, got %#v", stored)
	}

	// Continue changes nothing.
	state = pendingUpdate{Available: true, LatestVersion: "9.9.10"}
	if applyUpdateChoice(cfg, &state, SelectionOption{Key: "continue"}) {
		t.Fatal("continue should not exit")
	}
	if stored := readPendingUpdate(cfg.StoragePath); stored.LatestVersion != "9.9.9" {
		t.Fatalf("continue should not write the state, got %#v", stored)
	}
}

func TestApplyUpdateChoiceDisablesChecks(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "otakase.conf")
	if err := os.WriteFile(configPath, []byte("RofiSelection=false\n"), 0644); err != nil {
		t.Fatal(err)
	}
	saved := ConfigFilePath
	ConfigFilePath = configPath
	t.Cleanup(func() { ConfigFilePath = saved })

	cfg := &Config{CheckUpdates: true, StoragePath: t.TempDir()}
	state := pendingUpdate{Available: true, LatestVersion: "9.9.9"}
	applyUpdateChoice(cfg, &state, SelectionOption{Key: "stop-checks"})
	if cfg.CheckUpdates {
		t.Fatal("CheckUpdates should be off for this run too")
	}
	values, err := LoadConfigFromFile(configPath)
	if err != nil || values["CheckUpdates"] != "false" || values["RofiSelection"] != "false" {
		t.Fatalf("config file: %v %v", values, err)
	}
	if readPendingUpdate(cfg.StoragePath).Available {
		t.Fatal("the update should no longer be offered")
	}
}

func TestSetConfigBoolOption(t *testing.T) {
	if err := setConfigBoolOption(" ", "CheckUpdates", false); err == nil {
		t.Fatal("expected an error without a config path")
	}
	path := filepath.Join(t.TempDir(), "otakase.conf")
	if err := os.WriteFile(path, []byte("CheckUpdates=false\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := setConfigBoolOption(path, "CheckUpdates", true); err != nil {
		t.Fatal(err)
	}
	if err := setConfigBoolOption(path, "DevBuilds", true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "CheckUpdates=true\nDevBuilds=true\n" {
		t.Fatalf("config file after two writes: %q", data)
	}
}

func TestHandlePendingUpdatePromptAppliesChoice(t *testing.T) {
	saved := promptUpdateTerminal
	t.Cleanup(func() { promptUpdateTerminal = saved })
	promptUpdateTerminal = func(string, pendingUpdate) (SelectionOption, error) {
		return SelectionOption{Key: "skip-version"}, nil
	}

	cfg := &Config{CheckUpdates: true, StoragePath: t.TempDir()}
	if err := writePendingUpdate(cfg.StoragePath, pendingUpdate{Available: true, LatestVersion: "9.9.9"}); err != nil {
		t.Fatal(err)
	}
	prompted, exit := handlePendingUpdatePrompt(cfg, "2.0.4", false)
	if !prompted || exit {
		t.Fatalf("prompted=%t exit=%t", prompted, exit)
	}
	if state := readPendingUpdate(cfg.StoragePath); state.SkippedVersion != "9.9.9" {
		t.Fatalf("expected the choice to be stored, got %#v", state)
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
	state := pendingUpdate{Available: false, LatestVersion: "9.9.9"}
	if err := writePendingUpdate(storage, state); err != nil {
		t.Fatalf("save: %v", err)
	}
	if prompted, exit := handlePendingUpdatePrompt(cfg, "2.0.4", false); prompted || exit {
		t.Fatalf("nothing available: prompted=%t exit=%t", prompted, exit)
	}
}

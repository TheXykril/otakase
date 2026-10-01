package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/thexykril/otakase/internal/loadproviders"
)

func TestProviderSelectionOptionsUsesDefaultAndSingleProviders(t *testing.T) {
	withAllProvidersEnabledForTest(t)
	options := providerSelectionOptions()
	if len(options) == 0 {
		t.Fatal("expected provider options")
	}
	if options[0].Key != "stacked" {
		t.Fatalf("first option key = %q, want stacked", options[0].Key)
	}
	if !strings.Contains(options[0].Label, "Default with fallback") {
		t.Fatalf("first option label = %q", options[0].Label)
	}
	for _, option := range options {
		if strings.Contains(option.Label, ", then ") {
			t.Fatalf("unexpected combo label %q", option.Label)
		}
	}
}

func TestMigrateProviderConfig(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string
		changed bool
	}{
		{name: "legacy senshi default", raw: `["senshi"]`, want: "stacked", changed: true},
		{name: "legacy allanime default", raw: `["allanime"]`, want: "stacked", changed: true},
		{name: "stack alias", raw: "stack", want: "stacked", changed: true},
		{name: "already stacked", raw: "stacked", want: "stacked", changed: false},
		{name: "single anineko", raw: `["anineko"]`, want: `["anineko"]`, changed: false},
		{name: "legacy pair", raw: `["senshi","anineko"]`, want: "stacked", changed: true},
	}

	for _, tc := range cases {
		got, changed := migrateProviderConfig(tc.raw)
		if changed != tc.changed || got != tc.want {
			t.Fatalf("%s: got (%q, %v), want (%q, %v)", tc.name, got, changed, tc.want, tc.changed)
		}
	}
}

func TestCanonicalProviderConfigValuePrefersStackedToken(t *testing.T) {
	withAllProvidersEnabledForTest(t)
	if got := canonicalProviderConfigValue("stacked"); got != "stacked" {
		t.Fatalf("got %q, want stacked", got)
	}
	if got := canonicalProviderConfigValue(""); got != "stacked" {
		t.Fatalf("empty got %q, want stacked", got)
	}
}

func TestMigrateOnVersionUpgradeWritesVersionAndUpdatesProvider(t *testing.T) {
	withAllProvidersEnabledForTest(t)
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "otakase.conf")
	storagePath := filepath.Join(tempDir, "share")
	if err := os.WriteFile(configPath, []byte("Provider=[\"anineko\",\"nyaa\"]\nStoragePath="+storagePath+"\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if config.Provider != `["anineko","nyaa"]` {
		t.Fatalf("pre-migration provider = %q", config.Provider)
	}

	updated, err := MigrateOnVersionUpgrade(configPath, &config, "2.1.0")
	if err != nil {
		t.Fatalf("MigrateOnVersionUpgrade: %v", err)
	}
	if !updated {
		t.Fatal("expected provider config update")
	}
	if config.Provider != "stacked" {
		t.Fatalf("provider = %q, want stacked", config.Provider)
	}

	versionBytes, err := os.ReadFile(filepath.Join(storagePath, "curd_version"))
	if err != nil {
		t.Fatalf("read version file: %v", err)
	}
	if strings.TrimSpace(string(versionBytes)) != "2.1.0" {
		t.Fatalf("version file = %q", string(versionBytes))
	}

	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(contents), "Provider=stacked") {
		t.Fatalf("config not persisted: %s", string(contents))
	}

	updated, err = MigrateOnVersionUpgrade(configPath, &config, "2.1.0")
	if err != nil {
		t.Fatalf("second migration: %v", err)
	}
	if updated {
		t.Fatal("expected no update on same version")
	}
}

func TestLoadConfigDoesNotDumpAllDefaultsIntoSparseFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "otakase.conf")
	storagePath := filepath.Join(tempDir, "share")
	initial := "StoragePath=" + storagePath + "\n" +
		"AddMissingOptions=true\n" +
		"Provider=stacked\n" +
		"Player=mpv\n"
	if err := os.WriteFile(configPath, []byte(initial), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	// In-memory defaults still apply.
	if config.VimKeys {
		t.Fatal("VimKeys default should be false in memory")
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(after)
	// Sparse file must not gain every historical default on a normal load.
	if strings.Contains(text, "VimKeys=") {
		t.Fatalf("LoadConfig must not append versioned options without an upgrade:\n%s", text)
	}
	if strings.Contains(text, "SkipOp=") {
		t.Fatalf("LoadConfig must not dump baseline defaults into sparse configs:\n%s", text)
	}
}

func TestMigrateOnVersionUpgradeInjectsOnlyOptionsForCrossedVersions(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "otakase.conf")
	storagePath := filepath.Join(tempDir, "share")
	initial := "StoragePath=" + storagePath + "\n" +
		"AddMissingOptions=true\n" +
		"Provider=stacked\n" +
		"Player=mpv\n"
	if err := os.WriteFile(configPath, []byte(initial), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.MkdirAll(storagePath, 0755); err != nil {
		t.Fatalf("mkdir storage: %v", err)
	}
	if err := os.WriteFile(filepath.Join(storagePath, "curd_version"), []byte("2.0.2\n"), 0644); err != nil {
		t.Fatalf("write version: %v", err)
	}

	config := PopulateConfig(map[string]string{
		"StoragePath":       storagePath,
		"AddMissingOptions": "true",
		"Provider":          "stacked",
		"Player":            "mpv",
	})

	updated, err := MigrateOnVersionUpgrade(configPath, &config, "2.0.3")
	if err != nil {
		t.Fatalf("MigrateOnVersionUpgrade: %v", err)
	}
	if !updated {
		t.Fatal("expected config file to gain 2.0.3 options on upgrade")
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config after migrate: %v", err)
	}
	text := string(after)
	if !strings.Contains(text, "VimKeys=false") {
		t.Fatalf("expected VimKeys=false for 2.0.3 upgrade:\n%s", text)
	}
	if !strings.Contains(text, "MpvPlaybackStartTimeout=20") {
		t.Fatalf("expected MpvPlaybackStartTimeout for 2.0.3 upgrade:\n%s", text)
	}
	// Must NOT dump unrelated baseline keys.
	if strings.Contains(text, "SkipOp=") || strings.Contains(text, "DiscordPresence=") {
		t.Fatalf("upgrade must not inject unregistered baseline options:\n%s", text)
	}

	updated, err = MigrateOnVersionUpgrade(configPath, &config, "2.0.3")
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if updated {
		t.Fatal("same version must not re-inject options")
	}
}

func TestInjectMissingConfigOptionsAddsOnlyRegisteredMissingKeys(t *testing.T) {
	m := map[string]string{"Player": "mpv", "VimKeys": "true"}
	added := injectMissingConfigOptions(m)
	if len(added) != len(configOptionsIntroducedInVersion())-1 {
		t.Fatalf("expected every registered option but VimKeys, got %v", added)
	}
	if m["VimKeys"] != "true" {
		t.Fatalf("an option already set must be left alone, got VimKeys=%q", m["VimKeys"])
	}
	if _, exists := m["SkipOp"]; exists {
		t.Fatal("baseline options must not be injected")
	}
	if second := injectMissingConfigOptions(m); len(second) != 0 {
		t.Fatalf("second inject should be empty, got %v", second)
	}
}

// An option added to defaultConfigMap but never registered was silently never
// added to existing configs, which is how ContinueWatchingRows, Quality, the
// Kodi and Cast options and others went missing on upgrade.
func TestEveryDefaultConfigOptionIsClassified(t *testing.T) {
	introduced := configOptionsIntroducedInVersion()
	baseline := baselineConfigOptions()
	for key := range defaultConfigMap() {
		_, registered := introduced[key]
		switch {
		case registered && baseline[key]:
			t.Errorf("%s is both baseline and registered as introduced", key)
		case !registered && !baseline[key]:
			t.Errorf("%s is in defaultConfigMap but not registered in configOptionsIntroducedInVersion", key)
		}
	}
	for key := range introduced {
		if _, ok := defaultConfigMap()[key]; !ok {
			t.Errorf("%s is registered but has no default", key)
		}
	}
}

func migrateFrom(t *testing.T, storedVersion, appVersion string, extra string) string {
	t.Helper()
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "otakase.conf")
	storagePath := filepath.Join(tempDir, "share")
	initial := "StoragePath=" + storagePath + "\nProvider=stacked\n" + extra
	if err := os.WriteFile(configPath, []byte(initial), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := writeStoredVersion(storagePath, storedVersion); err != nil {
		t.Fatalf("write version: %v", err)
	}
	config := PopulateConfig(map[string]string{"StoragePath": storagePath, "Provider": "stacked"})
	if _, err := MigrateOnVersionUpgrade(configPath, &config, appVersion); err != nil {
		t.Fatalf("MigrateOnVersionUpgrade: %v", err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	return string(after)
}

// Options whose release the user already passed still arrive on the next
// upgrade, so a late registration reaches them.
func TestUpgradeAddsOptionsFromReleasesAlreadyPassed(t *testing.T) {
	text := migrateFrom(t, "26.1.0", "26.1.1", "")
	for _, want := range []string{"ContinueWatchingRows=5", "Quality=best", "KodiHost=", "CastPort=0", "DevBuilds=false"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q after upgrade:\n%s", want, text)
		}
	}
}

// A dev build counts as the release it leads to, so 26.1.0-dev.4 -> 26.1.0
// used to cross no version at all and add nothing.
func TestUpgradeFromDevBuildToItsReleaseAddsOptions(t *testing.T) {
	text := migrateFrom(t, "26.1.0-dev.4", "26.1.0", "")
	for _, want := range []string{"AdultContent=false", "CastNextEpisode="} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q after upgrade:\n%s", want, text)
		}
	}
}

func TestUpgradeRespectsAddMissingOptionsFalse(t *testing.T) {
	text := migrateFrom(t, "2.2.2", "26.1.0", "AddMissingOptions=false\n")
	if strings.Contains(text, "DevBuilds=") {
		t.Fatalf("AddMissingOptions=false must not add options:\n%s", text)
	}
}

func TestCompareVersionsOrdering(t *testing.T) {
	if !versionLess("2.0.2", "2.0.3") {
		t.Fatal("2.0.2 should be < 2.0.3")
	}
	if !versionLess("", "2.0.3") {
		t.Fatal("empty stored version should be older than a release")
	}
	if versionLess("2.0.3", "2.0.3") {
		t.Fatal("equal versions should not be less")
	}
	if !versionLessOrEqual("2.0.3", "2.0.3") {
		t.Fatal("equal versions should be <= ")
	}
}

func TestConfiguredProviderNamesUsesStackedByDefault(t *testing.T) {
	withAllProvidersEnabledForTest(t)
	got := ConfiguredProviderNames(&Config{})
	want := []string{"anikoto", "kickassanime", "anizone", "anipub", "anineko", "nyaa", "sukebei", "anidb"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

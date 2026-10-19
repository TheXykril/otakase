package internal

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	_ "github.com/thexykril/otakase/internal/loadproviders"
)

func TestProviderMenuOffersTheStackThenEachProvider(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	menu := providerSelectionOptions()
	if len(menu) != len(fullTestStack)+1 {
		t.Fatalf("%d entries, want the stack plus one per provider", len(menu))
	}
	if menu[0].Key != "stacked" || !strings.HasPrefix(menu[0].Label, "Default with fallback (anikoto → ") {
		t.Fatalf("first entry %+v, want the stack", menu[0])
	}
	for i, name := range fullTestStack {
		if entry := menu[i+1]; entry.Key != `["`+name+`"]` || entry.Label != name {
			t.Errorf("entry %d is %+v, want %s alone", i+1, entry, name)
		}
	}
}

func TestMigrateProviderConfig(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	type migration struct {
		name, raw, want string
		changed         bool
	}
	for _, tc := range []migration{
		{"legacy senshi default", `["senshi"]`, "stacked", true},
		{"legacy allanime default", `["allanime"]`, "stacked", true},
		{"removed provider beside a working one", `["senshi","anineko"]`, "stacked", true},
		{"empty", "", "stacked", true},
		{"stack alias", "stack", "stacked", true},
		{"already stacked", "stacked", "stacked", false},
		{"padded stacked", " stacked ", "stacked", false},
		{"single anineko", `["anineko"]`, `["anineko"]`, false},
		{"single bare name", "anineko", `["anineko"]`, true},
		{"alias spelling", `["Kick-Ass Anime"]`, `["kickassanime"]`, true},
		{"own list", `["anipub","anineko"]`, "stacked", true},
	} {
		value, changed := migrateProviderConfig(tc.raw)
		if value != tc.want || changed != tc.changed {
			t.Errorf("%s: %q migrates to (%q, %v), want (%q, %v)", tc.name, tc.raw, value, changed, tc.want, tc.changed)
		}
	}
}

func TestIsFactoryDefaultProvider(t *testing.T) {
	withDisabledProvidersForTest(t, "anidb")
	for raw, want := range map[string]bool{
		"":                   true,
		"auto":               true,
		`["anidb"]`:          true,
		`["anipub"]`:         false,
		`["anipub","anidb"]`: false,
	} {
		if got := isFactoryDefaultProvider(raw); got != want {
			t.Errorf("%q: got %v, want %v", raw, got, want)
		}
	}
}

func TestParseConfigBool(t *testing.T) {
	for _, yes := range []string{"true", " YES ", "1", "y", "On"} {
		if got, err := parseConfigBool(yes); err != nil || !got {
			t.Errorf("%q: got (%v, %v)", yes, got, err)
		}
	}
	for _, no := range []string{"false", "No", "0", "n", "OFF"} {
		if got, err := parseConfigBool(no); err != nil || got {
			t.Errorf("%q: got (%v, %v)", no, got, err)
		}
	}
	if _, err := parseConfigBool("maybe"); err == nil {
		t.Error("an unknown word parsed")
	}
}

func TestMigrateProviderConfigReplacesASingleDisabledProvider(t *testing.T) {
	withDisabledProvidersForTest(t, "anidb")

	if got, changed := migrateProviderConfig(`["anidb"]`); got != "stacked" || !changed {
		t.Fatalf("got (%q, %v), want the stack", got, changed)
	}
}

func TestIsStackedProviderConfig(t *testing.T) {
	for _, raw := range []string{"stacked", "Stack", " auto ", "ALL"} {
		if !isStackedProviderConfig(raw) {
			t.Errorf("%q should read as the stack", raw)
		}
	}
	for _, raw := range []string{"", "anipub", "stacks"} {
		if isStackedProviderConfig(raw) {
			t.Errorf("%q should not read as the stack", raw)
		}
	}
}

func TestProviderConfigDisplayLabel(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	if got := providerConfigDisplayLabel(`["anineko","anipub"]`); got != "anineko → anipub" {
		t.Errorf("list: got %q", got)
	}
	if got := providerConfigDisplayLabel(`["anipub"]`); got != "anipub" {
		t.Errorf("single: got %q", got)
	}
	if got := providerConfigDisplayLabel("auto"); got != "Default with fallback ("+strings.Join(fullTestStack, " → ")+")" {
		t.Errorf("stack: got %q", got)
	}
}

func TestStoredVersionRoundTrip(t *testing.T) {
	storagePath := filepath.Join(t.TempDir(), "share")
	if got := readStoredVersion(storagePath); got != "" {
		t.Fatalf("no version written yet, got %q", got)
	}
	if err := writeStoredVersion(storagePath, " 26.8.0 "); err != nil {
		t.Fatalf("writeStoredVersion: %v", err)
	}
	if got := readStoredVersion(storagePath); got != "26.8.0" {
		t.Fatalf("got %q", got)
	}
	if err := writeStoredVersion("", "26.8.0"); err != nil {
		t.Fatalf("an empty storage path should be a no-op, got %v", err)
	}
}

func TestAppendConfigKeysLeavesExistingLinesAlone(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "otakase.conf")
	if err := os.WriteFile(configPath, []byte("Player=mpv\n# mine\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	values := map[string]string{"VimKeys": "false", "CheckUpdates": "true"}
	if err := appendConfigKeys(configPath, values, []string{"VimKeys", "Missing", "CheckUpdates"}); err != nil {
		t.Fatalf("appendConfigKeys: %v", err)
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if want := "Player=mpv\n# mine\nVimKeys=false\nCheckUpdates=true\n"; string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestCanonicalProviderConfigValuePrefersStackedToken(t *testing.T) {
	withAllProvidersEnabledForTest(t)
	for _, raw := range []string{"stacked", ""} {
		if got := canonicalProviderConfigValue(raw); got != "stacked" {
			t.Errorf("%q: got %q, want stacked", raw, got)
		}
	}
}

func TestUpgradeMovesAnOwnListToTheStackAndRecordsTheVersion(t *testing.T) {
	withAllProvidersEnabledForTest(t)
	dir := t.TempDir()
	confPath, share := filepath.Join(dir, "otakase.conf"), filepath.Join(dir, "share")
	if err := os.WriteFile(confPath, []byte("Provider=[\"anineko\",\"nyaa\"]\nStoragePath="+share+"\n"), 0644); err != nil {
		t.Fatalf("seeding the config: %v", err)
	}

	conf, err := LoadConfig(confPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if conf.Provider != `["anineko","nyaa"]` {
		t.Fatalf("loading alone changed the provider to %q", conf.Provider)
	}

	changed, err := MigrateOnVersionUpgrade(confPath, &conf, "2.1.0")
	if err != nil || !changed {
		t.Fatalf("first run of 2.1.0: (%v, %v), want a change", changed, err)
	}
	if conf.Provider != "stacked" {
		t.Fatalf("provider %q after the upgrade, want stacked", conf.Provider)
	}
	if v := readStoredVersion(share); v != "2.1.0" {
		t.Fatalf("recorded version %q", v)
	}
	onDisk, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("reading the config back: %v", err)
	}
	if !strings.Contains(string(onDisk), "Provider=stacked") {
		t.Fatalf("the file kept the old provider:\n%s", onDisk)
	}

	if changed, err := MigrateOnVersionUpgrade(confPath, &conf, "2.1.0"); err != nil || changed {
		t.Fatalf("second run of 2.1.0: (%v, %v), want nothing to do", changed, err)
	}
}

func TestMigrateOnVersionUpgradeWithoutAConfigFileMigratesInMemory(t *testing.T) {
	withAllProvidersEnabledForTest(t)
	storagePath := filepath.Join(t.TempDir(), "share")
	config := Config{StoragePath: storagePath, Provider: "auto"}

	updated, err := MigrateOnVersionUpgrade("", &config, "26.8.0")
	if err != nil || !updated || config.Provider != "stacked" {
		t.Fatalf("got (%v, %v) provider %q, want an in-memory migration to stacked", updated, err, config.Provider)
	}
	if got := readStoredVersion(storagePath); got != "26.8.0" {
		t.Fatalf("stored version = %q", got)
	}
}

// writeSparseConfig writes a hand-written config holding only a few options.
func writeSparseConfig(t *testing.T) (configPath, storagePath string) {
	t.Helper()
	dir := t.TempDir()
	configPath, storagePath = filepath.Join(dir, "otakase.conf"), filepath.Join(dir, "share")
	sparse := "StoragePath=" + storagePath + "\nAddMissingOptions=true\nProvider=stacked\nPlayer=mpv\n"
	if err := os.WriteFile(configPath, []byte(sparse), 0644); err != nil {
		t.Fatalf("seeding the config: %v", err)
	}
	return configPath, storagePath
}

// readConfigText is the config file's contents, failing the test when it
// cannot be read.
func readConfigText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func TestLoadConfigDoesNotDumpAllDefaultsIntoSparseFile(t *testing.T) {
	path, _ := writeSparseConfig(t)

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	// Defaults still apply in memory.
	if loaded.VimKeys {
		t.Fatal("VimKeys came up true though its default is false")
	}

	text := readConfigText(t, path)
	for _, key := range []string{"VimKeys=", "SkipOp="} {
		if strings.Contains(text, key) {
			t.Fatalf("a plain load wrote %s into a sparse config:\n%s", key, text)
		}
	}
}

func TestUpgradeAppendsOptionsOfTheCrossedRelease(t *testing.T) {
	path, share := writeSparseConfig(t)
	if err := writeStoredVersion(share, "2.0.2"); err != nil {
		t.Fatalf("recording 2.0.2: %v", err)
	}
	conf := PopulateConfig(map[string]string{"StoragePath": share, "AddMissingOptions": "true", "Provider": "stacked", "Player": "mpv"})

	changed, err := MigrateOnVersionUpgrade(path, &conf, "2.0.3")
	if err != nil || !changed {
		t.Fatalf("2.0.2 to 2.0.3: (%v, %v), want its options added", changed, err)
	}

	text := readConfigText(t, path)
	for _, want := range []string{"VimKeys=false", "MpvPlaybackStartTimeout=20"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %s after the 2.0.3 upgrade:\n%s", want, text)
		}
	}
	// Baseline options are never appended.
	for _, unwanted := range []string{"SkipOp=", "DiscordPresence="} {
		if strings.Contains(text, unwanted) {
			t.Errorf("upgrade appended baseline option %s:\n%s", unwanted, text)
		}
	}

	if changed, err := MigrateOnVersionUpgrade(path, &conf, "2.0.3"); err != nil || changed {
		t.Fatalf("second run of 2.0.3: (%v, %v), want nothing to do", changed, err)
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
		t.Fatalf("a second pass added %v again", second)
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

func TestVersionOrderingTreatsBlankAsOldest(t *testing.T) {
	for _, tc := range []struct {
		a, b         string
		less, lessEq bool
	}{
		{a: "2.0.2", b: "2.0.3", less: true, lessEq: true},
		{a: "", b: "2.0.3", less: true, lessEq: true},
		{a: "2.0.3", b: "2.0.3", less: false, lessEq: true},
	} {
		if got := versionLess(tc.a, tc.b); got != tc.less {
			t.Errorf("versionLess(%q, %q) = %v", tc.a, tc.b, got)
		}
		if got := versionAtMost(tc.a, tc.b); got != tc.lessEq {
			t.Errorf("versionAtMost(%q, %q) = %v", tc.a, tc.b, got)
		}
	}
}

func TestAnEmptyProviderSettingMeansTheStack(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	if got := ConfiguredProviderNames(&Config{}); !slices.Equal(got, fullTestStack) {
		t.Fatalf("got %v, want %v", got, fullTestStack)
	}
}

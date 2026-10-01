package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const stackedProviderConfigValue = "stacked"

func isStackedProviderConfig(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "stacked", "stack", "auto", "all":
		return true
	default:
		return false
	}
}

func isFactoryDefaultProvider(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	if isStackedProviderConfig(raw) {
		return true
	}

	names := parseProviderConfig(raw)
	switch len(names) {
	case 0:
		return true
	case 1:
		return !ProviderEnabled(names[0])
	}
	return false
}

// namesRemovedProvider reports whether a config names something this build no
// longer has.
//
// parseProviderConfig cannot answer this: when nothing in a config resolves it
// substitutes the head of the stack, so a config naming only removed providers
// comes back looking like a deliberate choice of whatever that happens to be.
func namesRemovedProvider(raw string) bool {
	for _, part := range parseProviderConfigParts(raw) {
		if strings.TrimSpace(strings.Trim(strings.TrimSpace(part), "\"'[]")) == "" {
			continue
		}
		if normalizeProviderName(part) == "" {
			return true
		}
	}
	return false
}

func providerListsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func migrateProviderConfig(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)

	// A list naming a provider that has since been removed is no longer the
	// list the user chose. Quietly dropping the dead name would leave them on
	// a shorter stack they never picked, so hand back the current default.
	if raw != "" && !isStackedProviderConfig(raw) && namesRemovedProvider(raw) {
		return stackedProviderConfigValue, true
	}

	if isFactoryDefaultProvider(raw) {
		if raw == stackedProviderConfigValue {
			return raw, false
		}
		return stackedProviderConfigValue, true
	}

	if isStackedProviderConfig(raw) {
		if raw != stackedProviderConfigValue {
			return stackedProviderConfigValue, true
		}
		return raw, false
	}

	names := parseProviderConfig(raw)
	if len(names) > 1 {
		return stackedProviderConfigValue, true
	}

	canonical := canonicalProviderConfigValue(raw)
	if canonical != raw {
		return canonical, true
	}
	return raw, false
}

func readStoredVersion(storagePath string) string {
	path := storageVersionFilePath(storagePath)
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func writeStoredVersion(storagePath, version string) error {
	storagePath = strings.TrimSpace(storagePath)
	version = strings.TrimSpace(version)
	if storagePath == "" || version == "" {
		return nil
	}
	if err := os.MkdirAll(storagePath, 0755); err != nil {
		return err
	}
	return os.WriteFile(storageVersionFilePath(storagePath), []byte(version+"\n"), 0644)
}

// configOptionsIntroducedInVersion maps config keys to the otakase release that
// introduced them. Only these keys are appended to an existing config on
// upgrade; baseline options (Player, StoragePath, …) are listed in
// baselineConfigOptions instead and only appear via createDefaultConfig, so a
// sparse hand-written config never gets every historical default dumped in.
//
// When adding a new option:
//  1. Add it to Config + defaultConfigMap()
//  2. Register it here under the release version that ships it
//
// TestEveryDefaultConfigOptionIsClassified fails until step 2 is done.
func configOptionsIntroducedInVersion() map[string]string {
	return map[string]string{
		// 2.0.3 — playback fallback timeout + vim selection motions
		"MpvPlaybackStartTimeout": "2.0.3",
		"VimKeys":                 "2.0.3",
		// 2.0.4 — idle background update checks
		"CheckUpdates": "2.0.4",
		// 2.0.5 — MPV episode playlist + alternate audio entries
		"MpvEpisodePlaylist": "2.0.5",
		// 1.1.0 — straight into the watching list, AnimeSkip client, theme tweaks
		"CurrentCategory":   "1.1.0",
		"AnimeSkipClientID": "1.1.0",
		"ThemeOverrides":    "1.1.0",
		// 1.3.0 — skip-time sources
		"ContributeSkipTimes": "1.3.0",
		"IntroDBSkipTimes":    "1.3.0",
		// 2.1.0 — casting
		"CastDevice":        "2.1.0",
		"CastTerminal":      "2.1.0",
		"CastPort":          "2.1.0",
		"CastBurnSubtitles": "2.1.0",
		"CastEncoder":       "2.1.0",
		// 26.1.0 — what a cast does when an episode ends
		"CastNextEpisode": "26.1.0",
		// 26.1.0 — update to dev builds
		"DevBuilds": "26.1.0",
		// 26.1.0 — hide 18+ titles
		"AdultContent": "26.1.0",
		// 26.1.0 — continue-watching rows, quality, downloads, Kodi casting
		"ContinueWatchingRows": "26.1.0",
		"Quality":              "26.1.0",
		"DownloadFormat":       "26.1.0",
		"KodiHost":             "26.1.0",
		"KodiUser":             "26.1.0",
		"KodiPassword":         "26.1.0",
	}
}

// baselineConfigOptions are the options every config has had from the start.
// They are written by createDefaultConfig and never appended on upgrade.
func baselineConfigOptions() map[string]bool {
	return map[string]bool{
		"Player": true, "MpvArgs": true, "StoragePath": true,
		"AnimeNameLanguage": true, "SubsLanguage": true, "MenuOrder": true,
		"SubOrDub": true, "SubStyle": true, "PercentageToMarkComplete": true,
		"NextEpisodePrompt": true, "AutoAudioFallback": true,
		"SkipOp": true, "SkipEd": true, "SkipFiller": true, "SkipRecap": true,
		"RofiSelection": true, "ImagePreview": true, "ScoreOnCompletion": true,
		"SaveMpvSpeed": true, "AddMissingOptions": true, "AlternateScreen": true,
		"DiscordPresence": true, "DiscordClientId": true,
		"Provider": true, "DisabledProviders": true, "ManualProviderSearch": true,
		"TrackingLocal": true, "TrackingRemote": true, "TrackingConfigured": true,
		"MyAnimeListClientID": true, "MyAnimeListClientSecret": true,
		"MyAnimeListImported": true, "MyAnimeListImportDismissed": true,
		"ShowNewEpisodes": true, "Theme": true, "DownloadDir": true,
	}
}

// injectMissingConfigOptions adds the default of every registered option the
// config lacks and reports the keys added.
//
// It deliberately ignores the version the user upgraded from. Gating on it
// lost options twice over: one registered late (after the user had already
// passed its release) was never added, and a dev build counts as the release
// it leads to, so going from 26.1.0-dev.4 to 26.1.0 crossed "nothing" and
// skipped every 26.1.0 option added after dev.4.
func injectMissingConfigOptions(configMap map[string]string) []string {
	if configMap == nil {
		return nil
	}
	defaults := defaultConfigMap()
	introduced := configOptionsIntroducedInVersion()

	keys := make([]string, 0, len(introduced))
	for key := range introduced {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	added := make([]string, 0)
	for _, key := range keys {
		if _, exists := configMap[key]; exists {
			continue
		}
		value, ok := defaults[key]
		if !ok {
			continue
		}
		configMap[key] = value
		added = append(added, key)
	}
	return added
}

// menuKeysIntroducedInVersion lists MenuOrder entries by the release that
// added them. MenuOrder shows only what it names, so an entry added after
// someone saved a MenuOrder of their own would never reach them otherwise.
func menuKeysIntroducedInVersion() map[string]string {
	return map[string]string{
		"SURPRISE": "26.1.0",
		"STATS":    "26.1.0",
	}
}

// injectMenuKeysSince adds the menu entries introduced in versions
// (fromVersion, toVersion] to a saved MenuOrder that lacks them, each after
// the entry it follows in the default order, or at the end. It reports the
// keys added. A config without a MenuOrder already gets the default, which
// has them.
func injectMenuKeysSince(configMap map[string]string, fromVersion, toVersion string) []string {
	current, exists := configMap["MenuOrder"]
	if !exists || strings.TrimSpace(current) == "" {
		return nil
	}
	order := []string{}
	present := map[string]bool{}
	for _, raw := range strings.Split(current, ",") {
		key := strings.TrimSpace(raw)
		if key == "" {
			continue
		}
		order = append(order, key)
		present[strings.ToUpper(key)] = true
	}
	defaults := strings.Split(defaultConfigMap()["MenuOrder"], ",")

	introduced := menuKeysIntroducedInVersion()
	keys := make([]string, 0, len(introduced))
	for key := range introduced {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	added := []string{}
	for _, key := range keys {
		since := introduced[key]
		if !versionLess(fromVersion, since) || !versionLessOrEqual(since, toVersion) || present[key] {
			continue
		}
		at := len(order)
		for i, defaultKey := range defaults {
			if defaultKey != key || i == 0 {
				continue
			}
			for j, existing := range order {
				if strings.EqualFold(existing, defaults[i-1]) {
					at = j + 1
				}
			}
		}
		order = append(order[:at], append([]string{key}, order[at:]...)...)
		present[key] = true
		added = append(added, key)
	}
	if len(added) > 0 {
		configMap["MenuOrder"] = strings.Join(order, ",")
	}
	return added
}

// appendConfigKeys appends only the given keys to the config file so existing
// user options and ordering are left untouched.
func appendConfigKeys(configPath string, configMap map[string]string, keys []string) error {
	if strings.TrimSpace(configPath) == "" || len(keys) == 0 {
		return nil
	}
	file, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	for _, key := range keys {
		value, ok := configMap[key]
		if !ok {
			continue
		}
		if _, err := fmt.Fprintf(file, "%s=%s\n", key, value); err != nil {
			return err
		}
	}
	return nil
}

// MigrateOnVersionUpgrade updates stored state and config when otakase is upgraded.
// On version change it appends registered config options the file lacks, then
// runs provider migrations.
// Same-version launches do not rewrite the config file.
// Returns whether the config file was updated.
func MigrateOnVersionUpgrade(configPath string, config *Config, appVersion string) (bool, error) {
	if config == nil {
		return false, nil
	}

	appVersion = strings.TrimSpace(appVersion)
	if appVersion == "" {
		appVersion = Version()
	}

	storagePath := os.ExpandEnv(config.StoragePath)
	if storagePath == "" {
		storagePath = filepath.Join(os.ExpandEnv("$HOME"), ".local", "share", AppName)
	}

	storedVersion := readStoredVersion(storagePath)
	configUpdated := false
	versionChanged := storedVersion != appVersion

	if versionChanged && strings.TrimSpace(configPath) != "" {
		configMap, err := LoadConfigFromFile(configPath)
		if err != nil {
			return false, err
		}

		// Respect AddMissingOptions=false as a hard opt-out of writing new keys.
		addMissing := true
		if config != nil {
			addMissing = config.AddMissingOptions
		}
		if val, exists := configMap["AddMissingOptions"]; exists {
			if parsed, parseErr := parseConfigBool(val); parseErr == nil {
				addMissing = parsed
			}
		}

		if addMissing {
			if added := injectMissingConfigOptions(configMap); len(added) > 0 {
				if err := appendConfigKeys(configPath, configMap, added); err != nil {
					return false, fmt.Errorf("append new config options: %w", err)
				}
				configUpdated = true
				Log(fmt.Sprintf("Injected config options for upgrade %s → %s: %s",
					storedVersion, appVersion, strings.Join(added, ", ")))
			}
		}

		if addMissing {
			if added := injectMenuKeysSince(configMap, storedVersion, appVersion); len(added) > 0 {
				// MenuOrder is already in the file; rewrite it with the entries.
				if err := SaveConfigToFile(configPath, configMap); err != nil {
					return configUpdated, fmt.Errorf("add new menu entries: %w", err)
				}
				configUpdated = true
				Log(fmt.Sprintf("Added menu entries for upgrade %s → %s: %s",
					storedVersion, appVersion, strings.Join(added, ", ")))
			}
		}

		if nextProvider, changed := migrateProviderConfig(configMap["Provider"]); changed {
			configMap["Provider"] = nextProvider
			// Provider value already exists in the file — rewrite the full map once.
			if err := SaveConfigToFile(configPath, configMap); err != nil {
				return configUpdated, err
			}
			configUpdated = true
		}

		// Refresh in-memory config so new keys (e.g. VimKeys) apply immediately.
		next := PopulateConfig(configMap)
		normalizeTrackingConfig(&next)
		*config = next
	} else if versionChanged {
		// No config path, still run in-memory provider migration.
		if nextProvider, changed := migrateProviderConfig(config.Provider); changed {
			config.Provider = nextProvider
			configUpdated = true
		}
	}

	if err := writeStoredVersion(storagePath, appVersion); err != nil {
		return configUpdated, fmt.Errorf("write version file: %w", err)
	}

	return configUpdated, nil
}

func parseConfigBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "y", "on":
		return true, nil
	case "false", "0", "no", "n", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool %q", value)
	}
}

func providerConfigDisplayLabel(raw string) string {
	if isStackedProviderConfig(raw) {
		names := defaultEnabledProviderStack()
		if len(names) == 0 {
			return "Default with fallback"
		}
		return fmt.Sprintf("Default with fallback (%s)", strings.Join(names, " → "))
	}
	names := parseProviderConfig(raw)
	if len(names) == 1 {
		return names[0]
	}
	if len(names) > 1 {
		return strings.Join(names, " → ")
	}
	return raw
}

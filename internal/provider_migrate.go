package internal

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// stackedProviderConfigValue is the Provider setting for "the default stack",
// written instead of the list itself so a config follows the stack as it is
// reordered between releases.
const stackedProviderConfigValue = "stacked" // the setting's value for the stack

// stackedProviderSpellings are every value read as the stack, the older
// spellings included.
var stackedProviderSpellings = []string{stackedProviderConfigValue, "stack", "auto", "all"}

// isStackedProviderConfig accepts the older spellings of "stacked" as well.
func isStackedProviderConfig(value string) bool {
	return slices.Contains(stackedProviderSpellings, strings.ToLower(strings.TrimSpace(value)))
}

// isFactoryDefaultProvider reports whether a Provider value is one the user
// never really chose: empty, the stack, or a single provider that can no
// longer be used. Each of these is migrated to the current stack.
func isFactoryDefaultProvider(value string) bool {
	v := strings.TrimSpace(value)
	if v == "" || isStackedProviderConfig(v) {
		return true
	}
	names := parseProviderConfig(v)
	return len(names) == 0 || (len(names) == 1 && !ProviderEnabled(names[0]))
}

// namesRemovedProvider reports whether a config names something this build no
// longer has.
//
// parseProviderConfig cannot answer this: when nothing in a config resolves it
// substitutes the head of the stack, so a config naming only removed providers
// comes back looking like a deliberate choice of whatever that happens to be.
func namesRemovedProvider(raw string) bool {
	return slices.ContainsFunc(parseProviderConfigParts(raw), func(part string) bool {
		blank := strings.TrimSpace(strings.Trim(strings.TrimSpace(part), "\"'[]")) == ""
		return !blank && normalizeProviderName(part) == ""
	})
}

// migrateProviderConfig returns the Provider value an upgraded config should
// hold, and whether that differs from raw. A single provider the user picked
// is kept; anything else becomes the stack.
func migrateProviderConfig(value string) (string, bool) {
	v := strings.TrimSpace(value)

	// A list naming a provider that has since been removed is no longer the
	// list the user chose. Quietly dropping the dead name would leave them on
	// a shorter stack they never picked, so hand back the current default.
	if v != "" && !isStackedProviderConfig(v) && namesRemovedProvider(v) {
		return stackedProviderConfigValue, true
	}

	if isFactoryDefaultProvider(v) || len(parseProviderConfig(v)) > 1 {
		return stackedProviderConfigValue, v != stackedProviderConfigValue
	}
	tidy := canonicalProviderConfigValue(v)
	return tidy, tidy != v
}

// readStoredVersion is the release that last ran against this storage, or ""
// before any has.
func readStoredVersion(storagePath string) string {
	data, err := os.ReadFile(storageVersionFilePath(storagePath))
	if err != nil {
		return "" // not written yet, or unreadable: treated alike
	}
	return strings.TrimSpace(string(data))
}

// writeStoredVersion records release as the one that last ran against the
// storage directory. Blank arguments write nothing.
func writeStoredVersion(storagePath, release string) error {
	dir, release := strings.TrimSpace(storagePath), strings.TrimSpace(release)
	if dir == "" || release == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(storageVersionFilePath(dir), []byte(release+"\n"), 0o644)
}

// configOptionsIntroducedInVersion maps config keys to the otakase release that
// introduced them. Only these keys are appended to an existing config on
// upgrade; baseline options (Player, StoragePath, …) are listed in
// baselineConfigOptions instead and only appear via createDefaultConfig, so a
// sparse hand-written config never gets every historical default dumped in.
// A new option goes into Config and defaultConfigMap, and here under the
// release that ships it; TestEveryDefaultConfigOptionIsClassified fails until
// it is registered.
func configOptionsIntroducedInVersion() map[string]string {
	return map[string]string{ // key -> first release that has it
		// 1.1.0 — straight into the watching list, AnimeSkip client, theme tweaks
		"AnimeSkipClientID": "1.1.0",
		"ThemeOverrides":    "1.1.0",
		// 1.3.0 — skip-time sources
		"ContributeSkipTimes": "1.3.0",
		"IntroDBSkipTimes":    "1.3.0",
		// 2.0.3 — stream start timeout, vim motions in menus
		"MpvPlaybackStartTimeout": "2.0.3",
		"VimKeys":                 "2.0.3",
		// 2.0.4 — update checks in the background
		"CheckUpdates": "2.0.4",
		// 2.0.5 — the episode playlist in mpv
		"MpvEpisodePlaylist": "2.0.5",
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
		// 26.1.2 — 18+ shows in continue watching, opt-in
		"ContinueWatchingAdult": "26.1.2",
		// 26.1.3 — fixed discovery port, so a firewall can let answers in
		"CastDiscoveryPort": "26.1.3",
		// 26.5.0 — line icons in the menus
		"Icons": "26.5.0",
		// 26.6.0 — the menu's actions get their own setting
		"MenuActions": "26.6.0",
		// 26.7.0 — otakase's own look in mpv
		"MpvSkin": "26.7.0",
		// 26.7.0 — menus follow more desktop themes, or a palette file
		"ThemeFile": "26.7.0",
		// 26.7.0 — movies, experimental and off by default
		"ExperimentalMovies": "26.7.0",
		"MovieSite":          "26.7.0",
		"MovieProvider":      "26.7.0",
		"TraktClientID":      "26.7.0",
		"TraktClientSecret":  "26.7.0",
		// 26.8.0 — the app linked to the Otakase Discord
		"DiscordBotURL":        "26.8.0",
		"DiscordShareWatching": "26.8.0",
		"DiscordWatchWithMe":   "26.8.0",
		"DiscordShareRatings":  "26.8.0",
		// 26.8.0 — where links opened from the browser show their menus
		"LinkMenus": "26.8.0",
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
	fallback := defaultConfigMap()
	missing := []string{}
	// Sorted, so the keys land in the file in the same order every time.
	for _, key := range slices.Sorted(maps.Keys(configOptionsIntroducedInVersion())) {
		_, present := configMap[key]
		value, known := fallback[key]
		if !present && known {
			configMap[key] = value
			missing = append(missing, key)
		}
	}
	return missing
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
	defaults := defaultMenuKeys()

	introduced := menuKeysIntroducedInVersion()
	keys := make([]string, 0, len(introduced))
	for key := range introduced {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	added := []string{}
	for _, key := range keys {
		since := introduced[key]
		if !versionLess(fromVersion, since) || !versionAtMost(since, toVersion) || present[key] {
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

// pastDefaultMenuOrders are the MenuOrder values earlier releases wrote, less
// SURPRISE and STATS: an upgrade adds those to a saved MenuOrder, and where it
// puts them depends on the release, so they are left out of the comparison.
// A config still holding one never chose its menu, so it gets the new one.
var pastDefaultMenuOrders = []string{
	"CURRENT,ALL,UNTRACKED,UPDATE,REMAP_PROVIDER,CONTINUE_LAST,TRACKER,PROVIDER",
	"CURRENT,ALL,PLANNING,PAUSED,DROPPED,REWATCHING,UNTRACKED,UPDATE,REMAP_PROVIDER,CONTINUE_LAST,TRACKER,PROVIDER,CAST",
}

// isPastDefaultMenuOrder reports whether a saved MenuOrder is one a release
// wrote rather than one the user chose.
func isPastDefaultMenuOrder(menuOrder string) bool {
	kept := []string{}
	for _, key := range menuKeys(&Config{MenuOrder: menuOrder}) {
		if key != "SURPRISE" && key != "STATS" {
			kept = append(kept, key)
		}
	}
	saved := strings.Join(kept, ",")
	for _, past := range pastDefaultMenuOrders {
		if saved == past {
			return true
		}
	}
	return false
}

// splitMenuActions gives a config written before MenuActions existed the new
// setting, and reports whether it changed anything.
//
// A MenuOrder that is a past default is replaced by the new, shorter menu. One
// the user wrote keeps every entry and its order: the lists stay in MenuOrder
// and the actions move to MenuActions. Moving them rather than leaving them is
// what keeps the default MenuActions from being added on top of a menu someone
// trimmed on purpose.
func splitMenuActions(configMap map[string]string) bool {
	if configMap == nil {
		return false
	}
	if _, exists := configMap["MenuActions"]; exists {
		return false
	}
	current, exists := configMap["MenuOrder"]
	if !exists || strings.TrimSpace(current) == "" {
		// Nothing chosen: the defaults of both settings apply.
		return false
	}
	if isPastDefaultMenuOrder(current) {
		defaults := defaultConfigMap()
		configMap["MenuOrder"] = defaults["MenuOrder"]
		configMap["MenuActions"] = defaults["MenuActions"]
		return true
	}

	lists, actions := []string{}, []string{}
	for _, key := range menuKeys(&Config{MenuOrder: current}) {
		if isMenuActionKey(key) {
			actions = append(actions, key)
		} else {
			lists = append(lists, key)
		}
	}
	if len(actions) == 0 {
		// An empty value reads as the default, which would add every action
		// to a menu that had none. TRACKER is always shown anyway.
		actions = []string{"TRACKER"}
	}
	configMap["MenuOrder"] = strings.Join(lists, ",")
	configMap["MenuActions"] = strings.Join(actions, ",")
	return true
}

// appendConfigKeys writes keys at the end of the config file. Appending, not
// rewriting, leaves the user's own lines and their order alone.
func appendConfigKeys(configPath string, configMap map[string]string, keys []string) (err error) {
	if len(keys) == 0 || strings.TrimSpace(configPath) == "" {
		return nil
	}
	f, err := os.OpenFile(configPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}()

	for _, name := range keys {
		value, ok := configMap[name]
		if !ok {
			continue
		}
		if _, err := fmt.Fprintf(f, "%s=%s\n", name, value); err != nil {
			return err
		}
	}
	return nil
}

// MigrateOnVersionUpgrade brings the config up to date the first time a new
// release runs: menu entries and options it introduced are added, unless
// AddMissingOptions is off, and an old Provider value is migrated. A launch
// of the same release as last time changes nothing. It reports whether the
// config was updated.
func MigrateOnVersionUpgrade(configPath string, config *Config, appVersion string) (bool, error) {
	if config == nil {
		return false, nil
	}
	if appVersion = strings.TrimSpace(appVersion); appVersion == "" {
		appVersion = Version()
	}
	storagePath := versionStorageDir(config)

	storedVersion := readStoredVersion(storagePath)
	var configUpdated bool
	switch {
	case storedVersion == appVersion:
	case strings.TrimSpace(configPath) == "":
		// Nothing on disk to update; the provider still migrates in memory.
		if migrated, changed := migrateProviderConfig(config.Provider); changed {
			config.Provider = migrated
			configUpdated = true
		}
	default:
		var err error
		configUpdated, err = migrateConfigFile(configPath, config, storedVersion, appVersion)
		if err != nil {
			return configUpdated, err
		}
	}

	if err := writeStoredVersion(storagePath, appVersion); err != nil {
		return configUpdated, fmt.Errorf("write version file: %w", err)
	}
	return configUpdated, nil
}

// versionStorageDir is where the version file lives: the configured storage
// path, or the default one under the home directory.
func versionStorageDir(config *Config) string {
	if dir := os.ExpandEnv(config.StoragePath); dir != "" {
		return dir
	}
	return filepath.Join(os.ExpandEnv("$HOME"), ".local", "share", AppName)
}

// migrateConfigFile is MigrateOnVersionUpgrade for a config on disk. The steps
// that rewrite the file run before the new options are appended, so none of
// them undoes another. config is reloaded from the result.
func migrateConfigFile(configPath string, config *Config, fromVersion, toVersion string) (bool, error) {
	configMap, err := LoadConfigFromFile(configPath)
	if err != nil {
		return false, err
	}

	// AddMissingOptions=false in the file is a hard opt-out of new keys.
	addMissing := config.AddMissingOptions
	if raw, ok := configMap["AddMissingOptions"]; ok {
		if parsed, parseErr := parseConfigBool(raw); parseErr == nil {
			addMissing = parsed
		}
	}

	updated := false
	if addMissing {
		if added := injectMenuKeysSince(configMap, fromVersion, toVersion); len(added) > 0 {
			// MenuOrder is already in the file; rewrite it with the entries.
			if err := SaveConfigToFile(configPath, configMap); err != nil {
				return updated, fmt.Errorf("add new menu entries: %w", err)
			}
			updated = true
			Log(fmt.Sprintf("Added menu entries for upgrade %s → %s: %s",
				fromVersion, toVersion, strings.Join(added, ", ")))
		}

		if splitMenuActions(configMap) {
			// Both settings are rewritten in place, before the new options are
			// appended, or MenuActions would arrive as its default.
			if err := SaveConfigToFile(configPath, configMap); err != nil {
				return updated, fmt.Errorf("split menu actions: %w", err)
			}
			updated = true
			Log(fmt.Sprintf("Menu for upgrade %s → %s: MenuOrder=%s MenuActions=%s",
				fromVersion, toVersion, configMap["MenuOrder"], configMap["MenuActions"]))
		}

		if added := injectMissingConfigOptions(configMap); len(added) > 0 {
			if err := appendConfigKeys(configPath, configMap, added); err != nil {
				return false, fmt.Errorf("append new config options: %w", err)
			}
			updated = true
			Log(fmt.Sprintf("Injected config options for upgrade %s → %s: %s",
				fromVersion, toVersion, strings.Join(added, ", ")))
		}
	}

	if migrated, changed := migrateProviderConfig(configMap["Provider"]); changed {
		configMap["Provider"] = migrated
		// Provider is already in the file, so the whole file is rewritten.
		if err := SaveConfigToFile(configPath, configMap); err != nil {
			return updated, err
		}
		updated = true
	}

	// Reload, so options added just now apply to this run.
	next := PopulateConfig(configMap)
	normalizeTrackingConfig(&next)
	*config = next
	return updated, nil
}

// configBoolWords are the spellings of yes and no a config file may use.
var configBoolWords = map[string]bool{
	"true": true, "1": true, "yes": true, "y": true, "on": true,
	"false": false, "0": false, "no": false, "n": false, "off": false,
}

// parseConfigBool reads a yes/no config value, case and spacing aside.
func parseConfigBool(text string) (bool, error) {
	if b, ok := configBoolWords[strings.ToLower(strings.TrimSpace(text))]; ok {
		return b, nil
	}
	return false, fmt.Errorf("invalid bool %q", text)
}

// providerConfigDisplayLabel is how a Provider value reads in menus and
// messages: the providers it stands for, in the order they are tried.
func providerConfigDisplayLabel(value string) string {
	const stackLabel = "Default with fallback"
	if !isStackedProviderConfig(value) {
		if names := parseProviderConfig(value); len(names) > 0 {
			return strings.Join(names, " → ")
		}
		return value
	}
	if stack := defaultEnabledProviderStack(); len(stack) > 0 {
		return stackLabel + " (" + strings.Join(stack, " → ") + ")"
	}
	return stackLabel
}

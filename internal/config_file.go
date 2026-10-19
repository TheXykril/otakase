package internal

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// LoadConfig reads the config file, creating it with the defaults when there
// is none. Settings the file leaves out take their defaults in memory; they
// are written to the file only by MigrateOnVersionUpgrade, so an ordinary
// start does not rewrite it.
func LoadConfig(configPath string) (Config, error) {
	configPath = os.ExpandEnv(configPath)
	ConfigFilePath = configPath

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		Out("Config file not found. Creating default config...")
		if err := createDefaultConfig(configPath); err != nil {
			return Config{}, fmt.Errorf("creating the config file: %w", err)
		}
	}

	onDisk, err := LoadConfigFromFile(configPath)
	if err != nil {
		return Config{}, fmt.Errorf("reading the config file: %w", err)
	}
	WarnAboutUnknownConfigKeys(onDisk)

	if upgradeStoredValues(onDisk) && configBool(onDisk, "AddMissingOptions", true) {
		if err := SaveConfigToFile(configPath, onDisk); err != nil {
			return Config{}, fmt.Errorf("saving the config file: %w", err)
		}
	}

	values := defaultConfigMap()
	for key, value := range onDisk {
		values[key] = value
	}
	config := PopulateConfig(values)
	normalizeTrackingConfig(&config)
	return config, nil
}

// retiredMyAnimeListClientID is the MyAnimeList client older configs were
// written with. Its tokens can't be refreshed by the current client, so the
// value is swapped for the default and the user signs in again.
const retiredMyAnimeListClientID = "f76d4c827152327b60654285d221222c"

// upgradeStoredValues rewrites settings saved in an old form: a provider
// under an old spelling and the retired MyAnimeList client. It reports
// whether anything changed.
func upgradeStoredValues(onDisk map[string]string) bool {
	changed := false
	if value, ok := onDisk["Provider"]; ok {
		if canonical := canonicalProviderConfigValue(value); canonical != value {
			onDisk["Provider"] = canonical
			changed = true
		}
	}
	if strings.TrimSpace(onDisk["MyAnimeListClientID"]) == retiredMyAnimeListClientID {
		onDisk["MyAnimeListClientID"] = defaultConfigMap()["MyAnimeListClientID"]
		changed = true
	}
	return changed
}

// configBool reads a boolean setting from a raw map, or fallback when it is
// missing or not a boolean.
func configBool(values map[string]string, key string, fallback bool) bool {
	if parsed, err := strconv.ParseBool(values[key]); err == nil {
		return parsed
	}
	return fallback
}

func createDefaultConfig(path string) error {
	return SaveConfigToFile(path, defaultConfigMap())
}

// LoadConfigFromFile reads a key=value file. Blank lines and lines starting
// with # are skipped; spaces around keys and values are trimmed.
func LoadConfigFromFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		if key, value, ok := strings.Cut(line, "="); ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return values, scanner.Err()
}

// SaveConfigToFile writes the settings sorted by key. The file is replaced in
// one rename, so a crash or a second running copy never leaves it half
// written.
func SaveConfigToFile(path string, values map[string]string) error {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	for _, key := range keys {
		buf.WriteString(key + "=" + values[key] + "\n")
	}
	return writeFileAtomic(path, buf.Bytes(), 0o644)
}

// PopulateConfig builds a Config from raw values. A missing, empty or
// unreadable value takes the setting's default.
func PopulateConfig(values map[string]string) Config {
	var config Config
	defaults := defaultConfigMap()
	target := reflect.ValueOf(&config).Elem()

	for i := 0; i < target.NumField(); i++ {
		key := target.Type().Field(i).Tag.Get("config")
		if key == "" || key == "-" {
			continue
		}
		field := target.Field(i)
		raw := values[key]
		if raw == "" {
			raw = defaults[key]
		}
		switch field.Kind() {
		case reflect.String:
			field.SetString(raw)
		case reflect.Int:
			n, err := strconv.Atoi(raw)
			if err != nil {
				n, _ = strconv.Atoi(defaults[key])
			}
			field.SetInt(int64(n))
		case reflect.Bool:
			b, err := strconv.ParseBool(raw)
			if err != nil {
				b, _ = strconv.ParseBool(defaults[key])
			}
			field.SetBool(b)
		case reflect.Slice:
			field.Set(reflect.ValueOf(parseStringArray(raw)))
		}
	}

	config.PercentageToMarkComplete = clamp(config.PercentageToMarkComplete, 0, 100)
	if config.MpvPlaybackStartTimeout <= 0 {
		config.MpvPlaybackStartTimeout = MpvStartWaitSeconds
	}
	config.MpvPlaybackStartTimeout = min(config.MpvPlaybackStartTimeout, maxMpvPlaybackStartTimeout)
	return config
}

func clamp(n, lo, hi int) int {
	return max(lo, min(n, hi))
}

// parseStringArray reads a list setting. It takes a JSON array, or the
// looser [a, "b", c] form people write by hand. Empty items are dropped.
func parseStringArray(value string) []string {
	value = strings.TrimSpace(value)
	var items []string
	if err := json.Unmarshal([]byte(value), &items); err != nil {
		inner := strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
		items = strings.Split(inner, ",")
	}
	var out []string
	for _, item := range items {
		if item = strings.Trim(strings.TrimSpace(item), `"`); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// persistSubStylePreference saves a "soft" or "hard" subtitle choice made in a
// prompt, so the question is not asked again.
func persistSubStylePreference(style string) error {
	style = strings.ToLower(strings.TrimSpace(style))
	if style != "soft" && style != "hard" {
		return nil
	}
	if config := GetGlobalConfig(); config != nil {
		config.SubStyle = style
	}
	if strings.TrimSpace(ConfigFilePath) == "" {
		return nil
	}
	values, err := LoadConfigFromFile(ConfigFilePath)
	if err != nil {
		return err
	}
	values["SubStyle"] = style
	return SaveConfigToFile(ConfigFilePath, values)
}

package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// The config file holds the user's tracker credentials and every preference, and
// otakase rewrites it on most runs (AddMissingOptions defaults to true). It was
// being written with os.Create -- truncate in place, then write -- with no lock
// and no atomic rename, so a crash or two concurrent runs can leave a mangled
// file. A real config in the wild carried "nimeListClientID", which is
// "MyAnimeListClientID" with its first three characters gone.
//
// Unrecognised keys were also kept forever without comment, so a typo such as
// SkipOP=true silently did nothing.

// KnownConfigKeys returns every key the config struct understands.
func KnownConfigKeys() []string {
	configType := reflect.TypeOf(Config{})
	keys := make([]string, 0, configType.NumField())
	for i := 0; i < configType.NumField(); i++ {
		if tag := configType.Field(i).Tag.Get("config"); tag != "" {
			keys = append(keys, tag)
		}
	}
	sort.Strings(keys)
	return keys
}

// closestKnownKey finds the known key a typo most likely meant, or "" when
// nothing is close enough to be worth suggesting.
func closestKnownKey(key string) string {
	best, bestDistance := "", -1
	limit := len(key)/3 + 1

	for _, known := range KnownConfigKeys() {
		// An exact case-insensitive match is almost always the intended key.
		if strings.EqualFold(known, key) {
			return known
		}
		distance := editDistance(strings.ToLower(key), strings.ToLower(known))
		if distance <= limit && (bestDistance == -1 || distance < bestDistance) {
			best, bestDistance = known, distance
		}
	}
	return best
}

// editDistance is the Levenshtein distance between two strings.
func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}

	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(min(current[j-1]+1, previous[j]+1), previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}

// UnknownConfigKeys returns the keys in a config map that otakase does not
// understand, sorted, each paired with the closest known key when there is one.
func UnknownConfigKeys(configMap map[string]string) map[string]string {
	known := make(map[string]struct{}, 64)
	for _, key := range KnownConfigKeys() {
		known[key] = struct{}{}
	}

	unknown := make(map[string]string)
	for key := range configMap {
		if _, ok := known[key]; ok {
			continue
		}
		unknown[key] = closestKnownKey(key)
	}
	return unknown
}

// WarnAboutUnknownConfigKeys tells the user about settings otakase ignores, so a
// typo is not silently dropped. Keys are never removed: an unrecognised key may
// belong to a newer otakase the user also runs.
func WarnAboutUnknownConfigKeys(configMap map[string]string) {
	unknown := UnknownConfigKeys(configMap)
	if len(unknown) == 0 {
		return
	}

	keys := make([]string, 0, len(unknown))
	for key := range unknown {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		message := fmt.Sprintf("Ignoring unknown config option %q", key)
		if suggestion := unknown[key]; suggestion != "" {
			message += fmt.Sprintf(" (did you mean %q?)", suggestion)
		}
		Out(message)
		Log(message)
	}
}

// writeFileAtomic writes data to path via a temporary file in the same
// directory, then renames it into place. rename(2) is atomic within a
// filesystem, so a reader either sees the old file or the new one, never a
// half-written mixture.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	// The temp file must share a filesystem with the destination or the rename
	// would fail across devices.
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary config file: %w", err)
	}
	tmpName := tmp.Name()

	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write temporary config file: %w", err)
	}
	// Flush to disk before the rename, so a power loss cannot leave the renamed
	// file present but empty.
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync temporary config file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temporary config file: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("set config file permissions: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("replace config file: %w", err)
	}
	return nil
}

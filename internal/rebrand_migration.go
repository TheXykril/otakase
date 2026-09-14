package internal

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	// legacyAppName is what this program was called before the rebrand. It
	// survives only so an existing install can be found and carried over.
	legacyAppName = "curd"
	// AppName is the current name, used for the config and storage directories.
	AppName = "otakase"
	// DisplayName is AppName as it appears in notifications and window titles.
	DisplayName = "Otakase"
)

// LegacyStoragePathDefault is the storage directory curd shipped as its default.
// Only a config still pointing at exactly this is repointed at the new default;
// anything the user chose themselves is left alone.
const (
	LegacyStoragePathDefault = "$HOME/.local/share/" + legacyAppName
	StoragePathDefault       = "$HOME/.local/share/" + AppName
)

// ConfigFileName is the config file inside the config directory.
func ConfigFileName() string { return AppName + ".conf" }

// MigrateLegacyAppDirs carries a curd-era install over to the otakase
// locations the first time otakase runs.
//
// The rename changes where the program looks for its config and its storage,
// and those directories hold the AniList and MyAnimeList tokens plus the local
// watch history. Without this the first launch after an update looks like a
// factory reset: logged out of both trackers, no history, no remembered
// providers -- recoverable, but alarming, and the data is still sitting on disk
// under the old name.
//
// The old directories are copied rather than moved, so a curd binary that is
// still installed keeps working. It reports what it did so the caller can tell
// the user where the originals are.
func MigrateLegacyAppDirs(configDir, homeDir string) (notes []string, err error) {
	legacyConfigDir := filepath.Join(configDir, legacyAppName)
	newConfigDir := filepath.Join(configDir, AppName)

	// Never touch an install that already exists under the new name.
	if _, statErr := os.Stat(newConfigDir); statErr == nil {
		return nil, nil
	} else if !os.IsNotExist(statErr) {
		return nil, statErr
	}
	if _, statErr := os.Stat(legacyConfigDir); statErr != nil {
		// Nothing to carry over: a fresh install, which is not an error.
		return nil, nil
	}

	if err := copyTree(legacyConfigDir, newConfigDir); err != nil {
		return nil, fmt.Errorf("copying %s to %s: %w", legacyConfigDir, newConfigDir, err)
	}
	notes = append(notes, fmt.Sprintf("config copied from %s", legacyConfigDir))

	// The config file is named after the program, so the copy arrives under the
	// old name and has to be renamed before anything looks for it.
	legacyConfigFile := filepath.Join(newConfigDir, legacyAppName+".conf")
	newConfigFile := filepath.Join(newConfigDir, ConfigFileName())
	if _, statErr := os.Stat(legacyConfigFile); statErr == nil {
		if err := os.Rename(legacyConfigFile, newConfigFile); err != nil {
			return notes, fmt.Errorf("renaming the copied config: %w", err)
		}
	}

	// Storage holds the watch history and the token cache. A config that still
	// names curd's default would keep reading the old directory forever, so the
	// directory is copied and the setting repointed -- but only when it is the
	// stock value, since a path the user chose is theirs to keep.
	legacyStorage := filepath.Join(homeDir, ".local", "share", legacyAppName)
	newStorage := filepath.Join(homeDir, ".local", "share", AppName)
	if _, statErr := os.Stat(legacyStorage); statErr == nil {
		if _, statErr := os.Stat(newStorage); os.IsNotExist(statErr) {
			if err := copyTree(legacyStorage, newStorage); err != nil {
				return notes, fmt.Errorf("copying %s to %s: %w", legacyStorage, newStorage, err)
			}
			notes = append(notes, fmt.Sprintf("history and tokens copied from %s", legacyStorage))
		}
	}
	if err := repointLegacyStoragePath(newConfigFile, homeDir); err != nil {
		return notes, err
	}

	notes = append(notes, fmt.Sprintf("the originals are untouched and can be removed once %s looks right", AppName))
	return notes, nil
}

// repointLegacyStoragePath rewrites a StoragePath that still names curd's
// default. Both the literal $HOME form and an already-expanded one are handled,
// because the config is written back out with whatever form it was read in.
func repointLegacyStoragePath(configFile, homeDir string) error {
	raw, err := os.ReadFile(configFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	expandedLegacy := filepath.Join(homeDir, ".local", "share", legacyAppName)
	expandedNew := filepath.Join(homeDir, ".local", "share", AppName)

	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		key, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(key) != "StoragePath" {
			continue
		}
		switch strings.TrimSpace(value) {
		case LegacyStoragePathDefault:
			lines[i] = strings.TrimSpace(key) + "=" + StoragePathDefault
		case expandedLegacy:
			lines[i] = strings.TrimSpace(key) + "=" + expandedNew
		}
	}
	return os.WriteFile(configFile, []byte(strings.Join(lines, "\n")), 0o600)
}

// copyTree copies a directory recursively, preserving file modes. Symlinks are
// skipped rather than followed, so a link pointing outside the tree cannot make
// the copy escape it.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case info.IsDir():
			return os.MkdirAll(target, info.Mode().Perm())
		case info.Mode()&os.ModeSymlink != 0:
			return nil
		case !info.Mode().IsRegular():
			return nil
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The rename moves where the program looks for its config and storage. Both
// hold things a user cannot regenerate by hand -- the AniList and MyAnimeList
// tokens, and the local watch history -- so a first launch after the rename
// must find them, not present an empty install.
func TestRenameCarriesOverAPreviousInstall(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config")

	legacyConfig := filepath.Join(configDir, "curd")
	legacyStorage := filepath.Join(home, ".local", "share", "curd")
	mustWrite(t, filepath.Join(legacyConfig, "curd.conf"),
		"Player=mpv\nStoragePath=$HOME/.local/share/curd\nSubOrDub=dub\n")
	mustWrite(t, filepath.Join(legacyStorage, "token"), "anilist-token")
	mustWrite(t, filepath.Join(legacyStorage, "history", "curd_history.txt"), "201514\t10\n")

	notes, err := MigrateLegacyAppDirs(configDir, home)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	if len(notes) == 0 {
		t.Error("the migration said nothing; the user gets no hint their data moved")
	}

	// The config has to arrive under the new name, or nothing will find it.
	newConfig := filepath.Join(configDir, "otakase", "otakase.conf")
	body, err := os.ReadFile(newConfig)
	if err != nil {
		t.Fatalf("config was not carried over: %v", err)
	}
	if got := string(body); !strings.Contains(got, "SubOrDub=dub") {
		t.Errorf("settings were lost in the copy: %q", got)
	}
	// A StoragePath still naming curd would read the old directory forever.
	if got := string(body); !strings.Contains(got, "StoragePath=$HOME/.local/share/otakase") {
		t.Errorf("StoragePath still points at the old directory: %q", got)
	}

	// The tokens and history are the part that cannot be typed back in.
	if _, err := os.Stat(filepath.Join(home, ".local", "share", "otakase", "token")); err != nil {
		t.Errorf("the tracker token did not come across: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "share", "otakase", "history", "curd_history.txt")); err != nil {
		t.Errorf("watch history did not come across: %v", err)
	}

	// Copied, not moved: a curd binary that is still installed must keep working.
	if _, err := os.Stat(filepath.Join(legacyConfig, "curd.conf")); err != nil {
		t.Errorf("the original install was destroyed: %v", err)
	}
}

// A path the user chose is theirs; only the stock default gets repointed.
func TestRenameLeavesACustomStoragePathAlone(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config")
	mustWrite(t, filepath.Join(configDir, "curd", "curd.conf"),
		"StoragePath=/mnt/media/anime\n")

	if _, err := MigrateLegacyAppDirs(configDir, home); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(configDir, "otakase", "otakase.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "StoragePath=/mnt/media/anime") {
		t.Errorf("a storage path the user chose was rewritten: %q", string(body))
	}
}

// Running again must not overwrite a live install with a stale copy.
func TestRenameDoesNotClobberAnExistingInstall(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config")
	mustWrite(t, filepath.Join(configDir, "curd", "curd.conf"), "SubOrDub=sub\n")
	mustWrite(t, filepath.Join(configDir, "otakase", "otakase.conf"), "SubOrDub=dub\n")

	notes, err := MigrateLegacyAppDirs(configDir, home)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("it migrated over a real install: %v", notes)
	}
	body, _ := os.ReadFile(filepath.Join(configDir, "otakase", "otakase.conf"))
	if !strings.Contains(string(body), "SubOrDub=dub") {
		t.Errorf("a live config was overwritten by the old one: %q", string(body))
	}
}

// A fresh machine has nothing to carry over, which is not a failure.
func TestRenameOnAFreshInstallIsQuiet(t *testing.T) {
	home := t.TempDir()
	notes, err := MigrateLegacyAppDirs(filepath.Join(home, ".config"), home)
	if err != nil {
		t.Fatalf("a fresh install reported an error: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("a fresh install printed migration notes: %v", notes)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

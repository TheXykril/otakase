package internal

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	// AppName names the config and storage directories.
	AppName = "otakase"
	// DisplayName is AppName as notifications and window titles show it.
	DisplayName = "Otakase"

	StoragePathDefault = "$HOME/.local/share/" + AppName

	historyFileName   = "history.txt"
	lastAnimeFileName = "last_anime"
	versionFileName   = "version"
)

// ConfigFileName is the config file inside the config directory.
func ConfigFileName() string { return AppName + ".conf" }

func storageDir(storagePath string) string {
	return os.ExpandEnv(strings.TrimSpace(storagePath))
}

// HistoryFile is the local watch history inside the storage directory.
func HistoryFile(storagePath string) string {
	return filepath.Join(storageDir(storagePath), historyFileName)
}

// LastAnimeFile holds the AniList id of the show played last.
func LastAnimeFile(storagePath string) string {
	return filepath.Join(storageDir(storagePath), lastAnimeFileName)
}

func storageVersionFilePath(storagePath string) string {
	return filepath.Join(storageDir(storagePath), versionFileName)
}

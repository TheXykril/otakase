package internal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/icons"
)

// Settings backup: Community → "Back up settings to Discord" keeps a copy
// of the config with the Otakase Discord bot, for the linked member rather
// than this app, and "Restore settings from Discord" brings it back, on
// this computer or a new one once it is linked. Passwords, secrets, tokens,
// keys and client ids never leave the computer, nor does StoragePath (it
// says where this computer keeps its files); restoring keeps this
// computer's own values for them.
//
//	POST /v1/backup {"config":"Key=value\n…"} → {"at"}
//	GET  /v1/backup → {"config","at","from_os"}, 404 when there is none

const maxBackupBytes = 64 << 10

// backupSecretKeyRe matches the options that hold something private.
var backupSecretKeyRe = regexp.MustCompile(`(?i)(secret|password|passwd|token|apikey|api_key|clientid|client_id)`)

// backupLocalKeys are options that only make sense on this computer.
var backupLocalKeys = map[string]bool{"StoragePath": true}

// backupKeyKept reports whether an option goes into a backup and comes back
// from one.
func backupKeyKept(key string) bool {
	return key != "" && !backupSecretKeyRe.MatchString(key) && !backupLocalKeys[key]
}

// backupText is the config file as it is backed up: its key=value lines
// without the private and local ones.
func backupText(raw string) string {
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		key, _, ok := strings.Cut(line, "=")
		if !ok || strings.HasPrefix(line, "#") || !backupKeyKept(strings.TrimSpace(key)) {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// mergeBackup lays a backup over the current options and reports how many
// it set. Private and local options keep their current values.
func mergeBackup(current map[string]string, backup string) (map[string]string, int) {
	merged := make(map[string]string, len(current))
	for k, v := range current {
		merged[k] = v
	}
	n := 0
	for _, line := range strings.Split(backup, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || strings.HasPrefix(key, "#") || !backupKeyKept(key) {
			continue
		}
		merged[key] = value
		n++
	}
	return merged, n
}

type settingsBackup struct {
	Config string    `json:"config"`
	At     time.Time `json:"at"`
	FromOS string    `json:"from_os"`
}

var errNoSettingsBackup = errors.New("no settings backup yet")

func saveSettingsBackup(config *Config) (int, error) {
	raw, err := os.ReadFile(ConfigFilePath)
	if err != nil {
		return 0, err
	}
	text := backupText(string(raw))
	if len(text) > maxBackupBytes {
		return 0, errors.New("the config is too large to back up (64 KB at most)")
	}
	if text == "" {
		return 0, errors.New("there are no settings to back up")
	}
	ctx, cancel := context.WithTimeout(context.Background(), discordRequestTimeout)
	defer cancel()
	if _, err := discordPostJSON(ctx, config, "/v1/backup", loadDiscordLink(config).Token, map[string]string{"config": text}, nil); err != nil {
		return 0, err
	}
	return strings.Count(text, "\n") + 1, nil
}

func fetchSettingsBackup(config *Config) (settingsBackup, error) {
	var bk settingsBackup
	ctx, cancel := context.WithTimeout(context.Background(), discordRequestTimeout)
	defer cancel()
	status, err := discordDo(ctx, config, http.MethodGet, "/v1/backup", loadDiscordLink(config).Token, "", nil, &bk)
	if status == http.StatusNotFound {
		return bk, errNoSettingsBackup
	}
	return bk, err
}

// restoreSettingsBackup writes a backup over the config, keeping the config
// as it was beside it, and reports how many options it set.
func restoreSettingsBackup(bk settingsBackup) (int, string, error) {
	path := ConfigFilePath
	if strings.TrimSpace(path) == "" {
		return 0, "", errors.New("no config file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, "", err
	}
	keep := path + ".before-restore"
	if err := os.WriteFile(keep, raw, 0o600); err != nil {
		return 0, "", fmt.Errorf("keeping a copy of the current settings: %w", err)
	}
	current, err := LoadConfigFromFile(path)
	if err != nil {
		return 0, "", err
	}
	merged, n := mergeBackup(current, bk.Config)
	if n == 0 {
		return 0, keep, errors.New("the backup has no settings in it")
	}
	return n, keep, SaveConfigToFile(path, merged)
}

func backupFromMenu(config *Config) {
	Out("Backing up your settings to the Otakase Discord…")
	n, err := saveSettingsBackup(config)
	if err != nil {
		Log(fmt.Sprintf("Settings backup: %v", err))
		Out("Could not back up your settings: " + err.Error())
	} else {
		Out(fmt.Sprintf("Backed up %d settings. Passwords, tokens and keys were left out. Restore them on any linked computer from this menu.", n))
	}
	pauseAfterNotice(config)
}

func restoreFromMenu(config *Config) {
	bk, err := fetchSettingsBackup(config)
	switch {
	case errors.Is(err, errNoSettingsBackup):
		Out("There is no settings backup yet. Use Back up settings to Discord on the computer that has them.")
		pauseAfterNotice(config)
		return
	case err != nil:
		Log(fmt.Sprintf("Settings restore: %v", err))
		Out("Could not get your settings backup: " + err.Error())
		pauseAfterNotice(config)
		return
	}
	from := bk.FromOS
	if from == "" {
		from = "another computer"
	}
	message := fmt.Sprintf("Restore the settings backed up on %s from %s? Your current settings are kept beside them as %s.before-restore; passwords, tokens and keys stay as they are.",
		bk.At.Local().Format("2006-01-02 15:04"), from, ConfigFileName())
	selected, err := selectWithOptionalMessage(config, []SelectionOption{
		{Key: "restore", Label: "Restore the settings from " + bk.At.Local().Format("2006-01-02"), Icon: icons.Import},
		{Key: "cancel", Label: "Keep my current settings", Icon: icons.No},
	}, "Restore", message)
	if err != nil {
		Log(fmt.Sprintf("Settings restore: %v", err))
		return
	}
	selected = NormalizeSelectionKey(selected)
	if SelectionMeansQuit(selected) {
		Exit(nil)
	}
	if selected.Key != "restore" {
		return
	}
	n, keep, err := restoreSettingsBackup(bk)
	if err != nil {
		Log(fmt.Sprintf("Settings restore: %v", err))
		Out("Could not restore your settings: " + err.Error())
	} else {
		Out(fmt.Sprintf("Restored %d settings (the old ones are in %s). Restart Otakase to use them.", n, keep))
	}
	pauseAfterNotice(config)
}

func init() {
	addCommunityRow(communityRow{
		options: func(config *Config, linked bool) []SelectionOption {
			if !linked {
				return nil
			}
			return []SelectionOption{
				{Key: "backup:save", Label: "Back up settings to Discord · passwords and keys left out", Icon: icons.Tracker},
				{Key: "backup:restore", Label: "Restore settings from Discord", Icon: icons.Import},
			}
		},
		handle: func(config *Config, key string) (bool, bool) {
			switch key {
			case "backup:save":
				EnterAppScreen()
				backupFromMenu(config)
			case "backup:restore":
				EnterAppScreen()
				restoreFromMenu(config)
			default:
				return false, false
			}
			return true, false
		},
	})
}

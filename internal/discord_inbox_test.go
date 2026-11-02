package internal

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// stubNotifications records desktop notifications instead of sending them.
func stubNotifications(t *testing.T) *[]string {
	t.Helper()
	var mu sync.Mutex
	var sent []string
	previous := notifySendRun
	notifySendRun = func(args []string) (string, error) {
		mu.Lock()
		sent = append(sent, args[len(args)-1])
		mu.Unlock()
		return "", nil
	}
	t.Cleanup(func() { notifySendRun = previous })
	return &sent
}

func TestKeptInboxMovesARequeuedShowUp(t *testing.T) {
	sent := stubNotifications(t)
	config := newDiscordTestConfig(t, "http://bot")
	keepInboxItem(config, discordInboxItem{ID: "1", Kind: "queue", AnilistID: 5, Title: "Frieren"})
	keepInboxItem(config, discordInboxItem{ID: "2", Kind: "notice", Title: "Hi"})
	keepInboxItem(config, discordInboxItem{ID: "3", Kind: "queue", AnilistID: 5, Title: "Frieren", From: "Mika"})
	kept := loadKeptInbox(config)
	if len(kept) != 2 || kept[0].ID != "3" || kept[1].ID != "2" {
		t.Errorf("kept %+v", kept)
	}
	if len(*sent) != 3 || !strings.Contains((*sent)[2], "Mika recommends Frieren") {
		t.Errorf("notifications %q", *sent)
	}
	if label := inboxRowLabel(config); label != "From Discord · 2" {
		t.Errorf("row %q", label)
	}
	if o := inboxOption(kept[0]); o.Key != "item:3" || o.Label != "Play Frieren · from Mika" {
		t.Errorf("option %+v", o)
	}
}

func TestDiscordSettingsOnlyAllowed(t *testing.T) {
	stubNotifications(t)
	dir := t.TempDir()
	previous := ConfigFilePath
	ConfigFilePath = filepath.Join(dir, "otakase.conf")
	t.Cleanup(func() { ConfigFilePath = previous })
	if err := SaveConfigToFile(ConfigFilePath, map[string]string{"DevBuilds": "false", "DiscordBotURL": "http://bot"}); err != nil {
		t.Fatal(err)
	}
	config := &Config{StoragePath: dir, DiscordBotURL: "http://bot"}
	applyDiscordSetting(config, discordInboxItem{Kind: "setting", Key: "DiscordBotURL", Value: "https://evil"})
	applyDiscordSetting(config, discordInboxItem{Kind: "setting", Key: "DevBuilds", Value: "true", Text: "Tester"})
	saved, err := LoadConfigFromFile(ConfigFilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !config.DevBuilds || saved["DevBuilds"] != "true" || saved["DiscordBotURL"] != "http://bot" || config.DiscordBotURL != "http://bot" {
		t.Errorf("config %+v saved %v", config.DevBuilds, saved)
	}
}

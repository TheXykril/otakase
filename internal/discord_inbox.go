package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/thexykril/otakase/internal/icons"
)

// The inbox is the way back from the Otakase Discord bot to a linked app:
// a show queued from a bot card, a reply to a support post, a setting the
// member changed in Discord. The app asks for it in the background for as
// long as it runs (one request held open by the bot until something
// arrives), handles each item by its kind and acks it. Shows and notices are
// kept on disk for the Community menu's From Discord list.

const (
	discordInboxFileName = "discord_inbox.json"
	discordInboxWait     = 25 // seconds the bot holds the request open
	maxKeptInboxItems    = 50
)

// discordInboxClient waits longer than the bot holds the request.
var discordInboxClient = &http.Client{Timeout: (discordInboxWait + 15) * time.Second}

// discordInboxItem is one message from the bot.
type discordInboxItem struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	At        time.Time `json:"at"`
	Expires   time.Time `json:"expires,omitzero"`
	Title     string    `json:"title,omitempty"`
	Text      string    `json:"text,omitempty"`
	URL       string    `json:"url,omitempty"`
	AnilistID int       `json:"anilist_id,omitempty"`
	Episode   int       `json:"episode,omitempty"`
	From      string    `json:"from,omitempty"`
	Key       string    `json:"key,omitempty"`
	Value     string    `json:"value,omitempty"`
}

// discordInboxHandlers handle items by kind. Features register theirs from
// init(); an item of a kind nobody handles is logged and dropped.
var discordInboxHandlers = map[string]func(config *Config, item discordInboxItem){
	"queue":   keepInboxItem,
	"notice":  keepInboxItem,
	"setting": applyDiscordSetting,
}

// registerDiscordInbox handles items of kind.
func registerDiscordInbox(kind string, fn func(config *Config, item discordInboxItem)) {
	discordInboxHandlers[kind] = fn
}

var (
	discordInboxOnce sync.Once
	// discordInboxRetry is the first wait after a failed request, doubled
	// up to discordInboxRetryMax. Tests shorten them.
	discordInboxRetry    = 30 * time.Second
	discordInboxRetryMax = 10 * time.Minute
	discordInboxIdle     = 2 * time.Minute // between checks while not linked
)

// StartDiscordInbox starts the background inbox reader, once per run.
func StartDiscordInbox(config *Config) {
	if !discordEnabled(config) {
		return
	}
	discordInboxOnce.Do(func() { go runDiscordInbox(config) })
}

func runDiscordInbox(config *Config) {
	wait := discordInboxRetry
	for {
		if !discordLinked(config) {
			time.Sleep(discordInboxIdle)
			continue
		}
		items, err := fetchDiscordInbox(config, discordInboxWait)
		if err != nil {
			if !errors.Is(err, errDiscordUnauthorized) {
				Log(fmt.Sprintf("Discord: inbox: %v", err))
				time.Sleep(wait)
				wait = min(wait*2, discordInboxRetryMax)
			}
			continue
		}
		wait = discordInboxRetry
		handleDiscordInbox(config, items)
	}
}

func fetchDiscordInbox(config *Config, waitSeconds int) ([]discordInboxItem, error) {
	link := loadDiscordLink(config)
	if link.Token == "" {
		return nil, errDiscordUnauthorized
	}
	var answer struct {
		Items []discordInboxItem `json:"items"`
	}
	path := fmt.Sprintf("/v1/inbox?wait=%d", waitSeconds)
	if _, err := discordDoWith(discordInboxClient, context.Background(), config, http.MethodGet, path, link.Token, "", nil, &answer); err != nil {
		return nil, err
	}
	return answer.Items, nil
}

// handleDiscordInbox runs each item's handler and acks them all, the ones
// nobody understood too: the bot would otherwise send them again at once.
func handleDiscordInbox(config *Config, items []discordInboxItem) {
	if len(items) == 0 {
		return
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
		if !item.Expires.IsZero() && time.Now().After(item.Expires) {
			continue
		}
		if fn, ok := discordInboxHandlers[item.Kind]; ok {
			fn(config, item)
		} else {
			Log(fmt.Sprintf("Discord: inbox item of unknown kind %q dropped (an update may know it)", item.Kind))
		}
	}
	link := loadDiscordLink(config)
	if link.Token == "" {
		return
	}
	if _, err := discordPostJSON(context.Background(), config, "/v1/inbox/ack", link.Token, map[string][]string{"ids": ids}, nil); err != nil {
		Log(fmt.Sprintf("Discord: inbox ack failed: %v", err))
	}
}

// Kept items: the From Discord list.

var discordInboxFileMu sync.Mutex

func discordInboxPath(config *Config) string {
	return filepath.Join(filepath.Dir(discordLinkPath(config)), discordInboxFileName)
}

func loadKeptInbox(config *Config) []discordInboxItem {
	data, err := os.ReadFile(discordInboxPath(config))
	if err != nil {
		return nil
	}
	var items []discordInboxItem
	if err := json.Unmarshal(data, &items); err != nil {
		Log(fmt.Sprintf("Discord: ignoring an unreadable %s: %v", discordInboxFileName, err))
		return nil
	}
	return items
}

func saveKeptInbox(config *Config, items []discordInboxItem) {
	path := discordInboxPath(config)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		Log(fmt.Sprintf("Discord: saving the inbox: %v", err))
		return
	}
	data, _ := json.MarshalIndent(items, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		Log(fmt.Sprintf("Discord: saving the inbox: %v", err))
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		Log(fmt.Sprintf("Discord: saving the inbox: %v", err))
	}
}

// changeKeptInbox edits the kept items under the file lock.
func changeKeptInbox(config *Config, fn func(items []discordInboxItem) []discordInboxItem) {
	discordInboxFileMu.Lock()
	defer discordInboxFileMu.Unlock()
	saveKeptInbox(config, fn(loadKeptInbox(config)))
}

// keepInboxItem keeps a queued show or a notice for the From Discord list
// and says so on the desktop. A show already queued moves to the top.
func keepInboxItem(config *Config, item discordInboxItem) {
	changeKeptInbox(config, func(items []discordInboxItem) []discordInboxItem {
		kept := items[:0:0]
		for _, x := range items {
			if item.Kind == "queue" && x.Kind == "queue" && x.AnilistID == item.AnilistID {
				continue
			}
			kept = append(kept, x)
		}
		kept = append([]discordInboxItem{item}, kept...)
		if len(kept) > maxKeptInboxItems {
			kept = kept[:maxKeptInboxItems]
		}
		return kept
	})
	notifyDesktop(inboxNotification(item))
}

// inboxNotification is the desktop line for a kept item.
func inboxNotification(item discordInboxItem) string {
	switch item.Kind {
	case "queue":
		if item.From != "" {
			return fmt.Sprintf("%s recommends %s · Community → From Discord", item.From, item.Title)
		}
		return fmt.Sprintf("Queued from Discord: %s · Community → From Discord", item.Title)
	}
	text := strings.TrimSpace(item.Title)
	if item.Text != "" {
		if text != "" {
			text += ": "
		}
		text += item.Text
	}
	return "Discord · " + text
}

// discordRemoteSettings are the options the bot may change, each for a
// reason the member gave in Discord (the Tester role turns on dev builds).
var discordRemoteSettings = map[string]bool{"DevBuilds": true}

// applyDiscordSetting changes an allowed option and writes it to the config.
func applyDiscordSetting(config *Config, item discordInboxItem) {
	if !discordRemoteSettings[item.Key] {
		Log(fmt.Sprintf("Discord: refused to change %q from Discord", item.Key))
		return
	}
	on := item.Value == "true"
	switch item.Key {
	case "DevBuilds":
		if config.DevBuilds == on {
			return
		}
		config.DevBuilds = on
	}
	saveConfigValue(config, item.Key, fmt.Sprintf("%t", on))
	if item.Text != "" {
		notifyDesktop(item.Text)
	}
}

// saveConfigValue writes one option to the config file.
func saveConfigValue(config *Config, key, value string) {
	if strings.TrimSpace(ConfigFilePath) == "" {
		return
	}
	configMap, err := LoadConfigFromFile(ConfigFilePath)
	if err != nil {
		Log(fmt.Sprintf("Discord: could not read the config to save %s: %v", key, err))
		return
	}
	configMap[key] = value
	if err := SaveConfigToFile(ConfigFilePath, configMap); err != nil {
		Log(fmt.Sprintf("Discord: could not save %s: %v", key, err))
	}
}

// The From Discord menu.

// inboxRowLabel is the Community row for the kept items, "" when none.
func inboxRowLabel(config *Config) string {
	n := len(loadKeptInbox(config))
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("From Discord · %d", n)
}

// showDiscordInbox lists the kept items. Picking a show queues it to play
// (it reports true, and the caller leaves for it); picking a notice opens
// its link. Either way the item is done and leaves the list.
func showDiscordInbox(config *Config) bool {
	for {
		items := loadKeptInbox(config)
		if len(items) == 0 {
			return false
		}
		var options []SelectionOption
		for _, it := range items {
			options = append(options, inboxOption(it))
		}
		options = append(options, SelectionOption{Key: "clear", Label: "Clear all", Icon: icons.No})
		selected, err := promptSelectOrdered(options)
		if err != nil {
			Log(fmt.Sprintf("Discord: inbox menu: %v", err))
			return false
		}
		selected = NormalizeSelectionKey(selected)
		switch {
		case SelectionMeansQuit(selected):
			Exit(nil)
		case SelectionMeansBack(selected):
			return false
		}
		if selected.Key == "clear" {
			changeKeptInbox(config, func([]discordInboxItem) []discordInboxItem { return nil })
			return false
		}
		id, _ := strings.CutPrefix(selected.Key, "item:")
		var picked *discordInboxItem
		changeKeptInbox(config, func(items []discordInboxItem) []discordInboxItem {
			kept := items[:0:0]
			for _, it := range items {
				if it.ID == id {
					x := it
					picked = &x
					continue
				}
				kept = append(kept, it)
			}
			return kept
		})
		if picked == nil {
			continue
		}
		if picked.Kind == "queue" && picked.AnilistID > 0 {
			SetOpenLink(OpenLink{AnilistID: picked.AnilistID, Episode: picked.Episode})
			return true
		}
		if picked.URL != "" {
			if err := discordOpenURL(picked.URL); err != nil {
				Out("Could not open a browser: " + picked.URL)
			}
		}
		EnterAppScreen()
	}
}

func inboxOption(it discordInboxItem) SelectionOption {
	key := "item:" + it.ID
	if it.Kind == "queue" {
		label := "Play " + it.Title
		if it.Episode > 0 {
			label += fmt.Sprintf(" · episode %d", it.Episode)
		}
		if it.From != "" {
			label += " · from " + it.From
		}
		return SelectionOption{Key: key, Label: label, Icon: icons.Play}
	}
	label := strings.TrimSpace(it.Title)
	if it.Text != "" {
		if label != "" {
			label += " · "
		}
		label += it.Text
	}
	if !it.At.IsZero() {
		label += " · " + it.At.Local().Format("2 Jan")
	}
	icon := icons.Info
	if it.URL != "" {
		icon = icons.OpenLink
	}
	return SelectionOption{Key: key, Label: label, Icon: icon}
}

package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/thexykril/otakase/internal/icons"
)

// The Otakase Discord's server club: one show staff pick for everyone to
// watch up to an episode by a date. The Community menu shows it as "Server
// club: <title> · ep N by <date>", and picking it plays the show from where
// the viewer is up to. Fetched from GET /v1/club in the background like the
// news and kept on disk, so the menu never waits on the network.

const (
	discordClubFileName = "discord_club.json"
	discordClubRefresh  = 3 * time.Hour
	discordClubShowFor  = 24 * time.Hour // an older copy is not shown
)

type discordClub struct {
	AniListID int       `json:"anilist_id"`
	Title     string    `json:"title"`
	Episode   int       `json:"episode"`
	Deadline  time.Time `json:"deadline"`
	CaughtUp  int       `json:"caught_up"`
	Joined    int       `json:"joined"`
	URL       string    `json:"url,omitempty"`
}

type discordClubCache struct {
	Club      *discordClub `json:"club"`
	FetchedAt time.Time    `json:"fetched_at"`
}

// discordClubFetching keeps menu visits from starting a second fetch.
var discordClubFetching atomic.Bool

func init() {
	addCommunityRow(communityRow{options: discordClubOptions, handle: handleDiscordClub})
}

func discordClubPath(config *Config) string {
	return filepath.Join(filepath.Dir(discordLinkPath(config)), discordClubFileName)
}

func loadDiscordClub(config *Config) (discordClubCache, bool) {
	data, err := os.ReadFile(discordClubPath(config))
	if err != nil {
		return discordClubCache{}, false
	}
	var c discordClubCache
	if err := json.Unmarshal(data, &c); err != nil {
		return discordClubCache{}, false
	}
	return c, true
}

func saveDiscordClub(config *Config, c discordClubCache) error {
	path := discordClubPath(config)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// refreshDiscordClubIfStale fetches the club when the cached copy is older
// than discordClubRefresh. A failure keeps the old copy.
func refreshDiscordClubIfStale(config *Config) {
	if !discordEnabled(config) || !discordClubFetching.CompareAndSwap(false, true) {
		return
	}
	defer discordClubFetching.Store(false)
	if c, ok := loadDiscordClub(config); ok && time.Since(c.FetchedAt) < discordClubRefresh {
		return
	}
	var c discordClubCache
	if _, err := discordDo(context.Background(), config, http.MethodGet, "/v1/club", "", "", nil, &c); err != nil {
		Log(fmt.Sprintf("Discord: club fetch failed: %v", err))
		return
	}
	c.FetchedAt = time.Now().UTC()
	if err := saveDiscordClub(config, c); err != nil {
		Log(fmt.Sprintf("Discord: could not cache the club: %v", err))
	}
}

// cachedDiscordClub is the club to show: a recent copy, still before its
// deadline.
func cachedDiscordClub(config *Config) (discordClub, bool) {
	if !discordEnabled(config) {
		return discordClub{}, false
	}
	c, ok := loadDiscordClub(config)
	if !ok || c.Club == nil || c.Club.AniListID <= 0 || time.Since(c.FetchedAt) > discordClubShowFor || !time.Now().Before(c.Club.Deadline) {
		return discordClub{}, false
	}
	return *c.Club, true
}

// label is "Server club: Frieren · ep 12 by 21 Oct".
func (c discordClub) label() string {
	return fmt.Sprintf("Server club: %s · ep %d by %s", c.Title, c.Episode, c.Deadline.Local().Format("2 Jan"))
}

func discordClubOptions(config *Config, linked bool) []SelectionOption {
	c, ok := cachedDiscordClub(config)
	if !ok {
		return nil
	}
	return []SelectionOption{{Key: "club-play", Label: c.label(), Icon: icons.Play}}
}

func handleDiscordClub(config *Config, key string) (handled, play bool) {
	if key != "club-play" {
		return false, false
	}
	c, ok := cachedDiscordClub(config)
	if !ok {
		return true, false
	}
	SetOpenLink(OpenLink{AnilistID: c.AniListID})
	return true, true
}

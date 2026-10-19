package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/icons"
)

// The Otakase Discord's latest announcement and member counts, shown in the
// Community menu and the update prompt. They are fetched in the background at
// most every discordNewsRefresh and kept on disk, so showing them never waits
// on the network; a copy too old to trust is not shown at all.

const (
	discordNewsFileName = "discord_news.json"
	discordNewsRefresh  = 6 * time.Hour
	// discordNewsShowFor is how long a cached copy is shown for. Offline for
	// longer than this and the news is hidden rather than shown stale.
	discordNewsShowFor = 24 * time.Hour
)

type discordAnnouncement struct {
	Title string    `json:"title"`
	Text  string    `json:"text"`
	URL   string    `json:"url"`
	At    time.Time `json:"at"`
}

type discordNews struct {
	Announcement *discordAnnouncement `json:"announcement"`
	Online       int                  `json:"online"`
	Members      int                  `json:"members"`
	Invite       string               `json:"invite"`
	// ScoreSharing is false when the server turned score sharing off; nil
	// from a bot too old to say.
	ScoreSharing *bool     `json:"score_sharing"`
	FetchedAt    time.Time `json:"fetched_at"`
}

func discordNewsPath(config *Config) string {
	return filepath.Join(filepath.Dir(discordLinkPath(config)), discordNewsFileName)
}

func loadDiscordNews(config *Config) (discordNews, bool) {
	data, err := os.ReadFile(discordNewsPath(config))
	if err != nil {
		return discordNews{}, false
	}
	var news discordNews
	if err := json.Unmarshal(data, &news); err != nil {
		return discordNews{}, false
	}
	return news, true
}

func saveDiscordNews(config *Config, news discordNews) error {
	path := discordNewsPath(config)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(news, "", "  ")
	if err != nil {
		return err
	}
	// Renamed into place, so a menu reading it meanwhile never sees half.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func fetchDiscordNews(config *Config) (discordNews, error) {
	var news discordNews
	if _, err := discordDo(context.Background(), config, http.MethodGet, "/v1/news", "", "", nil, &news); err != nil {
		return discordNews{}, err
	}
	news.FetchedAt = time.Now().UTC()
	return news, nil
}

// refreshDiscordNewsIfStale fetches the news when the cached copy is older
// than discordNewsRefresh. A failure keeps the old copy, which ages out of
// view on its own.
func refreshDiscordNewsIfStale(config *Config) {
	if !discordEnabled(config) {
		return
	}
	if news, ok := loadDiscordNews(config); ok && time.Since(news.FetchedAt) < discordNewsRefresh {
		return
	}
	news, err := fetchDiscordNews(config)
	if err != nil {
		Log(fmt.Sprintf("Discord: news fetch failed: %v", err))
		return
	}
	if err := saveDiscordNews(config, news); err != nil {
		Log(fmt.Sprintf("Discord: could not cache the news: %v", err))
	}
}

// cachedDiscordNews is the news to show, if there is a recent enough copy.
func cachedDiscordNews(config *Config) (discordNews, bool) {
	if !discordEnabled(config) {
		return discordNews{}, false
	}
	news, ok := loadDiscordNews(config)
	if !ok || time.Since(news.FetchedAt) > discordNewsShowFor {
		return discordNews{}, false
	}
	return news, true
}

func (news discordNews) invite() string {
	if strings.HasPrefix(news.Invite, "https://") {
		return news.Invite
	}
	return DiscordInviteURL
}

// countsLine is "12 online · 85 members", or empty without counts.
func (news discordNews) countsLine() string {
	var parts []string
	if news.Online > 0 {
		parts = append(parts, fmt.Sprintf("%d online", news.Online))
	}
	if news.Members > 0 {
		parts = append(parts, fmt.Sprintf("%d members", news.Members))
	}
	return strings.Join(parts, " · ")
}

// headline is "Announcement title · 3 Oct 2026", or empty.
func (news discordNews) headline() string {
	a := news.Announcement
	if a == nil || strings.TrimSpace(a.Title) == "" {
		return ""
	}
	line := strings.TrimSpace(a.Title)
	if !a.At.IsZero() {
		line += " · " + a.At.Local().Format("2 Jan 2006")
	}
	return line
}

// discordUpdateOption is the update prompt's "Join Discord" row, when there
// is news to go with it.
func discordUpdateOption() (SelectionOption, bool) {
	news, ok := cachedDiscordNews(GetGlobalConfig())
	if !ok {
		return SelectionOption{}, false
	}
	label := "Join the Otakase Discord"
	if counts := news.countsLine(); counts != "" {
		label += " · " + counts
	}
	return SelectionOption{Key: "discord", Label: label, Icon: icons.OpenLink}, true
}

// discordNewsLine is the update prompt's line about the latest announcement.
func discordNewsLine() string {
	news, ok := cachedDiscordNews(GetGlobalConfig())
	if !ok {
		return ""
	}
	if headline := news.headline(); headline != "" {
		return "Discord: " + headline
	}
	return ""
}

// openDiscordInvite opens the server's invite in the browser.
func openDiscordInvite() error {
	url := DiscordInviteURL
	if news, ok := cachedDiscordNews(GetGlobalConfig()); ok {
		url = news.invite()
	}
	return discordOpenURL(url)
}

// communityOptions are the Community menu's rows for the current state.
func communityOptions(config *Config, news discordNews, haveNews bool) []SelectionOption {
	var options []SelectionOption
	if haveNews {
		if headline := news.headline(); headline != "" && news.Announcement.URL != "" {
			options = append(options, SelectionOption{Key: "news", Label: "News: " + headline, Icon: icons.Info})
		}
	}
	join := "Join the Otakase Discord"
	if haveNews {
		if counts := news.countsLine(); counts != "" {
			join += " · " + counts
		}
	}
	options = append(options, SelectionOption{Key: "join", Label: join, Icon: icons.OpenLink})

	link := loadDiscordLink(config)
	switch {
	case link.Token != "":
		options = append(options, SelectionOption{Key: "unlink", Label: "Unlink Discord · linked as " + discordNameOr(link), Icon: icons.No})
		share := SelectionOption{Key: "share", Label: "Share what I watch: Off · shown in /whos-watching", Icon: icons.No}
		if config.DiscordShareWatching {
			share = SelectionOption{Key: "share", Label: "Share what I watch: On · shown in /whos-watching", Icon: icons.Yes}
		}
		options = append(options, share)
		options = append(options, SelectionOption{Key: "party", Label: "Join a watch party · paste the party link", Icon: icons.Play})
	case link.Revoked:
		options = append(options, SelectionOption{Key: "link", Label: "Link to Discord · the last link was removed in Discord", Icon: icons.Tracker})
	default:
		options = append(options, SelectionOption{Key: "link", Label: "Link to Discord · watch time, now watching, reports", Icon: icons.Tracker})
	}
	if label := inboxRowLabel(config); label != "" {
		options = append([]SelectionOption{{Key: "inbox", Label: label, Icon: icons.Planning}}, options...)
	}
	for _, row := range communityRows {
		options = append(options, row.options(config, link.Token != "")...)
	}
	options = append(options, SelectionOption{Key: "report", Label: "Report a problem", Icon: icons.Edit})
	return options
}

// communityRow is a feature's rows in the Community menu, added from its
// own file with addCommunityRow. options gives the rows (linked tells
// whether the app is linked); handle answers a pick of one of them, by key,
// and reports whether it was one of its own and whether the menu should
// leave for a show queued with SetOpenLink.
type communityRow struct {
	options func(config *Config, linked bool) []SelectionOption
	handle  func(config *Config, key string) (handled, play bool)
}

var communityRows []communityRow

func addCommunityRow(row communityRow) { communityRows = append(communityRows, row) }

// ShowCommunity is the Community menu: the server's news, the invite, the
// Discord link and its sharing switch, and problem reports. It reports true
// when a pick queued a show to play (SetOpenLink), for the caller to go to.
func ShowCommunity(config *Config) bool {
	if !discordEnabled(config) {
		return false
	}
	// A visit is a good moment to refresh, but not to wait for one.
	go refreshDiscordNewsIfStale(config)
	go refreshDiscordClubIfStale(config)
	for {
		news, haveNews := cachedDiscordNews(config)
		selected, err := promptSelectOrdered(communityOptions(config, news, haveNews))
		if err != nil {
			Log(fmt.Sprintf("Community menu: %v", err))
			return false
		}
		selected = NormalizeSelectionKey(selected)
		if SelectionMeansQuit(selected) {
			Exit(nil)
		}
		if SelectionMeansBack(selected) {
			return false
		}
		for _, row := range communityRows {
			if handled, play := row.handle(config, selected.Key); handled {
				if play {
					return true
				}
				break
			}
		}
		switch selected.Key {
		case "inbox":
			EnterAppScreen()
			if showDiscordInbox(config) {
				return true
			}
		case "news":
			if err := discordOpenURL(news.Announcement.URL); err != nil {
				Log(fmt.Sprintf("Community: opening the announcement: %v", err))
			}
		case "join":
			if err := openDiscordInvite(); err != nil {
				Log(fmt.Sprintf("Community: opening the invite: %v", err))
				Out("Could not open a browser: " + DiscordInviteURL)
			}
		case "link":
			EnterAppScreen()
			linkDiscordFromMenu(config)
		case "unlink":
			if reached, _ := unlinkDiscord(config); reached {
				Out("Unlinked from Discord.")
			} else {
				Out("Forgot the link here; run /unlink-app in Discord to revoke it there too.")
			}
		case "share":
			setDiscordShareWatching(config, !config.DiscordShareWatching)
		case "party":
			EnterAppScreen()
			JoinWatchPartyFromMenu(config, HistoryFile(config.StoragePath))
		case "report":
			EnterAppScreen()
			reportFromMenu(config)
		}
		EnterAppScreen()
	}
}

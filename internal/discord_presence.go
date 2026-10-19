package internal

import (
	"fmt"
	"sync"
	"time"

	"github.com/tr1xem/go-discordrpc/client"
)

// OtakaseDiscordAppID is the Discord application profiles show as
// "Watching Otakase".
const OtakaseDiscordAppID = "1556589176164589578"

// oldDiscordAppID is the app id older configs were written with. It is read as
// Otakase's own, so those profiles don't show another app's name.
const oldDiscordAppID = "1287457464148820089"

func discordAppID(configured string) string {
	if configured == "" || configured == oldDiscordAppID {
		return OtakaseDiscordAppID
	}
	return configured
}

// Discord fetches presence images itself, so they must be public URLs. They
// are read from main, so a new image works without a site deploy.
const (
	discordImageBase  = "https://raw.githubusercontent.com/TheXykril/otakase/main/site/img/"
	discordIconImage  = discordImageBase + "icon-512.png"
	discordMarkImage  = discordImageBase + "mark-256.png"
	discordPauseImage = discordImageBase + "pause-256.png"
)

// presenceRefresh is how often the activity is sent again even when nothing
// changed, so Discord doesn't drop it.
const presenceRefresh = 2 * time.Minute

// presenceShown is what the activity currently says, to skip sending it again
// unchanged.
type presenceShown struct {
	title   string
	episode int
	paused  bool
	joinURL string
}

var presence struct {
	sync.Mutex
	rpc      *client.Client
	shown    presenceShown
	sentAt   time.Time
	hasShown bool
}

// DiscordPresence shows the episode playing in the user's Discord status.
func DiscordPresence(anime Anime, paused bool, position, duration int, appID string) error {
	return DiscordPresenceWithForce(anime, paused, position, duration, appID, false)
}

// DiscordPresenceWithForce is DiscordPresence that sends the activity even if
// nothing changed, as after a seek, when the timestamps moved.
func DiscordPresenceWithForce(anime Anime, paused bool, position, duration int, appID string, force bool) error {
	presence.Lock()
	defer presence.Unlock()

	if presence.rpc == nil {
		rpc := client.NewClient(discordAppID(appID))
		if err := rpc.Login(); err != nil {
			return fmt.Errorf("discord login: %w", err)
		}
		presence.rpc = rpc
	}

	now := time.Now()
	want := presenceShown{title: GetAnimeName(anime), episode: anime.Ep.Number, paused: paused, joinURL: watchWithMeURL()}
	if !force && presence.hasShown && want == presence.shown && now.Sub(presence.sentAt) < presenceRefresh {
		return nil
	}

	if err := presence.rpc.SetActivity(presenceActivity(anime, want, position, duration, now)); err != nil {
		return fmt.Errorf("discord activity: %w", err)
	}
	presence.shown, presence.sentAt, presence.hasShown = want, now, true
	return nil
}

func presenceActivity(anime Anime, shown presenceShown, position, duration int, now time.Time) client.Activity {
	started := now.Add(-time.Duration(position) * time.Second)
	times := &client.Timestamps{Start: &started}
	if !shown.paused && duration > 60 && duration > position {
		ends := now.Add(time.Duration(duration-position) * time.Second)
		times.End = &ends
	}

	badge, badgeText := discordMarkImage, DisplayName
	if shown.paused {
		badge, badgeText = discordPauseImage, "Paused"
	}

	cover := anime.CoverImage
	if cover == "" {
		cover = discordIconImage
	}

	anilist := &client.Button{Label: "View on AniList", Url: fmt.Sprintf("https://anilist.co/anime/%d", anime.AnilistId)}
	buttons := []*client.Button{anilist, {Label: "View on MAL", Url: fmt.Sprintf("https://myanimelist.net/anime/%d", anime.MalId)}}
	if shown.joinURL != "" {
		// Discord shows two buttons at most.
		buttons = []*client.Button{{Label: "Watch with me", Url: shown.joinURL}, anilist}
	}

	return client.Activity{
		Type:       3, // Watching
		Name:       shown.title,
		Details:    shown.title,
		State:      fmt.Sprintf("Episode %d", shown.episode),
		LargeImage: cover,
		LargeText:  shown.title,
		SmallImage: badge,
		SmallText:  badgeText,
		Timestamps: times,
		Buttons:    buttons,
	}
}

// ConvertSecondsToMinutes turns a length in seconds into whole minutes.
func ConvertSecondsToMinutes(seconds int) int {
	return seconds / 60
}

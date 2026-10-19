package internal

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/appicon"
)

// New episodes of shows the member follows with /follow in Discord. The bot
// sends an "airing" item next to its DM; the app says so on the desktop and
// keeps the episode under Community → From Discord, where picking it plays
// from that episode (it is kept as a queued show, so it goes the same way).
// On Linux the notification has a Play button when the notification daemon
// offers actions; it opens the same Play in Otakase link as Discord does.

func init() {
	registerDiscordInbox("airing", keepAiringItem)
}

// airingNotification is the desktop line for an aired episode.
func airingNotification(item discordInboxItem) string {
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = "a show you follow"
	}
	if item.Episode > 0 {
		return fmt.Sprintf("Episode %d of %s is out", item.Episode, title)
	}
	return "A new episode of " + title + " is out"
}

// keepAiringItem keeps the episode as a queued show, replacing an older
// entry of the same show, and notifies.
func keepAiringItem(config *Config, item discordInboxItem) {
	queued := item
	queued.Kind = "queue"
	changeKeptInbox(config, func(items []discordInboxItem) []discordInboxItem {
		kept := items[:0:0]
		for _, x := range items {
			if x.Kind == "queue" && x.AnilistID == queued.AnilistID && queued.AnilistID > 0 {
				continue
			}
			kept = append(kept, x)
		}
		kept = append([]discordInboxItem{queued}, kept...)
		if len(kept) > maxKeptInboxItems {
			kept = kept[:maxKeptInboxItems]
		}
		return kept
	})
	notifyAiring(airingNotification(item), item.URL)
}

// airingNotifyWait is how long a notification with a Play button is waited
// on; notify-send only returns when it is clicked or closed.
const airingNotifyWait = time.Hour

// notifyAiring shows the notification, with a Play button where it can.
// It is a variable so tests stay off the desktop.
var notifyAiring = func(message, url string) {
	if runtime.GOOS != "linux" || url == "" {
		notifyDesktop(message)
		return
	}
	if _, err := exec.LookPath("notify-send"); err != nil {
		notifyDesktop(message)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), airingNotifyWait)
		defer cancel()
		args := []string{"-a", DisplayName, "-A", "play=Play"}
		if icon := appicon.Path(); icon != "" {
			args = append(args, "-i", icon)
		}
		started := time.Now()
		out, err := exec.CommandContext(ctx, "notify-send", append(args, DisplayName, message)...).Output()
		if err != nil {
			// An old notify-send without actions fails at once: show it
			// the plain way. One that ran a while was closed or timed out.
			if time.Since(started) < 2*time.Second {
				notifyDesktop(message)
			}
			return
		}
		if strings.TrimSpace(string(out)) == "play" {
			if err := discordOpenURL(url); err != nil {
				Log(fmt.Sprintf("Discord: opening %s: %v", url, err))
			}
		}
	}()
}

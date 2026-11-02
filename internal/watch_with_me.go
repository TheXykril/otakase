package internal

import (
	"fmt"
	"sync"

	"github.com/thexykril/otakase/internal/icons"
)

// Watch with me: with DiscordWatchWithMe on and the app linked, playing an
// episode asks the bot for a synced party this app hosts
//
//	POST /v1/party/start {"anilist_id":154587,"episode":12,"title":"Frieren"} → {"id","join_url"}
//
// and the Discord status gets a Watch with me button with the join link.
// Whoever presses it follows this player like in any synced party. Nothing
// is posted anywhere: the link is only on the button. The party ends when
// nothing has played for watchWithMeIdle, when the option is turned off, or
// when the bot ends it; the next episode then starts a new one.

// watchWithMeIdle is how long the party waits for the next episode, and
// watchWithMePoll how often it looks. Tests shorten them.
var ()

var watchWithMe struct {
	sync.Mutex
	client   *partyClient
	joinURL  string
	starting bool
}

func init() {
	addCommunityRow(communityRow{options: watchWithMeOptions, handle: watchWithMeHandle})
}

// stopWatchWithMe ends the party for everyone and takes the button down.
func stopWatchWithMe(c *partyClient) {
	if c == nil {
		return
	}
	c.Send(map[string]string{"type": "end"})
	c.Close()
	watchWithMe.Lock()
	if watchWithMe.client == c {
		watchWithMe.client, watchWithMe.joinURL = nil, ""
	}
	watchWithMe.Unlock()
}

// The Community menu's switch.

func watchWithMeOptions(config *Config, linked bool) []SelectionOption {
	if !linked {
		return nil
	}
	if config.DiscordWatchWithMe {
		return []SelectionOption{{Key: "wwm", Label: "Watch with me: On · a join button in your Discord status while you watch", Icon: icons.Yes}}
	}
	return []SelectionOption{{Key: "wwm", Label: "Watch with me: Off · a join button in your Discord status while you watch", Icon: icons.No}}
}

func watchWithMeHandle(config *Config, key string) (handled, play bool) {
	if key != "wwm" {
		return false, false
	}
	config.DiscordWatchWithMe = !config.DiscordWatchWithMe
	saveConfigValue(config, "DiscordWatchWithMe", fmt.Sprintf("%t", config.DiscordWatchWithMe))
	if !config.DiscordWatchWithMe {
		watchWithMe.Lock()
		c := watchWithMe.client
		watchWithMe.Unlock()
		stopWatchWithMe(c)
		return true, false
	}
	if !config.DiscordPresence {
		Out("Watch with me is on, but the button shows in your Discord status: turn DiscordPresence on too.")
	}
	return true, false
}

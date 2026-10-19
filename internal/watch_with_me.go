package internal

import (
	"context"
	"fmt"
	"sync"
	"time"

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
var (
	watchWithMeIdle = 90 * time.Second
	watchWithMePoll = 5 * time.Second
)

var watchWithMe struct {
	sync.Mutex
	client   *partyClient
	joinURL  string
	starting bool
}

// watchWithMeURL is the join link for the Discord status, "" when no
// party is running.
func watchWithMeURL() string {
	watchWithMe.Lock()
	defer watchWithMe.Unlock()
	return watchWithMe.joinURL
}

func init() {
	addCommunityRow(communityRow{options: watchWithMeOptions, handle: watchWithMeHandle})
}

// startWatchWithMe starts the party for an mpv playback when the option is
// on and none runs yet. It returns at once.
func startWatchWithMe(config *Config, anime *Anime, socket string) {
	if config == nil || !config.DiscordWatchWithMe || anime == nil || anime.AnilistId <= 0 || !usableSocket(socket) ||
		partyHosting.Load() || !discordLinked(config) {
		return
	}
	watchWithMe.Lock()
	if watchWithMe.client != nil || watchWithMe.starting {
		watchWithMe.Unlock()
		return
	}
	watchWithMe.starting = true
	watchWithMe.Unlock()
	show := heartbeatWatching{AnilistID: anime.AnilistId, Title: GetAnimeName(*anime), Episode: anime.Ep.Number}
	go func() {
		c, url, err := openWatchWithMe(config, show)
		watchWithMe.Lock()
		watchWithMe.starting = false
		if err == nil {
			watchWithMe.client, watchWithMe.joinURL = c, url
		}
		watchWithMe.Unlock()
		if err != nil {
			Log("Watch with me: " + err.Error())
			return
		}
		Log("Watch with me: hosting " + url)
		go watchWithMeIdleStop(config, c)
		runPartyHost(c, playingNow)
		c.Close()
		watchWithMe.Lock()
		if watchWithMe.client == c {
			watchWithMe.client, watchWithMe.joinURL = nil, ""
		}
		watchWithMe.Unlock()
	}()
}

// openWatchWithMe asks the bot for the party and connects to it as host.
func openWatchWithMe(config *Config, show heartbeatWatching) (*partyClient, string, error) {
	token := loadDiscordLink(config).Token
	var answer struct {
		ID      string `json:"id"`
		JoinURL string `json:"join_url"`
	}
	in := map[string]any{"anilist_id": show.AnilistID, "episode": show.Episode, "title": show.Title}
	if _, err := discordPostJSON(context.Background(), config, "/v1/party/start", token, in, &answer); err != nil {
		return nil, "", err
	}
	if _, ok := parsePartyID(answer.ID); !ok || answer.JoinURL == "" {
		return nil, "", fmt.Errorf("the bot answered without a party")
	}
	c, welcome, err := dialParty(discordBaseURL(config), token, answer.ID, Version())
	if err != nil {
		return nil, "", err
	}
	if !welcome.Host {
		c.Close()
		return nil, "", fmt.Errorf("the bot didn't make this app the host")
	}
	return c, answer.JoinURL, nil
}

// watchWithMeIdleStop ends the party once nothing has played for a while,
// or straight away when the option is turned off.
func watchWithMeIdleStop(config *Config, c *partyClient) {
	last := time.Now()
	for !c.isClosed() {
		time.Sleep(watchWithMePoll)
		_, _, socket := playingNow()
		if usableSocket(socket) && IsMPVRunning(socket) {
			last = time.Now()
		}
		if !config.DiscordWatchWithMe || time.Since(last) > watchWithMeIdle {
			stopWatchWithMe(c)
			return
		}
	}
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

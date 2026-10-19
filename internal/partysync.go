package internal

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// Synced watch parties: `otakase -party <eventID>` (or Join watch party in
// the menu) connects to a party planned with /watchparty sync:True. The
// party's host plays as usual and their app sends its mpv state; everyone
// else's app opens the same show and episode through its own provider and
// follows the host through mpv's IPC. Watch with me (watch_with_me.go)
// hosts a party of its own the same way, and partysync_fx.go adds the
// party's chat, reactions and votes in mpv.

// StartWatchParty joins the party that input names (an event id or a party
// link). For the host it starts sending the player state in the background
// and returns true: the host then picks the show and episode as usual. For
// a member it plays along until they close mpv or the party ends, and
// returns false.
func StartWatchParty(config *Config, databaseFile, input string) (hosting bool, err error) {
	id, ok := parsePartyID(input)
	if !ok {
		return false, fmt.Errorf("%q isn't a watch party id or link", strings.TrimSpace(input))
	}
	token, linked := discordToken()
	if !linked {
		return false, errPartyNotLinked
	}
	endBusy := BeginBusy(config, "Joining the watch party")
	client, welcome, err := dialParty(discordBotURL(), token, id, Version())
	endBusy()
	if err != nil {
		if errors.Is(err, errPartyNotLinked) {
			return false, fmt.Errorf("the bot doesn't know this app: link it again with otakase -discord-link")
		}
		return false, err
	}
	show := welcome.Show
	if show == "" {
		show = "the watch party"
	}
	if welcome.Host {
		Out(fmt.Sprintf("You're hosting %s. Pick the show and episode: everyone follows your player.", show))
		partyHosting.Store(true) // Watch with me stays out of the way
		go func() {
			runPartyHost(client, playingNow)
			partyHosting.Store(false)
		}()
		return true, nil
	}
	defer client.Close()
	Out(fmt.Sprintf("Joined %s. Waiting for the host to start playing...", show))
	runPartyMember(config, databaseFile, client)
	return false, nil
}

// JoinWatchPartyFromMenu asks for the party link and joins it. A member
// watches the party and comes back; the host comes back to pick the show,
// with the state already being sent.
func JoinWatchPartyFromMenu(config *Config, databaseFile string) {
	if _, linked := discordToken(); !linked {
		Out("Link this app first: run otakase -discord-link, then join the party again.")
		return
	}
	input, cancelled, err := promptCancelable(config, "Watch party",
		"Paste the watch party link (the Join in Otakase button) or its event id",
		"enter to join · esc to go back")
	if err != nil || cancelled || strings.TrimSpace(input) == "" {
		return
	}
	if _, err := StartWatchParty(config, databaseFile, input); err != nil {
		Out("Couldn't join the watch party: " + err.Error())
		Log("watch party: " + err.Error())
	}
}

// mpvOSD shows a short message in mpv.
func mpvOSD(socket, text string) {
	if socket == "" || text == "" {
		return
	}
	if _, err := MPVSendCommand(socket, []interface{}{"show-text", text, 3000}); err != nil {
		Log("watch party OSD: " + err.Error())
	}
}

// mpvPlayerState reads pause and position; loaded is false while nothing
// plays yet.
func mpvPlayerState(socket string) (paused bool, position float64, loaded bool, err error) {
	p, err := MPVSendCommand(socket, []interface{}{"get_property", "pause"})
	if err != nil {
		return false, 0, false, err
	}
	paused, _ = p.(bool)
	pos, err := MPVSendCommand(socket, []interface{}{"get_property", "time-pos"})
	if err != nil {
		if mpvConnectionLost(err) {
			return paused, 0, false, err
		}
		return paused, 0, false, nil // loading
	}
	position, loaded = pos.(float64)
	return paused, position, loaded, nil
}

func usableSocket(socket string) bool {
	return socket != "" && socket != "android-intent" && socket != "cast"
}

// partyHosting is set while this app hosts a /watchparty party.
var partyHosting atomic.Bool

// playingNow is the show, episode and mpv socket of this run's playback.
func playingNow() (anilistID, episode int, socket string) {
	a := GetGlobalAnime()
	if a == nil {
		return 0, 0, ""
	}
	return a.AnilistId, a.Ep.Number, a.Ep.Player.SocketPath
}

// runPartyHost sends the host's player state: on play, pause, seek and
// episode change, and every few seconds. current reports the show,
// episode and mpv socket playing now. It also runs the party's extras in
// the host's mpv (partysync_fx.go) and does what the party votes for.
func runPartyHost(c *partyClient, current func() (anilistID, episode int, socket string)) {
	var last *partyHostState
	var lastAt time.Time
	var members int
	var told string // the mpv told it hosts
	fx := newPartyFX(c.Send)
	fx.onVoted = partyHostVoted
	defer fx.close()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case m, ok := <-c.In:
			if !ok {
				return
			}
			if fx.handle(m) {
				continue
			}
			_, _, socket := current()
			switch m.Type {
			case "members":
				n := len(m.List) - 1 // not the host
				if n != members && usableSocket(socket) {
					mpvOSD(socket, fmt.Sprintf("Watch party: %d watching along", n))
				}
				members = n
			case "ended":
				if usableSocket(socket) {
					mpvOSD(socket, "The watch party ended")
				}
				Log("watch party ended")
				return
			case "gone":
				Log("watch party: " + m.Err.Error())
				return
			}
			continue
		case <-c.Connected:
			last = nil // resend straight away
			continue
		case <-tick.C:
		}
		anilistID, episode, socket := current()
		cur := partyHostState{AniListID: anilistID, Episode: episode}
		if usableSocket(socket) {
			paused, pos, loaded, err := mpvPlayerState(socket)
			if loaded && told != socket {
				mpvOSD(socket, "Watch party: everyone follows your player")
				told = socket
			}
			if loaded {
				fx.attach(socket)
			}
			fx.tick()
			switch {
			case err != nil || !loaded:
				// mpv closed or loading: the members wait where the host was.
				if last != nil {
					cur.Position = last.Position
				}
			default:
				cur.Playing, cur.Position = !paused, pos
			}
		} else if last != nil {
			cur.Position = last.Position
		}
		now := time.Now()
		if !partyShouldSend(last, lastAt, cur, now) {
			continue
		}
		cur.SentAt = c.ServerNow()
		msg := map[string]any{"type": "state", "anilist_id": cur.AniListID, "episode": cur.Episode,
			"playing": cur.Playing, "position": cur.Position, "sent_at": cur.SentAt}
		c.Send(msg)
		sent := cur
		last, lastAt = &sent, now
	}
}

// runPartyMember follows the host until mpv closes or the party ends.
func runPartyMember(config *Config, databaseFile string, c *partyClient) {
	// A party plays in mpv here, whatever -cast says.
	cfg := *config
	cfg.CastToDevice = false

	previous := GetGlobalAnime()
	defer SetGlobalAnime(previous)

	var host partyHostState
	var haveHost bool
	var anime *Anime
	fx := newPartyFX(c.Send)
	defer fx.close()
	for {
		// Wait for the host's state when there's nothing to play yet.
		for !haveHost {
			m, ok := <-c.In
			if !ok {
				return
			}
			switch m.Type {
			case "state":
				host, haveHost = m.partyHostState, true
			case "ended":
				Out("The watch party ended.")
				return
			case "gone":
				Out("Left the watch party: " + m.Err.Error())
				return
			}
		}
		if anime == nil || anime.AnilistId != host.AniListID {
			next, err := preparePartyAnime(&cfg, databaseFile, host.AniListID)
			if err != nil {
				Out("Couldn't open the host's show: " + err.Error())
				return
			}
			anime = next
			SetGlobalAnime(anime)
		}
		socket, ok := startPartyEpisode(&cfg, anime, host.Episode)
		if !ok {
			return
		}
		next, again := followHost(c, fx, anime, socket, host)
		if !again {
			return
		}
		host = next
	}
}

// preparePartyAnime finds the show on this app's providers: the mapping
// from the watch history when there is one, else a search.
func preparePartyAnime(config *Config, databaseFile string, anilistID int) (*Anime, error) {
	data, err := GetAnimeDataByID(anilistID, "")
	if err != nil {
		return nil, err
	}
	anime := data
	anime.Ep.Player.Speed = 1.0
	applyShowAudioMode(config, &anime)
	if known := LocalFindAnime(LocalGetAllAnime(databaseFile), anilistID, ""); known != nil && known.ProviderId != "" &&
		(known.ProviderName == "" || ProviderStackContains(config, known.ProviderName)) {
		anime.ProviderId, anime.ProviderName = known.ProviderId, known.ProviderName
		return &anime, nil
	}
	entry := &Entry{Media: Media{ID: anilistID, MalID: anime.MalId, Episodes: anime.TotalEpisodes, Title: anime.Title,
		IsAdult: anime.IsAdult}, Status: "CURRENT", CoverImage: anime.CoverImage}
	query := anime.Title.Romaji
	if query == "" {
		query = anime.Title.English
	}
	outcome, err := ResolveAnimeProviderMapping(config, &anime, query, entry)
	if err != nil {
		return nil, err
	}
	if outcome != ProviderMappingOK || anime.ProviderId == "" {
		return nil, errors.New("no source picked for it")
	}
	return &anime, nil
}

// startPartyEpisode opens episode in mpv and returns its socket.
func startPartyEpisode(config *Config, anime *Anime, episode int) (string, bool) {
	anime.Ep.Number = episode
	anime.Ep.Links = nil
	anime.Ep.NextEpisode = NextEpisode{}
	anime.Ep.Started, anime.Ep.IsCompleted = false, false
	anime.Ep.Player.PlaybackTime, anime.Ep.Resume = 0, false
	anime.Ep.SkipTimes = SkipTimes{}
	result, ok := resolveEpisodeLinksWithRecovery(config, anime, nil)
	if !ok || len(result.Links) == 0 {
		Out(fmt.Sprintf("Couldn't find episode %d on your providers.", episode))
		return "", false
	}
	anime.Ep.Links = result.Links
	applyStreamPlaybackHints(anime, anime.Ep.Links, result.LinkHints)
	title := fmt.Sprintf("%s - Episode %d (watch party)", GetAnimeName(*anime), episode)
	Out(title)
	socket := StartVideoWithProviderFallback(config, anime, title)
	anime.Ep.Player.SocketPath = socket
	if !usableSocket(socket) {
		Out("A synced watch party needs mpv on this device.")
		return "", false
	}
	return socket, true
}

// followHost keeps mpv on the host's state. It returns the host's state
// and true when the host switched episode (or show), false when the member
// closed mpv or the party is over. fx shows the party's chat, reactions and
// votes in mpv.
func followHost(c *partyClient, fx *partyFX, anime *Anime, socket string, host partyHostState) (partyHostState, bool) {
	c.Send(map[string]any{"type": "ready", "episode": anime.Ep.Number})
	lastSeek := time.Time{}
	synced := false
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case m, ok := <-c.In:
			if !ok {
				return host, false
			}
			if fx.handle(m) {
				continue
			}
			switch m.Type {
			case "state":
				host = m.partyHostState
			case "ended":
				mpvOSD(socket, "The watch party ended")
				Out("The watch party ended. Close the player when you're done.")
				waitForMPVClose(socket)
				return host, false
			case "gone":
				mpvOSD(socket, "Left the watch party")
				Out("Left the watch party: " + m.Err.Error())
				waitForMPVClose(socket)
				return host, false
			default:
				continue
			}
		case <-c.Connected:
			c.Send(map[string]any{"type": "ready", "episode": anime.Ep.Number})
			continue
		case <-tick.C:
		}
		if host.AniListID != anime.AnilistId {
			mpvOSD(socket, "The host started another show")
			ExitMPV(socket)
			return host, true
		}
		paused, pos, loaded, err := mpvPlayerState(socket)
		if err != nil && MPVConnectionGone(err) {
			Out("You left the watch party.")
			return host, false
		}
		if loaded {
			fx.attach(socket)
		}
		fx.tick()
		local := partyLocal{Episode: anime.Ep.Number, Loaded: loaded, Paused: paused, Position: pos, SinceSeek: time.Since(lastSeek)}
		act := decidePartySync(host, local, c.ServerNow())
		if act.Switch {
			mpvOSD(socket, act.OSD)
			ExitMPV(socket)
			return host, true
		}
		if act.Pause || act.Play {
			if _, err := MPVSendCommand(socket, []interface{}{"set_property", "pause", act.Pause}); err != nil {
				Log("watch party pause: " + err.Error())
			}
		}
		if act.Seek {
			if _, err := MPVSendCommand(socket, []interface{}{"seek", act.SeekTo, "absolute"}); err != nil {
				Log("watch party seek: " + err.Error())
			}
			lastSeek = time.Now()
		}
		if act.OSD != "" {
			mpvOSD(socket, act.OSD)
			synced = true
		} else if loaded && !synced {
			mpvOSD(socket, "Synced to host")
			synced = true
		}
	}
}

// waitForMPVClose returns once the member has closed mpv.
func waitForMPVClose(socket string) {
	for IsMPVRunning(socket) {
		time.Sleep(time.Second)
	}
}

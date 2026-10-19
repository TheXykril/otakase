package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// WatchUntracked plays a show picked by search, without reading or writing
// any history or remote list.
func WatchUntracked(userConfig *Config) {
	show, picked := pickUntrackedShow(userConfig)
	if !picked {
		return
	}

	first, backedOut, promptErr := promptEpisodeCancelable(userConfig, "Untracked",
		fmt.Sprintf("Which episode of %s?", GetAnimeName(show)),
		"a number · esc to go back")
	switch {
	case promptErr != nil:
		Log(fmt.Sprintf("Invalid episode number: %v", promptErr))
		return
	case backedOut:
		return
	}
	show.Ep.Number = first

	for playUntrackedEpisode(userConfig, &show) {
	}
}

// loadUntrackedLinks gets the links for the episode show is on: the ones
// prefetched while the last episode played when they arrived in time,
// otherwise a fresh lookup (preferred provider and mode first, the slower
// diagnosed recovery only after that fails). No links at all ends the program.
func loadUntrackedLinks(userConfig *Config, show *Anime) {
	if UsePrefetchedEpisode(show) {
		show.Ep.NextEpisode = NextEpisode{}
	} else {
		found, ok := resolveEpisodeLinksWithRecovery(userConfig, show, nil)
		if !ok {
			Exit(nil)
		}
		show.Ep.Links = found.Links
		applyStreamPlaybackHints(show, found.Links, found.LinkHints)
	}
	if len(show.Ep.Links) == 0 {
		Exit(fmt.Errorf("No episode links found"))
	}
}

// playUntrackedEpisode plays the episode show is on and moves it along. It
// reports whether to carry on with the next one.
func playUntrackedEpisode(userConfig *Config, show *Anime) bool {
	loadUntrackedLinks(userConfig, show)

	heading := fmt.Sprintf("%s - Episode %d", GetAnimeName(*show), show.Ep.Number)
	Out(heading)

	// A cast replaces mpv. It is checked before the prefetch: the next links
	// are resolved when the loop comes back round, and a rofi cast hands the
	// whole season to another process anyway.
	if userConfig.CastToDevice {
		show.Untracked = true
		if !castUntrackedEpisode(userConfig, show) {
			return false
		}
		advanceUntrackedCast(show)
		return true
	}

	go prefetchNextUntrackedEpisode(userConfig, show)

	// Moves to another provider if playback never starts, like the tracked
	// path.
	socket := StartVideoWithProviderFallback(userConfig, show, heading)
	resetUntrackedPlayer(show, socket)
	Log("Started mpv with socket path: " + socket)

	if socket == androidIntentSocket {
		return awaitExternalPlayer(show)
	}

	// Same skip-time lookup as tracked playback; it was once cast-only, so an
	// untracked show in mpv skipped nothing.
	go ApplySkipTimes(show, show.Ep.Number, userConfig, GetProvider())
	go readUntrackedDuration(show)
	monitorUntrackedPlayback(userConfig, show)
	return true
}

// resetUntrackedPlayer clears what the previous episode left on the player
// and points it at socket.
func resetUntrackedPlayer(show *Anime, socket string) {
	show.Ep.Player.SocketPath = socket
	show.Ep.Player.PlaybackTime = 0
	show.Ep.Started, show.Ep.IsCompleted = false, false
	show.Ep.Duration, show.Ep.SkipTimes = 0, SkipTimes{}
}

// awaitExternalPlayer waits for the user to say an episode handed to an
// external Android player is over, since there is no IPC to watch. It reports
// false when stdin closes first.
func awaitExternalPlayer(show *Anime) bool {
	Out(fmt.Sprintf("\nOpened external player for Episode %d.", show.Ep.Number))
	Out("Press Enter when you have finished watching...")
	if !AwaitEnter() {
		Log("stdin ended while waiting for the external player; stopping")
		return false
	}
	show.Ep.Number++
	show.Ep.Started = false
	return true
}

// pickUntrackedShow asks for a search and a result, and returns the chosen show
// with its ids filled in where they could be found. ok is false when the user
// backed out.
func pickUntrackedShow(userConfig *Config) (anime Anime, ok bool) {
	// Every way out of here returns rather than quitting: this is reached by
	// one keypress from a list, and the cost of pressing it by accident should
	// be one more keypress, not the program closing.
	for {
		query, cancelled, inputErr := promptCancelable(userConfig, "Untracked",
			"Search for an anime to watch without tracking it",
			"enter to search · esc to go back")
		if inputErr != nil {
			Log("Error getting user input: " + inputErr.Error())
			return anime, false
		}
		if cancelled {
			return anime, false
		}

		providerID, providerName, picked, back, searchErr := ResolveUntrackedProviderSearch(userConfig, query)
		if searchErr != nil {
			Log(fmt.Sprintf("Failed to search anime: %v", searchErr))
			Out(fmt.Sprintf("Could not search for %q: %v", query, searchErr))
			continue
		}
		if back {
			return anime, false
		}
		if providerID == "" {
			// Nothing chosen from the results: ask again rather than leaving,
			// since the search itself worked.
			continue
		}

		anime.ProviderId = providerID
		anime.ProviderName = providerName
		anime.Title.English = query
		anime.Title.Romaji = query
		// The picked show's own title, not what was typed: it is what the
		// skip-time lookup matches against, and what the cast panel shows.
		if title := strings.TrimSpace(picked.Title); title != "" {
			anime.Title.English = title
			anime.Title.Romaji = title
		}
		anime.MalId = malIDFromProviderExtraData(picked.ExtraData)
		// Skip services key on these ids, and an untracked show arrives with
		// none unless its provider attached one.
		ensureUntrackedIDs(&anime)
		// Having ids now makes the show look trackable; it is not.
		anime.Untracked = true
		return anime, true
	}
}

// readUntrackedDuration asks mpv for the episode's length once it has started
// playing. The monitor needs it to tell a finished episode from one quit early.
func readUntrackedDuration(anime *Anime) {
	for !anime.Ep.Started {
		time.Sleep(time.Second)
	}
	if anime.Ep.Duration != 0 {
		return
	}
	value, err := MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"get_property", "duration"})
	switch {
	case err != nil:
		Log("Error getting video duration: " + err.Error())
	case value == nil:
	default:
		seconds, ok := value.(float64)
		if !ok {
			Log("Error: duration is not a float64")
			return
		}
		anime.Ep.Duration = int(seconds + 0.5)
		Log(fmt.Sprintf("Video duration: %d seconds", anime.Ep.Duration))
	}
}

// untrackedLossOutcome is what monitorUntrackedPlayback does after mpv stops
// answering.
type untrackedLossOutcome int

const (
	lossCarryOn     untrackedLossOutcome = iota // poll again after the usual second
	lossPollSoon                                // poll again almost at once
	lossEpisodeOver                             // the episode counts as watched
)

// onUntrackedPollError decides what a failed position poll means. Quitting
// mpv short of the completion threshold, or before playback ever began, ends
// the program.
func onUntrackedPollError(userConfig *Config, show *Anime, pollErr error) untrackedLossOutcome {
	Log("Error getting playback time: " + pollErr.Error())

	if !show.Ep.Started {
		if mpvConnectionLost(pollErr) {
			// Rare once StartVideoWithProviderFallback has run, but leave cleanly.
			Log("MPV exited before untracked playback was marked started")
			Exit(nil)
		}
		return lossCarryOn
	}

	watched := PercentageWatched(show.Ep.Player.PlaybackTime, show.Ep.Duration)
	verdict := ClassifyPlaybackLoss(show.Ep.Player.SocketPath, true, watched, userConfig.PercentageToMarkComplete)
	Log(fmt.Sprintf("untracked playback loss: pct=%.1f action=%d", watched, verdict))
	switch verdict {
	case PlaybackLossWait:
		// A playlist switch or a reload.
		return lossPollSoon
	case PlaybackLossComplete:
		Log("Completed episode, starting next.")
		show.Ep.Number++
		show.Ep.Started = false
		show.Ep.IsCompleted = true
		return lossEpisodeOver
	case PlaybackLossExit:
		Log("Episode is not completed, exiting")
		Exit(nil)
	}
	return lossCarryOn
}

// onUntrackedPosition takes one position report from mpv: it marks playback
// started, applies any skip that is due and remembers the speed for the next
// episode.
func onUntrackedPosition(userConfig *Config, show *Anime, value interface{}) {
	if !show.Ep.Started {
		show.Ep.Started = true
		onUntrackedPlaybackStart(userConfig, show)
	}

	seconds, isFloat := value.(float64)
	if !isFloat {
		Log("Error: timePos is not a float64")
		return
	}
	show.Ep.Player.PlaybackTime = int(seconds + 0.5)

	socket := show.Ep.Player.SocketPath
	if target, due := SkipSeekTarget(show.Ep.SkipTimes, show.Ep.Player.PlaybackTime, userConfig); due {
		SeekMPV(socket, target)
	}
	if speed, speedErr := GetMPVPlaybackSpeed(socket); speedErr == nil {
		show.Ep.Player.Speed = speed
	}
}

// monitorUntrackedPlayback follows one episode in mpv, once a second, until it
// is over. It returns when the episode counts as watched, with anime moved on
// to the next one; quitting mpv short of that exits the program.
func monitorUntrackedPlayback(userConfig *Config, anime *Anime) {
	for {
		position, pollErr := MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"get_property", "time-pos"})
		if pollErr != nil {
			switch onUntrackedPollError(userConfig, anime, pollErr) {
			case lossEpisodeOver:
				return
			case lossPollSoon:
				time.Sleep(200 * time.Millisecond)
				continue
			}
		}
		if position != nil {
			onUntrackedPosition(userConfig, anime, position)
		}
		time.Sleep(time.Second)
	}
}

// onUntrackedPlaybackStart runs once, at the first position mpv reports.
func onUntrackedPlaybackStart(userConfig *Config, anime *Anime) {
	socket := anime.Ep.Player.SocketPath
	if userConfig.SaveMpvSpeed && anime.Ep.Player.Speed > 0 {
		if _, err := MPVSendCommand(socket, []interface{}{"set_property", "speed", anime.Ep.Player.Speed}); err != nil {
			Log("Error setting playback speed: " + err.Error())
		}
	}
	// Equal start and end means the skip lookup found nothing for that part.
	skips := anime.Ep.SkipTimes
	if skips.Op.Start != skips.Op.End || skips.Ed.Start != skips.Ed.End {
		if err := SendSkipTimesToMPV(anime); err != nil {
			Log("Error sending skip times to MPV: " + err.Error())
		}
	}
}

// prefetchNextUntrackedEpisode resolves the following episode while this one
// plays, so the next round of the loop starts sooner. Only the preferred
// SubOrDub is tried: nobody can be asked about audio from a goroutine.
func prefetchNextUntrackedEpisode(userConfig *Config, anime *Anime) {
	if userConfig == nil || anime == nil {
		return
	}
	upcoming := anime.Ep.Number + 1
	scratch := *anime
	found, resolveErr := ResolveEpisodeURL(*userConfig, &scratch, upcoming)
	if resolveErr != nil {
		Log(fmt.Sprintf("Error getting next untracked episode link for ep %d: %v", upcoming, resolveErr))
		return
	}
	anime.Ep.NextEpisode = NextEpisode{
		Number:       upcoming,
		Links:        found.Links,
		ProviderName: found.ProviderName,
		ProviderId:   found.ProviderID,
		Mode:         found.Mode,
		LinkHints:    found.LinkHints,
	}
}

// castUntrackedEpisode casts one Untracked Watching episode, reporting whether
// to go on to the next one.
//
// The same shape as StartPlayback's cast branch, which Untracked Watching
// never reaches: it starts mpv itself, so -cast used to be ignored here and
// the episode opened locally. With rofi the season is handed to a terminal
// window, which advances it on its own; this process is then done with it.
func castUntrackedEpisode(config *Config, anime *Anime) bool {
	LeaveAppScreen()

	// Skip times come from AniSkip and the like, which are keyed by MAL id,
	// and an untracked show arrives with none unless its provider attached
	// one. Looked up once for the show, before the handoff, so the terminal
	// window inherits it.
	ensureUntrackedIDs(anime)

	if config.RofiSelection {
		err := handOffCastToTerminal(config, anime)
		if err == nil {
			// The window owns the season now. Returning would reopen the
			// menu under it, which is not what the tracked path does either:
			// main exits once a rofi cast is handed off.
			Exit(nil)
		}
		if errors.Is(err, ErrCastStopped) {
			return false
		}
		Out("Casting here instead of in a terminal: " + err.Error())
		Log(fmt.Sprintf("cast: handoff failed: %v", err))
	}

	err := CastEpisode(config, anime)
	if err == nil {
		return true
	}
	if !errors.Is(err, ErrCastStopped) {
		Out("Casting failed: " + err.Error())
		Log(fmt.Sprintf("cast: %v", err))
	}
	return false
}

// ensureUntrackedIDs fills in the MAL and AniList ids of an untracked show from
// its title, where they are missing. Once per show: an episode later the title
// is the same.
func ensureUntrackedIDs(anime *Anime) {
	if anime == nil || (anime.MalId != 0 && anime.AnilistId != 0) {
		return
	}
	malID, anilistID := lookupUntrackedIDs(GetAnimeName(*anime))
	if anime.MalId == 0 {
		anime.MalId = malID
	}
	if anime.AnilistId == 0 {
		anime.AnilistId = anilistID
	}
}

// lookupUntrackedIDs finds the MAL and AniList ids of an untracked show by its
// title, or zeros.
//
// Only an exact title match is taken. A near miss is usually another season or
// a sequel, and its skip times would jump to the wrong place in this episode --
// worse than not skipping at all.
func lookupUntrackedIDs(title string) (malID, anilistID int) {
	want := untrackedTitleKey(title)
	if want == "" {
		return 0, 0
	}

	requestBody, err := json.Marshal(map[string]interface{}{
		"query": `query ($search: String) {
			Page(page: 1, perPage: 10) {
				media(search: $search, type: ANIME) {
					id
					idMal
					title { romaji english }
				}
			}
		}`,
		"variables": map[string]string{"search": title},
	})
	if err != nil {
		return 0, 0
	}
	body, err := doAniListSearchRequest(untrackedAniListEndpoint, requestBody, "")
	if err != nil {
		Log(fmt.Sprintf("untracked: could not look up %q on AniList: %v", title, err))
		return 0, 0
	}

	var response struct {
		Data struct {
			Page struct {
				Media []struct {
					ID    int `json:"id"`
					IDMal int `json:"idMal"`
					Title struct {
						Romaji  string `json:"romaji"`
						English string `json:"english"`
					} `json:"title"`
				} `json:"media"`
			} `json:"Page"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		Log(fmt.Sprintf("untracked: could not read AniList's answer for %q: %v", title, err))
		return 0, 0
	}
	for _, media := range response.Data.Page.Media {
		if media.ID == 0 && media.IDMal == 0 {
			continue
		}
		if untrackedTitleKey(media.Title.Romaji) == want || untrackedTitleKey(media.Title.English) == want {
			Log(fmt.Sprintf("untracked: %q is AniList %d, MAL %d", title, media.ID, media.IDMal))
			return media.IDMal, media.ID
		}
	}
	Log(fmt.Sprintf("untracked: no exact AniList match for %q, playing without skip times", title))
	return 0, 0
}

// untrackedAniListEndpoint is a variable so tests can answer for AniList.
var untrackedAniListEndpoint = "https://graphql.anilist.co"

// untrackedTitleKey reduces a title to its lowercase letters and digits, so
// punctuation that differs between a provider and AniList -- a curly
// apostrophe, a colon, a dash -- does not stop an exact match.
func untrackedTitleKey(title string) string {
	var key strings.Builder
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			key.WriteRune(r)
		}
	}
	return key.String()
}

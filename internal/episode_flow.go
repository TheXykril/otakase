package internal

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/icons"
)

// episodeLinkResolver is the resolve ResolveEpisodeLinks runs. A variable so a
// test can drive the callers without a provider or a network.
var episodeLinkResolver = resolveEpisodeLinksWithRecovery

// ResolveEpisodeLinks fills anime.Ep with a playable stream for the episode it
// names, reporting whether it found one.
//
// Preferred-first resolve; the diagnosed recovery menu only after that fails.
// ok=false means nothing played and the caller should stop -- either the
// resolve found nothing or the viewer backed out of the recovery menu.
//
// It is shared rather than inlined into StartPlayback because the process
// spawned for a rofi cast never reaches StartPlayback: it casts episode after
// episode itself, and StartNextEpisode clears Ep.Links "to force fetching new
// ones" without fetching anything. This is that fetch, for both callers.
func ResolveEpisodeLinks(config *Config, anime *Anime) bool {
	endBusy := BeginBusy(config, fmt.Sprintf("Finding episode %d", anime.Ep.Number))
	episodeResult, ok := episodeLinkResolver(config, anime, nil)
	endBusy()
	if !ok {
		return false
	}
	Log(fmt.Sprintf("Successfully retrieved %s/%s episode link. Links count: %d", episodeResult.ProviderName, episodeResult.Mode, len(episodeResult.Links)))
	anime.Ep.Links = episodeResult.Links
	anime.Ep.Mode = episodeResult.Mode
	applyStreamPlaybackHints(anime, anime.Ep.Links, episodeResult.LinkHints)
	return true
}

// StartPlayback plays the current episode, locally or on a cast device, and
// returns mpv's socket. Instead of a socket it returns "" when nothing
// played, or a sentinel ("cast", "android-intent") when the episode played
// somewhere otakase cannot watch it.
func StartPlayback(userConfig *Config, anime *Anime) string {
	// Without either there is nothing to look up, and nowhere sensible to
	// go back to.
	switch {
	case anime.ProviderId == "":
		Out("Error: No anime ID found")
		restoreAndExit(1)
	case anime.Ep.Number <= 0:
		Out("Error: Invalid episode number")
		restoreAndExit(1)
	}

	// Reaching here for an episode that has not been broadcast means the user is
	// caught up. Searching every provider for it produces a failure that reads as
	// a broken tool, so say what is actually the case first. This is the same
	// answer the end-of-episode prompt gives; picking the show again from the
	// list a day later arrives by a different route and deserves the same one.
	if availability := nextEpisodeAiring(anime, anime.Ep.Number); !availability.Aired {
		if !confirmUnairedEpisode(userConfig, anime.Ep.Number, availability) {
			LeaveAppScreen()
			return ""
		}
	}

	if !UsePrefetchedEpisode(anime) && !ResolveEpisodeLinks(userConfig, anime) {
		LeaveAppScreen()
		return ""
	}
	if len(anime.Ep.Links) == 0 {
		Out("No episode links found")
		LeaveAppScreen()
		return ""
	}
	Log(fmt.Sprintf("Episode links validation passed. Found %d links", len(anime.Ep.Links)))

	go prefetchNextEpisode(userConfig, anime)

	// Remember the show for Continue Last Session and the continue-watching
	// rows of the home menu.
	writeLastPlayedAnimeID(userConfig.StoragePath, anime.AnilistId, anime.IsAdult)

	title := fmt.Sprintf("%s - Episode %d", GetAnimeName(*anime), anime.Ep.Number)
	announceEpisode(userConfig, anime, title)

	if userConfig.CastToDevice {
		// Restored before casting rather than after: casting is a long
		// text-progress operation with no TUI to preserve, every line it prints
		// over the next 20 minutes needs a real terminal to land in, and a
		// failure message printed into the alternate buffer would be discarded
		// by the restore a microsecond later -- the same reason the three
		// earlier returns above call LeaveAppScreen() before giving up.
		LeaveAppScreen()

		// A rofi launch has no terminal to show controls in, so the cast is
		// handed to one: that process owns ffmpeg, the server, the device and
		// all tracking, which is what keeps exactly one process writing
		// history. If it cannot be done the episode still plays here, without
		// controls, because a missing terminal must not mean a missing episode.
		if userConfig.RofiSelection {
			if err := handOffCastToTerminal(userConfig, anime); errors.Is(err, ErrCastStopped) {
				// The viewer escaped the device menu: nothing to fall back to.
				return ""
			} else if err != nil {
				Out("Casting here instead of in a terminal: " + err.Error())
				Log(fmt.Sprintf("cast: handoff failed: %v", err))
			} else {
				return ""
			}
		}

		// Casting owns the episode until it ends, and there is no mpv socket to
		// hand back: the caller's playback loop has nothing to poll.
		castErr := CastEpisode(userConfig, anime)
		socket, report := castOutcome(castErr)
		if report {
			Out("Casting failed: " + castErr.Error())
			Log(fmt.Sprintf("cast: %v", castErr))
		}
		return socket
	}

	return StartVideoWithProviderFallback(userConfig, anime, title)
}

// prefetchNextEpisode resolves the episode after the one starting, so it can
// follow without a wait. The result goes to anime.Ep.NextEpisode.
func prefetchNextEpisode(userConfig *Config, anime *Anime) {
	next := anime.Ep.Number + 1
	if next > anime.TotalEpisodes {
		Log(fmt.Sprintf("Next episode %d exceeds total episodes %d, skipping prefetch", next, anime.TotalEpisodes))
		return
	}
	if userConfig.SkipFiller && IsFiller(anime.FillerEpisodes, anime.Ep.Number) {
		next = NextCanonEpisode(anime.FillerEpisodes, next)
	}
	// A copy, so the lookup cannot disturb the episode that is playing.
	upcoming := *anime
	result, err := ResolveEpisodeURL(*userConfig, &upcoming, next)
	if err != nil {
		Log(fmt.Sprintf("Error getting next episode link for ep %d: %v", next, err))
		return
	}
	anime.Ep.NextEpisode = NextEpisode{
		Number:       next,
		Links:        result.Links,
		ProviderName: result.ProviderName,
		ProviderId:   result.ProviderID,
		Mode:         result.Mode,
		LinkHints:    result.LinkHints,
	}
}

// announceEpisode says which episode is starting, with the show's cover when
// rofi has one cached for the notification.
func announceEpisode(userConfig *Config, anime *Anime, title string) {
	if anime.CoverImage == "" || !userConfig.ImagePreview || !userConfig.RofiSelection {
		Out(title)
		return
	}
	// The rofi grid has fetched the cover already, if it was shown there.
	cover, _ := coverCachePath(anime.CoverImage)
	if _, err := os.Stat(cover); err != nil {
		Log(fmt.Sprintf("Image does not exist at %s", cover))
		Out(title)
		return
	}
	Log(fmt.Sprintf("Image found at %s", cover))
	Out(imageNotice(cover, title))
}

// atLastKnownEpisode reports whether the episode playing is the show's last,
// as far as the total is known.
func atLastKnownEpisode(anime *Anime) bool {
	return anime.TotalEpisodes > 0 && anime.Ep.Number >= anime.TotalEpisodes
}

// nextEpisodeLabel is the yes option of a next-episode prompt: continueLabel,
// or finishing the series when there is no next episode.
func nextEpisodeLabel(anime *Anime, continueLabel string) string {
	if atLastKnownEpisode(anime) {
		return "Yes, finish series"
	}
	return continueLabel
}

// NextEpisodePromptCLI asks in the terminal, after an episode, whether to go
// on to the next one.
func NextEpisodePromptCLI(userConfig *Config) bool {
	anime := GetGlobalAnime()
	next := anime.Ep.Number + 1

	// Same reasoning as the rofi prompt: do not offer an episode that has not
	// been broadcast, since choosing it can only fail.
	if !atLastKnownEpisode(anime) {
		if availability := nextEpisodeAiring(anime, next); !availability.Aired {
			return confirmUnairedEpisode(userConfig, next, availability)
		}
		Out(fmt.Sprintf("Start next episode (%d)?", next))
	} else {
		Out("Finish this series?")
	}

	// The menu brings its own quit and back.
	answer, err := DynamicSelect([]SelectionOption{
		{Key: "yes", Label: nextEpisodeLabel(anime, fmt.Sprintf("Yes, continue to episode %d", next)), Icon: icons.Play},
	})
	if err != nil {
		Log(fmt.Sprintf("Error in CLI next episode prompt selection: %v", err))
		return false
	}
	Log(fmt.Sprintf("CLI User Selected Key: '%s', Label: '%s'", answer.Key, answer.Label))
	if answer.Key == "-1" || answer.Key == "-2" {
		Out("Exiting")
		return false
	}
	return answer.Key == "yes"
}

// NextEpisodePromptContinuous is the terminal's next-episode prompt, shown
// beside playback for the whole episode. Answering it decides what happens
// to the episode: moving on counts it as watched, and quitting counts it only
// past PercentageToMarkComplete.
func NextEpisodePromptContinuous(userConfig *Config, databaseFile string, userToken string) {
	anime := GetGlobalAnime()
	for {
		for !anime.Ep.Started {
			time.Sleep(time.Second)
		}

		if atLastKnownEpisode(anime) {
			Out("Finish this series or quit?")
		} else {
			Out(fmt.Sprintf("Continue to next episode (%d) or quit?", anime.Ep.Number+1))
		}
		answer, err := DynamicSelect([]SelectionOption{
			{Key: "yes", Label: nextEpisodeLabel(anime, "Yes, start next episode now"), Icon: icons.Play},
		})
		if err != nil {
			Log(fmt.Sprintf("Error in CLI continuous next episode prompt: %v", err))
			return
		}
		Log(fmt.Sprintf("CLI Continuous User Selected Key: '%s', Label: '%s'", answer.Key, answer.Label))

		switch answer.Key {
		case "-1", "-2":
			quitDuringEpisode(userConfig, anime, databaseFile, userToken)
			return
		case "yes":
			anime.Ep.IsCompleted = true
			socket := anime.Ep.Player.SocketPath
			if atLastKnownEpisode(anime) {
				ExitMPV(socket)
			}
			StartNextEpisode(anime, userConfig, databaseFile, userToken)
			// The main loop plays the next episode once this one's player
			// is gone.
			ExitMPV(socket)
			return
		}
	}
}

// quitDuringEpisode ends the run from the continuous prompt, saving the
// episode as watched when enough of it was.
func quitDuringEpisode(userConfig *Config, anime *Anime, databaseFile, userToken string) {
	watched := PercentageWatched(anime.Ep.Player.PlaybackTime, anime.Ep.Duration)
	if int(watched) < userConfig.PercentageToMarkComplete {
		Out(fmt.Sprintf("Episode not completed (%.1f%% watched). Exiting.", watched))
	} else {
		anime.Ep.IsCompleted = true
		if anime.Ep.Number == anime.TotalEpisodes {
			HandleLastEpisodeCompletion(userConfig, anime, userToken)
		}
		if err := LocalUpdateAnime(databaseFile, anime.AnilistId, anime.ProviderId, anime.Ep.Number, anime.Ep.Player.PlaybackTime, ConvertSecondsToMinutes(anime.Ep.Duration), GetAnimeName(*anime), CurrentAnimeProviderName(anime)); err != nil {
			Log("Error updating local database on quit: " + err.Error())
		}
		// Completing a rewatch above sets its progress along with it.
		if !(anime.TotalEpisodes > 0 && anime.Ep.Number == anime.TotalEpisodes && anime.Rewatching) {
			go func(id, episode int) {
				if err := UpdateAnimeProgress(userToken, id, episode); err != nil {
					Log("Error updating Anilist progress on quit: " + err.Error())
				}
			}(anime.AnilistId, anime.Ep.Number)
		}
		Out(fmt.Sprintf("Episode completed (%.1f%% watched). Exiting.", watched))
	}
	ExitMPV(anime.Ep.Player.SocketPath)
	Exit(nil)
}

// NextEpisodePromptRofi asks in rofi, after an episode, whether to go on to
// the next one.
func NextEpisodePromptRofi(userConfig *Config) bool {
	anime := GetGlobalAnime()
	next := anime.Ep.Number + 1

	// Say so rather than offering an episode that cannot be played: picking it
	// would search every provider and fail, which reads as a broken tool rather
	// than an episode that has not been broadcast.
	if !atLastKnownEpisode(anime) {
		if availability := nextEpisodeAiring(anime, next); !availability.Aired {
			return confirmUnairedEpisode(userConfig, next, availability)
		}
	}

	answer, err := DynamicSelect([]SelectionOption{
		{Key: "yes", Label: nextEpisodeLabel(anime, fmt.Sprintf("Yes, start episode %d", next)), Icon: icons.Play},
	})
	if err != nil {
		Log(fmt.Sprintf("Error in next episode prompt selection: %v", err))
		return false
	}
	Log(fmt.Sprintf("Rofi User Selected Key: '%s', Label: '%s'", answer.Key, answer.Label))
	return answer.Key == "yes"
}

// StartNextEpisode moves anime on from an episode just finished: it records
// the progress and readies the next episode for the playback loop, or ends
// the run after the show's last.
func StartNextEpisode(anime *Anime, userConfig *Config, databaseFile string, userToken string) {
	finished := anime.Ep.Number

	if anime.TotalEpisodes > 0 && finished == anime.TotalEpisodes {
		HandleLastEpisodeCompletion(userConfig, anime, userToken)
		if !anime.Rewatching {
			// UpdateAnimeProgress says so itself when it works.
			if err := UpdateAnimeProgress(userToken, anime.AnilistId, finished); err != nil {
				Log("Error updating Anilist progress: " + err.Error())
			}
		}
		Out("Series completed!")
		Exit(nil)
		return
	}

	anime.Ep.Number++
	if anime.TotalEpisodes > 0 && anime.Ep.Number > anime.TotalEpisodes {
		Out("Reached end of series")
		Exit(nil)
		return
	}

	if UsePrefetchedEpisode(anime) {
		Log(fmt.Sprintf("Using prefetched links for episode %d", anime.Ep.Number))
	} else {
		// Emptied so the playback loop fetches this episode's own.
		anime.Ep.Links = []string{}
		Log(fmt.Sprintf("No prefetched links available for episode %d, will fetch new ones", anime.Ep.Number))
	}

	// Reset episode flags. The skip times are the last episode's until this
	// one's are looked up, and skipping by them jumps somewhere arbitrary.
	anime.Ep.Started = false
	anime.Ep.IsCompleted = false
	anime.Ep.SkipTimes = SkipTimes{}
	Log("Completed episode, starting next.")

	if err := LocalUpdateAnime(databaseFile, anime.AnilistId, anime.ProviderId, anime.Ep.Number, 0, 0, GetAnimeName(*anime), CurrentAnimeProviderName(anime)); err != nil {
		Log("Error updating local database: " + err.Error())
	}
	// The next episode should not wait on the tracker.
	go func(id int) {
		if err := UpdateAnimeProgress(userToken, id, finished); err != nil {
			Log("Error updating Anilist progress: " + err.Error())
		}
	}(anime.AnilistId)

	Out(fmt.Sprint("Starting next episode: ", anime.Ep.Number))
}

// HandleLastEpisodeCompletion wraps up a show after its last episode: the
// score, the tracker status, and any sequel. It does nothing for any other
// episode.
func HandleLastEpisodeCompletion(userConfig *Config, anime *Anime, userToken string) {
	if anime.TotalEpisodes <= 0 || anime.Ep.Number != anime.TotalEpisodes {
		return
	}

	var summary []string
	canWriteRemote := ShouldWriteRemoteTracking(userConfig, anime)

	// One place decides whether a rating actually happened, so the menu, the
	// cast panel and the escape key cannot report it differently.
	//
	// A declined rating is not a failed one. RateAnime cannot tell them apart on
	// error alone, and conflating them is how "Anime rated successfully!" came to
	// be printed for a rating that was never written.
	applyRating := func() {
		score, err := rateAnimeScore(userToken, anime.AnilistId)
		switch {
		case errors.Is(err, ErrRatingDeclined):
			Out("Rating skipped.")
			summary = append(summary, "rating skipped")
		case err != nil:
			Log(fmt.Sprintf("Error rating anime: %v", err))
			Out("Failed to rate anime")
			summary = append(summary, "rating failed")
		default:
			Out("Anime rated successfully!")
			summary = append(summary, "rating saved")
			if score >= 0 {
				offerRatingShare(userConfig, anime, score)
			}
		}
	}

	if userConfig.ScoreOnCompletion && !anime.IsAiring && canWriteRemote {
		Out("You've completed this anime! Would you like to rate it?")

		if castWindowNonInteractive() {
			// Asked in the panel rather than answered for the viewer, because the
			// alternative is a countdown whose only outcomes are "write a rating
			// nobody chose" and "write nothing at all". Arrows and one key work
			// from a sofa; typing a number does not. See
			// wiki/Developer-Cast-Window-Prompts.md.
			if score, given := castAwaitScore(userConfig, anime, castCountdownDuration); given {
				if err := RateAnimeWithScore(userToken, anime.AnilistId, float64(score)); err != nil {
					Log(fmt.Sprintf("Error rating anime: %v", err))
					castPanelSay(userConfig, anime, "Rating failed.")
					summary = append(summary, "rating failed")
				} else {
					castPanelSay(userConfig, anime, fmt.Sprintf("Rated %d.", score))
					summary = append(summary, fmt.Sprintf("rating saved (%d)", score))
				}
			} else {
				castPanelSay(userConfig, anime, "No answer -- rating skipped.")
				summary = append(summary, "rating skipped (cast window)")
			}
		} else {
			scoreOptions := []SelectionOption{
				{Key: "yes", Label: "Yes, rate this anime", Icon: icons.Star},
				{Key: "no", Label: "No, skip rating", Icon: icons.No},
			}

			selectedOption, err := DynamicSelect(scoreOptions)
			// Back (-2) and no are treated as skip; a failed menu says
			// nothing.
			switch {
			case err != nil:
				Log(fmt.Sprintf("Error in score prompt selection: %v", err))
			case selectedOption.Key == "yes":
				applyRating()
			default:
				summary = append(summary, "rating skipped")
			}
		}
	} else {
		summary = append(summary, "rating skipped")
	}

	if !anime.IsAiring {
		if canWriteRemote {
			summary = append(summary, markShowCompleted(anime, userToken))
		} else {
			summary = append(summary, "tracker updates skipped")
		}
	}

	if sequelSummary := handleSequelCheck(userConfig, anime, userToken); sequelSummary != "" {
		summary = append(summary, sequelSummary)
	}
	if len(summary) > 0 {
		line := "Completion summary: " + strings.Join(summary, "; ")
		// A cast window holding the terminal would turn this into a desktop
		// notification, and this is the one line a viewer comes back to. Held
		// until the session releases the screen, then printed to the terminal
		// where it lands in the scrollback.
		emitCompletionSummary(line)
	}
}

// markShowCompleted moves a finished show to Completed on the tracker, which
// for a rewatch also counts the rewatch, and returns its summary entry.
func markShowCompleted(anime *Anime, userToken string) string {
	if anime.Rewatching {
		if err := CompleteAnimeRewatch(userToken, *anime); err != nil {
			Log("Error completing anime rewatch: " + err.Error())
			return "rewatch completion failed"
		}
		return "rewatch completed"
	}
	if err := UpdateAnimeStatus(userToken, anime.AnilistId, "COMPLETED"); err != nil {
		Log("Error updating anime status to completed: " + err.Error())
		return "status update failed"
	}
	return "marked completed"
}

// emitCompletionSummary sends the completion summary wherever it can actually be
// read: held for the session when a cast panel owns the terminal, printed
// otherwise.
//
// A cast window holding the terminal would turn Out into a desktop notification,
// and this is the one line a viewer comes back to. Split out of
// HandleLastEpisodeCompletion so the choice is testable: a test that calls
// deferCastSummary itself passes whether or not this branch exists at all.
func emitCompletionSummary(line string) {
	if castPanelOwnsScreen() {
		deferCastSummary(line)
		return
	}
	Out(line)
}

// handleSequelCheck offers what to do about a finished show's sequel, and
// returns its summary entry.
func handleSequelCheck(userConfig *Config, anime *Anime, userToken string) (summary string) {
	if !ShouldWriteRemoteTracking(userConfig, anime) {
		return "sequel check skipped"
	}

	// The show is already finished and saved; a failure here must not
	// take the rest of the wrap-up down with it.
	defer func() {
		if r := recover(); r != nil {
			Log(fmt.Sprintf("Recovered from panic in handleSequelCheck: %v", r))
			summary = "sequel check failed"
		}
	}()

	sequels, err := GetAnimeSequel(anime.AnilistId, userToken)
	if err != nil {
		Log(fmt.Sprintf("Error fetching sequel information: %v", err))
		return "sequel check failed"
	}
	if len(sequels) == 0 {
		Log("No sequel found for this anime")
		return "no sequel found"
	}

	// Declined whole, ahead of both the "which one" and the "what do you want to
	// do with it" menus: a further instalment is a different show to put on a
	// list, not a continuation of this one, and it is not a decision to make on
	// someone's behalf while they are not there to make it. Reported rather than
	// dropped -- see wiki/Developer-Cast-Window-Prompts.md.
	if castWindowNonInteractive() {
		title := sequelDisplayTitle(userConfig, &sequels[0])
		Out(fmt.Sprintf("Sequel available: %s. Leaving your list alone.", title))
		return "sequel skipped (cast window)"
	}

	sequel := &sequels[0]
	if len(sequels) > 1 {
		picked, ok := selectSequel(userConfig, sequels)
		if !ok {
			return "sequel skipped"
		}
		sequel = picked
	}
	return promptSequelAction(userConfig, sequel, userToken)
}

// selectSequel asks which of several sequels to act on.
func selectSequel(userConfig *Config, sequels []SequelInfo) (*SequelInfo, bool) {
	Out("Multiple sequels found.")
	options := make([]SelectionOption, 0, len(sequels)+1)
	for i := range sequels {
		options = append(options, SelectionOption{
			Key:   strconv.Itoa(i),
			Label: fmt.Sprintf("%s (%s)", sequelDisplayTitle(userConfig, &sequels[i]), sequelStatusText(sequels[i].Status)),
		})
	}
	options = append(options, SelectionOption{Key: "skip", Label: "Skip", Icon: icons.Skip})

	picked, err := promptSelect(options)
	if err != nil {
		Log(fmt.Sprintf("Error selecting sequel: %v", err))
		return nil, false
	}
	// Skip, quit and back all leave the sequel alone, as does anything that
	// is not one of the rows.
	index, err := strconv.Atoi(picked.Key)
	if err != nil || index < 0 || index >= len(sequels) {
		return nil, false
	}
	return &sequels[index], true
}

// sequelDisplayTitle is a sequel's name in the language the config prefers.
func sequelDisplayTitle(userConfig *Config, sequel *SequelInfo) string {
	switch {
	case sequel == nil:
		return ""
	case userConfig != nil && userConfig.AnimeNameLanguage == "english" && sequel.Title.English != "":
		return sequel.Title.English
	case sequel.Title.Romaji != "":
		return sequel.Title.Romaji
	}
	return strconv.Itoa(sequel.ID)
}

// sequelStatusText is an airing status as words, e.g. "not yet released".
func sequelStatusText(status string) string {
	return strings.ToLower(strings.ReplaceAll(status, "_", " "))
}

// sequelLists are the lists a sequel can be added to from the prompt.
var sequelLists = map[string]struct{ status, name string }{
	"watching": {"CURRENT", "Watching"},
	"planning": {"PLANNING", "Plan to Watch"},
}

// promptSequelAction offers to put sequel on a list or open its page, and
// returns its summary entry.
func promptSequelAction(userConfig *Config, sequel *SequelInfo, userToken string) string {
	if sequel == nil {
		return ""
	}
	title := sequelDisplayTitle(userConfig, sequel)
	Log(fmt.Sprintf("Found sequel: %s (ID: %d)", title, sequel.ID))

	var status string
	var listed bool
	if user := GetGlobalUser(); user != nil && hasAnyEntries(user.AnimeList) {
		status, listed = FindSequelInAnimeList(user.AnimeList, sequel.ID)
	}
	if listed {
		Out(fmt.Sprintf("Sequel found: %s (%s)", title, status))
	} else {
		Out(fmt.Sprintf("Sequel found: %s", title))
	}

	// Only what would change something: a sequel already being watched is
	// not offered Watching, nor one already planned Plan to Watch.
	var options []SelectionOption
	startable := !listed || status == "PLANNING" || status == "PAUSED" || status == "DROPPED"
	if sequel.Status != "NOT_YET_RELEASED" && startable {
		options = append(options, SelectionOption{Key: "watching", Label: "Add to Watching", Icon: icons.Watching})
	}
	if !listed || status != "PLANNING" {
		options = append(options, SelectionOption{Key: "planning", Label: "Add to Plan to Watch", Icon: icons.Planning})
	}
	options = append(options,
		SelectionOption{Key: "details", Label: "Open AniList details", Icon: icons.Info},
		SelectionOption{Key: "skip", Label: "Skip", Icon: icons.Skip},
	)

	picked, err := promptSelect(options)
	if err != nil {
		Log(fmt.Sprintf("Error in sequel action prompt: %v", err))
		return "sequel action failed"
	}

	if list, ok := sequelLists[picked.Key]; ok {
		if err := AddAnimeToList(sequel.ID, list.status, userToken); err != nil {
			Log(fmt.Sprintf("Error adding sequel to %s: %v", list.name, err))
			Out(fmt.Sprintf("Failed to add sequel to %s.", list.name))
			return "sequel add failed"
		}
		Out(fmt.Sprintf("Added '%s' to %s.", title, list.name))
		return "sequel added to " + list.name
	}
	switch picked.Key {
	case "details":
		page := sequel.SiteURL
		if page == "" {
			page = aniListPage(sequel.ID)
		}
		if !openInDefaultBrowser(page) {
			return "sequel details failed"
		}
		return "sequel details opened"
	case "skip", "-1", "-2":
		Log("User skipped sequel action")
	}
	return "sequel skipped"
}

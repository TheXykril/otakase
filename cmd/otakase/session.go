package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/thexykril/otakase/internal"
)

// session is one interactive run: the show picked at launch and every
// episode played from it after that.
type session struct {
	cfg          *internal.Config
	anime        *internal.Anime
	user         *internal.User
	databaseFile string

	// download saves the episodes asked for with -episodes instead of
	// playing them.
	download bool
	episodes string
}

// start signs in, picks the show and readies it for the first episode. It
// reports false when the run ended along the way.
func (s *session) start(partyArg string, addNew bool) bool {
	// Said before anything slow: the update check below can hold the first
	// menu back, and a keybind launch otherwise shows nothing until then.
	internal.AnnounceLaunch(s.cfg)

	// Look for a newer release now, alongside the tracker sign-in below; the
	// result is offered before the first menu of this same run.
	internal.StartBackgroundUpdateCheck(s.cfg, buildVersion())
	internal.StartDiscordBackground(s.cfg)

	// From here on the launch can block on the network, with no terminal to show
	// it when otakase was started from a keybind.
	internal.BeginStartupProgress(s.cfg, "Otakase is starting")

	if internal.UsesRemoteTracking(s.cfg) {
		internal.StartupStage("Signing in to your tracker")
		if err := internal.EnsureConfiguredTrackersReady(s.cfg, s.user); err != nil {
			internal.Log("Error preparing trackers: " + err.Error())
			internal.Exit(err)
		}
	}

	s.databaseFile = internal.HistoryFile(s.cfg.StoragePath)
	history := internal.LocalGetAllAnime(s.databaseFile)

	// A synced watch party: a member follows the host and is done after it;
	// the host goes on to pick the show and episode as usual.
	if partyArg != "" {
		internal.EndStartupProgress()
		hosting, partyErr := internal.StartWatchParty(s.cfg, s.databaseFile, partyArg)
		if partyErr != nil {
			internal.Out("Couldn't join the watch party: " + partyErr.Error())
			internal.Exit(nil)
			return false
		}
		if !hosting {
			internal.Exit(nil)
			return false
		}
		internal.BeginStartupProgress(s.cfg, "Otakase is starting")
	}

	if addNew {
		internal.AddNewAnime(s.cfg, s.anime, s.user, &history)
	}

	if internal.OfferCrashReport(s.cfg) {
		internal.BeginStartupProgress(s.cfg, "Otakase is starting")
	}

	// April 12: letters to open, a community message, next year's letter.
	if internal.OfferBirthdayCapsule(s.cfg) {
		internal.BeginStartupProgress(s.cfg, "Otakase is starting")
	}

	if prompted, exit := internal.HandleLaunchUpdatePrompt(s.cfg, buildVersion()); exit {
		internal.Exit(nil)
		return false
	} else if prompted {
		// The prompt was a menu, which ends the startup progress; the list
		// load ahead can still be slow.
		internal.BeginStartupProgress(s.cfg, "Otakase is starting")
	}

	internal.StartupStage("Loading your anime list")
	internal.Setup(s.cfg, s.anime, s.user, &history)

	noteRewatch(s.anime, s.user)
	s.anime.Ep.Player.Speed = 1.0
	if s.cfg.DiscordPresence {
		internal.Out("Starting Discord broadcast.")
	}

	// Needed only once an episode is checked for filler, so the first one
	// does not wait for it.
	go s.fetchFillerList()
	return true
}

func (s *session) fetchFillerList() {
	if s.anime.MalId == 0 {
		malID, err := internal.GetAnimeMalID(s.anime.AnilistId)
		if err != nil {
			internal.Log("Error getting MAL ID: " + err.Error())
			return
		}
		s.anime.MalId = malID
	}

	fillers, err := internal.FillerEpisodes(s.anime.MalId)
	if err != nil {
		internal.Log("Error getting filler list: " + err.Error())
		return
	}
	s.anime.FillerEpisodes = fillers
	internal.Log("Filler list fetched successfully")
}

// run plays episode after episode. It only returns once the run is over;
// most endings leave through internal.Exit instead.
func (s *session) run() {
	for {
		internal.Log(*s.anime)
		s.lookUpShowIDs()
		s.skipUnwantedEpisodes()

		// Downloading reuses everything above -- tracker selection, provider
		// mapping, episode resolution -- and simply saves the stream instead of
		// handing it to MPV.
		if s.download {
			s.downloadEpisodes()
			return
		}
		s.playEpisode()
	}
}

// lookUpShowIDs fills in the MyAnimeList id, which filler data and skip times
// are keyed by, and the cover Discord shows.
func (s *session) lookUpShowIDs() {
	var err error
	if s.cfg.DiscordPresence {
		s.anime.MalId, s.anime.CoverImage, err = internal.GetAnimeIDAndImage(s.anime.AnilistId)
		if err != nil {
			internal.Log("Error getting anime ID and image: " + err.Error())
		}
		// No presence yet: until mpv reports the real duration it would show
		// a made-up one.
		internal.Log("Waiting for MPV to start to get actual video duration before showing Discord presence")
		return
	}
	if s.anime.MalId == 0 {
		s.anime.MalId, err = internal.GetAnimeMalID(s.anime.AnilistId)
		if err != nil {
			internal.Log("Error getting anime MAL ID: " + err.Error())
		}
	}
}

// skippedEpisode reports whether the config wants this episode passed over.
func skippedEpisode(ep internal.Episode, cfg *internal.Config) bool {
	return (ep.IsFiller && cfg.SkipFiller) || (ep.IsRecap && cfg.SkipRecap)
}

// skipUnwantedEpisodes moves past filler and recap episodes the config skips,
// recording each move so a restart does not land on them again.
func (s *session) skipUnwantedEpisodes() {
	a := s.anime
	for {
		internal.LoadEpisodeDetails(a, a.MalId, a.Ep.Number)
		a.Ep.IsFiller = internal.IsFiller(a.FillerEpisodes, a.Ep.Number)

		if !skippedEpisode(a.Ep, s.cfg) {
			// Episodes were skipped to get here, so they count as watched.
			if a.Ep.LastWasSkipped {
				go internal.UpdateAnimeProgress(s.user.Token, a.AnilistId, a.Ep.Number-1)
			}
			return
		}

		if a.Ep.IsFiller {
			internal.Out(fmt.Sprint("Filler episode, skipping: ", a.Ep.Number))
			a.Ep.Number = internal.NextCanonEpisode(a.FillerEpisodes, a.Ep.Number)
		} else {
			internal.Out(fmt.Sprint("Recap episode, skipping: ", a.Ep.Number))
			a.Ep.Number++
		}
		a.Ep.LastWasSkipped = true
		a.Ep.Started = false
		internal.LocalUpdateAnime(s.databaseFile, a.AnilistId, a.ProviderId, a.Ep.Number, 0, 0, internal.GetAnimeName(*a), internal.CurrentAnimeProviderName(a))

		if a.TotalEpisodes > 0 && a.Ep.Number > a.TotalEpisodes {
			internal.Out("Reached end of series")
			internal.Exit(nil)
		}
	}
}

func (s *session) downloadEpisodes() {
	from, to, rangeErr := internal.ParseEpisodeRange(s.episodes, s.anime.Ep.Number)
	if rangeErr != nil {
		internal.Out(rangeErr.Error())
		internal.Exit(rangeErr)
	}

	dir := internal.ResolveDownloadDir(s.cfg)
	internal.Out(fmt.Sprintf("Downloading episodes %d-%d to %s", from, to, dir))

	results := internal.DownloadEpisodes(*s.cfg, s.anime, from, to, dir)
	failed := 0
	for _, result := range results {
		if result.Err != nil {
			failed++
		}
	}
	internal.Out(fmt.Sprintf("Downloaded %d of %d episode(s).", len(results)-failed, len(results)))
	if failed > 0 {
		internal.Exit(fmt.Errorf("%d episode(s) failed to download", failed))
	}
	internal.Exit(nil)
}

// playEpisode plays the current episode and leaves the session ready for the
// next one.
func (s *session) playEpisode() {
	socket := internal.StartPlayback(s.cfg, s.anime)
	s.anime.Ep.Player.SocketPath = socket
	internal.Log(fmt.Sprint("Playback starting time: ", s.anime.Ep.Player.PlaybackTime))
	internal.Log(socket)

	// StartPlayback reports "could not start playback" by returning an empty
	// socket path, having already said why. Continuing past it started the
	// playback watchers for a session that does not exist, and they then
	// polled a socket that would never answer -- once a second, forever.
	// A cast played the episode somewhere else and is finished, the same
	// shape as the android-intent case below: carry on rather than poll a
	// socket that will never exist. The literal matches what
	// internal.castSocketSentinel returns; main is a different package and
	// cannot see it, which is how the android-intent sentinel is written
	// here too.
	switch socket {
	case "cast":
		// The countdown inside CastEpisode already asked, and this branch
		// is only reached when it said advance: asking again would put an
		// interactive menu in front of a viewer across the room.
		if !internal.AdvanceAfterEpisode(s.cfg, s.anime, s.user, s.databaseFile, func() bool { return true }) {
			internal.Exit(nil)
		}
		return
	case "":
		internal.Log("Playback did not start; no MPV socket")
		internal.Exit(nil)
		return
	case "android-intent":
		s.awaitExternalPlayer()
		return
	}

	// After playback is running, build the mpv episode playlist and audio
	// options while it is idle: no startup cost and no stutter mid-buffer.
	w := newEpisodeWatch(s)
	internal.StartPlaylistController(s.cfg, s.anime, socket, w.done)
	internal.StartDiscordMPVWatch(s.cfg, s.anime, socket, w.done)
	w.run()
	s.afterEpisode()
}

// awaitExternalPlayer waits out an episode handed to an Android player, which
// otakase cannot watch, so the viewer says when it is over.
func (s *session) awaitExternalPlayer() {
	internal.Out(fmt.Sprintf("\nOpened external player for Episode %d.", s.anime.Ep.Number))
	internal.Out("Press Enter when you have finished watching...")

	// Wait for user input to confirm completion. A stdin that has
	// ended is not someone pressing enter, and this loop starts the
	// next episode each time round.
	if !internal.AwaitEnter() {
		internal.Exit(nil)
	}

	// The same call the spawned cast process makes, so a rofi cast and
	// a local playback advance through one implementation rather than
	// two that can drift. Unlike a cast, nothing has asked yet here, so
	// this asks with the same menu local playback always has.
	if internal.AdvanceAfterEpisode(s.cfg, s.anime, s.user, s.databaseFile, func() bool {
		return internal.NextEpisodePromptCLI(s.cfg)
	}) {
		return
	}
	internal.Exit(nil)
}

// afterEpisode settles the tracker once an episode has stopped and moves on
// to the next one, or ends the run when the show is finished.
func (s *session) afterEpisode() {
	a := s.anime
	if a.TotalEpisodes > 0 && a.Ep.Number > a.TotalEpisodes {
		internal.Out("Reached end of series")
		internal.Exit(nil)
	}

	if a.Ep.IsCompleted && !a.Rewatching {
		s.recordCompletedEpisode()
	}
	// A rewatch writes no score, so it ends here whatever the config says.
	if a.Rewatching && a.Ep.IsCompleted && a.Ep.Number-1 == a.TotalEpisodes {
		a.Ep.Number--
		internal.Out("Completed anime. (Rewatching so no scoring)")
		internal.LocalDeleteAnime(s.databaseFile, a.AnilistId, a.ProviderId)
		internal.Exit(nil)
	}

	if a.Ep.IsCompleted {
		if !s.cfg.NextEpisodePrompt {
			internal.StartNextEpisode(a, s.cfg, s.databaseFile, s.user.Token)
			return
		}
		// With the prompt on, the viewer already answered it, either from the
		// CLI prompt running beside playback or from rofi after it.
		if !s.cfg.RofiSelection {
			internal.Out("CLI mode: continuous prompt handling next episode logic")
		}
	}

	// The next episode's links are fetched during playback; give a fetch
	// still in flight a few seconds before starting another.
	for range 5 {
		if internal.UsePrefetchedEpisode(a) {
			internal.Log("Using prefetched next episode link")
			break
		}
		time.Sleep(time.Second)
	}

	if len(a.Ep.Links) == 0 {
		links, _, err := internal.GetEpisodeURLForPlayback(*s.cfg, a.ProviderId, a.Ep.Number)
		if err != nil {
			internal.Log("Failed to get episode links: " + err.Error())
			internal.Out("Failed to get episode links. Try again later.")
			internal.Exit(fmt.Errorf("failed to get episode links: %v", err))
			return
		}
		a.Ep.Links = links
	}
	if len(a.Ep.Links) == 0 {
		internal.Out("No episode links found. Try again later.")
		internal.Exit(errors.New("no episode links found"))
	}
}

// recordCompletedEpisode writes the progress of the episode just finished,
// which the episode number has already moved past, and ends the run with a
// score when it was the show's last.
func (s *session) recordCompletedEpisode() {
	a := s.anime
	token := s.user.Token
	finished := a.Ep.Number - 1

	if a.TotalEpisodes > 0 && finished != a.TotalEpisodes {
		// Nothing waits on it mid-show.
		go func(id int) {
			if err := internal.UpdateAnimeProgress(token, id, finished); err != nil {
				internal.Log("Error updating Anilist progress: " + err.Error())
			}
		}(a.AnilistId)
	} else {
		internal.ExitMPV(a.Ep.Player.SocketPath)
		if err := internal.UpdateAnimeProgress(token, a.AnilistId, finished); err != nil {
			internal.Log("Error updating Anilist progress: " + err.Error())
		}
	}
	a.Ep.IsCompleted = false

	// A show still airing is not finished however many episodes it has so
	// far, so the tracker is asked before a score is.
	if finished != a.TotalEpisodes || !s.cfg.ScoreOnCompletion || a.TotalEpisodes <= 0 {
		return
	}
	latest, err := internal.GetAnimeDataByID(a.AnilistId, token)
	if err != nil {
		internal.Log("Error getting updated anime data: " + err.Error())
		return
	}
	if latest.IsAiring {
		return
	}
	a.Ep.Number = finished
	internal.Out("Completed anime.")
	// A decline is the viewer's own choice, so it is not reported
	// back to them as an error.
	if rateErr := internal.RateAnime(token, a.AnilistId); errors.Is(rateErr, internal.ErrRatingDeclined) {
		internal.Out("Score unchanged.")
	} else if rateErr != nil {
		internal.Log("Error rating anime: " + rateErr.Error())
		internal.Out("Error rating anime: " + rateErr.Error())
	}
	internal.LocalDeleteAnime(s.databaseFile, a.AnilistId, a.ProviderId)
	internal.Exit(nil)
}

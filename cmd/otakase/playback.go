package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/thexykril/otakase/internal"
)

// episodeWatch follows one episode playing in mpv: it skips the opening and
// ending, keeps the watch history and Discord up to date, and notices when
// the episode is over.
type episodeWatch struct {
	*session
	socket string

	// done is closed once the episode is over, which stops every watcher
	// started for it. stop may be called from any of them, any number of
	// times.
	done chan struct{}
	stop func()

	// wg covers the watchers the next episode has to wait for; the rest
	// end on their own.
	wg sync.WaitGroup
}

func newEpisodeWatch(s *session) *episodeWatch {
	done := make(chan struct{})
	return &episodeWatch{
		session: s,
		socket:  s.anime.Ep.Player.SocketPath,
		done:    done,
		stop:    sync.OnceFunc(func() { close(done) }),
	}
}

// stopped reports whether the episode is over, without waiting.
func (w *episodeWatch) stopped() bool {
	select {
	case <-w.done:
		return true
	default:
		return false
	}
}

// run starts the watchers and returns once the episode is over and the ones
// that matter to the next episode have finished.
func (w *episodeWatch) run() {
	w.wg.Add(3)
	go func() {
		defer w.wg.Done()
		w.checkEpisodeDetails()
	}()
	go func() {
		defer w.wg.Done()
		w.updateDiscordPresence()
	}()
	go w.applySkipTimes()
	go w.readDuration()
	go w.promptForNextEpisode()
	go func() {
		defer w.wg.Done()
		w.trackPlayback()
	}()

	w.skipAndFollowSpeed()
	w.wg.Wait()
}

// mpvGet reads one mpv property.
func (w *episodeWatch) mpvGet(property string) (any, error) {
	return internal.MPVSendCommand(w.socket, []any{"get_property", property})
}

// roundSeconds turns a position mpv reports into whole seconds.
func roundSeconds(seconds float64) int {
	return int(seconds + 0.5)
}

// mpvPosition is the playback position in whole seconds, 0 when mpv has none.
func (w *episodeWatch) mpvPosition() int {
	value, err := w.mpvGet("time-pos")
	if err != nil {
		return 0
	}
	if seconds, ok := value.(float64); ok {
		return roundSeconds(seconds)
	}
	return 0
}

// cliPrompt reports whether the terminal's next-episode prompt runs beside
// playback. It then decides what happens when the episode ends, so the other
// watchers leave that to it.
func (w *episodeWatch) cliPrompt() bool {
	return w.cfg.NextEpisodePrompt && !w.cfg.RofiSelection
}

// checkEpisodeDetails looks the episode up once more now that it is playing:
// filler data may have arrived since it was started, and one that should
// have been skipped is ended here.
func (w *episodeWatch) checkEpisodeDetails() {
	a := w.anime
	internal.LoadEpisodeDetails(a, a.MalId, a.Ep.Number)
	// if filler episode or recap episode and skip is enabled
	if !skippedEpisode(a.Ep, w.cfg) {
		return
	}
	if a.Ep.IsFiller && w.cfg.SkipFiller {
		internal.Out(fmt.Sprint("Filler Episode, starting next episode: ", a.Ep.Number+1))
		internal.Log("Filler episode detected")
	} else {
		internal.Out(fmt.Sprint("Recap Episode, starting next episode: ", a.Ep.Number+1))
		internal.Log("Recap episode detected")
	}

	a.Ep.IsCompleted = true
	if w.cfg.NextEpisodePrompt {
		// When NextEpisodePrompt is enabled, just call StartNextEpisode - it handles Rofi prompting internally
		internal.ExitMPV(w.socket)
		internal.StartNextEpisode(a, w.cfg, w.databaseFile, w.user.Token)
		return
	}
	internal.StartNextEpisode(a, w.cfg, w.databaseFile, w.user.Token)
	if _, err := internal.MPVSendCommand(w.socket, []any{"quit"}); err != nil {
		internal.Log("Error closing MPV: " + err.Error())
	}
	w.stop()
}

// presenceState decides when Discord is told about playback. Only what a
// viewer would notice is sent -- a pause or resume, a seek -- plus a refresh
// every 30 seconds to keep the activity alive.
type presenceState struct {
	paused   bool
	position int
	lastSent time.Time
	// ready is set once mpv has reported the real duration. Until then
	// nothing is sent, because the time bar would show a made-up length.
	ready bool
}

// observe takes the latest playback state and reports whether to send it,
// whether it was a seek, and the duration to show.
func (p *presenceState) observe(position int, paused bool, duration int, now time.Time) (send, seek bool, total int) {
	if p.position > 0 {
		// Normal playback plus a buffer stall moves the position by a few
		// seconds between polls; more than that, either way, was a seek.
		moved := position - p.position
		seek = moved < -5 || moved > 7
	}
	p.position = position

	due := p.lastSent.IsZero() || now.Sub(p.lastSent) >= 30*time.Second
	if !due && !seek && paused == p.paused {
		return false, false, 0
	}

	switch {
	case duration > 0:
		p.ready = true
		total = duration
	case !p.ready:
		p.sent(paused, now)
		return false, false, 0
	default:
		// Known once and gone now: a duration just past the position
		// keeps Discord from dividing by zero.
		total = position + 1
	}
	return true, seek, total
}

// sent records what Discord was last told.
func (p *presenceState) sent(paused bool, at time.Time) {
	p.paused = paused
	p.lastSent = at
}

func (w *episodeWatch) updateDiscordPresence() {
	if !w.cfg.DiscordPresence {
		return
	}
	var state presenceState
	for !w.stopped() {
		pauseValue, err := w.mpvGet("pause")
		if err != nil {
			// Without this the loop retried a dead socket every 5s
			// forever, logging the same error each time.
			if internal.MPVConnectionGone(err) {
				internal.Log("MPV is gone, stopping Discord presence updates")
				return
			}
			internal.Log("Error getting pause status: " + err.Error())
			time.Sleep(5 * time.Second)
			continue
		}

		position := w.mpvPosition()
		// An unanswered pause state is shown as paused.
		paused := true
		if pauseValue != nil {
			if value, ok := pauseValue.(bool); ok {
				paused = value
			} else {
				internal.Log(fmt.Sprintf("Error: pause state is not a bool (%T)", pauseValue))
			}
		}

		if send, seek, total := state.observe(position, paused, w.anime.Ep.Duration, time.Now()); send {
			var presenceErr error
			if seek {
				// Forced, or Discord's own filtering can drop it as a
				// repeat of the last update.
				presenceErr = internal.DiscordPresenceWithForce(*w.anime, paused, position, total, w.cfg.DiscordClientId, true)
			} else {
				presenceErr = internal.DiscordPresence(*w.anime, paused, position, total, w.cfg.DiscordClientId)
			}
			if presenceErr != nil {
				internal.Log("Error setting Discord presence: " + presenceErr.Error())
			}
			state.sent(paused, time.Now())
		}
		time.Sleep(2 * time.Second)
	}
}

// applySkipTimes looks the skip times up beside playback.
//
// Through the whole chain -- the provider's own timings, AniSkip,
// Anime-Skip -- rather than AniSkip alone. This episode used to ask
// only AniSkip while every later one in the playlist asked everything,
// so a show AniSkip does not cover started unskipped and then began
// skipping an episode later.
func (w *episodeWatch) applySkipTimes() {
	internal.SendMPVSkinState(w.cfg, w.anime, w.socket, true)
	resolution := internal.ApplySkipTimes(w.anime, w.anime.Ep.Number, w.cfg, internal.GetProvider())
	internal.Log(w.anime.Ep.SkipTimes)
	internal.StartSkipMarker(w.cfg, w.anime, w.socket, resolution.IDs, w.done)
}

// readDuration waits for the episode to start and asks mpv how long it is.
func (w *episodeWatch) readDuration() {
	for !w.anime.Ep.Started {
		time.Sleep(time.Second)
	}
	// Asked every time playback starts, not only when the duration is
	// still unset: Setup now seeds Ep.Duration with the tracker's average
	// episode length, so the cast panel has a total to show before
	// anything has been measured. mpv reports the real length of the file
	// in hand and has to win over that estimate.
	value, err := w.mpvGet("duration")
	if err != nil {
		internal.Log("Error getting video duration: " + err.Error())
		return
	}
	if value == nil {
		return
	}
	duration, ok := value.(float64)
	if !ok {
		internal.Log("Error: duration is not a float64")
		return
	}
	w.anime.Ep.Duration = roundSeconds(duration)
	internal.Log(fmt.Sprintf("Video duration: %d seconds", w.anime.Ep.Duration))

	// Initialize Discord presence with correct duration (first time with real duration)
	if !w.cfg.DiscordPresence {
		return
	}
	pauseValue, _ := w.mpvGet("pause")
	position := w.mpvPosition()
	paused := false
	if pauseValue != nil {
		if value, ok := pauseValue.(bool); ok {
			paused = value
		} else {
			internal.Log(fmt.Sprintf("Error: pause state is not a bool (%T)", pauseValue))
		}
	}
	internal.Log("Initializing Discord presence with real video duration")
	if presenceErr := internal.DiscordPresence(*w.anime, paused, position, w.anime.Ep.Duration, w.cfg.DiscordClientId); presenceErr != nil {
		internal.Log("Discord presence error: " + presenceErr.Error())
	}
}

// promptForNextEpisode runs the terminal's next-episode prompt for as long as
// the episode plays; its answer ends the episode.
func (w *episodeWatch) promptForNextEpisode() {
	if !w.cliPrompt() {
		return
	}
	internal.NextEpisodePromptContinuous(w.cfg, w.databaseFile, w.user.Token)
	w.stop()
}

// trackPlayback polls mpv once a second, saving the position to the watch
// history, until the episode ends one way or another.
func (w *episodeWatch) trackPlayback() {
	a := w.anime
	for !w.stopped() {
		time.Sleep(time.Second)

		position, err := w.mpvGet("time-pos")
		if err != nil {
			internal.Log("Error getting playback time: " + err.Error())

			// Nothing will ever answer a socket for an episode that never
			// began. The bail-out below is reached only in CLI mode, so a
			// rofi user whose playback failed to start polled forever.
			if !a.Ep.Started && internal.MPVConnectionGone(err) &&
				!internal.IsMPVRunning(w.socket) {
				internal.Log("MPV never started, stopping playback time updates")
				return
			}

			if w.cliPrompt() {
				// ...but only while MPV is still there. Once it has exited,
				// continuing just re-polls a dead socket every second.
				if internal.MPVConnectionGone(err) && !internal.IsMPVRunning(w.socket) {
					internal.Log("MPV is gone, stopping playback time updates")
					return
				}
				continue
			}

			if a.Ep.Started {
				if w.playbackLost(true) {
					return
				}
				continue
			}
		}

		if position != nil && !w.savePosition(position) {
			continue
		}

		if !w.stillPlaying() && w.playbackLost(false) {
			return
		}
	}
}

// savePosition takes a position mpv reported, starting the episode's
// per-file setup the first time one arrives. It reports false when the value
// was not a position.
func (w *episodeWatch) savePosition(value any) bool {
	a := w.anime
	if !a.Ep.Started {
		a.Ep.Started = true
		if w.cfg.SaveMpvSpeed {
			if _, err := internal.MPVSendCommand(w.socket, []any{"set_property", "speed", a.Ep.Player.Speed}); err != nil {
				internal.Log("Error setting playback speed: " + err.Error())
			}
		}
		// The opening and ending as mpv chapters.
		if err := internal.SendSkipTimesToMPV(a); err != nil {
			internal.Log("Error sending skip times to MPV: " + err.Error())
		}

		// The provider's other subtitle languages, for mpv's
		// own subtitle key.
		internal.OfferAlternateSubtitles(a)
	}

	// Seek once, as soon as there is a file to seek in.
	if a.Ep.Resume {
		internal.SeekMPV(w.socket, a.Ep.Player.PlaybackTime)
		a.Ep.Resume = false
	}

	seconds, ok := value.(float64)
	if !ok {
		internal.Log("Error: timePos is not a float64")
		return false
	}
	a.Ep.Player.PlaybackTime = roundSeconds(seconds)
	if err := internal.LocalUpdateAnime(w.databaseFile, a.AnilistId, a.ProviderId, a.Ep.Number, a.Ep.Player.PlaybackTime, internal.ConvertSecondsToMinutes(a.Ep.Duration), internal.GetAnimeName(*a), internal.CurrentAnimeProviderName(a)); err != nil {
		internal.Log("Error updating local database: " + err.Error())
	}
	internal.NoteSyncedResume(w.cfg, a)
	internal.NoteSubtitleChoice(w.cfg, a)
	return true
}

// hasPlayback asks mpv whether anything is loaded.
func (w *episodeWatch) hasPlayback() (bool, error) {
	playing, err := internal.HasActivePlayback(w.socket)
	// A gone connection is not an inconclusive error: MPV has exited,
	// which is definitively "nothing is playing". Treating it as an
	// error left the loop re-polling a dead socket and logging the
	// same failure every iteration.
	if err != nil && internal.MPVConnectionGone(err) {
		return false, nil
	}
	return playing, err
}

// stillPlaying reports false only when a started episode has had nothing
// loaded for two seconds running. A playlist switch empties mpv for a moment,
// so a single empty answer is not enough.
func (w *episodeWatch) stillPlaying() bool {
	playing, err := w.hasPlayback()
	if err != nil {
		internal.Log("Error checking playback status: " + err.Error())
		return true
	}
	if playing || !w.anime.Ep.Started {
		return true
	}
	time.Sleep(2 * time.Second)

	playing, err = w.hasPlayback()
	if err != nil {
		internal.Log("Error checking playback status: " + err.Error())
		return true
	}
	if playing {
		return true
	}
	// In a terminal with the next-episode prompt on, the prompt decides
	// what an ended episode means.
	return w.cliPrompt()
}

// playbackLost decides what an episode that stopped playing means: mpv
// switching files, the viewer quitting early, or the episode finished. socketLost
// is set when mpv stopped answering altogether rather than answering with
// nothing loaded. It reports true when the watcher should stop.
func (w *episodeWatch) playbackLost(socketLost bool) bool {
	a := w.anime
	watched := internal.PercentageWatched(a.Ep.Player.PlaybackTime, a.Ep.Duration)
	action := internal.ClassifyPlaybackLoss(w.socket, a.Ep.Started, watched, w.cfg.PercentageToMarkComplete)
	if socketLost {
		internal.Log(fmt.Sprintf("playback loss: pct=%.1f action=%d mpvRunning=%v switching=%v",
			watched, action, internal.IsMPVRunning(w.socket), internal.PlaylistSwitchInProgress()))
	} else {
		internal.Log(fmt.Sprintf("no active playback: pct=%.1f action=%d", watched, action))
	}

	switch action {
	case internal.PlaybackLossWait:
		// Playlist jump / demuxer reload / pause — MPV still open.
		return false
	case internal.PlaybackLossExit:
		internal.Log("Episode is not completed, exiting")
		internal.Exit(nil)
		return true
	}

	if socketLost {
		for _, value := range []any{watched, a.Ep.Player.Speed, a.Ep.Player.PlaybackTime, a.Ep.Duration, w.cfg.PercentageToMarkComplete} {
			internal.Log(fmt.Sprint(value))
		}
	}
	a.Ep.IsCompleted = true
	w.episodeFinished(socketLost)
	w.stop()
	return true
}

// episodeFinished moves on from an episode watched past the completion mark,
// or asks first when the config wants it to.
func (w *episodeWatch) episodeFinished(socketLost bool) {
	a := w.anime
	if !w.cfg.NextEpisodePrompt {
		internal.StartNextEpisode(a, w.cfg, w.databaseFile, w.user.Token)
		return
	}

	// Only rofi gets here with the prompt on: in a terminal the prompt
	// running beside playback owns the ending (see cliPrompt), so rofi asks
	// now instead.
	if internal.NextEpisodePromptRofi(w.cfg) {
		internal.StartNextEpisode(a, w.cfg, w.databaseFile, w.user.Token)
		return
	}
	if socketLost && a.Ep.Number == a.TotalEpisodes {
		internal.HandleLastEpisodeCompletion(w.cfg, a, w.user.Token)
	}
	w.saveFinishedEpisode()
	internal.Exit(nil)
}

// saveFinishedEpisode writes the episode the viewer stopped after to the watch
// history and, unless this is a rewatch, to the tracker.
func (w *episodeWatch) saveFinishedEpisode() {
	a := w.anime
	if err := internal.LocalUpdateAnime(w.databaseFile, a.AnilistId, a.ProviderId, a.Ep.Number, a.Ep.Player.PlaybackTime, internal.ConvertSecondsToMinutes(a.Ep.Duration), internal.GetAnimeName(*a), internal.CurrentAnimeProviderName(a)); err != nil {
		internal.Log("Error updating local database on quit: " + err.Error())
	}
	if a.Rewatching {
		return
	}
	if err := internal.UpdateAnimeProgress(w.user.Token, a.AnilistId, a.Ep.Number); err != nil {
		internal.Log("Error updating Anilist progress on quit: " + err.Error())
		return
	}
	internal.Out(fmt.Sprintf("Episode completed! Progress updated: %d", a.Ep.Number))
}

// skipAndFollowSpeed runs on the main goroutine until the episode is over,
// seeking past the parts the config skips and remembering the speed the
// viewer set, so the next episode starts at it.
func (w *episodeWatch) skipAndFollowSpeed() {
	a := w.anime
	for !w.stopped() {
		if target, ok := internal.SkipSeekTarget(a.Ep.SkipTimes, a.Ep.Player.PlaybackTime, w.cfg); ok {
			internal.SeekMPV(w.socket, target)
		}
		if _, err := w.mpvGet("time-pos"); err == nil && a.Ep.Started {
			if speed, speedErr := internal.GetMPVPlaybackSpeed(w.socket); speedErr != nil {
				internal.Log("Failed to get mpv speed " + speedErr.Error())
			} else {
				a.Ep.Player.Speed = speed
			}
		}
		time.Sleep(time.Second)
	}
}

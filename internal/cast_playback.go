package internal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/thexykril/otakase/internal/cast"
)

// castPollInterval is how often the device is asked where it is. A second is
// enough to catch a skip window and to notice the episode ending, without
// making a conversation out of it.
//
// A var, not a const: see castStartupGrace below for why.
var castPollInterval = time.Second

// castStopTimeout bounds the one teardown step that talks to the device. The
// interrupt path gives every cleanup two seconds between them, so a device
// that has stopped answering must not be allowed to spend all of it.
const castStopTimeout = time.Second

// castStartTimeout is how long to wait for ffmpeg to write the first segment
// before giving up on the stream.
const castStartTimeout = 30 * time.Second

// castStartupGrace is how long an idle status is tolerated before the device
// has reported anything but idle. Load is fire-and-forget: a device that
// cannot reach this machine (AP/client isolation, a guest VLAN) never starts,
// and its status looks identical to an episode that just finished. Without a
// grace period the first poll would report a full episode watched in a
// second.
//
// A var, not a const: the final-review test seam needs to shrink this (along
// with castPollInterval and castStallTimeout) to keep watchCast's table tests
// fast. Production code never assigns to it.
var castStartupGrace = 30 * time.Second

// castStallTimeout is how long a position may sit unchanged, once the device
// has started, before watchCast gives up on it. The vendored library never
// clears a.application when the receiver reports no applications at all, so
// a session left with nothing playing (the viewer stopped the cast, or cast
// something else to the same device) can otherwise be polled forever holding
// ffmpeg, the scratch directory and the HTTP server open. A var for the same
// reason as castPollInterval and castStartupGrace.
var castStallTimeout = 2 * time.Minute

func init() {
	// This package must not import internal, so package cast's stderr logger
	// (M3: log ffmpeg's stderr on any exit, not only on failure) is wired to
	// the real one here rather than at its own definition.
	cast.Log = func(msg string) { Log(msg) }
}

// Narrow views of the cast session, server and remux, so watchCast -- which is
// where this feature's failure handling lives -- can be tested without a
// device or an ffmpeg. The concrete types (*cast.Session, *cast.Server,
// *cast.Remux) satisfy these as they are; CastEpisode passes them unchanged.
type castSession interface {
	Progress() (cast.Progress, error)
	SeekToTime(seconds float64) error
	Pause() error
	Unpause() error
	SetVolume(level float64) error
	Volume() float64
}

type castServer interface {
	Err() error
	// Fetched reports whether the device ever asked for anything. A cast that
	// fails with this false did not fail at the receiver: nothing reached this
	// machine, which is what a host firewall looks like from here.
	Fetched() bool
	URL(name string) string
}

type castRemux interface {
	Err() error
	Done() <-chan struct{}
}

// castSpansFor turns resolved skip times into spans to seek past, honouring
// the settings that decide whether each is wanted at all.
//
// A zero span is one otakase never resolved, not one that starts at the
// beginning: offering it would seek the device back to the start of the
// episode on the first poll.
func castSpansFor(times SkipTimes, config *Config) []cast.Span {
	spans := []cast.Span{}
	if config == nil {
		return spans
	}
	if config.SkipOp && times.Op.End > times.Op.Start {
		spans = append(spans, cast.Span{Start: float64(times.Op.Start), End: float64(times.Op.End)})
	}
	if config.SkipEd && times.Ed.End > times.Ed.Start {
		spans = append(spans, cast.Span{Start: float64(times.Ed.Start), End: float64(times.Ed.End)})
	}
	return spans
}

// shouldPushRemoteProgress reports whether a remote tracker can be written.
//
// A nil check is not enough. main stores a zero-value User globally before any
// sign-in happens, and the spawned cast process returns into RunCastSession
// before EnsureConfiguredTrackersReady would have filled it in -- so
// GetGlobalUser there is non-nil with an empty Token, and an unauthenticated
// mutation goes out, fails, and is only logged while the viewer is told the
// episode was marked.
func shouldPushRemoteProgress(config *Config, user *User) bool {
	return UsesRemoteTracking(config) && user != nil && strings.TrimSpace(user.Token) != ""
}

// ApplyCastSubStyle makes a cast ask for the burned-in subtitle variant.
//
// The Default Media Receiver renders WebVTT only, and no provider here
// supplies WebVTT, so a soft-subbed cast plays with no subtitles at all. In
// memory only: providers read the live preference through
// providerhost.CurrentSubStyle, which reads this field, so nothing needs to be
// written to disk -- and rewriting the viewer's stored preference because they
// cast once would change every later local playback.
//
// explicitlyAskedSoft says the viewer passed -softsub on this same run, which
// is the one case worth answering out loud rather than silently overriding.
func ApplyCastSubStyle(config *Config, explicitlyAskedSoft bool) {
	if config == nil {
		return
	}
	if explicitlyAskedSoft {
		Out("Note: casting cannot show soft subtitles, so the hardsubbed stream is used instead.")
	}
	config.SubStyle = "hard"
}

// CastEpisode plays the already-resolved episode on a Chromecast instead of in
// mpv, and keeps tracking it while it plays.
func CastEpisode(config *Config, anime *Anime) error {
	if config == nil || anime == nil {
		return fmt.Errorf("cast: nothing to play")
	}
	if len(anime.Ep.Links) == 0 {
		return fmt.Errorf("cast: no episode links")
	}

	ffmpeg, err := ffmpegPath()
	if err != nil {
		if errors.Is(err, ErrFFmpegMissing) {
			return fmt.Errorf("cast: ffmpeg is required to cast episodes")
		}
		return err
	}

	device, err := chooseCastDevice(config)
	if err != nil {
		return err
	}

	// The screen is taken as soon as the device is known, and not before:
	// chooseCastDevice may have to show a menu, which cannot happen under a
	// takeover. From here on the terminal shows the frame and nothing else --
	// every message becomes a notification, because Out checks the same flag.
	var panel *castPanelWriter
	if commands := castControlsPossible(config); commands {
		release := castTakeScreen()
		cancelRelease := RegisterExitCleanup(release)
		panel = &castPanelWriter{home: true, size: castTerminalSize}
		castPanelForControls = panel
		defer func() {
			castPanelForControls = nil
			cancelRelease()
			release()
		}()
	}
	// Resolved here because casting reaches nothing that would do it for us:
	// StartPlayback runs the whole cast inline and hands back no socket path,
	// and main exits on that before the goroutine local playback relies on has
	// started. Without this the spans below are always empty, so no opening is
	// ever skipped and pressing s reports -- accurately -- that nothing is
	// known.
	ensureCastSkipTimes(anime, config, func(a *Anime, episode int, cfg *Config, provider any) {
		ApplySkipTimes(a, episode, cfg, provider)
	})

	castStatus := func(message string) {
		if panel != nil {
			fmt.Print(panel.status(GetAnimeName(*anime), anime.Ep.Number, device.Name, message))
			return
		}
		Out(message)
	}

	// The stream is remuxed into a directory per cast and thrown away after:
	// it is a transcode buffer, not a download. It lives under the storage
	// path rather than the system temp directory: $TMPDIR is tmpfs on this and
	// most modern Linux systems, and an event playlist keeps every segment for
	// the whole episode written so far -- a film would be several GB of RAM.
	scratchRoot := filepath.Join(os.ExpandEnv(config.StoragePath), "cast-scratch")
	if err := os.MkdirAll(scratchRoot, 0o755); err != nil {
		return fmt.Errorf("cast: could not create the stream directory: %w", err)
	}
	streamDir, err := os.MkdirTemp(scratchRoot, "otakase-cast-")
	if err != nil {
		return fmt.Errorf("cast: could not create the stream directory: %w", err)
	}

	streamURL := PrioritizeLink(anime.Ep.Links)
	referrer := anime.Ep.StreamReferrer
	if referrer == "" {
		referrer = streamReferrer(CurrentAnimeProviderName(anime))
	}

	// Torn down through one once-guarded function, registered both as a normal
	// defer and as an exit cleanup: the interrupt handler calls os.Exit, which
	// skips every deferred cleanup in this call, so a Ctrl+C or SIGTERM mid-cast
	// would otherwise leave the device holding a stream whose server just died,
	// ffmpeg orphaned, and the scratch directory on disk.
	//
	// resourceMu guards these three: the interrupt path runs teardown on the
	// signal-handler goroutine, which has no happens-before edge to this
	// goroutine's writes below. Without the lock a stale nil read on a weakly
	// ordered target (this project cross-compiles to arm64) would silently
	// skip a teardown step -- an orphaned ffmpeg is exactly what this exists
	// to prevent. Once assigned, this goroutine keeps using its own local
	// copy (rx, srv, s) rather than reading back through the lock.
	var (
		resourceMu sync.Mutex
		remux      *cast.Remux
		server     *cast.Server
		session    *cast.Session
	)
	var teardownOnce sync.Once
	teardown := func() {
		teardownOnce.Do(func() {
			resourceMu.Lock()
			s, srv, rx := session, server, remux
			resourceMu.Unlock()

			// Telling the device to stop is a network round trip, and the only
			// step here that can hang. It gets its own bound so a device that
			// has stopped answering cannot starve the steps below it: an
			// orphaned ffmpeg and a leaked scratch directory are exactly what
			// this teardown exists to prevent, and on the interrupt path every
			// cleanup shares one budget.
			if s != nil {
				stopped := make(chan struct{})
				go func() {
					defer close(stopped)
					_ = s.Stop()
				}()
				select {
				case <-stopped:
				case <-time.After(castStopTimeout):
				}
			}
			if srv != nil {
				_ = srv.Close()
			}
			if rx != nil {
				rx.Stop()
			}
			// OTAKASE_CAST_KEEP leaves the stream directory behind, so a cast
			// that failed can be inspected afterwards: what the device was
			// actually served is otherwise deleted the moment it gives up.
			if os.Getenv("OTAKASE_CAST_KEEP") != "" {
				Log("cast: keeping the stream directory: " + streamDir)
				Out("Kept the stream directory for inspection: " + streamDir)
			} else {
				_ = os.RemoveAll(streamDir)
			}
		})
	}
	cancel := RegisterExitCleanup(teardown)
	defer cancel()
	defer teardown()

	castStatus("Preparing the stream…")
	Log(fmt.Sprintf("cast: remuxing %s (referrer %q) into %s", streamURL, referrer, streamDir))
	// Subtitles are drawn into the picture when the stream has them, which is
	// the only way a Chromecast shows them: it renders WebVTT alone, no
	// provider here supplies WebVTT, and the receiver will not enable a
	// subtitle track from an HLS manifest without a Cast track API the
	// vendored library cannot reach. Burning costs a re-encode, so it happens
	// only when there is something to burn.
	remuxArgs := cast.BuildRemuxArgs(streamURL, referrer, streamDir)
	if castShouldBurnSubtitles(config, anime) {
		castStatus("Fetching subtitles…")
		subtitlePath, subErr := fetchCastSubtitle(anime.Ep.SubtitleURL, referrer, streamDir)
		if subErr != nil {
			// The episode is worth more than its subtitles: play it without.
			Out("Subtitles could not be fetched, casting without them: " + subErr.Error())
			Log(fmt.Sprintf("cast: %v", subErr))
		} else {
			castStatus("Preparing the stream with subtitles…")
			encoder := castBurnEncoder(config, ffmpeg)
			Log(fmt.Sprintf("cast: burning subtitles with %s (hardware=%t)", encoder.Name, encoder.Hardware()))
			remuxArgs = cast.BuildBurnArgs(streamURL, referrer, subtitlePath, streamDir, encoder)
		}
	}

	rx, err := cast.StartFFmpeg(ffmpeg, remuxArgs, streamDir)
	if err != nil {
		return err
	}
	resourceMu.Lock()
	remux = rx
	resourceMu.Unlock()

	playlistErr := make(chan error, 1)
	go func() {
		playlistErr <- cast.WaitForPlaylist(streamDir, castStartTimeout)
	}()

	select {
	case err := <-playlistErr:
		if err != nil {
			if remuxErr := rx.Err(); remuxErr != nil {
				return remuxErr
			}
			return err
		}
	case <-rx.Done():
		if remuxErr := rx.Err(); remuxErr != nil {
			return remuxErr
		}
		// ffmpeg exited without complaint, which a stream short enough to
		// finish before the wait noticed will do. The playlist was written
		// either way, so take the wait's own answer.
		if err := <-playlistErr; err != nil {
			return err
		}
	}

	srv, err := cast.NewServerOnPort(streamDir, config.CastPort)
	if err != nil {
		return err
	}
	resourceMu.Lock()
	server = srv
	resourceMu.Unlock()

	s, err := cast.Connect(device)
	if err != nil {
		return err
	}
	resourceMu.Lock()
	session = s
	resourceMu.Unlock()

	if anime.Ep.SubtitleURL != "" {
		// The Default Media Receiver renders WebVTT only, and Load carries no
		// subtitle track. Say so rather than letting the episode arrive silently
		// without the subtitles the viewer was expecting.
		if !config.CastBurnSubtitles && playlistAudioMode(anime, config) != "dub" {
			Out("Note: this stream has subtitles, and CastBurnSubtitles is off, so they will not appear.")
		}
	}

	if anime.Ep.Resume && anime.Ep.Player.PlaybackTime > 0 {
		// Session.Play always starts at zero: a real resume would need the
		// seek deferred until the remux has succeeded (an event playlist is
		// only a few segments long at t=0), which is another timing-dependent
		// branch in a loop that has already been through several review
		// rounds. Disclosed instead, so the viewer picking "Episode 5 (15:32)"
		// is told why it started over rather than left to notice on their own.
		// formatResumePosition is the gate rather than the wording: it already
		// decides when a resume point is worth mentioning at all (too early,
		// or near enough to the end that resuming is pointless). Its duration
		// argument is minutes, and Ep.Duration is seconds in memory.
		if formatResumePosition(anime.Ep.Player.PlaybackTime, ConvertSecondsToMinutes(anime.Ep.Duration)) != "" {
			Out(fmt.Sprintf("Note: casting starts from the beginning -- resuming at %d:%02d is not supported yet.",
				anime.Ep.Player.PlaybackTime/60, anime.Ep.Player.PlaybackTime%60))
		}
	}

	if err := s.Play(srv.URL(cast.PlaylistName)); err != nil {
		return err
	}
	castStatus(fmt.Sprintf("Waiting for %s to start…", device.Name))

	commands, haveControls := startCastControls(config)
	if !haveControls {
		Out("Controls need a terminal; use the device's own remote or app.")
	}

	return watchCastWithControls(config, anime, s, srv, rx, device, commands)
}

// watchCast follows the episode while the device plays it.
// watchCast follows the episode while the device plays it, with no controls.
func watchCast(config *Config, anime *Anime, session castSession, server castServer, remux castRemux, device cast.Device) error {
	return watchCastWithControls(config, anime, session, server, remux, device, nil)
}

// watchCastWithControls is watchCast with a channel of viewer commands. A nil
// channel blocks forever in the select below, which makes this behave exactly
// as the plain sleep it replaced.
func watchCastWithControls(config *Config, anime *Anime, session castSession, server castServer, remux castRemux, device cast.Device, commands <-chan castCommand) error {
	spans := castSpansFor(anime.Ep.SkipTimes, config)
	marked := false

	// started tracks whether the device has given evidence it is actually
	// playing -- a position that has moved, not merely a non-idle state. A
	// Default Media Receiver that accepts the load and then cannot fetch the
	// stream (AP/client isolation, a guest VLAN) sits in BUFFERING, which is
	// not idle, for as long as the connection takes to fail; if "non-idle"
	// were enough, that window would latch started and the IDLE that follows
	// would read as a finished episode instead of a device that never played
	// anything. Without this, the viewer sees "Playing on X." followed a
	// second later by "Playback finished." on a black screen.
	started := false
	paused := false
	startupDeadline := time.Now().Add(castStartupGrace)

	// lastPosition/lastPositionChange back Important 3's stall bound: a
	// stale application status can leave the device reporting a frozen
	// position forever once the session goes away (Session.Progress's
	// IsIdleScreen check closes the "receiver went back to idle" case; this
	// closes "the receiver reports no applications at all", which the
	// library also never clears). Gated on started, not checked before it:
	// the BUFFERING-forever case above is an accepted, separate risk this
	// bound is not meant to police.
	lastPosition := 0.0
	lastPositionChange := time.Now()

	// remuxErr is remembered rather than acted on the moment it appears: once
	// the episode is on disk the device can still play it out, and tearing
	// down here over a failure in ffmpeg's tail (a truncated end, a 403 on
	// the last segment) would take away an episode the viewer could have
	// finished watching. It is reported once playback actually ends, so an
	// ending is never described as clean when it wasn't.
	var remuxErr error

	remuxDone := func() bool {
		select {
		case <-remux.Done():
			return true
		default:
			return false
		}
	}

	// A remux that has finished *successfully*. remuxDone() alone is true when
	// ffmpeg died too, and both call sites below would then be reasoning about
	// a truncated episode: the duration would be the live edge where ffmpeg
	// stopped, and the mark-complete threshold would be a fraction of it. A
	// remux that dies two minutes into a 24-minute episode must not mark it
	// watched at 8:30 and push episode-complete to a tracker.
	//
	// Checking both remuxErr and remux.Err() is deliberate: remuxErr is the
	// remembered copy, and re-reading closes the window where ffmpeg fails
	// between the top-of-loop check and these two call sites.
	remuxSucceeded := func() bool {
		return remuxDone() && remuxErr == nil && remux.Err() == nil
	}

	// Written on the way out so a cast that ended early still offers a resume
	// point next time. The mpv path gets this from its own playback loop; a
	// cast has no loop left once this function returns. The marked guard
	// keeps this from overwriting a completed episode's record with an
	// in-progress one on an exit that follows a successful mark.
	// Everything an exit needs to write a partial row, guarded because one of
	// the two callers is the signal-handler goroutine: the window closing runs
	// this without unwinding the loop, so it cannot read the loop's own
	// variables.
	var (
		savedMu       sync.Mutex
		savedPosition float64
		savedDuration int
		savedMarked   bool
	)
	recordPosition := func(position float64) {
		savedMu.Lock()
		savedPosition = position
		savedDuration = anime.Ep.Duration
		savedMarked = marked
		savedMu.Unlock()
	}
	// Read once, before the loop writes anything: the exit cleanup runs on
	// another goroutine, and GetAnimeName takes an Anime by value, so calling
	// it there would copy the whole struct while this loop is writing
	// Ep.Duration and Ep.Player.PlaybackTime. None of these change during a
	// cast.
	historyPath := filepath.Join(os.ExpandEnv(config.StoragePath), "curd_history.txt")
	animeName := GetAnimeName(*anime)
	animeProvider := CurrentAnimeProviderName(anime)
	anilistID, providerID, episodeNumber := anime.AnilistId, anime.ProviderId, anime.Ep.Number

	writePartial := func() {
		savedMu.Lock()
		position, duration, alreadyMarked := savedPosition, savedDuration, savedMarked
		savedMu.Unlock()

		if alreadyMarked || position < 1 {
			return
		}
		LocalUpdateAnime(
			historyPath,
			anilistID, providerID, episodeNumber,
			// Minutes, as at the mark-complete call above: Ep.Duration is held
			// in seconds in memory, and this column is read back as minutes.
			int(position), ConvertSecondsToMinutes(duration),
			animeName, animeProvider,
		)
	}
	savePartial := func(position float64) {
		recordPosition(position)
		writePartial()
	}

	// The window closing is a signal, not a return: the process exits without
	// unwinding this function, so the position has to be written from a
	// cleanup or the README's promise that closing the window saves your place
	// is simply untrue. Cancelled on every normal exit below, where savePartial
	// is called directly with the position of that moment.
	cancelSave := RegisterExitCleanup(writePartial)
	defer cancelSave()

	// The panel owns the bottom of the screen while controls are live. It is
	// published for castOut, which has to clear the frame before printing so a
	// message does not land inside the box.
	// Only drawn to a real terminal. Redrawing a frame in place needs cursor
	// escapes that mean nothing to a pipe or a log file, and a caller that
	// captures stdout would be handed several kilobytes a second of them --
	// which deadlocks any test that reads the pipe only after the call returns.
	// The panel belongs to CastEpisode, which took the screen before the remux
	// started so its progress had somewhere to go.
	panel := castPanelForControls

	// Created once, reset only when a poll actually happens: a timer recreated
	// on every select would let a stream of keypresses postpone Progress()
	// indefinitely, and with it the remux and server error checks.
	pollTimer := time.NewTimer(castPollInterval)
	defer pollTimer.Stop()

	for {
		// A nil command channel blocks forever, so with no controls this
		// select degrades to exactly the sleep it replaced.
		select {
		case <-pollTimer.C:
			pollTimer.Reset(castPollInterval)
		case command := <-commands:
			wasPaused := paused
			stop, moved, err := applyCastCommand(command, session, &paused, spans, lastPosition)
			if err != nil {
				Log(fmt.Sprintf("cast: %v", err))
			}
			if stop {
				castOut(commands != nil, "Stopped.")
				savePartial(lastPosition)
				return nil
			}
			// A command that moved the episode is the freshest position there
			// is: the device will not report it until the next poll, and
			// without this every press in a burst acts on the same stale
			// reading and they collapse into one seek.
			lastPosition = moved
			recordPosition(lastPosition)
			// Only on resume, and deliberately not on every command. While
			// paused the stall clock is not consulted at all, so by the time
			// playback resumes it holds a reading as old as the pause and
			// would fire at once. Resetting on every command instead would
			// let any repeated keypress hold the bound open forever -- the
			// same way a reset in the skip branch once made it unreachable.
			if wasPaused && !paused {
				lastPositionChange = time.Now()
			}
			continue
		}

		// Checked before the device is polled: ffmpeg dying leaves the server
		// healthy and the device playing out what is already on disk, so this
		// is the failure most likely to reach a viewer disguised as a
		// finished episode. It is only remembered here, not returned -- see
		// remuxErr above -- and reported below once playback actually ends.
		if remuxErr == nil {
			remuxErr = remux.Err()
		}
		// A dead server means nothing further can be fetched, so unlike a
		// dead remux there is no watchable episode left to protect: return
		// immediately.
		if serverErr := server.Err(); serverErr != nil {
			savePartial(lastPosition)
			return serverErr
		}

		progress, err := session.Progress()
		if err != nil {
			castOut(commands != nil, fmt.Sprintf("Lost contact with %s.", device.Name))
			Log(fmt.Sprintf("cast: lost contact with the device: %v", err))
			savePartial(lastPosition)
			// A remembered ffmpeg failure outranks losing the device: if the
			// remux died and the device then stopped answering, ffmpeg is the
			// cause worth reporting, and returning nil here would bury it.
			if remuxErr != nil {
				return remuxErr
			}
			return nil
		}

		if progress.Idle {
			if !started {
				if remuxErr != nil {
					savePartial(lastPosition)
					return remuxErr
				}
				if time.Now().Before(startupDeadline) {
					continue
				}
				savePartial(lastPosition)
				// Two very different failures used to share one message. If
				// the device never fetched anything, the receiver never got
				// the chance to refuse it -- so say that, and say how to fix
				// the thing that is almost always responsible.
				if !server.Fetched() {
					hint := castFirewallHint(config, castServerHost(server.URL("")), castDetectFirewall())
					return fmt.Errorf("cast: %s never fetched the stream from this machine.\n%s", device.Name, hint)
				}
				return fmt.Errorf("cast: %s never started playing -- it fetched the stream but would not play it", device.Name)
			}
			// An episode already marked watched is one the viewer saw through,
			// so a failure in ffmpeg's tail is cosmetic by the time it lands:
			// telling them "Casting failed" about an episode they just
			// finished would be its own kind of lie. Log it and report the
			// ending honestly.
			if remuxErr != nil && marked {
				Log(fmt.Sprintf("cast: the remux ended badly after the episode was marked watched: %v", remuxErr))
				remuxErr = nil
			}
			savePartial(lastPosition)
			if remuxErr != nil {
				return remuxErr
			}
			castOut(commands != nil, "Playback finished.")
			return nil
		}
		// Evidence of playback, not merely of a non-idle state: a device that
		// accepted the load and then could not fetch the stream sits in
		// BUFFERING -- which is not idle -- for as long as the connection
		// takes to fail. Only a position that has moved distinguishes a device
		// that is playing from one that is still trying to.
		if progress.Position > 0 {
			started = true
		}

		// The stall bound: gated on started, since a stuck-before-it-began
		// device is castStartupGrace's problem, not this one. Compared as a
		// float64, not the int PlaybackTime below: truncating to int would
		// round away the sub-second progress a slow poll interval can still
		// see between ticks, and read as a stall that was not one.
		if progress.Position != lastPosition {
			lastPosition = progress.Position
			recordPosition(lastPosition)
			lastPositionChange = time.Now()
		} else if started && !paused && time.Since(lastPositionChange) >= castStallTimeout {
			savePartial(lastPosition)
			return fmt.Errorf("cast: %s stopped reporting progress -- playback may have been stopped on the device", device.Name)
		}

		anime.Ep.Player.PlaybackTime = int(progress.Position)

		if panel != nil {
			state := "PLAYING"
			if paused {
				state = "PAUSED"
			}
			fmt.Print(panel.frame(castPanelState{
				Title:    animeName,
				Episode:  episodeNumber,
				Device:   device.Name,
				Position: progress.Position,
				Duration: progress.Duration,
				State:    state,
				Volume:   session.Volume(),
			}))
		}
		// Until ffmpeg writes EXT-X-ENDLIST the playlist only advertises what
		// has been remuxed so far, so this duration is a live edge, not an
		// episode length. Marking against it completes the episode early on any
		// source slower than playback -- and remuxSucceeded(), not remuxDone(),
		// is what makes this specifically the *finished* duration: remuxDone()
		// alone is also true when ffmpeg died, which is a truncated edge, not
		// a finished one.
		if progress.Duration > 0 && remuxSucceeded() {
			anime.Ep.Duration = int(progress.Duration)
		}

		if target, ok := cast.NextSkip(progress.Position, spans); ok {
			if err := session.SeekToTime(target); err != nil {
				Log(fmt.Sprintf("cast: skip failed: %v", err))
			}
			// Deliberately no stall-clock reset here. A healthy seek moves the
			// position, which resets the clock above on the next poll anyway --
			// so a reset here would only ever take effect when the position is
			// frozen, which is the one case the stall bound exists to catch.
			// Worse, NextSkip keeps returning true for as long as the position
			// is inside a span, so a freeze during the opening or the ending
			// (when a viewer is most likely to stop the cast from the device)
			// would restart the clock every second and the bound could never
			// fire. The cost of leaving it out is a poll or two of a
			// 120-second budget.
			continue
		}

		if !marked && remuxSucceeded() && cast.ShouldMarkComplete(progress, config.PercentageToMarkComplete) {
			marked = true
			recordPosition(progress.Position)
			LocalUpdateAnime(
				filepath.Join(os.ExpandEnv(config.StoragePath), "curd_history.txt"),
				anime.AnilistId, anime.ProviderId, anime.Ep.Number,
				// Minutes, not seconds: LocalUpdateAnime's animeDuration column
				// is minutes, which is how formatResumePosition reads it back.
				// The mpv path converts at its own call site for the same reason.
				int(progress.Position), ConvertSecondsToMinutes(int(progress.Duration)),
				GetAnimeName(*anime), CurrentAnimeProviderName(anime),
			)
			// GetGlobalUser returns nil when nothing signed in, and local-only
			// tracking is a supported mode -- dereferencing it here would panic
			// on the one path a local-only user reaches.
			if user := GetGlobalUser(); shouldPushRemoteProgress(config, user) {
				if err := UpdateAnimeProgress(user.Token, anime.AnilistId, anime.Ep.Number); err != nil {
					Log(fmt.Sprintf("cast: could not update remote progress: %v", err))
				}
			}
			castOut(commands != nil, fmt.Sprintf("Episode %d marked as watched.", anime.Ep.Number))
		}
	}
}

// chooseCastDevice finds the device to play on, asking only when the answer is
// not already obvious.
func chooseCastDevice(config *Config) (cast.Device, error) {
	Out("Looking for cast devices...")
	devices, err := cast.Discover(context.Background(), cast.DefaultDiscoveryTimeout)
	if err != nil {
		return cast.Device{}, err
	}
	if len(devices) == 0 {
		return cast.Device{}, fmt.Errorf("cast: no devices found on this network")
	}

	configuredMissing := false
	if configured := config.CastDevice; configured != "" {
		for _, device := range devices {
			if device.Name == configured {
				return device, nil
			}
		}
		Out(fmt.Sprintf("%q was not found; pick another device.", configured))
		configuredMissing = true
	}

	// The lone device is not "another device" when a configured name just
	// failed to match it -- that is silently ignoring the mismatch and
	// casting to it anyway, right after telling the user to pick.
	if len(devices) == 1 && !configuredMissing {
		return devices[0], nil
	}

	options := make([]SelectionOption, 0, len(devices))
	for _, device := range devices {
		options = append(options, SelectionOption{Key: device.UUID, Label: device.String()})
	}
	selected, err := DynamicSelectPreserveOrder(options)
	if err != nil {
		return cast.Device{}, err
	}
	for _, device := range devices {
		if device.UUID == selected.Key {
			return device, nil
		}
	}
	return cast.Device{}, fmt.Errorf("cast: no device chosen")
}

package internal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

// castProgressTimeout bounds a single status check against the device.
//
// The vendored library sets no read or write deadline on its connection: a
// device that stops acknowledging traffic without closing the connection --
// wifi power-save is a common trigger, and this project has seen it cluster
// near the end of an episode -- leaves the write itself blocked on the OS's
// own TCP retransmission timeout, tens of seconds, well past the library's
// own 5s response-wait (which never starts, because the write that would
// lead to it is what is stuck). Without this bound, a dead connection reads
// as a frozen panel for half a minute before "lost contact" ever appears.
const castProgressTimeout = 10 * time.Second

// progressWithin calls session.Progress with an external bound, for the
// reason castProgressTimeout documents. The goroutine below is not
// cancellable -- the vendored call underneath takes no context -- so a
// progress call that is still stuck when within elapses keeps running and
// writes to done on its own time; done is buffered so that write never
// blocks, and nothing here reads it again once this function has returned.
func progressWithin(session castSession, within time.Duration) (cast.Progress, error) {
	type result struct {
		progress cast.Progress
		err      error
	}
	done := make(chan result, 1)
	go func() {
		p, err := session.Progress()
		done <- result{p, err}
	}()
	select {
	case r := <-done:
		return r.progress, r.err
	case <-time.After(within):
		return cast.Progress{}, fmt.Errorf("cast: device did not respond to a status check within %s", within)
	}
}

// castStartTimeout is how long to wait for ffmpeg to write the first segment
// before giving up on the stream.
// 90s rather than 30s. Burning subtitles re-encodes the video, so the first
// segment can take considerably longer to appear than a stream copy does.
const castStartTimeout = 90 * time.Second

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
// 90s rather than 30s: a TV waking from standby, joining Wi-Fi and buffering a
// first segment regularly needs more than half a minute, and the cost of
// waiting too long is a pause before an error, while the cost of giving up too
// soon is a cast that failed for no reason the viewer can see.
var castStartupGrace = 90 * time.Second

// castStallTimeout is how long a position may sit unchanged, once the device
// has started, before watchCast gives up on it. The vendored library never
// clears a.application when the receiver reports no applications at all, so
// a session left with nothing playing (the viewer stopped the cast, or cast
// something else to the same device) can otherwise be polled forever holding
// ffmpeg, the scratch directory and the HTTP server open. A var for the same
// reason as castPollInterval and castStartupGrace.
var castStallTimeout = 2 * time.Minute

// castEndStallTimeout is how long a position may sit unchanged at the very end
// of a fully delivered episode before watchCast calls it finished. Some
// receivers never report IDLE after the last segment of an HLS stream that was
// loaded as live (no EXT-X-ENDLIST yet): they sit on the final frame reporting
// PLAYING, and without this the viewer watched a frozen panel for the whole of
// castStallTimeout and was then told the cast had failed.
var castEndStallTimeout = 5 * time.Second

// castFetchHintDelay is how long the device may go without asking this machine
// for anything before the panel says a firewall may be blocking it. Short of
// castStartupGrace on purpose: that bound has to allow for a TV waking from
// standby, but a device that has fetched nothing after this long is usually not
// going to, and the viewer can act on the hint while the cast is still waiting.
// A var for the same reason as castPollInterval.
var castFetchHintDelay = 20 * time.Second

// castEndSlack is how close to the end a frozen position must be to count as
// the end rather than a stall: the receiver's last reported position lands a
// fraction of a second either side of the measured duration.
const castEndSlack = 3.0

// castSkipBackOffStep and castSkipBackOffTries shape the retry of a skip whose
// rebuild failed near the end: each try starts the stream this much earlier.
// Small steps replay as little of the ending as possible, and five of them
// reach back past any final segment a provider has served here so far (the
// longest seen is about 10s). A failed try costs about two seconds.
const (
	castSkipBackOffStep  = 2.0
	castSkipBackOffTries = 5
)

// castSkipBackOff retries a failed skip progressively earlier, never at or
// before where playback already is, reporting whether one of them worked.
func castSkipBackOff(session castSession, target, position float64) bool {
	for try := 1; try <= castSkipBackOffTries; try++ {
		earlier := target - castSkipBackOffStep*float64(try)
		if earlier <= position {
			return false
		}
		err := session.SeekToTime(earlier)
		Log(fmt.Sprintf("cast: retrying the skip at %.1f: err=%v", earlier, err))
		if err == nil {
			return true
		}
	}
	return false
}

// castEndLead is how far before the end the episode is called finished. At
// least one poll interval, so the last answer before the receiver goes quiet is
// always inside it.
const castEndLead = 1.0

// castSkipNothingLeft is how close to the end a skip must land for there to be
// nothing after it worth rebuilding a stream for.
const castSkipNothingLeft = 1.0

func init() {
	// This package must not import internal, so package cast's stderr logger
	// (M3: log ffmpeg's stderr on any exit, not only on failure) is wired to
	// the real one here rather than at its own definition.
	cast.Log = func(msg string) { Log(msg) }
}

// Narrow views of the cast session, server and remux, so watchCast -- which is
// where this feature's failure handling lives -- can be tested without a
// device or an ffmpeg. The concrete types (any cast.Player, *cast.Server,
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
	if config.SkipRecap && times.Recap.End > times.Recap.Start {
		spans = append(spans, cast.Span{Start: float64(times.Recap.Start), End: float64(times.Recap.End)})
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

// ErrCastStopped reports that the viewer ended the cast themselves.
//
// It is not a failure and it is not a finished episode: nothing is wrong, and
// nothing should advance. Without it both look like the nil a finished episode
// returns, and pressing q would start the next episode.
var ErrCastStopped = errors.New("cast: stopped by the viewer")

// castSocketSentinel is what StartPlayback hands back for a cast that played
// an episode through.
//
// It follows the android-intent sentinel already in cmd/otakase/main.go: a
// player that ran somewhere else and is now finished, whose caller should carry
// on rather than poll a socket that will never exist.
const castSocketSentinel = "cast"

// castOutcome turns a cast's error into the socket path StartPlayback returns
// and whether the failure is worth reporting to the viewer.
func castOutcome(err error) (socket string, report bool) {
	switch {
	case err == nil:
		return castSocketSentinel, false
	case errors.Is(err, ErrCastStopped):
		return "", false
	default:
		return "", true
	}
}

// CastEpisode plays the already-resolved episode on a Chromecast instead of in
// mpv, and keeps tracking it while it plays.
func CastEpisode(config *Config, anime *Anime) error {
	var device *cast.Device
	for {
		err := castEpisodeOnce(config, anime, device)
		var switched *castAudioSwitch
		if !errors.As(err, &switched) {
			return err
		}
		device = &switched.device
		switchCastAudio(config, anime, switched.position)
	}
}

func castEpisodeOnce(config *Config, anime *Anime, reuse *cast.Device) error {
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

	// Restarted in the other language, the episode stays on the device it
	// was on, without looking for devices again.
	var device cast.Device
	if reuse != nil {
		device = *reuse
	} else if device, err = chooseCastDevice(config); err != nil {
		return err
	}

	// The screen is taken as soon as the device is known, and not before:
	// chooseCastDevice may have to show a menu, which cannot happen under a
	// takeover. From here on the terminal shows the frame and nothing else --
	// every message becomes a notification, because Out checks the same flag.
	//
	// A cast session already holding the terminal for the whole cast owns the
	// panel; taking it again here would blink the screen off and straight back
	// on. Taking it here is the fallback for a cast launched directly in a
	// terminal, which has no session around it.
	var panel *castPanelWriter
	if castPanelForControls != nil {
		panel = castPanelForControls
	} else if commands := castControlsPossible(config); commands {
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
			fmt.Print(panel.status(GetAnimeName(*anime), anime.Ep.Number, device.Name, message, castPanelPlaybackKeys))
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
		session    cast.Player
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

	// One probe of the source answers two questions. The device reports dur=-1
	// for the whole episode, so the panel's total has to come from here: the
	// tracker's average is a fallback worth having but only an average -- 24:00
	// for an episode that runs 24:40. And a stream carrying several dubs needs
	// the one asked for named, or ffmpeg casts whichever the host marks default.
	// A failure is not worth stopping for: the estimate still shows a total,
	// and ffmpeg still picks a track.
	durationEstimated := true
	mode := playlistAudioMode(anime, config)
	var tracks cast.TrackSelection
	if ffprobe, probeErr := cast.FFprobePathFor(ffmpeg); probeErr != nil {
		Log(fmt.Sprintf("cast: no ffprobe to read the stream with: %v", probeErr))
	} else if info, probeErr := cast.ProbeStream(ffprobe, streamURL, referrer, anime.Ep.StreamHeaders); probeErr != nil {
		Log(fmt.Sprintf("cast: %v", probeErr))
	} else {
		if info.Duration > 0 {
			anime.Ep.Duration = int(info.Duration + 0.5)
			durationEstimated = false
			Log(fmt.Sprintf("cast: the episode is %d seconds long", anime.Ep.Duration))
		}
		want := "ja"
		if mode == "dub" {
			want = "en"
		}
		tracks = cast.SelectTracks(info, want)
		Log(fmt.Sprintf("cast: %d audio tracks, playing %q with %v", len(info.Audio), tracks.AudioLanguage, tracks.Maps))
		if mode == "dub" && len(info.Audio) > 1 && tracks.AudioLanguage != "en" {
			Out("This stream has no English audio, so the cast plays " + castLanguageName(tracks.AudioLanguage) + ".")
		}
	}
	// Subtitles are drawn into the picture when the stream has them, which is
	// the only way a Chromecast shows them: it renders WebVTT alone, no
	// provider here supplies WebVTT, and the receiver will not enable a
	// subtitle track from an HLS manifest without a Cast track API the
	// vendored library cannot reach. Burning costs a re-encode, so it happens
	// only when there is something to burn.
	source := &castStreamSource{
		ffmpeg:    ffmpeg,
		streamURL: streamURL,
		referrer:  referrer,
		headers:   anime.Ep.StreamHeaders,
		rootDir:   streamDir,
		maps:      tracks.Maps,
		chooseEncoder: func() cast.Encoder {
			return castBurnEncoder(config, ffmpeg)
		},
	}
	burn := castShouldBurnSubtitles(config, anime, tracks.AudioLanguage)
	if !burn {
		// Said out loud, because a cast that silently plays without subtitles
		// looks identical to one where burning failed, and the viewer is left
		// guessing which. Each reason below is a different thing to fix.
		switch {
		case config != nil && !config.CastBurnSubtitles:
			Log("cast: not burning subtitles: CastBurnSubtitles is off")
		case strings.TrimSpace(anime.Ep.SubtitleURL) != "":
			Log(fmt.Sprintf("cast: not burning subtitles: the audio is a dub (language %q, mode %s)", tracks.AudioLanguage, mode))
		default:
			Log("cast: not burning subtitles: this provider gave no subtitle track for this episode")
		}
	}
	if burn {
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
			source.subtitlePath = subtitlePath
			source.encoder = encoder
		}
	}

	// Resuming is a seek before there is anything to seek: the stream is built
	// from the saved position, so what the device is handed is the rest of the
	// episode as a stream of its own and it never has to seek at all.
	//
	// formatResumePosition is the gate rather than the wording, as it was when
	// this could only be disclosed: it already decides when a resume point is
	// worth acting on -- too early to be worth anything, or near enough to the
	// end that resuming is pointless. Its duration argument is minutes, and
	// Ep.Duration is seconds in memory.
	resumeAt := castResumeAt(config, anime)
	if resumeAt > 0 {
		castStatus(fmt.Sprintf("Resuming at %s…", castClock(resumeAt)))
		Log(fmt.Sprintf("cast: resuming at %.1f", resumeAt))
	}

	first, err := source.start(resumeAt)
	if err != nil {
		return err
	}
	resourceMu.Lock()
	remux = first.remux
	resourceMu.Unlock()
	remuxes := newCastRemuxSwitch(first.remux)

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

	if err := s.Play(srv.URL(first.path)); err != nil {
		return err
	}

	// The device cannot seek, so a seek rebuilds the stream at the target and
	// hands the device a new one. The wrapper keeps everything above it in
	// episode time -- the panel, the progress written to the trackers, the
	// completion threshold -- rather than in the clock of whichever stream is
	// currently playing. See cast_seek.go.
	// The generation currently playing, so the one it replaces can be cleared.
	playing := first
	seeking := &castSeekingSession{inner: s, base: resumeAt, restart: func(target float64) error {
		// No status here: the watch loop shows the destination as the presses
		// arrive, before this runs, which is the only moment a viewer can still
		// be told anything -- rebuilding the stream blocks that loop.
		Log(fmt.Sprintf("cast: rebuilding the stream at %.1f", target))

		next, startErr := source.start(target)
		if startErr != nil {
			return startErr
		}

		previous := remuxes.swap(next.remux)
		resourceMu.Lock()
		remux = next.remux
		resourceMu.Unlock()

		if err := s.Play(srv.URL(next.path)); err != nil {
			return err
		}

		// The generation just left behind holds every segment written for it,
		// which for a long episode is hundreds of megabytes. The device has
		// moved on to the new stream, so it is no longer fetching from there.
		if previous != nil && playing.dir != "" && playing.dir != next.dir {
			if err := os.RemoveAll(playing.dir); err != nil {
				Log(fmt.Sprintf("cast: could not clear the previous stream: %v", err))
			}
		}
		playing = next
		return nil
	}}
	if !durationEstimated && anime.Ep.Duration > 0 {
		// Only a measured length: a seek guard built on the tracker's average
		// would refuse the last minutes of an episode that runs longer than
		// average, and allow a seek past the end of one that runs shorter.
		seeking.duration = float64(anime.Ep.Duration)
	}
	castStatus(fmt.Sprintf("Waiting for %s to start…", device.Name))

	commands, releaseControls, haveControls := startCastControls(config)
	if !haveControls {
		Out("Controls need a terminal; use the device's own remote or app.")
	}
	// Released here rather than at the top of the function because the
	// subscription must outlive the countdown below, which reads the same
	// channel. The next episode takes a subscription of its own.
	defer releaseControls()

	if err := watchCastWithControls(config, anime, seeking, srv, remuxes, device, commands, durationEstimated); err != nil {
		return err
	}

	// The countdown runs in this episode's panel, which still owns the screen:
	// the teardown below has not run yet, so there is a frame to draw in.
	if !castAwaitNextEpisode(config, anime, panel, commands) {
		return ErrCastStopped
	}
	return nil
}

// watchCast follows the episode while the device plays it.
// watchCast follows the episode while the device plays it, with no controls.
func watchCast(config *Config, anime *Anime, session castSession, server castServer, remux castRemux, device cast.Device, durationEstimated bool) error {
	return watchCastWithControls(config, anime, session, server, remux, device, nil, durationEstimated)
}

// watchCastWithControls is watchCast with a channel of viewer commands. A nil
// channel blocks forever in the select below, which makes this behave exactly
// as the plain sleep it replaced.
// durationEstimated says Ep.Duration is the tracker's average episode length
// rather than this episode's measured one, so the panel can mark the total as
// an estimate instead of presenting a guess as a reading.
func watchCastWithControls(config *Config, anime *Anime, session castSession, server castServer, remux castRemux, device cast.Device, commands <-chan castCommand, durationEstimated bool) error {
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
	fetchHintAt := time.Now().Add(castFetchHintDelay)
	fetchHinted := false

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
	untracked := anime.Untracked
	syncsResume := ShouldWriteRemoteTracking(config, anime)

	writePartial := func() {
		savedMu.Lock()
		position, duration, alreadyMarked := savedPosition, savedDuration, savedMarked
		savedMu.Unlock()

		if alreadyMarked || position < 1 || untracked {
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
		if syncsResume {
			syncedResumePusher.note(config, anilistID, episodeNumber, int(position))
		}
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
	// Registered after writePartial so the position it records on the way
	// out is the one written to the trackers.
	if syncsResume {
		cancelFlush := RegisterExitCleanup(syncedResumePusher.flush)
		defer cancelFlush()
	}

	// markWatched records the episode as seen, locally and on the remote
	// tracker. Callers check remuxSucceeded() first: see its comment for why a
	// truncated stream must not mark anything.
	markWatched := func(position, duration float64) {
		marked = true
		recordPosition(position)
		if untracked {
			return
		}
		LocalUpdateAnime(
			historyPath,
			anilistID, providerID, episodeNumber,
			// Minutes, not seconds: LocalUpdateAnime's animeDuration column
			// is minutes, which is how formatResumePosition reads it back.
			// The mpv path converts at its own call site for the same reason.
			int(position), ConvertSecondsToMinutes(int(duration)),
			animeName, animeProvider,
		)
		// GetGlobalUser returns nil when nothing signed in, and local-only
		// tracking is a supported mode -- dereferencing it here would panic
		// on the one path a local-only user reaches.
		if user := GetGlobalUser(); shouldPushRemoteProgress(config, user) {
			if err := UpdateAnimeProgress(user.Token, anilistID, episodeNumber); err != nil {
				Log(fmt.Sprintf("cast: could not update remote progress: %v", err))
			}
		}
		castOut(commands != nil, fmt.Sprintf("Episode %d marked as watched.", episodeNumber))
	}

	// reached records that the position is past the watched threshold, against
	// a finished remux's duration. The episode is marked from it when the cast
	// ends, however it ends -- finished, stopped, or the device lost -- which
	// is how the mpv path judges it too: by where playback was when it
	// stopped, not by where it once passed through.
	reached := false
	reachedDuration := 0.0
	defer func() {
		// Checked against lastPosition as well: a seek back out of the
		// threshold moves it at once, before any poll could clear reached.
		final := cast.Progress{Position: lastPosition, Duration: reachedDuration}
		if !marked && reached && cast.ShouldMarkComplete(final, config.PercentageToMarkComplete) {
			markWatched(lastPosition, reachedDuration)
		}
	}()

	// finish ends a cast that played through, however the end was noticed:
	// the device going idle, the ending skip running to the end, or the
	// device freezing on the last frame.
	finish := func() error {
		// An episode already marked watched is one the viewer saw through,
		// so a failure in ffmpeg's tail is cosmetic by the time it lands:
		// telling them "Casting failed" about an episode they just
		// finished would be its own kind of lie. Log it and report the
		// ending honestly.
		if remuxErr != nil && (marked || reached) {
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

	// A skip whose rebuild failed is not retried: the loop would otherwise
	// launch another ffmpeg every poll for as long as the position stays in
	// the span, each one failing the same way. A skip that landed early on
	// purpose (castSkipBackOff) is recorded here too.
	failedSkips := map[float64]bool{}

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

			var (
				stop    bool
				moved   float64
				err     error
				pending []castCommand
			)
			if _, isSeek := seekStepFor(command); isSeek {
				// A seek restarts the stream, which takes seconds and blocks
				// this loop for all of them. So the presses are gathered first,
				// with the destination shown as each one arrives, and the
				// restart runs once the viewer has stopped pressing. Without
				// this, a second press changed nothing on screen until the first
				// seek had finished.
				var target float64
				target, pending = collectSeekTarget(command, commands, lastPosition, func(target float64) {
					// Shown on every press, which is the point: the restart that
					// follows freezes this loop, so the destination has to be on
					// screen before it starts.
					message := fmt.Sprintf("Seeking to %s…", castClock(target))
					if panel != nil {
						fmt.Print(panel.status(animeName, episodeNumber, device.Name, message, castPanelPlaybackKeys))
						return
					}
					castOut(commands != nil, message)
				})
				moved, err = applyCastSeek(session, &paused, target)
			} else {
				stop, moved, err = applyCastCommand(command, session, &paused, spans, lastPosition)
			}
			// Logged whether or not it worked. A seek that the receiver quietly
			// declines is indistinguishable from a keypress that never arrived,
			// and the two have completely different causes -- one is this
			// program, the other is the stream having no known duration to seek
			// within.
			Log(fmt.Sprintf("cast: command %d at pos=%.1f -> pos=%.1f err=%v", command, lastPosition, moved, err))
			if err != nil {
				Log(fmt.Sprintf("cast: %v", err))
			}
			if stop && command == castCmdSwitchAudio {
				castOut(commands != nil, "Switching audio…")
				savePartial(lastPosition)
				return &castAudioSwitch{position: lastPosition, device: device}
			}
			if stop {
				castOut(commands != nil, "Stopped.")
				savePartial(lastPosition)
				// Not nil: a finished episode returns nil, and the caller
				// advances on that. The viewer asked for this to end.
				return ErrCastStopped
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

			// Whatever arrived during the burst that was not a seek. A stop in
			// here ends the episode exactly as one arriving on its own does.
			for _, queued := range pending {
				wasPaused := paused
				stop, moved, err := applyCastCommand(queued, session, &paused, spans, lastPosition)
				Log(fmt.Sprintf("cast: queued command %d at pos=%.1f -> pos=%.1f err=%v", queued, lastPosition, moved, err))
				if err != nil {
					Log(fmt.Sprintf("cast: %v", err))
				}
				if stop && queued == castCmdSwitchAudio {
					castOut(commands != nil, "Switching audio…")
					savePartial(lastPosition)
					return &castAudioSwitch{position: lastPosition, device: device}
				}
				if stop {
					castOut(commands != nil, "Stopped.")
					savePartial(lastPosition)
					return ErrCastStopped
				}
				lastPosition = moved
				recordPosition(lastPosition)
				if wasPaused && !paused {
					lastPositionChange = time.Now()
				}
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

		progress, err := progressWithin(session, castProgressTimeout)
		if err != nil {
			Log(fmt.Sprintf("cast: lost contact with the device: %v", err))
			// Some receivers go unresponsive to status polls right at the
			// true end of a stream they have already fully received, rather
			// than reporting idle promptly -- confirmed on hardware: the
			// whole episode was already delivered (remuxSucceeded, i.e.
			// EXT-X-ENDLIST was written) and the watched threshold had
			// already been crossed (marked) before contact was lost. That
			// combination is what makes this safe to treat as a finish
			// instead of a failure -- unlike marked alone, it cannot fire
			// on an episode still being delivered when the connection
			// happened to drop, since there is nothing left it could have
			// missed.
			if (marked || reached) && remuxSucceeded() {
				castOut(commands != nil, "Lost contact near the end -- treating the episode as finished.")
				savePartial(lastPosition)
				return nil
			}
			castOut(commands != nil, fmt.Sprintf("Lost contact with %s.", device.Name))
			savePartial(lastPosition)
			// A remembered ffmpeg failure outranks losing the device: if the
			// remux died and the device then stopped answering, ffmpeg is the
			// cause worth reporting, and returning nil here would bury it.
			if remuxErr != nil {
				return remuxErr
			}
			// Losing the device mid-episode is a failure, not a completion:
			// nothing has finished, and the caller must not advance.
			return fmt.Errorf("cast: lost contact with %s", device.Name)
		}

		// Before the idle check, because a device that cannot reach this
		// machine may sit in BUFFERING rather than IDLE while it tries.
		if !started && !fetchHinted && !server.Fetched() && !time.Now().Before(fetchHintAt) {
			fetchHinted = true
			hint := castWaitingFirewallHint(config, castServerHost(server.URL("")), castDetectFirewall(), device.Name)
			Log("cast: " + strings.ReplaceAll(hint, "\n", " "))
			if panel != nil {
				fmt.Print(panel.status(animeName, episodeNumber, device.Name, hint, castPanelPlaybackKeys))
			} else {
				castOut(commands != nil, hint)
			}
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
					hint, command := castFirewallHintWithCommand(config, castServerHost(server.URL("")), castDetectFirewall())
					hint = castCopyFirewallCommand(command, hint)
					return fmt.Errorf("cast: %s never fetched the stream from this machine.\n%s", device.Name, hint)
				}
				return fmt.Errorf("cast: %s never started playing -- it fetched the stream but would not play it", device.Name)
			}
			return finish()
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
		} else if started && !paused && remuxSucceeded() && progress.Duration > 0 &&
			progress.Position >= progress.Duration-castEndSlack &&
			time.Since(lastPositionChange) >= castEndStallTimeout {
			// Frozen on the last frame of an episode that was fully delivered:
			// the receiver has played everything there is and simply never
			// said so. See castEndStallTimeout.
			Log(fmt.Sprintf("cast: the device stopped at %.1f of %.1f, treating the episode as finished", progress.Position, progress.Duration))
			if !marked && cast.ShouldMarkComplete(progress, config.PercentageToMarkComplete) {
				markWatched(progress.Position, progress.Duration)
			}
			return finish()
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
			// The device reports dur=-1 for the whole episode: ffmpeg writes no
			// EXT-X-ENDLIST until the remux finishes, so the receiver treats the
			// playlist as live and never learns a length. The panel showed --:--
			// for the total because of it. The provider already told us how long
			// the episode is, so prefer that and fall back to the device only
			// when we were told nothing.
			total := progress.Duration
			estimated := false
			if total <= 0 && anime.Ep.Duration > 0 {
				total = float64(anime.Ep.Duration)
				estimated = durationEstimated
			}
			fmt.Print(panel.frame(castPanelState{
				Title:     animeName,
				Episode:   episodeNumber,
				Device:    device.Name,
				Position:  progress.Position,
				Duration:  total,
				Estimated: estimated,
				State:     state,
				Volume:    session.Volume(),
				Skips:     spans,
			}))
		}
		// Finished a moment early, on the last poll before the end. Receivers
		// stop answering status checks as the last segment plays out (seen on
		// hardware: the final answer at 6.8s of a 7.4s stream, then silence
		// until castProgressTimeout), so waiting for the true end cost the
		// viewer ten seconds of a frozen panel before the countdown. Only a
		// finished remux: before that the duration is the live edge, and being
		// near it means ffmpeg is behind, not that the episode is over.
		if started && !paused && remuxSucceeded() && progress.Duration > 0 &&
			progress.Position >= progress.Duration-castEndLead {
			Log(fmt.Sprintf("cast: at %.1f of %.1f, finishing", progress.Position, progress.Duration))
			if !marked && cast.ShouldMarkComplete(progress, config.PercentageToMarkComplete) {
				markWatched(progress.Position, progress.Duration)
			}
			return finish()
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

		if target, ok := cast.NextSkip(progress.Position, spans); ok && !failedSkips[target] {
			// finishAtEnd ends the episode on the device's behalf. The receiver
			// is paused first, or the ending the viewer asked to skip plays on
			// under the next-episode countdown: returning only stops this loop
			// watching, not the receiver playing.
			finishAtEnd := func() error {
				if err := session.Pause(); err != nil {
					Log(fmt.Sprintf("cast: could not hold the ending: %v", err))
				}
				end := cast.Progress{Position: progress.Duration, Duration: progress.Duration}
				if !marked && remuxSucceeded() && cast.ShouldMarkComplete(end, config.PercentageToMarkComplete) {
					markWatched(progress.Duration, progress.Duration)
				}
				return finish()
			}
			nearEnd := progress.Duration > 0 && target >= progress.Duration-castSeekTailGuard
			// An ending that runs right to the last second leaves nothing after
			// it to rebuild a stream for.
			if progress.Duration > 0 && target >= progress.Duration-castSkipNothingLeft {
				Log(fmt.Sprintf("cast: the skip to %.1f reaches the end of the episode, finishing", target))
				return finishAtEnd()
			}
			if err := session.SeekToTime(target); err != nil {
				Log(fmt.Sprintf("cast: skip failed: %v", err))
				// A few seconds after an ending are still worth a rebuild -- a
				// post-credits line, a preview -- so it is tried. But ffmpeg's
				// HLS demuxer produces no frames at all for a start inside the
				// source's last segment (seen on hardware: a 6.4s final segment
				// at 1414.6, and -ss 1415 onwards gave nothing while 1414 played
				// to the end), and a hardware encoder then refuses to open. So
				// the rebuild is retried a little earlier, replaying a few
				// seconds of the ending rather than losing what follows it.
				if nearEnd && !castSkipBackOff(session, target, progress.Position) {
					return finishAtEnd()
				}
				// Settled either way. After a back-off the stream starts inside
				// the span on purpose, and without this the next poll would
				// read that as the ending still playing and skip it again.
				failedSkips[target] = true
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

		// Only noted here, not written: the episode is marked when the cast
		// ends (the deferred check above), as the mpv path does. Marking the
		// moment the position crossed the threshold meant a seek past it marked
		// an episode the viewer was still watching -- or had seeked into by
		// mistake and then back out of, which clears it again below.
		if !cast.ShouldMarkComplete(progress, config.PercentageToMarkComplete) {
			reached = false
		} else if remuxSucceeded() {
			reached = true
			reachedDuration = progress.Duration
		}
	}
}

// chooseCastDevice finds the device to play on, asking only when the answer is
// not already obvious.
//
// castSessionDevice remembers which device the viewer picked this run, so a
// later episode in the same process -- the normal case, since runCastLoop
// calls this again for every episode -- does not re-prompt when nothing in
// the config says which device to use. Session-only: never written to
// config, gone the moment the process exits.
var castSessionDevice string

func chooseCastDevice(config *Config) (cast.Device, error) {
	Out("Looking for cast devices...")
	cast.SetKodi(cast.KodiSettings{
		Hosts:    strings.Split(config.KodiHost, ","),
		User:     config.KodiUser,
		Password: config.KodiPassword,
	})
	cast.SetDiscoveryPort(config.CastDiscoveryPort)
	devices, err := cast.Discover(context.Background(), cast.DefaultDiscoveryTimeout)
	if err != nil {
		return cast.Device{}, err
	}
	if len(devices) == 0 {
		const none = "cast: no devices found on this network"
		if runtime.GOOS == "linux" {
			if hint, command := castDiscoveryFirewallHint(config, cast.LocalAddress(), castDetectFirewall()); hint != "" {
				return cast.Device{}, errors.New(none + "\n" + castCopyFirewallCommand(command, hint))
			}
		}
		return cast.Device{}, errors.New(none)
	}

	configuredMissing := false
	if configured := config.CastDevice; configured != "" {
		for _, device := range devices {
			if device.Name == configured {
				castSessionDevice = device.Name
				return device, nil
			}
		}
		Out(fmt.Sprintf("%q was not found; pick another device.", configured))
		configuredMissing = true
	} else if castSessionDevice != "" {
		// Nothing configured, but this run already picked once -- honour
		// that pick again rather than asking a second time. Falls through
		// to the normal menu if the remembered device is no longer
		// answering discovery (turned off, renamed).
		for _, device := range devices {
			if device.Name == castSessionDevice {
				return device, nil
			}
		}
	}

	// The lone device is not "another device" when a configured name just
	// failed to match it -- that is silently ignoring the mismatch and
	// casting to it anyway, right after telling the user to pick.
	if len(devices) == 1 && !configuredMissing {
		castSessionDevice = devices[0].Name
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
	// Escape is the viewer declining, not a failure: reported as a stop, so
	// no caller retries it -- the rofi handoff used to fall back to casting in
	// place and show this same menu a second time.
	if selected.Key == "-1" || selected.Key == "-2" {
		return cast.Device{}, ErrCastStopped
	}
	for _, device := range devices {
		if device.UUID == selected.Key {
			castSessionDevice = device.Name
			return device, nil
		}
	}
	return cast.Device{}, fmt.Errorf("cast: no device chosen")
}

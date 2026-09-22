package internal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/thexykril/otakase/internal/cast"
)

// castPollInterval is how often the device is asked where it is. A second is
// enough to catch a skip window and to notice the episode ending, without
// making a conversation out of it.
const castPollInterval = time.Second

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
const castStartupGrace = 30 * time.Second

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
			_ = os.RemoveAll(streamDir)
		})
	}
	cancel := RegisterExitCleanup(teardown)
	defer cancel()
	defer teardown()

	Out(fmt.Sprintf("Preparing the stream for %s...", device.Name))
	rx, err := cast.StartRemux(ffmpeg, streamURL, referrer, streamDir)
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

	srv, err := cast.NewServer(streamDir)
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
		Out("Note: this stream's subtitles cannot be cast. Try SubStyle=hard for a hardsubbed stream.")
	}

	if err := s.Play(srv.URL(cast.PlaylistName)); err != nil {
		return err
	}
	Out(fmt.Sprintf("Playing on %s.", device.Name))

	return watchCast(config, anime, s, srv, rx, device)
}

// watchCast follows the episode while the device plays it.
func watchCast(config *Config, anime *Anime, session *cast.Session, server *cast.Server, remux *cast.Remux, device cast.Device) error {
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
	startupDeadline := time.Now().Add(castStartupGrace)

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

	for {
		time.Sleep(castPollInterval)

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
			return serverErr
		}

		progress, err := session.Progress()
		if err != nil {
			Out(fmt.Sprintf("Lost contact with %s.", device.Name))
			Log(fmt.Sprintf("cast: lost contact with the device: %v", err))
			return nil
		}

		if progress.Idle {
			if !started {
				if remuxErr != nil {
					return remuxErr
				}
				if time.Now().Before(startupDeadline) {
					continue
				}
				return fmt.Errorf("cast: %s never started playing -- it may not be able to reach this machine on the network", device.Name)
			}
			if remuxErr != nil {
				return remuxErr
			}
			Out("Playback finished.")
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

		anime.Ep.Player.PlaybackTime = int(progress.Position)
		// Until ffmpeg writes EXT-X-ENDLIST the playlist only advertises what
		// has been remuxed so far, so this duration is a live edge, not an
		// episode length. Marking against it completes the episode early on any
		// source slower than playback.
		if progress.Duration > 0 && remuxDone() {
			anime.Ep.Duration = int(progress.Duration)
		}

		if target, ok := cast.NextSkip(progress.Position, spans); ok {
			if err := session.SeekToTime(target); err != nil {
				Log(fmt.Sprintf("cast: skip failed: %v", err))
			}
			continue
		}

		if !marked && remuxDone() && cast.ShouldMarkComplete(progress, config.PercentageToMarkComplete) {
			marked = true
			LocalUpdateAnime(
				filepath.Join(os.ExpandEnv(config.StoragePath), "curd_history.txt"),
				anime.AnilistId, anime.ProviderId, anime.Ep.Number,
				int(progress.Position), int(progress.Duration),
				GetAnimeName(*anime), CurrentAnimeProviderName(anime),
			)
			// GetGlobalUser returns nil when nothing signed in, and local-only
			// tracking is a supported mode -- dereferencing it here would panic
			// on the one path a local-only user reaches.
			if user := GetGlobalUser(); UsesRemoteTracking(config) && user != nil {
				if err := UpdateAnimeProgress(user.Token, anime.AnilistId, anime.Ep.Number); err != nil {
					Log(fmt.Sprintf("cast: could not update remote progress: %v", err))
				}
			}
			Out(fmt.Sprintf("Episode %d marked as watched.", anime.Ep.Number))
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

	if configured := config.CastDevice; configured != "" {
		for _, device := range devices {
			if device.Name == configured {
				return device, nil
			}
		}
		Out(fmt.Sprintf("%q was not found; pick another device.", configured))
	}

	if len(devices) == 1 {
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

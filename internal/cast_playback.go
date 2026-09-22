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
	var (
		remux   *cast.Remux
		server  *cast.Server
		session *cast.Session
	)
	var teardownOnce sync.Once
	teardown := func() {
		teardownOnce.Do(func() {
			if session != nil {
				_ = session.Stop()
			}
			if server != nil {
				_ = server.Close()
			}
			if remux != nil {
				remux.Stop()
			}
			_ = os.RemoveAll(streamDir)
		})
	}
	cancel := RegisterExitCleanup(teardown)
	defer cancel()
	defer teardown()

	Out(fmt.Sprintf("Preparing the stream for %s...", device.Name))
	remux, err = cast.StartRemux(ffmpeg, streamURL, referrer, streamDir)
	if err != nil {
		return err
	}

	playlistErr := make(chan error, 1)
	go func() {
		playlistErr <- cast.WaitForPlaylist(streamDir, castStartTimeout)
	}()

	select {
	case err := <-playlistErr:
		if err != nil {
			if remuxErr := remux.Err(); remuxErr != nil {
				return remuxErr
			}
			return err
		}
	case <-remux.Done():
		if remuxErr := remux.Err(); remuxErr != nil {
			return remuxErr
		}
		// ffmpeg exited without complaint, which a stream short enough to
		// finish before the wait noticed will do. The playlist was written
		// either way, so take the wait's own answer.
		if err := <-playlistErr; err != nil {
			return err
		}
	}

	server, err = cast.NewServer(streamDir)
	if err != nil {
		return err
	}

	session, err = cast.Connect(device)
	if err != nil {
		return err
	}

	if anime.Ep.SubtitleURL != "" {
		// The Default Media Receiver renders WebVTT only, and Load carries no
		// subtitle track. Say so rather than letting the episode arrive silently
		// without the subtitles the viewer was expecting.
		Out("Note: this stream's subtitles cannot be cast. Try SubStyle=hard for a hardsubbed stream.")
	}

	if err := session.Play(server.URL(cast.PlaylistName)); err != nil {
		return err
	}
	Out(fmt.Sprintf("Playing on %s.", device.Name))

	return watchCast(config, anime, session, server, remux, device)
}

// watchCast follows the episode while the device plays it.
func watchCast(config *Config, anime *Anime, session *cast.Session, server *cast.Server, remux *cast.Remux, device cast.Device) error {
	spans := castSpansFor(anime.Ep.SkipTimes, config)
	marked := false

	// started tracks whether the device has ever reported anything but idle.
	// Session.Progress returns Progress{Idle: true} both for "no status
	// received yet" and for "the device refused the load" -- the same value
	// PlayerState == "IDLE" produces once an episode ends. Without this, a
	// device that never started (AP/client isolation, a guest VLAN) reaches
	// the viewer as "Playing on X." followed a second later by "Playback
	// finished." on a black screen.
	started := false
	startupDeadline := time.Now().Add(castStartupGrace)

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

		// Checked before the device is polled, and before the server: ffmpeg
		// dying leaves the server healthy and the device playing out what is
		// already on disk, so this is the failure most likely to reach a
		// viewer disguised as a finished episode.
		if remuxErr := remux.Err(); remuxErr != nil {
			return remuxErr
		}
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
				if time.Now().Before(startupDeadline) {
					continue
				}
				return fmt.Errorf("cast: %s never started playing -- it may not be able to reach this machine on the network", device.Name)
			}
			Out("Playback finished.")
			return nil
		}
		started = true

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

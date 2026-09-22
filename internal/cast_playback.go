package internal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
		return err
	}

	device, err := chooseCastDevice(config)
	if err != nil {
		return err
	}

	// The stream is remuxed into a directory per cast and thrown away after:
	// it is a transcode buffer, not a download.
	streamDir, err := os.MkdirTemp("", "otakase-cast-")
	if err != nil {
		return fmt.Errorf("cast: could not create the stream directory: %w", err)
	}
	defer os.RemoveAll(streamDir)

	streamURL := PrioritizeLink(anime.Ep.Links)
	referrer := anime.Ep.StreamReferrer
	if referrer == "" {
		referrer = streamReferrer(CurrentAnimeProviderName(anime))
	}

	Out(fmt.Sprintf("Preparing the stream for %s...", device.Name))
	remux, err := cast.StartRemux(ffmpeg, streamURL, referrer, streamDir)
	if err != nil {
		return err
	}
	defer remux.Stop()

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

	server, err := cast.NewServer(streamDir)
	if err != nil {
		return err
	}
	defer server.Close()

	session, err := cast.Connect(device)
	if err != nil {
		return err
	}
	defer session.Stop()

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

	return watchCast(config, anime, session, server)
}

// watchCast follows the episode while the device plays it.
func watchCast(config *Config, anime *Anime, session *cast.Session, server *cast.Server) error {
	spans := castSpansFor(anime.Ep.SkipTimes, config)
	marked := false

	for {
		time.Sleep(castPollInterval)

		// Checked before the device is polled: a dead server is the cause and
		// the device going idle is only the symptom, so reporting it first is
		// what turns "Playback finished." into the truth.
		if serverErr := server.Err(); serverErr != nil {
			return serverErr
		}

		progress, err := session.Progress()
		if err != nil {
			Log(fmt.Sprintf("cast: lost contact with the device: %v", err))
			return nil
		}
		if progress.Idle {
			Out("Playback finished.")
			return nil
		}

		anime.Ep.Player.PlaybackTime = int(progress.Position)
		if progress.Duration > 0 {
			anime.Ep.Duration = int(progress.Duration)
		}

		if target, ok := cast.NextSkip(progress.Position, spans); ok {
			if err := session.SeekToTime(target); err != nil {
				Log(fmt.Sprintf("cast: skip failed: %v", err))
			}
			continue
		}

		if !marked && cast.ShouldMarkComplete(progress, config.PercentageToMarkComplete) {
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

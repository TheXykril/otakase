package internal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/cast"
)

// pushCastRatingCard shows a rating card on the cast device for the season-end
// score prompt, reconnecting to it first.
//
// By the time HandleLastEpisodeCompletion runs, CastEpisode has already torn
// its session down -- the device is disconnected and its stream server is
// closed. Nothing here can reuse that connection, so this opens a small one
// of its own: discover, match the configured device, serve one PNG, load it,
// and hand back a release that disconnects again.
//
// The work runs on its own goroutine rather than blocking the caller:
// discovery alone takes cast.DefaultDiscoveryTimeout, and the score picker
// this decorates must start answering the viewer immediately -- not after a
// reconnect that a powered-off or unreachable device would otherwise make the
// viewer wait out for nothing. The returned release func waits for the
// background work if it is still running, so a fast score pick after a slow
// reconnect still cleans up correctly instead of leaking a connection.
//
// Every failure is logged and answered with a no-op cleanup. A viewer without
// a working reconnect still gets the terminal-only prompt exactly as before
// this feature existed -- the card is decoration, not a dependency of rating
// actually working.
func pushCastRatingCard(config *Config, anime *Anime) func() {
	result := make(chan func(), 1)
	go func() { result <- doPushCastRatingCard(config, anime) }()

	return func() { (<-result)() }
}

// doPushCastRatingCard is pushCastRatingCard's actual work, split out so it
// can run on its own goroutine without pushCastRatingCard's signature having
// to change.
func doPushCastRatingCard(config *Config, anime *Anime) func() {
	noop := func() {}
	if config == nil || anime == nil || strings.TrimSpace(config.CastDevice) == "" {
		return noop
	}

	devices, err := cast.Discover(context.Background(), cast.DefaultDiscoveryTimeout)
	if err != nil {
		Log(fmt.Sprintf("cast: could not discover devices for the rating card: %v", err))
		return noop
	}
	device, ok := findCastDeviceByName(devices, config.CastDevice)
	if !ok {
		Log(fmt.Sprintf("cast: %q was not found for the rating card", config.CastDevice))
		return noop
	}

	scratchRoot := filepath.Join(os.ExpandEnv(config.StoragePath), "cast-scratch")
	if err := os.MkdirAll(scratchRoot, 0o755); err != nil {
		Log(fmt.Sprintf("cast: could not create a scratch directory for the rating card: %v", err))
		return noop
	}
	dir, err := os.MkdirTemp(scratchRoot, "otakase-rating-")
	if err != nil {
		Log(fmt.Sprintf("cast: could not create a scratch directory for the rating card: %v", err))
		return noop
	}
	cleanupDir := func() { _ = os.RemoveAll(dir) }

	// The configured firewall port, same as the episode server: a viewer who
	// opened one port for casting must not have the rating card silently fail
	// to reach the device because this server picked a different, unopened
	// one.
	srv, err := cast.NewServerOnPort(dir, config.CastPort)
	if err != nil {
		Log(fmt.Sprintf("cast: could not serve the rating card: %v", err))
		cleanupDir()
		return noop
	}

	session, err := cast.Connect(device)
	if err != nil {
		Log(fmt.Sprintf("cast: could not reconnect to %s for the rating card: %v", device.Name, err))
		_ = srv.Close()
		cleanupDir()
		return noop
	}

	// Skipped rather than forced: a device already running something else --
	// a cast started from another device, YouTube, Netflix -- must not be
	// pulled away from it just to show a rating prompt nobody asked for on
	// that screen.
	if !session.ReceiverIsAvailable() {
		Log(fmt.Sprintf("cast: %s is running something else, skipping the rating card", device.Name))
		stopSessionWithin(session, castStopTimeout)
		_ = srv.Close()
		cleanupDir()
		return noop
	}

	lines := []string{GetAnimeName(*anime), "Rate this anime - check your terminal"}
	card := buildIdleCardPNG(anime.CoverImage, lines)
	if err := os.WriteFile(filepath.Join(dir, "rating.png"), card, 0o644); err != nil {
		Log(fmt.Sprintf("cast: could not write the rating card: %v", err))
		stopSessionWithin(session, castStopTimeout)
		_ = srv.Close()
		cleanupDir()
		return noop
	}
	if err := session.PlayImage(srv.URL("rating.png")); err != nil {
		Log(fmt.Sprintf("cast: could not show the rating card: %v", err))
		// Fall through to the same cleanup as a successful push: the
		// connection and server were still opened and still need closing.
	}

	return func() {
		stopSessionWithin(session, castStopTimeout)
		_ = srv.Close()
		cleanupDir()
	}
}

// stopSessionWithin bounds Session.Stop the same way CastEpisode's teardown
// does: it is the one step here that talks to the device, and a device that
// has stopped answering must not hold up the rest of cleanup.
func stopSessionWithin(session *cast.Session, within time.Duration) {
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		_ = session.Stop()
	}()
	select {
	case <-stopped:
	case <-time.After(within):
	}
}

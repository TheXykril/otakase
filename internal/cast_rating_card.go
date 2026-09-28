package internal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/thexykril/otakase/internal/cast"
)

// pushCastRatingCard shows a rating card on the cast device for the season-end
// score prompt, reconnecting to it first.
//
// By the time HandleLastEpisodeCompletion runs, CastEpisode has already torn
// its session down -- the device is disconnected and its stream server is
// closed. Nothing here can reuse that connection, so this opens a small one
// of its own: discover, match the configured device, serve one PNG, load it,
// and hand back a cleanup that disconnects again.
//
// Every failure is logged and answered with a no-op cleanup. A viewer without
// a working reconnect still gets the terminal-only prompt exactly as before
// this feature existed -- the card is decoration, not a dependency of rating
// actually working.
func pushCastRatingCard(config *Config, anime *Anime) func() {
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

	srv, err := cast.NewServer(dir)
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

	lines := []string{GetAnimeName(*anime), "Rate this anime — check your terminal"}
	card := buildIdleCardPNG(anime.CoverImage, lines)
	if err := os.WriteFile(filepath.Join(dir, "rating.png"), card, 0o644); err != nil {
		Log(fmt.Sprintf("cast: could not write the rating card: %v", err))
		_ = session.Stop()
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
		_ = session.Stop()
		_ = srv.Close()
		cleanupDir()
	}
}

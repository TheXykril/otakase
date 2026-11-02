package internal

import (
	"strings"
	"sync/atomic"
)

// mpv's own playlist doubles as an episode picker. One row is the real
// stream; every other row is a black lavfi clip with an #EXTINF title. When
// the viewer lands on one of those, the controller resolves that episode and
// swaps it in.

const ()

// While an episode switch runs, time-pos disappears for a bit. The playback
// monitor checks this flag so it doesn't read that as mpv being closed.
var playlistSwitching atomic.Bool

// PlaylistSwitchInProgress is true while the playlist is swapping episodes.
func PlaylistSwitchInProgress() bool { return playlistSwitching.Load() }

// playlistAudioMode is the audio the playlist rows are built for.
//
// That is whatever is playing, not what the config asks for. With
// AutoAudioFallback a sub-only show plays sub even for a dub user, and rows
// built for dub would all fail to resolve.
//
// Ep.Mode must be checked for emptiness before normalising it: normalising
// "" gives "sub", which would override a dub preference.
func playlistAudioMode(anime *Anime, config *Config) string {
	if anime != nil && strings.TrimSpace(anime.Ep.Mode) != "" {
		return normalizeTranslationType(anime.Ep.Mode)
	}
	if config != nil {
		return normalizeTranslationType(config.SubOrDub)
	}
	return "sub"
}

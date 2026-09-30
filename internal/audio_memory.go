package internal

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/thexykril/otakase/internal/cast"
)

// Sub or dub, remembered per show. Switching mid-episode already works -- the
// player's playlist offers the other audio as its own row, and a cast takes
// the a key -- but the next launch went back to the global SubOrDub, so a
// show watched dubbed in a subbed-by-default setup had to be switched every
// single time.

// configuredAudio is SubOrDub as the config and flags set it, captured before
// any show's memory changes it for a run. A show without a memory plays this,
// not whatever the previous show in the same run left behind.
var configuredAudio struct {
	once sync.Once
	mode string
}

func configuredAudioMode(config *Config) string {
	configuredAudio.once.Do(func() {
		configuredAudio.mode = normalizeTranslationType(config.SubOrDub)
	})
	return configuredAudio.mode
}

// applyShowAudioMode sets the audio for the show about to play: what the
// viewer last switched it to, else the configured default. -sub and -dub
// given for this run win over both.
func applyShowAudioMode(config *Config, anime *Anime) {
	if config == nil || anime == nil {
		return
	}
	base := configuredAudioMode(config)
	if config.SubOrDubFlag {
		return
	}
	mode := base
	if remembered := loadShowPrefs(config.StoragePath)[strconv.Itoa(anime.AnilistId)].AudioMode; remembered != "" {
		mode = normalizeTranslationType(remembered)
	}
	if mode != normalizeTranslationType(config.SubOrDub) {
		Log(fmt.Sprintf("Audio for show %d: %s (remembered)", anime.AnilistId, mode))
	}
	config.SubOrDub = mode
}

// rememberShowAudioMode records the audio the viewer switched a show to.
func rememberShowAudioMode(storagePath string, anilistID int, mode string) {
	mode = normalizeTranslationType(mode)
	if err := updateShowPrefs(storagePath, anilistID, func(p *ShowPrefs) {
		p.AudioMode = mode
	}); err != nil {
		Log(fmt.Sprintf("Could not remember %s for show %d: %v", mode, anilistID, err))
	}
}

// castAudioSwitch ends a cast episode so it can restart in the other
// language: where it was, and on which device.
type castAudioSwitch struct {
	position float64
	device   cast.Device
}

func (s *castAudioSwitch) Error() string { return "cast: switching audio" }

// switchCastAudio re-resolves the episode in the other language and sets it
// to resume at position. When that language is not there, the episode is left
// as it was and resumes in the language it had.
func switchCastAudio(config *Config, anime *Anime, position float64) {
	current := normalizeTranslationType(anime.Ep.Mode)
	if strings.TrimSpace(anime.Ep.Mode) == "" {
		current = normalizeTranslationType(config.SubOrDub)
	}
	want := alternateTranslationType(current)

	// Falling back without asking: a prompt cannot be drawn under the panel,
	// and the fallback is detected below instead.
	before := anime.Ep
	fallback := config.AutoAudioFallback
	config.SubOrDub, config.AutoAudioFallback = want, true
	resolved := switchAudioResolver(config, anime)
	config.AutoAudioFallback = fallback

	if !resolved || normalizeTranslationType(anime.Ep.Mode) != want {
		anime.Ep = before
		config.SubOrDub = current
		Out(fmt.Sprintf("No %s for this episode, so it stays %s.", want, current))
		Log(fmt.Sprintf("cast: no %s for episode %d; staying on %s", want, anime.Ep.Number, current))
	} else {
		rememberShowAudioMode(config.StoragePath, anime.AnilistId, want)
		Log(fmt.Sprintf("cast: episode %d switched to %s at %.0fs", anime.Ep.Number, want, position))
	}

	anime.Ep.Resume = true
	anime.Ep.Player.PlaybackTime = int(position)
	// castResumeAt reads the history row of the provider now playing, which a
	// switch may have changed; this carries the position across regardless.
	anime.syncedResume = syncedResume{Episode: anime.Ep.Number, Seconds: int(position)}
}

// switchAudioResolver finds the episode's links for config.SubOrDub. Tests
// replace it.
var switchAudioResolver = ResolveEpisodeLinks

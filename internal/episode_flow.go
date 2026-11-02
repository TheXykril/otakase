package internal

import (
	"fmt"
)

// episodeLinkResolver is the resolve ResolveEpisodeLinks runs. A variable so a
// test can drive the callers without a provider or a network.
var episodeLinkResolver = resolveEpisodeLinksWithRecovery

// ResolveEpisodeLinks fills anime.Ep with a playable stream for the episode it
// names, reporting whether it found one.
//
// Preferred-first resolve; the diagnosed recovery menu only after that fails.
// ok=false means nothing played and the caller should stop -- either the
// resolve found nothing or the viewer backed out of the recovery menu.
//
// It is shared rather than inlined into StartPlayback because the process
// spawned for a rofi cast never reaches StartPlayback: it casts episode after
// episode itself, and StartNextEpisode clears Ep.Links "to force fetching new
// ones" without fetching anything. This is that fetch, for both callers.
func ResolveEpisodeLinks(config *Config, anime *Anime) bool {
	endBusy := BeginBusy(config, fmt.Sprintf("Finding episode %d", anime.Ep.Number))
	episodeResult, ok := episodeLinkResolver(config, anime, nil)
	endBusy()
	if !ok {
		return false
	}
	Log(fmt.Sprintf("Successfully retrieved %s/%s episode link. Links count: %d", episodeResult.ProviderName, episodeResult.Mode, len(episodeResult.Links)))
	anime.Ep.Links = episodeResult.Links
	anime.Ep.Mode = episodeResult.Mode
	applyStreamPlaybackHints(anime, anime.Ep.Links, episodeResult.LinkHints)
	return true
}

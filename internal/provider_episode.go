package internal

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/thexykril/otakase/internal/icons"
)

// noStreamsError is the alternate audio coming up empty on every provider,
// as opposed to a provider failing to answer.
type noStreamsError struct{ mode string }

func (e noStreamsError) Error() string {
	return fmt.Sprintf("no %s streams available", e.mode)
}

// providerForID returns the provider a qualified id names with the id
// inside it, or GetProvider and the id as given when it is bare.
func providerForID(id string) (Provider, string, error) {
	providerName, rawID, ok := ParseProviderQualifiedID(id)
	if !ok {
		return GetProvider(), id, nil
	}
	provider, err := ProviderByName(providerName)
	return provider, rawID, err
}

// EpisodesList lists a show's episode numbers as its provider writes them.
func EpisodesList(showID, mode string) ([]string, error) {
	provider, rawID, err := providerForID(showID)
	if err != nil {
		return nil, err
	}
	return provider.EpisodesList(rawID, mode)
}

// GetProviderTotalEpisodes is the highest episode number a show's provider
// lists in either audio.
func GetProviderTotalEpisodes(showID, mode string) (int, error) {
	provider, rawID, err := providerForID(showID)
	if err != nil {
		return 0, err
	}
	return getProviderTotalEpisodes(provider, rawID, mode)
}

// getProviderTotalEpisodes checks both audios, since a dub that is still
// airing lists fewer episodes than the sub.
func getProviderTotalEpisodes(provider Provider, showID, mode string) (int, error) {
	if provider == nil {
		return 0, fmt.Errorf("provider is not configured")
	}
	if strings.TrimSpace(showID) == "" {
		return 0, fmt.Errorf("provider id is empty")
	}

	preferred := normalizeTranslationType(mode)
	total := 0
	var failures []string
	for _, lookupMode := range []string{preferred, alternateTranslationType(preferred)} {
		episodes, err := provider.EpisodesList(showID, lookupMode)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", lookupMode, err))
			continue
		}
		total = max(total, inferTotalEpisodesFromEpisodeList(episodes))
	}

	switch {
	case total > 0:
		return total, nil
	case len(failures) > 0:
		return 0, fmt.Errorf("provider episode list lookup failed: %s", strings.Join(failures, "; "))
	}
	return 0, fmt.Errorf("provider returned no usable episode numbers")
}

// determineProviderTotalEpisodes finds how many episodes a show has from the
// providers, for a show the tracker has no count for. The stored mapping is
// asked first; failing that, each configured provider is searched in turn,
// so one host being down or not having the show yet does not leave the
// total unknown. An anime played straight from a tracker list has no
// mapping at all.
func determineProviderTotalEpisodes(config *Config, query string, anime *Anime, mode string) (int, error) {
	if anime == nil {
		return 0, fmt.Errorf("missing anime")
	}

	if providerName, providerID := AnimeProviderID(anime); providerID != "" {
		if provider, err := ProviderByName(providerName); err == nil {
			if total, err := getProviderTotalEpisodes(provider, providerID, mode); err == nil && total > 0 {
				return total, nil
			}
		}
	}

	scoringQuery := strings.TrimSpace(query)
	if scoringQuery == "" {
		scoringQuery = animeSearchTitle(anime)
	}
	var failures []string
	for _, providerName := range configuredProviderNames(config) {
		provider, err := ProviderByName(providerName)
		if err != nil {
			continue
		}
		options, err := provider.SearchAnime(query, mode)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s search: %v", providerName, err))
			continue
		}
		option, ok := selectBestProviderSearchResult(options, anime, scoringQuery)
		if !ok {
			continue
		}
		providerID := option.Key
		if _, rawID, qualified := ParseProviderQualifiedID(option.Key); qualified {
			providerID = rawID
		}
		if strings.TrimSpace(providerID) == "" {
			continue
		}
		if total, err := getProviderTotalEpisodes(provider, providerID, mode); err == nil && total > 0 {
			return total, nil
		}
	}

	if len(failures) > 0 {
		return 0, fmt.Errorf("could not determine total episodes from any provider: %s", strings.Join(failures, "; "))
	}
	return 0, fmt.Errorf("could not determine total episodes from any provider")
}

// inferTotalEpisodesFromEpisodeList is the highest whole episode number in a
// provider's list. Specials such as "12.5" count as the episode they follow.
func inferTotalEpisodesFromEpisodeList(episodes []string) int {
	total := 0
	for _, episode := range episodes {
		if number, err := strconv.ParseFloat(strings.TrimSpace(episode), 64); err == nil && number > 0 {
			total = max(total, int(number))
		}
	}
	return total
}

// GetEpisodeURL resolves an episode of a show by id, in config.SubOrDub.
func GetEpisodeURL(config Config, id string, epNo int) ([]string, error) {
	provider, rawID, err := providerForID(id)
	if err != nil {
		return nil, err
	}
	return getProviderEpisodeURLForMode(provider, config, rawID, epNo, config.SubOrDub)
}

// GetEpisodeURLForPlayback is GetEpisodeURL with the audio fallback: when the
// preferred audio has nothing, the other is offered, or taken outright with
// AutoAudioFallback on. It returns the audio the links are in.
func GetEpisodeURLForPlayback(config Config, id string, epNo int) ([]string, string, error) {
	preferredMode := normalizeTranslationType(config.SubOrDub)
	links, preferredErr := GetEpisodeURL(config, id, epNo)
	if preferredErr == nil && len(links) > 0 {
		return links, preferredMode, nil
	}

	fallbackMode := alternateTranslationType(preferredMode)
	fallbackConfig := config
	fallbackConfig.SubOrDub = fallbackMode
	fallbackLinks, fallbackErr := GetEpisodeURL(fallbackConfig, id, epNo)
	if fallbackErr != nil || len(fallbackLinks) == 0 {
		if preferredErr != nil {
			return nil, preferredMode, preferredErr
		}
		return nil, preferredMode, fallbackErr
	}

	// Like every other fallback, this one asks only when AutoAudioFallback is
	// off. It used to ask regardless, so the next episode of a show with no
	// dub stopped on a question with one useful answer.
	if !config.AutoAudioFallback {
		Out(audioFallbackPrompt(preferredMode, fallbackMode))
		selected, selectErr := promptSelect([]SelectionOption{
			{Key: "play", Label: "Play " + fallbackMode, Icon: icons.Audio},
			{Key: "cancel", Label: "Cancel", Icon: icons.No},
		})
		if selectErr != nil {
			return nil, preferredMode, selectErr
		}
		if selected.Key != "play" {
			return nil, preferredMode, preferredErr
		}
	} else {
		Out(fmt.Sprintf("No %s for episode %d — playing %s.", preferredMode, epNo, fallbackMode))
	}
	return fallbackLinks, fallbackMode, nil
}

func getProviderEpisodeURLForModeWithHints(provider Provider, config Config, id string, epNo int, mode string) ([]string, map[string]StreamPlaybackHint, error) {
	mode = normalizeTranslationType(mode)
	if resolver, ok := provider.(providerHintResolver); ok {
		return resolver.GetEpisodeURLForModeWithHints(config, id, epNo, mode)
	}
	links, err := getProviderEpisodeURLForMode(provider, config, id, epNo, mode)
	return links, nil, err
}

func getProviderEpisodeURLForMode(provider Provider, config Config, id string, epNo int, mode string) ([]string, error) {
	mode = normalizeTranslationType(mode)
	if resolver, ok := provider.(ProviderModeResolver); ok {
		return resolver.GetEpisodeURLForMode(config, id, epNo, mode)
	}
	config.SubOrDub = mode
	return provider.GetEpisodeURL(config, id, epNo)
}

// episodeModeResult resolves an episode in one audio across the providers
// suited to the show.
func episodeModeResult(config Config, anime *Anime, epNo int, mode string) (ProviderEpisodeResult, error) {
	mode = normalizeTranslationType(mode)
	if anime == nil {
		config.SubOrDub = mode
		links, err := GetEpisodeURL(config, "", epNo)
		return ProviderEpisodeResult{Links: links, ProviderName: GetProvider().Name(), Mode: mode}, err
	}
	return episodeModeResultWithProviders(config, anime, epNo, mode, providerNamesForAnime(&config, anime))
}

// episodeModeResultWithProviders tries providerNames in order and takes the
// first that has the episode in mode. The anime's mapping is moved to that
// provider, so the next episode starts there.
func episodeModeResultWithProviders(config Config, anime *Anime, epNo int, mode string, providerNames []string) (ProviderEpisodeResult, error) {
	mode = normalizeTranslationType(mode)
	if len(providerNames) == 0 {
		providerNames = []string{firstEnabledProviderName()}
	}

	var failures []string
	for _, providerName := range providerNames {
		provider, err := ProviderByName(providerName)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}

		providerID, err := findProviderIDForAnime(provider, &config, anime, mode)
		if err != nil {
			noteProviderResult(providerName, "lookup", err)
			failures = append(failures, fmt.Sprintf("%s lookup: %v", providerName, err))
			continue
		}

		links, linkHints, err := getProviderEpisodeURLForModeWithHints(provider, config, providerID, epNo, mode)

		// A stored id can belong to a different host: otakase used to write the id
		// of whichever provider served an episode next to the name of the first
		// *configured* one, so anineko slugs ended up filed under "anipub" and
		// were handed back to anipub on every later run. Re-derive the mapping
		// once by searching before believing the provider does not carry it.
		if (err != nil || len(links) == 0) && storedProviderIDMatches(anime, providerName, providerID) {
			if fresh, searchErr := searchProviderIDForAnime(provider, &config, anime, mode); searchErr == nil && fresh != providerID {
				Log(fmt.Sprintf("Stored %s id %q did not work; re-derived %q", providerName, providerID, fresh))
				if freshLinks, freshHints, freshErr := getProviderEpisodeURLForModeWithHints(provider, config, fresh, epNo, mode); freshErr == nil && len(freshLinks) > 0 {
					providerID, links, linkHints, err = fresh, freshLinks, freshHints, nil
				}
			}
		}

		if err == nil && len(links) == 0 {
			err = fmt.Errorf("no episode links")
		}
		noteProviderResult(providerName, "episode", err)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s episode: %v", providerName, err))
			continue
		}

		anime.ProviderName = providerName
		anime.ProviderId = providerID
		result := ProviderEpisodeResult{
			Links:        links,
			LinkHints:    linkHints,
			ProviderName: providerName,
			ProviderID:   providerID,
			Mode:         mode,
		}
		// Every way an episode is resolved for playing, casting or
		// downloading ends here, so the quality is chosen once for all.
		applyQualityPreference(&config, anime, &result)
		return result, nil
	}

	return ProviderEpisodeResult{}, fmt.Errorf("no %s episode links found across providers %s for %q episode %d: %s",
		mode, strings.Join(providerNames, ","), animeSearchTitle(anime), epNo, strings.Join(failures, "; "))
}

// filterExcludedProviders drops the excluded providers, however they are
// spelled, and keeps the rest as given.
func filterExcludedProviders(providerNames []string, exclude []string) []string {
	excluded := uniqueProviderNames(exclude)
	return slices.DeleteFunc(slices.Clone(providerNames), func(name string) bool {
		return slices.Contains(excluded, normalizeProviderName(name))
	})
}

// ResolveEpisodeURLExcludingProvidersMode resolves an episode in an explicit
// audio, skipping the providers in exclude: the ones whose stream just failed.
func ResolveEpisodeURLExcludingProvidersMode(config Config, anime *Anime, epNo int, exclude []string, mode string) (ProviderEpisodeResult, error) {
	providerNames := filterExcludedProviders(providerNamesForAnime(&config, anime), exclude)
	if len(providerNames) == 0 {
		return ProviderEpisodeResult{}, fmt.Errorf("no providers left to try")
	}
	return episodeModeResultWithProviders(config, anime, epNo, mode, providerNames)
}

// ResolveEpisodeURLAlternateModeWithPrompt probes the non-preferred audio and,
// with AutoAudioFallback off, asks before switching to it. The preferred
// audio is never given up silently.
func ResolveEpisodeURLAlternateModeWithPrompt(config Config, anime *Anime, epNo int, exclude []string) (ProviderEpisodeResult, error) {
	// AutoAudioFallback is decided here rather than at each call site, so every
	// route into the alternate audio -- the preferred-first resolve, the recovery
	// menu, the playlist controller -- obeys the setting the same way.
	return resolveEpisodeURLAlternateMode(config, anime, epNo, exclude, !config.AutoAudioFallback, nil)
}

// ResolveEpisodeURLAlternateModeAfterFailure is the prompt variant for a
// stream that never started. When the other audio resolves to the very links
// that just failed -- a dual-audio torrent serves both -- switching cannot
// help, so it reports that instead of announcing a missing dub.
func ResolveEpisodeURLAlternateModeAfterFailure(config Config, anime *Anime, epNo int, exclude []string, failed []string) (ProviderEpisodeResult, error) {
	return resolveEpisodeURLAlternateMode(config, anime, epNo, exclude, !config.AutoAudioFallback, failed)
}

// ResolveEpisodeURLAlternateModeAuto switches to the other audio without asking.
//
// Some shows exist in one language only, and for those the prompt has a single
// useful answer -- it just stands between the user and the episode. What was
// played is still reported, so an automatic switch is never a silent one.
func ResolveEpisodeURLAlternateModeAuto(config Config, anime *Anime, epNo int, exclude []string) (ProviderEpisodeResult, error) {
	return resolveEpisodeURLAlternateMode(config, anime, epNo, exclude, false, nil)
}

func resolveEpisodeURLAlternateMode(config Config, anime *Anime, epNo int, exclude []string, ask bool, failed []string) (ProviderEpisodeResult, error) {
	preferredMode := normalizeTranslationType(config.SubOrDub)
	fallbackMode := alternateTranslationType(preferredMode)

	providerNames := filterExcludedProviders(providerNamesForAnime(&config, anime), exclude)
	if len(providerNames) == 0 {
		return ProviderEpisodeResult{}, fmt.Errorf("no providers left to try for %s", fallbackMode)
	}

	// The probe resolves on a copy: until the switch is accepted, the anime
	// keeps the mapping it had.
	probe := anime
	if anime != nil {
		copied := *anime
		probe = &copied
	}
	result, err := episodeModeResultWithProviders(config, probe, epNo, fallbackMode, providerNames)
	if err != nil {
		return ProviderEpisodeResult{}, err
	}
	if len(result.Links) == 0 {
		return ProviderEpisodeResult{}, noStreamsError{mode: fallbackMode}
	}
	if len(failed) > 0 && slices.Equal(result.Links, failed) {
		return ProviderEpisodeResult{}, fmt.Errorf("%s resolves to the same stream that failed to start", fallbackMode)
	}

	if ask {
		Out(audioFallbackPrompt(preferredMode, fallbackMode))
		selected, selectErr := promptSelect([]SelectionOption{
			{Key: "play", Label: "Play " + fallbackMode, Icon: icons.Audio},
			{Key: "cancel", Label: "Cancel", Icon: icons.No},
		})
		if selectErr != nil {
			return ProviderEpisodeResult{}, selectErr
		}
		if selected.Key != "play" {
			return ProviderEpisodeResult{}, fmt.Errorf("%s unavailable and %s fallback declined", preferredMode, fallbackMode)
		}
	} else {
		Out(fmt.Sprintf("No %s for episode %d — playing %s.", preferredMode, epNo, fallbackMode))
	}

	if anime != nil {
		anime.ProviderName = result.ProviderName
		anime.ProviderId = result.ProviderID
	}
	return result, nil
}

// isNoStreamsError reports whether err says the other audio has nothing
// anywhere, so offering it again is pointless.
func isNoStreamsError(err error) bool {
	var noStreams noStreamsError
	return errors.As(err, &noStreams)
}

// ResolveEpisodeURL resolves an episode in config.SubOrDub, with no fallback.
func ResolveEpisodeURL(config Config, anime *Anime, epNo int) (ProviderEpisodeResult, error) {
	return episodeModeResult(config, anime, epNo, config.SubOrDub)
}

// ResolveEpisodeURLForPlayback resolves an episode for playing. The preferred
// audio is tried on every provider before the other audio is considered:
// another host having the dub beats switching to the sub.
func ResolveEpisodeURLForPlayback(config Config, anime *Anime, epNo int) (ProviderEpisodeResult, error) {
	result, preferredErr := episodeModeResult(config, anime, epNo, config.SubOrDub)
	if preferredErr == nil && len(result.Links) > 0 {
		return result, nil
	}

	fallback, fallbackErr := ResolveEpisodeURLAlternateModeWithPrompt(config, anime, epNo, nil)
	if fallbackErr != nil || len(fallback.Links) == 0 {
		if preferredErr != nil {
			return ProviderEpisodeResult{}, preferredErr
		}
		return ProviderEpisodeResult{}, fallbackErr
	}
	return fallback, nil
}

func audioFallbackPrompt(preferredMode, fallbackMode string) string {
	unavailable := "Sub"
	if normalizeTranslationType(preferredMode) == "dub" {
		unavailable = "Dub"
	}
	return unavailable + " is unavailable for this episode. Play " + fallbackMode + " instead?"
}

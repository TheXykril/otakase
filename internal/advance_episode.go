package internal

// advanceOutcome is what finishing an episode means for what happens next.
//
// Separated from the doing so the decisions are testable without a tracker, a
// database file or a network: everything below is a question about the episode
// just watched, and none of it needs those to answer.
type advanceOutcome struct {
	// Continue reports that there is another episode to play.
	Continue bool
	// SeriesFinished reports that the episode just watched was the last one.
	SeriesFinished bool
	// PushProgress reports that the tracker should be told about this episode.
	PushProgress bool
}

// advanceDecision works out what finishing this episode means.
//
// wantsNext is the viewer's answer, however it was asked -- a menu for local
// playback, a countdown for a cast.
func advanceDecision(anime *Anime, wantsNext bool) advanceOutcome {
	if anime == nil {
		return advanceOutcome{}
	}

	// A show with no known episode count cannot be at its end.
	atEnd := anime.TotalEpisodes > 0 && anime.Ep.Number >= anime.TotalEpisodes

	return advanceOutcome{
		Continue:       wantsNext && !atEnd,
		SeriesFinished: atEnd,
		// A rewatch already has a completed entry, and pushing progress to it
		// rewrites a finished record.
		PushProgress: !anime.Rewatching,
	}
}

// AdvanceAfterEpisode completes the episode just watched and prepares the next
// one on anime, reporting whether there is another to play.
//
// It is the block that used to sit inside main's playback loop, moved here so
// the process spawned for a rofi cast runs the same code rather than a second
// copy of it. Nothing about it is cast-specific.
//
// wantsNext supplies the viewer's answer to "start the next episode?". The two
// callers ask differently -- local playback and an android-intent launch ask
// with a menu (NextEpisodePromptCLI), while a cast has already asked with its
// own countdown by the time this is called, and asking again would put an
// interactive menu in front of a viewer across the room. A nil wantsNext is
// treated as "no": a caller that forgot to supply an answer must not silently
// advance.
func AdvanceAfterEpisode(config *Config, anime *Anime, user *User, databaseFile string, wantsNext func() bool) bool {
	if config == nil || anime == nil || user == nil {
		return false
	}

	anime.Ep.IsCompleted = true
	LocalUpdateAnime(databaseFile, anime.AnilistId, anime.ProviderId, anime.Ep.Number, 0, 0,
		GetAnimeName(*anime), CurrentAnimeProviderName(anime))

	answer := false
	if wantsNext != nil {
		answer = wantsNext()
	}
	decided := advanceDecision(anime, answer)

	if decided.Continue {
		StartNextEpisode(anime, config, databaseFile, user.Token)
		return true
	}

	if decided.SeriesFinished {
		HandleLastEpisodeCompletion(config, anime, user.Token)
	}
	if decided.PushProgress {
		UpdateAnimeProgress(user.Token, anime.AnilistId, anime.Ep.Number)
	}
	return false
}

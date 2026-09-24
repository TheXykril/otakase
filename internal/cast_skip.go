package internal

// castSkipResolver is the shape of ApplySkipTimes, narrowed to what casting
// needs and injectable so the decision can be tested without four network
// lookups.
type castSkipResolver func(anime *Anime, episode int, config *Config, provider any)

// ensureCastSkipTimes resolves an episode's opening and ending times if the
// caller has not already.
//
// Casting needs this because it never reaches the place local playback gets
// them: StartPlayback runs a whole cast inline and returns no socket path, and
// main exits on that before the goroutine that would have resolved them ever
// starts. Without this an episode casts with no times at all -- nothing is
// skipped, and pressing s truthfully reports that nothing is known.
//
// Times already in hand are kept: the rofi handoff carries them in its session
// file, and looking them up again would pay for four sources twice.
func ensureCastSkipTimes(anime *Anime, config *Config, resolve castSkipResolver) {
	if anime == nil || resolve == nil {
		return
	}
	if anime.Ep.SkipTimes.Op.End > 0 || anime.Ep.SkipTimes.Ed.End > 0 {
		return
	}
	resolve(anime, anime.Ep.Number, config, GetProvider())
}

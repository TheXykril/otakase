package internal

import (
	"fmt"
	"strconv"
)

// Offering "start episode 11" for an episode that has not been broadcast yet
// sends the user into a dead end: they pick it, every provider is searched, and
// the answer is a failure that reads like something is broken. Nothing is
// broken -- the episode does not exist yet, and otakase already knows that, because
// the same tracker data drives the airing countdowns in the list.

// nextEpisodeAvailability describes whether the episode after the one just
// finished can actually be played.
type nextEpisodeAvailability struct {
	// Aired reports whether the episode has been broadcast. It is true whenever
	// the answer is unknown: guessing "not out" would refuse an episode the user
	// could have watched, which is the worse mistake of the two.
	Aired bool
	// Delay is how long until it airs, e.g. "4d". Empty when unknown.
	Delay string
}

// nextEpisodeAiring reports whether episode number can be watched yet, using the
// tracker entry otakase already holds in memory. No network request is made: this
// runs at the end of an episode, and a prompt that stalls on a lookup is worse
// than one that occasionally cannot say.
func nextEpisodeAiring(anime *Anime, number int) nextEpisodeAvailability {
	unknown := nextEpisodeAvailability{Aired: true}
	if anime == nil || anime.AnilistId == 0 {
		return unknown
	}

	user := GetGlobalUser()
	if user == nil {
		return unknown
	}

	entry, err := FindAnimeByAnilistID(user.AnimeList, strconv.Itoa(anime.AnilistId))
	if err != nil || entry == nil || entry.Media.NextAiringEpisode == nil {
		// A finished show has no next airing episode, and everything it has is
		// out; either way there is nothing to warn about.
		return unknown
	}

	next := entry.Media.NextAiringEpisode
	if next.Episode <= 0 {
		return unknown
	}

	// nextAiringEpisode names the episode still to broadcast, so anything below
	// it has aired.
	if number < next.Episode {
		return unknown
	}

	availability := nextEpisodeAvailability{Aired: false}
	// A countdown is only meaningful for the very next episode; anything beyond
	// it airs later still, and saying "4d" would understate the wait.
	if number == next.Episode {
		availability.Delay = formatAiringDelay(next.TimeUntilAiring)
	}
	return availability
}

// unairedEpisodeNotice is what to tell the user instead of offering the episode.
func unairedEpisodeNotice(number int, availability nextEpisodeAvailability) string {
	if availability.Delay != "" {
		return fmt.Sprintf("Episode %d airs in %s", number, availability.Delay)
	}
	return fmt.Sprintf("Episode %d has not aired yet", number)
}

// confirmUnairedEpisode reports an episode the tracker says is still to come and
// asks whether to look for it regardless. It answers true to go ahead.
//
// There is an escape hatch rather than a flat refusal because the schedule is
// read from a cached list: an episode that aired an hour ago can still be listed
// as upcoming, and refusing outright would mean otakase knowing better than the
// user about something it only half knows. Choosing to try lands exactly where
// this used to land on its own -- a provider search that may fail -- which is no
// worse than the behaviour it replaces.
func confirmUnairedEpisode(config *Config, number int, availability nextEpisodeAvailability) bool {
	notice := unairedEpisodeNotice(number, availability)
	Log(fmt.Sprintf("Episode availability: %s", notice))

	options := []SelectionOption{
		{Key: "wait", Label: "Done"},
		{Key: "try", Label: fmt.Sprintf("Look for episode %d anyway", number)},
	}

	if config != nil && config.RofiSelection {
		selected, err := RofiSelectWithMessage(options, false, "Caught up", notice)
		if err != nil {
			Log(fmt.Sprintf("Error showing the unaired episode notice: %v", err))
			return false
		}
		return selected.Key == "try"
	}

	Out(notice + ". You are caught up.")
	selected, err := promptSelectOrdered(options)
	if err != nil {
		Log(fmt.Sprintf("Error showing the unaired episode notice: %v", err))
		return false
	}
	return selected.Key == "try"
}

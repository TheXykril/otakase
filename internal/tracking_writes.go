package internal

import (
	"errors"
	"strconv"
)

// Routing list changes to whichever trackers are configured.

// resolveMyAnimeListID maps an AniList ID to MyAnimeList's, from the loaded
// list when it has the show and from AniList otherwise.
func resolveMyAnimeListID(anilistID int) (int, error) {
	if anilistID == 0 {
		return 0, errors.New("invalid AniList ID")
	}
	if session := GetGlobalUser(); session != nil {
		known, lookupErr := FindAnimeByAnilistID(session.AnimeList, strconv.Itoa(anilistID))
		if lookupErr == nil && known.Media.MalID != 0 {
			return known.Media.MalID, nil
		}
	}
	return GetAnimeMalID(anilistID)
}

// trackerWrite is one change as each tracker takes it. MyAnimeList is written
// by its own ID, which malID supplies; it is only looked up when MyAnimeList
// is about to be written.
type trackerWrite struct {
	aniList     func() error
	malID       func() (int, error)
	myAnimeList func(malID int) error
}

func (w trackerWrite) toMyAnimeList() error {
	id, lookupErr := w.malID()
	if lookupErr != nil {
		return lookupErr
	}
	return w.myAnimeList(id)
}

// writeTrackers applies a change to whichever trackers config writes to for
// anime. With both, AniList goes first and MyAnimeList is still written when
// AniList fails; the AniList error is the one returned.
func writeTrackers(config *Config, anime *Anime, write trackerWrite) error {
	route := ratingRouteFor(config, anime)
	if route == ratingRouteNone {
		return nil
	}

	var aniListErr, malErr error
	if route != ratingRouteMyAnimeList {
		aniListErr = write.aniList()
	}
	if route != ratingRouteAniList {
		malErr = write.toMyAnimeList()
	}
	if aniListErr != nil {
		return aniListErr
	}
	return malErr
}

// malIDFor returns a MyAnimeList ID lookup for a show: known when it is
// non-zero, otherwise resolved from the AniList ID.
func malIDFor(known, anilistID int) func() (int, error) {
	if known != 0 {
		return func() (int, error) { return known, nil }
	}
	return func() (int, error) { return resolveMyAnimeListID(anilistID) }
}

// writeForCurrentShow routes a change for mediaID by the show now playing.
// toMAL receives the config and the resolved MyAnimeList ID.
func writeForCurrentShow(mediaID int, toAniList func() error, toMAL func(*Config, int) error) error {
	cfg := GetGlobalConfig()
	return writeTrackers(cfg, GetGlobalAnime(), trackerWrite{
		aniList:     toAniList,
		malID:       malIDFor(0, mediaID),
		myAnimeList: func(malID int) error { return toMAL(cfg, malID) },
	})
}

func UpdateAnimeProgress(token string, mediaID, progress int) error {
	noteProgressWritten()
	err := writeForCurrentShow(mediaID,
		func() error { return UpdateAniListAnimeProgress(token, mediaID, progress) },
		func(cfg *Config, malID int) error { return updateMyAnimeListProgress(cfg, malID, progress) })
	if err == nil {
		// The episode is watched: the synced position moves on with it.
		noteEpisodeFinished(GetGlobalConfig(), mediaID, progress)
	}
	return err
}

func UpdateAnimeStatus(token string, mediaID int, status string) error {
	return writeForCurrentShow(mediaID,
		func() error { return UpdateAniListAnimeStatus(token, mediaID, status) },
		func(cfg *Config, malID int) error { return updateMyAnimeListStatus(cfg, malID, status) })
}

// ErrRatingDeclined means the viewer chose not to rate, as opposed to the
// rating failing.
//
// The two used to look the same: RateAnime returned nil when the score prompt
// was cancelled, every caller took that as success, and announced a rating
// that was never saved. Escaping the prompt in a terminal did it, and a cast
// window did it every time.
var ErrRatingDeclined = errors.New("rating declined")

// RateAnime asks for a score and writes it.
func RateAnime(token string, mediaID int) error {
	_, rateErr := rateAnimeScore(token, mediaID)
	return rateErr
}

// rateAnimeScore is RateAnime that also returns the score written, or -1
// when nothing was.
func rateAnimeScore(token string, mediaID int) (float64, error) {
	const notRated = -1
	if !ShouldWriteRemoteTracking(GetGlobalConfig(), GetGlobalAnime()) {
		return notRated, nil
	}

	// Asked here for every configuration, and a decline reported as one. The
	// single-tracker routes once left the asking to RateAnimeWithScore, which
	// swallowed the cancel and returned nil, so escaping the prompt printed
	// "Anime rated successfully!" with nothing saved -- on the most common
	// setup there is. ErrRatingDeclined only reached dual tracking until then.
	score, declined, promptErr := promptAnimeScoreValue()
	switch {
	case promptErr != nil:
		return notRated, promptErr
	case declined:
		return notRated, ErrRatingDeclined
	}
	if writeErr := RateAnimeWithScore(token, mediaID, score); writeErr != nil {
		return notRated, writeErr
	}
	return score, nil
}

// ratingRoute is where a score -- or any other tracker write -- goes.
//
// Kept apart from the writing so it can be tested without a network, like
// advanceDecision and anilistRewatchCompletion. The routing is what broke: a
// single-tracker config took a branch that ignored the score it was given and
// opened the prompt again, and no test saw it because every test of it was
// configured onto another branch.
type ratingRoute int

const (
	ratingRouteNone ratingRoute = iota
	ratingRouteDual
	ratingRouteAniList
	ratingRouteMyAnimeList
)

func ratingRouteFor(config *Config, anime *Anime) ratingRoute {
	if !ShouldWriteRemoteTracking(config, anime) {
		return ratingRouteNone
	}
	toAniList, toMAL := UsesAniListTracking(config), UsesMyAnimeListTracking(config)
	switch {
	case toAniList && toMAL:
		return ratingRouteDual
	case toAniList:
		return ratingRouteAniList
	case toMAL:
		return ratingRouteMyAnimeList
	}
	return ratingRouteNone
}

// RateAnimeWithScore writes a rating that has already been decided.
//
// Choosing and writing are separate because the cast panel picks the score
// with arrow keys and cannot open a prompt, and because a caller that already
// has a score should not have to answer a question to use it.
//
// Both single-tracker routes write the score passed in. They used to call
// RateAniListAnime or rateMyAnimeListAnime, which prompted again and threw this
// argument away; on a cast the prompt was auto-cancelled by CastNonInteractive,
// the cancel came back as nil, and the panel said "Rated 9." with nothing
// saved. Asking is RateAnime's job.
func RateAnimeWithScore(token string, mediaID int, score float64) error {
	return writeForCurrentShow(mediaID,
		func() error { return saveAniListAnimeScore(token, mediaID, score) },
		func(cfg *Config, malID int) error {
			return rateMyAnimeListAnimeWithScore(cfg, malID, myAnimeListScore(score))
		})
}

func AddAnimeToWatchingList(animeID int, token string) error {
	return writeForCurrentShow(animeID,
		func() error { return AddAniListAnimeToWatchingList(animeID, token) },
		func(cfg *Config, malID int) error { return addMyAnimeListAnimeToList(cfg, malID, "CURRENT") })
}

func AddAnimeToList(animeID int, status string, token string) error {
	return writeForCurrentShow(animeID,
		func() error { return AddAniListAnimeToList(animeID, status, token) },
		func(cfg *Config, malID int) error { return addMyAnimeListAnimeToList(cfg, malID, status) })
}

// writeForShow routes a change by show itself rather than the global anime:
// a rewatch belongs to the show it is handed.
func writeForShow(show Anime, toAniList func() error, toMAL func(*Config, int) error) error {
	cfg := GetGlobalConfig()
	return writeTrackers(cfg, &show, trackerWrite{
		aniList:     toAniList,
		malID:       malIDFor(show.MalId, show.AnilistId),
		myAnimeList: func(malID int) error { return toMAL(cfg, malID) },
	})
}

func CompleteAnimeRewatch(token string, anime Anime) error {
	return writeForShow(anime,
		func() error { return CompleteAniListAnimeRewatch(token, anime) },
		func(cfg *Config, malID int) error { return completeMyAnimeListRewatch(cfg, malID, anime) })
}

// myAnimeListRewatchStart is the MyAnimeList side of starting a rewatch: back
// to watching from episode zero, started on startedAt, finish date cleared.
func myAnimeListRewatchStart(startedAt FuzzyDate) map[string]string {
	fields := myAnimeListStatusFields("REPEATING")
	fields["num_watched_episodes"] = "0"
	fields["start_date"] = formatMyAnimeListDate(startedAt)
	fields["finish_date"] = ""
	return fields
}

func StartAnimeRewatch(token string, anime Anime) error {
	today := currentFuzzyDate()
	return writeForShow(anime,
		func() error {
			rewatching, fromStart := "REPEATING", 0
			return SaveAniListAnimeListEntry(token, anime.AnilistId, &rewatching, &fromStart, nil, nil, &today, &FuzzyDate{})
		},
		func(cfg *Config, malID int) error {
			return updateMyAnimeListListStatus(cfg, malID, myAnimeListRewatchStart(today))
		})
}

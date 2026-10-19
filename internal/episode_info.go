package internal

import (
	"slices"
	"sync"

	"github.com/thexykril/otakase/internal/jikan"
)

var (
	jikanOnce   sync.Once
	jikanShared *jikan.Client
)

func jikanClient() *jikan.Client {
	jikanOnce.Do(func() { jikanShared = jikan.New(sharedHTTPClient) })
	return jikanShared
}

// LoadEpisodeDetails fills in the title, air date, synopsis and filler/recap
// flags of an episode from MyAnimeList. When the lookup fails the fields are
// cleared, so an episode is never shown with the previous one's details; the
// failure is logged and not returned, since playback does not depend on it.
func LoadEpisodeDetails(anime *Anime, malID, episode int) {
	anime.Ep.Title = AnimeTitle{}
	anime.Ep.Aired = ""
	anime.Ep.Synopsis = ""
	anime.Ep.IsFiller = false
	anime.Ep.IsRecap = false

	details, err := jikanClient().Episode(malID, episode)
	if err != nil {
		Log("episode details: " + err.Error())
		return
	}
	anime.Ep.Title = AnimeTitle{English: details.Title, Romaji: details.TitleRomaji, Japanese: details.TitleJapanese}
	anime.Ep.Aired = details.Aired
	anime.Ep.Synopsis = details.Synopsis
	anime.Ep.IsFiller = details.Filler
	anime.Ep.IsRecap = details.Recap
	// The length is left alone: the player measures the file actually playing.
}

// FillerEpisodes lists the filler episodes of a show by number.
func FillerEpisodes(malID int) ([]int, error) {
	return jikanClient().FillerEpisodes(malID)
}

// ShowPictures lists every picture MyAnimeList has for a show.
func ShowPictures(malID int) ([]string, error) {
	return jikanClient().Pictures(malID)
}

// IsFiller reports whether an episode is in the filler list.
func IsFiller(filler []int, episode int) bool {
	return slices.Contains(filler, episode)
}

// NextCanonEpisode is the first episode after current that is not filler.
func NextCanonEpisode(filler []int, current int) int {
	next := current + 1
	for IsFiller(filler, next) {
		next++
	}
	return next
}

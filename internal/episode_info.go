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

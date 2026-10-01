package internal

import "sync"

// AniList lets an entry be hidden from other users; MyAnimeList cannot hide a
// single entry, only a whole list. Dual tracking used to copy every AniList
// entry across, so a show kept private on AniList turned up for anyone viewing
// the MyAnimeList list. A private entry is now never written to MyAnimeList.
//
// The set is keyed by MyAnimeList id because that is all the MyAnimeList
// writers are handed. It is rebuilt whenever both lists are reconciled.
var aniListPrivate struct {
	mu     sync.RWMutex
	malIDs map[int]struct{}
}

// noteAniListPrivateEntries records which entries of an AniList list are
// private, replacing what was known before.
func noteAniListPrivateEntries(list AnimeList) {
	ids := map[int]struct{}{}
	for _, entry := range getEntriesByCategory(list, "ALL") {
		if entry.Private && entry.Media.MalID > 0 {
			ids[entry.Media.MalID] = struct{}{}
		}
	}
	aniListPrivate.mu.Lock()
	aniListPrivate.malIDs = ids
	aniListPrivate.mu.Unlock()
}

// isAniListPrivateMyAnimeListID reports whether the show with this
// MyAnimeList id is private on AniList.
func isAniListPrivateMyAnimeListID(malID int) bool {
	aniListPrivate.mu.RLock()
	defer aniListPrivate.mu.RUnlock()
	_, ok := aniListPrivate.malIDs[malID]
	return ok
}

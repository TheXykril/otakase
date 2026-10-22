package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Passing 100, 500 or 1000 episodes rolls the credits (otakase_movie.go) once,
// as the viewer quits after the episode that got them there.
//
// The count is the tracker's own all-time total, read once on the way out and
// only when this run wrote progress -- every other exit costs nothing. The
// highest milestone celebrated is kept in a small file, so each plays once.

var episodeMilestones = []int{100, 500, 1000}

const milestoneFileName = "milestone"

// progressWritten is set once this run has sent an episode's progress.
var progressWritten atomic.Bool

func noteProgressWritten() { progressWritten.Store(true) }

func milestoneFile(storagePath string) string {
	return filepath.Join(storageDir(storagePath), milestoneFileName)
}

// milestoneReached is the highest milestone at or below total, 0 for none.
func milestoneReached(total int) int {
	reached := 0
	for _, milestone := range episodeMilestones {
		if total >= milestone {
			reached = milestone
		}
	}
	return reached
}

// milestoneToCelebrate decides what an exit plays, given the episode total
// and the milestone celebrated last. seen is false when nothing has been
// recorded yet: a viewer who was already past a milestone before this
// existed has it recorded quietly, rather than being shown credits for an
// episode they watched months ago.
func milestoneToCelebrate(total, celebrated int, seen bool) (play, record int) {
	reached := milestoneReached(total)
	if !seen {
		return 0, reached
	}
	if reached > celebrated {
		return reached, reached
	}
	return 0, celebrated
}

func readMilestone(path string) (int, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0, false
	}
	return value, true
}

// celebrateMilestoneOnExit plays the credits when this run's episodes took
// the viewer past a milestone.
func celebrateMilestoneOnExit(config *Config) {
	if config == nil || !progressWritten.Load() || statsTrackerKey(config) == TrackingRemoteNone {
		return
	}
	total, err := fetchEpisodesWatchedWithin(config, milestoneCheckWait)
	if err != nil {
		Log(fmt.Sprintf("Milestone: could not read the episode total: %v", err))
		return
	}
	path := milestoneFile(config.StoragePath)
	celebrated, seen := readMilestone(path)
	play, record := milestoneToCelebrate(total, celebrated, seen)
	if !seen || record != celebrated {
		if err := os.WriteFile(path, []byte(strconv.Itoa(record)+"\n"), 0o644); err != nil {
			Log(fmt.Sprintf("Milestone: could not save: %v", err))
			// Without the record it would play again on every exit.
			return
		}
	}
	if play > 0 {
		Log(fmt.Sprintf("Milestone: %d episodes", play))
		PlayOtakaseMovie(config, nil, play)
	}
}

// milestoneCheckWait caps how long quitting can wait on the tracker. A slow
// or absent network costs this at most, and the milestone is caught on a
// later exit instead.
const milestoneCheckWait = 4 * time.Second

func fetchEpisodesWatchedWithin(config *Config, wait time.Duration) (int, error) {
	type answer struct {
		total int
		err   error
	}
	done := make(chan answer, 1)
	go func() {
		total, err := fetchEpisodesWatched(config)
		done <- answer{total, err}
	}()
	select {
	case got := <-done:
		return got.total, got.err
	case <-time.After(wait):
		return 0, fmt.Errorf("no answer within %s", wait)
	}
}

const aniListEpisodesWatchedQuery = `query { Viewer { statistics { anime { episodesWatched } } } }`

// fetchEpisodesWatched reads the all-time episode count alone, so the check on
// the way out is one small request rather than the whole stats page.
func fetchEpisodesWatched(config *Config) (int, error) {
	key := statsTrackerKey(config)
	if key == TrackingRemoteAniList || key == TrackingRemoteBoth {
		token, err := GetTokenFromFile(anilistTokenPath(config))
		if err == nil {
			var answer struct {
				Data struct {
					Viewer struct {
						Statistics struct {
							Anime struct {
								EpisodesWatched int `json:"episodesWatched"`
							} `json:"anime"`
						} `json:"statistics"`
					} `json:"Viewer"`
				} `json:"data"`
			}
			if err = aniListStatsRequest(token, aniListEpisodesWatchedQuery, nil, &answer); err == nil {
				return answer.Data.Viewer.Statistics.Anime.EpisodesWatched, nil
			}
		}
		if key == TrackingRemoteAniList {
			return 0, err
		}
	}
	stats, err := fetchMyAnimeListWatchStats(config)
	if err != nil {
		return 0, err
	}
	return stats.MyAnimeList.Episodes, nil
}

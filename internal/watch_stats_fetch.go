package internal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// statsCacheTTL is how long a fetched page is reused. Opening the page twice
// in a row should not cost a dozen AniList requests against a rate limit the
// rest of the program shares; five minutes is short enough that an episode
// just watched shows up on the next visit after that.
//
// The cache is memory only. The page promises to be the tracker's numbers and
// nothing else, and a file would outlive a tracker switch or a list edit on
// the website.
const statsCacheTTL = 5 * time.Minute

// statsActivityPageCap bounds the paging. Fifty activities a page and ten
// pages is five hundred updates in ninety days -- more than daily watching
// produces -- and stops a runaway feed from spending the rate limit.
const (
	statsActivityPerPage = 50
	statsActivityPageCap = 10
)

type statsCacheEntry struct {
	key       string
	stats     watchStats
	fetchedAt time.Time
}

var (
	statsCacheMu sync.Mutex
	statsCache   statsCacheEntry
)

// statsTrackerKey names where the stats come from for a config, so a cached
// page is never shown after the tracker has been switched.
func statsTrackerKey(config *Config) string {
	if config == nil {
		return TrackingRemoteNone
	}
	return normalizeRemoteTracker(config.TrackingRemote)
}

// fetchWatchStats reads the stats from the configured tracker, reusing a
// recent answer. With both trackers configured AniList is asked first, since
// only it has the dated history the week and streak rows are built from;
// MyAnimeList answers if AniList cannot.
func fetchWatchStats(config *Config) (watchStats, error) {
	key := statsTrackerKey(config)

	statsCacheMu.Lock()
	if statsCache.key == key && time.Since(statsCache.fetchedAt) < statsCacheTTL {
		cached := statsCache.stats
		statsCacheMu.Unlock()
		Log(fmt.Sprintf("Stats: reusing %s stats from %s ago", cached.Source, time.Since(statsCache.fetchedAt).Round(time.Second)))
		return cached, nil
	}
	statsCacheMu.Unlock()

	var (
		stats watchStats
		err   error
	)
	switch key {
	case TrackingRemoteAniList:
		stats, err = fetchAniListWatchStats(config, time.Now())
	case TrackingRemoteMyAnimeList:
		stats, err = fetchMyAnimeListWatchStats(config)
	case TrackingRemoteBoth:
		stats, err = fetchAniListWatchStats(config, time.Now())
		if err != nil {
			Log(fmt.Sprintf("Stats: AniList failed, trying MyAnimeList: %v", err))
			stats, err = fetchMyAnimeListWatchStats(config)
		}
	default:
		return watchStats{}, errStatsNeedTracker
	}
	if err != nil {
		return watchStats{}, err
	}

	statsCacheMu.Lock()
	statsCache = statsCacheEntry{key: key, stats: stats, fetchedAt: time.Now()}
	statsCacheMu.Unlock()
	return stats, nil
}

var errStatsNeedTracker = fmt.Errorf("stats need a remote tracker")

// isRateLimitError recognises AniList's 429 once makePostRequest has given up
// retrying it. It is worth a sentence of its own: it is the one failure that
// fixes itself if the viewer simply waits.
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, fmt.Sprintf("status %d", http.StatusTooManyRequests)) ||
		strings.Contains(strings.ToLower(message), "too many requests")
}

// statsErrorMessage turns a failed fetch into the line the page shows.
func statsErrorMessage(source string, err error) string {
	if isRateLimitError(err) {
		return source + " is rate limiting requests right now. Try again in a minute."
	}
	return fmt.Sprintf("Could not read stats from %s: %v", source, err)
}

const aniListStatsQuery = `
query {
	Viewer {
		id
		statistics {
			anime {
				count
				episodesWatched
				minutesWatched
				meanScore
				genres(limit: 3, sort: COUNT_DESC) { genre count }
				formats(limit: 3, sort: COUNT_DESC) { format count }
				statuses(sort: COUNT_DESC) { status count }
			}
		}
	}
}`

const aniListStatsActivityQuery = `
query ($userId: Int, $since: Int, $page: Int, $perPage: Int) {
	Page(page: $page, perPage: $perPage) {
		pageInfo { hasNextPage }
		activities(userId: $userId, type: ANIME_LIST, createdAt_greater: $since, sort: ID_DESC) {
			... on ListActivity {
				createdAt
				status
				progress
				media {
					id
					duration
					title { romaji english native }
				}
			}
		}
	}
}`

type aniListStatsResponse struct {
	Data struct {
		Viewer struct {
			ID         int `json:"id"`
			Statistics struct {
				Anime struct {
					Count           int     `json:"count"`
					EpisodesWatched int     `json:"episodesWatched"`
					MinutesWatched  int     `json:"minutesWatched"`
					MeanScore       float64 `json:"meanScore"`
					Genres          []struct {
						Genre string `json:"genre"`
						Count int    `json:"count"`
					} `json:"genres"`
					Formats []struct {
						Format string `json:"format"`
						Count  int    `json:"count"`
					} `json:"formats"`
					Statuses []struct {
						Status string `json:"status"`
						Count  int    `json:"count"`
					} `json:"statuses"`
				} `json:"anime"`
			} `json:"statistics"`
		} `json:"Viewer"`
	} `json:"data"`
}

type aniListStatsActivityResponse struct {
	Data struct {
		Page struct {
			PageInfo struct {
				HasNextPage bool `json:"hasNextPage"`
			} `json:"pageInfo"`
			Activities []struct {
				CreatedAt int64  `json:"createdAt"`
				Status    string `json:"status"`
				// progress is a string on AniList: "5", or "3 - 5" for a run of
				// updates folded into one activity.
				Progress *string `json:"progress"`
				Media    *struct {
					ID       int `json:"id"`
					Duration int `json:"duration"`
					Title    struct {
						Romaji  string `json:"romaji"`
						English string `json:"english"`
						Native  string `json:"native"`
					} `json:"title"`
				} `json:"media"`
			} `json:"activities"`
		} `json:"Page"`
	} `json:"data"`
}

// aniListStatsRequest runs one query and decodes it. makePostRequest already
// renews an expired token and backs off on a 429, so this only reshapes the
// answer.
func aniListStatsRequest(token, query string, variables map[string]interface{}, out interface{}) error {
	headers := map[string]string{"Content-Type": "application/json"}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	response, err := makePostRequest("https://graphql.anilist.co", query, variables, headers)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func fetchAniListWatchStats(config *Config, now time.Time) (watchStats, error) {
	// The AniList token lives in its own file. user.Token is not it in every
	// mode -- with MyAnimeList alongside, it can be the MyAnimeList token.
	token, err := GetTokenFromFile(anilistTokenPath(config))
	if err != nil {
		return watchStats{}, fmt.Errorf("no AniList sign-in: %w", err)
	}

	var totals aniListStatsResponse
	if err := aniListStatsRequest(token, aniListStatsQuery, nil, &totals); err != nil {
		return watchStats{}, err
	}
	viewer := totals.Data.Viewer
	anime := viewer.Statistics.Anime

	summary := &aniListStatsTotals{
		Count:           anime.Count,
		EpisodesWatched: anime.EpisodesWatched,
		MinutesWatched:  anime.MinutesWatched,
		MeanScore:       anime.MeanScore,
	}
	for _, genre := range anime.Genres {
		summary.Genres = append(summary.Genres, statCount{Name: genre.Genre, Count: genre.Count})
	}
	for _, format := range anime.Formats {
		summary.Formats = append(summary.Formats, statCount{Name: format.Format, Count: format.Count})
	}
	for _, status := range anime.Statuses {
		summary.Statuses = append(summary.Statuses, statCount{Name: status.Status, Count: status.Count})
	}

	stats := watchStats{Source: "AniList", AniList: summary}
	if viewer.ID == 0 {
		stats.HistoryNote = "AniList did not say which user this is, so there is no recent history."
		return stats, nil
	}

	since := now.AddDate(0, 0, -statsHistoryDays).Unix()
	for page := 1; ; page++ {
		if page > statsActivityPageCap {
			stats.HistoryNote = "Recent history was cut short: more activity than the page reads."
			break
		}
		var activities aniListStatsActivityResponse
		err := aniListStatsRequest(token, aniListStatsActivityQuery, map[string]interface{}{
			"userId":  viewer.ID,
			"since":   since,
			"page":    page,
			"perPage": statsActivityPerPage,
		}, &activities)
		if err != nil {
			// The totals already arrived and are right; a history that stopped
			// part way is still worth showing, as long as the page says so.
			Log(fmt.Sprintf("Stats: activity page %d failed: %v", page, err))
			if isRateLimitError(err) {
				stats.HistoryNote = "AniList rate limited the history, so recent numbers may be low."
			} else {
				stats.HistoryNote = "AniList stopped answering part way, so recent numbers may be low."
			}
			break
		}

		for _, item := range activities.Data.Page.Activities {
			// Fragments that are not a ListActivity decode as empty objects.
			if item.CreatedAt == 0 || item.Media == nil {
				continue
			}
			progress := ""
			if item.Progress != nil {
				progress = *item.Progress
			}
			media := Media{ID: item.Media.ID, Title: AnimeTitle{
				Romaji:   item.Media.Title.Romaji,
				English:  item.Media.Title.English,
				Japanese: item.Media.Title.Native,
			}}
			stats.Activities = append(stats.Activities, statsActivity{
				At:       time.Unix(item.CreatedAt, 0),
				MediaID:  item.Media.ID,
				Title:    mediaDisplayTitle(media, config),
				Episodes: statsActivityEpisodes(item.Status, progress),
				Duration: item.Media.Duration,
			})
		}
		if !activities.Data.Page.PageInfo.HasNextPage {
			break
		}
	}
	Log(fmt.Sprintf("Stats: AniList totals and %d activities in the last %d days", len(stats.Activities), statsHistoryDays))
	return stats, nil
}

type myAnimeListStatsResponse struct {
	AnimeStatistics struct {
		NumItemsWatching    int     `json:"num_items_watching"`
		NumItemsCompleted   int     `json:"num_items_completed"`
		NumItemsOnHold      int     `json:"num_items_on_hold"`
		NumItemsDropped     int     `json:"num_items_dropped"`
		NumItemsPlanToWatch int     `json:"num_items_plan_to_watch"`
		NumItems            int     `json:"num_items"`
		NumDaysWatched      float64 `json:"num_days_watched"`
		NumEpisodes         int     `json:"num_episodes"`
		NumTimesRewatched   int     `json:"num_times_rewatched"`
		MeanScore           float64 `json:"mean_score"`
	} `json:"anime_statistics"`
}

func fetchMyAnimeListWatchStats(config *Config) (watchStats, error) {
	var response myAnimeListStatsResponse
	if err := myAnimeListRequest(config, http.MethodGet, "/users/@me?fields=anime_statistics", nil, &response); err != nil {
		return watchStats{}, err
	}
	raw := response.AnimeStatistics
	return watchStats{
		Source: "MyAnimeList",
		MyAnimeList: &myAnimeListStatsTotals{
			Items:       raw.NumItems,
			Watching:    raw.NumItemsWatching,
			Completed:   raw.NumItemsCompleted,
			OnHold:      raw.NumItemsOnHold,
			Dropped:     raw.NumItemsDropped,
			PlanToWatch: raw.NumItemsPlanToWatch,
			Episodes:    raw.NumEpisodes,
			DaysWatched: raw.NumDaysWatched,
			Rewatched:   raw.NumTimesRewatched,
			MeanScore:   raw.MeanScore,
		},
	}, nil
}

package internal

import "time"

// The tracker account and its list, in the shape AniList gives it.
// MyAnimeList answers are converted to the same shape.

// User is the signed-in tracker account and its list.
type User struct {
	Token     string
	Username  string
	Id        int
	AnimeList AnimeList
	// ListSync, when set, refreshes AnimeList in the background; read its
	// Current list rather than AnimeList while it runs.
	ListSync *AnimeListSync
}

// AnimeList is the viewer's list, one slice per status.
type AnimeList struct {
	Watching   []Entry `json:"watching"`
	Completed  []Entry `json:"completed"`
	Paused     []Entry `json:"paused"`
	Dropped    []Entry `json:"dropped"`
	Planning   []Entry `json:"planning"`
	Rewatching []Entry `json:"rewatching"`
}

// Entry is a show on the viewer's list.
type Entry struct {
	// ListID is the list entry's own id, not the show's.
	ListID   int     `json:"id"`
	Media    Media   `json:"media"`
	Progress int     `json:"progress"`
	Repeat   int     `json:"repeat"`
	Score    float64 `json:"score"`
	// Status is the list the entry is on, in AniList's names: CURRENT,
	// REPEATING, COMPLETED, PAUSED, DROPPED or PLANNING.
	Status      string    `json:"status"`
	StartedAt   FuzzyDate `json:"startedAt"`
	CompletedAt FuzzyDate `json:"completedAt"`
	CoverImage  string    `json:"coverImage"`
	UpdatedAt   time.Time `json:"updatedAt"`
	// Private is AniList's "hide from other users" flag. MyAnimeList has no
	// such flag, so a private entry is kept off MyAnimeList entirely.
	Private bool `json:"private,omitempty"`
}

// Media is a show as the trackers describe it.
type Media struct {
	// Duration is the average episode length in minutes.
	Duration int        `json:"duration"`
	Episodes int        `json:"episodes"`
	Format   string     `json:"format"`
	ID       int        `json:"id"`
	MalID    int        `json:"mal_id"`
	Title    AnimeTitle `json:"title"`
	// Status is AniList's: FINISHED, RELEASING, NOT_YET_RELEASED and so on.
	Status            string                 `json:"status"`
	NextAiringEpisode *NextAiringEpisodeInfo `json:"nextAiringEpisode,omitempty"`
	// IsAdult marks 18+ titles: AniList's isAdult, or MyAnimeList rating
	// them "black" (explicit). AdultContent=false hides them.
	IsAdult bool `json:"isAdult,omitempty"`
}

// FuzzyDate is a tracker date, any part of which may be 0 for unknown.
type FuzzyDate struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

// NextAiringEpisodeInfo is AniList's next broadcast: the episode and the
// seconds until it airs.
type NextAiringEpisodeInfo struct {
	Episode         int
	TimeUntilAiring int
}

// AniListAnime is one show in an AniList search result.
type AniListAnime struct {
	ID       int `json:"id"`
	Episodes int `json:"episodes"`
	Title    struct {
		Romaji  string `json:"romaji"`
		English string `json:"english"`
		Native  string `json:"native"`
	} `json:"title"`
	CoverImage struct {
		Large string `json:"large"`
	} `json:"coverImage"`
}

// Page is the page of results an AniList search returns.
type Page struct {
	Media []AniListAnime `json:"media"`
}

// ResponseData is the data of an AniList search response.
type ResponseData struct {
	Page Page `json:"Page"`
}

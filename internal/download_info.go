package internal

import (
	"time"
)

// A download is saved with a small file beside it naming what it is: which
// show and episode, in which language, from where, and where its opening and
// ending fall. Played back later with no connection, the video alone says none
// of that -- the filename is for people, and titles are ambiguous -- so this is
// what lets an offline episode still be tracked and still skip its opening.

// DownloadInfo describes one downloaded episode.
type DownloadInfo struct {
	Version       int       `json:"version"`
	Title         string    `json:"title"`
	AnilistID     int       `json:"anilist_id,omitempty"`
	MalID         int       `json:"mal_id,omitempty"`
	Episode       int       `json:"episode"`
	TotalEpisodes int       `json:"total_episodes,omitempty"`
	Mode          string    `json:"mode"`
	Provider      string    `json:"provider,omitempty"`
	ProviderID    string    `json:"provider_id,omitempty"`
	File          string    `json:"file"`
	SkipTimes     SkipTimes `json:"skip_times"`
	DownloadedAt  time.Time `json:"downloaded_at"`
}

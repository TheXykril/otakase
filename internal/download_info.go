package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A download is saved with a small file beside it naming what it is: which
// show and episode, in which language, from where, and where its opening and
// ending fall. Played back later with no connection, the video alone says none
// of that -- the filename is for people, and titles are ambiguous -- so this is
// what lets an offline episode still be tracked and still skip its opening.

// downloadInfoVersion is bumped when a field changes meaning.
const downloadInfoVersion = 1

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

// DownloadInfoPath names the info file for a downloaded video. It drops the
// container so the file does not depend on which one was used.
func DownloadInfoPath(video string) string {
	return strings.TrimSuffix(video, filepath.Ext(video)) + ".otakase.json"
}

// ReadDownloadInfo loads the info file saved beside a downloaded video.
func ReadDownloadInfo(video string) (DownloadInfo, error) {
	raw, err := os.ReadFile(DownloadInfoPath(video))
	if err != nil {
		return DownloadInfo{}, err
	}
	var info DownloadInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return DownloadInfo{}, fmt.Errorf("read %s: %w", DownloadInfoPath(video), err)
	}
	return info, nil
}

// saveDownloadInfo writes the info file atomically, so a crash never leaves
// half of one to be misread.
func saveDownloadInfo(video string, info DownloadInfo) error {
	raw, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	target := DownloadInfoPath(video)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// downloadSkipTimes looks up where an episode's opening and ending fall. It
// runs once the video is saved, while there is still a connection to ask.
var downloadSkipTimes = func(config Config, anime *Anime, episode int, mode string) SkipTimes {
	ref := SkipRef{MalID: anime.MalId, AniListID: anime.AnilistId, Episode: episode, Mode: mode}
	resolution := ResolveSkipTimes(ref, DefaultSkipSources(&config, nil)...)
	Log(fmt.Sprintf("Download of episode %d: %s", episode, resolution.Describe()))
	return resolution.Times
}

// writeDownloadInfo records a finished download. It never fails the download:
// the video is what was asked for, and it plays without this.
func writeDownloadInfo(config Config, anime *Anime, episode int, mode, provider, video string) {
	// The stored id belongs to the show's own provider; a fallback to another
	// one leaves nothing that id would find there.
	storedProvider, providerID := providerIDForAnime(anime)
	if normalizeProviderName(storedProvider) != normalizeProviderName(provider) {
		providerID = ""
	}
	info := DownloadInfo{
		Version:       downloadInfoVersion,
		Title:         GetAnimeName(*anime),
		AnilistID:     anime.AnilistId,
		MalID:         anime.MalId,
		Episode:       episode,
		TotalEpisodes: anime.TotalEpisodes,
		Mode:          mode,
		Provider:      provider,
		ProviderID:    providerID,
		File:          filepath.Base(video),
		SkipTimes:     downloadSkipTimes(config, anime, episode, mode),
		DownloadedAt:  time.Now().UTC(),
	}
	if err := saveDownloadInfo(video, info); err != nil {
		Log(fmt.Sprintf("Download of episode %d: could not save its info file: %v", episode, err))
	}
}

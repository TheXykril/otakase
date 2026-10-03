package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/icons"
)

// Continue watching: the shows played most recently, as the first rows of the
// Watching list when MenuActions names CONTINUE_LAST, so picking one up again is one choice rather than a category and
// a search. "Continue Last Session" only ever reached the single last show.
//
// The watch history already holds each show's episode and position, but not
// when it was played, so the order lives in its own small file beside it.

// recentShowsFile lists recently played shows, newest first.
const recentShowsFile = "recent.json"

// recentShowsKept is how many shows the file remembers. More than any menu
// shows, so lowering ContinueWatchingRows and raising it again loses nothing.
const recentShowsKept = 20

// resumeRowPrefix marks a home menu key as a continue-watching row. It cannot
// collide with a category or action key, which are all plain words.
const resumeRowPrefix = "RESUME:"

type recentShow struct {
	AnilistID int       `json:"anilist_id"`
	PlayedAt  time.Time `json:"played_at"`
	// Adult is the tracker's 18+ flag when the show was played.
	Adult bool `json:"adult,omitempty"`
}

func recentShowsPath(storagePath string) string {
	storagePath = strings.TrimSpace(os.ExpandEnv(storagePath))
	if storagePath == "" {
		return ""
	}
	return filepath.Join(storagePath, recentShowsFile)
}

func loadRecentShows(storagePath string) []recentShow {
	path := recentShowsPath(storagePath)
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var shows []recentShow
	if err := json.Unmarshal(raw, &shows); err != nil {
		Log(fmt.Sprintf("Continue watching: unreadable %s: %v", path, err))
		return nil
	}
	return shows
}

// noteRecentShow moves a show to the front of the recent list, marked 18+ or
// not so the rows can leave it out.
func noteRecentShow(storagePath string, anilistID int, adult bool, now time.Time) {
	path := recentShowsPath(storagePath)
	if path == "" || anilistID <= 0 {
		return
	}
	shows := []recentShow{{AnilistID: anilistID, PlayedAt: now.UTC(), Adult: adult}}
	for _, show := range loadRecentShows(storagePath) {
		if show.AnilistID != anilistID && show.AnilistID > 0 && len(shows) < recentShowsKept {
			shows = append(shows, show)
		}
	}
	raw, err := json.MarshalIndent(shows, "", "  ")
	if err != nil {
		return
	}
	// Written whole and renamed into place: the file is read on every menu,
	// and a half-written one would empty the rows.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		Log(fmt.Sprintf("Continue watching: %v", err))
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		Log(fmt.Sprintf("Continue watching: %v", err))
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		Log(fmt.Sprintf("Continue watching: %v", err))
	}
}

// continueWatchingRows builds the home menu rows for the recent shows that
// still have an entry in the watch history. A show missing from it has
// nothing to resume, and its title would have to be fetched to name it.
//
// With a remote tracker a show must also still be on the list: playback looks
// the pick up there, and one removed since would end the run with an error.
func continueWatchingRows(config *Config, list *AnimeList) []SelectionOption {
	if config == nil || config.ContinueWatchingRows <= 0 {
		return nil
	}
	recent := loadRecentShows(config.StoragePath)
	if len(recent) == 0 {
		return nil
	}
	history := LocalGetAllAnime(localHistoryPath(config.StoragePath))
	// The home menu opens with these rows, whoever is looking at the screen,
	// so 18+ shows need their own opt-in on top of AdultContent.
	showAdult := config.AdultContent && config.ContinueWatchingAdult

	rows := []SelectionOption{}
	for _, show := range recent {
		if len(rows) >= config.ContinueWatchingRows {
			break
		}
		if show.Adult && !showAdult {
			continue
		}
		entry := LocalFindAnime(history, show.AnilistID, "")
		if entry == nil {
			continue
		}
		cover := ""
		if list != nil && UsesRemoteTracking(config) {
			listed, err := FindAnimeByAnilistID(*list, strconv.Itoa(show.AnilistID))
			// The list's flag also covers shows recorded before Adult was.
			if err != nil || (listed.Media.IsAdult && !showAdult) {
				continue
			}
			cover = listed.CoverImage
		}
		rows = append(rows, SelectionOption{
			Key:       resumeRowPrefix + strconv.Itoa(show.AnilistID),
			Label:     continueWatchingLabel(GetAnimeName(*entry), entry.Ep.Number, entry.Ep.Player.PlaybackTime),
			Icon:      icons.Play,
			Thumbnail: cover,
		})
	}
	return rows
}

// continueWatchingLabel names a row: "Frieren · ep 13 at 12:34". A position
// too early to be worth resuming is left out, as the list rows do.
func continueWatchingLabel(title string, episode, playbackSeconds int) string {
	label := title
	if episode <= 0 {
		return label
	}
	label += fmt.Sprintf(" · ep %d", episode)
	if playbackSeconds >= resumeMinSeconds {
		label += " at " + formatClock(playbackSeconds)
	}
	return label
}

func formatClock(seconds int) string {
	if seconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
	}
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}

// resumeRowAnilistID reads the show a continue-watching row stands for.
func resumeRowAnilistID(key string) (int, bool) {
	rest, found := strings.CutPrefix(key, resumeRowPrefix)
	if !found {
		return 0, false
	}
	id, err := strconv.Atoi(rest)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

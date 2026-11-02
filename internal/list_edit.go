package internal

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/thexykril/otakase/internal/icons"
)

// listStatusOptions are the tracker lists a show can be put on, keyed by
// AniList's status names.
func listStatusOptions() []SelectionOption {
	return []SelectionOption{
		{Key: "CURRENT", Label: "Currently Watching", Icon: icons.Watching},
		{Key: "COMPLETED", Label: "Completed", Icon: icons.Completed},
		{Key: "PAUSED", Label: "On Hold", Icon: icons.Paused},
		{Key: "DROPPED", Label: "Dropped", Icon: icons.Dropped},
		{Key: "PLANNING", Label: "Plan to Watch", Icon: icons.Planning},
		// AniList's name for Rewatching.
		{Key: "REPEATING", Label: "Rewatching", Icon: icons.Rewatching},
	}
}

// statusLabel is the label of status among options, "None" when it has none.
func statusLabel(options []SelectionOption, status string) string {
	for _, option := range options {
		if status != "" && option.Key == status {
			return option.Label
		}
	}
	return "None"
}

// entryUpdate is what happens after one change to a list entry.
type entryUpdate int

const (
	// entryPickAgain goes back to the list of shows, nothing changed.
	entryPickAgain entryUpdate = iota
	// entryChanged refreshes the list and is done.
	entryChanged
	// entryLeave is done without touching the list.
	entryLeave
)

// UpdateAnimeEntry edits a show on the tracker list: its status, progress,
// score, or the quality kept for it locally.
func UpdateAnimeEntry(userConfig *Config, user *User) {
	if !UsesRemoteTracking(userConfig) {
		Out("Remote tracking is disabled in your config.")
		return
	}

	actions := []SelectionOption{
		{Key: "CATEGORY", Label: "Change Anime Category", Icon: icons.Folder},
		{Key: "PROGRESS", Label: "Change Progress", Icon: icons.Counter},
		{Key: "SCORE", Label: "Add/Change Score", Icon: icons.Star},
		{Key: "QUALITY", Label: "Quality for this show", Icon: icons.Quality},
	}
	for {
		action, err := DynamicSelect(actions)
		if err != nil {
			Log(fmt.Sprintf("Failed to select update option: %v", err))
			Exit(fmt.Errorf("Failed to select update option"))
		}
		switch action.Key {
		case "-1":
			Exit(nil)
		case "-2":
			// Back to the home menu.
			return
		}
		if editListEntries(userConfig, user, action.Key) {
			return
		}
		EnterAppScreen()
	}
}

// editListEntries asks which show to change and applies action to it. It
// reports false when the viewer went back to the choice of action.
func editListEntries(userConfig *Config, user *User, action string) bool {
	if user.ListSync != nil {
		user.AnimeList = user.ListSync.Current()
	}
	preview := userConfig.RofiSelection && userConfig.ImagePreview
	var options []SelectionOption
	var previews map[string]RofiSelectPreview
	if preview {
		previews = buildCategoryPreviewOptions(user.AnimeList, "ALL")
	} else {
		options = buildCategorySelectionOptions(user.AnimeList, "ALL")
	}

	for {
		var picked SelectionOption
		var err error
		if preview {
			picked, err = DynamicSelectPreviewWithRefresh(previews, false, &PreviewSelectionRefreshConfig{
				Updates:   user.ListSync.Updates(),
				Refreshed: user.ListSync.RefreshDone(),
				BuildOptions: func(list AnimeList) map[string]RofiSelectPreview {
					return buildCategoryPreviewOptions(list, "ALL")
				},
			})
		} else {
			picked, err = DynamicSelectWithRefresh(options, &SelectionRefreshConfig{
				Updates:   user.ListSync.Updates(),
				Refreshed: user.ListSync.RefreshDone(),
				BuildOptions: func(list AnimeList) []SelectionOption {
					return buildCategorySelectionOptions(list, "ALL")
				},
			})
		}
		if err != nil {
			Log(fmt.Sprintf("Failed to select anime: %v", err))
			Exit(fmt.Errorf("Failed to select anime"))
		}
		switch picked.Key {
		case "-1":
			Exit(nil)
		case "-2":
			return false
		}

		animeID, err := strconv.Atoi(picked.Key)
		if err != nil {
			Log(fmt.Sprintf("Failed to convert anime ID: %v", err))
			Exit(fmt.Errorf("Failed to convert anime ID"))
		}
		if user.ListSync != nil {
			user.AnimeList = user.ListSync.Current()
		}
		entry, err := FindAnimeByAnilistID(user.AnimeList, picked.Key)
		if err != nil {
			Log(fmt.Sprintf("Can not find the anime in tracked anime list: %v", err))
			Exit(fmt.Errorf("Can not find the anime in tracked anime list"))
		}
		EnterAppScreen()

		switch applyEntryUpdate(userConfig, user, action, animeID, entry) {
		case entryPickAgain:
			EnterAppScreen()
			continue
		case entryLeave:
			return true
		}
		if err := RefreshUserAnimeList(userConfig, user); err != nil {
			Log(fmt.Sprintf("Failed to refresh anime list: %v", err))
			Exit(fmt.Errorf("Failed to refresh anime list"))
		}
		Out("Anime updated successfully!")
		return true
	}
}

// applyEntryUpdate makes one change to the entry for animeID.
func applyEntryUpdate(userConfig *Config, user *User, action string, animeID int, entry *Entry) entryUpdate {
	switch action {
	case "CATEGORY":
		statuses := listStatusOptions()
		Out(fmt.Sprintf("Current category: %s", statusLabel(statuses, entry.Status)))
		status, err := DynamicSelect(statuses)
		if err != nil {
			Log(fmt.Sprintf("Failed to select category: %v", err))
			Exit(fmt.Errorf("Failed to select category"))
		}
		switch status.Key {
		case "-1":
			Exit(nil)
		case "-2":
			return entryPickAgain
		}
		if err := UpdateAnimeStatus(user.Token, animeID, status.Key); err != nil {
			Log(fmt.Sprintf("Failed to update anime status: %v", err))
			Exit(fmt.Errorf("Failed to update anime status"))
		}

	case "PROGRESS":
		current := "None"
		if entry.Progress > 0 {
			current = strconv.Itoa(entry.Progress)
		}
		// Backing out leaves the progress as it is and returns to
		// the list, the way backing out of the category question
		// above does. It used to close the program: the answer was
		// read as a bare line, and an empty one failed to parse.
		progress, cancelled, err := promptProgressCancelable(userConfig, "Progress",
			"Set the episodes watched",
			fmt.Sprintf("currently %s · a number · esc to leave it alone", current))
		if err != nil {
			Log(fmt.Sprintf("Failed to get progress input: %v", err))
			cancelled = true
		}
		if cancelled {
			return entryPickAgain
		}
		if err := UpdateAnimeProgress(user.Token, animeID, progress); err != nil {
			Log(fmt.Sprintf("Failed to update anime progress: %v", err))
			Exit(fmt.Errorf("Failed to update anime progress"))
		}

	case "SCORE":
		current := "None"
		if entry.Score > 0 {
			current = strconv.Itoa(int(entry.Score))
		}
		Out(fmt.Sprintf("Current score: %s", current))
		err := RateAnime(user.Token, animeID)
		switch {
		case errors.Is(err, ErrRatingDeclined):
			// Escaping the prompt is an answer, not a failure. This
			// used to exit the program with "Failed to update anime
			// score" because the decline was indistinguishable from
			// a broken write.
			Out("Score unchanged.")
		case err != nil:
			Log(fmt.Sprintf("Failed to update anime score: %v", err))
			Exit(fmt.Errorf("Failed to update anime score"))
		}

	case "QUALITY":
		// Kept locally in show_prefs.json, not on the tracker, so
		// there is no list to refresh afterwards.
		changed, qualityErr := PromptShowQuality(userConfig, animeID)
		if qualityErr != nil {
			Log(fmt.Sprintf("Failed to set the show's quality: %v", qualityErr))
			Out("Could not save the quality for this show.")
			return entryLeave
		}
		if !changed {
			return entryPickAgain
		}
		return entryLeave
	}
	return entryChanged
}

// getEntriesByCategory is the entries a list key stands for, each show once.
// CURRENT includes rewatches, and ALL is every list together.
func getEntriesByCategory(list AnimeList, category string) []Entry {
	var buckets [][]Entry
	switch category {
	case "ALL":
		buckets = [][]Entry{list.Watching, list.Completed, list.Paused, list.Dropped, list.Planning, list.Rewatching}
	case "CURRENT":
		buckets = [][]Entry{list.Watching, list.Rewatching}
	case "COMPLETED":
		buckets = [][]Entry{list.Completed}
	case "PAUSED":
		buckets = [][]Entry{list.Paused}
	case "DROPPED":
		buckets = [][]Entry{list.Dropped}
	case "PLANNING":
		buckets = [][]Entry{list.Planning}
	case "REWATCHING":
		buckets = [][]Entry{list.Rewatching}
	default:
		return []Entry{}
	}
	// A stale cache or two trackers merged can put one show on two lists.
	entries := make([]Entry, 0)
	for _, bucket := range buckets {
		entries = append(entries, bucket...)
	}
	return dedupeEntriesByMediaID(entries)
}

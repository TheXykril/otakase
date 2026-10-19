package internal

import (
	"fmt"
)

// "Already watched some elsewhere": adding a show that was started somewhere
// else -- a streaming site, a friend's TV -- with the episodes already seen,
// so the tracker counts them and otakase starts at the next one instead of
// episode 1. Finishing it that way marks it completed.

// watchedElsewhereKey is the add-to-list choice that asks for progress.
const watchedElsewhereKey = "WATCHED_ELSEWHERE"

// watchedElsewhereStatus is the list a show belongs on after progress
// episodes, and the progress to record: completed once every episode is
// seen, with the count held at the total.
func watchedElsewhereStatus(progress, total int) (string, int) {
	if total > 0 && progress >= total {
		return "COMPLETED", total
	}
	return "CURRENT", progress
}

// addWatchedElsewhere asks how far the viewer got and records it. A show not
// finished goes on to its next episode; a finished one returns to the list.
func addWatchedElsewhere(config *Config, user *User, animeID int, selected SelectionOption) SelectionOption {
	back := SelectionOption{Key: "-2", Label: "Back"}

	total := 0
	if media, err := GetAnimeDataByID(animeID, user.Token); err == nil {
		total = media.TotalEpisodes
	} else {
		Log(fmt.Sprintf("Watched elsewhere: episode count for %d: %v", animeID, err))
	}
	hint := "a number · esc to go back"
	if total > 0 {
		hint = fmt.Sprintf("of %d · %s", total, hint)
	}
	progress, cancelled, err := promptProgressCancelable(config, "Add", "How many episodes have you watched?", hint)
	if err != nil {
		Log(fmt.Sprintf("Watched elsewhere: %v", err))
		return back
	}
	if cancelled {
		return back
	}

	status, progress := watchedElsewhereStatus(progress, total)
	if err := UpdateAnimeStatus(user.Token, animeID, status); err != nil {
		Log(fmt.Sprintf("Failed to add anime to list: %v", err))
		Exit(fmt.Errorf("Failed to add anime to list"))
	}
	if err := UpdateAnimeProgress(user.Token, animeID, progress); err != nil {
		Log(fmt.Sprintf("Failed to set progress: %v", err))
		Exit(fmt.Errorf("Failed to set progress"))
	}
	if err := RefreshUserAnimeList(config, user); err != nil {
		Log(fmt.Sprintf("Failed to refresh anime list: %v", err))
		Exit(fmt.Errorf("Failed to refresh anime list"))
	}

	if status == "COMPLETED" {
		Out(fmt.Sprintf("Added as completed, all %d episodes watched.", progress))
		return back
	}
	return selected
}

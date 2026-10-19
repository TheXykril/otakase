package internal

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/pkg/browser"

	"github.com/thexykril/otakase/internal/icons"
)

// backOption is what the list flows hand back when the viewer went back.
var backOption = SelectionOption{Key: "-2", Label: "Back"}

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

// aniListPage is a show's page on AniList.
func aniListPage(id int) string {
	return fmt.Sprintf("https://anilist.co/anime/%d", id)
}

// openInDefaultBrowser opens url, saying so first, and reports whether it
// worked.
func openInDefaultBrowser(url string) bool {
	Out(fmt.Sprintf("Opening %s", url))
	if err := browser.OpenURL(url); err != nil {
		Log(fmt.Sprintf("Error opening browser: %v", err))
		Out("Failed to open browser.")
		return false
	}
	return true
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

// AddNewAnime searches AniList and puts the show picked on one of the
// tracker's lists. It returns the show, or backOption when the viewer went
// back at any point.
func AddNewAnime(userConfig *Config, anime *Anime, user *User, databaseAnimes *[]Anime) SelectionOption {
	// Backing out of the question, or a search that fails, returns to the list
	// this was opened from. Both used to close the program, which made opening
	// this by accident expensive and a dropped connection fatal.
	query, cancelled, err := promptCancelable(userConfig, "Add",
		"Search AniList for an anime",
		"enter to search · esc to go back")
	if err != nil {
		Log("Error getting user input: " + err.Error())
		return backOption
	}
	if cancelled {
		return backOption
	}

	preview := userConfig.RofiSelection && userConfig.ImagePreview
	var options []SelectionOption
	var previews map[string]RofiSelectPreview
	endBusy := BeginBusy(userConfig, "Searching AniList")
	if preview {
		previews, err = SearchAnimeAnilistPreview(query, user.Token)
	} else {
		options, err = SearchAnimeAnilist(query, user.Token)
	}
	endBusy()
	if err != nil {
		Log(fmt.Sprintf("Failed to search anime: %v", err))
		Out(fmt.Sprintf("Could not search for %q: %v", query, err))
		return backOption
	}

	var show SelectionOption
	if preview {
		show, err = DynamicSelectPreview(previews, false)
	} else {
		show, err = DynamicSelect(options)
	}
	if err != nil {
		Log(fmt.Sprintf("No anime available: %v", err))
		Exit(fmt.Errorf("No anime available"))
	}
	switch show.Key {
	case "-1":
		Exit(nil)
	case "-2":
		return backOption
	}

	animeID, err := strconv.Atoi(show.Key)
	if err != nil {
		Log(fmt.Sprintf("Failed to convert anime ID to integer: %v", err))
		Exit(fmt.Errorf("Failed to convert anime ID to integer"))
	}
	if !UsesRemoteTracking(userConfig) {
		return show
	}

	EnterAppScreen()
	Out("Select which list to add the anime to:")
	lists := append(listStatusOptions(),
		SelectionOption{Key: watchedElsewhereKey, Label: "Already watched some elsewhere", Icon: icons.Counter})
	list, err := DynamicSelect(lists)
	if err != nil {
		Log(fmt.Sprintf("Failed to select category: %v", err))
		Exit(fmt.Errorf("Failed to select category"))
	}
	switch list.Key {
	case "-1":
		Exit(nil)
	case "-2":
		return backOption
	case watchedElsewhereKey:
		return addWatchedElsewhere(userConfig, user, animeID, show)
	}

	if err := UpdateAnimeStatus(user.Token, animeID, list.Key); err != nil {
		Log(fmt.Sprintf("Failed to add anime to list: %v", err))
		Exit(fmt.Errorf("Failed to add anime to list"))
	}
	if err := RefreshUserAnimeList(userConfig, user); err != nil {
		Log(fmt.Sprintf("Failed to refresh anime list: %v", err))
		Exit(fmt.Errorf("Failed to refresh anime list"))
	}
	return show
}

// handleUnreleasedAnime is what picking a show that has not started airing
// leads to: there is nothing to play, so it offers to plan it instead.
func handleUnreleasedAnime(userConfig *Config, user *User, anime *Anime, entry Entry) {
	// Whatever happens, the next turn shows the menu rather than coming
	// straight back here.
	defer func() { anime.Ep.ContinueLast = false }()

	title := entry.Media.Title.Romaji
	if userConfig != nil && userConfig.AnimeNameLanguage == "english" && entry.Media.Title.English != "" {
		title = entry.Media.Title.English
	}
	if title == "" {
		title = strconv.Itoa(entry.Media.ID)
	}
	Out(fmt.Sprintf("%s is not released yet.", title))

	var options []SelectionOption
	if UsesRemoteTracking(userConfig) {
		options = append(options, SelectionOption{Key: "planning", Label: "Add to Plan to Watch", Icon: icons.Planning})
	}
	options = append(options,
		SelectionOption{Key: "details", Label: "Open AniList details", Icon: icons.Info},
		SelectionOption{Key: "back", Label: "Back to list", Icon: icons.Back},
	)
	picked, err := promptSelect(options)
	if err != nil {
		Log(fmt.Sprintf("Error in unreleased anime prompt: %v", err))
		return
	}

	switch picked.Key {
	case "planning":
		if err := AddAnimeToList(entry.Media.ID, "PLANNING", user.Token); err != nil {
			Log(fmt.Sprintf("Error adding unreleased anime to planning: %v", err))
			Out("Failed to add to Plan to Watch.")
			return
		}
		Out("Added to Plan to Watch.")
		if err := RefreshUserAnimeList(userConfig, user); err != nil {
			Log("Error refreshing anime list: " + err.Error())
		}
	case "details":
		openInDefaultBrowser(aniListPage(entry.Media.ID))
	case "-1":
		Exit(nil)
	}
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

// ChangeProvider lets the viewer pick the provider episodes come from, and
// saves the choice to the config file.
func ChangeProvider(userConfig *Config) {
	options := providerSelectionOptions()
	if len(options) == 0 {
		Out("\nNo providers are currently enabled.\n")
		return
	}
	picked, err := DynamicSelect(options)
	if err != nil || picked.Key == "-1" || picked.Key == "-2" {
		return
	}

	provider := canonicalProviderConfigValue(picked.Key)
	userConfig.Provider = provider
	// Dropped so the next lookup builds the provider just chosen.
	CurrentProvider = nil
	if values, err := LoadConfigFromFile(ConfigFilePath); err == nil {
		values["Provider"] = provider
		SaveConfigToFile(ConfigFilePath, values)
	}

	Out(fmt.Sprintf("\nProvider successfully changed to %s.\n", providerConfigDisplayLabel(userConfig.Provider)))
	// Long enough to read before the menu redraws over it.
	time.Sleep(time.Second)
}

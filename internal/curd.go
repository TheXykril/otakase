package internal

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gen2brain/beeep"
	"github.com/pkg/browser"

	"github.com/thexykril/otakase/internal/torrentstream"
)

var alternateScreenActive bool

// ClearLogFile removes all contents from the specified log file
func ClearLogFile(logFile string) error {
	// Drop any cached append handle first: it still points at the pre-truncation
	// file description, and writing through it would restore the old length.
	if err := CloseLogFile(); err != nil {
		return fmt.Errorf("failed to close log file: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(logFile), 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open the file with truncate flag to clear its contents.
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	return nil
}

// LogData logs the input data into a specified log file with the format [LOG] time lineNumber: logData
func Log(data interface{}) error {
	// Attempt to marshal the data into JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	// Get the caller information
	_, filename, lineNumber, ok := runtime.Caller(1) // Caller 1 gives the caller of LogData
	if !ok {
		return fmt.Errorf("unable to get caller information")
	}

	// Log the current time and the JSON representation along with caller info
	currentTime := time.Now().Format("2006/01/02 15:04:05")
	logMessage := fmt.Sprintf("[LOG] %s %s:%d: %s\n", currentTime, filename, lineNumber, jsonData)
	return writeLogLine(logMessage)
}

// ClearScreen clears the terminal screen and saves the state
func ClearScreen() {
	userCurdConfig := GetGlobalConfig()
	if userCurdConfig == nil {
		return
	}

	if userCurdConfig.AlternateScreen == false {
		return
	}

	fmt.Print("\033[?1049h") // Switch to alternate screen buffer
	fmt.Print("\033[2J")     // Clear the entire screen
	fmt.Print("\033[H")      // Move cursor to the top left
	alternateScreenActive = true
}

// RestoreScreen restores the original terminal state
func RestoreScreen() {
	if !alternateScreenActive {
		return
	}

	fmt.Print("\033[?1049l") // Switch back to the main screen buffer
	alternateScreenActive = false
}

func ExitCurd(err error) {
	RestoreScreen()

	// Quitting during a slow launch must not leave a "starting..." notification
	// on screen describing something that is no longer happening.
	EndStartupProgress()

	// Torrent-backed playback keeps a client and a temporary cache directory
	// alive for the session. Leaving either behind would mean a stray port and a
	// download the user never asked to keep.
	torrentstream.Shutdown()

	anime := GetGlobalAnime()
	socketPath := ""
	if (anime != nil) && (anime.Ep.Player.SocketPath != "") {
		socketPath = anime.Ep.Player.SocketPath
		_, err = MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"quit"})
		if err != nil {
			Log("Error closing MPV: " + err.Error())
		}
	}

	CurdOut("Have a great day!")
	// If the error is not about the connection refused, print the error
	if err != nil && (socketPath == "" || !strings.Contains(err.Error(), "dial unix "+socketPath+": connect: connection refused")) {
		CurdOut(fmt.Sprintf("Error: %v", err))
		if runtime.GOOS == "windows" {
			fmt.Println("Press Enter to exit")
			AwaitEnter()
			exitWithRestore(1)
		} else {
			exitWithRestore(1)
		}
	}
	exitWithRestore(0)
}

func CurdOut(data interface{}) {
	userCurdConfig := GetGlobalConfig()
	if userCurdConfig == nil {
		userCurdConfig = &CurdConfig{}
	}
	if !userCurdConfig.RofiSelection {
		fmt.Println(fmt.Sprintf("%v", data))
	} else {
		switch runtime.GOOS {
		case "windows":
			err := beeep.Notify(
				DisplayName,
				fmt.Sprintf("%v", data),
				"",
			)

			if err != nil {
				Log(fmt.Sprintf("Failed to send notification: %v", err))
			}
		case "linux":
			// Check if the input starts with "-i" for image notification
			dataStr := fmt.Sprintf("%v", data)
			if strings.HasPrefix(dataStr, "-i") && userCurdConfig.ImagePreview && userCurdConfig.RofiSelection {
				// Split the string to get image path and message
				parts := strings.SplitN(dataStr, " ", 3)
				if len(parts) == 3 {
					// Remove quotes from the message
					message := strings.Trim(parts[2], "\"")
					cmd := exec.Command("notify-send",
						"-a", DisplayName,
						"-h", "string:x-canonical-private-synchronous:curd-notification",
						DisplayName,
						"-i", parts[1],
						message)
					err := cmd.Run()
					if err != nil {
						Log(fmt.Sprintf("%v", cmd))
						Log(fmt.Sprintf("Failed to send notification: %v", err))
					}
				}
			} else {
				cmd := exec.Command("notify-send",
					"-a", DisplayName,
					"-h", "string:x-canonical-private-synchronous:curd-notification",
					DisplayName,
					dataStr)
				err := cmd.Run()
				if err != nil {
					Log(fmt.Sprintf("%v", cmd))
					Log(fmt.Sprintf("Failed to send notification: %v", err))
				}
			}
		}
	}
}

func UpdateAnimeEntry(userCurdConfig *CurdConfig, user *User) {
	if !UsesRemoteTracking(userCurdConfig) {
		CurdOut("Remote tracking is disabled in your config.")
		return
	}

	// Create update options
	updateOptions := []SelectionOption{
		{Key: "CATEGORY", Label: "Change Anime Category"},
		{Key: "PROGRESS", Label: "Change Progress"},
		{Key: "SCORE", Label: "Add/Change Score"},
	}

	// Navigation loop for update option selection
updateOptionLoop:
	for {
		// Select update option
		updateSelection, err := DynamicSelect(updateOptions)
		if err != nil {
			Log(fmt.Sprintf("Failed to select update option: %v", err))
			ExitCurd(fmt.Errorf("Failed to select update option"))
		}

		if updateSelection.Key == "-1" {
			ExitCurd(nil)
		}

		// Back from update selection returns to home menu
		if updateSelection.Key == "-2" {
			return // Return to caller (home menu)
		}

		// Get user's anime list
		var animeListOptions []SelectionOption
		var animeListMapPreview map[string]RofiSelectPreview

		if user.ListSync != nil {
			user.AnimeList = user.ListSync.Current()
		}

		if userCurdConfig.RofiSelection && userCurdConfig.ImagePreview {
			animeListMapPreview = buildCategoryPreviewOptions(user.AnimeList, "ALL")
		} else {
			animeListOptions = buildCategorySelectionOptions(user.AnimeList, "ALL")
		}

		// Anime selection loop
	animeSelectLoop:
		for {
			// Select anime to update
			var selectedAnime SelectionOption
			if userCurdConfig.RofiSelection && userCurdConfig.ImagePreview {
				selectedAnime, err = DynamicSelectPreviewWithRefresh(animeListMapPreview, false, &PreviewSelectionRefreshConfig{
					Updates: user.ListSync.Updates(),
					BuildOptions: func(list AnimeList) map[string]RofiSelectPreview {
						return buildCategoryPreviewOptions(list, "ALL")
					},
				})
			} else {
				selectedAnime, err = DynamicSelectWithRefresh(animeListOptions, &SelectionRefreshConfig{
					Updates: user.ListSync.Updates(),
					BuildOptions: func(list AnimeList) []SelectionOption {
						return buildCategorySelectionOptions(list, "ALL")
					},
				})
			}
			if err != nil {
				Log(fmt.Sprintf("Failed to select anime: %v", err))
				ExitCurd(fmt.Errorf("Failed to select anime"))
			}

			if selectedAnime.Key == "-1" {
				ExitCurd(nil)
			}

			// Back from anime selection goes to update option selection
			if selectedAnime.Key == "-2" {
				ClearScreen()
				continue updateOptionLoop
			}

			animeID, err := strconv.Atoi(selectedAnime.Key)
			if err != nil {
				Log(fmt.Sprintf("Failed to convert anime ID: %v", err))
				ExitCurd(fmt.Errorf("Failed to convert anime ID"))
			}

			if user.ListSync != nil {
				user.AnimeList = user.ListSync.Current()
			}

			// After getting animeID, get the current anime entry
			selectedAnilistAnime, err := FindAnimeByAnilistID(user.AnimeList, selectedAnime.Key)
			if err != nil {
				Log(fmt.Sprintf("Can not find the anime in tracked anime list: %v", err))
				ExitCurd(fmt.Errorf("Can not find the anime in tracked anime list"))
			}
			ClearScreen()

			// Final selection loop (for category/progress/score)
			for {
				switch updateSelection.Key {
				case "CATEGORY":
					categories := []SelectionOption{
						{Key: "CURRENT", Label: "Currently Watching"},
						{Key: "COMPLETED", Label: "Completed"},
						{Key: "PAUSED", Label: "On Hold"},
						{Key: "DROPPED", Label: "Dropped"},
						{Key: "PLANNING", Label: "Plan to Watch"},
						{Key: "REPEATING", Label: "Rewatching"}, // Anilist uses REPEATING for rewatching
					}

					currentStatus := "None"
					if selectedAnilistAnime.Status != "" {
						// Find the label for the current status
						for _, cat := range categories {
							if cat.Key == selectedAnilistAnime.Status {
								currentStatus = cat.Label
								break
							}
						}
					}
					CurdOut(fmt.Sprintf("Current category: %s", currentStatus))

					categorySelection, err := DynamicSelect(categories)
					if err != nil {
						Log(fmt.Sprintf("Failed to select category: %v", err))
						ExitCurd(fmt.Errorf("Failed to select category"))
					}

					if categorySelection.Key == "-1" {
						ExitCurd(nil)
					}

					// Back from category selection goes to anime selection
					if categorySelection.Key == "-2" {
						ClearScreen()
						continue animeSelectLoop
					}

					err = UpdateAnimeStatus(user.Token, animeID, categorySelection.Key)
					if err != nil {
						Log(fmt.Sprintf("Failed to update anime status: %v", err))
						ExitCurd(fmt.Errorf("Failed to update anime status"))
					}

				case "PROGRESS":
					currentProgress := "None"
					if selectedAnilistAnime.Progress > 0 {
						currentProgress = strconv.Itoa(selectedAnilistAnime.Progress)
					}

					// Backing out leaves the progress as it is and returns to
					// the list, the way backing out of the category question
					// above does. It used to close the program: the answer was
					// read as a bare line, and an empty one failed to parse.
					progressNum, cancelled, err := promptProgressCancelable(userCurdConfig, "Progress",
						"Set the episodes watched",
						fmt.Sprintf("currently %s · a number · esc to leave it alone", currentProgress))
					if err != nil {
						Log(fmt.Sprintf("Failed to get progress input: %v", err))
						cancelled = true
					}
					if cancelled {
						ClearScreen()
						continue animeSelectLoop
					}

					err = UpdateAnimeProgress(user.Token, animeID, progressNum)
					if err != nil {
						Log(fmt.Sprintf("Failed to update anime progress: %v", err))
						ExitCurd(fmt.Errorf("Failed to update anime progress"))
					}

				case "SCORE":
					currentScore := "None"
					if selectedAnilistAnime.Score > 0 {
						currentScore = strconv.Itoa(int(selectedAnilistAnime.Score))
					}
					CurdOut(fmt.Sprintf("Current score: %s", currentScore))

					err = RateAnime(user.Token, animeID)
					if err != nil {
						Log(fmt.Sprintf("Failed to update anime score: %v", err))
						ExitCurd(fmt.Errorf("Failed to update anime score"))
					}
				}

				if err := RefreshUserAnimeList(userCurdConfig, user); err != nil {
					Log(fmt.Sprintf("Failed to refresh anime list: %v", err))
					ExitCurd(fmt.Errorf("Failed to refresh anime list"))
				}

				CurdOut("Anime updated successfully!")
				return
			}
		}
	}
}

func UpdateCurd(repo, fileName string) error {
	// Get the path of the currently running executable
	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("unable to find current executable: %v", err)
	}
	// Resolve symlinks so we replace the real binary (e.g. /usr/local/bin/curd).
	if resolved, resolveErr := filepath.EvalSymlinks(executablePath); resolveErr == nil && resolved != "" {
		executablePath = resolved
	}

	binaryName, err := curdReleaseBinaryName()
	if err != nil {
		return err
	}
	_ = fileName // retained for call-site compatibility

	if strings.TrimSpace(repo) == "" {
		repo = DefaultUpdateRepo
	}
	// GitHub release URL for curd
	url := fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", repo, binaryName)

	// Prefer a temp file next to the executable (same filesystem → atomic rename).
	// Fall back to OS temp dir when the install dir is not writable (e.g. /usr/bin).
	tmpPath, out, err := createUpdateTempFile(executablePath, binaryName)
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %v", err)
	}

	// Download the curd executable
	client := sharedHTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	resp, err := client.Get(url)
	if err != nil {
		out.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to download file: %v", err)
	}
	defer resp.Body.Close()

	// Check if the download was successful
	if resp.StatusCode != http.StatusOK {
		out.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to download file: received status code %d", resp.StatusCode)
	}

	// Copy the downloaded content to the temporary file
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write to temporary file: %v", err)
	}
	if err := out.Chmod(0755); err != nil {
		out.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to set file permissions: %v", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close temporary file: %v", err)
	}

	// Replace the old executable (prompts for sudo if the install path is not writable).
	if err := replaceExecutable(tmpPath, executablePath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func AddNewAnime(userCurdConfig *CurdConfig, anime *Anime, user *User, databaseAnimes *[]Anime) SelectionOption {
	var animeMapPreview map[string]RofiSelectPreview
	var animeOptions []SelectionOption
	var anilistSelectedOption SelectionOption

	// Backing out of the question, or a search that fails, returns to the list
	// this was opened from. Both used to close the program, which made opening
	// this by accident expensive and a dropped connection fatal.
	query, cancelled, err := promptCancelable(userCurdConfig, "Add",
		"Search AniList for an anime",
		"enter to search · esc to go back")
	if err != nil {
		Log("Error getting user input: " + err.Error())
		return SelectionOption{Key: "-2", Label: "Back"}
	}
	if cancelled {
		return SelectionOption{Key: "-2", Label: "Back"}
	}
	if userCurdConfig.RofiSelection && userCurdConfig.ImagePreview {
		animeMapPreview, err = SearchAnimeAnilistPreview(query, user.Token)
	} else {
		animeOptions, err = SearchAnimeAnilist(query, user.Token)
	}
	if err != nil {
		Log(fmt.Sprintf("Failed to search anime: %v", err))
		CurdOut(fmt.Sprintf("Could not search for %q: %v", query, err))
		return SelectionOption{Key: "-2", Label: "Back"}
	}
	if userCurdConfig.RofiSelection && userCurdConfig.ImagePreview {
		anilistSelectedOption, err = DynamicSelectPreview(animeMapPreview, false)
	} else {
		anilistSelectedOption, err = DynamicSelect(animeOptions)
	}
	if err != nil {
		Log(fmt.Sprintf("No anime available: %v", err))
		ExitCurd(fmt.Errorf("No anime available"))
	}

	if anilistSelectedOption.Key == "-1" {
		ExitCurd(nil)
	}

	// Handle back button - return to caller
	if anilistSelectedOption.Key == "-2" {
		return SelectionOption{Key: "-2", Label: "Back"}
	}

	animeID, err := strconv.Atoi(anilistSelectedOption.Key)
	if err != nil {
		Log(fmt.Sprintf("Failed to convert anime ID to integer: %v", err))
		ExitCurd(fmt.Errorf("Failed to convert anime ID to integer"))
	}

	if !UsesRemoteTracking(userCurdConfig) {
		return anilistSelectedOption
	}

	// Add category selection before adding to list
	categories := []SelectionOption{
		{Key: "CURRENT", Label: "Currently Watching"},
		{Key: "COMPLETED", Label: "Completed"},
		{Key: "PAUSED", Label: "On Hold"},
		{Key: "DROPPED", Label: "Dropped"},
		{Key: "PLANNING", Label: "Plan to Watch"},
		{Key: "REPEATING", Label: "Rewatching"}, // Anilist uses REPEATING for rewatching
	}

	ClearScreen()
	CurdOut("Select which list to add the anime to:")

	categorySelection, err := DynamicSelect(categories)
	if err != nil {
		Log(fmt.Sprintf("Failed to select category: %v", err))
		ExitCurd(fmt.Errorf("Failed to select category"))
	}

	if categorySelection.Key == "-1" {
		ExitCurd(nil)
	}

	// Handle back button - return to caller
	if categorySelection.Key == "-2" {
		return SelectionOption{Key: "-2", Label: "Back"}
	}

	err = UpdateAnimeStatus(user.Token, animeID, categorySelection.Key)
	if err != nil {
		Log(fmt.Sprintf("Failed to add anime to list: %v", err))
		ExitCurd(fmt.Errorf("Failed to add anime to list"))
	}

	if err := RefreshUserAnimeList(userCurdConfig, user); err != nil {
		Log(fmt.Sprintf("Failed to refresh anime list: %v", err))
		ExitCurd(fmt.Errorf("Failed to refresh anime list"))
	}

	return anilistSelectedOption
}

func SetupCurd(userCurdConfig *CurdConfig, anime *Anime, user *User, databaseAnimes *[]Anime) {
	var err error
	var startingRewatch bool

	// Filter anime list based on selected category
	var animeListOptions []SelectionOption
	var animeListMapPreview map[string]RofiSelectPreview

	// Initialize anime list. On a cache hit this is instant (reads from disk) and
	// the user ID is seeded from the cached payload, avoiding a blocking network
	// round-trip to AniList. The real user ID + latest list are refreshed in the
	// background goroutine inside InitializeUserAnimeList.
	if err := InitializeUserAnimeList(userCurdConfig, user); err != nil {
		Log(fmt.Sprintf("Failed to initialize anime list: %v", err))
		ExitCurd(fmt.Errorf("Failed to get user data\nYou can reset the token by running `curd -change-token`"))
	}

	// Variables for selection results (used in both branches and after)
	var anilistSelectedOption SelectionOption
	var selectedAllanimeAnime SelectionOption
	var userQuery string
	_ = selectedAllanimeAnime // Used later in the function

	// Navigation loop for the entire setup process
	for {
		startingRewatch = false

		// If continueLast flag is set, directly get the last watched anime
		if anime.Ep.ContinueLast {
			// Get the last anime ID from the curd_id file
			idFilePath := filepath.Join(os.ExpandEnv(userCurdConfig.StoragePath), "curd_id")
			idBytes, err := os.ReadFile(idFilePath)
			if err != nil {
				Log("Error reading curd_id file: " + err.Error())
				ExitCurd(fmt.Errorf("No last watched anime found"))
			}

			anilistID, err := strconv.Atoi(string(idBytes))
			if err != nil {
				Log("Error converting anilist ID: " + err.Error())
				ExitCurd(fmt.Errorf("Invalid anime ID in curd_id file"))
			}

			// Find the anime in database
			animePointer := LocalFindAnime(*databaseAnimes, anilistID, "")
			if animePointer == nil {
				ExitCurd(fmt.Errorf("Last watched anime not found in database"))
			}

			// Set the anime details
			anime.AnilistId = animePointer.AnilistId
			anilistSelectedOption.Key = strconv.Itoa(animePointer.AnilistId)
			// anime.ProviderId = animePointer.ProviderId
			// anime.Title = animePointer.Title
			// anime.Ep.Number = animePointer.Ep.Number
			// anime.Ep.Player.PlaybackTime = animePointer.Ep.Player.PlaybackTime
			// anime.Ep.Resume = true

		} else {
			// An action chosen by its key from inside a list, to be answered on
			// the next turn of the loop. It has to live outside the loop: the
			// selection below is declared inside it and overwritten by the menu
			// every time round, so handing an action back that way silently
			// dropped it and showed the menu instead.
			var pendingAction SelectionOption

			// Navigation loop for category and anime selection
		categorySelectionLoop:
			for {
				// Skip category selection if Current flag is set
				var categorySelection SelectionOption
				if pendingAction.Key != "" {
					categorySelection = pendingAction
					pendingAction = SelectionOption{}
				} else if SkipCategoryMenu(userCurdConfig) {
					categorySelection = SelectionOption{
						Key:   "CURRENT",
						Label: "Currently Watching",
					}
				} else {
					// Create category selection map
					// Get ordered categories
					orderedCategories := getOrderedCategories(userCurdConfig)

					// Use DynamicSelect with ordered categories directly
					categorySelection, err = DynamicSelect(orderedCategories)

					if err != nil {
						Log(fmt.Sprintf("Failed to select category: %v", err))
						ExitCurd(fmt.Errorf("Failed to select category"))
					}

					categorySelection = NormalizeSelectionKey(categorySelection)
					if SelectionMeansQuit(categorySelection) {
						ExitCurd(nil)
					}

					if SelectionMeansBack(categorySelection) {
						continue
					}
					if categorySelection.Key == "" {
						// Empty/cancelled selection — re-show category menu.
						continue
					}

					ClearScreen()
				}

				// Handled here rather than beside the prompt above: an action can
				// also arrive from a key pressed inside a list, and inside the
				// prompt's branch it was skipped entirely -- every action key fell
				// through to being looked up as a category, which has no entries
				// and shows an empty list.
				if categorySelection.Key == "PROVIDER" {
					ClearScreen()
					ChangeProvider(userCurdConfig)
					ClearScreen()
					continue categorySelectionLoop
				} else if categorySelection.Key == "TRACKER" {
					ClearScreen()
					ChangeTracker(userCurdConfig, user)
					ClearScreen()
					continue categorySelectionLoop
				} else if categorySelection.Key == "UPDATE" {
					ClearScreen()
					UpdateAnimeEntry(userCurdConfig, user)
					// If UpdateAnimeEntry returns, user pressed back - continue to category selection
					ClearScreen()
					continue categorySelectionLoop
				} else if categorySelection.Key == "UNTRACKED" {
					ClearScreen()
					WatchUntracked(userCurdConfig)
					// If WatchUntracked returns, user pressed back OR watched is done- continue to category selection
					ClearScreen()
					continue categorySelectionLoop
				} else if categorySelection.Key == "REMAP_PROVIDER" {
					ClearScreen()
					RemapProviderAnime(userCurdConfig, user, databaseAnimes)
					ClearScreen()
					continue categorySelectionLoop
				} else if categorySelection.Key == "CONTINUE_LAST" {
					anime.Ep.ContinueLast = true
				}

				if user.ListSync != nil {
					user.AnimeList = user.ListSync.Current()
				}

				if userCurdConfig.RofiSelection && userCurdConfig.ImagePreview {
					animeListMapPreview = buildCategoryPreviewOptions(user.AnimeList, categorySelection.Key)
				} else {
					animeListOptions = buildCategorySelectionOptions(user.AnimeList, categorySelection.Key)
				}

				// Anime selection loop (for back navigation)
			animeSelectionLoop:
				for {
					if anime.Ep.ContinueLast {
						// Get the last watched anime ID from the curd_id file
						curdIDPath := filepath.Join(os.ExpandEnv(userCurdConfig.StoragePath), "curd_id")
						curdIDBytes, err := os.ReadFile(curdIDPath)
						if err != nil {
							Log(fmt.Sprintf("Error reading curd_id file: %v", err))
							ExitCurd(fmt.Errorf("Error reading curd_id file"))
						}

						lastWatchedID, err := strconv.Atoi(strings.TrimSpace(string(curdIDBytes)))
						if err != nil {
							Log(fmt.Sprintf("Error converting curd_id to integer: %v", err))
							ExitCurd(fmt.Errorf("Error converting curd_id to integer"))
						}

						anime.AnilistId = lastWatchedID
						anilistSelectedOption.Key = strconv.Itoa(lastWatchedID)
						break categorySelectionLoop
					}

					// Select anime to watch (Anilist)
					var err error
					if userCurdConfig.RofiSelection && userCurdConfig.ImagePreview {
						anilistSelectedOption, err = DynamicSelectPreviewWithRefresh(animeListMapPreview, true, &PreviewSelectionRefreshConfig{
							Updates: user.ListSync.Updates(),
							BuildOptions: func(list AnimeList) map[string]RofiSelectPreview {
								return buildCategoryPreviewOptions(list, categorySelection.Key)
							},
						})
					} else {
						// Add "Add new anime" option to the slice
						tempOptions := make([]SelectionOption, len(animeListOptions))
						copy(tempOptions, animeListOptions)

						tempOptions = append(tempOptions, SelectionOption{
							Key:   "add_new",
							Label: "Add new anime",
						})

						// Tab moves between categories without leaving the list.
						// The whole list is already in memory, so switching one
						// is a filter rather than a fetch. activeCategory follows
						// the tab so a background refresh rebuilds what is on
						// screen rather than the category first opened.
						categoryTabs, categoryActions := SplitMenuOrder(userCurdConfig.MenuOrder)
						// Counting is a filter over a list already in memory, so
						// every tab can say how much is behind it.
						for i := range categoryTabs {
							categoryTabs[i].Count = len(getEntriesByCategory(user.AnimeList, categoryTabs[i].Key))
						}
						activeCategory := categorySelection.Key

						anilistSelectedOption, err = DynamicSelectWithRefresh(tempOptions, &SelectionRefreshConfig{
							Updates: user.ListSync.Updates(),
							BuildOptions: func(list AnimeList) []SelectionOption {
								return buildCategorySelectionOptions(list, activeCategory)
							},
							Categories:     categoryTabs,
							ActiveCategory: activeCategory,
							Actions:        categoryActions,
							LoadCategory: func(key string) []SelectionOption {
								activeCategory = key
								return buildCategorySelectionOptions(user.AnimeList, key)
							},
						})
					}
					if err != nil {
						Log(fmt.Sprintf("Error selecting anime: %v", err))
						ExitCurd(fmt.Errorf("Error selecting anime"))
					}

					Log(anilistSelectedOption)

					anilistSelectedOption = NormalizeSelectionKey(anilistSelectedOption)
					if SelectionMeansQuit(anilistSelectedOption) {
						ExitCurd(nil)
					}

					// Handle back navigation - go back to category selection
					if SelectionMeansBack(anilistSelectedOption) {
						if SkipCategoryMenu(userCurdConfig) {
							// Nothing was shown before this list, so there is
							// nothing behind it to go back to.
							ExitCurd(nil)
						}
						ClearScreen()
						continue categorySelectionLoop
					}
					// An action chosen by its key from the list is the same
					// request as choosing it from the menu, so it is answered by
					// the same code: hand the key back to the category loop
					// rather than duplicating the dispatch here.
					if isMenuActionKey(anilistSelectedOption.Key) {
						pendingAction = anilistSelectedOption
						ClearScreen()
						continue categorySelectionLoop
					}

					if anilistSelectedOption.Key == "" {
						continue animeSelectionLoop
					}

					if anilistSelectedOption.Label == "add_new" || anilistSelectedOption.Key == "add_new" {
						addResult := AddNewAnime(userCurdConfig, anime, user, databaseAnimes)
						if addResult.Key == "-2" {
							// Back from add new anime goes to anime selection
							ClearScreen()
							continue animeSelectionLoop
						}
						anilistSelectedOption = addResult
					}

					anime.AnilistId, err = strconv.Atoi(anilistSelectedOption.Key)
					if err != nil {
						Log(fmt.Sprintf("Error converting Anilist ID: %v", err))
						ExitCurd(fmt.Errorf("Error converting Anilist ID"))
					}

					// Successfully selected anime, break out of both loops
					break categorySelectionLoop
				}
			}
		}
		// Wait for the background refresh goroutine to finish before reading
		// progress+1. RefreshDone() is a channel that is closed (broadcast) the
		// instant the goroutine completes — no polling, no race with the UI
		// Updates consumer that already drained the updates channel.
		if user.ListSync != nil {
			select {
			case <-user.ListSync.RefreshDone():
				Log("Background refresh done, using latest anime list for playback")
			case <-time.After(10 * time.Second):
				Log("Timed out waiting for background anime list refresh; using cached list")
			}
			user.AnimeList = user.ListSync.Current()
		}

		animePointer := LocalFindAnime(*databaseAnimes, anime.AnilistId, "")

		// Get anime entry
		selectedAnilistAnime, err := FindAnimeByAnilistID(user.AnimeList, anilistSelectedOption.Key)
		if err != nil {
			if UsesRemoteTracking(userCurdConfig) {
				Log(fmt.Sprintf("Can not find the anime in tracked anime list: %v", err))
				ExitCurd(fmt.Errorf("Can not find the anime in tracked anime list"))
			}

			fallbackAnime, fallbackErr := GetAnimeDataByID(anime.AnilistId, "")
			if fallbackErr != nil {
				Log(fmt.Sprintf("Failed to fetch fallback anime data: %v", fallbackErr))
				ExitCurd(fmt.Errorf("Failed to get anime data"))
			}

			selectedAnilistAnime = &Entry{
				Media: Media{
					ID:       fallbackAnime.AnilistId,
					MalID:    fallbackAnime.MalId,
					Episodes: fallbackAnime.TotalEpisodes,
					Title:    fallbackAnime.Title,
					Status:   "FINISHED",
				},
				Progress:   0,
				Status:     "CURRENT",
				CoverImage: fallbackAnime.CoverImage,
			}
		}

		if selectedAnilistAnime.Media.Status == "NOT_YET_RELEASED" {
			handleUnreleasedAnime(userCurdConfig, user, anime, *selectedAnilistAnime)
			ClearScreen()
			continue
		}

		// Set anime entry
		anime.Title = selectedAnilistAnime.Media.Title
		anime.TotalEpisodes = selectedAnilistAnime.Media.Episodes
		anime.CoverImage = selectedAnilistAnime.CoverImage
		if selectedAnilistAnime.Media.MalID != 0 {
			anime.MalId = selectedAnilistAnime.Media.MalID
		}
		if anime.MalId == 0 {
			anime.MalId, _ = GetAnimeMalID(anime.AnilistId)
		}
		anime.IsAiring = selectedAnilistAnime.Media.Status == "RELEASING" || selectedAnilistAnime.Media.Status == "NOT_YET_RELEASED"
		anime.Rewatching = selectedAnilistAnime.Status == "REPEATING"
		anime.Repeat = selectedAnilistAnime.Repeat
		anime.StartedAt = selectedAnilistAnime.StartedAt
		anime.CompletedAt = selectedAnilistAnime.CompletedAt
		anime.Ep.Number = nextEpisodeFromProgress(selectedAnilistAnime.Progress)
		userQuery = anime.Title.Romaji

		if selectedAnilistAnime.Status == "COMPLETED" {
			CurdOut("This anime is completed. Start rewatch from episode 1? Continue without updating tracker? Open details?")
			selectedOption, err := promptSelect([]SelectionOption{
				{Key: "rewatch", Label: "Start rewatch from episode 1"},
				{Key: "continue", Label: "Continue without updating tracker"},
				{Key: "details", Label: "Open details"},
			})
			if err != nil {
				Log(fmt.Sprintf("Error in completed anime prompt: %v", err))
				ExitCurd(fmt.Errorf("Failed to select completed anime action"))
			}

			switch selectedOption.Key {
			case "rewatch":
				err = StartAnimeRewatch(user.Token, *anime)
				if err != nil {
					Log(fmt.Sprintf("Error starting anime rewatch: %v", err))
					ExitCurd(fmt.Errorf("Failed to move anime to rewatching"))
				}

				anime.Rewatching = true
				anime.SkipRemoteSync = false
				anime.Ep.Number = 1
				anime.Ep.Player.PlaybackTime = 0
				anime.Ep.Resume = false
				startingRewatch = true
				CurdOut("Moved anime to Rewatching and restarting from episode 1.")

				if err := RefreshUserAnimeList(userCurdConfig, user); err != nil {
					Log("Error refreshing anime list: " + err.Error())
					ExitCurd(err)
				}
			case "continue":
				anime.Rewatching = false
				anime.SkipRemoteSync = true
				anime.Ep.Number = 1
				anime.Ep.Player.PlaybackTime = 0
				anime.Ep.Resume = false
				startingRewatch = true
				CurdOut("Starting episode 1 without updating your tracker.")
			case "details":
				url := fmt.Sprintf("https://anilist.co/anime/%d", anime.AnilistId)
				CurdOut(fmt.Sprintf("Opening %s", url))
				if err := browser.OpenURL(url); err != nil {
					Log(fmt.Sprintf("Error opening browser: %v", err))
					CurdOut("Failed to open browser.")
				}
				anime.Ep.ContinueLast = false
				continue
			case "-1":
				ExitCurd(nil)
			default:
				anime.Ep.ContinueLast = false
				continue
			}
		}

		// Check if we need to research provider ID
		needsProviderSearch := false
		if animePointer == nil {
			needsProviderSearch = true
		} else if animePointer.ProviderId == "" {
			needsProviderSearch = true
		} else if animePointer.ProviderName != "" && !ProviderStackContains(userCurdConfig, animePointer.ProviderName) {
			needsProviderSearch = true
		}

		// if anime not found in database or provider changed, find it in animeList
		if needsProviderSearch {
			Log("Anime not found in database for current provider, searching in animeList...")
			mappingOutcome, mappingErr := ResolveAnimeProviderMapping(userCurdConfig, anime, string(userQuery), selectedAnilistAnime)
			if mappingErr != nil {
				Log(fmt.Sprintf("Provider mapping failed: %v", mappingErr))
				ExitCurd(mappingErr)
			}
			switch mappingOutcome {
			case ProviderMappingBack:
				CurdOut("Going back to main menu...")
				RestoreScreen()
				if anime.Ep.ContinueLast {
					anime.Ep.ContinueLast = false
				}
				continue
			case ProviderMappingQuit:
				ExitCurd(nil)
			}
		}

		// if anime found in database, use its playback state
		if animePointer != nil {
			if !needsProviderSearch {
				anime.ProviderId = animePointer.ProviderId
				anime.ProviderName = animePointer.ProviderName
			}
			anime.Ep.Player.PlaybackTime = animePointer.Ep.Player.PlaybackTime
			if anime.Ep.Number == animePointer.Ep.Number {
				anime.Ep.Resume = true
			}

			// If local history episode is ahead of AniList upstream, prompt user
			anilistEpisode := nextEpisodeFromProgress(selectedAnilistAnime.Progress)
			if animePointer.Ep.Number > anilistEpisode {
				trackerLabel := RemoteTrackingDisplayName(userCurdConfig)
				Log(fmt.Sprintf("Local history episode (%d) is ahead of %s episode (%d), prompting user", animePointer.Ep.Number, trackerLabel, anilistEpisode))
				options := []SelectionOption{
					{Key: "update_upstream", Label: fmt.Sprintf("Use %s episode %d", DisplayName, animePointer.Ep.Number)},
					{Key: "use_anilist", Label: fmt.Sprintf("Use %s episode %d", trackerLabel, anilistEpisode)},
				}
				CurdOut(fmt.Sprintf("Curd has episode %d. %s has episode %d. Pick one.", animePointer.Ep.Number, trackerLabel, anilistEpisode))
				selectedOption, err := DynamicSelect(options)
				if err != nil {
					Log("Error in episode conflict selection: " + err.Error())
				} else if selectedOption.Key == "-1" {
					ExitCurd(nil)
				} else if selectedOption.Key == "update_upstream" {
					// Update remote progress to match local history (progress = local ep - 1, since local ep is "next to watch")
					progressToUpdate := animePointer.Ep.Number - 1
					if err := UpdateAnimeProgress(user.Token, anime.AnilistId, progressToUpdate); err != nil {
						Log(fmt.Sprintf("Error updating %s progress to %d: %v", trackerLabel, progressToUpdate, err))
					} else {
						Log(fmt.Sprintf("Updated %s progress to %d", trackerLabel, progressToUpdate))
					}
					anime.Ep.Number = animePointer.Ep.Number
					anime.Ep.Resume = true
				} else if selectedOption.Key == "use_anilist" {
					// Use remote next-episode number (already set from selectedAnilistAnime.Progress + 1)
					anime.Ep.Number = anilistEpisode
					anime.Ep.Player.PlaybackTime = 0
					anime.Ep.Resume = false
				}
			} else {
				anime.Ep.Number = animePointer.Ep.Number
			}
		}

		if startingRewatch {
			anime.Ep.Player.PlaybackTime = 0
			anime.Ep.Resume = false
		}

		if selectedAllanimeAnime.Key == "-1" {
			ExitCurd(nil)
		}

		// If anime is not in watching list, prompt user to add it into watching list
		isInWatchingList := false
		for _, entry := range user.AnimeList.Watching {
			if entry.Media.ID == anime.AnilistId {
				isInWatchingList = true
				break
			}
		}
		if !isInWatchingList {
			for _, entry := range user.AnimeList.Rewatching {
				if entry.Media.ID == anime.AnilistId {
					isInWatchingList = true
					break
				}
			}
		}
		if anime.Rewatching {
			isInWatchingList = true
		}

		if ShouldWriteRemoteTracking(userCurdConfig, anime) && !isInWatchingList {
			// Create options for the prompt
			options := []SelectionOption{
				{Key: "yes", Label: "Add to watching list"},
				{Key: "no", Label: "Continue without adding"},
			}

			var selectedOption SelectionOption
			var err error

			// Use rofi for selection
			selectedOption, err = DynamicSelect(options)
			if err != nil {
				Log("Error in selection: " + err.Error())
				ExitCurd(err)
			}

			if selectedOption.Key == "yes" {
				err = AddAnimeToWatchingList(anime.AnilistId, user.Token)
				if err != nil {
					Log("Error adding anime to watching list: " + err.Error())
					ExitCurd(err)
				}
				if err := RefreshUserAnimeList(userCurdConfig, user); err != nil {
					Log("Error refreshing anime list: " + err.Error())
					ExitCurd(err)
				}
			} else if selectedOption.Key == "-1" {
				ExitCurd(nil)
			} else if selectedOption.Key == "-2" {
				// Handle back button - go back to main menu
				CurdOut("Going back to main menu...")
				RestoreScreen()
				// If we were continuing last, disable it so we go to menu next loop
				if anime.Ep.ContinueLast {
					anime.Ep.ContinueLast = false
				}
				continue
			}
		}

		// If upstream is ahead, update the episode number
		if temp_anime, err := FindAnimeByAnilistID(user.AnimeList, strconv.Itoa(anime.AnilistId)); err == nil {
			upstreamNextEpisode := nextEpisodeFromProgress(temp_anime.Progress)
			if upstreamNextEpisode > anime.Ep.Number {
				anime.Ep.Number = upstreamNextEpisode
				anime.Ep.Player.PlaybackTime = 0
				anime.Ep.Resume = false
			}
		}

		if anime.TotalEpisodes == 0 {
			// Get updated anime data
			Log(selectedAllanimeAnime)
			updatedAnime, err := GetAnimeDataByID(anime.AnilistId, user.Token)
			Log(updatedAnime)
			if err != nil {
				Log(fmt.Sprintf("Error getting updated anime data: %v", err))
			} else {
				anime.TotalEpisodes = updatedAnime.TotalEpisodes
				Log(fmt.Sprintf("Updated total episodes: %d", anime.TotalEpisodes))
			}
		}

		if anime.TotalEpisodes == 0 { // If failed to get anime data
			CurdOut("AniList/MAL did not return total episodes. Attempting to retrieve from provider episode list.")
			providerName, providerID := AnimeProviderID(anime)
			providerTotal, err := GetProviderTotalEpisodes(QualifyProviderID(providerName, providerID), userCurdConfig.SubOrDub)
			if err != nil {
				Log(fmt.Sprintf("Failed to retrieve total episodes from provider episode list: %v", err))
			} else {
				anime.TotalEpisodes = providerTotal
				CurdOut(fmt.Sprintf("Retrieved total episodes from provider: %d", anime.TotalEpisodes))
			}
		}

		if anime.TotalEpisodes == 0 { // If the mapped provider alone couldn't provide a total
			CurdOut("Attempting to determine total episodes from the full provider stack.")
			totalQuery := animeSearchTitle(anime)
			if strings.TrimSpace(totalQuery) == "" {
				totalQuery = string(userQuery)
			}
			stackTotal, stackErr := determineProviderTotalEpisodes(userCurdConfig, totalQuery, anime, userCurdConfig.SubOrDub)
			if stackErr != nil {
				Log(fmt.Sprintf("Failed to determine total episodes from provider stack: %v", stackErr))
			} else {
				anime.TotalEpisodes = stackTotal
				CurdOut(fmt.Sprintf("Retrieved total episodes from provider stack: %d", anime.TotalEpisodes))
			}
		}

		if anime.TotalEpisodes == 0 { // If provider episode list did not have a usable total
			CurdOut("Attempting to retrieve total episodes from anime search results.")
			animeList, err := SearchAnime(string(userQuery), userCurdConfig.SubOrDub)
			if err != nil {
				CurdOut(fmt.Sprintf("Failed to retrieve anime list: %v", err))
			} else {
				for _, option := range animeList {
					optionProviderName, optionProviderID, ok := ParseProviderQualifiedID(option.Key)
					if !ok {
						optionProviderName = configuredProviderNames(userCurdConfig)[0]
						optionProviderID = option.Key
					}
					if optionProviderID == anime.ProviderId && optionProviderName == CurrentAnimeProviderName(anime) {
						// Extract total episodes from the label
						if matches := regexp.MustCompile(`\((\d+) episodes\)`).FindStringSubmatch(option.Label); len(matches) > 1 {
							anime.TotalEpisodes, _ = strconv.Atoi(matches[1])
							CurdOut(fmt.Sprintf("Retrieved total episodes: %d", anime.TotalEpisodes))
							break
						}
					}
				}
			}

			if anime.TotalEpisodes == 0 {
				// Not knowing the total is a reason to ask which episode to
				// start from, not a reason to insist on an answer: the tracker
				// knows the progress either way. Backing out carries on from
				// it, which is what the branch below does when the total is
				// known, rather than closing the program over a question.
				fromProgress := nextEpisodeFromProgress(selectedAnilistAnime.Progress)
				CurdOut("Still unable to determine total episodes.")
				episodeNumber, cancelled, err := promptEpisodeCancelable(userCurdConfig, "Episode",
					"Which episode do you want to start from?",
					fmt.Sprintf("a number · esc to continue from %d, your %s progress", fromProgress, RemoteTrackingDisplayName(userCurdConfig)))
				if err != nil {
					Log("Invalid episode input: " + err.Error())
					cancelled = true
				}
				if cancelled {
					episodeNumber = fromProgress
				}
				anime.Ep.Number = episodeNumber
			} else {
				anime.Ep.Number = nextEpisodeFromProgress(selectedAnilistAnime.Progress)
			}
		} else if anime.TotalEpisodes < anime.Ep.Number { // Handle weird cases
			Log(fmt.Sprintf("Weird case: anime.TotalEpisodes < anime.Ep.Number: %v < %v", anime.TotalEpisodes, anime.Ep.Number))
			// The question has an answer either way, so backing out of it is
			// the same as declining: start at the last episode. Only the rofi
			// half used to close the program over a failed read.
			answer, cancelled, err := promptCancelable(userCurdConfig, "Episode",
				"Start this anime from the beginning?",
				fmt.Sprintf("y or n · esc to start at episode %d", anime.TotalEpisodes))
			if err != nil {
				Log("Error getting user input: " + err.Error())
				cancelled = true
			}
			if !cancelled && isAffirmativeAnswer(answer) {
				anime.Ep.Number = 1
			} else {
				anime.Ep.Number = anime.TotalEpisodes
			}
		}

		// If we reached here successfully, break the loop
		break
	}
}

// CreateOrWriteTokenFile creates the token file if it doesn't exist and writes the token to it
func WriteTokenToFile(token string, filePath string) error {
	// Extract the directory path
	dir := filepath.Dir(filePath)

	// Create all necessary parent directories
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directories: %v", err)
	}

	// Write the token to the file, creating it if it doesn't exist
	err := os.WriteFile(filePath, []byte(token), 0644)
	if err != nil {
		return fmt.Errorf("failed to write token to file: %v", err)
	}

	return nil
}

func handleUnreleasedAnime(userCurdConfig *CurdConfig, user *User, anime *Anime, entry Entry) {
	title := entry.Media.Title.Romaji
	if userCurdConfig != nil && userCurdConfig.AnimeNameLanguage == "english" && entry.Media.Title.English != "" {
		title = entry.Media.Title.English
	}
	if title == "" {
		title = strconv.Itoa(entry.Media.ID)
	}

	CurdOut(fmt.Sprintf("%s is not released yet.", title))
	options := []SelectionOption{}
	if UsesRemoteTracking(userCurdConfig) {
		options = append(options, SelectionOption{Key: "planning", Label: "Add to Plan to Watch"})
	}
	options = append(options,
		SelectionOption{Key: "details", Label: "Open AniList details"},
		SelectionOption{Key: "back", Label: "Back to list"},
	)

	selected, err := promptSelect(options)
	if err != nil {
		Log(fmt.Sprintf("Error in unreleased anime prompt: %v", err))
		anime.Ep.ContinueLast = false
		return
	}

	switch selected.Key {
	case "planning":
		if err := AddAnimeToList(entry.Media.ID, "PLANNING", user.Token); err != nil {
			Log(fmt.Sprintf("Error adding unreleased anime to planning: %v", err))
			CurdOut("Failed to add to Plan to Watch.")
		} else {
			CurdOut("Added to Plan to Watch.")
			if err := RefreshUserAnimeList(userCurdConfig, user); err != nil {
				Log("Error refreshing anime list: " + err.Error())
			}
		}
	case "details":
		url := fmt.Sprintf("https://anilist.co/anime/%d", entry.Media.ID)
		CurdOut(fmt.Sprintf("Opening %s", url))
		if err := browser.OpenURL(url); err != nil {
			Log(fmt.Sprintf("Error opening browser: %v", err))
			CurdOut("Failed to open browser.")
		}
	case "-1":
		ExitCurd(nil)
	}
	anime.Ep.ContinueLast = false
}

func StartCurd(userCurdConfig *CurdConfig, anime *Anime) string {
	if err := resolveRuntimeProviderID(userCurdConfig, anime); err != nil {
		Log(fmt.Sprintf("Failed to resolve provider id: %v", err))
		CurdOut("Failed to resolve anime provider id: " + err.Error())
		exitWithRestore(1)
	}

	// Validate inputs
	if anime.ProviderId == "" {
		CurdOut("Error: No anime ID found")
		exitWithRestore(1)
	}
	if anime.Ep.Number <= 0 {
		CurdOut("Error: Invalid episode number")
		exitWithRestore(1)
	}

	// Reaching here for an episode that has not been broadcast means the user is
	// caught up. Searching every provider for it produces a failure that reads as
	// a broken tool, so say what is actually the case first. This is the same
	// answer the end-of-episode prompt gives; picking the show again from the
	// list a day later arrives by a different route and deserves the same one.
	if availability := nextEpisodeAiring(anime, anime.Ep.Number); !availability.Aired {
		if !confirmUnairedEpisode(userCurdConfig, anime.Ep.Number, availability) {
			RestoreScreen()
			return ""
		}
	}

	if (anime.Ep.NextEpisode.Number == anime.Ep.Number) && (len(anime.Ep.NextEpisode.Links) > 0) {
		anime.Ep.Links = anime.Ep.NextEpisode.Links
		anime.Ep.StreamReferrer = ""
		anime.Ep.SubtitleURL = ""
		if anime.Ep.NextEpisode.ProviderName != "" {
			anime.ProviderName = anime.Ep.NextEpisode.ProviderName
			anime.ProviderId = anime.Ep.NextEpisode.ProviderId
		}
	} else {
		// Preferred-first resolve; diagnosed recovery only after that fails.
		episodeResult, ok := resolveEpisodeLinksWithRecovery(userCurdConfig, anime, nil, true)
		if !ok {
			RestoreScreen()
			return ""
		}
		Log(fmt.Sprintf("Successfully retrieved %s/%s episode link. Links count: %d", episodeResult.ProviderName, episodeResult.Mode, len(episodeResult.Links)))
		anime.Ep.Links = episodeResult.Links
		anime.Ep.Mode = episodeResult.Mode
		applyStreamPlaybackHints(anime, anime.Ep.Links, episodeResult.LinkHints)
	}

	if len(anime.Ep.Links) == 0 {
		CurdOut("No episode links found")
		RestoreScreen()
		return ""
	}
	Log(fmt.Sprintf("Episode links validation passed. Found %d links", len(anime.Ep.Links)))

	// Modify the goroutine in main.go where next episode links are fetched
	// Get next episode link in parallel
	go func() {
		nextEpNum := anime.Ep.Number + 1
		if nextEpNum <= anime.TotalEpisodes {
			// Get next canon episode number if filler skip is enabled
			if userCurdConfig.SkipFiller && IsEpisodeFiller(anime.FillerEpisodes, anime.Ep.Number) {
				nextEpNum = GetNextCanonEpisode(anime.FillerEpisodes, nextEpNum)
			}
			nextEpisode := *anime
			nextEpisode.ProviderId = anime.ProviderId
			nextEpisode.ProviderName = anime.ProviderName
			nextResult, err := ResolveEpisodeURL(*userCurdConfig, &nextEpisode, nextEpNum)
			if err != nil {
				Log(fmt.Sprintf("Error getting next episode link for ep %d: %v", nextEpNum, err))
			} else {
				anime.Ep.NextEpisode = NextEpisode{
					Number:       nextEpNum,
					Links:        nextResult.Links,
					ProviderName: nextResult.ProviderName,
					ProviderId:   nextResult.ProviderID,
					Mode:         nextResult.Mode,
				}
			}
		} else {
			Log(fmt.Sprintf("Next episode %d exceeds total episodes %d, skipping prefetch", nextEpNum, anime.TotalEpisodes))
		}
	}()

	// Write anime.AnilistId to curd_id in the storage path
	idFilePath := filepath.Join(os.ExpandEnv(userCurdConfig.StoragePath), "curd_id")
	Log(fmt.Sprintf("idFilePath: %v", idFilePath))
	if err := os.MkdirAll(filepath.Dir(idFilePath), 0755); err != nil {
		Log(fmt.Sprintf("Failed to create directory for curd_id: %v", err))
	} else {
		if err := os.WriteFile(idFilePath, []byte(fmt.Sprintf("%d", anime.AnilistId)), 0644); err != nil {
			Log(fmt.Sprintf("Failed to write AnilistId to file: %v", err))
		}
	}

	// Display starting message with cover image and episode info
	if anime.CoverImage != "" && userCurdConfig.ImagePreview && userCurdConfig.RofiSelection {
		// Get the cached image path
		cacheDir := os.ExpandEnv("${HOME}/.cache/curd/images")
		filename := fmt.Sprintf("%x.jpg", md5.Sum([]byte(anime.CoverImage)))
		cachePath := filepath.Join(cacheDir, filename)

		// Display the image if it exists in cache
		_, err := os.Stat(cachePath)
		if err == nil {
			// File exists
			Log(fmt.Sprintf("Image found at %s", cachePath))
			CurdOut(fmt.Sprintf("-i %s \"%s - Episode %d\"", cachePath, GetAnimeName(*anime), anime.Ep.Number))
		} else {
			// File does not exist
			Log(fmt.Sprintf("Image does not exist at %s", cachePath))
			CurdOut(fmt.Sprintf("%s - Episode %d",
				GetAnimeName(*anime),
				anime.Ep.Number))

		}
	} else {
		CurdOut(fmt.Sprintf("%s - Episode %d", GetAnimeName(*anime), anime.Ep.Number))
	}
	title := fmt.Sprintf("%s - Episode %d", GetAnimeName(*anime), anime.Ep.Number)
	return StartVideoWithProviderFallback(userCurdConfig, anime, title)
}

func resolveRuntimeProviderID(userCurdConfig *CurdConfig, anime *Anime) error {
	if anime == nil || anime.ProviderId == "" {
		return nil
	}

	providerName, providerID := AnimeProviderID(anime)
	if providerName != "animepahe" || !ProviderEnabled("animepahe") {
		return nil
	}
	if !ProviderStackContains(userCurdConfig, "animepahe") {
		return nil
	}

	provider, err := ProviderByName(providerName)
	if err != nil {
		return err
	}

	query := GetAnimeName(*anime)
	if query == "" {
		query = anime.Title.Romaji
	}
	if query == "" {
		query = anime.Title.English
	}

	resolved, err := resolveProviderID(provider, providerID, query)
	if err != nil {
		return err
	}
	if resolved != "" && resolved != providerID {
		Log(fmt.Sprintf("Resolved Animepahe provider id %s to runtime id %s", providerID, resolved))
		anime.ProviderId = resolved
		anime.ProviderName = providerName
	}

	return nil
}

func reselectProviderAnime(userCurdConfig *CurdConfig, anime *Anime, reason error) bool {
	providerName, _ := AnimeProviderID(anime)
	if providerName != "animepahe" || !ProviderEnabled("animepahe") {
		return false
	}

	if reason != nil {
		Log(fmt.Sprintf("Attempting Animepahe provider reselect after error: %v", reason))
	}

	query := GetAnimeName(*anime)
	if query == "" {
		query = anime.Title.Romaji
	}
	if query == "" {
		query = anime.Title.English
	}
	if query == "" {
		return false
	}

	options, err := SearchAnime(query, userCurdConfig.SubOrDub)
	if err != nil {
		Log(fmt.Sprintf("Animepahe provider reselect search failed for %q: %v", query, err))
		return false
	}
	if len(options) == 0 {
		Log(fmt.Sprintf("Animepahe provider reselect found no results for %q", query))
		return false
	}

	CurdOut("The saved Animepahe mapping is stale. Please select the anime again.")
	var trackerEntry *Entry
	if anime.TotalEpisodes > 0 {
		trackerEntry = &Entry{Media: Media{Episodes: anime.TotalEpisodes}}
	}
	selected, err := promptProviderSearchSelection(userCurdConfig, options, trackerEntry)
	if err != nil || selected.Key == "-1" || selected.Key == "-2" || selected.Key == "" {
		if err != nil {
			Log(fmt.Sprintf("Animepahe provider reselect failed: %v", err))
		}
		return false
	}

	if selectedProviderName, rawProviderID, ok := ParseProviderQualifiedID(selected.Key); ok {
		anime.ProviderName = selectedProviderName
		anime.ProviderId = rawProviderID
	} else {
		anime.ProviderName = "animepahe"
		anime.ProviderId = selected.Key
	}
	anime.Ep.NextEpisode = NextEpisode{}
	Log(fmt.Sprintf("Updated Animepahe ProviderId to %s after stale mapping", anime.ProviderId))
	return true
}

func getEntriesByCategory(list AnimeList, category string) []Entry {
	switch category {
	case "ALL":
		// Combine all categories into one slice. Dedupe by Media.ID so Show All
		// never surfaces the same anime twice (e.g. stale cache / dual-track edge cases).
		allEntries := make([]Entry, 0)
		allEntries = append(allEntries, list.Watching...)
		allEntries = append(allEntries, list.Completed...)
		allEntries = append(allEntries, list.Paused...)
		allEntries = append(allEntries, list.Dropped...)
		allEntries = append(allEntries, list.Planning...)
		allEntries = append(allEntries, list.Rewatching...)
		return dedupeEntriesByMediaID(allEntries)
	case "CURRENT":
		currentEntries := make([]Entry, 0, len(list.Watching)+len(list.Rewatching))
		currentEntries = append(currentEntries, list.Watching...)
		currentEntries = append(currentEntries, list.Rewatching...)
		return dedupeEntriesByMediaID(currentEntries)
	case "COMPLETED":
		return dedupeEntriesByMediaID(list.Completed)
	case "PAUSED":
		return dedupeEntriesByMediaID(list.Paused)
	case "DROPPED":
		return dedupeEntriesByMediaID(list.Dropped)
	case "PLANNING":
		return dedupeEntriesByMediaID(list.Planning)
	case "REWATCHING": // Added for completeness, though "ALL" covers it.
		return dedupeEntriesByMediaID(list.Rewatching)
	default:
		return []Entry{}
	}
}

func NextEpisodePromptCLI(userCurdConfig *CurdConfig) bool {
	anime := GetGlobalAnime()

	// Show the next episode number that will be started
	nextEpisodeNum := anime.Ep.Number + 1
	isLastKnownEpisode := anime.TotalEpisodes > 0 && anime.Ep.Number >= anime.TotalEpisodes

	// Same reasoning as the rofi prompt: do not offer an episode that has not
	// been broadcast, since choosing it can only fail.
	if !isLastKnownEpisode {
		if availability := nextEpisodeAiring(anime, nextEpisodeNum); !availability.Aired {
			return confirmUnairedEpisode(userCurdConfig, nextEpisodeNum, availability)
		}
	}

	if isLastKnownEpisode {
		CurdOut("Finish this series?")
	} else {
		CurdOut(fmt.Sprintf("Start next episode (%d)?", nextEpisodeNum))
	}

	// Create options for the selection - no "quit" option since it's built into selection menu
	label := fmt.Sprintf("Yes, continue to episode %d", nextEpisodeNum)
	if isLastKnownEpisode {
		label = "Yes, finish series"
	}
	options := []SelectionOption{
		{Key: "yes", Label: label},
	}

	// Use DynamicSelect for CLI mode
	selectedOption, err := DynamicSelect(options)
	if err != nil {
		Log(fmt.Sprintf("Error in CLI next episode prompt selection: %v", err))
		return false
	}

	Log(fmt.Sprintf("CLI User Selected Key: '%s', Label: '%s'", selectedOption.Key, selectedOption.Label))

	if selectedOption.Key == "-1" || selectedOption.Key == "-2" {
		// User selected to quit/back via the built-in option
		CurdOut("Exiting")
		return false
	}

	return selectedOption.Key == "yes"
}

// NextEpisodePromptContinuous provides a continuous next episode prompt for CLI mode
// This runs throughout the episode duration and handles completion logic
func NextEpisodePromptContinuous(userCurdConfig *CurdConfig, databaseFile string, userToken string) {
	anime := GetGlobalAnime()

	for {
		// Check if episode has started
		if !anime.Ep.Started {
			time.Sleep(1 * time.Second)
			continue
		}

		// Show the next episode number that will be started
		nextEpisodeNum := anime.Ep.Number + 1
		isLastKnownEpisode := anime.TotalEpisodes > 0 && anime.Ep.Number >= anime.TotalEpisodes
		if isLastKnownEpisode {
			CurdOut("Finish this series or quit?")
		} else {
			CurdOut(fmt.Sprintf("Continue to next episode (%d) or quit?", nextEpisodeNum))
		}

		// Create options for the selection - no "quit" option since it's built into selection menu
		label := "Yes, start next episode now"
		if isLastKnownEpisode {
			label = "Yes, finish series"
		}
		options := []SelectionOption{
			{Key: "yes", Label: label},
		}

		// Use DynamicSelect for CLI mode
		selectedOption, err := DynamicSelect(options)
		if err != nil {
			Log(fmt.Sprintf("Error in CLI continuous next episode prompt: %v", err))
			break
		}

		Log(fmt.Sprintf("CLI Continuous User Selected Key: '%s', Label: '%s'", selectedOption.Key, selectedOption.Label))

		if selectedOption.Key == "-1" || selectedOption.Key == "-2" {
			// User selected to quit/back via the built-in option

			// Check completion percentage
			percentageWatched := PercentageWatched(anime.Ep.Player.PlaybackTime, anime.Ep.Duration)

			if int(percentageWatched) >= userCurdConfig.PercentageToMarkComplete {
				// Episode is considered completed, mark it and update progress
				anime.Ep.IsCompleted = true

				// Handle completion if this was the last episode
				if anime.Ep.Number == anime.TotalEpisodes {
					HandleLastEpisodeCompletion(userCurdConfig, anime, userToken)
				}

				// Update local database
				if updateErr := LocalUpdateAnime(databaseFile, anime.AnilistId, anime.ProviderId, anime.Ep.Number, anime.Ep.Player.PlaybackTime, ConvertSecondsToMinutes(anime.Ep.Duration), GetAnimeName(*anime), CurrentAnimeProviderName(anime)); updateErr != nil {
					Log("Error updating local database on quit: " + updateErr.Error())
				}

				if !(anime.TotalEpisodes > 0 && anime.Ep.Number == anime.TotalEpisodes && anime.Rewatching) {
					completedEpisode := anime.Ep.Number
					go func(progressEpisode int) {
						if progressErr := UpdateAnimeProgress(userToken, anime.AnilistId, progressEpisode); progressErr != nil {
							Log("Error updating Anilist progress on quit: " + progressErr.Error())
						}
					}(completedEpisode)
				}

				CurdOut(fmt.Sprintf("Episode completed (%.1f%% watched). Exiting.", percentageWatched))
			} else {
				CurdOut(fmt.Sprintf("Episode not completed (%.1f%% watched). Exiting.", percentageWatched))
			}

			ExitMPV(anime.Ep.Player.SocketPath)
			ExitCurd(nil)
			return
		}

		if selectedOption.Key == "yes" {
			// User wants to start next episode immediately
			anime.Ep.IsCompleted = true
			socketPath := anime.Ep.Player.SocketPath
			if anime.TotalEpisodes > 0 && anime.Ep.Number >= anime.TotalEpisodes {
				ExitMPV(socketPath)
			}
			StartNextEpisode(anime, userCurdConfig, databaseFile, userToken)
			ExitMPV(socketPath)
			return // Exit this function, let the main loop handle next episode
		}
	}
}

// Simple next episode prompt for Rofi mode - just asks if user wants to continue
func NextEpisodePromptRofi(userCurdConfig *CurdConfig) bool {
	anime := GetGlobalAnime()

	// Show the next episode number that will be started
	nextEpisodeNum := anime.Ep.Number + 1
	isLastKnownEpisode := anime.TotalEpisodes > 0 && anime.Ep.Number >= anime.TotalEpisodes

	// Say so rather than offering an episode that cannot be played: picking it
	// would search every provider and fail, which reads as a broken tool rather
	// than an episode that has not been broadcast.
	if !isLastKnownEpisode {
		if availability := nextEpisodeAiring(anime, nextEpisodeNum); !availability.Aired {
			return confirmUnairedEpisode(userCurdConfig, nextEpisodeNum, availability)
		}
	}

	// Create options for the selection
	label := fmt.Sprintf("Yes, start episode %d", nextEpisodeNum)
	if isLastKnownEpisode {
		label = "Yes, finish series"
	}
	options := []SelectionOption{
		{Key: "yes", Label: label},
	}

	// Use DynamicSelect for Rofi mode
	selectedOption, err := DynamicSelect(options)
	if err != nil {
		Log(fmt.Sprintf("Error in next episode prompt selection: %v", err))
		return false
	}

	Log(fmt.Sprintf("Rofi User Selected Key: '%s', Label: '%s'", selectedOption.Key, selectedOption.Label))

	return selectedOption.Key == "yes"
}

// StartNextEpisode handles the logic for starting the next episode
// It updates the episode number, resets necessary flags, and handles database updates
func StartNextEpisode(anime *Anime, userCurdConfig *CurdConfig, databaseFile string, userToken string) {
	// Save previous episode number for progress update
	prevEpisode := anime.Ep.Number

	// Check if we just completed the last episode
	if anime.TotalEpisodes > 0 && anime.Ep.Number == anime.TotalEpisodes {
		// Handle scoring and completion for the last episode
		HandleLastEpisodeCompletion(userCurdConfig, anime, userToken)

		if !anime.Rewatching {
			err := UpdateAnimeProgress(userToken, anime.AnilistId, prevEpisode)
			if err != nil {
				Log("Error updating Anilist progress: " + err.Error())
			}
		}
		// Note: UpdateAnimeProgress already outputs a message on success

		CurdOut("Series completed!")
		ExitCurd(nil)
		return
	}

	// Increment episode number
	anime.Ep.Number++

	// Check if we've reached the end of the series
	if anime.TotalEpisodes > 0 && anime.Ep.Number > anime.TotalEpisodes {
		CurdOut("Reached end of series")
		ExitCurd(nil)
		return
	}

	// Use prefetched links if available for the next episode
	if (anime.Ep.NextEpisode.Number == anime.Ep.Number) && (len(anime.Ep.NextEpisode.Links) > 0) {
		anime.Ep.Links = anime.Ep.NextEpisode.Links
		anime.Ep.StreamReferrer = ""
		anime.Ep.SubtitleURL = ""
		if anime.Ep.NextEpisode.ProviderName != "" {
			anime.ProviderName = anime.Ep.NextEpisode.ProviderName
			anime.ProviderId = anime.Ep.NextEpisode.ProviderId
		}
		Log(fmt.Sprintf("Using prefetched links for episode %d", anime.Ep.Number))
	} else {
		// Clear links to force fetching new ones
		anime.Ep.Links = []string{}
		Log(fmt.Sprintf("No prefetched links available for episode %d, will fetch new ones", anime.Ep.Number))
	}

	// Reset episode flags
	anime.Ep.Started = false
	anime.Ep.IsCompleted = false

	// Log the transition
	Log("Completed episode, starting next.")

	// Update local database
	err := LocalUpdateAnime(databaseFile, anime.AnilistId, anime.ProviderId, anime.Ep.Number, 0, 0, GetAnimeName(*anime), CurrentAnimeProviderName(anime))
	if err != nil {
		Log("Error updating local database: " + err.Error())
	}

	go func() {
		if progressErr := UpdateAnimeProgress(userToken, anime.AnilistId, prevEpisode); progressErr != nil {
			Log("Error updating Anilist progress: " + progressErr.Error())
		}
	}()

	// Output message to user
	CurdOut(fmt.Sprint("Starting next episode: ", anime.Ep.Number))
}

// HandleLastEpisodeCompletion handles scoring and completion for the last episode
func HandleLastEpisodeCompletion(userCurdConfig *CurdConfig, anime *Anime, userToken string) {
	if anime.TotalEpisodes <= 0 || anime.Ep.Number != anime.TotalEpisodes {
		return
	}

	summary := []string{}
	canWriteRemote := ShouldWriteRemoteTracking(userCurdConfig, anime)

	if userCurdConfig.ScoreOnCompletion && !anime.IsAiring && canWriteRemote {
		CurdOut("You've completed this anime! Would you like to rate it?")

		scoreOptions := []SelectionOption{
			{Key: "yes", Label: "Yes, rate this anime"},
			{Key: "no", Label: "No, skip rating"},
		}

		selectedOption, err := DynamicSelect(scoreOptions)
		if err != nil {
			Log(fmt.Sprintf("Error in score prompt selection: %v", err))
		} else if selectedOption.Key == "yes" {
			err = RateAnime(userToken, anime.AnilistId)
			if err != nil {
				Log(fmt.Sprintf("Error rating anime: %v", err))
				CurdOut("Failed to rate anime")
				summary = append(summary, "rating failed")
			} else {
				CurdOut("Anime rated successfully!")
				summary = append(summary, "rating saved")
			}
		} else {
			summary = append(summary, "rating skipped")
		}
		// Back (-2) and no are treated as skip
	} else {
		summary = append(summary, "rating skipped")
	}

	if !anime.IsAiring && canWriteRemote {
		if anime.Rewatching {
			err := CompleteAnimeRewatch(userToken, *anime)
			if err != nil {
				Log("Error completing anime rewatch: " + err.Error())
				summary = append(summary, "rewatch completion failed")
			} else {
				summary = append(summary, "rewatch completed")
			}
		} else {
			err := UpdateAnimeStatus(userToken, anime.AnilistId, "COMPLETED")
			if err != nil {
				Log("Error updating anime status to completed: " + err.Error())
				summary = append(summary, "status update failed")
			} else {
				summary = append(summary, "marked completed")
			}
		}
	} else if !anime.IsAiring {
		summary = append(summary, "tracker updates skipped")
	}

	if sequelSummary := handleSequelCheck(userCurdConfig, anime, userToken); sequelSummary != "" {
		summary = append(summary, sequelSummary)
	}
	if len(summary) > 0 {
		CurdOut("Completion summary: " + strings.Join(summary, "; "))
	}
}

// handleSequelCheck checks for sequels and prompts the user accordingly
func handleSequelCheck(userCurdConfig *CurdConfig, anime *Anime, userToken string) (summary string) {
	if !ShouldWriteRemoteTracking(userCurdConfig, anime) {
		return "sequel check skipped"
	}

	// Recover from any panics in this function to prevent crashes
	defer func() {
		if r := recover(); r != nil {
			Log(fmt.Sprintf("Recovered from panic in handleSequelCheck: %v", r))
			summary = "sequel check failed"
		}
	}()

	// Fetch sequel information
	sequels, err := GetAnimeSequel(anime.AnilistId, userToken)
	if err != nil {
		Log(fmt.Sprintf("Error fetching sequel information: %v", err))
		return "sequel check failed"
	}

	if len(sequels) == 0 {
		Log("No sequel found for this anime")
		return "no sequel found"
	}

	sequel := &sequels[0]
	if len(sequels) > 1 {
		selectedSequel, ok := selectSequel(userCurdConfig, sequels)
		if !ok {
			return "sequel skipped"
		}
		sequel = selectedSequel
	}

	return promptSequelAction(userCurdConfig, sequel, userToken)
}

func selectSequel(userCurdConfig *CurdConfig, sequels []SequelInfo) (*SequelInfo, bool) {
	CurdOut("Multiple sequels found.")
	options := make([]SelectionOption, 0, len(sequels)+1)
	for i := range sequels {
		title := sequelDisplayTitle(userCurdConfig, &sequels[i])
		status := strings.ToLower(strings.ReplaceAll(sequels[i].Status, "_", " "))
		options = append(options, SelectionOption{
			Key:   strconv.Itoa(i),
			Label: fmt.Sprintf("%s (%s)", title, status),
		})
	}
	options = append(options, SelectionOption{Key: "skip", Label: "Skip"})

	selected, err := promptSelect(options)
	if err != nil {
		Log(fmt.Sprintf("Error selecting sequel: %v", err))
		return nil, false
	}
	if selected.Key == "skip" || selected.Key == "-1" || selected.Key == "-2" {
		return nil, false
	}
	index, err := strconv.Atoi(selected.Key)
	if err != nil || index < 0 || index >= len(sequels) {
		return nil, false
	}
	return &sequels[index], true
}

func sequelDisplayTitle(userCurdConfig *CurdConfig, sequel *SequelInfo) string {
	if sequel == nil {
		return ""
	}
	if userCurdConfig != nil && userCurdConfig.AnimeNameLanguage == "english" && sequel.Title.English != "" {
		return sequel.Title.English
	}
	if sequel.Title.Romaji != "" {
		return sequel.Title.Romaji
	}
	return strconv.Itoa(sequel.ID)
}

func promptSequelAction(userCurdConfig *CurdConfig, sequel *SequelInfo, userToken string) string {
	if sequel == nil {
		return ""
	}

	sequelTitle := sequelDisplayTitle(userCurdConfig, sequel)
	Log(fmt.Sprintf("Found sequel: %s (ID: %d)", sequelTitle, sequel.ID))

	currentUser := GetGlobalUser()
	userAnimeList := AnimeList{}
	if currentUser != nil {
		userAnimeList = currentUser.AnimeList
	}
	sequelStatus, isInList := "", false
	if hasAnyEntries(userAnimeList) {
		sequelStatus, isInList = FindSequelInAnimeList(userAnimeList, sequel.ID)
	}

	if isInList {
		CurdOut(fmt.Sprintf("Sequel found: %s (%s)", sequelTitle, sequelStatus))
	} else {
		CurdOut(fmt.Sprintf("Sequel found: %s", sequelTitle))
	}

	options := []SelectionOption{}
	if sequel.Status != "NOT_YET_RELEASED" {
		if !isInList || sequelStatus == "PLANNING" || sequelStatus == "PAUSED" || sequelStatus == "DROPPED" {
			options = append(options, SelectionOption{Key: "watching", Label: "Add to Watching"})
		}
	}
	if !isInList || sequelStatus != "PLANNING" {
		options = append(options, SelectionOption{Key: "planning", Label: "Add to Plan to Watch"})
	}
	options = append(options,
		SelectionOption{Key: "details", Label: "Open AniList details"},
		SelectionOption{Key: "skip", Label: "Skip"},
	)

	selected, err := promptSelect(options)
	if err != nil {
		Log(fmt.Sprintf("Error in sequel action prompt: %v", err))
		return "sequel action failed"
	}

	switch selected.Key {
	case "watching":
		if err := AddAnimeToList(sequel.ID, "CURRENT", userToken); err != nil {
			Log(fmt.Sprintf("Error adding sequel to watching list: %v", err))
			CurdOut("Failed to add sequel to Watching.")
			return "sequel add failed"
		} else {
			CurdOut(fmt.Sprintf("Added '%s' to Watching.", sequelTitle))
			return "sequel added to Watching"
		}
	case "planning":
		if err := AddAnimeToList(sequel.ID, "PLANNING", userToken); err != nil {
			Log(fmt.Sprintf("Error adding sequel to planning list: %v", err))
			CurdOut("Failed to add sequel to Plan to Watch.")
			return "sequel add failed"
		} else {
			CurdOut(fmt.Sprintf("Added '%s' to Plan to Watch.", sequelTitle))
			return "sequel added to Plan to Watch"
		}
	case "details":
		url := sequel.SiteURL
		if url == "" {
			url = fmt.Sprintf("https://anilist.co/anime/%d", sequel.ID)
		}
		CurdOut(fmt.Sprintf("Opening %s", url))
		if err := browser.OpenURL(url); err != nil {
			Log(fmt.Sprintf("Error opening browser: %v", err))
			CurdOut("Failed to open browser.")
			return "sequel details failed"
		}
		return "sequel details opened"
	case "skip", "-1", "-2":
		Log("User skipped sequel action")
	}
	return "sequel skipped"
}

// ChangeProvider allows the user to switch the anime provider
func ChangeProvider(userCurdConfig *CurdConfig) {
	options := providerSelectionOptions()
	if len(options) == 0 {
		CurdOut("\nNo providers are currently enabled.\n")
		return
	}

	selected, err := DynamicSelect(options)
	if err != nil || selected.Key == "-1" || selected.Key == "-2" {
		return
	}

	// Update the config
	providerValue := canonicalProviderConfigValue(selected.Key)
	userCurdConfig.Provider = providerValue
	CurrentProvider = nil // reset the provider instance

	// Save to config file
	configPath := GlobalConfigPath
	configMap, err := LoadConfigFromFile(configPath)
	if err == nil {
		configMap["Provider"] = providerValue
		SaveConfigToFile(configPath, configMap)
	}

	CurdOut(fmt.Sprintf("\nProvider successfully changed to %s.\n", providerConfigDisplayLabel(userCurdConfig.Provider)))
	time.Sleep(1 * time.Second)
}

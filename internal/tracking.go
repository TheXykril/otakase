package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	TrackingRemoteNone        = "none"
	TrackingRemoteAniList     = "anilist"
	TrackingRemoteMyAnimeList = "myanimelist"
	TrackingRemoteBoth        = "anilist+myanimelist"

	// remoteTrackerWriteDelay paces writes to either tracker. Both are rate
	// limited, and a first dual sync can run to hundreds of entries.
	remoteTrackerWriteDelay = 350 * time.Millisecond
)

func UsesLocalTracking(config *Config) bool {
	return true
}

func UsesRemoteTracking(config *Config) bool {
	if config == nil {
		return true
	}
	return normalizeRemoteTracker(config.TrackingRemote) != TrackingRemoteNone
}

func ShouldWriteRemoteTracking(config *Config, anime *Anime) bool {
	if !UsesRemoteTracking(config) {
		return false
	}
	return anime == nil || !anime.SkipRemoteSync
}

func UsesAniListTracking(config *Config) bool {
	if config == nil {
		return true
	}
	switch normalizeRemoteTracker(config.TrackingRemote) {
	case TrackingRemoteAniList, TrackingRemoteBoth:
		return true
	default:
		return false
	}
}

func UsesMyAnimeListTracking(config *Config) bool {
	if config == nil {
		return false
	}
	switch normalizeRemoteTracker(config.TrackingRemote) {
	case TrackingRemoteMyAnimeList, TrackingRemoteBoth:
		return true
	default:
		return false
	}
}

func UsesDualRemoteTracking(config *Config) bool {
	if config == nil {
		return false
	}
	return normalizeRemoteTracker(config.TrackingRemote) == TrackingRemoteBoth
}

func trackingCategoryEnabled(config *Config, key string) bool {
	switch key {
	case "CURRENT", "ALL", "UNTRACKED", "CONTINUE_LAST", "REMAP_PROVIDER", "PROVIDER":
		return true
	case "UPDATE", "PLANNING", "COMPLETED", "PAUSED", "DROPPED", "REWATCHING":
		return UsesRemoteTracking(config)
	default:
		return true
	}
}

func EnsureTrackingConfigured(config *Config) error {
	if config == nil {
		return fmt.Errorf("missing config")
	}

	normalizeTrackingConfig(config)
	if config.TrackingConfigured {
		// NOTE: persistTrackingConfig is only needed when tracker is changed not every startup
		return nil
	}

	options := []SelectionOption{
		{Key: "local", Label: "local"},
		{Key: "anilist", Label: "anilist"},
		{Key: "myanimelist", Label: "myanimelist"},
		{Key: "anilist+myanimelist", Label: "anilist + myanimelist"},
	}

	Out("Choose tracking mode. Local history stays enabled in every mode.")
	selected, err := DynamicSelect(options)
	if err != nil {
		return err
	}

	switch selected.Key {
	case "local":
		config.TrackingLocal = true
		config.TrackingRemote = TrackingRemoteNone
	case "anilist":
		config.TrackingLocal = true
		config.TrackingRemote = TrackingRemoteAniList
	case "myanimelist":
		config.TrackingLocal = true
		config.TrackingRemote = TrackingRemoteMyAnimeList
	case "anilist+myanimelist":
		config.TrackingLocal = true
		config.TrackingRemote = TrackingRemoteBoth
	case "-1", "-2":
		return fmt.Errorf("tracking setup cancelled")
	default:
		return fmt.Errorf("unsupported tracking selection: %s", selected.Key)
	}

	config.TrackingConfigured = true
	normalizeTrackingConfig(config)
	return persistTrackingConfig(config)
}

func persistTrackingConfig(config *Config) error {
	if config == nil || GlobalConfigPath == "" {
		return nil
	}

	configMap, err := LoadConfigFromFile(GlobalConfigPath)
	if err != nil {
		return err
	}

	configMap["TrackingLocal"] = strconv.FormatBool(config.TrackingLocal)
	configMap["TrackingRemote"] = config.TrackingRemote
	configMap["TrackingConfigured"] = strconv.FormatBool(config.TrackingConfigured)
	configMap["MyAnimeListClientID"] = config.MyAnimeListClientID
	configMap["MyAnimeListClientSecret"] = config.MyAnimeListClientSecret
	configMap["MyAnimeListImported"] = strconv.FormatBool(config.MyAnimeListImported)
	configMap["MyAnimeListImportDismissed"] = strconv.FormatBool(config.MyAnimeListImportDismissed)

	return SaveConfigToFile(GlobalConfigPath, configMap)
}

// promptTrackingInput asks for one of the things setting up a tracker needs: a
// client id, a secret, a pasted callback URL.
//
// Each used to be read as a bare line from a reader built for that one read,
// which threw away anything typed past it -- so pasting the id and the secret
// together lost the secret. Where an empty answer already meant something
// ("optional", "start over"), backing out means that; where it did not, it is
// the same refusal it always was.
func promptTrackingInput(prompt string, allowEmpty bool) (string, error) {
	hint := "esc to go back"
	if allowEmpty {
		hint = "leave empty to skip · esc to skip"
	}

	value, cancelled, err := promptCancelable(GetGlobalConfig(), "Tracker", prompt, hint)
	if err != nil {
		return "", err
	}
	if cancelled {
		if allowEmpty {
			return "", nil
		}
		return "", fmt.Errorf("input required")
	}
	return value, nil
}

// promptAnimeScoreValue asks for a score out of ten, and reports backing out
// of the question rather than treating it as a score.
//
// Not rating something is the ordinary answer to being asked -- the question
// arrives unbidden after an episode finishes -- and it used to close the
// program: an empty line failed to parse, and every caller passed that error up
// to a caller that quit on it.
func promptAnimeScoreValue() (float64, bool, error) {
	return valueFromAnswers(parseAnimeScore, func() (string, bool, error) {
		return promptCancelable(GetGlobalConfig(), "Score", "Rate this anime", "0 to 10 · esc to skip")
	})
}

func parseAnimeScore(raw string) (float64, error) {
	score, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid score: %w", err)
	}
	if score < 0 || score > 10 {
		return 0, fmt.Errorf("score must be between 0 and 10")
	}
	return score, nil
}

func ensureMyAnimeListCredentialsConfigured(config *Config) error {
	clientID, _ := myAnimeListClientCredentials(config)
	if clientID != "" {
		return nil
	}

	Out("MyAnimeList sync needs MAL application credentials.")
	clientIDInput, err := promptTrackingInput("Enter your MyAnimeList client ID", false)
	if err != nil {
		return err
	}
	clientSecretInput, err := promptTrackingInput("Enter your MyAnimeList client secret (optional)", true)
	if err != nil {
		return err
	}

	config.MyAnimeListClientID = clientIDInput
	config.MyAnimeListClientSecret = clientSecretInput
	return persistTrackingConfig(config)
}

func ensureAniListTrackerReady(config *Config, user *User) error {
	tokenPath := filepath.Join(os.ExpandEnv(config.StoragePath), "anilist_token.json")
	if token, err := GetTokenFromFile(tokenPath); err == nil && strings.TrimSpace(token) != "" {
		if user != nil && user.Token == "" {
			user.Token = token
		}
		return nil
	}

	ChangeToken(config, user)
	return nil
}

func ensureMyAnimeListTrackerReady(config *Config, user *User) error {
	if err := ensureMyAnimeListCredentialsConfigured(config); err != nil {
		return err
	}
	if token, err := GetMyAnimeListAccessToken(config); err == nil && strings.TrimSpace(token) != "" {
		return nil
	}
	return ChangeMyAnimeListToken(config, user)
}

func EnsureConfiguredTrackersReady(config *Config, user *User) error {
	if config == nil {
		return fmt.Errorf("missing config")
	}

	if UsesAniListTracking(config) {
		if err := ensureAniListTrackerReady(config, user); err != nil {
			return err
		}
	}
	if UsesMyAnimeListTracking(config) {
		if err := ensureMyAnimeListTrackerReady(config, user); err != nil {
			return err
		}
	}

	return LoadTokenForConfiguredTracking(config, user)
}

func localAnimeListFromHistory(history []Anime) AnimeList {
	list := AnimeList{}
	for _, anime := range history {
		title := anime.Title
		if title.English == "" && title.Romaji == "" {
			title.English = strconv.Itoa(anime.AnilistId)
			title.Romaji = title.English
		}

		progress := 0
		if anime.Ep.Number > 0 {
			progress = anime.Ep.Number - 1
		}

		entry := Entry{
			Media: Media{
				ID:       anime.AnilistId,
				MalID:    anime.MalId,
				Title:    title,
				Episodes: anime.TotalEpisodes,
			},
			Progress: progress,
			Status:   "CURRENT",
		}
		list.Watching = append(list.Watching, entry)
	}
	return list
}

func nextEpisodeFromProgress(progress int) int {
	if progress < 0 {
		return 1
	}
	return progress + 1
}

func BuildLocalAnimeList(storagePath string) AnimeList {
	historyPath := filepath.Join(os.ExpandEnv(storagePath), "curd_history.txt")
	return localAnimeListFromHistory(LocalGetAllAnime(historyPath))
}

func hasAnyEntries(list AnimeList) bool {
	return len(list.Watching)+len(list.Completed)+len(list.Paused)+len(list.Dropped)+len(list.Planning)+len(list.Rewatching) > 0
}

func animeListEntryCount(list AnimeList) int {
	return len(getEntriesByCategory(list, "ALL"))
}

func countEntriesMissingMyAnimeListID(list AnimeList) int {
	count := 0
	for _, entry := range getEntriesByCategory(list, "ALL") {
		if entry.Media.MalID == 0 {
			count++
		}
	}
	return count
}

func countAniListDeletes(list AnimeList) int {
	count := 0
	for _, entry := range getEntriesByCategory(list, "ALL") {
		if entry.ListID != 0 {
			count++
		}
	}
	return count
}

type remoteSyncPreview struct {
	Action             string
	AniListEntries     int
	MyAnimeListEntries int
	Writes             int
	Deletes            int
	MissingMALIDs      int
}

func confirmRemoteSync(preview remoteSyncPreview) (bool, error) {
	if os.Getenv("CURD_TEST_AUTO_CONFIRM_REMOTE_SYNC") == "1" {
		return true, nil
	}

	Out(fmt.Sprintf("%s: AniList %d, MyAnimeList %d.", preview.Action, preview.AniListEntries, preview.MyAnimeListEntries))
	if preview.Deletes > 0 {
		Out(fmt.Sprintf("This will write %d entries and delete %d entries.", preview.Writes, preview.Deletes))
	} else {
		Out(fmt.Sprintf("This will write %d entries.", preview.Writes))
	}
	if preview.MissingMALIDs > 0 {
		Out(fmt.Sprintf("%d entries need a MyAnimeList ID lookup.", preview.MissingMALIDs))
	}

	selected, err := promptSelect([]SelectionOption{
		{Key: "continue", Label: "Continue"},
		{Key: "cancel", Label: "Cancel"},
	})
	if err != nil {
		return false, err
	}
	return selected.Key == "continue", nil
}

func writeTrackingBackup(config *Config, action string, aniList, myAnimeList AnimeList) (string, error) {
	if config == nil {
		return "", fmt.Errorf("missing config")
	}

	backupDir := filepath.Join(os.ExpandEnv(config.StoragePath), "tracking-backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", err
	}

	slug := strings.NewReplacer(" ", "-", "+", "-", ":", "", "/", "-").Replace(strings.ToLower(action))
	backupPath := filepath.Join(backupDir, fmt.Sprintf("%s-%s.json", time.Now().Format("20060102-150405"), slug))
	payload := struct {
		Action      string    `json:"action"`
		CreatedAt   time.Time `json:"created_at"`
		AniList     AnimeList `json:"anilist"`
		MyAnimeList AnimeList `json:"myanimelist"`
	}{
		Action:      action,
		CreatedAt:   time.Now(),
		AniList:     aniList,
		MyAnimeList: myAnimeList,
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(backupPath, data, 0o644); err != nil {
		return "", err
	}
	return backupPath, nil
}

func currentFuzzyDate() FuzzyDate {
	now := time.Now()
	return FuzzyDate{
		Year:  now.Year(),
		Month: int(now.Month()),
		Day:   now.Day(),
	}
}

func entryFreshnessScore(entry Entry) int {
	score := entry.Progress * 100
	switch entry.Status {
	case "REPEATING":
		score += 60
	case "CURRENT":
		score += 50
	case "COMPLETED":
		score += 40
	case "PAUSED":
		score += 30
	case "PLANNING":
		score += 20
	case "DROPPED":
		score += 10
	}
	if entry.Score > 0 {
		score += int(entry.Score * 10)
	}
	if entry.Repeat > 0 {
		score += entry.Repeat
	}
	if entry.CoverImage != "" {
		score++
	}
	return score
}

func mergeEntryMetadata(preferred, fallback Entry) Entry {
	if preferred.ListID == 0 {
		preferred.ListID = fallback.ListID
	}
	if preferred.Media.ID == 0 {
		preferred.Media.ID = fallback.Media.ID
	}
	if preferred.Media.MalID == 0 {
		preferred.Media.MalID = fallback.Media.MalID
	}
	if preferred.Media.Duration == 0 {
		preferred.Media.Duration = fallback.Media.Duration
	}
	if preferred.Media.Episodes == 0 {
		preferred.Media.Episodes = fallback.Media.Episodes
	}
	if preferred.Media.Title.English == "" {
		preferred.Media.Title.English = fallback.Media.Title.English
	}
	if preferred.Media.Title.Romaji == "" {
		preferred.Media.Title.Romaji = fallback.Media.Title.Romaji
	}
	if preferred.Media.Title.Japanese == "" {
		preferred.Media.Title.Japanese = fallback.Media.Title.Japanese
	}
	if preferred.Media.Status == "" {
		preferred.Media.Status = fallback.Media.Status
	}
	// Only AniList reports a broadcast schedule, so a merge that takes the
	// MyAnimeList entry -- which is what happens right after otakase pushes progress
	// there, making it the more recently updated of the two -- silently loses it.
	// Everything downstream then behaves as though the show's schedule were
	// unknown: no airing countdown in the list, and no way to tell "you are
	// caught up" apart from "this episode could not be found".
	if preferred.Media.NextAiringEpisode == nil {
		preferred.Media.NextAiringEpisode = fallback.Media.NextAiringEpisode
	}
	if preferred.Media.Format == "" {
		preferred.Media.Format = fallback.Media.Format
	}
	if preferred.CoverImage == "" {
		preferred.CoverImage = fallback.CoverImage
	}
	if preferred.Repeat == 0 && fallback.Repeat > 0 {
		preferred.Repeat = fallback.Repeat
	}
	return preferred
}

func mergeAnimeEntries(existing, incoming Entry) Entry {
	switch {
	case existing.UpdatedAt.IsZero() && !incoming.UpdatedAt.IsZero():
		return mergeEntryMetadata(incoming, existing)
	case !existing.UpdatedAt.IsZero() && incoming.UpdatedAt.IsZero():
		return mergeEntryMetadata(existing, incoming)
	case existing.UpdatedAt.After(incoming.UpdatedAt):
		return mergeEntryMetadata(existing, incoming)
	case incoming.UpdatedAt.After(existing.UpdatedAt):
		return mergeEntryMetadata(incoming, existing)
	case entryFreshnessScore(incoming) > entryFreshnessScore(existing):
		return mergeEntryMetadata(incoming, existing)
	default:
		return mergeEntryMetadata(existing, incoming)
	}
}

func mergeAnimeLists(primary, secondary AnimeList) AnimeList {
	merged := make(map[int]Entry)
	for _, entry := range getEntriesByCategory(primary, "ALL") {
		if entry.Media.ID == 0 {
			continue
		}
		merged[entry.Media.ID] = entry
	}
	for _, entry := range getEntriesByCategory(secondary, "ALL") {
		if entry.Media.ID == 0 {
			continue
		}
		if existing, ok := merged[entry.Media.ID]; ok {
			merged[entry.Media.ID] = mergeAnimeEntries(existing, entry)
			continue
		}
		merged[entry.Media.ID] = entry
	}

	result := AnimeList{}
	for _, entry := range merged {
		switch entry.Status {
		case "CURRENT":
			result.Watching = append(result.Watching, entry)
		case "COMPLETED":
			result.Completed = append(result.Completed, entry)
		case "PAUSED":
			result.Paused = append(result.Paused, entry)
		case "DROPPED":
			result.Dropped = append(result.Dropped, entry)
		case "PLANNING":
			result.Planning = append(result.Planning, entry)
		case "REPEATING":
			result.Rewatching = append(result.Rewatching, entry)
		}
	}
	return result
}

func mapEntriesByMediaID(list AnimeList) map[int]Entry {
	entries := make(map[int]Entry)
	for _, entry := range getEntriesByCategory(list, "ALL") {
		if entry.Media.ID == 0 {
			continue
		}
		entries[entry.Media.ID] = entry
	}
	return entries
}

func animeListFromEntryMap(entries map[int]Entry) AnimeList {
	ids := make([]int, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	result := AnimeList{}
	for _, id := range ids {
		entry := entries[id]
		switch entry.Status {
		case "CURRENT":
			result.Watching = append(result.Watching, entry)
		case "COMPLETED":
			result.Completed = append(result.Completed, entry)
		case "PAUSED":
			result.Paused = append(result.Paused, entry)
		case "DROPPED":
			result.Dropped = append(result.Dropped, entry)
		case "PLANNING":
			result.Planning = append(result.Planning, entry)
		case "REPEATING":
			result.Rewatching = append(result.Rewatching, entry)
		}
	}
	return result
}

func entriesEquivalentForSync(current, desired Entry) bool {
	current = mergeEntryMetadata(current, desired)
	desired = mergeEntryMetadata(desired, current)

	return current.Media.ID == desired.Media.ID &&
		current.Media.MalID == desired.Media.MalID &&
		current.Progress == desired.Progress &&
		current.Repeat == desired.Repeat &&
		current.Score == desired.Score &&
		current.Status == desired.Status &&
		current.StartedAt == desired.StartedAt &&
		current.CompletedAt == desired.CompletedAt
}

func myAnimeListEntriesEquivalentForSync(current, desired Entry) bool {
	if entriesEquivalentForSync(current, desired) {
		return true
	}

	if current.Repeat == 0 && desired.Repeat > 0 {
		current.Repeat = desired.Repeat
		return entriesEquivalentForSync(current, desired)
	}

	return false
}

type dualRemoteSyncPlan struct {
	Merged             AnimeList
	AniListUpdates     []Entry
	MyAnimeListUpdates []Entry
}

func buildDualRemoteSyncPlan(aniList, myAnimeList AnimeList) dualRemoteSyncPlan {
	aniMap := mapEntriesByMediaID(aniList)
	malMap := mapEntriesByMediaID(myAnimeList)
	unionIDs := make(map[int]struct{}, len(aniMap)+len(malMap))
	for id := range aniMap {
		unionIDs[id] = struct{}{}
	}
	for id := range malMap {
		unionIDs[id] = struct{}{}
	}

	orderedIDs := make([]int, 0, len(unionIDs))
	for id := range unionIDs {
		orderedIDs = append(orderedIDs, id)
	}
	sort.Ints(orderedIDs)

	mergedEntries := make(map[int]Entry, len(orderedIDs))
	plan := dualRemoteSyncPlan{}
	for _, id := range orderedIDs {
		aniEntry, hasAni := aniMap[id]
		malEntry, hasMal := malMap[id]

		switch {
		case hasAni && hasMal:
			winner := mergeAnimeEntries(aniEntry, malEntry)
			mergedEntries[id] = winner
			if !entriesEquivalentForSync(aniEntry, winner) {
				plan.AniListUpdates = append(plan.AniListUpdates, winner)
			}
			if !myAnimeListEntriesEquivalentForSync(malEntry, winner) {
				plan.MyAnimeListUpdates = append(plan.MyAnimeListUpdates, winner)
			}
		case hasAni:
			mergedEntries[id] = aniEntry
			plan.MyAnimeListUpdates = append(plan.MyAnimeListUpdates, aniEntry)
		case hasMal:
			mergedEntries[id] = malEntry
			plan.AniListUpdates = append(plan.AniListUpdates, malEntry)
		}
	}

	plan.Merged = animeListFromEntryMap(mergedEntries)
	return plan
}

func syncDualRemoteTrackers(config *Config, aniListToken string, aniListUser, myAnimeListUser *User) (AnimeList, error) {
	if config == nil {
		return AnimeList{}, fmt.Errorf("missing config")
	}
	if aniListUser == nil || myAnimeListUser == nil {
		return AnimeList{}, fmt.Errorf("missing tracker users")
	}

	// The merge is local and instant; the writes it implies are neither, and
	// nothing on screen waits for them -- the merged list below is already what
	// otakase shows. So they are handed to the background and the launch continues.
	plan := buildDualRemoteSyncPlan(aniListUser.AnimeList, myAnimeListUser.AnimeList)
	Log(fmt.Sprintf("Dual sync: %d AniList and %d MyAnimeList update(s) queued",
		len(plan.AniListUpdates), len(plan.MyAnimeListUpdates)))

	startDualSyncWrites(config, dualSyncWrites{
		aniListToken: aniListToken,
		aniList:      plan.AniListUpdates,
		myAnimeList:  plan.MyAnimeListUpdates,
	})

	aniListUser.AnimeList = plan.Merged
	myAnimeListUser.AnimeList = plan.Merged
	if err := saveAniListAnimeListCache(config.StoragePath, aniListUser.Id, plan.Merged); err != nil {
		Log(fmt.Sprintf("Failed to save AniList anime list cache after dual sync: %v", err))
	}
	if err := saveMyAnimeListCache(config.StoragePath, myAnimeListUser.Id, plan.Merged); err != nil {
		Log(fmt.Sprintf("Failed to save MyAnimeList anime list cache after dual sync: %v", err))
	}
	return plan.Merged, nil
}
func InitializeCombinedRemoteAnimeList(config *Config, user *User) error {
	aniListToken, err := GetTokenFromFile(filepath.Join(os.ExpandEnv(config.StoragePath), "anilist_token.json"))
	if err != nil {
		return err
	}
	StartupStage("Signing in to MyAnimeList")
	myAnimeListToken, err := GetMyAnimeListAccessToken(config)
	if err != nil {
		return err
	}

	StartupStage("Reading your AniList list")
	aniListUser := &User{Token: aniListToken}
	if err := InitializeAniListUserAnimeList(config, aniListUser); err != nil {
		return err
	}

	StartupStage("Reading your MyAnimeList list")
	myAnimeListUser := &User{Token: myAnimeListToken}
	if err := InitializeMyAnimeListUserAnimeList(config, myAnimeListUser); err != nil {
		return err
	}

	StartupStage("Checking MyAnimeList import")
	if err := maybeImportAniListToMyAnimeList(config, myAnimeListUser); err != nil {
		return err
	}

	StartupStage("Reconciling both trackers")
	merged, err := syncDualRemoteTrackers(config, aniListToken, aniListUser, myAnimeListUser)
	if err != nil {
		return err
	}

	StartupStage("Building the menu")
	user.Token = aniListToken
	user.AnimeList = merged
	user.ListSync = NewAnimeListSync(user.AnimeList)

	// The merge above is built from two caches, because both trackers answer
	// from cache and refresh behind us. Left there, the session would run
	// entirely on stale data: an episode that aired an hour ago still reads as
	// unreleased, and the list shows yesterday's progress. Worse, the refreshes
	// do arrive -- into the two sub-lists, which nothing reads once the merged
	// list replaces them.
	//
	// So merge again when they land, and only then call the launch refreshed.
	// Playback waits on that (briefly, and with its own timeout), which is what
	// a single-tracker setup has always done.
	go refreshCombinedRemoteAnimeList(config, user, aniListUser, myAnimeListUser)
	return nil
}

// combinedRefreshDeadline bounds the wait for both trackers. A refresh that
// never answers must not leave the list permanently marked "refreshing", since
// callers block on that.
const combinedRefreshDeadline = 20 * time.Second

// combinedRefreshDeadlineForTest lets a test exercise the give-up path without
// spending the real deadline waiting for it.
var combinedRefreshDeadlineForTest = combinedRefreshDeadline

func refreshCombinedRemoteAnimeList(config *Config, user, aniListUser, myAnimeListUser *User) {
	defer user.ListSync.MarkRefreshDone()

	deadline := time.After(combinedRefreshDeadlineForTest)
	for _, source := range []*User{aniListUser, myAnimeListUser} {
		if source == nil || source.ListSync == nil {
			continue
		}
		select {
		case <-source.ListSync.RefreshDone():
		case <-deadline:
			Log("Combined refresh: a tracker did not answer in time; keeping the cached merge")
			return
		}
	}

	fresh := buildDualRemoteSyncPlan(aniListUser.ListSync.Current(), myAnimeListUser.ListSync.Current())
	if animeListEqual(user.ListSync.Current(), fresh.Merged) {
		return
	}

	// Published through ListSync only. Assigning user.AnimeList here would race
	// with the launch reading it, and readers take it from ListSync.Current()
	// anyway; notify so menus already on screen redraw.
	user.ListSync.Replace(fresh.Merged, true)
	Log("Combined refresh: merged list updated from both trackers")

	if err := saveAniListAnimeListCache(config.StoragePath, aniListUser.Id, fresh.Merged); err != nil {
		Log(fmt.Sprintf("Combined refresh: failed to save AniList cache: %v", err))
	}
	if err := saveMyAnimeListCache(config.StoragePath, myAnimeListUser.Id, fresh.Merged); err != nil {
		Log(fmt.Sprintf("Combined refresh: failed to save MyAnimeList cache: %v", err))
	}
}

func RefreshCombinedRemoteAnimeList(config *Config, user *User) error {
	aniListToken, err := GetTokenFromFile(filepath.Join(os.ExpandEnv(config.StoragePath), "anilist_token.json"))
	if err != nil {
		return err
	}
	myAnimeListToken, err := GetMyAnimeListAccessToken(config)
	if err != nil {
		return err
	}

	aniListUser := &User{Token: aniListToken}
	if err := RefreshAniListUserAnimeList(config, aniListUser); err != nil {
		if err := InitializeAniListUserAnimeList(config, aniListUser); err != nil {
			return err
		}
	}
	myAnimeListUser := &User{Token: myAnimeListToken}
	if err := RefreshMyAnimeListUserAnimeList(config, myAnimeListUser); err != nil {
		if err := InitializeMyAnimeListUserAnimeList(config, myAnimeListUser); err != nil {
			return err
		}
	}

	merged, err := syncDualRemoteTrackers(config, aniListToken, aniListUser, myAnimeListUser)
	if err != nil {
		return err
	}
	user.Token = aniListToken
	user.AnimeList = merged
	if user.ListSync == nil {
		user.ListSync = NewAnimeListSync(merged)
		user.ListSync.MarkRefreshDone()
		return nil
	}
	user.ListSync.Replace(merged, true)
	user.ListSync.MarkRefreshDone()
	return nil
}

func InitializeUserAnimeList(userConfig *Config, user *User) error {
	if userConfig == nil || user == nil {
		return fmt.Errorf("missing user or config")
	}

	switch normalizeRemoteTracker(userConfig.TrackingRemote) {
	case TrackingRemoteAniList:
		return InitializeAniListUserAnimeList(userConfig, user)
	case TrackingRemoteMyAnimeList:
		if err := InitializeMyAnimeListUserAnimeList(userConfig, user); err != nil {
			return err
		}
		return maybeImportAniListToMyAnimeList(userConfig, user)
	case TrackingRemoteBoth:
		if err := InitializeCombinedRemoteAnimeList(userConfig, user); err != nil {
			return err
		}
		return maybeImportAniListToMyAnimeList(userConfig, user)
	default:
		list := BuildLocalAnimeList(userConfig.StoragePath)
		user.AnimeList = list
		user.ListSync = NewAnimeListSync(list)
		user.ListSync.MarkRefreshDone()
		return nil
	}
}

func RefreshUserAnimeList(userConfig *Config, user *User) error {
	if userConfig == nil || user == nil {
		return fmt.Errorf("missing user or config")
	}

	switch normalizeRemoteTracker(userConfig.TrackingRemote) {
	case TrackingRemoteAniList:
		return RefreshAniListUserAnimeList(userConfig, user)
	case TrackingRemoteMyAnimeList:
		return RefreshMyAnimeListUserAnimeList(userConfig, user)
	case TrackingRemoteBoth:
		return RefreshCombinedRemoteAnimeList(userConfig, user)
	default:
		list := BuildLocalAnimeList(userConfig.StoragePath)
		user.AnimeList = list
		if user.ListSync == nil {
			user.ListSync = NewAnimeListSync(list)
			user.ListSync.MarkRefreshDone()
			return nil
		}
		user.ListSync.Replace(list, true)
		user.ListSync.MarkRefreshDone()
		return nil
	}
}

func LoadTokenForConfiguredTracking(config *Config, user *User) error {
	if config == nil || user == nil {
		return fmt.Errorf("missing config or user")
	}

	switch normalizeRemoteTracker(config.TrackingRemote) {
	case TrackingRemoteAniList:
		token, err := GetTokenFromFile(filepath.Join(os.ExpandEnv(config.StoragePath), "anilist_token.json"))
		if err != nil {
			return err
		}
		user.Token = token
	case TrackingRemoteMyAnimeList:
		token, err := GetMyAnimeListAccessToken(config)
		if err != nil {
			return err
		}
		user.Token = token
	case TrackingRemoteBoth:
		token, err := GetTokenFromFile(filepath.Join(os.ExpandEnv(config.StoragePath), "anilist_token.json"))
		if err == nil && strings.TrimSpace(token) != "" {
			user.Token = token
			return nil
		}
		token, err = GetMyAnimeListAccessToken(config)
		if err != nil {
			return err
		}
		user.Token = token
	default:
		user.Token = ""
	}

	return nil
}

func ChangeTrackingToken(config *Config, user *User) {
	switch normalizeRemoteTracker(config.TrackingRemote) {
	case TrackingRemoteAniList:
		ChangeToken(config, user)
	case TrackingRemoteMyAnimeList:
		if err := ChangeMyAnimeListToken(config, user); err != nil {
			Exit(err)
		}
	case TrackingRemoteBoth:
		if err := ensureMyAnimeListCredentialsConfigured(config); err != nil {
			Exit(err)
		}
		ChangeToken(config, user)
		if err := ChangeMyAnimeListToken(config, user); err != nil {
			Exit(err)
		}
		if err := LoadTokenForConfiguredTracking(config, user); err != nil {
			Exit(err)
		}
	default:
		Out("Remote tracking is disabled.")
	}
}

func resolveMyAnimeListID(anilistID int) (int, error) {
	if anilistID == 0 {
		return 0, fmt.Errorf("invalid AniList ID")
	}

	if user := GetGlobalUser(); user != nil {
		if entry, err := FindAnimeByAnilistID(user.AnimeList, strconv.Itoa(anilistID)); err == nil && entry.Media.MalID != 0 {
			return entry.Media.MalID, nil
		}
	}

	return GetAnimeMalID(anilistID)
}

func UpdateAnimeProgress(token string, mediaID, progress int) error {
	config := GetGlobalConfig()
	switch {
	case !ShouldWriteRemoteTracking(config, GetGlobalAnime()):
		return nil
	case UsesDualRemoteTracking(config):
		var firstErr error
		if err := UpdateAniListAnimeProgress(token, mediaID, progress); err != nil {
			firstErr = err
		}
		malID, err := resolveMyAnimeListID(mediaID)
		if err != nil {
			if firstErr != nil {
				return firstErr
			}
			return err
		}
		if err := updateMyAnimeListProgress(config, malID, progress); err != nil && firstErr == nil {
			firstErr = err
		}
		return firstErr
	case UsesAniListTracking(config):
		return UpdateAniListAnimeProgress(token, mediaID, progress)
	case UsesMyAnimeListTracking(config):
		malID, err := resolveMyAnimeListID(mediaID)
		if err != nil {
			return err
		}
		return updateMyAnimeListProgress(config, malID, progress)
	default:
		return nil
	}
}

func UpdateAnimeStatus(token string, mediaID int, status string) error {
	config := GetGlobalConfig()
	switch {
	case !ShouldWriteRemoteTracking(config, GetGlobalAnime()):
		return nil
	case UsesDualRemoteTracking(config):
		var firstErr error
		if err := UpdateAniListAnimeStatus(token, mediaID, status); err != nil {
			firstErr = err
		}
		malID, err := resolveMyAnimeListID(mediaID)
		if err != nil {
			if firstErr != nil {
				return firstErr
			}
			return err
		}
		if err := updateMyAnimeListStatus(config, malID, status); err != nil && firstErr == nil {
			firstErr = err
		}
		return firstErr
	case UsesAniListTracking(config):
		return UpdateAniListAnimeStatus(token, mediaID, status)
	case UsesMyAnimeListTracking(config):
		malID, err := resolveMyAnimeListID(mediaID)
		if err != nil {
			return err
		}
		return updateMyAnimeListStatus(config, malID, status)
	default:
		return nil
	}
}

func RateAnime(token string, mediaID int) error {
	config := GetGlobalConfig()
	switch {
	case !ShouldWriteRemoteTracking(config, GetGlobalAnime()):
		return nil
	case UsesDualRemoteTracking(config):
		score, cancelled, err := promptAnimeScoreValue()
		if err != nil {
			return err
		}
		if cancelled {
			return nil
		}
		var firstErr error
		if err := saveAniListAnimeScore(token, mediaID, score); err != nil {
			firstErr = err
		}
		malID, err := resolveMyAnimeListID(mediaID)
		if err != nil {
			if firstErr != nil {
				return firstErr
			}
			return err
		}
		if err := rateMyAnimeListAnimeWithScore(config, malID, int(score+0.5)); err != nil && firstErr == nil {
			firstErr = err
		}
		return firstErr
	case UsesAniListTracking(config):
		return RateAniListAnime(token, mediaID)
	case UsesMyAnimeListTracking(config):
		malID, err := resolveMyAnimeListID(mediaID)
		if err != nil {
			return err
		}
		return rateMyAnimeListAnime(config, malID)
	default:
		return nil
	}
}

func AddAnimeToWatchingList(animeID int, token string) error {
	config := GetGlobalConfig()
	switch {
	case !ShouldWriteRemoteTracking(config, GetGlobalAnime()):
		return nil
	case UsesDualRemoteTracking(config):
		var firstErr error
		if err := AddAniListAnimeToWatchingList(animeID, token); err != nil {
			firstErr = err
		}
		malID, err := resolveMyAnimeListID(animeID)
		if err != nil {
			if firstErr != nil {
				return firstErr
			}
			return err
		}
		if err := addMyAnimeListAnimeToList(config, malID, "CURRENT"); err != nil && firstErr == nil {
			firstErr = err
		}
		return firstErr
	case UsesAniListTracking(config):
		return AddAniListAnimeToWatchingList(animeID, token)
	case UsesMyAnimeListTracking(config):
		malID, err := resolveMyAnimeListID(animeID)
		if err != nil {
			return err
		}
		return addMyAnimeListAnimeToList(config, malID, "CURRENT")
	default:
		return nil
	}
}

func CompleteAnimeRewatch(token string, anime Anime) error {
	config := GetGlobalConfig()
	switch {
	case !ShouldWriteRemoteTracking(config, &anime):
		return nil
	case UsesDualRemoteTracking(config):
		var firstErr error
		if err := CompleteAniListAnimeRewatch(token, anime); err != nil {
			firstErr = err
		}
		malID := anime.MalId
		if malID == 0 {
			var err error
			malID, err = resolveMyAnimeListID(anime.AnilistId)
			if err != nil {
				if firstErr != nil {
					return firstErr
				}
				return err
			}
		}
		if err := completeMyAnimeListRewatch(config, malID, anime); err != nil && firstErr == nil {
			firstErr = err
		}
		return firstErr
	case UsesAniListTracking(config):
		return CompleteAniListAnimeRewatch(token, anime)
	case UsesMyAnimeListTracking(config):
		malID := anime.MalId
		if malID == 0 {
			var err error
			malID, err = resolveMyAnimeListID(anime.AnilistId)
			if err != nil {
				return err
			}
		}
		return completeMyAnimeListRewatch(config, malID, anime)
	default:
		return nil
	}
}

func StartAnimeRewatch(token string, anime Anime) error {
	config := GetGlobalConfig()
	startedAt := currentFuzzyDate()
	completedAt := FuzzyDate{}
	switch {
	case !ShouldWriteRemoteTracking(config, &anime):
		return nil
	case UsesDualRemoteTracking(config):
		var firstErr error
		status := "REPEATING"
		progress := 0
		if err := SaveAniListAnimeListEntry(token, anime.AnilistId, &status, &progress, nil, nil, &startedAt, &completedAt); err != nil {
			firstErr = err
		}
		malID := anime.MalId
		if malID == 0 {
			var err error
			malID, err = resolveMyAnimeListID(anime.AnilistId)
			if err != nil {
				if firstErr != nil {
					return firstErr
				}
				return err
			}
		}
		if err := updateMyAnimeListListStatus(config, malID, map[string]string{
			"status":               "watching",
			"is_rewatching":        "true",
			"num_watched_episodes": "0",
			"start_date":           formatMyAnimeListDate(startedAt),
			"finish_date":          "",
		}); err != nil && firstErr == nil {
			firstErr = err
		}
		return firstErr
	case UsesAniListTracking(config):
		status := "REPEATING"
		progress := 0
		return SaveAniListAnimeListEntry(token, anime.AnilistId, &status, &progress, nil, nil, &startedAt, &completedAt)
	case UsesMyAnimeListTracking(config):
		malID := anime.MalId
		if malID == 0 {
			var err error
			malID, err = resolveMyAnimeListID(anime.AnilistId)
			if err != nil {
				return err
			}
		}
		return updateMyAnimeListListStatus(config, malID, map[string]string{
			"status":               "watching",
			"is_rewatching":        "true",
			"num_watched_episodes": "0",
			"start_date":           formatMyAnimeListDate(startedAt),
			"finish_date":          "",
		})
	default:
		return nil
	}
}

func AddAnimeToList(animeID int, status string, token string) error {
	config := GetGlobalConfig()
	switch {
	case !ShouldWriteRemoteTracking(config, GetGlobalAnime()):
		return nil
	case UsesDualRemoteTracking(config):
		var firstErr error
		if err := AddAniListAnimeToList(animeID, status, token); err != nil {
			firstErr = err
		}
		malID, err := resolveMyAnimeListID(animeID)
		if err != nil {
			if firstErr != nil {
				return firstErr
			}
			return err
		}
		if err := addMyAnimeListAnimeToList(config, malID, status); err != nil && firstErr == nil {
			firstErr = err
		}
		return firstErr
	case UsesAniListTracking(config):
		return AddAniListAnimeToList(animeID, status, token)
	case UsesMyAnimeListTracking(config):
		malID, err := resolveMyAnimeListID(animeID)
		if err != nil {
			return err
		}
		return addMyAnimeListAnimeToList(config, malID, status)
	default:
		return nil
	}
}

func maybeImportAniListToMyAnimeList(config *Config, user *User) error {
	if config == nil || user == nil || !UsesMyAnimeListTracking(config) || config.MyAnimeListImported || config.MyAnimeListImportDismissed || hasAnyEntries(user.AnimeList) {
		return nil
	}

	legacyTokenPath := filepath.Join(os.ExpandEnv(config.StoragePath), "anilist_token.json")
	if _, err := os.Stat(legacyTokenPath); err != nil {
		return nil
	}

	options := []SelectionOption{
		{Key: "yes", Label: "Import your existing AniList progress into MyAnimeList"},
		{Key: "no", Label: "Start with MyAnimeList as-is"},
	}

	Out("AniList tracking data was found.")
	selected, err := promptSelect(options)
	if err != nil {
		return err
	}

	if selected.Key == "yes" {
		if err := ImportAniListTrackingToMyAnimeList(config); err != nil {
			return err
		}
		if err := RefreshMyAnimeListUserAnimeList(config, user); err != nil {
			return err
		}
		config.MyAnimeListImported = true
		config.MyAnimeListImportDismissed = false
		return persistTrackingConfig(config)
	}

	config.MyAnimeListImportDismissed = true
	return persistTrackingConfig(config)
}

func ImportAniListTrackingToMyAnimeList(config *Config) error {
	aniListTokenPath := filepath.Join(os.ExpandEnv(config.StoragePath), "anilist_token.json")
	aniListToken, err := GetTokenFromFile(aniListTokenPath)
	if err != nil {
		return err
	}

	userID, _, err := GetAnilistUserID(aniListToken)
	if err != nil {
		return err
	}

	userData, err := GetUserData(aniListToken, userID)
	if err != nil {
		return err
	}

	sourceList := ParseAnimeList(userData)
	for _, entry := range getEntriesByCategory(sourceList, "ALL") {
		if entry.Media.MalID == 0 {
			continue
		}

		payload := map[string]string{
			"status":               aniListStatusToMyAnimeListStatus(entry.Status),
			"num_watched_episodes": strconv.Itoa(entry.Progress),
		}
		if entry.Score > 0 {
			payload["score"] = strconv.Itoa(int(entry.Score + 0.5))
		}
		if entry.Repeat > 0 {
			payload["num_times_rewatched"] = strconv.Itoa(entry.Repeat)
		}
		if entry.Status == "REPEATING" {
			payload["is_rewatching"] = "true"
			payload["status"] = "watching"
		}

		if err := updateMyAnimeListListStatus(config, entry.Media.MalID, payload); err != nil {
			return err
		}
	}

	return nil
}

func trackingSummary(config *Config) string {
	parts := make([]string, 0, 2)
	if UsesLocalTracking(config) {
		parts = append(parts, "local")
	}
	remote := normalizeRemoteTracker(config.TrackingRemote)
	if remote != TrackingRemoteNone {
		parts = append(parts, remote)
	}
	return strings.Join(parts, "+")
}

func RemoteTrackingDisplayName(config *Config) string {
	switch normalizeRemoteTracker(config.TrackingRemote) {
	case TrackingRemoteBoth:
		return "AniList + MyAnimeList"
	case TrackingRemoteMyAnimeList:
		return "MyAnimeList"
	case TrackingRemoteAniList:
		return "AniList"
	default:
		return "remote tracking"
	}
}

func fetchAniListAnimeListFromToken(token string) (AnimeList, error) {
	userID, _, err := GetAnilistUserID(token)
	if err != nil {
		return AnimeList{}, err
	}
	userData, err := GetUserData(token, userID)
	if err != nil {
		return AnimeList{}, err
	}
	return ParseAnimeList(userData), nil
}

func saveAniListTrackedEntry(token string, entry Entry) error {
	status := entry.Status
	progress := entry.Progress
	repeat := entry.Repeat
	score := entry.Score
	return SaveAniListAnimeListEntry(token, entry.Media.ID, &status, &progress, &repeat, &score, &entry.StartedAt, &entry.CompletedAt)
}

func saveMyAnimeListTrackedEntry(config *Config, entry Entry) error {
	malID := entry.Media.MalID
	if malID == 0 {
		var err error
		malID, err = resolveMyAnimeListID(entry.Media.ID)
		if err != nil {
			return err
		}
	}

	payload := map[string]string{
		"status":               aniListStatusToMyAnimeListStatus(entry.Status),
		"num_watched_episodes": strconv.Itoa(entry.Progress),
		"score":                strconv.Itoa(int(entry.Score + 0.5)),
		"start_date":           formatMyAnimeListDate(entry.StartedAt),
		"finish_date":          formatMyAnimeListDate(entry.CompletedAt),
		"is_rewatching":        "false",
	}
	if entry.Status == "REPEATING" {
		payload["status"] = "watching"
		payload["is_rewatching"] = "true"
	}
	if entry.Status == "COMPLETED" && entry.Repeat > 0 {
		delete(payload, "is_rewatching")
		if err := updateMyAnimeListListStatus(config, malID, payload); err != nil {
			return err
		}
		return updateMyAnimeListListStatus(config, malID, map[string]string{
			"num_times_rewatched": strconv.Itoa(entry.Repeat),
		})
	}
	payload["num_times_rewatched"] = strconv.Itoa(entry.Repeat)
	return updateMyAnimeListListStatus(config, malID, payload)
}

func upsertAnimeListToAniList(token string, list AnimeList) error {
	for index, entry := range getEntriesByCategory(list, "ALL") {
		if index > 0 {
			time.Sleep(350 * time.Millisecond)
		}
		if err := saveAniListTrackedEntry(token, entry); err != nil {
			return err
		}
	}
	return nil
}

func upsertAnimeListToMyAnimeList(config *Config, list AnimeList) error {
	for _, entry := range getEntriesByCategory(list, "ALL") {
		if err := saveMyAnimeListTrackedEntry(config, entry); err != nil {
			return err
		}
	}
	return nil
}

func wipeAniListRemote(config *Config) error {
	token, err := GetTokenFromFile(filepath.Join(os.ExpandEnv(config.StoragePath), "anilist_token.json"))
	if err != nil {
		return err
	}
	list, err := fetchAniListAnimeListFromToken(token)
	if err != nil {
		return err
	}
	for index, entry := range getEntriesByCategory(list, "ALL") {
		if entry.ListID == 0 {
			continue
		}
		if index > 0 {
			time.Sleep(350 * time.Millisecond)
		}
		if err := DeleteAniListListEntry(token, entry.ListID); err != nil {
			return err
		}
	}
	return nil
}

func wipeMyAnimeListRemote(config *Config) error {
	list, err := FetchLatestMyAnimeList(config, &User{})
	if err != nil {
		return err
	}
	for _, entry := range getEntriesByCategory(list, "ALL") {
		if entry.Media.MalID == 0 {
			continue
		}
		if err := deleteMyAnimeListEntry(config, entry.Media.MalID); err != nil {
			return err
		}
	}
	return nil
}

func ReplaceMyAnimeListWithAniList(config *Config) error {
	token, err := GetTokenFromFile(filepath.Join(os.ExpandEnv(config.StoragePath), "anilist_token.json"))
	if err != nil {
		return err
	}
	sourceList, err := fetchAniListAnimeListFromToken(token)
	if err != nil {
		return err
	}
	targetList, err := FetchLatestMyAnimeList(config, &User{})
	if err != nil {
		return err
	}
	ok, err := confirmRemoteSync(remoteSyncPreview{
		Action:             "Replace MyAnimeList with AniList",
		AniListEntries:     animeListEntryCount(sourceList),
		MyAnimeListEntries: animeListEntryCount(targetList),
		Writes:             animeListEntryCount(sourceList),
		Deletes:            animeListEntryCount(targetList),
		MissingMALIDs:      countEntriesMissingMyAnimeListID(sourceList),
	})
	if err != nil {
		return err
	}
	if !ok {
		Out("Tracker sync cancelled.")
		return nil
	}
	backupPath, err := writeTrackingBackup(config, "replace-myanimelist-with-anilist", sourceList, targetList)
	if err != nil {
		return err
	}
	Out(fmt.Sprintf("Tracker backup saved: %s", backupPath))
	if err := wipeMyAnimeListRemote(config); err != nil {
		return err
	}
	return upsertAnimeListToMyAnimeList(config, sourceList)
}

func ReplaceAniListWithMyAnimeList(config *Config) error {
	token, err := GetTokenFromFile(filepath.Join(os.ExpandEnv(config.StoragePath), "anilist_token.json"))
	if err != nil {
		return err
	}
	sourceList, err := FetchLatestMyAnimeList(config, &User{})
	if err != nil {
		return err
	}
	targetList, err := fetchAniListAnimeListFromToken(token)
	if err != nil {
		return err
	}
	ok, err := confirmRemoteSync(remoteSyncPreview{
		Action:             "Replace AniList with MyAnimeList",
		AniListEntries:     animeListEntryCount(targetList),
		MyAnimeListEntries: animeListEntryCount(sourceList),
		Writes:             animeListEntryCount(sourceList),
		Deletes:            countAniListDeletes(targetList),
		MissingMALIDs:      countEntriesMissingMyAnimeListID(sourceList),
	})
	if err != nil {
		return err
	}
	if !ok {
		Out("Tracker sync cancelled.")
		return nil
	}
	backupPath, err := writeTrackingBackup(config, "replace-anilist-with-myanimelist", targetList, sourceList)
	if err != nil {
		return err
	}
	Out(fmt.Sprintf("Tracker backup saved: %s", backupPath))
	if err := wipeAniListRemote(config); err != nil {
		return err
	}
	return upsertAnimeListToAniList(token, sourceList)
}

func MergeRemoteLists(config *Config) error {
	token, err := GetTokenFromFile(filepath.Join(os.ExpandEnv(config.StoragePath), "anilist_token.json"))
	if err != nil {
		return err
	}
	aniListUser := &User{}
	aniList, err := fetchAniListAnimeListFromToken(token)
	if err != nil {
		return err
	}
	aniListUser.AnimeList = aniList

	myAnimeListUser := &User{}
	myAnimeList, err := FetchLatestMyAnimeList(config, myAnimeListUser)
	if err != nil {
		return err
	}
	myAnimeListUser.AnimeList = myAnimeList

	plan := buildDualRemoteSyncPlan(aniList, myAnimeList)
	ok, err := confirmRemoteSync(remoteSyncPreview{
		Action:             "Merge AniList and MyAnimeList",
		AniListEntries:     animeListEntryCount(aniList),
		MyAnimeListEntries: animeListEntryCount(myAnimeList),
		Writes:             len(plan.AniListUpdates) + len(plan.MyAnimeListUpdates),
		Deletes:            0,
		MissingMALIDs:      countEntriesMissingMyAnimeListID(plan.Merged),
	})
	if err != nil {
		return err
	}
	if !ok {
		Out("Tracker sync cancelled.")
		return nil
	}
	backupPath, err := writeTrackingBackup(config, "merge-anilist-and-myanimelist", aniList, myAnimeList)
	if err != nil {
		return err
	}
	Out(fmt.Sprintf("Tracker backup saved: %s", backupPath))
	_, err = syncDualRemoteTrackers(config, token, aniListUser, myAnimeListUser)
	return err
}

func trackersCanCrossSync(config *Config) bool {
	if _, err := GetTokenFromFile(filepath.Join(os.ExpandEnv(config.StoragePath), "anilist_token.json")); err != nil {
		return false
	}
	if _, err := GetMyAnimeListAccessToken(config); err != nil {
		return false
	}
	return true
}

func ChangeTracker(config *Config, user *User) {
	options := []SelectionOption{
		{Key: TrackingRemoteNone, Label: "local"},
		{Key: TrackingRemoteAniList, Label: "anilist"},
		{Key: TrackingRemoteMyAnimeList, Label: "myanimelist"},
		{Key: TrackingRemoteBoth, Label: "anilist + myanimelist"},
	}

	selected, err := DynamicSelect(options)
	if err != nil || selected.Key == "-1" || selected.Key == "-2" {
		return
	}

	config.TrackingLocal = true
	config.TrackingRemote = selected.Key
	config.TrackingConfigured = true
	normalizeTrackingConfig(config)
	if err := persistTrackingConfig(config); err != nil {
		Exit(err)
	}
	if err := EnsureConfiguredTrackersReady(config, user); err != nil {
		Exit(err)
	}

	if trackersCanCrossSync(config) {
		syncOptions := []SelectionOption{
			{Key: "skip", Label: "Keep current lists"},
			{Key: "merge", Label: "Merge both lists and update both"},
			{Key: "anilist_to_mal", Label: "Replace MyAnimeList with AniList"},
			{Key: "mal_to_anilist", Label: "Replace AniList with MyAnimeList"},
		}
		Out("Optional tracker sync action:")
		syncSelection, syncErr := DynamicSelect(syncOptions)
		if syncErr == nil {
			switch syncSelection.Key {
			case "merge":
				err = MergeRemoteLists(config)
			case "anilist_to_mal":
				err = ReplaceMyAnimeListWithAniList(config)
			case "mal_to_anilist":
				err = ReplaceAniListWithMyAnimeList(config)
			}
			if err != nil {
				Exit(err)
			}
		}
	}

	if err := RefreshUserAnimeList(config, user); err != nil {
		Exit(err)
	}
	Out(fmt.Sprintf("Tracker changed to %s.", selected.Label))
}

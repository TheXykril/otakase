package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/icons"
)

// One-off actions that copy or merge whole lists between the two trackers,
// plus the first-run AniList import into MyAnimeList.

// remoteSyncPreview is what a tracker sync action is about to do, shown
// before it is allowed to.
type remoteSyncPreview struct {
	Action             string
	AniListEntries     int
	MyAnimeListEntries int
	Writes             int
	Deletes            int
	MissingMALIDs      int
}

// summary is the lines shown before asking whether to go ahead.
func (p remoteSyncPreview) summary() []string {
	lines := []string{fmt.Sprintf("%s: AniList %d, MyAnimeList %d.", p.Action, p.AniListEntries, p.MyAnimeListEntries)}
	effect := fmt.Sprintf("This will write %d entries", p.Writes)
	if p.Deletes > 0 {
		effect += fmt.Sprintf(" and delete %d entries", p.Deletes)
	}
	lines = append(lines, effect+".")
	if p.MissingMALIDs > 0 {
		lines = append(lines, fmt.Sprintf("%d entries need a MyAnimeList ID lookup.", p.MissingMALIDs))
	}
	return lines
}

func confirmRemoteSync(preview remoteSyncPreview) (bool, error) {
	// The live tests run these actions with nobody there to answer.
	if os.Getenv("OTAKASE_TEST_AUTO_CONFIRM_REMOTE_SYNC") == "1" {
		return true, nil
	}

	for _, line := range preview.summary() {
		Out(line)
	}
	answer, selectErr := promptSelect([]SelectionOption{
		{Key: "continue", Label: "Continue", Icon: icons.Next},
		{Key: "cancel", Label: "Cancel", Icon: icons.No},
	})
	if selectErr != nil {
		return false, selectErr
	}
	return answer.Key == "continue", nil
}

// trackingBackup is the snapshot of both lists saved before a sync action
// changes either of them.
type trackingBackup struct {
	Action      string    `json:"action"`
	CreatedAt   time.Time `json:"created_at"`
	AniList     AnimeList `json:"anilist"`
	MyAnimeList AnimeList `json:"myanimelist"`
}

// backupNameReplacer turns an action name into something safe in a filename.
var backupNameReplacer = strings.NewReplacer(" ", "-", "+", "-", ":", "", "/", "-")

// writeTrackingBackup saves both lists under tracking-backups in the storage
// dir and returns the file written.
func writeTrackingBackup(config *Config, action string, aniList, myAnimeList AnimeList) (string, error) {
	if config == nil {
		return "", errors.New("missing config")
	}

	backupDir := filepath.Join(os.ExpandEnv(config.StoragePath), "tracking-backups")
	if mkdirErr := os.MkdirAll(backupDir, 0o755); mkdirErr != nil {
		return "", mkdirErr
	}

	taken := time.Now()
	snapshot := trackingBackup{Action: action, CreatedAt: taken, AniList: aniList, MyAnimeList: myAnimeList}
	encoded, encodeErr := json.MarshalIndent(snapshot, "", "  ")
	if encodeErr != nil {
		return "", encodeErr
	}

	slug := backupNameReplacer.Replace(strings.ToLower(action))
	target := filepath.Join(backupDir, fmt.Sprintf("%s-%s.json", taken.Format("20060102-150405"), slug))
	if writeErr := os.WriteFile(target, encoded, 0o644); writeErr != nil {
		return "", writeErr
	}
	return target, nil
}

// maybeImportAniListToMyAnimeList offers, once, to copy an AniList list into
// an empty MyAnimeList one. A saved AniList token is the sign there is one.
func maybeImportAniListToMyAnimeList(config *Config, user *User) error {
	switch {
	case config == nil || user == nil || !UsesMyAnimeListTracking(config):
		return nil
	case config.MyAnimeListImported, config.MyAnimeListImportDismissed:
		return nil
	case hasAnyEntries(user.AnimeList):
		return nil
	}
	if _, statErr := os.Stat(anilistTokenPath(config)); statErr != nil {
		return nil
	}

	Out("AniList tracking data was found.")
	answer, selectErr := promptSelect([]SelectionOption{
		{Key: "yes", Label: "Import your existing AniList progress into MyAnimeList", Icon: icons.Import},
		{Key: "no", Label: "Start with MyAnimeList as-is", Icon: icons.Next},
	})
	if selectErr != nil {
		return selectErr
	}

	if answer.Key == "yes" {
		if importErr := ImportAniListTrackingToMyAnimeList(config); importErr != nil {
			return importErr
		}
		if refreshErr := RefreshMyAnimeListUserAnimeList(config, user); refreshErr != nil {
			return refreshErr
		}
	}
	config.MyAnimeListImported = answer.Key == "yes"
	config.MyAnimeListImportDismissed = answer.Key != "yes"
	return persistTrackingConfig(config)
}

// myAnimeListImportFields is what importing an AniList entry writes to
// MyAnimeList. Unlike a sync it leaves dates alone and only sends a score or
// rewatch count that is actually set.
func myAnimeListImportFields(entry Entry) map[string]string {
	fields := map[string]string{"num_watched_episodes": strconv.Itoa(entry.Progress)}
	maps.Copy(fields, myAnimeListStatusFields(entry.Status))
	if entry.Status != "REPEATING" {
		// Only a rewatch sends the flag on import.
		delete(fields, "is_rewatching")
	}
	if entry.Score > 0 {
		fields["score"] = strconv.Itoa(myAnimeListScore(entry.Score))
	}
	if entry.Repeat > 0 {
		fields["num_times_rewatched"] = strconv.Itoa(entry.Repeat)
	}
	return fields
}

// ImportAniListTrackingToMyAnimeList copies the signed-in AniList list onto
// MyAnimeList. Shows without a MyAnimeList ID can't be written there, and
// private entries stay off it since MyAnimeList cannot hide one.
func ImportAniListTrackingToMyAnimeList(config *Config) error {
	token, tokenErr := storedAniListToken(config)
	if tokenErr != nil {
		return tokenErr
	}
	source, fetchErr := fetchAniListAnimeListFromToken(token)
	if fetchErr != nil {
		return fetchErr
	}

	for _, entry := range getEntriesByCategory(source, "ALL") {
		if entry.Private || entry.Media.MalID == 0 {
			continue
		}
		if writeErr := updateMyAnimeListListStatus(config, entry.Media.MalID, myAnimeListImportFields(entry)); writeErr != nil {
			return writeErr
		}
	}
	return nil
}

// fetchAniListAnimeListFromToken reads the whole list of whoever token
// belongs to.
func fetchAniListAnimeListFromToken(token string) (AnimeList, error) {
	viewer, _, viewerErr := GetAnilistUserID(token)
	if viewerErr != nil {
		return AnimeList{}, viewerErr
	}
	raw, fetchErr := GetUserData(token, viewer)
	if fetchErr != nil {
		return AnimeList{}, fetchErr
	}
	return ParseAnimeList(raw), nil
}

// saveAniListTrackedEntry writes an entry's whole list state to AniList.
func saveAniListTrackedEntry(token string, entry Entry) error {
	return SaveAniListAnimeListEntry(token, entry.Media.ID, &entry.Status, &entry.Progress, &entry.Repeat, &entry.Score, &entry.StartedAt, &entry.CompletedAt)
}

// myAnimeListEntryWrites are the writes that put an entry's whole list state
// on MyAnimeList, in order. A completed entry that has been rewatched gets
// its count in a second write after the completion, as finishing a rewatch
// does, and is not marked as being rewatched.
func myAnimeListEntryWrites(entry Entry) []map[string]string {
	state := myAnimeListStatusFields(entry.Status)
	state["is_rewatching"] = strconv.FormatBool(entry.Status == "REPEATING")
	state["num_watched_episodes"] = strconv.Itoa(entry.Progress)
	state["score"] = strconv.Itoa(myAnimeListScore(entry.Score))
	state["start_date"] = formatMyAnimeListDate(entry.StartedAt)
	state["finish_date"] = formatMyAnimeListDate(entry.CompletedAt)
	rewatchCount := strconv.Itoa(entry.Repeat)

	if entry.Repeat > 0 && entry.Status == "COMPLETED" {
		delete(state, "is_rewatching")
		return []map[string]string{state, {"num_times_rewatched": rewatchCount}}
	}
	state["num_times_rewatched"] = rewatchCount
	return []map[string]string{state}
}

// saveMyAnimeListTrackedEntry writes an entry's whole list state to
// MyAnimeList, looking its MyAnimeList ID up when the entry has none.
func saveMyAnimeListTrackedEntry(config *Config, entry Entry) error {
	malID, lookupErr := malIDFor(entry.Media.MalID, entry.Media.ID)()
	if lookupErr != nil {
		return lookupErr
	}
	for _, fields := range myAnimeListEntryWrites(entry) {
		if writeErr := updateMyAnimeListListStatus(config, malID, fields); writeErr != nil {
			return writeErr
		}
	}
	return nil
}

// eachEntry calls apply on every entry of list in turn, stopping at the first
// error. With paced set, consecutive calls are spaced out for the rate limit.
func eachEntry(list AnimeList, paced bool, apply func(Entry) error) error {
	for i, entry := range getEntriesByCategory(list, "ALL") {
		if paced && i > 0 {
			sleepBetweenRemoteWrites()
		}
		if applyErr := apply(entry); applyErr != nil {
			return applyErr
		}
	}
	return nil
}

func upsertAnimeListToAniList(token string, list AnimeList) error {
	return eachEntry(list, true, func(entry Entry) error {
		return saveAniListTrackedEntry(token, entry)
	})
}

func upsertAnimeListToMyAnimeList(config *Config, list AnimeList) error {
	return eachEntry(list, false, func(entry Entry) error {
		return saveMyAnimeListTrackedEntry(config, entry)
	})
}

// wipeAniListRemote deletes every entry of the signed-in AniList list.
func wipeAniListRemote(config *Config) error {
	token, tokenErr := storedAniListToken(config)
	if tokenErr != nil {
		return tokenErr
	}
	current, fetchErr := fetchAniListAnimeListFromToken(token)
	if fetchErr != nil {
		return fetchErr
	}
	// The pause depends on the position in the whole list, rows or not, as
	// it always has.
	for i, entry := range getEntriesByCategory(current, "ALL") {
		if !hasAniListRow(entry) {
			continue
		}
		if i > 0 {
			sleepBetweenRemoteWrites()
		}
		if deleteErr := DeleteAniListListEntry(token, entry.ListID); deleteErr != nil {
			return deleteErr
		}
	}
	return nil
}

// wipeMyAnimeListRemote deletes every entry of the signed-in MyAnimeList list.
func wipeMyAnimeListRemote(config *Config) error {
	current, fetchErr := FetchLatestMyAnimeList(config, &User{})
	if fetchErr != nil {
		return fetchErr
	}
	return eachEntry(current, false, func(entry Entry) error {
		if lacksMalID(entry) {
			return nil
		}
		return deleteMyAnimeListEntry(config, entry.Media.MalID)
	})
}

// remoteLists is both trackers' lists as they stand, read for a sync action.
type remoteLists struct {
	aniListToken string
	aniList      AnimeList
	myAnimeList  AnimeList
}

// fetchRemoteLists reads both lists fresh from the trackers. myAnimeListUser
// receives the MyAnimeList profile.
func fetchRemoteLists(config *Config, myAnimeListUser *User) (remoteLists, error) {
	var lists remoteLists
	var err error
	if lists.aniListToken, err = storedAniListToken(config); err != nil {
		return remoteLists{}, err
	}
	if lists.aniList, err = fetchAniListAnimeListFromToken(lists.aniListToken); err != nil {
		return remoteLists{}, err
	}
	if lists.myAnimeList, err = FetchLatestMyAnimeList(config, myAnimeListUser); err != nil {
		return remoteLists{}, err
	}
	return lists, nil
}

// runRemoteSyncAction asks before a sync action and, given the go-ahead, saves
// both lists as a backup and only then runs apply.
func runRemoteSyncAction(config *Config, lists remoteLists, backupName string, preview remoteSyncPreview, apply func() error) error {
	approved, confirmErr := confirmRemoteSync(preview)
	if confirmErr != nil {
		return confirmErr
	}
	if !approved {
		Out("Tracker sync cancelled.")
		return nil
	}
	saved, backupErr := writeTrackingBackup(config, backupName, lists.aniList, lists.myAnimeList)
	if backupErr != nil {
		return backupErr
	}
	Out("Tracker backup saved: " + saved)
	return apply()
}

// replacePreview describes overwriting one tracker's list with the other's.
func replacePreview(action string, lists remoteLists, source, target AnimeList, deletable func(Entry) bool) remoteSyncPreview {
	return remoteSyncPreview{
		Action:             action,
		AniListEntries:     countEntries(lists.aniList, anyEntry),
		MyAnimeListEntries: countEntries(lists.myAnimeList, anyEntry),
		Writes:             countEntries(source, anyEntry),
		Deletes:            countEntries(target, deletable),
		MissingMALIDs:      countEntries(source, lacksMalID),
	}
}

func ReplaceMyAnimeListWithAniList(config *Config) error {
	lists, fetchErr := fetchRemoteLists(config, &User{})
	if fetchErr != nil {
		return fetchErr
	}
	preview := replacePreview("Replace MyAnimeList with AniList", lists, lists.aniList, lists.myAnimeList, anyEntry)
	return runRemoteSyncAction(config, lists, "replace-myanimelist-with-anilist", preview, func() error {
		if wipeErr := wipeMyAnimeListRemote(config); wipeErr != nil {
			return wipeErr
		}
		return upsertAnimeListToMyAnimeList(config, lists.aniList)
	})
}

func ReplaceAniListWithMyAnimeList(config *Config) error {
	lists, fetchErr := fetchRemoteLists(config, &User{})
	if fetchErr != nil {
		return fetchErr
	}
	preview := replacePreview("Replace AniList with MyAnimeList", lists, lists.myAnimeList, lists.aniList, hasAniListRow)
	return runRemoteSyncAction(config, lists, "replace-anilist-with-myanimelist", preview, func() error {
		if wipeErr := wipeAniListRemote(config); wipeErr != nil {
			return wipeErr
		}
		return upsertAnimeListToAniList(lists.aniListToken, lists.myAnimeList)
	})
}

// MergeRemoteLists reconciles both lists and writes the result to both.
func MergeRemoteLists(config *Config) error {
	malSide := &User{}
	lists, fetchErr := fetchRemoteLists(config, malSide)
	if fetchErr != nil {
		return fetchErr
	}
	malSide.AnimeList = lists.myAnimeList
	aniSide := &User{AnimeList: lists.aniList}

	plan := buildDualRemoteSyncPlan(lists.aniList, lists.myAnimeList)
	preview := remoteSyncPreview{
		Action:             "Merge AniList and MyAnimeList",
		AniListEntries:     countEntries(lists.aniList, anyEntry),
		MyAnimeListEntries: countEntries(lists.myAnimeList, anyEntry),
		Writes:             len(plan.AniListUpdates) + len(plan.MyAnimeListUpdates),
		MissingMALIDs:      countEntries(plan.Merged, lacksMalID),
	}
	return runRemoteSyncAction(config, lists, "merge-anilist-and-myanimelist", preview, func() error {
		_, syncErr := syncDualRemoteTrackers(config, lists.aniListToken, aniSide, malSide)
		return syncErr
	})
}

// trackersCanCrossSync reports whether both trackers are signed in, which
// the sync actions need whatever tracker is now chosen.
func trackersCanCrossSync(config *Config) bool {
	_, aniListErr := storedAniListToken(config)
	if aniListErr != nil {
		return false
	}
	_, malErr := GetMyAnimeListAccessToken(config)
	return malErr == nil
}

// trackerSyncActions are the optional actions offered after a tracker change,
// by option key. Keeping the lists as they are is the absence of one.
var trackerSyncActions = map[string]func(*Config) error{
	"merge":          MergeRemoteLists,
	"anilist_to_mal": ReplaceMyAnimeListWithAniList,
	"mal_to_anilist": ReplaceAniListWithMyAnimeList,
}

// offerTrackerSyncAction asks which sync action, if any, to run after a
// tracker change and runs it.
func offerTrackerSyncAction(config *Config) {
	Out("Optional tracker sync action:")
	picked, selectErr := DynamicSelect([]SelectionOption{
		{Key: "skip", Label: "Keep current lists", Icon: icons.Yes},
		{Key: "merge", Label: "Merge both lists and update both", Icon: icons.Merge},
		{Key: "anilist_to_mal", Label: "Replace MyAnimeList with AniList", Icon: icons.Remap},
		{Key: "mal_to_anilist", Label: "Replace AniList with MyAnimeList", Icon: icons.Remap},
	})
	if selectErr != nil {
		return
	}
	if action, known := trackerSyncActions[picked.Key]; known {
		exitOnError(action(config))
	}
}

// ChangeTracker switches tracking mode from the menu, signs in where needed,
// optionally reconciles the two lists, and reloads.
func ChangeTracker(config *Config, user *User) {
	picked, selectErr := DynamicSelect(trackerChoices())
	if selectErr != nil || selectionCancelled(picked) {
		return
	}

	exitOnError(setTracker(config, picked.Key))
	exitOnError(EnsureConfiguredTrackersReady(config, user))
	if trackersCanCrossSync(config) {
		offerTrackerSyncAction(config)
	}
	exitOnError(RefreshUserAnimeList(config, user))
	Out(fmt.Sprintf("Tracker changed to %s.", picked.Label))
}

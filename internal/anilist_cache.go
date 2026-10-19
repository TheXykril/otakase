package internal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	// anilistAnimeListCacheFileName lives in the storage directory.
	anilistAnimeListCacheFileName = "anilist_list_cache.json"
)

// animeListCachePayload is how a cached list sits on disk, for AniList and
// MyAnimeList alike.
type animeListCachePayload struct {
	AnimeList AnimeList `json:"anime_list"`
	UpdatedAt time.Time `json:"updated_at"`

	// UserID is the tracker account the list belongs to; 0 when unknown.
	UserID int `json:"user_id"`
}

// ownedBy reports whether the cache may be shown to userID. A zero on either
// side means the owner is unknown, which is not a mismatch.
func (p animeListCachePayload) ownedBy(userID int) bool {
	return p.UserID == 0 || userID == 0 || p.UserID == userID
}

// encodeListCache is the file content for a list cached now for userID.
func encodeListCache(userID int, list AnimeList) ([]byte, error) {
	return json.MarshalIndent(animeListCachePayload{
		AnimeList: list,
		UpdatedAt: time.Now(),
		UserID:    userID,
	}, "", "  ")
}

// AnimeListSync is the list menus draw from while a refresh runs behind them.
// Only the newest list is ever queued on Updates: a menu that was busy has no
// use for the ones it missed.
type AnimeListSync struct {
	mu       sync.RWMutex
	list     AnimeList
	updates  chan AnimeList
	finished chan struct{}
	finish   sync.Once
}

func NewAnimeListSync(start AnimeList) *AnimeListSync {
	ls := &AnimeListSync{list: start}
	ls.updates = make(chan AnimeList, 1)
	ls.finished = make(chan struct{})
	return ls
}

// settledAnimeListSync is a sync for a list that is already fresh, so nothing
// waits for a refresh.
func settledAnimeListSync(list AnimeList) *AnimeListSync {
	ls := NewAnimeListSync(list)
	ls.MarkRefreshDone()
	return ls
}

// MarkRefreshDone is safe to call repeatedly; calls after the first do nothing.
func (ls *AnimeListSync) MarkRefreshDone() {
	ls.finish.Do(func() { close(ls.finished) })
}

// RefreshDone is closed when the background refresh ends, successful or not.
func (ls *AnimeListSync) RefreshDone() <-chan struct{} { return ls.finished }

func (ls *AnimeListSync) Current() AnimeList {
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	return ls.list
}

// Replace installs next and reports whether it differs from what was there.
// When it does and notify is set, next is also queued on Updates.
func (ls *AnimeListSync) Replace(next AnimeList, notify bool) bool {
	ls.mu.Lock()
	old := ls.list
	ls.list = next
	ls.mu.Unlock()

	if animeListEqual(old, next) {
		return false
	}
	if notify {
		ls.offer(next)
	}
	return true
}

// offer queues list without blocking. Anything still sitting in the one-slot
// buffer is out of date by now and gets dropped to make room.
func (ls *AnimeListSync) offer(list AnimeList) {
	for {
		select {
		case ls.updates <- list:
			return
		default:
		}
		select {
		case <-ls.updates:
		default:
		}
	}
}

func (ls *AnimeListSync) Updates() <-chan AnimeList { return ls.updates }

// showCachedList puts a list read from cache in front of user. The cache's
// account ID saves a Viewer lookup before the menu can open.
func showCachedList(user *User, cached animeListCachePayload) {
	if user.Id == 0 {
		user.Id = cached.UserID
	}
	user.AnimeList = cached.AnimeList
	user.ListSync = NewAnimeListSync(cached.AnimeList)
}

// replaceUserList swaps in a list fetched just now. A sync menus are already
// watching is updated in place so they redraw.
func replaceUserList(user *User, fresh AnimeList) {
	user.AnimeList = fresh
	if user.ListSync == nil {
		user.ListSync = settledAnimeListSync(fresh)
		return
	}
	user.ListSync.Replace(fresh, true)
}

func aniListCachePath(storageDir string) string {
	dir := os.ExpandEnv(storageDir)
	return filepath.Join(dir, anilistAnimeListCacheFileName)
}

// loadAniListAnimeListCache won't hand back a cache saved for a different
// account, so switching accounts never shows the old list. A missing file
// is reported as fs.ErrNotExist.
func loadAniListAnimeListCache(storageDir string, accountID int) (animeListCachePayload, error) {
	raw, readErr := os.ReadFile(aniListCachePath(storageDir))
	if readErr != nil {
		return animeListCachePayload{}, readErr
	}
	var cached animeListCachePayload
	if decodeErr := json.Unmarshal(raw, &cached); decodeErr != nil {
		return animeListCachePayload{}, fmt.Errorf("failed to parse anime list cache: %w", decodeErr)
	}
	if !cached.ownedBy(accountID) {
		return animeListCachePayload{}, errors.New("anime list cache belongs to a different AniList user")
	}
	return cached, nil
}

func saveAniListAnimeListCache(storageDir string, accountID int, list AnimeList) error {
	encoded, encodeErr := encodeListCache(accountID, list)
	if encodeErr != nil {
		return fmt.Errorf("failed to marshal anime list cache: %w", encodeErr)
	}
	if writeErr := writeFileAtomic(aniListCachePath(storageDir), encoded, 0o644); writeErr != nil {
		return fmt.Errorf("failed to write anime list cache: %w", writeErr)
	}
	return nil
}

// animeListEqual compares two lists in their cached form; that is the test
// for whether a refresh changed anything worth redrawing.
func animeListEqual(x, y AnimeList) bool {
	left, leftErr := json.Marshal(x)
	if leftErr != nil {
		return false
	}
	right, rightErr := json.Marshal(y)
	return rightErr == nil && bytes.Equal(left, right)
}

// FetchLatestAniListAnimeList reads the viewer's list with cover images.
func FetchLatestAniListAnimeList(token string, userID int) (AnimeList, error) {
	vars := map[string]interface{}{"userId": userID, "type": "ANIME"}
	var collection aniListCollectionResponse
	if queryErr := aniListQuery(aniListCollectionQueryFor(true), vars, token, &collection); queryErr != nil {
		return AnimeList{}, queryErr
	}
	return animeListFromCollection(collection), nil
}

// ensureAniListUserID looks the viewer up when the ID is not known yet. A
// username already set is kept unless replaceUsername is true.
func ensureAniListUserID(user *User, replaceUsername bool) error {
	if user.Id != 0 {
		return nil
	}
	viewerID, viewerName, lookupErr := GetAnilistUserID(user.Token)
	if lookupErr != nil {
		return lookupErr
	}
	user.Id = viewerID
	if user.Username == "" || replaceUsername {
		user.Username = viewerName
	}
	return nil
}

// pullAniListAnimeList fetches the list and caches it. A failed cache write is
// only logged, under logPrefix, since the list itself is fine.
func pullAniListAnimeList(userConfig *Config, user *User, replaceUsername bool, logPrefix string) (AnimeList, error) {
	if idErr := ensureAniListUserID(user, replaceUsername); idErr != nil {
		return AnimeList{}, idErr
	}
	fresh, fetchErr := FetchLatestAniListAnimeList(user.Token, user.Id)
	if fetchErr != nil {
		return AnimeList{}, fetchErr
	}
	if saveErr := saveAniListAnimeListCache(userConfig.StoragePath, user.Id, fresh); saveErr != nil {
		Log(fmt.Sprintf("%s: %v", logPrefix, saveErr))
	}
	return fresh, nil
}

// refreshAniListAnimeListInBackground starts a refresh of the list user is
// already showing. Without a sync to report to there is nothing to do.
func refreshAniListAnimeListInBackground(userConfig *Config, user *User) {
	if user != nil && user.ListSync != nil {
		go runAniListRefresh(userConfig, user, user.ListSync)
	}
}

func runAniListRefresh(userConfig *Config, user *User, target *AnimeListSync) {
	// Anything waiting on RefreshDone has to be released, failure or not.
	defer target.MarkRefreshDone()
	fresh, pullErr := pullAniListAnimeList(userConfig, user, false, "Failed to save refreshed anime list cache")
	if pullErr != nil {
		Log(fmt.Sprintf("Failed to refresh anime list in background: %v", pullErr))
		return
	}
	target.Replace(fresh, true)
}

// InitializeAniListUserAnimeList shows the cached list at once and refreshes
// it behind the menu. Start-up only waits on AniList when there is no cache.
func InitializeAniListUserAnimeList(userConfig *Config, user *User) error {
	cached, cacheErr := loadAniListAnimeListCache(userConfig.StoragePath, user.Id)
	if cacheErr == nil {
		showCachedList(user, cached)
		refreshAniListAnimeListInBackground(userConfig, user)
		return nil
	}
	if !errors.Is(cacheErr, fs.ErrNotExist) {
		Log(fmt.Sprintf("Failed to load anime list cache, fetching latest instead: %v", cacheErr))
	}

	fresh, pullErr := pullAniListAnimeList(userConfig, user, true, "Failed to save anime list cache")
	if pullErr != nil {
		return pullErr
	}
	user.AnimeList = fresh
	// Fetched just now: nothing left to refresh.
	user.ListSync = settledAnimeListSync(fresh)
	return nil
}

func RefreshAniListUserAnimeList(userConfig *Config, user *User) error {
	fresh, pullErr := pullAniListAnimeList(userConfig, user, false, "Failed to save anime list cache")
	if pullErr != nil {
		return pullErr
	}
	replaceUserList(user, fresh)
	return nil
}

// entryHasNewEpisodes is true when an episode past the viewer's progress has
// already aired.
func entryHasNewEpisodes(entry Entry, config *Config) bool {
	if !config.ShowNewEpisodes {
		return false
	}
	upcoming := entry.Media.NextAiringEpisode
	return upcoming != nil && upcoming.Episode > nextEpisodeFromProgress(entry.Progress)
}

// categoryRow is one show of a category as both list views draw it.
type categoryRow struct {
	key    string
	title  string
	entry  Entry
	resume string
	fresh  bool
}

// categoryRows is the visible part of a category, most recent first.
func categoryRows(source AnimeList, category string) []categoryRow {
	cfg := GetGlobalConfig()
	resumeAt := resumePointsByAnilistID(cfg)
	visible := sortEntriesByRecency(visibleEntriesByCategory(source, category, cfg), cfg)

	rows := make([]categoryRow, 0, len(visible))
	for _, entry := range visible {
		rows = append(rows, categoryRow{
			key:    strconv.Itoa(entry.Media.ID),
			title:  mediaDisplayTitle(entry.Media, cfg),
			entry:  entry,
			resume: resumeAt[entry.Media.ID],
			fresh:  entryHasNewEpisodes(entry, cfg),
		})
	}
	return rows
}

func buildCategorySelectionOptions(source AnimeList, category string) []SelectionOption {
	opts := []SelectionOption{}
	for _, row := range categoryRows(source, category) {
		opts = append(opts, SelectionOption{
			Key:            row.key,
			Label:          WithEpisodeCountsAndNote(row.title, row.entry, row.resume),
			Title:          row.title,
			Thumbnail:      row.entry.CoverImage,
			HasNewEpisodes: row.fresh,
		})
	}
	return opts
}

// buildCategoryPreviewOptions is the image-grid form. The grid is a map and
// so has no order, which is why each preview carries its rank.
func buildCategoryPreviewOptions(source AnimeList, category string) map[string]RofiSelectPreview {
	grid := map[string]RofiSelectPreview{}
	for rank, row := range categoryRows(source, category) {
		grid[row.key] = RofiSelectPreview{
			Title:          WithEpisodeCountsForGrid(row.title, row.entry, row.resume),
			Rank:           rank,
			HasNewEpisodes: row.fresh,
			CoverImage:     row.entry.CoverImage,
		}
	}
	return grid
}

package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const myAnimeListAPIBaseURL = "https://api.myanimelist.net/v2"

// Files kept in the storage dir: the last list seen, and the MyAnimeList to
// AniList id map.
const (
	myAnimeListCacheFile = "myanimelist_list_cache.json"
	trackingIDCacheFile  = "tracking_id_cache.json"
)

// aniListMalBatchSize caps the MyAnimeList ids per aliased AniList query.
const aniListMalBatchSize = 25

// myAnimeListAnime is the anime part of a list item.
type myAnimeListAnime struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	MainPicture struct {
		Medium string `json:"medium"`
		Large  string `json:"large"`
	} `json:"main_picture"`
	AlternativeTitles struct {
		En string `json:"en"`
		Ja string `json:"ja"`
	} `json:"alternative_titles"`
	NumEpisodes            int    `json:"num_episodes"`
	AverageEpisodeDuration int    `json:"average_episode_duration"`
	Status                 string `json:"status"`
	// NSFW is MyAnimeList's rating: "white", "gray" or "black" (explicit).
	NSFW string `json:"nsfw"`
}

// The animelist endpoint pages through items, each a show plus the user's
// entry for it.
type (
	// myAnimeListListStatus is one list entry, the shape list items and
	// my_list_status writes both return.
	myAnimeListListStatus struct {
		Status       string `json:"status"`
		IsRewatching bool   `json:"is_rewatching"`
		Score        int    `json:"score"`

		NumEpisodesWatched int `json:"num_episodes_watched"`
		NumTimesRewatched  int `json:"num_times_rewatched"`

		// YYYY-MM-DD, or shorter when only part of the date is known.
		StartDate  string `json:"start_date"`
		FinishDate string `json:"finish_date"`
		UpdatedAt  string `json:"updated_at"`
	}

	myAnimeListListItem struct {
		Node       myAnimeListAnime      `json:"node"`
		ListStatus myAnimeListListStatus `json:"list_status"`
	}

	myAnimeListListPage struct {
		Data   []myAnimeListListItem `json:"data"`
		Paging myAnimeListPaging     `json:"paging"`
	}

	myAnimeListPaging struct {
		// Next is the full URL of the following page, empty on the last.
		Next string `json:"next"`
	}
)

// trackingIDCache remembers which AniList id each MyAnimeList id maps to, so a
// list refresh only asks AniList about new titles.
type trackingIDCache struct {
	MalToAniList map[string]int `json:"mal_to_anilist"` // keyed by the MyAnimeList id as text
}

// aniListMalMedia is one aliased Media result of lookupAniListMediaByMalIDs.
type aniListMalMedia struct {
	ID       int    `json:"id"`
	Episodes int    `json:"episodes"`
	Duration int    `json:"duration"`
	Status   string `json:"status"`
	Format   string `json:"format"`
	IsAdult  bool   `json:"isAdult"`
	Title    struct {
		Romaji  string `json:"romaji"`
		English string `json:"english"`
		Native  string `json:"native"`
	} `json:"title"`
	CoverImage struct {
		Large string `json:"large"`
	} `json:"coverImage"`
}

type aniListMediaLookupResult struct {
	Media Media
	Cover string
}

func myAnimeListFile(storagePath, name string) string {
	return filepath.Join(os.ExpandEnv(storagePath), name)
}

// writeCacheFile replaces path through a temporary file, so a crash mid-write
// cannot leave a truncated cache behind.
func writeCacheFile(path string, data []byte) error {
	if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o755); mkdirErr != nil {
		return mkdirErr
	}
	staging := path + ".tmp"
	if writeErr := os.WriteFile(staging, data, 0o644); writeErr != nil {
		return writeErr
	}
	renameErr := os.Rename(staging, path)
	if renameErr != nil {
		_ = os.Remove(staging)
	}
	return renameErr
}

// myAnimeListRequest calls the MyAnimeList API. requestURL is a path under the
// API base or, for paging links, a full URL. form goes in the query for GET
// and in the body otherwise. A rejected token is renewed and the request sent
// once more.
func myAnimeListRequest(config *Config, method, requestURL string, form url.Values, out interface{}) error {
	accessToken, err := GetMyAnimeListAccessToken(config)
	if err != nil {
		return err
	}

	// A REST API answering a redirect is telling us the request failed, not
	// where to send it again. MyAnimeList redirects a rate-limited write to
	// /error.json, which never responds; following that turned a throttling
	// response into a minute-long hang and a failed launch. Surface the redirect
	// as the error it is.
	client := *sharedHTTPClient
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	resp, body, err := sendMyAnimeListRequest(&client, method, requestURL, form, accessToken)
	if err != nil {
		return err
	}
	if isInvalidTokenResponse(resp.StatusCode, body) {
		tokenPath := myAnimeListTokenPath(config)
		stored, err := loadToken(tokenPath)
		if err != nil {
			Log(fmt.Sprintf("Failed to load MyAnimeList token after unauthorized response, starting login: %v", err))
		}
		if accessToken, err = renewMyAnimeListAccessToken(config, tokenPath, stored, "unauthorized API response"); err != nil {
			return err
		}
		if resp, body, err = sendMyAnimeListRequest(&client, method, requestURL, form, accessToken); err != nil {
			return err
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := strings.TrimSpace(string(body))
		if location := resp.Header.Get("Location"); location != "" {
			// A redirect body is usually empty, and the destination is the
			// only useful part: MyAnimeList sends rate-limited writes to
			// /error.json.
			detail = "redirected to " + location
		}
		if detail == "" {
			detail = resp.Status
		}
		// The status code was missing entirely, so a throttled write
		// reported only an empty body and gave nothing to diagnose.
		return fmt.Errorf("MyAnimeList API %s %s failed with status %d: %s",
			method, requestURL, resp.StatusCode, detail)
	}

	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

// sendMyAnimeListRequest sends one request and returns the response with its
// body already read.
func sendMyAnimeListRequest(client *http.Client, method, requestURL string, form url.Values, token string) (*http.Response, []byte, error) {
	target := requestURL
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		target = myAnimeListAPIBaseURL + requestURL
	}

	var body io.Reader
	if method == http.MethodGet {
		if len(form) > 0 {
			separator := "?"
			if strings.Contains(target, "?") {
				separator = "&"
			}
			target += separator + form.Encode()
		}
	} else if form != nil {
		body = strings.NewReader(form.Encode())
	}

	req, err := http.NewRequest(method, target, body)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	data, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, data, readErr
}

// parseMyAnimeListDate reads a YYYY-MM-DD list date. MyAnimeList also sends
// partial dates like "2024-05"; those come back as the zero date, which the
// rest of the app treats as unknown.
func parseMyAnimeListDate(date string) FuzzyDate {
	fields := strings.Split(date, "-")
	if len(fields) != 3 {
		return FuzzyDate{}
	}
	var numbers [3]int
	for i, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil {
			return FuzzyDate{}
		}
		numbers[i] = n
	}
	parsed := FuzzyDate{Year: numbers[0], Month: numbers[1], Day: numbers[2]}
	if parsed.Year < 1 || parsed.Month < 1 || parsed.Month > 12 || parsed.Day < 1 || parsed.Day > 31 {
		return FuzzyDate{}
	}
	return parsed
}

// parseMyAnimeListTimestamp reads updated_at. RFC3339 parsing also accepts
// fractional seconds, so one layout covers both forms MyAnimeList sends.
func parseMyAnimeListTimestamp(stamp string) time.Time {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(stamp))
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

// formatMyAnimeListDate gives the form value for a date. An incomplete date is
// sent empty, which clears it on MyAnimeList.
func formatMyAnimeListDate(date FuzzyDate) string {
	if date.Year == 0 || date.Month == 0 || date.Day == 0 {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", date.Year, date.Month, date.Day)
}

var myAnimeListToAniListStatus = map[string]string{
	"watching":      "CURRENT",
	"completed":     "COMPLETED",
	"on_hold":       "PAUSED",
	"dropped":       "DROPPED",
	"plan_to_watch": "PLANNING",
}

// myAnimeListStatusToAniListStatus maps a list status to AniList's names.
// MyAnimeList has no rewatching status, only a flag on top of one.
func myAnimeListStatusToAniListStatus(malStatus string, rewatching bool) string {
	if rewatching {
		return "REPEATING"
	}
	if mapped, ok := myAnimeListToAniListStatus[strings.ToLower(malStatus)]; ok {
		return mapped
	}
	return "CURRENT"
}

// aniListStatusToMyAnimeListStatus is the reverse mapping. REPEATING becomes
// watching; callers set is_rewatching themselves.
func aniListStatusToMyAnimeListStatus(aniListStatus string) string {
	aniListStatus = strings.ToUpper(strings.TrimSpace(aniListStatus))
	for malStatus, mapped := range myAnimeListToAniListStatus {
		if mapped == aniListStatus {
			return malStatus
		}
	}
	return "watching"
}

// myAnimeListMediaStatusToAniListStatus maps an airing status. Anything else
// MyAnimeList reports has finished airing.
func myAnimeListMediaStatusToAniListStatus(airing string) string {
	if mapped, ok := myAnimeListAiringStatus[strings.ToLower(airing)]; ok {
		return mapped
	}
	return "FINISHED"
}

var myAnimeListAiringStatus = map[string]string{
	"currently_airing": "RELEASING",
	"not_yet_aired":    "NOT_YET_RELEASED",
}

// loadTrackingIDCache always returns a usable map, even with an error, so a
// broken cache only costs extra lookups.
func loadTrackingIDCache(dir string) (trackingIDCache, error) {
	var cache trackingIDCache
	data, err := os.ReadFile(myAnimeListFile(dir, trackingIDCacheFile))
	if err == nil {
		err = json.Unmarshal(data, &cache)
	}
	if err != nil || cache.MalToAniList == nil {
		cache.MalToAniList = make(map[string]int)
	}
	return cache, err
}

func saveTrackingIDCache(dir string, cache trackingIDCache) error {
	if cache.MalToAniList == nil {
		cache.MalToAniList = make(map[string]int)
	}
	data, marshalErr := json.MarshalIndent(cache, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}
	return writeCacheFile(myAnimeListFile(dir, trackingIDCacheFile), data)
}

// safeAniListString returns v when it is a string and "" otherwise, for fields
// of an untyped GraphQL response.
func safeAniListString(v any) string {
	text, _ := v.(string)
	return text
}

// lookupAniListMediaByMalIDs finds the AniList media for several MyAnimeList
// ids in one aliased query. Ids AniList does not know are left out.
func lookupAniListMediaByMalIDs(ids []int) (map[int]aniListMediaLookupResult, error) {
	found := make(map[int]aniListMediaLookupResult, len(ids))
	if len(ids) == 0 {
		return found, nil
	}

	var gql strings.Builder
	gql.WriteString("query {")
	for i, id := range ids {
		fmt.Fprintf(&gql, " m%d: Media(idMal: %d, type: ANIME) { id idMal episodes duration status format isAdult title { romaji english native } coverImage { large } }", i, id)
	}
	gql.WriteString(" }")

	// makePostRequest already retries a 429 itself; this covers AniList still
	// throttling after that, with a longer pause each time.
	const attempts = 3
	post := func() (map[string]any, error) {
		return makePostRequest("https://graphql.anilist.co", gql.String(), nil, map[string]string{"Content-Type": "application/json"})
	}
	response, err := post()
	for attempt := 1; attempt < attempts && err != nil && strings.Contains(err.Error(), "429"); attempt++ {
		time.Sleep(time.Duration(attempt) * 750 * time.Millisecond)
		response, err = post()
	}
	if err != nil {
		return nil, err
	}

	aliased, isObject := response["data"].(map[string]any)
	if !isObject {
		return nil, fmt.Errorf("invalid AniList batch response")
	}
	raw, err := json.Marshal(aliased)
	if err != nil {
		return nil, err
	}
	var byAlias map[string]*aniListMalMedia
	if err := json.Unmarshal(raw, &byAlias); err != nil {
		return nil, fmt.Errorf("invalid AniList batch response: %w", err)
	}
	for i, id := range ids {
		if media := byAlias[fmt.Sprintf("m%d", i)]; media != nil && media.ID != 0 {
			found[id] = media.lookupResult(id)
		}
	}
	return found, nil
}

func (m *aniListMalMedia) lookupResult(malID int) aniListMediaLookupResult {
	result := aniListMediaLookupResult{Cover: m.CoverImage.Large}
	result.Media = Media{ID: m.ID, MalID: malID, Episodes: m.Episodes, Duration: m.Duration, Status: m.Status, Format: m.Format, IsAdult: m.IsAdult}
	result.Media.Title = AnimeTitle{Romaji: m.Title.Romaji, English: m.Title.English, Japanese: m.Title.Native}
	return result
}

// myAnimeListEntry builds the list entry for item. AniList's metadata wins;
// MyAnimeList fills whatever AniList left empty.
func myAnimeListEntry(item myAnimeListListItem, lookup aniListMediaLookupResult) Entry {
	node, status := item.Node, item.ListStatus
	media := lookup.Media
	if node.NSFW == "black" {
		media.IsAdult = true
	}
	if media.Title.English == "" {
		media.Title.English = node.AlternativeTitles.En
	}
	if media.Title.Romaji == "" {
		media.Title.Romaji = node.Title
	}
	if media.Title.Japanese == "" {
		media.Title.Japanese = node.AlternativeTitles.Ja
	}
	if media.Episodes == 0 {
		media.Episodes = node.NumEpisodes
	}
	if media.Duration == 0 && node.AverageEpisodeDuration > 0 {
		media.Duration = node.AverageEpisodeDuration / 60 // seconds to minutes
	}
	if media.Status == "" {
		media.Status = myAnimeListMediaStatusToAniListStatus(node.Status)
	}

	cover := lookup.Cover
	if cover == "" {
		cover = node.MainPicture.Large
	}
	return Entry{
		Media:       media,
		Progress:    status.NumEpisodesWatched,
		Repeat:      status.NumTimesRewatched,
		Score:       float64(status.Score),
		Status:      myAnimeListStatusToAniListStatus(status.Status, status.IsRewatching),
		StartedAt:   parseMyAnimeListDate(status.StartDate),
		CompletedAt: parseMyAnimeListDate(status.FinishDate),
		CoverImage:  cover,
		UpdatedAt:   parseMyAnimeListTimestamp(status.UpdatedAt),
	}
}

func fileEntryByStatus(list *AnimeList, entry Entry) {
	switch entry.Status {
	case "CURRENT":
		list.Watching = append(list.Watching, entry)
	case "COMPLETED":
		list.Completed = append(list.Completed, entry)
	case "PAUSED":
		list.Paused = append(list.Paused, entry)
	case "DROPPED":
		list.Dropped = append(list.Dropped, entry)
	case "PLANNING":
		list.Planning = append(list.Planning, entry)
	case "REPEATING":
		list.Rewatching = append(list.Rewatching, entry)
	}
}

// resolveMyAnimeListPage maps each item of a list page to AniList media. Ids in
// the cache need no lookup; the rest are asked for in batches and added to it.
func resolveMyAnimeListPage(items []myAnimeListListItem, cache trackingIDCache) map[int]aniListMediaLookupResult {
	resolved := make(map[int]aniListMediaLookupResult, len(items))
	var unresolved []int
	for _, item := range items {
		malID := item.Node.ID
		if aniListID := cache.MalToAniList[strconv.Itoa(malID)]; aniListID != 0 {
			resolved[malID] = aniListMediaLookupResult{Media: Media{ID: aniListID, MalID: malID}}
		} else {
			unresolved = append(unresolved, malID)
		}
	}

	for start := 0; start < len(unresolved); start += aniListMalBatchSize {
		batch := unresolved[start:min(start+aniListMalBatchSize, len(unresolved))]
		// A failed batch leaves its titles out of this refresh only; the
		// next one asks again.
		found, err := lookupAniListMediaByMalIDs(batch)
		if err != nil {
			Log(fmt.Sprintf("AniList batch lookup failed for MAL IDs %v: %v", batch, err))
		}
		for malID, lookup := range found {
			resolved[malID] = lookup
			cache.MalToAniList[strconv.Itoa(malID)] = lookup.Media.ID
		}
	}
	return resolved
}

// myAnimeListListQuery asks for the first list page with every field the
// entries need. Later pages come from the paging link, which carries it.
var myAnimeListListQuery = url.Values{
	"limit":  {"1000"},
	"fields": {"list_status,num_episodes,average_episode_duration,alternative_titles,main_picture,status,nsfw"},
	"sort":   {"list_updated_at"},
}

// FetchLatestMyAnimeList downloads the whole MyAnimeList list. The app keys
// everything by AniList id, so a title AniList cannot match is skipped. With a
// user, its MyAnimeList id and name are filled in too.
func FetchLatestMyAnimeList(config *Config, user *User) (AnimeList, error) {
	cache, cacheErr := loadTrackingIDCache(config.StoragePath)
	if cacheErr != nil && !os.IsNotExist(cacheErr) {
		Log(fmt.Sprintf("Failed to load MAL tracking ID cache: %v", cacheErr))
	}

	var list AnimeList
	pageURL, query := "/users/@me/animelist", myAnimeListListQuery
	for pageURL != "" {
		var page myAnimeListListPage
		if err := myAnimeListRequest(config, http.MethodGet, pageURL, query, &page); err != nil {
			return AnimeList{}, err
		}
		pageURL, query = page.Paging.Next, nil

		resolved := resolveMyAnimeListPage(page.Data, cache)
		for _, item := range page.Data {
			if lookup, found := resolved[item.Node.ID]; found {
				fileEntryByStatus(&list, myAnimeListEntry(item, lookup))
			} else {
				Log(fmt.Sprintf("Skipping MAL anime %d: no AniList match", item.Node.ID))
			}
		}
		if err := saveTrackingIDCache(config.StoragePath, cache); err != nil {
			Log(fmt.Sprintf("Failed to save MAL tracking ID cache: %v", err))
		}
	}

	if user == nil {
		return list, nil
	}
	// The profile is a nicety; the list is still good without it.
	if id, name, err := fetchMyAnimeListProfile(config); err == nil {
		user.Id, user.Username = id, name
	}
	return list, nil
}

func fetchMyAnimeListProfile(config *Config) (int, string, error) {
	var profile struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	err := myAnimeListRequest(config, http.MethodGet, "/users/@me", url.Values{"fields": {"id,name"}}, &profile)
	return profile.ID, profile.Name, err
}

// loadMyAnimeListCache reads the cached list, refusing one saved for another
// account so a sign-in switch never shows the previous user's list.
func loadMyAnimeListCache(dir string, userID int) (animeListCachePayload, error) {
	raw, readErr := os.ReadFile(myAnimeListFile(dir, myAnimeListCacheFile))
	if readErr != nil {
		return animeListCachePayload{}, readErr
	}
	var cached animeListCachePayload
	if decodeErr := json.Unmarshal(raw, &cached); decodeErr != nil {
		return animeListCachePayload{}, decodeErr
	}
	if !cached.ownedBy(userID) {
		return animeListCachePayload{}, errors.New("anime list cache belongs to a different MyAnimeList user")
	}
	return cached, nil
}

func saveMyAnimeListCache(dir string, userID int, list AnimeList) error {
	encoded, encodeErr := encodeListCache(userID, list)
	if encodeErr != nil {
		return encodeErr
	}
	return writeCacheFile(myAnimeListFile(dir, myAnimeListCacheFile), encoded)
}

func saveMyAnimeListCacheOrLog(config *Config, user *User, list AnimeList) {
	if err := saveMyAnimeListCache(config.StoragePath, user.Id, list); err != nil {
		Log(fmt.Sprintf("Failed to save MyAnimeList anime list cache: %v", err))
	}
}

// refreshMyAnimeListInBackground runs on its own goroutine after a cached list
// was shown, and hands the fresh list to user.ListSync.
func refreshMyAnimeListInBackground(config *Config, user *User) {
	target := user.ListSync
	defer target.MarkRefreshDone()

	latest, fetchErr := FetchLatestMyAnimeList(config, user)
	if fetchErr != nil {
		Log(fmt.Sprintf("Background MyAnimeList refresh failed: %v", fetchErr))
		return
	}
	saveMyAnimeListCacheOrLog(config, user, latest)
	target.Replace(latest, true)
}

// InitializeMyAnimeListUserAnimeList loads the list for user. A cached list is
// shown straight away and refreshed in the background; without one the list is
// fetched before returning.
func InitializeMyAnimeListUserAnimeList(config *Config, user *User) error {
	cached, cacheErr := loadMyAnimeListCache(config.StoragePath, user.Id)
	if cacheErr == nil {
		showCachedList(user, cached)
		go refreshMyAnimeListInBackground(config, user)
		return nil
	}

	latest, fetchErr := FetchLatestMyAnimeList(config, user)
	if fetchErr != nil {
		return fetchErr
	}
	user.AnimeList = latest
	user.ListSync = settledAnimeListSync(latest)
	saveMyAnimeListCacheOrLog(config, user, latest)
	return nil
}

// RefreshMyAnimeListUserAnimeList fetches the list now and replaces user's.
func RefreshMyAnimeListUserAnimeList(config *Config, user *User) error {
	latest, fetchErr := FetchLatestMyAnimeList(config, user)
	if fetchErr != nil {
		return fetchErr
	}
	replaceUserList(user, latest)
	saveMyAnimeListCacheOrLog(config, user, latest)
	return nil
}

// myAnimeListStatusPath is the my_list_status endpoint for one show.
func myAnimeListStatusPath(malID int) string {
	return "/anime/" + strconv.Itoa(malID) + "/my_list_status"
}

// updateMyAnimeListListStatus writes fields of the list entry for malID. Blank
// values are dropped so they don't clear anything, except the dates, where
// blank is how a date is cleared.
func updateMyAnimeListListStatus(config *Config, malID int, fields map[string]string) error {
	if isAniListPrivateMyAnimeListID(malID) {
		Log(fmt.Sprintf("MyAnimeList: not writing %d, it is private on AniList", malID))
		return nil
	}
	form := url.Values{}
	for key, value := range fields {
		clearsDate := key == "start_date" || key == "finish_date"
		if clearsDate || strings.TrimSpace(value) != "" {
			form.Set(key, value)
		}
	}
	var echoed myAnimeListListStatus
	return myAnimeListRequest(config, http.MethodPut, myAnimeListStatusPath(malID), form, &echoed)
}

// writeMyAnimeListAndReport writes fields and, once that worked, prints done.
func writeMyAnimeListAndReport(config *Config, malID int, fields map[string]string, done string) error {
	if writeErr := updateMyAnimeListListStatus(config, malID, fields); writeErr != nil {
		return writeErr
	}
	Out(done)
	return nil
}

func updateMyAnimeListProgress(config *Config, malID int, progress int) error {
	return writeMyAnimeListAndReport(config, malID,
		map[string]string{"num_watched_episodes": strconv.Itoa(progress)},
		fmt.Sprint("Anime progress updated! Latest watched episode: ", progress))
}

// myAnimeListStatusFields are the form fields that set an AniList status.
func myAnimeListStatusFields(aniListStatus string) map[string]string {
	rewatching := strings.EqualFold(aniListStatus, "REPEATING")
	return map[string]string{
		"status":        aniListStatusToMyAnimeListStatus(aniListStatus),
		"is_rewatching": strconv.FormatBool(rewatching),
	}
}

func updateMyAnimeListStatus(config *Config, malID int, status string) error {
	return writeMyAnimeListAndReport(config, malID, myAnimeListStatusFields(status),
		"Anime status updated to: "+status)
}

// myAnimeListScore rounds a 0-10 score to the whole number MyAnimeList takes.
func myAnimeListScore(score float64) int {
	return int(score + 0.5)
}

func rateMyAnimeListAnime(config *Config, malID int) error {
	score, cancelled, promptErr := promptAnimeScoreValue()
	if promptErr != nil || cancelled {
		return promptErr
	}
	return rateMyAnimeListAnimeWithScore(config, malID, myAnimeListScore(score))
}

func rateMyAnimeListAnimeWithScore(config *Config, malID int, score int) error {
	return writeMyAnimeListAndReport(config, malID,
		map[string]string{"score": strconv.Itoa(score)},
		fmt.Sprintf("Successfully rated anime (malId: %d) with score: %d", malID, score))
}

func addMyAnimeListAnimeToList(config *Config, malID int, status string) error {
	return writeMyAnimeListAndReport(config, malID, myAnimeListStatusFields(status),
		"Anime added to: "+status)
}

// completeMyAnimeListRewatch marks a finished rewatch as completed and counts
// it. A missing start date is set to today along with the finish date.
func completeMyAnimeListRewatch(config *Config, malID int, anime Anime) error {
	today := currentFuzzyDate()
	began := anime.StartedAt
	if began == (FuzzyDate{}) {
		began = today
	}
	completion := myAnimeListStatusFields("COMPLETED")
	completion["num_watched_episodes"] = strconv.Itoa(anime.Ep.Number)
	completion["start_date"] = formatMyAnimeListDate(began)
	completion["finish_date"] = formatMyAnimeListDate(today)
	// The count goes in a second write, after the completion.
	count := map[string]string{"num_times_rewatched": strconv.Itoa(anime.Repeat + 1)}
	for _, fields := range []map[string]string{completion, count} {
		if writeErr := updateMyAnimeListListStatus(config, malID, fields); writeErr != nil {
			return writeErr
		}
	}
	return nil
}

func deleteMyAnimeListEntry(config *Config, malID int) error {
	return myAnimeListRequest(config, http.MethodDelete, myAnimeListStatusPath(malID), nil, nil)
}

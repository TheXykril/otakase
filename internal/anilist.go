package internal

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// anilistEndpoint is a variable only so tests can point it at a local server.
var anilistEndpoint = "https://graphql.anilist.co"

// aniListAttempts is how many times one request is sent while AniList keeps
// answering 429. The last 429 is returned to the caller as an error.
const aniListAttempts = 5

// aniListSleep waits out a 429. Tests swap it so a retry costs nothing.
var aniListSleep = time.Sleep

var aniListStatusLabels = map[string]string{
	"CURRENT":   "Currently Watching",
	"COMPLETED": "Completed",
	"PAUSED":    "On Hold",
	"DROPPED":   "Dropped",
	"PLANNING":  "Plan to Watch",
	"REPEATING": "Rewatching",
}

// The shapes below mirror the GraphQL selections. Fields a query does not ask
// for simply stay zero.

type aniListTitle struct {
	Romaji  string `json:"romaji"`
	English string `json:"english"`
	Native  string `json:"native"`
}

func (t aniListTitle) animeTitle() AnimeTitle {
	return AnimeTitle{Romaji: t.Romaji, English: t.English, Japanese: t.Native}
}

type aniListMedia struct {
	ID         int          `json:"id"`
	IDMal      int          `json:"idMal"`
	IsAdult    bool         `json:"isAdult"`
	Episodes   int          `json:"episodes"`
	Duration   int          `json:"duration"`
	Format     string       `json:"format"`
	Status     string       `json:"status"`
	SiteURL    string       `json:"siteUrl"`
	Title      aniListTitle `json:"title"`
	CoverImage struct {
		Large string `json:"large"`
	} `json:"coverImage"`
	NextAiringEpisode *struct {
		Episode         int `json:"episode"`
		TimeUntilAiring int `json:"timeUntilAiring"`
	} `json:"nextAiringEpisode"`
}

type aniListMediaResponse struct {
	Data *struct {
		Media *aniListMedia `json:"Media"`
	} `json:"data"`
}

type aniListListEntry struct {
	ID          int           `json:"id"`
	Private     bool          `json:"private"`
	Status      string        `json:"status"`
	Score       float64       `json:"score"`
	Progress    int           `json:"progress"`
	Repeat      int           `json:"repeat"`
	UpdatedAt   int64         `json:"updatedAt"`
	StartedAt   FuzzyDate     `json:"startedAt"`
	CompletedAt FuzzyDate     `json:"completedAt"`
	Media       *aniListMedia `json:"media"`
}

type aniListCollectionResponse struct {
	Data *struct {
		MediaListCollection *struct {
			Lists []aniListList `json:"lists"`
		} `json:"MediaListCollection"`
	} `json:"data"`
}

type aniListList struct {
	Name         string             `json:"name"`
	IsCustomList bool               `json:"isCustomList"`
	Entries      []aniListListEntry `json:"entries"`
}

const aniListTitleFields = `title { romaji english native }`

const aniListSearchQuery = `query ($search: String, $isAdult: Boolean) {
	Page(page: 1, perPage: 50) {
		media(search: $search, type: ANIME, isAdult: $isAdult) {
			id episodes ` + aniListTitleFields + `%s
		}
	}
}`

// The list query is the same with or without covers; only the preview grid
// needs them, and they make the answer noticeably larger.
const aniListCollectionQuery = `query ($userId: Int, $type: MediaType) {
	MediaListCollection(userId: $userId, type: $type) {
		lists {
			name isCustomList
			entries {
				id private status score progress repeat updatedAt
				startedAt { year month day }
				completedAt { year month day }
				media {
					id idMal isAdult episodes duration format status%s
					` + aniListTitleFields + `
					nextAiringEpisode { episode timeUntilAiring }
				}
			}
		}
	}
}`

const aniListCoverField = `
					coverImage { large }`

func aniListSearchQueryFor(withCover bool) string {
	return fmt.Sprintf(aniListSearchQuery, coverFieldIf(withCover))
}

func aniListCollectionQueryFor(withCover bool) string {
	return fmt.Sprintf(aniListCollectionQuery, coverFieldIf(withCover))
}

func coverFieldIf(withCover bool) string {
	if withCover {
		return aniListCoverField
	}
	return ""
}

const aniListSaveEntryMutation = `mutation (
	$mediaId: Int, $status: MediaListStatus, $progress: Int, $repeat: Int,
	$score: Float, $startedAt: FuzzyDateInput, $completedAt: FuzzyDateInput
) {
	SaveMediaListEntry(
		mediaId: $mediaId, status: $status, progress: $progress, repeat: $repeat,
		score: $score, startedAt: $startedAt, completedAt: $completedAt
	) {
		id status progress repeat
		startedAt { year month day }
		completedAt { year month day }
	}
}`

const aniListSetStatusMutation = `mutation ($mediaId: Int, $status: MediaListStatus) {
	SaveMediaListEntry(mediaId: $mediaId, status: $status) { id status }
}`

const aniListSetScoreMutation = `mutation ($mediaId: Int, $score: Float) {
	SaveMediaListEntry(mediaId: $mediaId, score: $score) { id mediaId score }
}`

// mediaDisplayTitle names a show for a list row: English when that is the
// configured language, else the first title it has, else its AniList ID.
func mediaDisplayTitle(media Media, config *Config) string {
	names := media.Title
	if names.English != "" && useEnglishAnimeNames(config) {
		return names.English
	}
	fallback := "Unknown Anime"
	if id := media.ID; id != 0 {
		fallback = strconv.Itoa(id)
	}
	return cmp.Or(names.Romaji, names.English, names.Japanese, fallback)
}

// levenshtein is the case-insensitive edit distance, used to put the closest
// search results first. Only two rows of the table are ever needed.
func levenshtein(a, b string) int {
	ar := []rune(strings.ToLower(a))
	br := []rune(strings.ToLower(b))
	if len(ar) == 0 {
		return len(br)
	}

	prev := make([]int, len(br)+1)
	next := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i, ca := range ar {
		next[0] = i + 1
		for j, cb := range br {
			substitute := prev[j]
			if ca != cb {
				substitute++
			}
			next[j+1] = min(prev[j+1]+1, next[j]+1, substitute)
		}
		prev, next = next, prev
	}
	return prev[len(br)]
}

func aniListHTTPClient() *http.Client {
	if sharedHTTPClient != nil {
		return sharedHTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func aniListRetryDelay(resp *http.Response, attempt int) time.Duration {
	if resp != nil {
		seconds, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After")))
		if err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return time.Duration(attempt+1) * 2 * time.Second
}

// statusError builds the error for a non-200 answer. The two wordings are
// kept apart because callers look for "status 429" in the general one.
type statusError func(code int, body []byte) error

func searchStatusError(code int, body []byte) error {
	return fmt.Errorf("failed to search for anime. Status Code: %d, Response: %s", code, body)
}

func requestStatusError(code int, body []byte) error {
	return fmt.Errorf("failed with status %d: %s", code, body)
}

// postAniList sends one GraphQL body and returns the raw answer. The token is
// swapped for the stored AniList one when there is one (the caller may hold
// the MyAnimeList token), and an "invalid token" answer gets one renewal and
// a second try.
func postAniList(url string, body []byte, headers map[string]string, token string, onStatus statusError) ([]byte, error) {
	token = anilistTokenForAPI(nil, token)
	data, err := sendAniList(url, body, headers, token, onStatus)
	if err == nil || token == "" || !isInvalidTokenError(err) {
		return data, err
	}
	renewed, ok, renewErr := tryRenewAniListTokenForAPI(token, "invalid token")
	if renewErr != nil {
		return nil, renewErr
	}
	if !ok {
		return nil, err
	}
	return sendAniList(url, body, headers, renewed, onStatus)
}

// newAniListRequest builds one JSON POST. Extra headers override the
// defaults; a non-empty token is sent as a bearer token on top of them.
func newAniListRequest(endpoint string, body []byte, headers map[string]string, token string) (*http.Request, error) {
	req, buildErr := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if buildErr != nil {
		return nil, fmt.Errorf("failed to create request: %w", buildErr)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	if token == "" {
		return req, nil
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return req, nil
}

// roundTripAniList sends req and returns the response with its body read
// and closed.
func roundTripAniList(client *http.Client, req *http.Request) (*http.Response, []byte, error) {
	resp, sendErr := client.Do(req)
	if sendErr != nil {
		return nil, nil, fmt.Errorf("failed to send request: %w", sendErr)
	}
	defer resp.Body.Close()
	payload, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, nil, fmt.Errorf("failed to read response body: %w", readErr)
	}
	return resp, payload, nil
}

// sendAniList retries only on 429; any other failure is returned at once.
func sendAniList(endpoint string, body []byte, headers map[string]string, token string, onStatus statusError) ([]byte, error) {
	client := aniListHTTPClient()
	for attempt := 1; ; attempt++ {
		req, buildErr := newAniListRequest(endpoint, body, headers, token)
		if buildErr != nil {
			return nil, buildErr
		}
		resp, payload, tripErr := roundTripAniList(client, req)
		if tripErr != nil {
			return nil, tripErr
		}

		switch {
		case resp.StatusCode == http.StatusTooManyRequests && attempt < aniListAttempts:
			aniListSleep(aniListRetryDelay(resp, attempt-1))
		case resp.StatusCode != http.StatusOK:
			return nil, onStatus(resp.StatusCode, payload)
		default:
			if gqlErr := graphQLErrors(payload); gqlErr != nil {
				return nil, gqlErr
			}
			return payload, nil
		}
	}
}

// graphQLErrors reports the "errors" member of a 200 answer. AniList sends
// an expired token this way too, which is what triggers the renewal above.
// A body that is not JSON is left for the caller's decode to reject.
func graphQLErrors(data []byte) error {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(data, &envelope) != nil {
		return nil
	}
	raw, ok := envelope["errors"]
	if !ok {
		return nil
	}
	var compact bytes.Buffer
	if json.Compact(&compact, raw) != nil {
		compact.Write(raw)
	}
	return fmt.Errorf("graphql errors: %s", strings.TrimSpace(compact.String()))
}

// splitBearer pulls a bearer token out of caller-supplied headers so it goes
// through the same token resolution as everything else.
func splitBearer(headers map[string]string) (map[string]string, string) {
	rest := make(map[string]string, len(headers))
	token := ""
	for key, value := range headers {
		if key == "Authorization" && strings.HasPrefix(value, "Bearer ") {
			token = strings.TrimPrefix(value, "Bearer ")
			continue
		}
		rest[key] = value
	}
	return rest, token
}

// graphQLRequest is the body of every AniList call.
type graphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

func encodeGraphQL(query string, variables map[string]interface{}) ([]byte, error) {
	encoded, marshalErr := json.Marshal(graphQLRequest{Query: query, Variables: variables})
	if marshalErr != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", marshalErr)
	}
	return encoded, nil
}

// decodeAniListStrict unmarshals an answer, wrapping any failure the same
// way every caller reports it.
func decodeAniListStrict(payload []byte, out interface{}) error {
	if decodeErr := json.Unmarshal(payload, out); decodeErr != nil {
		return fmt.Errorf("failed to unmarshal response: %w", decodeErr)
	}
	return nil
}

// makePostRequest is the untyped entry point other files use. New code here
// decodes into structs through aniListQuery instead.
func makePostRequest(endpoint, query string, variables map[string]interface{}, headers map[string]string) (map[string]interface{}, error) {
	body, encodeErr := encodeGraphQL(query, variables)
	if encodeErr != nil {
		return nil, encodeErr
	}
	rest, bearer := splitBearer(headers)
	payload, postErr := postAniList(endpoint, body, rest, bearer, requestStatusError)
	if postErr != nil {
		return nil, postErr
	}
	var decoded map[string]interface{}
	if decodeErr := decodeAniListStrict(payload, &decoded); decodeErr != nil {
		return nil, decodeErr
	}
	return decoded, nil
}

// doAniListSearchRequest posts a prepared body and returns the raw answer,
// with search wording on a failed status.
func doAniListSearchRequest(url string, requestBody []byte, token string) ([]byte, error) {
	return postAniList(url, requestBody, nil, token, searchStatusError)
}

// aniListQuery runs one query against AniList and decodes the answer into out.
func aniListQuery(query string, variables map[string]interface{}, token string, out interface{}) error {
	body, err := encodeGraphQL(query, variables)
	if err != nil {
		return err
	}
	data, err := postAniList(anilistEndpoint, body, nil, token, requestStatusError)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return decodeAniList(data, out)
}

// decodeAniList is lenient about a field of the wrong type: the rest of the
// answer is still used and that field stays zero, which is how the old
// map-walking code behaved.
func decodeAniList(data []byte, out interface{}) error {
	err := json.Unmarshal(data, out)
	var typeErr *json.UnmarshalTypeError
	if err != nil && !errors.As(err, &typeErr) {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return nil
}

type aniListSearchHit struct {
	id       int
	title    string
	cover    string
	episodes int
}

// searchAniList returns the ten results whose title is closest to query.
func searchAniList(query, token string, withCover bool) ([]aniListSearchHit, error) {
	body, encodeErr := encodeGraphQL(aniListSearchQueryFor(withCover), searchVariables(query))
	if encodeErr != nil {
		return nil, encodeErr
	}
	payload, searchErr := doAniListSearchRequest(anilistEndpoint, body, token)
	if searchErr != nil {
		return nil, searchErr
	}
	var answer struct {
		Data ResponseData `json:"data"`
	}
	if decodeErr := decodeAniListStrict(payload, &answer); decodeErr != nil {
		return nil, decodeErr
	}

	media := answer.Data.Page.Media
	hits := make([]aniListSearchHit, 0, len(media))
	distance := make([]int, 0, len(media))
	for _, anime := range media {
		title := cmp.Or(anime.Title.English, anime.Title.Romaji)
		hits = append(hits, aniListSearchHit{anime.ID, title, anime.CoverImage.Large, anime.Episodes})
		distance = append(distance, levenshtein(title, query))
	}
	sort.Sort(byDistance{hits, distance})
	if len(hits) > 10 {
		hits = hits[:10]
	}
	return hits, nil
}

type byDistance struct {
	hits     []aniListSearchHit
	distance []int
}

func (s byDistance) Len() int           { return len(s.hits) }
func (s byDistance) Less(i, j int) bool { return s.distance[i] < s.distance[j] }
func (s byDistance) Swap(i, j int) {
	s.hits[i], s.hits[j] = s.hits[j], s.hits[i]
	s.distance[i], s.distance[j] = s.distance[j], s.distance[i]
}

// SearchAnimeAnilistPreview is SearchAnimeAnilist with cover images, keyed by
// AniList ID for the image grid.
func SearchAnimeAnilistPreview(query, token string) (map[string]RofiSelectPreview, error) {
	hits, err := searchAniList(query, token, true)
	if err != nil {
		return nil, err
	}
	previews := make(map[string]RofiSelectPreview, len(hits))
	for _, hit := range hits {
		previews[strconv.Itoa(hit.id)] = RofiSelectPreview{
			Title:      FormatAnimeSearchLabel(hit.title, hit.episodes),
			CoverImage: hit.cover,
		}
	}
	return previews, nil
}

// SearchAnimeAnilist searches AniList and returns the closest ten matches,
// keyed by AniList ID.
func SearchAnimeAnilist(query, token string) ([]SelectionOption, error) {
	hits, err := searchAniList(query, token, false)
	if err != nil {
		return nil, err
	}
	var options []SelectionOption
	for _, hit := range hits {
		options = append(options, SelectionOption{
			Key:   strconv.Itoa(hit.id),
			Label: FormatAnimeSearchLabel(hit.title, hit.episodes),
			Title: hit.title,
		})
	}
	return options, nil
}

// GetAnilistUserID returns the signed-in viewer's ID and name.
func GetAnilistUserID(token string) (int, string, error) {
	var answer struct {
		Data *struct {
			Viewer *struct {
				ID   *int   `json:"id"`
				Name string `json:"name"`
			} `json:"Viewer"`
		} `json:"data"`
	}
	if queryErr := aniListQuery(`query { Viewer { id name } }`, nil, token, &answer); queryErr != nil {
		return 0, "", queryErr
	}

	// The first piece missing, outermost first, is the one reported.
	missing := ""
	switch viewer := answer.Data; {
	case viewer == nil:
		missing = "data"
	case viewer.Viewer == nil:
		missing = "Viewer"
	case viewer.Viewer.ID == nil:
		missing = "id"
	case viewer.Viewer.Name == "":
		missing = "name"
	}
	if missing != "" {
		return 0, "", fmt.Errorf("invalid AniList viewer response: %s field missing", missing)
	}
	me := answer.Data.Viewer
	return *me.ID, me.Name, nil
}

// setAniListStatus puts animeID on the status list, wrapping a failure with
// failure as its prefix.
func setAniListStatus(animeID int, status, token, failure string) error {
	vars := map[string]interface{}{"mediaId": animeID, "status": status}
	if queryErr := aniListQuery(aniListSetStatusMutation, vars, token, nil); queryErr != nil {
		return fmt.Errorf("%s: %w", failure, queryErr)
	}
	return nil
}

func AddAniListAnimeToWatchingList(mediaID int, token string) error {
	if addErr := setAniListStatus(mediaID, "CURRENT", token, "failed to add anime"); addErr != nil {
		return addErr
	}
	Out(fmt.Sprintf("Anime with ID %d has been added to your watching list.", mediaID))
	return nil
}

// AddAniListAnimeToList puts an anime on the list for status (CURRENT,
// PLANNING, PAUSED, DROPPED, ...).
func AddAniListAnimeToList(mediaID int, status string, token string) error {
	if addErr := setAniListStatus(mediaID, status, token, "failed to add anime to list"); addErr != nil {
		return addErr
	}
	Out("Anime added to: " + aniListStatusLabels[status])
	return nil
}

// fetchAniListMedia looks one media up by ID. A missing Media means AniList
// has no such ID, which callers report as "not found".
func fetchAniListMedia(query string, id int, token string) (*aniListMedia, error) {
	var response aniListMediaResponse
	if err := aniListQuery(query, map[string]interface{}{"id": id}, token, &response); err != nil {
		return nil, err
	}
	if response.Data == nil {
		return nil, fmt.Errorf("invalid AniList media response: data field missing")
	}
	if response.Data.Media == nil {
		return nil, fmt.Errorf("AniList media %d not found", id)
	}
	return response.Data.Media, nil
}

// GetAnimeMalID maps an AniList media ID to its MyAnimeList ID.
func GetAnimeMalID(mediaID int) (int, error) {
	found, lookupErr := fetchAniListMedia(`query ($id: Int) { Media(id: $id) { idMal } }`, mediaID, "")
	switch {
	case lookupErr != nil:
		return 0, lookupErr
	case found.IDMal == 0:
		return 0, fmt.Errorf("AniList media %d has no MyAnimeList ID", mediaID)
	}
	return found.IDMal, nil
}

// GetAnimeIDAndImage returns the MyAnimeList ID (0 when there is none) and
// the cover URL for an AniList media ID.
func GetAnimeIDAndImage(mediaID int) (int, string, error) {
	const query = `query ($id: Int) { Media(id: $id) { idMal coverImage { large } } }`
	found, lookupErr := fetchAniListMedia(query, mediaID, "")
	if lookupErr != nil {
		return 0, "", lookupErr
	}
	return found.IDMal, found.CoverImage.Large, nil
}

// GetUserData returns the viewer's whole anime list collection, undecoded,
// for ParseAnimeList.
func GetUserData(token string, viewerID int) (map[string]interface{}, error) {
	vars := map[string]interface{}{"userId": viewerID, "type": "ANIME"}
	return makePostRequest(anilistEndpoint, aniListCollectionQueryFor(false), vars, bearerHeader(token))
}

func bearerHeader(token string) map[string]string {
	if token == "" {
		return nil
	}
	return map[string]string{"Authorization": "Bearer " + token}
}

func UpdateAniListAnimeProgress(accessToken string, mediaID, progress int) error {
	saveErr := SaveAniListAnimeListEntry(accessToken, mediaID, nil, &progress, nil, nil, nil, nil)
	if saveErr == nil {
		Out(fmt.Sprint("Anime progress updated! Latest watched episode: ", progress))
	}
	return saveErr
}

func UpdateAniListAnimeStatus(accessToken string, mediaID int, status string) error {
	if saveErr := SaveAniListAnimeListEntry(accessToken, mediaID, &status, nil, nil, nil, nil, nil); saveErr != nil {
		return fmt.Errorf("failed to update anime status: %w", saveErr)
	}
	Out("Anime status updated to: " + aniListStatusLabels[status])
	return nil
}

// SaveAniListAnimeListEntry writes the given fields of a list entry; a nil
// argument leaves that field as it is on AniList. A zero date is sent as null,
// which clears the date there.
func SaveAniListAnimeListEntry(token string, mediaID int, status *string, progress, repeat *int, score *float64, startedAt, completedAt *FuzzyDate) error {
	vars := map[string]interface{}{"mediaId": mediaID}
	setVariable(vars, "status", status)
	setVariable(vars, "progress", progress)
	setVariable(vars, "repeat", repeat)
	setVariable(vars, "score", score)
	setDateVariable(vars, "startedAt", startedAt)
	setDateVariable(vars, "completedAt", completedAt)
	return aniListQuery(aniListSaveEntryMutation, vars, token, nil)
}

func setVariable[T any](variables map[string]interface{}, key string, value *T) {
	if value != nil {
		variables[key] = *value
	}
}

// setDateVariable sends a given date, or null for the zero date.
func setDateVariable(variables map[string]interface{}, key string, date *FuzzyDate) {
	if date != nil {
		variables[key] = fuzzyDateInput(*date)
	}
}

func fuzzyDateInput(date FuzzyDate) interface{} {
	if date == (FuzzyDate{}) {
		return nil
	}
	return date
}

// anilistRewatchCompletion is what finishing a rewatch writes to the entry: one
// more repeat than it already had, and the date the viewer actually started
// rather than today.
//
// Separated from the request so it can be tested without a network -- and
// because both values come off the Anime struct, which is exactly what a
// process that did not load the entry itself is liable to be missing.
func anilistRewatchCompletion(anime Anime, completedAt FuzzyDate) (repeat int, startedAt FuzzyDate) {
	repeat = anime.Repeat + 1
	startedAt = anime.StartedAt
	if startedAt == (FuzzyDate{}) {
		startedAt = completedAt
	}
	return repeat, startedAt
}

// CompleteAniListAnimeRewatch closes a rewatch as completed today, at the
// episode the viewer finished on.
func CompleteAniListAnimeRewatch(token string, show Anime) error {
	today := currentFuzzyDate()
	repeats, began := anilistRewatchCompletion(show, today)
	completed, lastEpisode := "COMPLETED", show.Ep.Number
	return SaveAniListAnimeListEntry(token, show.AnilistId, &completed, &lastEpisode, &repeats, nil, &began, &today)
}

// RateAniListAnime asks for a score and saves it; cancelling saves nothing.
func RateAniListAnime(accessToken string, animeID int) error {
	score, skipped, promptErr := promptAnimeScoreValue()
	if promptErr != nil || skipped {
		return promptErr
	}
	return saveAniListAnimeScore(accessToken, animeID, score)
}

func saveAniListAnimeScore(accessToken string, mediaID int, score float64) error {
	vars := map[string]interface{}{"mediaId": mediaID, "score": score}
	rateErr := aniListQuery(aniListSetScoreMutation, vars, accessToken, nil)
	if rateErr == nil {
		Out(fmt.Sprintf("Successfully rated anime (mediaId: %d) with score: %.2f", mediaID, score))
	}
	return rateErr
}

// dedupeEntriesByMediaID drops later entries for a Media.ID already seen.
// Entries with no ID are all kept, since there is nothing to compare.
func dedupeEntriesByMediaID(list []Entry) []Entry {
	if len(list) <= 1 {
		return list
	}
	seen := make(map[int]struct{}, len(list))
	unique := make([]Entry, 0, len(list))
	for _, candidate := range list {
		id := candidate.Media.ID
		if _, dup := seen[id]; dup && id != 0 {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, candidate)
	}
	return unique
}

// ParseAnimeList sorts a MediaListCollection answer (as GetUserData returns
// it) into status buckets.
func ParseAnimeList(raw map[string]interface{}) AnimeList {
	switch {
	case raw == nil:
		Log("AniList response is nil")
		return AnimeList{}
	case raw["data"] == nil:
		Log("Anilist request failed")
		return AnimeList{}
	}
	// Round-tripped through JSON so the typed decoder can be reused.
	encoded, encodeErr := json.Marshal(raw)
	if encodeErr != nil {
		Log(fmt.Sprintf("AniList response could not be re-encoded: %v", encodeErr))
		return AnimeList{}
	}
	var collection aniListCollectionResponse
	if decodeErr := decodeAniList(encoded, &collection); decodeErr != nil {
		Log(fmt.Sprintf("AniList response is invalid: %v", decodeErr))
		return AnimeList{}
	}
	return animeListFromCollection(collection)
}

func animeListFromCollection(response aniListCollectionResponse) AnimeList {
	if response.Data == nil {
		Log("Anilist request failed")
		return AnimeList{}
	}
	if response.Data.MediaListCollection == nil {
		Log("AniList response MediaListCollection field is invalid")
		return AnimeList{}
	}

	var flat []Entry
	for _, list := range response.Data.MediaListCollection.Lists {
		// Custom lists re-export entries that are already under a status
		// list; reading them would count a show twice.
		if list.IsCustomList {
			continue
		}
		for _, item := range list.Entries {
			if item.Media != nil {
				flat = append(flat, item.entry())
			}
		}
	}

	// AniList should not file one show under two statuses; if it does, the
	// first wins.
	var sorted AnimeList
	for _, entry := range dedupeEntriesByMediaID(flat) {
		sorted.add(entry)
	}
	return sorted
}

func (raw aniListListEntry) entry() Entry {
	media := raw.Media
	entry := Entry{
		ListID:      raw.ID,
		Progress:    raw.Progress,
		Repeat:      raw.Repeat,
		Score:       raw.Score,
		Status:      raw.Status,
		StartedAt:   raw.StartedAt,
		CompletedAt: raw.CompletedAt,
		UpdatedAt:   time.Unix(raw.UpdatedAt, 0).UTC(),
		CoverImage:  media.CoverImage.Large,
		Private:     raw.Private,
		Media: Media{
			ID:       media.ID,
			MalID:    media.IDMal,
			Duration: media.Duration,
			Episodes: media.Episodes,
			Format:   media.Format,
			Status:   media.Status,
			Title:    media.Title.animeTitle(),
			IsAdult:  media.IsAdult,
		},
	}
	if next := media.NextAiringEpisode; next != nil {
		entry.Media.NextAiringEpisode = &NextAiringEpisodeInfo{
			Episode:         next.Episode,
			TimeUntilAiring: next.TimeUntilAiring,
		}
	}
	return entry
}

// add files an entry under its status. An unknown status is dropped.
func (list *AnimeList) add(entry Entry) {
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

// FindAnimeByAnilistID returns a copy of the entry for an AniList ID given as
// a string, looking through every bucket.
func FindAnimeByAnilistID(list AnimeList, rawID string) (*Entry, error) {
	want, convErr := strconv.Atoi(rawID)
	if convErr != nil {
		return nil, fmt.Errorf("invalid ID format: %s", rawID)
	}
	for _, candidate := range getEntriesByCategory(list, "ALL") {
		if candidate.Media.ID != want {
			continue
		}
		found := candidate
		return &found, nil
	}
	return nil, fmt.Errorf("anime with ID %d not found", want)
}

// mediaShapeError checks the two levels every single-Media answer must have.
func mediaShapeError(hasData, hasMedia bool) error {
	switch {
	case !hasData:
		return errors.New("invalid response format: data field missing")
	case !hasMedia:
		return errors.New("invalid response format: Media field missing")
	}
	return nil
}

const aniListAnimeDetailsQuery = `query ($id: Int) {
	Media(id: $id, type: ANIME) {
		id idMal isAdult episodes duration status
		` + aniListTitleFields + `
		coverImage { large }
		nextAiringEpisode { episode }
	}
}`

// GetAnimeDataByID fetches the details of one show that the player needs
// before the list entry, if any, is known.
func GetAnimeDataByID(mediaID int, token string) (Anime, error) {
	var answer aniListMediaResponse
	if queryErr := aniListQuery(aniListAnimeDetailsQuery, map[string]interface{}{"id": mediaID}, token, &answer); queryErr != nil {
		return Anime{}, fmt.Errorf("failed to get anime data: %w", queryErr)
	}
	if shapeErr := mediaShapeError(answer.Data != nil, answer.Data != nil && answer.Data.Media != nil); shapeErr != nil {
		return Anime{}, shapeErr
	}

	media := answer.Data.Media
	details := Anime{
		AnilistId:     mediaID,
		MalId:         media.IDMal,
		IsAdult:       media.IsAdult,
		TotalEpisodes: media.Episodes,
		Title:         media.Title.animeTitle(),
		CoverImage:    media.CoverImage.Large,
		// A show with a next episode scheduled is airing whatever its status says.
		IsAiring: media.Status == "RELEASING" || media.NextAiringEpisode != nil,
	}
	details.Ep.Duration = media.Duration * 60
	return details, nil
}

type (
	// SequelInfo is one show AniList lists as a sequel, enough to offer it.
	SequelInfo struct {
		ID, Episodes int
		Title        AnimeTitle
		CoverImage   string
		// Status is AniList's: FINISHED, RELEASING or NOT_YET_RELEASED.
		Status  string
		SiteURL string
	}

	// aniListRelationsResponse is the answer to aniListRelationsQuery.
	aniListRelationsResponse struct {
		Data *struct {
			Media *struct {
				Relations *struct {
					Edges []struct {
						RelationType string        `json:"relationType"`
						Node         *aniListMedia `json:"node"`
					} `json:"edges"`
				} `json:"relations"`
			} `json:"Media"`
		} `json:"data"`
	}
)

const aniListRelationsQuery = `query ($id: Int) {
	Media(id: $id, type: ANIME) {
		relations {
			edges {
				relationType
				node { id title { romaji english } coverImage { large } episodes status siteUrl }
			}
		}
	}
}`

// GetAnimeSequel returns the shows AniList lists as sequels of animeID, or
// nil when there are none.
func GetAnimeSequel(mediaID int, token string) ([]SequelInfo, error) {
	var answer aniListRelationsResponse
	if queryErr := aniListQuery(aniListRelationsQuery, map[string]interface{}{"id": mediaID}, token, &answer); queryErr != nil {
		return nil, fmt.Errorf("failed to get anime relations: %w", queryErr)
	}
	if shapeErr := mediaShapeError(answer.Data != nil, answer.Data != nil && answer.Data.Media != nil); shapeErr != nil {
		return nil, shapeErr
	}
	relations := answer.Data.Media.Relations
	if relations == nil {
		return nil, nil
	}

	var found []SequelInfo
	for _, edge := range relations.Edges {
		if edge.RelationType != "SEQUEL" || edge.Node == nil {
			continue
		}
		sequel := edge.Node
		found = append(found, SequelInfo{
			ID:         sequel.ID,
			Title:      AnimeTitle{Romaji: sequel.Title.Romaji, English: sequel.Title.English},
			CoverImage: sequel.CoverImage.Large,
			Episodes:   sequel.Episodes,
			Status:     sequel.Status,
			SiteURL:    sequel.SiteURL,
		})
	}
	return found, nil
}

// DeleteAniListListEntry removes a list entry by its list (not media) ID.
func DeleteAniListListEntry(token string, entryID int) error {
	const mutation = `mutation ($id: Int) { DeleteMediaListEntry(id: $id) { deleted } }`
	return aniListQuery(mutation, map[string]interface{}{"id": entryID}, token, nil)
}

// FindSequelInAnimeList reports which list a sequel is already on. The order
// matters only for a show on two lists at once, which AniList does not allow.
func FindSequelInAnimeList(list AnimeList, sequelID int) (status string, onList bool) {
	lookIn := []struct {
		status  string
		entries []Entry
	}{
		{"CURRENT", list.Watching},
		{"PLANNING", list.Planning},
		{"COMPLETED", list.Completed},
		{"PAUSED", list.Paused},
		{"DROPPED", list.Dropped},
		{"REWATCHING", list.Rewatching},
	}
	for _, bucket := range lookIn {
		for _, entry := range bucket.entries {
			if entry.Media.ID == sequelID {
				return bucket.status, true
			}
		}
	}
	return "", false
}

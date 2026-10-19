package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// aniListCall is one request the fake AniList received.
type aniListCall struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
	Auth      string                 `json:"-"`
}

// fakeAniList points every AniList request at a local server that answers
// with reply. There is no global config, so the token a test passes is the
// one sent, and 429 back-off does not sleep.
func fakeAniList(t *testing.T, reply func(call aniListCall, n int) (int, string)) *[]aniListCall {
	t.Helper()
	var mu sync.Mutex
	calls := []aniListCall{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var call aniListCall
		_ = json.Unmarshal(body, &call)
		call.Auth = r.Header.Get("Authorization")
		mu.Lock()
		calls = append(calls, call)
		n := len(calls)
		mu.Unlock()
		status, answer := reply(call, n)
		if status == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "7")
		}
		w.WriteHeader(status)
		io.WriteString(w, answer)
	}))
	t.Cleanup(server.Close)

	oldEndpoint, oldSleep, oldConfig := anilistEndpoint, aniListSleep, globalConfig
	anilistEndpoint = server.URL
	aniListSleep = func(time.Duration) {}
	globalConfig = nil
	t.Cleanup(func() {
		anilistEndpoint, aniListSleep, globalConfig = oldEndpoint, oldSleep, oldConfig
	})
	return &calls
}

func answerAlways(body string) func(aniListCall, int) (int, string) {
	return func(aniListCall, int) (int, string) { return http.StatusOK, body }
}

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"kitten", "sitting", 3},
		{"Frieren", "frieren", 0},
		{"flaw", "lawn", 2},
		{"日本", "日本語", 1},
	}
	for _, c := range cases {
		if got := levenshtein(c.a, c.b); got != c.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestMediaDisplayTitleFallsBackToID(t *testing.T) {
	title := mediaDisplayTitle(Media{ID: 42}, &Config{AnimeNameLanguage: "english"})
	if title != "42" {
		t.Fatalf("expected ID fallback for empty titles, got %q", title)
	}
	if got := mediaDisplayTitle(Media{}, nil); got != "Unknown Anime" {
		t.Fatalf("no title and no ID = %q", got)
	}
	if got := mediaDisplayTitle(Media{Title: AnimeTitle{Japanese: "例"}}, nil); got != "例" {
		t.Fatalf("native title was not used as the last resort: %q", got)
	}
}

// collection wraps lists the way GetUserData hands them to ParseAnimeList.
func collection(t *testing.T, lists string) map[string]interface{} {
	t.Helper()
	var input map[string]interface{}
	raw := `{"data":{"MediaListCollection":{"lists":[` + lists + `]}}}`
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		t.Fatalf("bad fixture: %v", err)
	}
	return input
}

const exampleEntry = `{"id":1001,"status":"CURRENT","progress":3,"repeat":0,"updatedAt":1700000000,
	"media":{"id":42,"idMal":420,"episodes":12,"duration":24,"format":"TV","status":"RELEASING",
	"title":{"english":"Example Show","romaji":"Example Show","native":"例"}}}`

func TestParseAnimeListMalformedInputReturnsEmptyList(t *testing.T) {
	inputs := []map[string]interface{}{
		nil,
		{"data": nil},
		{"data": "nonsense"},
		{"data": map[string]interface{}{}},
	}
	for _, input := range inputs {
		if got := ParseAnimeList(input); len(getEntriesByCategory(got, "ALL")) != 0 {
			t.Errorf("ParseAnimeList(%v) found entries: %#v", input, got)
		}
	}
}

func TestParseAnimeListSkipsCustomListsThatReExportEntries(t *testing.T) {
	// AniList returns the same MediaList entry under the status list and again
	// under each custom list that includes it.
	list := ParseAnimeList(collection(t, `
		{"name":"Watching","isCustomList":false,"entries":[`+exampleEntry+`]},
		{"name":"Favorites","isCustomList":true,"entries":[`+exampleEntry+`]}`))

	if len(list.Watching) != 1 {
		t.Fatalf("expected custom-list re-export to be skipped, watching=%d", len(list.Watching))
	}
	all := getEntriesByCategory(list, "ALL")
	if len(all) != 1 || all[0].Media.ID != 42 {
		t.Fatalf("Show All should list media 42 once, got %#v", all)
	}
}

func TestParseAnimeListReadsOnlyCustomListAsNothing(t *testing.T) {
	// A show that is only on a custom list is still on a status list as far
	// as AniList is concerned, so a lone custom list contributes nothing.
	list := ParseAnimeList(collection(t, `{"name":"Favorites","isCustomList":true,"entries":[`+exampleEntry+`]}`))
	if all := getEntriesByCategory(list, "ALL"); len(all) != 0 {
		t.Fatalf("custom list entries were read: %#v", all)
	}
}

func TestParseAnimeListDedupesSameMediaWithoutIsCustomListFlag(t *testing.T) {
	// Older/cached payloads may omit isCustomList; still dedupe by media ID.
	entry := `{"id":1,"status":"CURRENT","progress":1,"media":{"id":7,"title":{"english":"Seven"}}}`
	list := ParseAnimeList(collection(t, `
		{"name":"Watching","entries":[`+entry+`]},
		{"name":"Seasonal","entries":[`+entry+`]}`))
	if len(list.Watching) != 1 {
		t.Fatalf("expected media-ID dedupe without isCustomList, watching=%d", len(list.Watching))
	}
}

func TestParseAnimeListDedupesAcrossStatuses(t *testing.T) {
	// The first list wins even when the copy sits under another status.
	list := ParseAnimeList(collection(t, `
		{"name":"Completed","entries":[{"status":"COMPLETED","media":{"id":7}}]},
		{"name":"Watching","entries":[{"status":"CURRENT","media":{"id":7}},{"status":"CURRENT","media":{"id":8}}]}`))
	if len(list.Completed) != 1 || len(list.Watching) != 1 || list.Watching[0].Media.ID != 8 {
		t.Fatalf("completed=%#v watching=%#v", list.Completed, list.Watching)
	}
}

func TestParseAnimeListReadsEveryField(t *testing.T) {
	list := ParseAnimeList(collection(t, `{"name":"Rewatching","entries":[{
		"id":55,"status":"REPEATING","score":8.5,"progress":4,"repeat":2,"updatedAt":1700000000,
		"private":true,
		"startedAt":{"year":2024,"month":3,"day":null},
		"completedAt":null,
		"media":{"id":9,"idMal":90,"isAdult":true,"episodes":24,"duration":23,"format":"TV",
			"status":"RELEASING","coverImage":{"large":"https://img/9.jpg"},
			"title":{"romaji":"Kyuu","english":"Nine","native":"九"},
			"nextAiringEpisode":{"episode":5,"timeUntilAiring":3600}}}]}`))

	if len(list.Rewatching) != 1 {
		t.Fatalf("REPEATING should land in Rewatching: %#v", list)
	}
	got := list.Rewatching[0]
	want := Entry{
		ListID:      55,
		Status:      "REPEATING",
		Score:       8.5,
		Progress:    4,
		Repeat:      2,
		UpdatedAt:   time.Unix(1700000000, 0).UTC(),
		Private:     true,
		StartedAt:   FuzzyDate{Year: 2024, Month: 3},
		CoverImage:  "https://img/9.jpg",
		CompletedAt: FuzzyDate{},
		Media: Media{
			ID: 9, MalID: 90, IsAdult: true, Episodes: 24, Duration: 23, Format: "TV", Status: "RELEASING",
			Title:             AnimeTitle{Romaji: "Kyuu", English: "Nine", Japanese: "九"},
			NextAiringEpisode: &NextAiringEpisodeInfo{Episode: 5, TimeUntilAiring: 3600},
		},
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("entry\n got %s\nwant %s", gotJSON, wantJSON)
	}
}

func TestParseAnimeListToleratesAWrongTypedField(t *testing.T) {
	// One odd field must not lose the whole list; it just reads as zero.
	list := ParseAnimeList(collection(t, `{"name":"Watching","entries":[
		{"status":"CURRENT","progress":"three","media":{"id":1}},
		{"status":"CURRENT","progress":2,"media":{"id":2}},
		{"status":"CURRENT"},
		{"status":"SOMETHING_NEW","media":{"id":3}}]}`))
	if len(list.Watching) != 2 {
		t.Fatalf("watching = %#v", list.Watching)
	}
	if list.Watching[0].Progress != 0 || list.Watching[1].Progress != 2 {
		t.Fatalf("progress = %d, %d", list.Watching[0].Progress, list.Watching[1].Progress)
	}
	if all := getEntriesByCategory(list, "ALL"); len(all) != 2 {
		t.Fatalf("an entry without media or with an unknown status was kept: %#v", all)
	}
}

func TestDedupeEntriesByMediaIDKeepsFirstAndZeroIDs(t *testing.T) {
	entries := []Entry{
		{Media: Media{ID: 1}, Progress: 1},
		{Media: Media{ID: 0}, Progress: 2},
		{Media: Media{ID: 1}, Progress: 3},
		{Media: Media{ID: 0}, Progress: 4},
		{Media: Media{ID: 2}, Progress: 5},
	}
	got := dedupeEntriesByMediaID(entries)
	var progress []int
	for _, entry := range got {
		progress = append(progress, entry.Progress)
	}
	if fmt.Sprint(progress) != "[1 2 4 5]" {
		t.Fatalf("kept progress %v, want [1 2 4 5]", progress)
	}
}

func TestFindAnimeByAnilistID(t *testing.T) {
	list := AnimeList{
		Completed:  []Entry{{Media: Media{ID: 3}, Progress: 12}},
		Rewatching: []Entry{{Media: Media{ID: 4}, Progress: 2}},
	}
	entry, err := FindAnimeByAnilistID(list, "4")
	if err != nil || entry.Progress != 2 {
		t.Fatalf("entry=%#v err=%v", entry, err)
	}
	if _, err := FindAnimeByAnilistID(list, "5"); err == nil || err.Error() != "anime with ID 5 not found" {
		t.Fatalf("missing ID err = %v", err)
	}
	if _, err := FindAnimeByAnilistID(list, "abc"); err == nil || !strings.Contains(err.Error(), "invalid ID format") {
		t.Fatalf("bad ID err = %v", err)
	}
}

func TestFindSequelInAnimeList(t *testing.T) {
	list := AnimeList{
		Planning:   []Entry{{Media: Media{ID: 1}}},
		Rewatching: []Entry{{Media: Media{ID: 2}}},
	}
	if status, ok := FindSequelInAnimeList(list, 1); !ok || status != "PLANNING" {
		t.Fatalf("got %q %v", status, ok)
	}
	// The sequel prompt names this list REWATCHING, not AniList's REPEATING.
	if status, ok := FindSequelInAnimeList(list, 2); !ok || status != "REWATCHING" {
		t.Fatalf("got %q %v", status, ok)
	}
	if _, ok := FindSequelInAnimeList(list, 3); ok {
		t.Fatal("found a sequel that is not on the list")
	}
}

func TestAniListRetryDelay(t *testing.T) {
	withHeader := &http.Response{Header: http.Header{"Retry-After": {" 9 "}}}
	if got := aniListRetryDelay(withHeader, 0); got != 9*time.Second {
		t.Fatalf("Retry-After ignored: %v", got)
	}
	bad := &http.Response{Header: http.Header{"Retry-After": {"soon"}}}
	if got := aniListRetryDelay(bad, 2); got != 6*time.Second {
		t.Fatalf("fallback delay = %v, want 6s", got)
	}
	if got := aniListRetryDelay(nil, 0); got != 2*time.Second {
		t.Fatalf("nil response delay = %v", got)
	}
}

func TestAniListRequestRetriesRateLimit(t *testing.T) {
	calls := fakeAniList(t, func(_ aniListCall, n int) (int, string) {
		if n < 3 {
			return http.StatusTooManyRequests, "slow down"
		}
		return http.StatusOK, `{"data":{"Viewer":{"id":5,"name":"someone"}}}`
	})
	var waited []time.Duration
	aniListSleep = func(d time.Duration) { waited = append(waited, d) }

	id, name, err := GetAnilistUserID("tok")
	if err != nil || id != 5 || name != "someone" {
		t.Fatalf("id=%d name=%q err=%v", id, name, err)
	}
	if len(*calls) != 3 || fmt.Sprint(waited) != "[7s 7s]" {
		t.Fatalf("calls=%d waited=%v", len(*calls), waited)
	}
	if (*calls)[0].Auth != "Bearer tok" {
		t.Fatalf("Authorization = %q", (*calls)[0].Auth)
	}
}

func TestAniListRequestGivesUpAfterFiveRateLimits(t *testing.T) {
	calls := fakeAniList(t, func(aniListCall, int) (int, string) {
		return http.StatusTooManyRequests, "Too Many Requests."
	})
	_, err := makePostRequest(anilistEndpoint, `query { Viewer { id } }`, nil, nil)
	if len(*calls) != 5 {
		t.Fatalf("sent %d times, want 5", len(*calls))
	}
	// The stats page tells a rate limit apart by this wording.
	if !isRateLimitError(err) || !strings.HasPrefix(err.Error(), "failed with status 429") {
		t.Fatalf("err = %v", err)
	}
}

func TestAniListSearchKeepsItsStatusWording(t *testing.T) {
	fakeAniList(t, func(aniListCall, int) (int, string) { return http.StatusInternalServerError, "boom" })
	_, err := SearchAnimeAnilist("x", "")
	if err == nil || err.Error() != "failed to search for anime. Status Code: 500, Response: boom" {
		t.Fatalf("err = %v", err)
	}
}

func TestAniListGraphQLErrorsAreErrors(t *testing.T) {
	calls := fakeAniList(t, answerAlways(`{"errors":[{"message":"Not Found.","status":404}],"data":{"Media":null}}`))
	_, err := GetAnimeMalID(1)
	if err == nil || err.Error() != `graphql errors: [{"message":"Not Found.","status":404}]` {
		t.Fatalf("err = %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("a GraphQL error was retried: %d calls", len(*calls))
	}
}

func TestMakePostRequestDecodesIntoAMap(t *testing.T) {
	calls := fakeAniList(t, answerAlways(`{"data":{"Media":{"idMal":12}}}`))
	headers := map[string]string{"Authorization": "Bearer abc", "Content-Type": "application/json"}
	response, err := makePostRequest(anilistEndpoint, "q", map[string]interface{}{"id": 1}, headers)
	if err != nil {
		t.Fatal(err)
	}
	media := response["data"].(map[string]interface{})["Media"].(map[string]interface{})
	if media["idMal"] != float64(12) {
		t.Fatalf("response = %#v", response)
	}
	if (*calls)[0].Auth != "Bearer abc" || (*calls)[0].Variables["id"] != float64(1) {
		t.Fatalf("call = %#v", (*calls)[0])
	}
	if headers["Authorization"] != "Bearer abc" {
		t.Fatal("caller's headers were changed")
	}
}

func TestSearchAnimeAnilistRanksByTitleDistance(t *testing.T) {
	var media []string
	for i := 1; i <= 12; i++ {
		media = append(media, fmt.Sprintf(`{"id":%d,"episodes":%d,"title":{"english":"%s"}}`, i, i, strings.Repeat("z", i+3)))
	}
	media = append(media, `{"id":99,"episodes":12,"title":{"english":"","romaji":"Frieren"},"coverImage":{"large":"c.jpg"}}`)
	calls := fakeAniList(t, answerAlways(`{"data":{"Page":{"media":[`+strings.Join(media, ",")+`]}}}`))

	options, err := SearchAnimeAnilist("frieren", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 10 {
		t.Fatalf("got %d options, want the closest 10", len(options))
	}
	if options[0].Key != "99" || options[0].Title != "Frieren" {
		t.Fatalf("closest match is not first: %#v", options[0])
	}
	if strings.Contains((*calls)[0].Query, "coverImage") {
		t.Fatal("the list search asked for covers it does not show")
	}
	if (*calls)[0].Variables["search"] != "frieren" {
		t.Fatalf("variables = %#v", (*calls)[0].Variables)
	}

	previews, err := SearchAnimeAnilistPreview("frieren", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(previews) != 10 || previews["99"].CoverImage != "c.jpg" {
		t.Fatalf("previews = %#v", previews)
	}
	if !strings.Contains((*calls)[1].Query, "coverImage") {
		t.Fatal("the preview search did not ask for covers")
	}
}

func TestGetAnilistUserIDRejectsIncompleteViewer(t *testing.T) {
	cases := map[string]string{
		`{}`:                               "data field missing",
		`{"data":{"Viewer":null}}`:         "Viewer field missing",
		`{"data":{"Viewer":{"name":"x"}}}`: "id field missing",
		`{"data":{"Viewer":{"id":3}}}`:     "name field missing",
	}
	for answer, want := range cases {
		fakeAniList(t, answerAlways(answer))
		if _, _, err := GetAnilistUserID("tok"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("answer %s: err = %v, want %q", answer, err, want)
		}
	}
}

func TestGetAnimeMalIDAndImage(t *testing.T) {
	fakeAniList(t, answerAlways(`{"data":{"Media":{"idMal":null,"coverImage":{"large":"x.jpg"}}}}`))
	if _, err := GetAnimeMalID(3); err == nil || err.Error() != "AniList media 3 has no MyAnimeList ID" {
		t.Fatalf("err = %v", err)
	}
	malID, image, err := GetAnimeIDAndImage(3)
	if err != nil || malID != 0 || image != "x.jpg" {
		t.Fatalf("malID=%d image=%q err=%v", malID, image, err)
	}

	fakeAniList(t, answerAlways(`{"data":{"Media":null}}`))
	if _, _, err := GetAnimeIDAndImage(3); err == nil || err.Error() != "AniList media 3 not found" {
		t.Fatalf("err = %v", err)
	}
}

func TestGetAnimeDataByID(t *testing.T) {
	fakeAniList(t, answerAlways(`{"data":{"Media":{"id":5,"idMal":50,"isAdult":false,"episodes":null,
		"duration":24,"status":"NOT_YET_RELEASED","title":{"romaji":"Go","english":"Five","native":"五"},
		"coverImage":{"large":"5.jpg"},"nextAiringEpisode":{"episode":1}}}}`))
	anime, err := GetAnimeDataByID(5, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if anime.AnilistId != 5 || anime.MalId != 50 || anime.TotalEpisodes != 0 || anime.Ep.Duration != 24*60 ||
		anime.Title.English != "Five" || anime.CoverImage != "5.jpg" || !anime.IsAiring {
		t.Fatalf("anime = %#v", anime)
	}

	fakeAniList(t, answerAlways(`{"data":{"Media":null}}`))
	if _, err := GetAnimeDataByID(5, "tok"); err == nil || err.Error() != "invalid response format: Media field missing" {
		t.Fatalf("err = %v", err)
	}
}

func TestGetAnimeSequelKeepsOnlySequels(t *testing.T) {
	fakeAniList(t, answerAlways(`{"data":{"Media":{"relations":{"edges":[
		{"relationType":"PREQUEL","node":{"id":1}},
		{"relationType":"SEQUEL","node":{"id":2,"title":{"romaji":"Ni","english":"Two"},
			"coverImage":{"large":"2.jpg"},"episodes":12,"status":"FINISHED","siteUrl":"https://anilist.co/anime/2"}},
		{"relationType":"SEQUEL","node":null}]}}}}`))
	sequels, err := GetAnimeSequel(1, "")
	if err != nil {
		t.Fatal(err)
	}
	want := SequelInfo{ID: 2, Title: AnimeTitle{Romaji: "Ni", English: "Two"}, CoverImage: "2.jpg",
		Episodes: 12, Status: "FINISHED", SiteURL: "https://anilist.co/anime/2"}
	if len(sequels) != 1 || sequels[0] != want {
		t.Fatalf("sequels = %#v", sequels)
	}

	fakeAniList(t, answerAlways(`{"data":{"Media":{"relations":{"edges":[]}}}}`))
	if sequels, err := GetAnimeSequel(1, ""); err != nil || sequels != nil {
		t.Fatalf("no sequels: %#v %v", sequels, err)
	}
}

func TestSaveAniListAnimeListEntrySendsOnlyGivenFields(t *testing.T) {
	calls := fakeAniList(t, answerAlways(`{"data":{"SaveMediaListEntry":{"id":1}}}`))
	progress := 4
	cleared := FuzzyDate{}
	if err := SaveAniListAnimeListEntry("tok", 7, nil, &progress, nil, nil, &cleared, nil); err != nil {
		t.Fatal(err)
	}
	vars := (*calls)[0].Variables
	if len(vars) != 3 || vars["mediaId"] != float64(7) || vars["progress"] != float64(4) {
		t.Fatalf("variables = %#v", vars)
	}
	// A zero date is an explicit null, which clears it on AniList.
	if value, ok := vars["startedAt"]; !ok || value != nil {
		t.Fatalf("startedAt = %#v (present %v)", value, ok)
	}
}

func TestCompleteAniListAnimeRewatchSendsTheRewatch(t *testing.T) {
	calls := fakeAniList(t, answerAlways(`{"data":{"SaveMediaListEntry":{"id":1}}}`))
	anime := Anime{AnilistId: 8, Repeat: 1, StartedAt: FuzzyDate{Year: 2025, Month: 1, Day: 2}}
	anime.Ep.Number = 12
	if err := CompleteAniListAnimeRewatch("tok", anime); err != nil {
		t.Fatal(err)
	}
	vars := (*calls)[0].Variables
	started, _ := json.Marshal(vars["startedAt"])
	if vars["status"] != "COMPLETED" || vars["progress"] != float64(12) || vars["repeat"] != float64(2) ||
		string(started) != `{"day":2,"month":1,"year":2025}` || vars["completedAt"] == nil {
		t.Fatalf("variables = %#v", vars)
	}
}

func TestListMutationsUseTheStatusVariable(t *testing.T) {
	calls := fakeAniList(t, answerAlways(`{"data":{"SaveMediaListEntry":{"id":1}}}`))
	if err := AddAniListAnimeToWatchingList(3, "tok"); err != nil {
		t.Fatal(err)
	}
	if err := AddAniListAnimeToList(3, "PLANNING", "tok"); err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].Variables["status"] != "CURRENT" || (*calls)[1].Variables["status"] != "PLANNING" {
		t.Fatalf("calls = %#v", *calls)
	}
}

func TestFetchLatestAniListAnimeListAsksForCovers(t *testing.T) {
	calls := fakeAniList(t, answerAlways(`{"data":{"MediaListCollection":{"lists":[{"name":"Planning","entries":[
		{"status":"PLANNING","media":{"id":4,"coverImage":{"large":"4.jpg"}}}]}]}}}`))
	list, err := FetchLatestAniListAnimeList("tok", 77)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Planning) != 1 || list.Planning[0].CoverImage != "4.jpg" {
		t.Fatalf("list = %#v", list)
	}
	call := (*calls)[0]
	if !strings.Contains(call.Query, "coverImage") || call.Variables["userId"] != float64(77) || call.Variables["type"] != "ANIME" {
		t.Fatalf("call = %#v", call)
	}

	if _, err := GetUserData("tok", 77); err != nil {
		t.Fatal(err)
	}
	if strings.Contains((*calls)[1].Query, "coverImage") {
		t.Fatal("GetUserData asked for covers")
	}
}

func TestAnimeListSyncKeepsOnlyTheNewestUpdate(t *testing.T) {
	listSync := NewAnimeListSync(AnimeList{})
	first := AnimeList{Watching: []Entry{{Media: Media{ID: 1}}}}
	second := AnimeList{Watching: []Entry{{Media: Media{ID: 2}}}}
	if !listSync.Replace(first, true) || !listSync.Replace(second, true) {
		t.Fatal("a changed list was reported unchanged")
	}
	if listSync.Replace(second, true) {
		t.Fatal("the same list was reported changed")
	}
	select {
	case got := <-listSync.Updates():
		if got.Watching[0].Media.ID != 2 {
			t.Fatalf("got a stale update: %#v", got)
		}
	default:
		t.Fatal("no update was published")
	}
	select {
	case got := <-listSync.Updates():
		t.Fatalf("a second update was left behind: %#v", got)
	default:
	}

	listSync.MarkRefreshDone()
	listSync.MarkRefreshDone()
	<-listSync.RefreshDone()
}

func TestAniListCacheRoundTripAndOwner(t *testing.T) {
	dir := t.TempDir()
	list := AnimeList{Paused: []Entry{{Media: Media{ID: 6}, Progress: 3}}}
	if err := saveAniListAnimeListCache(dir, 10, list); err != nil {
		t.Fatal(err)
	}
	payload, err := loadAniListAnimeListCache(dir, 10)
	if err != nil || payload.UserID != 10 || !animeListEqual(payload.AnimeList, list) {
		t.Fatalf("payload=%#v err=%v", payload, err)
	}
	// An unknown viewer (0) may use whatever cache is there.
	if _, err := loadAniListAnimeListCache(dir, 0); err != nil {
		t.Fatalf("unknown viewer: %v", err)
	}
	if _, err := loadAniListAnimeListCache(dir, 11); err == nil {
		t.Fatal("another account's cache was loaded")
	}
	if _, err := os.Stat(filepath.Join(dir, anilistAnimeListCacheFileName)); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAniListAnimeListCache(t.TempDir(), 10); !os.IsNotExist(err) {
		t.Fatalf("missing cache err = %v", err)
	}
}

func TestInitializeAniListUserAnimeListWithoutCacheFetches(t *testing.T) {
	calls := fakeAniList(t, func(call aniListCall, _ int) (int, string) {
		if strings.Contains(call.Query, "Viewer") {
			return http.StatusOK, `{"data":{"Viewer":{"id":12,"name":"fresh"}}}`
		}
		return http.StatusOK, `{"data":{"MediaListCollection":{"lists":[{"entries":[{"status":"CURRENT","media":{"id":1}}]}]}}}`
	})
	config := &Config{StoragePath: t.TempDir()}
	user := &User{Token: "tok", Username: "stale"}
	if err := InitializeAniListUserAnimeList(config, user); err != nil {
		t.Fatal(err)
	}
	if user.Id != 12 || user.Username != "fresh" || len(user.AnimeList.Watching) != 1 || len(*calls) != 2 {
		t.Fatalf("user=%#v calls=%d", user, len(*calls))
	}
	select {
	case <-user.ListSync.RefreshDone():
	default:
		t.Fatal("a blocking fetch left the refresh pending")
	}
	if _, err := loadAniListAnimeListCache(config.StoragePath, 12); err != nil {
		t.Fatalf("cache not written: %v", err)
	}
}

func TestInitializeAniListUserAnimeListFromCacheRefreshesBehind(t *testing.T) {
	calls := fakeAniList(t, answerAlways(`{"data":{"MediaListCollection":{"lists":[{"entries":[
		{"status":"CURRENT","progress":5,"media":{"id":1}}]}]}}}`))
	config := &Config{StoragePath: t.TempDir()}
	cachedList := AnimeList{Watching: []Entry{{Status: "CURRENT", Progress: 4, Media: Media{ID: 1}}}}
	if err := saveAniListAnimeListCache(config.StoragePath, 12, cachedList); err != nil {
		t.Fatal(err)
	}

	user := &User{Token: "tok"}
	if err := InitializeAniListUserAnimeList(config, user); err != nil {
		t.Fatal(err)
	}
	if user.Id != 12 || user.AnimeList.Watching[0].Progress != 4 {
		t.Fatalf("cache not used first: %#v", user)
	}
	select {
	case <-user.ListSync.RefreshDone():
	case <-time.After(5 * time.Second):
		t.Fatal("background refresh never finished")
	}
	if got := user.ListSync.Current().Watching[0].Progress; got != 5 {
		t.Fatalf("refreshed progress = %d, want 5", got)
	}
	// The cached ID stands in for the Viewer lookup.
	if len(*calls) != 1 || strings.Contains((*calls)[0].Query, "Viewer") {
		t.Fatalf("calls = %#v", *calls)
	}
}

func TestEntryHasNewEpisodes(t *testing.T) {
	airing := Entry{Progress: 3, Media: Media{NextAiringEpisode: &NextAiringEpisodeInfo{Episode: 6}}}
	caughtUp := Entry{Progress: 4, Media: Media{NextAiringEpisode: &NextAiringEpisodeInfo{Episode: 5}}}
	on := &Config{ShowNewEpisodes: true}
	if !entryHasNewEpisodes(airing, on) {
		t.Error("episode 5 has aired past progress 3")
	}
	if entryHasNewEpisodes(caughtUp, on) {
		t.Error("caught-up entry flagged")
	}
	if entryHasNewEpisodes(airing, &Config{}) {
		t.Error("flagged with ShowNewEpisodes off")
	}
	if entryHasNewEpisodes(Entry{Progress: 3}, on) {
		t.Error("flagged without a next airing episode")
	}
}

package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestExtractOAuthCodeFromInput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input, state string
		want         string
		wantErr      string
	}{
		{input: "http://localhost:8123/oauth/callback?code=test-code&state=expected", state: "expected", want: "test-code"},
		{input: "http://localhost:8123/oauth/callback?code=test-code", state: "expected", want: "test-code"},
		{input: "http://localhost:8123/oauth/callback?code=test-code&state=wrong", state: "expected", wantErr: "callback state does not match the pending login"},
		{input: "http://localhost:8123/oauth/callback?code=test-code&state=other", want: "test-code"},
		{input: "http://localhost:8123/oauth/callback?state=expected", state: "expected", wantErr: "callback URL does not include an authorization code"},
		{input: "?code=from-query&state=expected", state: "expected", want: "from-query"},
		{input: "code=from-query&state=wrong", state: "expected", wantErr: "callback state does not match the pending login"},
		{input: "  raw-code \n", state: "expected", want: "raw-code"},
		{input: "   ", wantErr: "empty callback input"},
	}
	for _, tc := range cases {
		got, err := extractOAuthCodeFromInput(tc.input, tc.state)
		if tc.wantErr != "" {
			if err == nil || err.Error() != tc.wantErr {
				t.Errorf("%q: err = %v, want %q", tc.input, err, tc.wantErr)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%q: got %q, %v, want %q", tc.input, got, err, tc.want)
		}
	}
}

func TestMyAnimeListStatusConversion(t *testing.T) {
	t.Parallel()

	toAniList := []struct {
		status     string
		rewatching bool
		want       string
	}{
		{"watching", false, "CURRENT"},
		{"watching", true, "REPEATING"},
		{"completed", true, "REPEATING"},
		{"Completed", false, "COMPLETED"},
		{"on_hold", false, "PAUSED"},
		{"dropped", false, "DROPPED"},
		{"plan_to_watch", false, "PLANNING"},
		{"", false, "CURRENT"},
		{"something_new", false, "CURRENT"},
	}
	for _, tc := range toAniList {
		if got := myAnimeListStatusToAniListStatus(tc.status, tc.rewatching); got != tc.want {
			t.Errorf("myAnimeListStatusToAniListStatus(%q, %v) = %q, want %q", tc.status, tc.rewatching, got, tc.want)
		}
	}

	toMyAnimeList := map[string]string{
		"CURRENT":     "watching",
		" current ":   "watching",
		"COMPLETED":   "completed",
		"PAUSED":      "on_hold",
		"DROPPED":     "dropped",
		"PLANNING":    "plan_to_watch",
		"REPEATING":   "watching",
		"":            "watching",
		"UNKNOWN_NEW": "watching",
	}
	for status, want := range toMyAnimeList {
		if got := aniListStatusToMyAnimeListStatus(status); got != want {
			t.Errorf("aniListStatusToMyAnimeListStatus(%q) = %q, want %q", status, got, want)
		}
	}

	media := map[string]string{
		"currently_airing": "RELEASING",
		"not_yet_aired":    "NOT_YET_RELEASED",
		"finished_airing":  "FINISHED",
		"":                 "FINISHED",
	}
	for status, want := range media {
		if got := myAnimeListMediaStatusToAniListStatus(status); got != want {
			t.Errorf("myAnimeListMediaStatusToAniListStatus(%q) = %q, want %q", status, got, want)
		}
	}
}

func TestMyAnimeListStatusFields(t *testing.T) {
	t.Parallel()

	cases := map[string][2]string{
		"REPEATING": {"watching", "true"},
		"repeating": {"watching", "true"},
		"COMPLETED": {"completed", "false"},
		"PLANNING":  {"plan_to_watch", "false"},
	}
	for status, want := range cases {
		got := myAnimeListStatusFields(status)
		if got["status"] != want[0] || got["is_rewatching"] != want[1] || len(got) != 2 {
			t.Errorf("myAnimeListStatusFields(%q) = %v, want status %s, is_rewatching %s", status, got, want[0], want[1])
		}
	}
}

func TestMyAnimeListDates(t *testing.T) {
	t.Parallel()

	parse := map[string]FuzzyDate{
		"2026-05-23":  {Year: 2026, Month: 5, Day: 23},
		"1999-12-31":  {Year: 1999, Month: 12, Day: 31},
		"":            {},
		"2026":        {},
		"2026-05":     {},
		"0000-05-01":  {},
		"2026-05-32":  {},
		"2026-xx-01":  {},
		"2026-05-01-": {},
	}
	for input, want := range parse {
		if got := parseMyAnimeListDate(input); got != want {
			t.Errorf("parseMyAnimeListDate(%q) = %+v, want %+v", input, got, want)
		}
	}

	format := map[FuzzyDate]string{
		{Year: 2026, Month: 5, Day: 3}: "2026-05-03",
		{Year: 2026, Month: 5}:         "",
		{}:                             "",
	}
	for date, want := range format {
		if got := formatMyAnimeListDate(date); got != want {
			t.Errorf("formatMyAnimeListDate(%+v) = %q, want %q", date, got, want)
		}
	}

	want := time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)
	for _, input := range []string{"2026-05-23T10:00:00+00:00", "2026-05-23T19:00:00+09:00", " 2026-05-23T10:00:00.000Z "} {
		got := parseMyAnimeListTimestamp(input)
		if !got.Equal(want) || got.Location() != time.UTC {
			t.Errorf("parseMyAnimeListTimestamp(%q) = %v, want %v in UTC", input, got, want)
		}
	}
	for _, input := range []string{"", "yesterday", "2026-05-23"} {
		if got := parseMyAnimeListTimestamp(input); !got.IsZero() {
			t.Errorf("parseMyAnimeListTimestamp(%q) = %v, want zero", input, got)
		}
	}
}

func TestTrackingIDCacheRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir() + "/storage"
	payload, err := loadTrackingIDCache(dir)
	if !os.IsNotExist(err) || payload.MalToAniList == nil {
		t.Fatalf("missing cache: %+v, %v; want an empty map and a not-exist error", payload, err)
	}

	payload.MalToAniList["1"] = 100
	if err := saveTrackingIDCache(dir, payload); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadTrackingIDCache(dir)
	if err != nil || loaded.MalToAniList["1"] != 100 {
		t.Fatalf("loaded %+v, %v", loaded, err)
	}

	if err := os.WriteFile(myAnimeListFile(dir, trackingIDCacheFile), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	broken, err := loadTrackingIDCache(dir)
	if err == nil || broken.MalToAniList == nil || len(broken.MalToAniList) != 0 {
		t.Fatalf("broken cache: %+v, %v; want an error and an empty map", broken, err)
	}
}

func TestMyAnimeListCacheRejectsOtherUser(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	list := AnimeList{Watching: []Entry{{Media: Media{ID: 1}}}}
	if err := saveMyAnimeListCache(dir, 7, list); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMyAnimeListCache(dir, 8); err == nil {
		t.Fatal("a cache saved for another user must not load")
	}
	for _, userID := range []int{0, 7} {
		payload, err := loadMyAnimeListCache(dir, userID)
		if err != nil || payload.UserID != 7 || len(payload.AnimeList.Watching) != 1 {
			t.Fatalf("user %d: %+v, %v", userID, payload, err)
		}
	}
}

// anilistBatchHandler answers aliased Media lookups from known, keyed by
// MyAnimeList id, and records which ids were asked for.
func anilistBatchHandler(t *testing.T, known map[int]string, asked *[]int) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query string `json:"query"`
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &request); err != nil {
			t.Errorf("decode GraphQL request: %v", err)
		}
		data := map[string]json.RawMessage{}
		for _, part := range strings.Split(request.Query, " m")[1:] {
			var idx, malID int
			if _, err := fmt.Sscanf(part, "%d: Media(idMal: %d", &idx, &malID); err != nil {
				t.Errorf("unexpected query part %q", part)
				continue
			}
			*asked = append(*asked, malID)
			media, ok := known[malID]
			if !ok {
				media = "null"
			}
			data["m"+strconv.Itoa(idx)] = json.RawMessage(media)
		}
		out, _ := json.Marshal(map[string]any{"data": data})
		writeJSON(t, w, http.StatusOK, string(out))
	}
}

func TestLookupAniListMediaByMalIDs(t *testing.T) {
	var asked []int
	routeHTTPTo(t, anilistBatchHandler(t, map[int]string{
		5: `{"id":50,"idMal":5,"episodes":12,"duration":24,"status":"FINISHED","format":"TV","isAdult":true,
			"title":{"romaji":"Romaji","english":null,"native":"Native"},"coverImage":{"large":"cover.jpg"}}`,
		6: `{"id":0}`,
		7: `{"id":70,"episodes":null,"title":null,"coverImage":null}`,
	}, &asked))

	got, err := lookupAniListMediaByMalIDs([]int{5, 6, 7, 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 4 {
		t.Fatalf("asked for %v, want all four ids in one query", asked)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2 (ids without a match or AniList id are skipped): %+v", len(got), got)
	}
	want := aniListMediaLookupResult{
		Media: Media{ID: 50, MalID: 5, Episodes: 12, Duration: 24, Status: "FINISHED", Format: "TV", IsAdult: true,
			Title: AnimeTitle{Romaji: "Romaji", Japanese: "Native"}},
		Cover: "cover.jpg",
	}
	if got[5] != want {
		t.Fatalf("got %+v, want %+v", got[5], want)
	}
	if got[7].Media.ID != 70 || got[7].Media.MalID != 7 {
		t.Fatalf("null fields should leave zero values, got %+v", got[7])
	}

	if empty, err := lookupAniListMediaByMalIDs(nil); err != nil || len(empty) != 0 {
		t.Fatalf("no ids: %v, %v", empty, err)
	}
}

func TestFetchLatestMyAnimeList(t *testing.T) {
	config := myAnimeListTestConfig(t, &OAuthToken{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)})
	// Show 1 is already mapped, so AniList is not asked about it.
	if err := saveTrackingIDCache(config.StoragePath, trackingIDCache{MalToAniList: map[string]int{"1": 1001}}); err != nil {
		t.Fatal(err)
	}

	page1 := `{"data":[
		{"node":{"id":1,"title":"Cached Show","main_picture":{"large":"mal1.jpg"},"alternative_titles":{"en":"Cached EN","ja":"Cached JA"},
			"num_episodes":12,"average_episode_duration":1440,"status":"currently_airing","nsfw":"white"},
		 "list_status":{"status":"watching","score":8,"num_episodes_watched":3,"is_rewatching":false,"num_times_rewatched":1,
			"start_date":"2026-01-02","finish_date":"","updated_at":"2026-01-05T10:00:00+00:00"}},
		{"node":{"id":2,"title":"Looked Up","main_picture":{"large":"mal2.jpg"},"alternative_titles":{"en":"MAL EN","ja":""},
			"num_episodes":24,"average_episode_duration":600,"status":"finished_airing","nsfw":"black"},
		 "list_status":{"status":"completed","is_rewatching":true}}
	],"paging":{"next":"https://api.myanimelist.net/v2/users/@me/animelist?offset=2"}}`
	page2 := `{"data":[
		{"node":{"id":3,"title":"Unknown To AniList"},"list_status":{"status":"plan_to_watch"}},
		{"node":{"id":4,"title":"Planned","alternative_titles":{}},"list_status":{"status":"plan_to_watch","finish_date":"2026-02"}}
	],"paging":{}}`

	var asked []int
	anilist := anilistBatchHandler(t, map[int]string{
		2: `{"id":2002,"episodes":0,"duration":0,"status":"","format":"TV","isAdult":false,"title":{"romaji":"AL Romaji","english":"","native":"AL Native"},"coverImage":{"large":""}}`,
		4: `{"id":4004,"title":{"romaji":"Planned AL"},"coverImage":{"large":"al4.jpg"}}`,
	}, &asked)
	routeHTTPTo(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "graphql.anilist.co":
			anilist(w, r)
		case r.URL.Path == "/v2/users/@me/animelist" && r.URL.Query().Get("offset") == "":
			query := r.URL.Query()
			if query.Get("limit") != "1000" || query.Get("sort") != "list_updated_at" || !strings.Contains(query.Get("fields"), "nsfw") {
				t.Errorf("first page query = %v", query)
			}
			writeJSON(t, w, http.StatusOK, page1)
		case r.URL.Path == "/v2/users/@me/animelist":
			if r.URL.RawQuery != "offset=2" {
				t.Errorf("paging link changed: %q", r.URL.RawQuery)
			}
			writeJSON(t, w, http.StatusOK, page2)
		case r.URL.Path == "/v2/users/@me":
			writeJSON(t, w, http.StatusOK, `{"id":42,"name":"tester"}`)
		default:
			t.Errorf("unexpected request %s%s", r.Host, r.URL.Path)
		}
	})

	user := &User{}
	list, err := FetchLatestMyAnimeList(config, user)
	if err != nil {
		t.Fatal(err)
	}
	if user.Id != 42 || user.Username != "tester" {
		t.Fatalf("user = %d %q", user.Id, user.Username)
	}
	if fmt.Sprint(asked) != "[2 3 4]" {
		t.Fatalf("AniList was asked about %v, want 2, 3 and 4", asked)
	}

	if len(list.Watching) != 1 || len(list.Rewatching) != 1 || len(list.Planning) != 1 || len(list.Completed) != 0 {
		t.Fatalf("list = %+v", list)
	}

	cached := list.Watching[0]
	wantCached := Entry{
		Media: Media{ID: 1001, MalID: 1, Episodes: 12, Duration: 24, Status: "RELEASING",
			Title: AnimeTitle{English: "Cached EN", Romaji: "Cached Show", Japanese: "Cached JA"}},
		Progress: 3, Repeat: 1, Score: 8, Status: "CURRENT",
		StartedAt:  FuzzyDate{Year: 2026, Month: 1, Day: 2},
		CoverImage: "mal1.jpg",
		UpdatedAt:  time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC),
	}
	if !entriesEqual(cached, wantCached) {
		t.Errorf("cached entry = %+v\nwant %+v", cached, wantCached)
	}

	looked := list.Rewatching[0]
	wantLooked := Entry{
		Media: Media{ID: 2002, MalID: 2, Episodes: 24, Duration: 10, Status: "FINISHED", Format: "TV", IsAdult: true,
			Title: AnimeTitle{English: "MAL EN", Romaji: "AL Romaji", Japanese: "AL Native"}},
		Status:     "REPEATING",
		CoverImage: "mal2.jpg",
	}
	if !entriesEqual(looked, wantLooked) {
		t.Errorf("looked-up entry = %+v\nwant %+v", looked, wantLooked)
	}

	planned := list.Planning[0]
	if planned.Media.ID != 4004 || planned.CoverImage != "al4.jpg" || planned.CompletedAt != (FuzzyDate{}) {
		t.Errorf("planned entry = %+v", planned)
	}

	saved, err := loadTrackingIDCache(config.StoragePath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.MalToAniList["2"] != 2002 || saved.MalToAniList["4"] != 4004 || saved.MalToAniList["1"] != 1001 {
		t.Fatalf("tracking cache = %v", saved.MalToAniList)
	}
	if _, ok := saved.MalToAniList["3"]; ok {
		t.Fatal("an unmatched id must not be cached")
	}
}

func TestUpdateMyAnimeListListStatusDropsBlankValues(t *testing.T) {
	config := myAnimeListTestConfig(t, &OAuthToken{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)})
	var form url.Values
	routeHTTPTo(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v2/anime/9/my_list_status" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		form, _ = url.ParseQuery(string(body))
		writeJSON(t, w, http.StatusOK, `{"status":"watching"}`)
	})

	err := updateMyAnimeListListStatus(config, 9, map[string]string{
		"score":       " ",
		"status":      "watching",
		"start_date":  "",
		"finish_date": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := url.Values{"status": {"watching"}, "start_date": {""}, "finish_date": {""}}
	if form.Encode() != want.Encode() {
		t.Fatalf("form = %v, want %v", form, want)
	}
}

func entriesEqual(a, b Entry) bool {
	return a.Media == b.Media && a.Progress == b.Progress && a.Repeat == b.Repeat && a.Score == b.Score &&
		a.Status == b.Status && a.StartedAt == b.StartedAt && a.CompletedAt == b.CompletedAt &&
		a.CoverImage == b.CoverImage && a.UpdatedAt.Equal(b.UpdatedAt)
}

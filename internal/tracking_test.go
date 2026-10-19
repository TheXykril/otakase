package internal

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNormalizeRemoteTrackerAliases(t *testing.T) {
	t.Parallel()

	aliases := map[string][]string{
		TrackingRemoteNone:        {"", "none", "off", "local"},
		TrackingRemoteAniList:     {"anilist", "ani-list"},
		TrackingRemoteMyAnimeList: {"mal", "myanimelist", "my-anime-list", "my_anime_list"},
		TrackingRemoteBoth:        {"both", "anilist,myanimelist"},
		"":                        {"not-a-provider"},
	}
	for want, spellings := range aliases {
		for _, spelling := range spellings {
			if got := canonicalTrackerName(spelling); got != want {
				t.Errorf("canonicalTrackerName(%q) = %q, want %q", spelling, got, want)
			}
		}
	}
}

func TestNormalizeTrackingConfig(t *testing.T) {
	t.Parallel()

	t.Run("local history stays on without a remote", func(t *testing.T) {
		cfg := &Config{TrackingRemote: "none", TrackingConfigured: true}
		normalizeTrackingConfig(cfg)
		if !cfg.TrackingLocal {
			t.Error("local history left off")
		}
		if cfg.TrackingRemote != TrackingRemoteNone {
			t.Errorf("remote became %q", cfg.TrackingRemote)
		}
	})

	t.Run("unknown remote goes back through setup", func(t *testing.T) {
		cfg := &Config{TrackingLocal: true, TrackingRemote: "totally-invalid", TrackingConfigured: true}
		normalizeTrackingConfig(cfg)
		if cfg.TrackingConfigured {
			t.Error("still marked configured")
		}
		if cfg.TrackingRemote != TrackingRemoteAniList {
			t.Errorf("remote = %q, want the AniList default", cfg.TrackingRemote)
		}
		if !cfg.TrackingLocal {
			t.Error("local history switched off")
		}
	})
}

func TestMainMenuHidesRemoteOnlyEntriesForLocalTracking(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		MenuOrder:      "CURRENT,PLANNING,ALL",
		MenuActions:    "UPDATE,PROVIDER",
		TrackingLocal:  true,
		TrackingRemote: TrackingRemoteNone,
	}
	tabs, actions := mainMenuLayout(cfg, AnimeList{})
	var tabKeys, actionKeys []string
	for _, tab := range tabs {
		tabKeys = append(tabKeys, tab.Key)
	}
	for _, action := range actions {
		actionKeys = append(actionKeys, action.Key)
	}
	if !slices.Equal(tabKeys, []string{"CURRENT", "ALL"}) {
		t.Errorf("tabs = %v", tabKeys)
	}
	if !slices.Equal(actionKeys, []string{"PROVIDER", "TRACKER"}) {
		t.Errorf("actions = %v", actionKeys)
	}
}

func TestLocalAnimeListFromHistoryUsesNextEpisodeAsProgressBase(t *testing.T) {
	t.Parallel()

	history := []Anime{
		{
			AnilistId:     123,
			MalId:         456,
			TotalEpisodes: 28,
			Title:         AnimeTitle{English: "Frieren", Romaji: "Sousou no Frieren"},
			Ep:            Episode{Number: 7},
		},
		{AnilistId: 9},
	}
	got := localAnimeListFromHistory(history).Watching
	if len(got) != 2 {
		t.Fatalf("%d watching entries, want 2", len(got))
	}

	first := got[0]
	wantMedia := Media{ID: 123, MalID: 456, Episodes: 28, Title: history[0].Title}
	if !reflect.DeepEqual(first.Media, wantMedia) {
		t.Errorf("media = %+v, want %+v", first.Media, wantMedia)
	}
	if first.Status != "CURRENT" || first.Progress != 6 {
		t.Errorf("episode 7 next gave %s/%d, want CURRENT/6", first.Status, first.Progress)
	}

	second := got[1]
	if second.Progress != 0 {
		t.Errorf("show never started has progress %d", second.Progress)
	}
	if second.Media.Title != (AnimeTitle{English: "9", Romaji: "9"}) {
		t.Errorf("untitled show should go by its ID, got %+v", second.Media.Title)
	}

	if empty := localAnimeListFromHistory(nil); hasAnyEntries(empty) {
		t.Errorf("no history gave %+v", empty)
	}
}

func TestTrackerPredicatesPerMode(t *testing.T) {
	t.Parallel()

	type flags struct{ remote, aniList, myAnimeList, dual bool }
	cases := map[string]struct {
		cfg  *Config
		want flags
	}{
		// A nil config behaves like the old AniList-only setup.
		"no config":   {nil, flags{true, true, false, false}},
		"local":       {&Config{TrackingRemote: TrackingRemoteNone}, flags{}},
		"anilist":     {&Config{TrackingRemote: TrackingRemoteAniList}, flags{true, true, false, false}},
		"myanimelist": {&Config{TrackingRemote: TrackingRemoteMyAnimeList}, flags{true, false, true, false}},
		"both":        {&Config{TrackingRemote: TrackingRemoteBoth}, flags{true, true, true, true}},
		// Remote, but nothing is written anywhere.
		"unknown": {&Config{TrackingRemote: "carrier-pigeon"}, flags{remote: true}},
	}
	for name, tc := range cases {
		got := flags{
			remote:      UsesRemoteTracking(tc.cfg),
			aniList:     UsesAniListTracking(tc.cfg),
			myAnimeList: UsesMyAnimeListTracking(tc.cfg),
			dual:        UsesDualRemoteTracking(tc.cfg),
		}
		if got != tc.want {
			t.Errorf("%s: %+v, want %+v", name, got, tc.want)
		}
	}
}

func TestTrackingCategoryEnabled(t *testing.T) {
	t.Parallel()

	local := &Config{TrackingRemote: TrackingRemoteNone}
	remote := &Config{TrackingRemote: TrackingRemoteAniList}
	for _, key := range []string{"UPDATE", "PLANNING", "SURPRISE"} {
		if trackingCategoryEnabled(local, key) {
			t.Errorf("%s offered with local tracking", key)
		}
		if !trackingCategoryEnabled(remote, key) {
			t.Errorf("%s hidden with AniList", key)
		}
	}
	if !trackingCategoryEnabled(local, "STATS") || !trackingCategoryEnabled(local, "CURRENT") {
		t.Error("stats and history-backed entries should always be there")
	}
	if trackingCategoryEnabled(remote, "MOVIES") || !trackingCategoryEnabled(&Config{ExperimentalMovies: true}, "MOVIES") {
		t.Error("movies should follow the experimental switch only")
	}
}

func TestMergeAnimeEntriesPrefersNewestRemoteUpdate(t *testing.T) {
	t.Parallel()

	at := func(hour int) time.Time { return time.Date(2026, 5, 17, hour, 0, 0, 0, time.UTC) }
	earlier := Entry{Media: Media{ID: 1, MalID: 101}, Status: "CURRENT", Progress: 3, UpdatedAt: at(10)}
	later := Entry{Media: Media{ID: 1, MalID: 101}, Status: "COMPLETED", Progress: 12, UpdatedAt: at(12)}

	for _, pair := range [][2]Entry{{earlier, later}, {later, earlier}} {
		got := mergeAnimeEntries(pair[0], pair[1])
		if got.Status != "COMPLETED" || got.Progress != 12 {
			t.Errorf("the later update should win in either order, got %+v", got)
		}
	}
}

func TestMergeAnimeEntriesWithoutTimesPrefersTheEntryFurtherAlong(t *testing.T) {
	t.Parallel()

	entry := func(progress int, stamped bool) Entry {
		e := Entry{Media: Media{ID: 1}, Status: "CURRENT", Progress: progress}
		if stamped {
			e.UpdatedAt = time.Now()
		}
		return e
	}
	cases := []struct {
		name               string
		existing, incoming Entry
		want               int
	}{
		{"a time beats no time", entry(9, false), entry(1, true), 1},
		{"tie goes further along", entry(2, false), entry(5, false), 5},
		{"identical", entry(5, false), entry(5, false), 5},
	}
	for _, tc := range cases {
		if got := mergeAnimeEntries(tc.existing, tc.incoming).Progress; got != tc.want {
			t.Errorf("%s: progress %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestMergeEntryMetadataKeepsTheScheduleAndAdultFlag(t *testing.T) {
	t.Parallel()

	schedule := &NextAiringEpisodeInfo{Episode: 5}
	fromMAL := Entry{Media: Media{ID: 4, Title: AnimeTitle{Romaji: "Show"}}, Status: "CURRENT", Progress: 4}
	fromAniList := Entry{
		ListID: 99, Repeat: 1, CoverImage: "cover",
		Media: Media{ID: 4, MalID: 400, IsAdult: true, NextAiringEpisode: schedule, Title: AnimeTitle{Romaji: "Other", English: "Show EN"}},
	}

	got := mergeEntryMetadata(fromMAL, fromAniList)
	switch {
	case got.Media.NextAiringEpisode != schedule:
		t.Error("airing schedule lost")
	case !got.Media.IsAdult:
		t.Error("adult flag lost")
	case got.Media.Title.Romaji != "Show" || got.Media.Title.English != "Show EN":
		t.Errorf("titles = %+v", got.Media.Title)
	case got.ListID != 99 || got.Media.MalID != 400 || got.Repeat != 1 || got.CoverImage != "cover":
		t.Errorf("missing fields not filled: %+v", got)
	case got.Progress != 4 || got.Status != "CURRENT":
		t.Errorf("list state should stay the preferred entry's: %+v", got)
	}
}

func TestSortedMediaIDs(t *testing.T) {
	t.Parallel()

	a := map[int]Entry{5: {}, 1: {}}
	b := map[int]Entry{3: {}, 5: {}}
	if got := sortedMediaIDs(a, b); !slices.Equal(got, []int{1, 3, 5}) {
		t.Errorf("sortedMediaIDs = %v", got)
	}
	if got := sortedMediaIDs(); len(got) != 0 {
		t.Errorf("no indexes gave %v", got)
	}
}

func TestBuildDualRemoteSyncPlanUsesNewestEntryAndPropagatesMissingAnime(t *testing.T) {
	t.Parallel()

	at := func(hour int) time.Time { return time.Date(2026, 5, 17, hour, 0, 0, 0, time.UTC) }
	aniList := AnimeList{Watching: []Entry{
		{Media: Media{ID: 1, MalID: 101}, Status: "CURRENT", Progress: 2, UpdatedAt: at(9)},
	}}
	myAnimeList := AnimeList{
		Completed: []Entry{{Media: Media{ID: 1, MalID: 101}, Status: "COMPLETED", Progress: 12, UpdatedAt: at(11)}},
		Planning:  []Entry{{Media: Media{ID: 2, MalID: 202}, Status: "PLANNING", UpdatedAt: at(8)}},
	}

	plan := buildDualRemoteSyncPlan(aniList, myAnimeList)
	if n := len(plan.MyAnimeListUpdates); n != 0 {
		t.Fatalf("MyAnimeList already has the winners but got %d updates", n)
	}
	var ids []int
	for _, update := range plan.AniListUpdates {
		ids = append(ids, update.Media.ID)
	}
	if !slices.Equal(ids, []int{1, 2}) {
		t.Fatalf("AniList updates for %v, want [1 2] in that order", ids)
	}

	winner, found := mapEntriesByMediaID(plan.Merged)[1]
	if !found {
		t.Fatal("media 1 missing from the merged list")
	}
	if winner.Status != "COMPLETED" || winner.Progress != 12 {
		t.Errorf("merge should keep the later MyAnimeList state, got %+v", winner)
	}
}

func TestBuildDualRemoteSyncPlanKeepsPrivateEntriesOffMyAnimeList(t *testing.T) {
	t.Parallel()

	hidden := Entry{Media: Media{ID: 8, MalID: 800}, Status: "CURRENT", Progress: 3, Private: true}
	plan := buildDualRemoteSyncPlan(AnimeList{Watching: []Entry{hidden}}, AnimeList{})
	if len(plan.MyAnimeListUpdates) != 0 {
		t.Errorf("private entry queued for MyAnimeList: %+v", plan.MyAnimeListUpdates)
	}
	if _, kept := mapEntriesByMediaID(plan.Merged)[8]; !kept {
		t.Error("private entry dropped from the merged list")
	}
}

// MyAnimeList does not always report a rewatch count AniList has, and that
// gap on its own must not cause a write on every launch.
func TestBuildDualRemoteSyncPlanIgnoresARewatchCountMyAnimeListLacks(t *testing.T) {
	t.Parallel()

	onAniList := Entry{
		Media:     Media{ID: 3, MalID: 303},
		Status:    "COMPLETED",
		Progress:  12,
		Repeat:    2,
		UpdatedAt: time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC),
	}
	onMAL := onAniList
	onMAL.Repeat = 0
	onMAL.UpdatedAt = onMAL.UpdatedAt.Add(-time.Hour)

	plan := buildDualRemoteSyncPlan(AnimeList{Completed: []Entry{onAniList}}, AnimeList{Completed: []Entry{onMAL}})
	if writes := len(plan.AniListUpdates) + len(plan.MyAnimeListUpdates); writes != 0 {
		t.Fatalf("%d writes queued: AniList %+v, MyAnimeList %+v", writes, plan.AniListUpdates, plan.MyAnimeListUpdates)
	}
}

func TestRemoteSyncPreviewSummary(t *testing.T) {
	t.Parallel()

	preview := remoteSyncPreview{Action: "Merge", AniListEntries: 3, MyAnimeListEntries: 2, Writes: 4}
	want := []string{"Merge: AniList 3, MyAnimeList 2.", "This will write 4 entries."}
	if got := preview.summary(); !slices.Equal(got, want) {
		t.Errorf("summary = %q", got)
	}

	preview.Deletes, preview.MissingMALIDs = 2, 1
	want = []string{
		"Merge: AniList 3, MyAnimeList 2.",
		"This will write 4 entries and delete 2 entries.",
		"1 entries need a MyAnimeList ID lookup.",
	}
	if got := preview.summary(); !slices.Equal(got, want) {
		t.Errorf("summary = %q", got)
	}
}

func TestMyAnimeListEntryWrites(t *testing.T) {
	t.Parallel()

	started := FuzzyDate{Year: 2025, Month: 1, Day: 2}
	finished := FuzzyDate{Year: 2025, Month: 3, Day: 4}

	t.Run("rewatched completion sends the count on its own", func(t *testing.T) {
		writes := myAnimeListEntryWrites(Entry{
			Status: "COMPLETED", Progress: 12, Score: 7.5, Repeat: 2,
			StartedAt: started, CompletedAt: finished,
		})
		want := []map[string]string{
			{
				"status":               "completed",
				"num_watched_episodes": "12",
				"score":                "8",
				"start_date":           "2025-01-02",
				"finish_date":          "2025-03-04",
			},
			{"num_times_rewatched": "2"},
		}
		if !reflect.DeepEqual(writes, want) {
			t.Errorf("writes = %v\nwant     %v", writes, want)
		}
	})

	t.Run("rewatch in progress is watching again", func(t *testing.T) {
		writes := myAnimeListEntryWrites(Entry{Status: "REPEATING", Progress: 3, Repeat: 1})
		if len(writes) != 1 {
			t.Fatalf("got %d writes, want one", len(writes))
		}
		fields := writes[0]
		if fields["status"] != "watching" || fields["is_rewatching"] != "true" || fields["num_times_rewatched"] != "1" {
			t.Errorf("fields = %v", fields)
		}
	})

	t.Run("ordinary entry is not a rewatch", func(t *testing.T) {
		writes := myAnimeListEntryWrites(Entry{Status: "CURRENT", Progress: 4})
		if len(writes) != 1 || writes[0]["is_rewatching"] != "false" || writes[0]["num_times_rewatched"] != "0" {
			t.Errorf("writes = %v", writes)
		}
	})
}

func TestMyAnimeListImportFieldsOnlySendWhatIsSet(t *testing.T) {
	t.Parallel()

	plain := myAnimeListImportFields(Entry{Status: "PLANNING"})
	if _, ok := plain["score"]; ok {
		t.Errorf("an unscored entry imported a score: %v", plain)
	}
	if _, ok := plain["num_times_rewatched"]; ok {
		t.Errorf("an entry never rewatched imported a count: %v", plain)
	}

	rewatch := myAnimeListImportFields(Entry{Status: "REPEATING", Progress: 2, Score: 6.4, Repeat: 1})
	want := map[string]string{
		"status":               "watching",
		"is_rewatching":        "true",
		"num_watched_episodes": "2",
		"score":                "6",
		"num_times_rewatched":  "1",
	}
	if !reflect.DeepEqual(rewatch, want) {
		t.Errorf("fields = %v, want %v", rewatch, want)
	}
}

// recordedWrite returns a trackerWrite that logs each step it is asked for.
func recordedWrite(steps *[]string, aniListErr, malIDErr, myAnimeListErr error) trackerWrite {
	return trackerWrite{
		aniList: func() error {
			*steps = append(*steps, "anilist")
			return aniListErr
		},
		malID: func() (int, error) {
			*steps = append(*steps, "lookup")
			return 77, malIDErr
		},
		myAnimeList: func(malID int) error {
			*steps = append(*steps, "myanimelist")
			if malID != 77 {
				return errors.New("wrong MyAnimeList ID")
			}
			return myAnimeListErr
		},
	}
}

func TestWriteTrackersFollowsTheConfiguredTracker(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name   string
		config *Config
		anime  *Anime
		want   []string
	}{
		{"local", &Config{TrackingRemote: TrackingRemoteNone}, nil, nil},
		{"anilist", &Config{TrackingRemote: TrackingRemoteAniList}, nil, []string{"anilist"}},
		{"myanimelist", &Config{TrackingRemote: TrackingRemoteMyAnimeList}, nil, []string{"lookup", "myanimelist"}},
		{"both", &Config{TrackingRemote: TrackingRemoteBoth}, nil, []string{"anilist", "lookup", "myanimelist"}},
		{"skipped for this session", &Config{TrackingRemote: TrackingRemoteBoth}, &Anime{SkipRemoteSync: true}, nil},
	} {
		var steps []string
		if err := writeTrackers(c.config, c.anime, recordedWrite(&steps, nil, nil, nil)); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if !reflect.DeepEqual(steps, c.want) {
			t.Errorf("%s: steps = %v, want %v", c.name, steps, c.want)
		}
	}
}

func TestWriteTrackersWithBothReportsTheAniListErrorFirst(t *testing.T) {
	t.Parallel()

	both := &Config{TrackingRemote: TrackingRemoteBoth}
	aniListDown := errors.New("anilist down")
	noMapping := errors.New("no mapping")
	malDown := errors.New("myanimelist down")

	for _, c := range []struct {
		name                         string
		aniList, lookup, myAnimeList error
		want                         error
		steps                        []string
	}{
		{"AniList fails, MyAnimeList still written", aniListDown, nil, nil, aniListDown, []string{"anilist", "lookup", "myanimelist"}},
		{"both fail", aniListDown, nil, malDown, aniListDown, []string{"anilist", "lookup", "myanimelist"}},
		{"lookup fails", nil, noMapping, nil, noMapping, []string{"anilist", "lookup"}},
		{"AniList and lookup fail", aniListDown, noMapping, nil, aniListDown, []string{"anilist", "lookup"}},
		{"MyAnimeList fails", nil, nil, malDown, malDown, []string{"anilist", "lookup", "myanimelist"}},
	} {
		var steps []string
		err := writeTrackers(both, nil, recordedWrite(&steps, c.aniList, c.lookup, c.myAnimeList))
		if !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
		if !reflect.DeepEqual(steps, c.steps) {
			t.Errorf("%s: steps = %v, want %v", c.name, steps, c.steps)
		}
	}
}

// trackerRequest is one request a tracker write sent.
type trackerRequest struct {
	host   string
	method string
	path   string
	form   map[string]string
	query  string
}

// recordTrackerRequests points both trackers at a local server and records
// what reaches it. AniList is answered as if every mutation succeeded and
// every show maps to MyAnimeList ID malID.
func recordTrackerRequests(t *testing.T, tracking string, malID int) (*Config, func() []trackerRequest) {
	t.Helper()

	config := myAnimeListTestConfig(t, &OAuthToken{AccessToken: "mal-access", RefreshToken: "mal-refresh", ExpiresAt: time.Now().Add(time.Hour)})
	config.TrackingRemote = tracking
	withGlobalConfig(t, config)

	previousUser, previousAnime := GetGlobalUser(), GetGlobalAnime()
	SetGlobalUser(&User{})
	SetGlobalAnime(nil)
	t.Cleanup(func() {
		SetGlobalUser(previousUser)
		SetGlobalAnime(previousAnime)
	})

	var mu sync.Mutex
	var requests []trackerRequest
	routeHTTPTo(t, func(w http.ResponseWriter, r *http.Request) {
		request := trackerRequest{host: r.Host, method: r.Method, path: r.URL.Path, form: map[string]string{}}
		switch r.Host {
		case "graphql.anilist.co":
			body, _ := io.ReadAll(r.Body)
			var call struct{ Query string }
			_ = json.Unmarshal(body, &call)
			request.query = call.Query
			if strings.Contains(call.Query, "idMal") {
				writeJSON(t, w, http.StatusOK, `{"data":{"Media":{"idMal":`+strconv.Itoa(malID)+`}}}`)
			} else {
				writeJSON(t, w, http.StatusOK, `{"data":{"SaveMediaListEntry":{"id":1}}}`)
			}
		case "api.myanimelist.net":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse form: %v", err)
			}
			for key := range r.PostForm {
				request.form[key] = r.PostForm.Get(key)
			}
			writeJSON(t, w, http.StatusOK, `{}`)
		default:
			t.Errorf("unexpected request to %s%s", r.Host, r.URL.Path)
		}
		mu.Lock()
		requests = append(requests, request)
		mu.Unlock()
	})

	return config, func() []trackerRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]trackerRequest(nil), requests...)
	}
}

func TestUpdateAnimeProgressWithBothWritesAniListThenMyAnimeList(t *testing.T) {
	_, requests := recordTrackerRequests(t, TrackingRemoteBoth, 0)
	// The loaded list already knows the MyAnimeList ID, so none is looked up.
	GetGlobalUser().AnimeList = AnimeList{Watching: []Entry{{Media: Media{ID: 21, MalID: 2100}, Status: "CURRENT"}}}

	if err := UpdateAnimeProgress("anilist-token", 21, 7); err != nil {
		t.Fatalf("UpdateAnimeProgress: %v", err)
	}

	got := requests()
	if len(got) != 2 {
		t.Fatalf("sent %d requests, want 2: %+v", len(got), got)
	}
	if got[0].host != "graphql.anilist.co" || !strings.Contains(got[0].query, "SaveMediaListEntry") {
		t.Errorf("first request should save the AniList entry, got %+v", got[0])
	}
	if got[1].method != http.MethodPut || got[1].path != "/v2/anime/2100/my_list_status" {
		t.Errorf("second request = %s %s, want the MyAnimeList entry for 2100", got[1].method, got[1].path)
	}
	if got[1].form["num_watched_episodes"] != "7" {
		t.Errorf("MyAnimeList form = %v", got[1].form)
	}
}

func TestRateAnimeWithScoreOnMyAnimeListLooksUpTheIDAndRounds(t *testing.T) {
	_, requests := recordTrackerRequests(t, TrackingRemoteMyAnimeList, 4242)

	if err := RateAnimeWithScore("token", 42, 7.5); err != nil {
		t.Fatalf("RateAnimeWithScore: %v", err)
	}

	got := requests()
	if len(got) != 2 {
		t.Fatalf("sent %d requests, want a lookup and a write: %+v", len(got), got)
	}
	if !strings.Contains(got[0].query, "idMal") {
		t.Errorf("first request should look up the MyAnimeList ID, got %+v", got[0])
	}
	if got[1].path != "/v2/anime/4242/my_list_status" || got[1].form["score"] != "8" {
		t.Errorf("write = %s %v, want score 8 on 4242", got[1].path, got[1].form)
	}
}

func TestStartAnimeRewatchUsesTheAnimeItIsGiven(t *testing.T) {
	_, requests := recordTrackerRequests(t, TrackingRemoteMyAnimeList, 0)
	// The global anime is skipped this session; the one being rewatched is not.
	SetGlobalAnime(&Anime{SkipRemoteSync: true})

	if err := StartAnimeRewatch("token", Anime{AnilistId: 5, MalId: 500}); err != nil {
		t.Fatalf("StartAnimeRewatch: %v", err)
	}

	got := requests()
	if len(got) != 1 {
		t.Fatalf("sent %d requests, want only the MyAnimeList write: %+v", len(got), got)
	}
	want := myAnimeListRewatchStart(currentFuzzyDate())
	delete(want, "finish_date")
	for key, value := range want {
		if got[0].form[key] != value {
			t.Errorf("%s = %q, want %q", key, got[0].form[key], value)
		}
	}
	if value, sent := got[0].form["finish_date"]; !sent || value != "" {
		t.Errorf("the old finish date should be cleared, form = %v", got[0].form)
	}
}

func TestTrackerWritesAreSkippedForASkippedSession(t *testing.T) {
	_, requests := recordTrackerRequests(t, TrackingRemoteBoth, 1)
	SetGlobalAnime(&Anime{SkipRemoteSync: true})

	if err := AddAnimeToList(1, "PLANNING", "token"); err != nil {
		t.Fatalf("AddAnimeToList: %v", err)
	}
	if err := UpdateAnimeStatus("token", 1, "PAUSED"); err != nil {
		t.Fatalf("UpdateAnimeStatus: %v", err)
	}
	if got := requests(); len(got) != 0 {
		t.Fatalf("a skipped session still wrote: %+v", got)
	}
}

func TestLoadTokenForConfiguredTracking(t *testing.T) {
	routeHTTPTo(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request to %s%s", r.Host, r.URL.Path)
	})
	previousUser := GetGlobalUser()
	SetGlobalUser(nil)
	t.Cleanup(func() { SetGlobalUser(previousUser) })

	malToken := &OAuthToken{AccessToken: "mal-access", RefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour)}
	withAniList := myAnimeListTestConfig(t, malToken)
	writeAnilistTokenFileForTest(t, withAniList.StoragePath, "anilist-access")
	withoutAniList := myAnimeListTestConfig(t, malToken)

	for _, c := range []struct {
		name   string
		config *Config
		remote string
		want   string
	}{
		{"anilist", withAniList, TrackingRemoteAniList, "anilist-access"},
		{"myanimelist", withAniList, TrackingRemoteMyAnimeList, "mal-access"},
		{"both prefers AniList", withAniList, TrackingRemoteBoth, "anilist-access"},
		{"both without an AniList token", withoutAniList, TrackingRemoteBoth, "mal-access"},
		{"local", withAniList, TrackingRemoteNone, ""},
	} {
		config := *c.config
		config.TrackingRemote = c.remote
		user := &User{Token: "stale"}
		if err := LoadTokenForConfiguredTracking(&config, user); err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if user.Token != c.want {
			t.Errorf("%s: token = %q, want %q", c.name, user.Token, c.want)
		}
	}

	aniListOnly := *withoutAniList
	aniListOnly.TrackingRemote = TrackingRemoteAniList
	user := &User{Token: "kept"}
	if err := LoadTokenForConfiguredTracking(&aniListOnly, user); err == nil || user.Token != "kept" {
		t.Errorf("a missing AniList token should fail and leave the token alone, got err %v, token %q", err, user.Token)
	}
}

func TestRefreshUserAnimeListPublishesLocalHistory(t *testing.T) {
	storage := t.TempDir()
	if err := LocalUpdateAnime(HistoryFile(storage), 77, "provider-77", 4, 0, 24, "Local Show", "anikoto"); err != nil {
		t.Fatalf("seed history: %v", err)
	}
	config := &Config{StoragePath: storage, TrackingRemote: TrackingRemoteNone}

	user := &User{}
	if err := RefreshUserAnimeList(config, user); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if user.ListSync == nil || len(user.AnimeList.Watching) != 1 || user.AnimeList.Watching[0].Progress != 3 {
		t.Fatalf("local list = %+v", user.AnimeList)
	}

	// After the next episode, a refresh updates the same ListSync so open
	// menus redraw.
	listSync := user.ListSync
	updates := listSync.Updates()
	if err := LocalUpdateAnime(HistoryFile(storage), 77, "provider-77", 5, 0, 24, "Local Show", "anikoto"); err != nil {
		t.Fatalf("advance history: %v", err)
	}
	if err := RefreshUserAnimeList(config, user); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if user.ListSync != listSync {
		t.Error("the refresh replaced the ListSync menus are watching")
	}
	select {
	case list := <-updates:
		if list.Watching[0].Progress != 4 {
			t.Errorf("published progress %d, want 4", list.Watching[0].Progress)
		}
	case <-time.After(time.Second):
		t.Error("the refreshed list was not published")
	}
}

func TestRemoteTrackingDisplayName(t *testing.T) {
	t.Parallel()

	for remote, want := range map[string]string{
		TrackingRemoteAniList:     "AniList",
		TrackingRemoteMyAnimeList: "MyAnimeList",
		TrackingRemoteBoth:        "AniList + MyAnimeList",
		TrackingRemoteNone:        "remote tracking",
	} {
		if got := RemoteTrackingDisplayName(&Config{TrackingRemote: remote}); got != want {
			t.Errorf("%q: got %q, want %q", remote, got, want)
		}
	}
}

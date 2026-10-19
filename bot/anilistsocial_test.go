package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestRateLimiter(t *testing.T) {
	now := time.Unix(1000, 0)
	var slept time.Duration
	l := &rateLimiter{per: 3, window: time.Minute, now: func() time.Time { return now },
		sleep: func(d time.Duration) { slept += d; now = now.Add(d) }}
	for range 3 {
		l.wait()
	}
	if slept != 0 {
		t.Fatalf("slept %v for the first three", slept)
	}
	now = now.Add(10 * time.Second)
	l.wait()
	if slept != 50*time.Second {
		t.Errorf("fourth call slept %v, want 50s", slept)
	}
}

func TestListCache(t *testing.T) {
	now := time.Unix(1000, 0)
	calls := map[int]int{}
	c := &listCache{lists: map[int]cachedList{}, now: func() time.Time { return now }, fetch: func(id int) ([]listEntry, error) {
		calls[id]++
		switch id {
		case 2:
			return nil, errPrivateList
		case 3:
			return nil, errors.New("anilist down")
		}
		return []listEntry{{MediaID: 1}}, nil
	}}
	c.get(1)
	c.get(1)
	c.get(2)
	c.get(2)
	c.get(3)
	c.get(3)
	if calls[1] != 1 || calls[2] != 1 || calls[3] != 2 {
		t.Errorf("calls %v", calls)
	}
	now = now.Add(15 * time.Minute) // private lists are read again, lists not yet
	c.get(1)
	if _, err := c.get(2); !errors.Is(err, errPrivateList) {
		t.Errorf("private: %v", err)
	}
	if calls[1] != 1 || calls[2] != 2 {
		t.Errorf("after 15 minutes: %v", calls)
	}
	now = now.Add(time.Hour)
	c.get(1)
	if calls[1] != 2 {
		t.Errorf("after an hour: %v", calls)
	}
}

func TestFetchAniListList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Variables map[string]any }
		json.NewDecoder(r.Body).Decode(&req)
		switch req.Variables["id"] {
		case 404.0:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"errors":[{"message":"Private User"}]}`))
		default:
			w.Write([]byte(`{"data":{"MediaListCollection":{"hasNextChunk":false,"lists":[
				{"isCustomList":false,"entries":[{"mediaId":1,"status":"COMPLETED","score":90,"completedAt":{"year":2026,"month":10,"day":2},"media":{"id":1,"title":{"english":"Frieren"}}}]},
				{"isCustomList":true,"entries":[{"mediaId":1,"status":"COMPLETED","score":90,"media":{"id":1}}]},
				{"isCustomList":false,"entries":[{"mediaId":2,"status":"PLANNING","score":0,"completedAt":{"year":null},"media":{"id":2,"title":{"romaji":"Dandadan"}}}]}
			]}}}`))
		}
	}))
	defer srv.Close()
	old := anilistAPI
	anilistAPI = srv.URL
	defer func() { anilistAPI = old }()

	list, err := fetchAniListList(7)
	if err != nil || len(list) != 2 {
		t.Fatalf("list %v %v", list, err)
	}
	if list[0].Media.name() != "Frieren" || list[0].Score != 90 || list[0].CompletedAt.Day != 2 || list[1].Media.name() != "Dandadan" || list[1].seen() {
		t.Errorf("entries %+v", list)
	}
	if _, err := fetchAniListList(404); !errors.Is(err, errPrivateList) {
		t.Errorf("private list: %v", err)
	}
}

func TestSeasons(t *testing.T) {
	for _, c := range []struct {
		date   string
		season string
	}{{"2026-01-01", "WINTER"}, {"2026-03-31", "WINTER"}, {"2026-04-01", "SPRING"}, {"2026-08-15", "SUMMER"}, {"2026-12-31", "FALL"}} {
		d, _ := time.Parse("2006-01-02", c.date)
		if s, y := seasonOf(d); s != c.season || y != 2026 {
			t.Errorf("%s: %s %d", c.date, s, y)
		}
	}
	if s, y := nextSeason("FALL", 2026); s != "WINTER" || y != 2027 {
		t.Errorf("after fall: %s %d", s, y)
	}
	if s, y := nextSeason("SPRING", 2026); s != "SUMMER" || y != 2026 {
		t.Errorf("after spring: %s %d", s, y)
	}
	if got := seasonTitle("FALL", 2026); got != "Fall 2026" {
		t.Errorf("title %q", got)
	}
}

func TestSeasonPreviewDue(t *testing.T) {
	at := func(s string) time.Time { d, _ := time.Parse("2006-01-02 15", s); return d }
	for _, c := range []struct {
		now    string
		posted string
		due    bool
	}{
		{"2026-10-01 05", "", false},           // before the airing hour
		{"2026-10-01 06", "", true},            // at it
		{"2026-10-01 06", "FALL 2026", false},  // posted already
		{"2026-10-01 06", "SUMMER 2026", true}, // last season's doesn't count
		{"2026-10-03 01", "", true},            // bot was offline on the first
		{"2026-10-04 12", "", false},           // too late
		{"2026-11-01 12", "", false},           // not a season's first month
		{"2027-01-01 07", "FALL 2026", true},
	} {
		s, y, due := seasonPreviewDue(at(c.now), 6, c.posted)
		if due != c.due {
			t.Errorf("%s posted %q: due %v", c.now, c.posted, due)
		}
		if due && (s == "" || y == 0) {
			t.Errorf("%s: no season", c.now)
		}
	}
}

func TestSeasonMessage(t *testing.T) {
	var shows []seasonShow
	for k := range 30 {
		s := seasonShow{Media: mediaBrief{ID: k + 1, Format: "TV"}, Start: fuzzyDate{2026, 10, 3}, Sequel: k%2 == 1}
		s.Media.Title.English = "Show " + string(rune('A'+k%26))
		shows = append(shows, s)
	}
	if got := seasonFacts(shows[1]); got != "Sequel · TV · starts 3 Oct" {
		t.Errorf("facts %q", got)
	}
	comps := seasonMessage("Fall 2026", shows, "attachment://season.png")
	box := comps[0].(discordgo.Container)
	var menu discordgo.SelectMenu
	for _, c := range box.Components {
		if row, ok := c.(discordgo.ActionsRow); ok {
			menu = row.Components[0].(discordgo.SelectMenu)
		}
	}
	if menu.CustomID != "season-pick" || len(menu.Options) != 25 {
		t.Errorf("menu %q with %d options", menu.CustomID, len(menu.Options))
	}
	text := box.Components[2].(discordgo.TextDisplay).Content
	if !strings.Contains(text, "**10.**") || strings.Contains(text, "**11.**") {
		t.Errorf("list: %s", text)
	}
}

func entry(id int, status string, score float64, genres ...string) listEntry {
	e := listEntry{MediaID: id, Status: status, Score: score, Media: mediaBrief{ID: id, Genres: genres, Popularity: id}}
	e.Media.Title.English = "Show " + string(rune('A'+id))
	return e
}

func TestRankShows(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	week := now.Add(-7 * 24 * time.Hour)
	done := func(id int, updated time.Time, completed fuzzyDate) listEntry {
		e := entry(id, "COMPLETED", 0)
		e.UpdatedAt, e.CompletedAt = updated.Unix(), completed
		return e
	}
	adult := entry(9, "CURRENT", 0)
	adult.Media.IsAdult = true
	lists := map[string][]listEntry{
		"a": {entry(1, "CURRENT", 0), entry(2, "CURRENT", 0), adult, done(5, now.Add(-time.Hour), fuzzyDate{2026, 10, 11})},
		"b": {entry(1, "CURRENT", 0), entry(3, "PLANNING", 0), adult, done(5, now.Add(-time.Hour), fuzzyDate{})},
		"c": {entry(1, "CURRENT", 0), entry(2, "CURRENT", 0), entry(4, "CURRENT", 0),
			done(6, now.Add(-time.Hour), fuzzyDate{2025, 1, 1}), // finished long ago, edited today
			done(7, now.Add(-10*24*time.Hour), fuzzyDate{})},
	}
	w := rankShows(lists, isWatching, 10)
	if len(w) != 3 || w[0].Media.ID != 1 || w[0].Members != 3 || w[1].Media.ID != 2 || w[2].Media.ID != 4 {
		t.Errorf("watching %+v", w)
	}
	f := rankShows(lists, finishedSince(week), 10)
	if len(f) != 1 || f[0].Media.ID != 5 || f[0].Members != 2 {
		t.Errorf("finished %+v", f)
	}
	if got := rankShows(lists, isWatching, 2); len(got) != 2 {
		t.Errorf("limit: %d", len(got))
	}
}

func TestCompareLists(t *testing.T) {
	you := []listEntry{entry(1, "COMPLETED", 90), entry(2, "COMPLETED", 60), entry(3, "CURRENT", 0), entry(4, "PLANNING", 0), entry(5, "COMPLETED", 80)}
	them := []listEntry{entry(1, "COMPLETED", 80), entry(2, "COMPLETED", 80), entry(3, "COMPLETED", 70), entry(4, "COMPLETED", 100),
		entry(5, "COMPLETED", 80), entry(6, "COMPLETED", 95), entry(7, "COMPLETED", 60), entry(8, "PLANNING", 100), entry(9, "COMPLETED", 75)}
	c := compareLists(you, them)
	if c.Common != 4 {
		t.Errorf("common %d", c.Common)
	}
	// diffs 10, 20, 0 → mean 10
	if c.Agreement != 90 {
		t.Errorf("agreement %d", c.Agreement)
	}
	if len(c.Shared) != 3 || c.Shared[0].Media.ID != 1 || c.Shared[1].Media.ID != 5 || c.Shared[2].Media.ID != 2 {
		t.Errorf("shared %+v", c.Shared)
	}
	// 4 is on your list as planning; 7 is under 7/10; 8 they only plan.
	if len(c.Loved) != 2 || c.Loved[0].MediaID != 6 || c.Loved[1].MediaID != 9 {
		t.Errorf("loved %+v", c.Loved)
	}
	if none := compareLists(nil, them); none.Agreement != -1 || none.Common != 0 {
		t.Errorf("nothing shared: %+v", none)
	}
	if tenScore(85) != "8.5" || tenScore(90) != "9" {
		t.Errorf("ten score %q %q", tenScore(85), tenScore(90))
	}
}

func TestRecommendations(t *testing.T) {
	mine := []listEntry{entry(1, "COMPLETED", 95, "Action", "Drama"), entry(2, "COMPLETED", 60, "Comedy"),
		entry(3, "CURRENT", 0, "Action"), entry(4, "PLANNING", 0, "Romance"), entry(5, "DROPPED", 10, "Horror")}
	if got := favouriteGenres(mine, 2); strings.Join(got, ",") != "Action,Drama" {
		t.Errorf("genres %v", got)
	}
	if got := topRated(mine, 2); len(got) != 2 || got[0].MediaID != 1 || got[1].MediaID != 2 {
		t.Errorf("top rated %+v", got)
	}
	onList := map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true}

	pool := recPool{}
	rec := pool.get(entry(10, "", 0, "Action").Media)
	rec.Because, rec.Rating = []string{"Show B"}, 120
	pool.addFans(map[string][]listEntry{
		"a": {entry(11, "COMPLETED", 95), entry(12, "COMPLETED", 85), entry(4, "COMPLETED", 100), entry(13, "COMPLETED", 70)},
		"b": {entry(11, "COMPLETED", 90), entry(12, "COMPLETED", 95)},
		"c": {entry(11, "COMPLETED", 92)},
	})
	pool.get(entry(14, "", 0, "Action", "Drama").Media).FromGenre = true
	pool.get(entry(15, "", 0, "Action").Media).FromGenre = true // one genre only: left out

	recs := rankRecommendations(pool, onList, []string{"Action", "Drama"}, 5)
	var ids []int
	for _, r := range recs {
		ids = append(ids, r.Media.ID)
	}
	want := []int{11, 10, 12, 14}
	if len(ids) != len(want) {
		t.Fatalf("recommended %v, want %v", ids, want)
	}
	for k := range want {
		if ids[k] != want[k] {
			t.Fatalf("recommended %v, want %v", ids, want)
		}
	}
	for k, want := range []string{"3 people here rated it 9+", "Because you liked Show B", "2 people here rated it 8+", "More Action and Drama, your favourite genres"} {
		if got := recs[k].reason(); got != want {
			t.Errorf("reason %d: %q, want %q", k, got, want)
		}
	}
	if joinAnd([]string{"a", "b", "c"}) != "a, b and c" || joinAnd([]string{"a"}) != "a" {
		t.Error("joinAnd")
	}
}

func TestPlanCard(t *testing.T) {
	var info planShowInfo
	info.ID, info.IDMal, info.SiteURL, info.Status, info.Format, info.Episodes = 5, 9, "https://anilist.co/anime/5", "NOT_YET_RELEASED", "TV", 12
	info.Title.English = "Frieren"
	info.StartDate = fuzzyDate{2026, 10, 3}
	e, comps := planCard(info)
	if !strings.HasPrefix(e.Description, "TV · 12 episodes · starts 3 Oct 2026") {
		t.Errorf("description %q", e.Description)
	}
	row := comps[0].(discordgo.ActionsRow)
	if len(row.Components) != 5 || row.Components[4].(discordgo.Button).CustomID != "app-queue:5" || row.Components[0].(discordgo.Button).CustomID != "plan-watch:5" || row.Components[1].(discordgo.Button).CustomID != "airing-follow:5" {
		t.Errorf("buttons %+v", row.Components)
	}
	info.Status, info.IDMal = "FINISHED", 0
	if _, comps := planCard(info); len(comps[0].(discordgo.ActionsRow).Components) != 3 {
		t.Errorf("finished show buttons %+v", comps)
	}
	if startText(fuzzyDate{2027, 1, 0}) != "Jan 2027" || startText(fuzzyDate{2027, 0, 0}) != "2027" {
		t.Error("start text")
	}
}

func TestAniListPlan(t *testing.T) {
	saved := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string
			Variables map[string]any
		}
		json.NewDecoder(r.Body).Decode(&req)
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"errors":[{"message":"Invalid token"}]}`))
			return
		}
		switch {
		case strings.Contains(req.Query, "SaveMediaListEntry"):
			saved++
			w.Write([]byte(`{"data":{"SaveMediaListEntry":{"id":1,"status":"PLANNING"}}}`))
		case req.Variables["id"] == 1.0:
			w.Write([]byte(`{"data":{"Media":{"mediaListEntry":{"status":"CURRENT"}}}}`))
		default:
			w.Write([]byte(`{"data":{"Media":{"mediaListEntry":null}}}`))
		}
	}))
	defer srv.Close()
	old := anilistAPI
	anilistAPI = srv.URL
	defer func() { anilistAPI = old }()

	if added, status, err := anilistPlan("good", 1); err != nil || added || status != "CURRENT" || saved != 0 {
		t.Errorf("on the list already: %v %q %v saved %d", added, status, err, saved)
	}
	if added, status, err := anilistPlan("good", 2); err != nil || !added || status != "PLANNING" || saved != 1 {
		t.Errorf("new: %v %q %v saved %d", added, status, err, saved)
	}
	if _, _, err := anilistPlan("old", 2); !errors.Is(err, errAniListToken) {
		t.Errorf("bad token: %v", err)
	}
}

func TestSocialPictures(t *testing.T) {
	check := func(name string, b []byte, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := png.Decode(bytes.NewReader(b)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	tiles := []tileShow{{Title: "A very long title that has to wrap onto two lines at least", Sub: "Sequel · TV · starts 3 Oct", Chip: "SEQUEL"}, {Title: "B", Chip: "NEW"}, {Title: "C"}}
	b, err := renderCoverBoard(coverBoard{Label: "SEASON PREVIEW", Title: "Fall 2026", Count: "48", Unit: " new shows", SpotLabel: "MOST POPULAR", Shows: tiles})
	check("cover board", b, err)
	b, err = renderCoverBoard(coverBoard{Label: "SERVER TOP", Title: "Watching now", Count: "0", Unit: " linked", Empty: "Nothing"})
	check("empty cover board", b, err)
	b, err = renderCompareCard(compareCard{You: "Xykril", Them: "SomeoneWithAVeryLongAniListName", Common: 42, Agreement: 87,
		Shared: []tileShow{{Title: "Frieren", Chip: "9 · 8.5"}}, Loved: nil})
	check("compare card", b, err)
	b, err = renderCompareCard(compareCard{You: "a", Them: "b", Agreement: -1})
	check("compare card without scores", b, err)
}

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

// appTestBot is a bot with an empty store and fresh app limits and state.
func appTestBot(t *testing.T) *bot {
	t.Helper()
	linkStartLimit = newRateLimit(10, time.Hour)
	linkPollLimit = newRateLimit(120, time.Minute)
	linkCodeFails = newRateLimit(5, 10*time.Minute)
	heartbeatLimit = newRateLimit(4, time.Minute)
	reportLimit = newRateLimit(3, time.Hour)
	newsLimit = newRateLimit(30, time.Minute)
	pendingLinks.byCode, pendingLinks.byPoll = map[string]*pendingLink{}, map[string]*pendingLink{}
	watchingNow.m = map[string]nowWatching{}
	newsCache.reply, newsCache.at, newsCache.failed = nil, time.Time{}, time.Time{}
	appNow = time.Now
	t.Cleanup(func() { appNow = time.Now; fetchNews = (*bot).fetchNews })
	return &bot{store: openStore(filepath.Join(t.TempDir(), "s.json"))}
}

// apiDo sends one request through the public API's routes.
func apiDo(t *testing.T, b *bot, method, path string, body io.Reader, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	currentBot = b
	mux := http.NewServeMux()
	for p, h := range apiRoutes {
		mux.HandleFunc(p, h)
	}
	r := httptest.NewRequest(method, path, body)
	r.RemoteAddr = "192.0.2.1:1234"
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	apiHeaders(mux).ServeHTTP(w, r)
	return w
}

func apiJSON(t *testing.T, b *bot, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	hdr := map[string]string{"Content-Type": "application/json"}
	if token != "" {
		hdr["Authorization"] = "Bearer " + token
	}
	return apiDo(t, b, http.MethodPost, path, strings.NewReader(body), hdr)
}

// testToken links an app for user and returns its token.
func testToken(b *bot, user string) string {
	tok := newToken(32)
	b.store.update(func(d *storeData) { d.AppLinks[hashToken(tok)] = appLink{User: user, Created: appNow()} })
	return tok
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	return m
}

func TestLinkFlow(t *testing.T) {
	b := appTestBot(t)
	w := apiJSON(t, b, "/v1/link/start", "{}", "")
	if w.Code != http.StatusOK {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	start := decode(t, w)
	code, poll := start["code"].(string), start["poll"].(string)
	if !regexp.MustCompile(`^[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}$`).MatchString(code) {
		t.Fatalf("code %q", code)
	}
	if start["expires_in"].(float64) != 600 || len(poll) != 64 {
		t.Fatalf("start: %v", start)
	}
	pollBody := `{"poll":"` + poll + `"}`
	if w := apiJSON(t, b, "/v1/link/poll", pollBody, ""); w.Code != http.StatusAccepted || decode(t, w)["status"] != "pending" {
		t.Fatalf("pending poll: %d %s", w.Code, w.Body)
	}
	if err := bindLink("u1", "Xykril", "  "+strings.ToLower(code)+" ", appNow()); err != nil {
		t.Fatalf("bind: %v", err)
	}
	w = apiJSON(t, b, "/v1/link/poll", pollBody, "")
	if w.Code != http.StatusOK {
		t.Fatalf("poll: %d %s", w.Code, w.Body)
	}
	got := decode(t, w)
	token := got["token"].(string)
	if len(token) != 64 || got["discord_name"] != "Xykril" {
		t.Fatalf("poll: %v", got)
	}
	b.store.view(func(d *storeData) {
		if d.AppLinks[hashToken(token)].User != "u1" {
			t.Error("link not stored by token hash")
		}
		if _, plain := d.AppLinks[token]; plain {
			t.Error("token stored in plain")
		}
	})
	if w := apiJSON(t, b, "/v1/link/poll", pollBody, ""); w.Code != http.StatusGone {
		t.Fatalf("token must come once, got %d", w.Code)
	}
	if w := apiJSON(t, b, "/v1/heartbeat", `{"version":"26.7.0","os":"linux"}`, token); w.Code != http.StatusOK {
		t.Fatalf("heartbeat with new token: %d %s", w.Code, w.Body)
	}
	if w := apiJSON(t, b, "/v1/unlink", `{}`, token); w.Code != http.StatusOK {
		t.Fatalf("unlink: %d", w.Code)
	}
	if w := apiJSON(t, b, "/v1/heartbeat", `{}`, token); w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token: %d", w.Code)
	}
	if w := apiJSON(t, b, "/v1/unlink", `{}`, token); w.Code != http.StatusUnauthorized {
		t.Fatalf("unlink twice: %d", w.Code)
	}
}

func TestLinkStartRateLimit(t *testing.T) {
	b := appTestBot(t)
	for k := 0; k < 10; k++ {
		if w := apiJSON(t, b, "/v1/link/start", "{}", ""); w.Code != http.StatusOK {
			t.Fatalf("start %d: %d", k, w.Code)
		}
	}
	if w := apiJSON(t, b, "/v1/link/start", "{}", ""); w.Code != http.StatusTooManyRequests {
		t.Fatalf("11th start: %d", w.Code)
	}
	w := apiDo(t, b, http.MethodPost, "/v1/link/start", nil, map[string]string{"CF-Connecting-IP": "198.51.100.7"})
	if w.Code != http.StatusOK {
		t.Fatalf("other IP: %d", w.Code)
	}
}

func TestLinkExpires(t *testing.T) {
	b := appTestBot(t)
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	appNow = func() time.Time { return now }
	code, poll, err := startLink(now)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(11 * time.Minute)
	if err := bindLink("u1", "Xy", code, now); err != errLinkWrong {
		t.Fatalf("bind expired code: %v", err)
	}
	if w := apiJSON(t, b, "/v1/link/poll", `{"poll":"`+poll+`"}`, ""); w.Code != http.StatusGone || decode(t, w)["error"] != "expired" {
		t.Fatalf("expired poll: %d %s", w.Code, w.Body)
	}
	// A code bound just before it expires still leaves time for the poll.
	code, poll, _ = startLink(now)
	if err := bindLink("u1", "Xy", code, now.Add(9*time.Minute+50*time.Second)); err != nil {
		t.Fatal(err)
	}
	now = now.Add(11 * time.Minute)
	if w := apiJSON(t, b, "/v1/link/poll", `{"poll":"`+poll+`"}`, ""); w.Code != http.StatusOK {
		t.Fatalf("late bind: %d", w.Code)
	}
}

func TestLinkWrongCodes(t *testing.T) {
	appTestBot(t)
	now := time.Now()
	code, _, _ := startLink(now)
	for k := 0; k < 5; k++ {
		if err := bindLink("u1", "Xy", "AAAA-AAAA", now); err != errLinkWrong {
			t.Fatalf("wrong code %d: %v", k, err)
		}
	}
	if err := bindLink("u1", "Xy", code, now); err != errLinkTooMany {
		t.Fatalf("after 5 wrong codes: %v", err)
	}
	if err := bindLink("u1", "Xy", code, now.Add(11*time.Minute)); err != errLinkWrong {
		t.Fatalf("after the wait the (now expired) code is just wrong: %v", err)
	}
	// Someone else's bound code can't be taken over.
	code, _, _ = startLink(now)
	if err := bindLink("u2", "A", code, now); err != nil {
		t.Fatal(err)
	}
	if err := bindLink("u3", "B", code, now); err != errLinkWrong {
		t.Fatalf("taking a bound code: %v", err)
	}
	if err := bindLink("u2", "A", code, now); err != nil {
		t.Fatalf("same member again: %v", err)
	}
}

func TestLinkPollBadInput(t *testing.T) {
	b := appTestBot(t)
	for name, body := range map[string]string{
		"not json":   `poll=x`,
		"short":      `{"poll":"abc"}`,
		"too long":   `{"poll":"` + strings.Repeat("a", 200) + `"}`,
		"huge body":  `{"poll":"` + strings.Repeat("a", 5000) + `"}`,
		"wrong type": `{"poll":5}`,
	} {
		if w := apiJSON(t, b, "/v1/link/poll", body, ""); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	if w := apiJSON(t, b, "/v1/link/poll", `{"poll":"`+newToken(32)+`"}`, ""); w.Code != http.StatusGone {
		t.Errorf("unknown poll: %d", w.Code)
	}
	if w := apiDo(t, b, http.MethodGet, "/v1/link/poll", nil, nil); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: %d", w.Code)
	}
}

func TestLinkPollRateLimit(t *testing.T) {
	b := appTestBot(t)
	linkPollLimit = newRateLimit(3, time.Minute)
	body := `{"poll":"` + newToken(32) + `"}`
	for k := 0; k < 3; k++ {
		apiJSON(t, b, "/v1/link/poll", body, "")
	}
	if w := apiJSON(t, b, "/v1/link/poll", body, ""); w.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d", w.Code)
	}
}

func TestPendingLinksCapped(t *testing.T) {
	appTestBot(t)
	now := time.Now()
	for k := 0; k < maxPendingLinks; k++ {
		if _, _, err := startLink(now); err != nil {
			t.Fatalf("start %d: %v", k, err)
		}
	}
	if _, _, err := startLink(now); err != errLinkBusy {
		t.Fatalf("over the cap: %v", err)
	}
	if _, _, err := startLink(now.Add(linkCodeTTL + time.Second)); err != nil {
		t.Fatalf("expired ones are dropped: %v", err)
	}
}

func TestNormalizeLinkCode(t *testing.T) {
	for in, want := range map[string]string{
		"ABCD-2345": "ABCD2345", "abcd2345": "ABCD2345", " ab cd-23 45 ": "ABCD2345",
		"ABCD-0345": "", "ABCD-1345": "", "ABCO-2345": "", "ABCI-2345": "", "ABC-2345": "", "ABCDE-2345": "", "": "",
	} {
		got, ok := normalizeLinkCode(in)
		if got != want || ok != (want != "") {
			t.Errorf("%q: got %q %v", in, got, ok)
		}
	}
	for k := 0; k < 200; k++ {
		if c := newLinkCode(); strings.ContainsAny(c, "01OI") || len(c) != 8 {
			t.Fatalf("code %q", c)
		}
	}
}

func TestAddAppLinkCap(t *testing.T) {
	d := &storeData{AppLinks: map[string]appLink{"other": {User: "u2"}}}
	base := time.Now()
	for k := 0; k < maxAppsPerMember; k++ {
		addAppLink(d, string(rune('a'+k)), appLink{User: "u1", Created: base.Add(time.Duration(k) * time.Hour)})
	}
	// "a" was used most recently, so "b" is the longest unused.
	l := d.AppLinks["a"]
	l.LastSeen = base.Add(100 * time.Hour)
	d.AppLinks["a"] = l
	addAppLink(d, "new", appLink{User: "u1", Created: base.Add(200 * time.Hour)})
	if _, ok := d.AppLinks["b"]; ok {
		t.Error("the longest unused app should go")
	}
	n := 0
	for _, l := range d.AppLinks {
		if l.User == "u1" {
			n++
		}
	}
	if n != maxAppsPerMember || d.AppLinks["other"].User != "u2" || d.AppLinks["new"].User != "u1" {
		t.Errorf("%d apps: %v", n, d.AppLinks)
	}
}

func TestHeartbeat(t *testing.T) {
	b := appTestBot(t)
	now := time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC)
	appNow = func() time.Time { return now }
	tok := testToken(b, "u1")
	if w := apiJSON(t, b, "/v1/heartbeat", `{}`, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", w.Code)
	}
	if w := apiJSON(t, b, "/v1/heartbeat", `{}`, strings.Repeat("f", 64)); w.Code != http.StatusUnauthorized {
		t.Fatalf("unknown token: %d", w.Code)
	}
	if w := apiJSON(t, b, "/v1/heartbeat", `{"watching":`, tok); w.Code != http.StatusBadRequest {
		t.Fatalf("bad json: %d", w.Code)
	}
	body := `{"version":"26.7.0","dev_build":false,"os":"linux","share_watching":true,
		"watching":{"anilist_id":154587,"title":"Frieren @everyone","episode":12,"episodes":28},"watched_seconds":500}`
	w := apiJSON(t, b, "/v1/heartbeat", body, tok)
	if w.Code != http.StatusOK || decode(t, w)["ok"] != true {
		t.Fatalf("heartbeat: %d %s", w.Code, w.Body)
	}
	b.store.view(func(d *storeData) {
		l := d.AppLinks[hashToken(tok)]
		if l.Version != "26.7.0" || l.OS != "linux" || !l.LastSeen.Equal(now) {
			t.Errorf("link: %+v", l)
		}
		if st := d.AppStats["u1"]; st.Seconds != maxHeartbeatSecs || st.Weeks["2026-W42"] != maxHeartbeatSecs {
			t.Errorf("watch time capped at 120: %+v", st)
		}
	})
	list := watchingList(now)
	if len(list) != 1 || list[0].Show.AniListID != 154587 || list[0].Show.Episode != 12 {
		t.Fatalf("watching: %+v", list)
	}
	// Still there after two minutes, gone after three without a heartbeat.
	if len(watchingList(now.Add(2*time.Minute))) != 1 || len(watchingList(now.Add(4*time.Minute))) != 0 {
		t.Error("watching entry should expire after 3 minutes")
	}
	// Not sharing clears it; rate limit is 4 a minute, the bad one counted.
	if w := apiJSON(t, b, "/v1/heartbeat", body, tok); w.Code != http.StatusOK {
		t.Fatalf("3rd heartbeat: %d", w.Code)
	}
	apiJSON(t, b, "/v1/heartbeat", strings.Replace(body, `"share_watching":true`, `"share_watching":false`, 1), tok)
	if len(watchingList(now)) != 0 {
		t.Error("share_watching false should clear the entry")
	}
	if w := apiJSON(t, b, "/v1/heartbeat", body, tok); w.Code != http.StatusTooManyRequests {
		t.Fatalf("5th heartbeat in a minute: %d", w.Code)
	}
	now = now.Add(time.Minute)
	if w := apiJSON(t, b, "/v1/heartbeat", `{"watched_seconds":-50,"version":"<b>","os":"plan9"}`, tok); w.Code != http.StatusOK {
		t.Fatalf("next minute: %d", w.Code)
	}
	b.store.view(func(d *storeData) {
		if l := d.AppLinks[hashToken(tok)]; l.Version != "26.7.0" || l.OS != "other" {
			t.Errorf("bad version kept, odd system named other: %+v", l)
		}
		if d.AppStats["u1"].Seconds != 3*maxHeartbeatSecs {
			t.Errorf("negative seconds added: %d", d.AppStats["u1"].Seconds)
		}
	})
	if !appActivityDirty.Load() {
		t.Error("heartbeats should mark the store for saving")
	}
}

func TestHeartbeatBodyLimit(t *testing.T) {
	b := appTestBot(t)
	tok := testToken(b, "u1")
	body := `{"watching":{"title":"` + strings.Repeat("x", 5000) + `"}}`
	if w := apiJSON(t, b, "/v1/heartbeat", body, tok); w.Code != http.StatusBadRequest {
		t.Fatalf("got %d", w.Code)
	}
}

func TestCleanAppValues(t *testing.T) {
	for in, want := range map[string]string{"26.7.0": "26.7.0", "v26.7.0": "26.7.0", "26.7.0-dev+abc": "26.7.0-dev+abc",
		"26.7": "", "<@1>": "", "26.7.0 @everyone": "", strings.Repeat("1", 50): ""} {
		if got := cleanVersion(in); got != want {
			t.Errorf("cleanVersion(%q) = %q", in, got)
		}
	}
	for in, want := range map[string]string{"linux": "linux", " Windows ": "windows", "darwin": "darwin", "": "", "TempleOS": "other"} {
		if got := cleanOS(in); got != want {
			t.Errorf("cleanOS(%q) = %q", in, got)
		}
	}
	s := cleanShow(&appShow{AniListID: -3, Title: "Fri‮eren\x00\n  S2 " + strings.Repeat("x", 200), Episode: 1e6, Episodes: 28})
	if s == nil || s.AniListID != 0 || s.Episode != 0 || s.Episodes != 28 || !strings.HasPrefix(s.Title, "Frieren S2 x") || len([]rune(s.Title)) != 100 {
		t.Errorf("cleanShow: %+v", s)
	}
	if cleanShow(&appShow{Title: " \x01 "}) != nil || cleanShow(nil) != nil {
		t.Error("nothing usable should be nil")
	}
}

func TestWatchTime(t *testing.T) {
	if got := isoWeekKey(time.Date(2025, 12, 29, 0, 0, 0, 0, time.UTC)); got != "2026-W01" {
		t.Errorf("week across the year: %s", got)
	}
	var st appStats
	start := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	for k := 0; k < 15; k++ {
		addWatchTime(&st, 60, start.AddDate(0, 0, 7*k))
	}
	addWatchTime(&st, 0, start)
	if st.Seconds != 15*60 || len(st.Weeks) != keepWatchWeeks || st.Weeks["2026-W02"] != 0 || st.Weeks["2026-W16"] != 60 {
		t.Errorf("weeks: %+v", st)
	}
	for secs, want := range map[int64]string{0: "0 min", 59: "0 min", 600: "10 min", 3600: "1 h", 3*3600 + 20*60: "3 h 20 min"} {
		if got := watchDuration(secs); got != want {
			t.Errorf("watchDuration(%d) = %q", secs, got)
		}
	}
	if watchHours(42*3600+1800) != "42 h" || watchHours(1800) != "30 min" {
		t.Error("watchHours")
	}
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	e := watchTimeView("u1", false, appStats{Seconds: 7200, Weeks: map[string]int64{isoWeekKey(now): 3600, isoWeekKey(now.AddDate(0, 0, -7)): 1800}}, now)
	if !strings.Contains(e.Description, "**2 h**") || !strings.Contains(e.Description, "This week: 1 h") || !strings.Contains(e.Description, "Last week: 30 min") {
		t.Errorf("view: %s", e.Description)
	}
	if e := watchTimeView("u1", true, appStats{}, now); !strings.Contains(e.Description, "otakase -discord-link") {
		t.Errorf("empty self: %s", e.Description)
	}
	b := appTestBot(t)
	b.store.update(func(d *storeData) { d.AppStats["u1"] = appStats{Seconds: 42 * 3600} })
	if got := b.otakaseWatched("u1"); got != "42 h" {
		t.Errorf("profile line: %q", got)
	}
	if got := b.otakaseWatched("u2"); got != "" {
		t.Errorf("no watch time: %q", got)
	}
}

func TestProfileCardOtakaseLine(t *testing.T) {
	if _, err := renderProfileCard(profileCard{Name: "Xy", Completed: 12, Since: 2019, Otakase: "42 h"}); err != nil {
		t.Fatal(err)
	}
}

func TestWhosWatchingView(t *testing.T) {
	now := time.Now()
	list := []nowWatching{
		{User: "1", Show: appShow{AniListID: 10, Title: "app title", Episode: 3, Episodes: 12}, Since: now.Add(-time.Hour)},
		{User: "2", Show: appShow{AniListID: 10, Episode: 4}, Since: now.Add(-time.Minute)},
		{User: "3", Show: appShow{AniListID: 66, Title: "Adult"}, Since: now},
		{User: "4", Show: appShow{Title: "**Local** <@&99> @everyone"}, Since: now},
		{User: "5", Show: appShow{AniListID: 7, Title: "Other"}, Since: now},
	}
	lookups := 0
	lookup := func(id int) (airingShowInfo, bool) {
		lookups++
		var info airingShowInfo
		switch id {
		case 10:
			info.Title.English, info.SiteURL, info.CoverImage.ExtraLarge = "Frieren", "https://anilist.co/anime/10", "https://img/10.jpg"
		case 66:
			info.IsAdult, info.CoverImage.ExtraLarge = true, "https://img/66.jpg"
		case 7:
			info.Title.Romaji, info.CoverImage.ExtraLarge = "Sousou", "https://img/7.jpg"
		}
		return info, true
	}
	e := whosWatchingView(list, lookup)
	d := e.Description
	if lookups != 3 {
		t.Errorf("each show is looked up once, got %d", lookups)
	}
	if strings.Contains(d, "Adult") || strings.Contains(d, "<@3>") {
		t.Error("adult titles must be left out")
	}
	if !strings.Contains(d, "[**Frieren**](https://anilist.co/anime/10) · ep 3/12") || !strings.Contains(d, "ep 4 ·") {
		t.Errorf("AniList titles: %s", d)
	}
	if strings.Contains(d, "<@&99>") || strings.Contains(d, "@everyone") || !strings.Contains(d, `\*\*Local\*\*`) {
		t.Errorf("app titles must be escaped and quiet: %s", d)
	}
	if e.Thumbnail == nil || e.Thumbnail.URL != "https://img/10.jpg" {
		t.Errorf("cover of the most watched: %+v", e.Thumbnail)
	}
	if e := whosWatchingView(nil, lookup); e.Thumbnail != nil || !strings.Contains(e.Description, "Nobody") {
		t.Errorf("empty: %+v", e)
	}
}

func TestWatchingCapped(t *testing.T) {
	appTestBot(t)
	now := time.Now()
	for k := 0; k < maxWatching+5; k++ {
		setWatching(string(rune(0x4e00+k)), true, &appShow{AniListID: 1}, now)
	}
	if n := len(watchingList(now)); n != maxWatching {
		t.Errorf("got %d entries", n)
	}
}

func TestLinkedAppsView(t *testing.T) {
	e, comps := linkedAppsView(nil)
	if len(comps) != 0 || !strings.Contains(e.Description, "No Otakase app") {
		t.Errorf("empty: %v %v", e, comps)
	}
	var apps []memberApp
	for k := 0; k < 7; k++ {
		apps = append(apps, memberApp{ID: strings.Repeat(string(rune('a'+k)), 12), appLink: appLink{OS: "darwin", Version: "26.7.0", Created: time.Unix(1700000000, 0)}})
	}
	apps[0].LastSeen = time.Unix(1700001000, 0)
	e, comps = linkedAppsView(apps)
	if len(comps) != 2 || len(comps[0].(discordgo.ActionsRow).Components) != 5 || len(comps[1].(discordgo.ActionsRow).Components) != 2 {
		t.Fatalf("buttons: %v", comps)
	}
	if id := comps[0].(discordgo.ActionsRow).Components[0].(discordgo.Button).CustomID; id != "app-revoke:aaaaaaaaaaaa" {
		t.Errorf("custom id %q", id)
	}
	if !strings.Contains(e.Description, "macOS · Otakase 26.7.0") || !strings.Contains(e.Description, "last seen <t:1700001000:R>") || !strings.Contains(e.Description, "not used yet") {
		t.Errorf("list: %s", e.Description)
	}
}

func TestUnlinkAll(t *testing.T) {
	b := appTestBot(t)
	tok1, tok2, other := testToken(b, "u1"), testToken(b, "u1"), testToken(b, "u2")
	setWatching("u1", true, &appShow{AniListID: 1}, appNow())
	if n := b.unlinkAll("u1"); n != 2 {
		t.Fatalf("unlinked %d", n)
	}
	for _, tok := range []string{tok1, tok2} {
		if w := apiJSON(t, b, "/v1/heartbeat", `{}`, tok); w.Code != http.StatusUnauthorized {
			t.Errorf("revoked: %d", w.Code)
		}
	}
	if w := apiJSON(t, b, "/v1/heartbeat", `{}`, other); w.Code != http.StatusOK {
		t.Errorf("other member's app: %d", w.Code)
	}
	if len(watchingList(appNow())) != 0 {
		t.Error("unlinking should clear what they watch")
	}
}

func TestQuietAndCleanText(t *testing.T) {
	if got := quiet("hi @everyone and @here <@123> <@&5>"); strings.Contains(got, "@everyone") || strings.Contains(got, "@here") || strings.Contains(got, "<@") {
		t.Errorf("quiet: %q", got)
	}
	if got := cleanText("a\r\nb\x07‮\tc  ", true); got != "a\nb\tc" {
		t.Errorf("multiline: %q", got)
	}
	if got := cleanText(" a\n\n b\tc ", false); got != "a b c" {
		t.Errorf("one line: %q", got)
	}
	if got := cleanText("bad \xff utf8", false); got != "bad utf8" {
		t.Errorf("invalid utf8: %q", got)
	}
}

func TestNewsText(t *testing.T) {
	in := "# Otakase 26.7.0 is out\n**Big** update with __Discord__ linking, see [the notes](https://x.y/z) and <https://a.b>.\n" +
		"> quoted <@123> <@&4> <#5> <:shu:777> on <t:1760000000:D>\n```go\ncode\n```\n\n\n\n-# small ||spoiler|| ~~old~~ `tick`"
	got := newsText(in)
	for _, want := range []string{"Otakase 26.7.0 is out", "Big update with Discord linking, see the notes and https://a.b.",
		"quoted @member @role #channel :shu: on 9 Oct 2025", "small spoiler old tick"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, not := range []string{"**", "#", "```", "<", "\n\n\n"} {
		if strings.Contains(strings.ReplaceAll(got, "#channel", ""), not) {
			t.Errorf("left %q in:\n%s", not, got)
		}
	}
}

func TestAnnouncementFrom(t *testing.T) {
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	m := &discordgo.Message{ID: "3", ChannelID: "2", Timestamp: at, Content: "## Otakase 26.7.0\n" + strings.Repeat("word ", 200)}
	a := announcementFrom(m, "1")
	if a.Title != "Otakase 26.7.0" || len([]rune(a.Text)) != 500 || a.URL != "https://discord.com/channels/1/2/3" || a.At != "2026-10-01T09:00:00Z" {
		t.Errorf("%+v", a)
	}
	m = &discordgo.Message{Embeds: []*discordgo.MessageEmbed{{Title: "**Maintenance**", Description: "Back soon"}}}
	if a := announcementFrom(m, "1"); a.Title != "Maintenance" || a.Text != "Back soon" {
		t.Errorf("embed: %+v", a)
	}
	m = &discordgo.Message{Content: "One line only"}
	if a := announcementFrom(m, "1"); a.Title != "One line only" || a.Text != "One line only" {
		t.Errorf("one line: %+v", a)
	}
	if announcementFrom(&discordgo.Message{Content: "  "}, "1") != nil {
		t.Error("empty message")
	}
}

func TestNewsEndpoint(t *testing.T) {
	b := appTestBot(t)
	now := time.Now()
	appNow = func() time.Time { return now }
	calls := 0
	fail := false
	fetchNews = func(*bot) (*newsReply, error) {
		calls++
		if fail {
			return nil, errors.New("discord down")
		}
		return &newsReply{Announcement: &newsAnnouncement{Title: "Hi"}, Online: 12, Members: 85, Invite: appInviteURL}, nil
	}
	fail = true
	if w := apiDo(t, b, http.MethodGet, "/v1/news", nil, nil); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("no news yet: %d", w.Code)
	}
	fail = false
	if w := apiDo(t, b, http.MethodGet, "/v1/news", nil, nil); w.Code != http.StatusServiceUnavailable || calls != 1 {
		t.Fatalf("a failure is not retried for a minute: %d, %d fetches", w.Code, calls)
	}
	now = now.Add(time.Minute)
	w := apiDo(t, b, http.MethodGet, "/v1/news", nil, nil)
	got := decode(t, w)
	if w.Code != http.StatusOK || got["online"].(float64) != 12 || got["members"].(float64) != 85 || got["invite"] != appInviteURL ||
		got["announcement"].(map[string]any)["title"] != "Hi" {
		t.Fatalf("news: %d %v", w.Code, got)
	}
	apiDo(t, b, http.MethodGet, "/v1/news", nil, nil)
	if calls != 2 {
		t.Errorf("cached for 5 minutes: %d fetches", calls)
	}
	now = now.Add(6 * time.Minute)
	fail = true
	if w := apiDo(t, b, http.MethodGet, "/v1/news", nil, nil); w.Code != http.StatusOK {
		t.Errorf("stale news when Discord fails: %d", w.Code)
	}
	if w := apiDo(t, b, http.MethodPost, "/v1/news", nil, nil); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", w.Code)
	}
	newsLimit = newRateLimit(1, time.Minute)
	apiDo(t, b, http.MethodGet, "/v1/news", nil, nil)
	if w := apiDo(t, b, http.MethodGet, "/v1/news", nil, nil); w.Code != http.StatusTooManyRequests {
		t.Errorf("rate limit: %d", w.Code)
	}
}

// reportForm builds a multipart report.
func reportForm(t *testing.T, fields map[string]string, log []byte) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	if log != nil {
		fw, _ := mw.CreateFormFile("log", "otakase.log")
		fw.Write(log)
	}
	mw.Close()
	return &buf, mw.FormDataContentType()
}

func TestReportEndpoint(t *testing.T) {
	b := appTestBot(t)
	tok := testToken(b, "u1")
	send := func(fields map[string]string, log []byte, token string) *httptest.ResponseRecorder {
		body, ct := reportForm(t, fields, log)
		hdr := map[string]string{"Content-Type": ct}
		if token != "" {
			hdr["Authorization"] = "Bearer " + token
		}
		return apiDo(t, b, http.MethodPost, "/v1/report", body, hdr)
	}
	ok := map[string]string{"title": "No sound", "description": "mpv plays without sound", "diag": "Otakase version: 26.7.0"}
	if w := send(ok, nil, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", w.Code)
	}
	if w := apiJSON(t, b, "/v1/report", `{"title":"x"}`, tok); w.Code != http.StatusBadRequest {
		t.Fatalf("not multipart: %d", w.Code)
	}
	cases := map[string]struct {
		fields map[string]string
		log    []byte
		code   int
	}{
		"no title":         {map[string]string{"description": "x"}, nil, http.StatusBadRequest},
		"blank title":      {map[string]string{"title": " \x00 "}, nil, http.StatusBadRequest},
		"long title":       {map[string]string{"title": strings.Repeat("é", 101)}, nil, http.StatusBadRequest},
		"long description": {map[string]string{"title": "x", "description": strings.Repeat("a", 4001)}, nil, http.StatusBadRequest},
		"long diag":        {map[string]string{"title": "x", "diag": strings.Repeat("a", 8001)}, nil, http.StatusBadRequest},
		"binary log":       {ok, []byte("PNG\x00\x01"), http.StatusBadRequest},
		"invalid utf8 log": {ok, []byte("bad \xff"), http.StatusBadRequest},
		"log over 1 MB":    {ok, bytes.Repeat([]byte("a"), reportLogMax+10), http.StatusBadRequest},
		"body far too big": {ok, bytes.Repeat([]byte("a"), 2*reportLogMax), http.StatusRequestEntityTooLarge},
	}
	for name, c := range cases {
		if w := send(c.fields, c.log, tok); w.Code != c.code {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	// Valid reports count against 3 an hour; without a forum the bot says so.
	for k := 0; k < 3; k++ {
		if w := send(ok, []byte("log line\n"), tok); w.Code != http.StatusServiceUnavailable {
			t.Fatalf("report %d: %d %s", k, w.Code, w.Body)
		}
	}
	if w := send(ok, nil, tok); w.Code != http.StatusTooManyRequests {
		t.Fatalf("4th report: %d", w.Code)
	}
}

func TestReportMessage(t *testing.T) {
	rep := appReportData{Title: "No sound", Description: "it broke @everyone <@&1>", Diag: "OS: linux\n```oops```", Log: []byte("log")}
	m := reportMessage("42", rep, appLink{Version: "26.7.0", OS: "linux"})
	if !strings.HasPrefix(m.Content, "<@42> sent this from the Otakase app. Otakase version: 26.7.0 on Linux.") {
		t.Errorf("content: %q", m.Content)
	}
	if m.AllowedMentions == nil || len(m.AllowedMentions.Parse) != 0 || len(m.AllowedMentions.Users) != 1 || m.AllowedMentions.Users[0] != "42" {
		t.Errorf("only the member may be pinged: %+v", m.AllowedMentions)
	}
	if len(m.Embeds) != 2 || strings.Contains(m.Embeds[0].Description, "@everyone") || strings.Contains(m.Embeds[0].Description, "<@&") {
		t.Fatalf("embeds: %+v", m.Embeds)
	}
	if d := m.Embeds[1].Description; !strings.HasPrefix(d, "```\n") || strings.Count(d, "```") != 2 {
		t.Errorf("diag block must not close early: %q", d)
	}
	if len(m.Files) != 1 || m.Files[0].Name != "otakase.log" {
		t.Errorf("files: %+v", m.Files)
	}
	if v, ok := mentionedVersion(m.Content); !ok || v != "26.7.0" {
		t.Error("the version line should be readable by the version check")
	}
	rep.Description, rep.Diag = strings.Repeat("d", 4000), strings.Repeat("x", 3000)
	m = reportMessage("42", rep, appLink{})
	if len(m.Embeds) != 1 || len(m.Files) != 2 || m.Files[0].Name != "diagnostics.txt" {
		t.Errorf("long diag goes in a file: %d embeds, %+v", len(m.Embeds), m.Files)
	}
	if strings.Contains(m.Content, "version") {
		t.Errorf("unknown version: %q", m.Content)
	}
}

func TestReportTags(t *testing.T) {
	forum := &discordgo.Channel{AvailableTags: []discordgo.ForumTag{{ID: "1", Name: "Solved"}, {ID: "2", Name: "Windows"}, {ID: "3", Name: "macOS"}, {ID: "4", Name: "Linux / Arch"}}}
	for goos, want := range map[string]string{"windows": "2", "darwin": "3", "linux": "4", "freebsd": "", "": ""} {
		got := reportTags(forum, goos)
		if (want == "" && got != nil) || (want != "" && (len(got) != 1 || got[0] != want)) {
			t.Errorf("%s: %v", goos, got)
		}
	}
	forum.Flags = discordgo.ChannelFlagRequireTag
	if got := reportTags(forum, ""); len(got) != 1 || got[0] != "2" {
		t.Errorf("required tag: %v", got)
	}
}

func TestAppReports(t *testing.T) {
	m := map[string]appReport{}
	base := time.Now()
	for k := 0; k < 5; k++ {
		m[string(rune('a'+k))] = appReport{User: "u", At: base.Add(time.Duration(k) * time.Minute)}
	}
	pruneAppReports(m, 3)
	if len(m) != 3 || m["a"].User != "" || m["e"].User != "u" {
		t.Errorf("keeps the newest: %v", m)
	}
	b := appTestBot(t)
	b.store.update(func(d *storeData) { d.AppReports["th"] = appReport{User: "u1"} })
	if !b.appReporter("th", "u1") || b.appReporter("th", "u2") || b.appReporter("other", "u1") || b.appReporter("other", "") {
		t.Error("appReporter")
	}
}

func TestDashApps(t *testing.T) {
	b := appTestBot(t)
	now := appNow()
	b.store.update(func(d *storeData) {
		d.AppLinks["h1"] = appLink{User: "u1", Version: "26.7.0", OS: "linux", LastSeen: now}
		d.AppLinks["h2"] = appLink{User: "u1", Version: "26.7.0", OS: "windows"}
		d.AppLinks["h3"] = appLink{User: "u2"}
		d.AppStats["u1"] = appStats{Seconds: 7200, Weeks: map[string]int64{isoWeekKey(now): 3600}}
	})
	setWatching("u2", true, &appShow{Title: "<script>Frieren</script>", Episode: 2}, now)
	c := b.store.appCountsAt(now)
	if c.Apps != 3 || c.Members != 2 || c.Active != 1 || c.Versions[0] != (showCount{"26.7.0", 2}) || len(c.TopWeek) != 1 {
		t.Errorf("counts: %+v", c)
	}
	html := string(b.dashApps())
	if strings.Contains(html, "<script>") || !strings.Contains(html, "Linux") || !strings.Contains(html, "1 h") {
		t.Errorf("section: %s", html)
	}
}

func TestUnknownMember(t *testing.T) {
	gone := &discordgo.RESTError{Message: &discordgo.APIErrorMessage{Code: discordgo.ErrCodeUnknownMember}}
	other := &discordgo.RESTError{Message: &discordgo.APIErrorMessage{Code: discordgo.ErrCodeUnknownGuild}}
	if !unknownMember(gone) || unknownMember(other) || unknownMember(errors.New("timeout")) {
		t.Error("only Unknown Member means they left")
	}
}

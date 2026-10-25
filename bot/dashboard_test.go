package main

import (
	"bytes"
	"fmt"
	"html/template"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func dashTestBot(t *testing.T) *bot {
	t.Helper()
	b := &bot{store: openStore(filepath.Join(t.TempDir(), "s.json")), health: newHealth()}
	now := time.Now()
	b.store.update(func(d *storeData) {
		d.Follows = map[string][]followedShow{
			"a": {{ID: 1, Title: "Frieren"}, {ID: 2, Title: "Dandadan"}},
			"b": {{ID: 1, Title: "Frieren"}},
			"c": {},
		}
		d.Suggestions["m1"] = suggestion{Title: "Dark <mode>", Up: []string{"a", "b"}, Status: "planned", Created: now}
		d.Suggestions["m2"] = suggestion{Title: "Lists", Up: []string{"a"}, Down: []string{"b", "c"}, Created: now}
		d.Warnings["u1"] = []warning{{At: now.Add(-time.Hour), Reason: "secret reason"}, {At: now.Add(-60 * 24 * time.Hour), Reason: "old"}}
		d.AniList["a"] = anilistLink{ID: 1, Verified: true}
		d.AniList["b"] = anilistLink{ID: 2}
	})
	return b
}

func dashGet(t *testing.T, h http.Handler, target, remote string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.RemoteAddr = remote
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestDashboardIngressOnly(t *testing.T) {
	h := dashTestBot(t).dashboardHandler(true)
	if w := dashGet(t, h, "/", "10.0.0.5:1234", nil); w.Code != http.StatusForbidden {
		t.Fatalf("outside ingress: got %d", w.Code)
	}
	if w := dashGet(t, h, "/", ingressProxyIP+":1234", nil); w.Code != http.StatusOK {
		t.Fatalf("from ingress: got %d", w.Code)
	}
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = ingressProxyIP + ":1"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("POST without token: got %d", w.Code)
	}
	if w := dashGet(t, h, "/other", ingressProxyIP+":1", nil); w.Code != http.StatusNotFound {
		t.Fatalf("other path: got %d", w.Code)
	}
}

func TestDashboardSections(t *testing.T) {
	h := dashTestBot(t).dashboardHandler(false)
	fetchDashEvents = func(*bot) ([]*discordgo.GuildScheduledEvent, error) {
		return []*discordgo.GuildScheduledEvent{
			{Name: "Frieren night", Status: discordgo.GuildScheduledEventStatusScheduled, ScheduledStartTime: time.Now().Add(3*time.Hour + time.Minute), UserCount: 4},
			{Name: "Done party", Status: discordgo.GuildScheduledEventStatusCompleted},
		}, nil
	}
	dashEvents.at = time.Time{}
	cases := map[string][]string{
		"":                  {"Version", "Uptime", "All background jobs are working", `class="on"`},
		"?s=follows":        {"Frieren</td><td>2", "Members using /follow"},
		"?s=suggestions":    {"Dark &lt;mode&gt;", "Planned", "2 / −0"},
		"?s=watch-parties":  {"Frieren night", "in 3 hours"},
		"?s=moderation":     {"<td>1 · 2</td>", "Warnings, last 30 days"},
		"?s=anilist":        {"Linked members", ">2<", "Signed in"},
		"?s=settings":       {"daily_airing", `name="do" value="setting"`, "match_threshold"},
		"?s=safety":         {"Verification", "Pause invites 30 min", "Spoiler guard"},
		"?s=airing":         {"Daily board", "not posted"},
		"?s=support":        {"Open posts", "No helper points yet."},
		"?s=games":          {"Chat levels", "Guess the anime, this week", "Points by game"},
		"?s=riddles":        {"/riddle-add"},
		"?s=does-not-exist": {"Gateway latency"},
	}
	for q, wants := range cases {
		w := dashGet(t, h, "/"+q, "127.0.0.1:1", nil)
		body := w.Body.String()
		if w.Code != http.StatusOK {
			t.Fatalf("%s: got %d", q, w.Code)
		}
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %q", q, want)
			}
		}
		if strings.Contains(body, "secret reason") || strings.Contains(body, "<mode>") {
			t.Errorf("%s: leaked content", q)
		}
		if strings.Contains(body, "Done party") {
			t.Errorf("%s: shows finished event", q)
		}
	}
}

func TestDashboardIngressPath(t *testing.T) {
	h := dashTestBot(t).dashboardHandler(false)
	w := dashGet(t, h, "/", "127.0.0.1:1", map[string]string{"X-Ingress-Path": "/api/hassio_ingress/abc123"})
	if !strings.Contains(w.Body.String(), `<base href="/api/hassio_ingress/abc123/">`) {
		t.Error("no base href for ingress path")
	}
	if !strings.Contains(w.Body.String(), `href="?s=follows"`) {
		t.Error("links are not relative")
	}
	w = dashGet(t, h, "/", "127.0.0.1:1", map[string]string{"X-Ingress-Path": `//evil.example/"x`})
	if strings.Contains(w.Body.String(), "<base") {
		t.Error("bad ingress path used")
	}
}

func TestDashboardFailingJobs(t *testing.T) {
	b := dashTestBot(t)
	b.health.fail("Follow DMs")
	body := string(b.dashOverview())
	if !strings.Contains(body, "Follow DMs") || !strings.Contains(body, "failing") {
		t.Errorf("failing job not shown: %s", body)
	}
}

func TestDashboardAddr(t *testing.T) {
	opts := filepath.Join(t.TempDir(), "options.json")
	if _, _, ok := dashboardAddr("", opts); ok {
		t.Error("on without add-on or env")
	}
	os.WriteFile(opts, []byte("{}"), 0o600)
	if a, ingress, ok := dashboardAddr("", opts); !ok || !ingress || a != ":8099" {
		t.Errorf("add-on: %q %v %v", a, ingress, ok)
	}
	if a, ingress, ok := dashboardAddr(":9000", opts); !ok || ingress || a != "127.0.0.1:9000" {
		t.Errorf("env: %q %v %v", a, ingress, ok)
	}
	if _, _, ok := dashboardAddr("nonsense", opts); ok {
		t.Error("bad address accepted")
	}
}

func TestAddDashSection(t *testing.T) {
	before := len(dashSections)
	defer func() { dashSections = dashSections[:before] }()
	addDashSection("Quiz Scores!", func(*bot) template.HTML { return dashNote("hello quiz") })
	if s := dashSections[before]; s.Slug != "quiz-scores" {
		t.Fatalf("slug %q", s.Slug)
	}
	w := dashGet(t, dashTestBot(t).dashboardHandler(false), "/?s=quiz-scores", "127.0.0.1:1", nil)
	if !strings.Contains(w.Body.String(), "hello quiz") {
		t.Error("added section not shown")
	}
	addDashSection("Broken", func(*bot) template.HTML { panic("boom") })
	w = dashGet(t, dashTestBot(t).dashboardHandler(false), "/?s=broken", "127.0.0.1:1", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "could not be drawn") {
		t.Error("broken section not handled")
	}
}

func dashPostForm(t *testing.T, h http.Handler, form url.Values, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	r.RemoteAddr = "127.0.0.1:1"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestDashboardControls(t *testing.T) {
	b := dashTestBot(t)
	h := b.dashboardHandler(false)

	bad := url.Values{"t": {"nope"}, "s": {"settings"}, "do": {"setting"}, "name": {"welcome_dm"}, "v": {"false"}}
	if w := dashPostForm(t, h, bad, nil); w.Code != http.StatusForbidden {
		t.Fatalf("bad token: got %d", w.Code)
	}
	good := url.Values{"t": {dashToken}, "s": {"settings"}, "do": {"setting"}, "name": {"airing_hour_utc"}, "v": {"9"}}
	if w := dashPostForm(t, h, good, map[string]string{"Sec-Fetch-Site": "cross-site"}); w.Code != http.StatusForbidden {
		t.Fatalf("cross-site: got %d", w.Code)
	}
	if b.settingInt("airing_hour_utc") == 9 {
		t.Fatal("refused form still changed the setting")
	}

	w := dashPostForm(t, h, good, map[string]string{"X-Remote-User-Display-Name": "Xy <@1>"})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "./?s=settings" {
		t.Fatalf("save: got %d to %q", w.Code, w.Header().Get("Location"))
	}
	if b.settingInt("airing_hour_utc") != 9 {
		t.Error("setting not saved")
	}
	page := dashGet(t, h, "/?s=settings", "127.0.0.1:1", nil).Body.String()
	if !strings.Contains(page, "airing_hour_utc is now 9") || !strings.Contains(page, "Reset") {
		t.Error("no result or reset button after saving")
	}
	if strings.Contains(page, `http-equiv="refresh"`) {
		t.Error("page with controls refreshes by itself")
	}
	if strings.Contains(dashGet(t, h, "/?s=settings", "127.0.0.1:1", nil).Body.String(), "is now 9") {
		t.Error("result shown twice")
	}

	good.Set("v", "99")
	dashPostForm(t, h, good, nil)
	if b.settingInt("airing_hour_utc") != 9 {
		t.Error("out of range value saved")
	}
	if page := dashGet(t, h, "/?s=settings", "127.0.0.1:1", nil).Body.String(); !strings.Contains(page, `flash bad`) {
		t.Error("bad value not reported")
	}

	dashPostForm(t, h, url.Values{"t": {dashToken}, "s": {"settings"}, "do": {"setting-reset"}, "name": {"airing_hour_utc"}}, nil)
	if _, changed := b.settingValue("airing_hour_utc"); changed {
		t.Error("reset kept the change")
	}

	dashPostForm(t, h, url.Values{"t": {dashToken}, "s": {"safety"}, "do": {"spoiler"}, "v": {"hide"}}, nil)
	if b.spoilerMode() != spoilerHide {
		t.Error("spoiler guard not set")
	}
	dashPostForm(t, h, url.Values{"t": {dashToken}, "s": {"safety"}, "do": {"verify"}, "v": {"off"}}, nil)
	var mode string
	b.store.view(func(d *storeData) { mode = d.Verify.Mode })
	if mode != "off" {
		t.Errorf("verification mode %q", mode)
	}
	if page := dashGet(t, h, "/?s=safety", "127.0.0.1:1", nil).Body.String(); !strings.Contains(page, `<option value="off" selected>`) {
		t.Error("safety page doesn't show the new mode")
	}
}

func TestDashboardWho(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	if got := dashWho(r); got != "**Someone** (dashboard)" {
		t.Errorf("no header: %q", got)
	}
	r.Header.Set("X-Remote-User-Display-Name", "  <@123> **Xy**  ")
	if got := dashWho(r); got != "**123 Xy** (dashboard)" {
		t.Errorf("cleaned: %q", got)
	}
}

func TestLogRing(t *testing.T) {
	l := &logRing{}
	l.Write([]byte("one\ntw"))
	l.Write([]byte("o\n"))
	if got := l.last(5); len(got) != 2 || got[0] != "two" || got[1] != "one" {
		t.Errorf("lines %q", got)
	}
	for i := 0; i < logLinesKept+10; i++ {
		l.Write([]byte("x\n"))
	}
	if n := len(l.lines); n != logLinesKept {
		t.Errorf("kept %d lines", n)
	}
}

func TestDashboardMoreControls(t *testing.T) {
	b := dashTestBot(t)
	h := b.dashboardHandler(false)
	post := func(v url.Values) {
		v.Set("t", dashToken)
		if w := dashPostForm(t, h, v, nil); w.Code != http.StatusSeeOther {
			t.Fatalf("%v: got %d", v, w.Code)
		}
	}

	if page := dashGet(t, h, "/?s=moderation", "127.0.0.1:1", nil).Body.String(); !strings.Contains(page, `value="clear-warnings"`) || !strings.Contains(page, "required") {
		t.Error("no clear button with a tick box")
	}
	if page := dashGet(t, h, "/?s=moderation", "127.0.0.1:1", nil).Body.String(); !strings.Contains(page, `value="ticket-add"`) || !strings.Contains(page, `value="ticket-remove"`) {
		t.Error("watch ticket controls missing")
	}
	userID := "123456789012345678"
	post(url.Values{"s": {"moderation"}, "do": {"ticket-add"}, "text": {userID}, "v": {"5"}})
	post(url.Values{"s": {"moderation"}, "do": {"ticket-remove"}, "text": {userID}, "v": {"2"}})
	if got := b.watchTickets(userID); got != 3 {
		t.Errorf("watch ticket balance: %d", got)
	}
	post(url.Values{"s": {"moderation"}, "do": {"ticket-remove"}, "text": {userID}, "v": {"100"}})
	if got := b.watchTickets(userID); got != 0 {
		t.Errorf("ticket balance below zero: %d", got)
	}
	post(url.Values{"s": {"moderation"}, "do": {"clear-warnings"}, "name": {"u1"}})
	var left int
	b.store.view(func(d *storeData) { left = len(d.Warnings["u1"]) })
	if left != 0 {
		t.Error("warnings not cleared")
	}

	b.store.update(func(d *storeData) {
		d.Guess = map[string]guessScore{"a": {Total: 7, Week: 3, WeekOf: weekKey(time.Now())}}
		d.Rooms["r1"] = room{Owner: "a", Created: time.Now().Add(-5 * time.Minute)}
	})
	post(url.Values{"s": {"games"}, "do": {"guess-reset-week"}})
	var g guessScore
	b.store.view(func(d *storeData) { g = d.Guess["a"] })
	if g.Week != 0 || g.Total != 7 {
		t.Errorf("after reset: %+v", g)
	}

	post(url.Values{"s": {"suggestions"}, "do": {"suggestion"}, "name": {"m1"}, "v": {"bogus"}})
	if page := dashGet(t, h, "/?s=suggestions", "127.0.0.1:1", nil).Body.String(); !strings.Contains(page, "Unknown status") || !strings.Contains(page, `<option value="planned" selected>`) {
		t.Error("bad status not refused or current status not selected")
	}
	post(url.Values{"s": {"post"}, "do": {"say"}, "v": {"123"}, "text": {"hi"}})
	if page := dashGet(t, h, "/?s=post", "127.0.0.1:1", nil).Body.String(); !strings.Contains(page, "can&#39;t post in that channel") {
		t.Error("post to an unknown channel not refused")
	}
	if page := dashGet(t, h, "/?s=rooms", "127.0.0.1:1", nil).Body.String(); !strings.Contains(page, "5 minutes") {
		t.Error("room not listed")
	}
}

func TestDashboardTextsAndNotes(t *testing.T) {
	b := dashTestBot(t)
	h := b.dashboardHandler(false)
	post := func(v url.Values) {
		v.Set("t", dashToken)
		if w := dashPostForm(t, h, v, nil); w.Code != http.StatusSeeOther {
			t.Fatalf("%v: got %d", v, w.Code)
		}
	}
	if !strings.Contains(dashGet(t, h, "/?s=texts", "127.0.0.1:1", nil).Body.String(), "Welcome DM: greeting") {
		t.Fatal("texts page missing")
	}
	post(url.Values{"s": {"texts"}, "do": {"text-save"}, "name": {"faq:mpv"}, "text": {"Install mpv <now>."}})
	f, _ := faqByKey("mpv")
	if got := b.faqAnswer(f).Answer; got != "Install mpv <now>." {
		t.Errorf("faq answer %q", got)
	}
	if page := dashGet(t, h, "/?s=texts", "127.0.0.1:1", nil).Body.String(); !strings.Contains(page, "Install mpv &lt;now&gt;.</textarea>") {
		t.Error("saved text not in its box")
	}
	post(url.Values{"s": {"texts"}, "do": {"text-save"}, "name": {"welcome_steps"}, "text": {"- One\n\nTwo"}})
	m := b.welcomeDM()
	if s := fmt.Sprint(m.Components); !strings.Contains(s, "- One\n- Two") {
		t.Errorf("welcome steps not used: %s", s)
	}
	post(url.Values{"s": {"texts"}, "do": {"text-reset"}, "name": {"faq:mpv"}})
	if got := b.faqAnswer(f).Answer; got != f.Answer {
		t.Error("reset kept the change")
	}

	post(url.Values{"s": {"support"}, "do": {"known-add"}, "text": {"  Source   X is down "}})
	var notes []string
	b.store.view(func(d *storeData) { notes = d.KnownNotes })
	if len(notes) != 1 || notes[0] != "Source X is down" {
		t.Fatalf("notes %q", notes)
	}
	if got := withKnownNotes("list", notes); got != "- Source X is down\n\nlist" {
		t.Errorf("post text %q", got)
	}
	post(url.Values{"s": {"support"}, "do": {"known-remove"}, "name": {"Source X is down"}})
	b.store.view(func(d *storeData) { notes = d.KnownNotes })
	if len(notes) != 0 {
		t.Error("note not removed")
	}
}

func TestDashboardStatusesAndLevels(t *testing.T) {
	b := dashTestBot(t)
	h := b.dashboardHandler(false)
	post := func(v url.Values) {
		v.Set("t", dashToken)
		dashPostForm(t, h, v, nil)
	}
	b.pres = &presence{custom: func() (l []string) {
		b.store.view(func(d *storeData) { l = d.Statuses })
		return l
	}}
	post(url.Values{"s": {"bot"}, "do": {"status-add"}, "text": {"Watching Frieren"}})
	acts := b.pres.activities()
	if len(acts) != 2 || acts[1].State != "Watching Frieren" {
		t.Errorf("custom statuses not used: %d", len(acts))
	}
	post(url.Values{"s": {"bot"}, "do": {"status-reset"}})
	if len(b.pres.activities()) != 1+len(defaultStatuses) {
		t.Error("reset kept custom statuses")
	}

	b.store.update(func(d *storeData) { d.LevelRoles = map[int]string{3: "r3", 12: "r12"} })
	if got := b.levelRoles(); got[3] != "r3" || got[12] != "r12" {
		t.Errorf("picked roles %v", got)
	}
	post(url.Values{"s": {"games"}, "do": {"level-role-remove"}, "name": {"3"}})
	if _, ok := b.levelRoles()[3]; ok {
		t.Error("level role not removed")
	}
	post(url.Values{"s": {"games"}, "do": {"level-role-add"}, "v": {"nope"}, "text": {"5"}})
	if page := dashGet(t, h, "/?s=games", "127.0.0.1:1", nil).Body.String(); !strings.Contains(page, "isn&#39;t on the server") {
		t.Error("unknown role accepted")
	}
}

func TestDashboardBackup(t *testing.T) {
	b := dashTestBot(t)
	h := b.dashboardHandler(false)
	w := dashGet(t, h, "/?download=data", "127.0.0.1:1", nil)
	if !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") || !strings.Contains(w.Body.String(), `"Frieren"`) {
		t.Fatalf("download: %q", w.Header().Get("Content-Disposition"))
	}
	backup := w.Body.Bytes()
	b.store.update(func(d *storeData) { d.Follows = nil })

	upload := func(data []byte) *httptest.ResponseRecorder {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		for k, v := range map[string]string{"t": dashToken, "s": "bot", "do": "restore"} {
			mw.WriteField(k, v)
		}
		fw, _ := mw.CreateFormFile("file", "backup.json")
		fw.Write(data)
		mw.Close()
		r := httptest.NewRequest(http.MethodPost, "/", &body)
		r.RemoteAddr = "127.0.0.1:1"
		r.Header.Set("Content-Type", mw.FormDataContentType())
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := upload([]byte("not json")); w.Code != http.StatusSeeOther {
		t.Fatalf("bad file: got %d", w.Code)
	}
	if page := dashGet(t, h, "/?s=bot", "127.0.0.1:1", nil).Body.String(); !strings.Contains(page, "isn&#39;t a bot data file") {
		t.Error("bad backup not refused")
	}
	upload(backup)
	var n int
	b.store.view(func(d *storeData) { n = len(d.Follows) })
	if n != 3 {
		t.Errorf("restore gave %d follow lists", n)
	}
	if _, err := os.Stat(b.store.path + ".before-restore"); err != nil {
		t.Error("no copy of the old data")
	}
}

func TestDashNameNoSession(t *testing.T) {
	b := &bot{}
	if got := b.dashName("123456789012"); got != "member …789012" {
		t.Errorf("got %q", got)
	}
	if got := b.dashName("42"); got != "member 42" {
		t.Errorf("got %q", got)
	}
}

func TestDashboardNav(t *testing.T) {
	groups := dashNav("moderation")
	seen := map[string]bool{}
	on := ""
	for _, g := range groups {
		for _, p := range g.Pages {
			seen[p.Slug] = true
			if p.On {
				on = g.Key + "/" + p.Slug
			}
		}
	}
	for _, s := range dashSections {
		if !seen[s.Slug] {
			t.Errorf("page %s missing from the menu", s.Slug)
		}
	}
	if on != "safety/moderation" {
		t.Errorf("current page = %q", on)
	}
	h := dashTestBot(t).dashboardHandler(false)
	w := dashGet(t, h, "/?s=moderation", "127.0.0.1:1", nil)
	if body := w.Body.String(); !strings.Contains(body, `class="tabbar"`) || !strings.Contains(body, `<details class="sub">`) {
		t.Error("menus missing")
	}
	for _, path := range []string{"/dash.js", "/fonts/Inter-Medium.otf"} {
		if w := dashGet(t, h, path, "127.0.0.1:1", nil); w.Code != http.StatusOK || w.Body.Len() == 0 {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/fonts/../config.yaml", "/fonts/OtakaseSymbols.ttf", "/nope"} {
		if w := dashGet(t, h, path, "127.0.0.1:1", nil); w.Code != http.StatusNotFound {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
}

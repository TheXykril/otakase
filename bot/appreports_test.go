package main

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestCleanBackup(t *testing.T) {
	in := "# otakase\r\nPlayer=mpv\nKodiPassword=hunter2\nTraktClientSecret=abc\nMyAnimeListClientID=123\nSomeToken=x\n\nSkipOp=true\x07\n"
	got := cleanBackup(in)
	if got != "# otakase\nPlayer=mpv\nSkipOp=true" {
		t.Fatalf("cleanBackup = %q", got)
	}
}

func TestBackupEndpoints(t *testing.T) {
	b := appTestBot(t)
	backupLimit = newRateLimit(10, time.Hour)
	tok := testToken(b, "u1")
	other := testToken(b, "u1") // a second PC of the same member
	if w := apiDo(t, b, http.MethodGet, "/v1/backup", nil, map[string]string{"Authorization": "Bearer " + tok}); w.Code != http.StatusNotFound {
		t.Fatalf("get before any backup: %d", w.Code)
	}
	if w := apiJSON(t, b, "/v1/backup", `{"config":"Player=mpv\nKodiPassword=x"}`, tok); w.Code != http.StatusOK {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	w := apiDo(t, b, http.MethodGet, "/v1/backup", nil, map[string]string{"Authorization": "Bearer " + other})
	if w.Code != http.StatusOK {
		t.Fatalf("get from the other app: %d %s", w.Code, w.Body)
	}
	if got := decode(t, w); got["config"] != "Player=mpv" || got["at"] == "" {
		t.Fatalf("get: %v", got)
	}
	if w := apiJSON(t, b, "/v1/backup", `{"config":"`+strings.Repeat("a", maxBackupBytes+1)+`"}`, tok); w.Code != http.StatusRequestEntityTooLarge && w.Code != http.StatusBadRequest {
		t.Fatalf("too large: %d", w.Code)
	}
	if w := apiJSON(t, b, "/v1/backup", `{"config":"KodiPassword=x"}`, tok); w.Code != http.StatusBadRequest {
		t.Fatalf("only secrets: %d", w.Code)
	}
	if w := apiDo(t, b, http.MethodDelete, "/v1/backup", nil, map[string]string{"Authorization": "Bearer " + tok}); w.Code != http.StatusOK {
		t.Fatalf("delete: %d", w.Code)
	}
	if _, ok := b.memberBackup("u1"); ok {
		t.Fatal("backup still there after delete")
	}
	if w := apiJSON(t, b, "/v1/backup", `{"config":"Player=mpv"}`, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", w.Code)
	}
}

func TestBackupView(t *testing.T) {
	e, comps := backupView(appBackup{}, false)
	if len(comps) != 0 || !strings.Contains(e.Description, "no Otakase settings backup") {
		t.Fatalf("none: %v %v", e.Description, comps)
	}
	e, comps = backupView(appBackup{Config: "A=1\nB=2", At: time.Unix(1700000000, 0), FromOS: "windows", Version: "26.8.0"}, true)
	if len(comps) != 1 || !strings.Contains(e.Description, "<t:1700000000:R>") || !strings.Contains(e.Description, "Windows · Otakase 26.8.0, 2 settings") {
		t.Fatalf("backup: %v", e.Description)
	}
}

func TestProviderReports(t *testing.T) {
	b := appTestBot(t)
	providerReportLimit = newRateLimit(60, time.Hour)
	appProviderReports.m = map[string]map[string]providerReport{}
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	appNow = func() time.Time { return now }
	send := func(user, body string) int {
		return apiJSON(t, b, "/v1/provider-report", body, testToken(b, user)).Code
	}
	for _, u := range []string{"u1", "u2", "u3"} {
		if c := send(u, `{"provider":"AllAnime","ok":false,"kind":"search","error":"EOF"}`); c != http.StatusOK {
			t.Fatalf("report: %d", c)
		}
	}
	send("u4", `{"provider":"allanime","ok":true,"kind":"episode"}`)
	send("u1", `{"provider":"anikoto","ok":true}`)
	send("u2", `{"provider":"sukebei","ok":false}`)
	if c := send("u1", `{"provider":"bad name!","ok":false}`); c != http.StatusBadRequest {
		t.Fatalf("bad provider: %d", c)
	}
	// u1 tried again and allanime worked: their latest word counts.
	now = now.Add(10 * time.Minute)
	send("u3", `{"provider":"allanime","ok":true}`)

	lines := providerReportLines(providerTallies(now, false), now)
	want := []string{
		"🟢 1 source working for members",
		"🟠 1 source failing for some members",
		"Latest failure 10 min ago",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lines:\n%s", strings.Join(lines, "\n"))
	}
	adult := providerReportLines(providerTallies(now, true), now)
	if !strings.Contains(strings.Join(adult, "\n"), "🔴 1 source failing for everyone who tried") {
		t.Fatalf("18+ lines:\n%s", strings.Join(adult, "\n"))
	}
	e := withAppReports(&discordgo.MessageEmbed{Title: "Sources"}, now, false)
	if len(e.Fields) != 1 || !strings.Contains(e.Fields[0].Value, "1 source failing") || strings.Contains(e.Fields[0].Value, "allanime") {
		t.Fatalf("embed: %+v", e.Fields)
	}
	if strings.Contains(e.Fields[0].Value, "u1") {
		t.Fatal("reports must stay anonymous")
	}
	// Old reports drop out of the window.
	later := now.Add(4 * time.Hour)
	if got := providerTallies(later, false); len(got) != 0 {
		t.Fatalf("stale tallies: %v", got)
	}
	if e := withAppReports(&discordgo.MessageEmbed{Title: "Sources"}, later, false); len(e.Fields) != 0 {
		t.Fatal("no field without reports")
	}
}

func TestNormalScore(t *testing.T) {
	for in, want := range map[float64]float64{8: 8, 8.3: 8.5, 85: 8.5, 0: 0, 10: 10, 100: 10} {
		if got, ok := normalScore(in); !ok || got != want {
			t.Errorf("normalScore(%v) = %v %v, want %v", in, got, ok, want)
		}
	}
	for _, in := range []float64{-1, 101} {
		if _, ok := normalScore(in); ok {
			t.Errorf("normalScore(%v) accepted", in)
		}
	}
	if scoreStars(8) != "★★★★☆" || scoreText(8.5) != "8.5" || scoreText(7) != "7" {
		t.Fatal("score text")
	}
}

func TestRatingEndpoint(t *testing.T) {
	b := appTestBot(t)
	ratingLimit = newRateLimit(5, time.Hour)
	ratingsShared.m = map[string]time.Time{}
	shows := map[int]airingShowInfo{1: {SiteURL: "https://anilist.co/anime/1"}, 2: {IsAdult: true}}
	old := appShowInfo
	appShowInfo = func(id int) (airingShowInfo, bool) { s, ok := shows[id]; return s, ok }
	t.Cleanup(func() { appShowInfo = old })
	tok := testToken(b, "u1")
	for body, code := range map[string]int{
		`{"anilist_id":1,"score":11.5,"text":"x"}`: http.StatusServiceUnavailable, // valid, but no Discord in tests
		`{"anilist_id":1,"score":-2}`:              http.StatusBadRequest,
		`{"anilist_id":0,"score":5}`:               http.StatusBadRequest,
		`{"anilist_id":2,"score":5}`:               http.StatusForbidden,
		`{"anilist_id":3,"score":5}`:               http.StatusNotFound,
	} {
		if w := apiJSON(t, b, "/v1/rating", body, tok); w.Code != code {
			t.Errorf("%s: %d %s, want %d", body, w.Code, w.Body, code)
		}
	}
	b.store.update(func(d *storeData) { d.Settings = map[string]string{"score_sharing": "false"} })
	if w := apiJSON(t, b, "/v1/rating", `{"anilist_id":1,"score":8}`, tok); !strings.Contains(w.Body.String(), "turned off") {
		t.Errorf("sharing off: %d %s", w.Code, w.Body)
	}
}

func TestRatingMessage(t *testing.T) {
	info := airingShowInfo{ID: 154587, SiteURL: "https://anilist.co/anime/154587"}
	info.Title.Romaji = "Sousou no Frieren"
	info.CoverImage.ExtraLarge = "https://img/cover.jpg"
	msg := ratingMessage("42", info, 9.5, "@everyone loved it")
	e := msg.Embeds[0]
	if e.Title != "Sousou no Frieren" || e.Thumbnail == nil || !strings.Contains(e.Description, "<@42>") || !strings.Contains(e.Description, "9.5/10") {
		t.Fatalf("card: %+v", e)
	}
	if strings.Contains(e.Description, "@everyone") {
		t.Fatal("member text must not ping")
	}
	if msg.AllowedMentions == nil || len(msg.AllowedMentions.Users) != 0 {
		t.Fatal("the card must not ping the member")
	}
	row := msg.Components[0].(discordgo.ActionsRow)
	if len(row.Components) != 2 || row.Components[0].(discordgo.Button).Label != "Play in Otakase" || row.Components[1].(discordgo.Button).Label != "Queue in Otakase" {
		t.Fatalf("buttons: %+v", row.Components)
	}
}

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFollowList(t *testing.T) {
	var d storeData
	a := followedShow{ID: 1, Title: "Frieren"}
	if !addFollow(&d, "u", a) || !addFollow(&d, "u", a) || len(d.Follows["u"]) != 1 {
		t.Fatalf("add twice: %+v", d.Follows)
	}
	addFollow(&d, "u", followedShow{ID: 2, Title: "Dandadan"})
	addFollow(&d, "v", followedShow{ID: 2, Title: "Dandadan"})
	if got := followedIDs(d.Follows); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("ids: %v", got)
	}
	if f, ok := removeFollow(&d, "u", 1); !ok || f.Title != "Frieren" || len(d.Follows["u"]) != 1 {
		t.Errorf("remove: %v %+v", ok, d.Follows)
	}
	if _, ok := removeFollow(&d, "u", 1); ok {
		t.Error("removed twice")
	}
	removeFollow(&d, "u", 2)
	if _, ok := d.Follows["u"]; ok {
		t.Error("empty list kept")
	}
	for k := range maxFollows {
		addFollow(&d, "w", followedShow{ID: 100 + k})
	}
	if addFollow(&d, "w", followedShow{ID: 9999}) {
		t.Error("list over the limit")
	}
}

func TestEpisodeDMs(t *testing.T) {
	follows := map[string][]followedShow{
		"u": {{ID: 1, Title: "A"}, {ID: 2, Title: "B"}},
		"v": {{ID: 2, Title: "B"}},
	}
	aired := []followAiring{{MediaID: 2, At: 20, Episode: 5}, {MediaID: 1, At: 10, Episode: 3}, {MediaID: 3, At: 5, Episode: 1}}
	dms := episodeDMs(follows, aired)
	if len(dms) != 2 || len(dms["u"]) != 2 || len(dms["v"]) != 1 {
		t.Fatalf("dms: %+v", dms)
	}
	if dms["u"][0].Show.Title != "A" || dms["u"][1].Episode != 5 {
		t.Errorf("order or show: %+v", dms["u"])
	}
}

func TestEpisodeCard(t *testing.T) {
	card := func(a followAiring) string {
		b, _ := json.Marshal(episodeCard(a))
		return string(b)
	}
	got := card(followAiring{MediaID: 2, Episode: 12, Episodes: 12, MalID: 9, Cover: "https://img/c.jpg", Show: followedShow{Title: "B", URL: "https://anilist.co/anime/2"}})
	for _, want := range []string{"FINALE", "https://img/c.jpg", "https://myanimelist.net/anime/9", "follow-stop:2", `"type":9`, "open/?anilist=2", "ep=12"} {
		if !strings.Contains(got, want) {
			t.Errorf("finale card misses %q: %s", want, got)
		}
	}
	got = card(followAiring{MediaID: 3, Episode: 4, At: 100, NextAt: 700, NextEp: 5, Show: followedShow{Title: "C"}})
	for _, want := range []string{"Episode 4 is out.", "episode 5 \\u003ct:700:R", "https://anilist.co/anime/3"} {
		if !strings.Contains(got, want) {
			t.Errorf("card misses %q: %s", want, got)
		}
	}
	if strings.Contains(got, "myanimelist") || strings.Contains(got, `"type":9`) {
		t.Errorf("no MAL id or cover: %s", got)
	}
}

func TestFollowedChoices(t *testing.T) {
	list := []followedShow{{ID: 1, Title: "Frieren"}, {ID: 2, Title: "Dandadan"}}
	if got := followedChoices(list, ""); len(got) != 2 {
		t.Errorf("all: %d", len(got))
	}
	if got := followedChoices(list, "fRi"); len(got) != 1 || got[0].Value != "anilist:1" {
		t.Errorf("match: %+v", got)
	}
}

func TestFollowEmbeds(t *testing.T) {
	l := followLookup{Show: followedShow{ID: 1, Title: "A", URL: "u"}, NextAt: 1700000000, NextEp: 4}
	if e := followAckEmbed(l); !strings.Contains(e.Description, "<t:1700000000:R>") {
		t.Errorf("next: %s", e.Description)
	}
	if e := followAckEmbed(followLookup{Status: "FINISHED"}); !strings.Contains(e.Description, "finished") {
		t.Errorf("finished: %s", e.Description)
	}
	if e := followingEmbed(nil); !strings.Contains(e.Description, "/follow") {
		t.Errorf("empty: %s", e.Description)
	}
}

func TestFetchFollowAiring(t *testing.T) {
	var vars map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Variables map[string]any }
		json.NewDecoder(r.Body).Decode(&body)
		vars = body.Variables
		w.Write([]byte(`{"data":{"Page":{"pageInfo":{"hasNextPage":false},"airingSchedules":[
			{"airingAt":150,"episode":7,"mediaId":2,"media":{"episodes":12,"idMal":5,"coverImage":{"large":"c.jpg"},"nextAiringEpisode":{"airingAt":750,"episode":8}}}]}}}`))
	}))
	defer srv.Close()
	old := anilistAPI
	anilistAPI = srv.URL
	defer func() { anilistAPI = old }()

	got, err := fetchFollowAiring([]int{2}, time.Unix(100, 0), time.Unix(200, 0))
	if err != nil || len(got) != 1 || got[0].MediaID != 2 || got[0].Episode != 7 || got[0].Episodes != 12 ||
		got[0].MalID != 5 || got[0].Cover != "c.jpg" || got[0].NextEp != 8 || got[0].NextAt != 750 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if vars["from"].(float64) != 100 || vars["to"].(float64) != 201 {
		t.Errorf("window: %v", vars)
	}
}

func TestFollowStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	openStore(path).update(func(d *storeData) {
		addFollow(d, "u", followedShow{ID: 1, Title: "A"})
		d.FollowsChecked = now
	})
	again := openStore(path)
	if len(again.data.Follows["u"]) != 1 || !again.data.FollowsChecked.Equal(now) {
		t.Errorf("reloaded: %+v", again.data)
	}
}

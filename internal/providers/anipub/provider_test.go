package anipub

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/thexykril/otakase/internal/providerhost"
	"github.com/thexykril/otakase/internal/providers"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestParseSearch(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantIDs []int
		wantErr string
	}{
		{name: "list", body: `[{"Name":"Frieren","Id":2454},{"Name":"Frieren S2","Id":3101}]`, wantIDs: []int{2454, 3101}},
		{name: "empty list", body: `[]`},
		{name: "single object", body: `{"Name":"Delicious in Dungeon","Id":2475,"finder":"delicious-in-dungeon"}`, wantIDs: []int{2475}},
		{name: "not found", body: ` {"found":false} `},
		{name: "rate limited", body: `{"error":"Too many requests, please try again later."}`, wantErr: "rate limited"},
		{name: "blank", body: "  \n", wantErr: "empty search response"},
		{name: "object without id", body: `{"Name":"?"}`, wantErr: "parse anipub search response"},
		{name: "html", body: `<html>maintenance</html>`, wantErr: "parse anipub search response"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hits, err := parseSearch([]byte(tc.body))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(hits) != len(tc.wantIDs) {
				t.Fatalf("got %d hits, want %d", len(hits), len(tc.wantIDs))
			}
			for i, id := range tc.wantIDs {
				if hits[i].ID != id {
					t.Errorf("hit %d id = %d, want %d", i, hits[i].ID, id)
				}
			}
		})
	}

	if _, err := parseSearch([]byte(`{"error":"too many requests"}`)); !errors.Is(err, providerhost.ErrRateLimited) {
		t.Fatalf("throttling did not map to ErrRateLimited: %v", err)
	}
}

func TestSearchAnime(t *testing.T) {
	web := newFakeWeb(t)
	web.text("anipub.test/api/search/frieren beyond", fixture(t, "search_frieren.json"))
	web.text("anipub.test/api/info/2454", fixture(t, "info_2454.json"))
	// 3101's info is missing: it is still listed, without the extras.

	results, err := (&Provider{}).SearchAnime(" frieren beyond ", "dub")
	if err != nil {
		t.Fatal(err)
	}
	want := []providers.SelectionOption{
		{
			Key:       "2454",
			Label:     "Frieren: Beyond Journey's End · 28 eps",
			Title:     "Frieren: Beyond Journey's End",
			Thumbnail: "https://anipub.xyz/img/frieren-cover.jpg",
			ExtraData: SearchItem{ID: 2454, MalID: 52991, Name: "Frieren: Beyond Journey's End", Finder: "frieren-beyond-journeys-end", Episodes: 28},
		},
		{
			Key:       "3101",
			Label:     "Frieren: Beyond Journey's End Season 2",
			Title:     "Frieren: Beyond Journey's End Season 2",
			Thumbnail: "https://anipub.xyz/img/frieren-s2.jpg",
			ExtraData: SearchItem{ID: 3101, Name: "Frieren: Beyond Journey's End Season 2", Finder: "frieren-beyond-journeys-end-season-2"},
		},
	}
	if len(results) != len(want) {
		t.Fatalf("got %d results: %+v", len(results), results)
	}
	for i := range want {
		if results[i] != want[i] {
			t.Errorf("result %d:\n got %+v\nwant %+v", i, results[i], want[i])
		}
	}
	if got := web.header("anipub.test/api/info/2454", "Accept"); got != "application/json" {
		t.Errorf("info Accept = %q", got)
	}
	if got := web.header("anipub.test/api/search/frieren beyond", "Referer"); got != "http://anipub.test/" {
		t.Errorf("search Referer = %q", got)
	}
	if web.asked("anipub.test/api/info/0") {
		t.Error("asked for the info of a hit without an id")
	}
}

func TestSearchAnimeNothingFound(t *testing.T) {
	web := newFakeWeb(t)
	web.text("anipub.test/api/search/zzz", `{"found":false}`)
	web.text("anipub.test/api/search/broken", `[{"Name":"Broken","Id":0}]`)

	for _, query := range []string{"zzz", "broken"} {
		if _, err := (&Provider{}).SearchAnime(query, "sub"); err == nil || !strings.Contains(err.Error(), "no results for") {
			t.Errorf("%s: err = %v, want no results", query, err)
		}
	}
	if _, err := (&Provider{}).SearchAnime(" ", "sub"); err == nil {
		t.Error("empty query was accepted")
	}
}

func TestEpisodesList(t *testing.T) {
	web := newFakeWeb(t)
	web.text("anipub.test/v1/api/details/2454", fixture(t, "details_2454.json"))
	web.text("anipub.test/v1/api/details/9", `{"local":{"link":"src=https://www.anipub.xyz/video/1/sub","ep":[]}}`)

	got, err := (&Provider{}).EpisodesList(" 2454 ", "sub")
	if err != nil || strings.Join(got, ",") != "1,2,3" {
		t.Fatalf("2454: %q, %v", got, err)
	}
	got, err = (&Provider{}).EpisodesList("9", "sub")
	if err != nil || strings.Join(got, ",") != "1" {
		t.Fatalf("movie: %q, %v", got, err)
	}

	for id, want := range map[string]string{
		"":                    "empty show id",
		"rich-girl-caretaker": `invalid anipub show id "rich-girl-caretaker"`,
		"404":                 "anipub request failed with status 404",
	} {
		if _, err := (&Provider{}).EpisodesList(id, "sub"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", id, err, want)
		}
	}
}

func TestEpisodeLink(t *testing.T) {
	var details showDetails
	details.Local.Link = " src=https://www.anipub.xyz/video/1/sub "
	details.Local.Ep = append(details.Local.Ep, struct {
		Link string `json:"link"`
	}{Link: "https://www.anipub.xyz/video/2/sub"})

	for ep, want := range map[int]string{
		1: "https://www.anipub.xyz/video/1/sub",
		2: "https://www.anipub.xyz/video/2/sub",
	} {
		if got, err := episodeLink(details, ep); err != nil || got != want {
			t.Errorf("episode %d: %q, %v", ep, got, err)
		}
	}
	for _, ep := range []int{0, 3, -1} {
		if _, err := episodeLink(details, ep); err == nil {
			t.Errorf("episode %d was found", ep)
		}
	}
}

// serveEpisode sets up a show whose episodes resolve through megaplay.
func serveEpisode(t *testing.T, web *fakeWeb) {
	t.Helper()
	web.text("anipub.test/v1/api/details/2454", fixture(t, "details_2454.json"))
	for _, page := range []string{
		"megaplay.test/stream/s-2/107257/sub",
		"megaplay.test/stream/s-2/107257/dub",
		"megaplay.test/stream/mal/52991/3/sub",
	} {
		web.text(page, fixture(t, "stream_page.html"))
	}
	web.routes["megaplay.test/stream/getSources"] = func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("id") != "13461" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(fixture(t, "sources_13461.json")))
	}
}

func TestGetEpisodeURLSub(t *testing.T) {
	web := newFakeWeb(t)
	quietSubStyle(t)
	serveEpisode(t, web)

	links, hints, err := (&Provider{}).GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "2454", 1, "sub")
	if err != nil {
		t.Fatal(err)
	}
	const stream = "http://cdn.test/hls/107257/master.m3u8"
	if len(links) != 1 || links[0] != stream {
		t.Fatalf("links = %q", links)
	}
	hint := hints[stream]
	if hint.Referrer != "http://megaplay.test/" {
		t.Errorf("referrer = %q", hint.Referrer)
	}
	if hint.Subtitle != "http://cdn.test/subs/107257-eng.vtt" {
		t.Errorf("default subtitle = %q, want the English track", hint.Subtitle)
	}
	if len(hint.Subtitles) != 2 || hint.Subtitles[0].Label != "Portuguese - Brazilian" || hint.Subtitles[1].Label != "English" {
		t.Errorf("tracks = %+v", hint.Subtitles)
	}
	if got := web.header("megaplay.test/stream/s-2/107257/sub", "Referer"); got != "http://anipub.test/" {
		t.Errorf("stream page Referer = %q", got)
	}
	if got := web.header("megaplay.test/stream/getSources", "Referer"); got != "http://megaplay.test/stream/s-2/107257/sub" {
		t.Errorf("getSources Referer = %q", got)
	}
}

func TestGetEpisodeURLDubAndPlayLinks(t *testing.T) {
	web := newFakeWeb(t)
	quietSubStyle(t)
	serveEpisode(t, web)
	p := &Provider{}

	// The link says sub; dub was asked for.
	_, hints, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "2454", 1, "dub")
	if err != nil {
		t.Fatal(err)
	}
	hint := hints["http://cdn.test/hls/107257/master.m3u8"]
	if hint.Subtitle != "" || hint.Subtitles != nil {
		t.Errorf("dub kept subtitles: %+v", hint)
	}
	if !web.asked("megaplay.test/stream/s-2/107257/dub") {
		t.Error("dub page was not asked for")
	}

	links, err := p.GetEpisodeURLForMode(providers.PlaybackConfig{}, "2454", 3, "sub")
	if err != nil || len(links) != 1 {
		t.Fatalf("play link: %q, %v", links, err)
	}
	if !web.asked("megaplay.test/stream/mal/52991/3/sub") {
		t.Error("play link did not go through the mal route")
	}
}

func TestGetEpisodeURLFailures(t *testing.T) {
	web := newFakeWeb(t)
	quietSubStyle(t)
	web.text("anipub.test/v1/api/details/1", `{"local":{"link":"src=https://www.anipub.xyz/video/11/sub"}}`)
	web.text("megaplay.test/stream/s-2/11/sub", `<div id="player"></div>`)
	web.text("anipub.test/v1/api/details/2", `{"local":{"link":"src=https://www.anipub.xyz/video/22/sub"}}`)
	web.text("megaplay.test/stream/s-2/22/sub", `<div data-id="5"></div>`)
	web.text("megaplay.test/stream/getSources", `{"sources":{"file":" "},"tracks":[]}`)
	web.text("anipub.test/v1/api/details/3", `{"local":{"link":"src=https://www.anipub.xyz/watch/3"}}`)

	cases := []struct {
		id      string
		ep      int
		wantErr string
	}{
		{"x1", 1, `invalid anipub show id "x1"`},
		{"1", 0, "invalid episode number 0"},
		{"1", 2, "episode 2 not found"},
		{"1", 1, "megaplay data-id not found"},
		{"2", 1, "megaplay stream url missing"},
		{"3", 1, "unsupported video link"},
	}
	for _, tc := range cases {
		_, _, err := (&Provider{}).GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, tc.id, tc.ep, "sub")
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s ep %d: err = %v, want %q", tc.id, tc.ep, err, tc.wantErr)
		}
	}
}

func TestGetEpisodeURLRefusesAdStream(t *testing.T) {
	web := newFakeWeb(t)
	quietSubStyle(t)
	var logged []string
	previousLog := providerhost.Log
	providerhost.Log = func(msg string) { logged = append(logged, msg) }
	t.Cleanup(func() { providerhost.Log = previousLog })

	web.text("anipub.test/v1/api/details/5", `{"local":{"link":"src=https://www.anipub.xyz/video/55/sub"}}`)
	web.text("megaplay.test/stream/s-2/55/sub", `<div data-id="7"></div>`)
	web.text("megaplay.test/stream/getSources", `{"sources":{"file":"http://megap.kotocdn.site/55/master.m3u8"}}`)
	web.text("megap.kotocdn.site/55/master.m3u8", "#EXTM3U\n#EXTINF:5,\nhttps://p16-ad-sg.ibyteimg.com/obj/ad-site-i18n/a\n")

	_, _, err := (&Provider{}).GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "5", 1, "sub")
	if err == nil || !strings.Contains(err.Error(), "decoy") {
		t.Fatalf("err = %v, want a decoy refusal", err)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "rejected") {
		t.Fatalf("log = %q", logged)
	}
}

func TestDefaultSubtitle(t *testing.T) {
	type track = struct {
		File    string `json:"file"`
		Label   string `json:"label"`
		Kind    string `json:"kind"`
		Default bool   `json:"default"`
	}
	payload := func(tracks ...track) megaplaySourcesResponse {
		var p megaplaySourcesResponse
		p.Tracks = tracks
		return p
	}
	cases := []struct {
		name    string
		payload megaplaySourcesResponse
		mode    string
		want    string
	}{
		{"default flag", payload(track{File: "a.vtt", Label: "French", Kind: "captions", Default: true}, track{File: "b.vtt", Label: "English", Kind: "captions"}), "sub", "a.vtt"},
		{"english by label", payload(track{File: "a.vtt", Label: "French", Kind: "captions"}, track{File: "b.vtt", Label: "English [CC]", Kind: "captions"}), "sub", "b.vtt"},
		{"first caption", payload(track{File: "t.vtt", Kind: "thumbnails"}, track{File: "a.vtt", Label: "French", Kind: "captions"}), "sub", "a.vtt"},
		{"no captions", payload(track{File: "t.vtt", Kind: "thumbnails", Default: true}), "sub", ""},
		{"dub", payload(track{File: "a.vtt", Kind: "captions", Default: true}), "dub", ""},
	}
	for _, tc := range cases {
		if got := defaultSubtitle(tc.payload, tc.mode); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

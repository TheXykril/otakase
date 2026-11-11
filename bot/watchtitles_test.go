package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWatchTitleIDsRoundTrip(t *testing.T) {
	for _, imdb := range []string{"tt0903747", "tt0133093", "tt15398776"} {
		id := watchTitleID(imdb)
		if id > -watchTitleBase-1 || watchTitleIMDb(id) != imdb {
			t.Errorf("%s → %d → %q", imdb, id, watchTitleIMDb(id))
		}
		if !(watchShow{ID: id}).isTitle() {
			t.Errorf("%d is not a title", id)
		}
	}
	for _, id := range []int{1, 0, watchMovieID} {
		if watchTitleIMDb(id) != "" || (watchShow{ID: id}).isTitle() {
			t.Errorf("%d read as a title", id)
		}
	}
	if watchTitleID("nm0000123") != 0 {
		t.Error("a person read as a title")
	}
}

func TestWatchTitleSuggestionsKeepFilmsAndSeries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"d":[
			{"id":"tt0903747","l":"Breaking Bad","qid":"tvSeries","y":2008,"i":{"imageUrl":"https://m.media-amazon.com/images/M/x._V1_.jpg"}},
			{"id":"in0000274","l":"Breaking Bad","s":"Franchise"},
			{"id":"tt1924245","l":"Breaking Bad: Original Minisodes","qid":"videoGame"},
			{"id":"tt9243946","l":"El Camino","qid":"movie","y":2019}]}`))
	}))
	defer srv.Close()
	old := watchIMDbSearch
	watchIMDbSearch = srv.URL + "/"
	t.Cleanup(func() { watchIMDbSearch = old })
	got, err := watchTitleSearch("breaking bad")
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %+v", err, got)
	}
	if got[0].Format != "TV" || got[0].Cover != "https://m.media-amazon.com/images/M/x._V1_QL75_UX300_.jpg" || !got[0].series() {
		t.Errorf("series: %+v", got[0])
	}
	if got[1].Format != "MOVIE" || got[1].Episodes != 1 || got[1].series() {
		t.Errorf("film: %+v", got[1])
	}
}

func TestWatchTitleEpisodesNumberAcrossSeasons(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/lookup/shows":
			if r.URL.Query().Get("imdb") != "tt0903747" {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte(`{"id":169}`))
		case "/shows/169/episodes":
			w.Write([]byte(`[
				{"name":"Pilot","season":1,"number":1,"type":"regular","airstamp":"2008-01-21T03:00:00+00:00"},
				{"name":"Special","season":1,"number":null,"type":"significant_special"},
				{"name":"Cat's in the Bag","season":1,"number":2,"type":"regular","airstamp":"2008-01-28T03:00:00+00:00"},
				{"name":"Seven Thirty-Seven","season":2,"number":1,"type":"regular","airstamp":"2009-03-09T02:00:00+00:00"},
				{"name":"Later","season":3,"number":1,"type":"regular","airstamp":"2999-01-01T00:00:00+00:00"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := watchTVmaze
	watchTVmaze = srv.URL
	t.Cleanup(func() { watchTVmaze = old })
	watchTitleEpisodeCache.Lock()
	watchTitleEpisodeCache.m = nil
	watchTitleEpisodeCache.Unlock()
	id := watchTitleID("tt0903747")
	arg, err := watchTitleArg(context.Background(), id, 3)
	if err != nil || arg != "tt0903747:2:1" {
		t.Fatalf("episode 3 = %q %v", arg, err)
	}
	if _, err := watchTitleArg(context.Background(), id, 4); err == nil {
		t.Error("an episode not out yet was asked for")
	}
	previews := watchTitlePreviews(context.Background(), watchShow{ID: id, Format: "TV"})
	if previews[2].Title != "S1 E2 · Cat's in the Bag" || len(previews) != 3 {
		t.Errorf("previews: %+v", previews)
	}
	// A film is not on TVmaze and plays on its own.
	if arg, err := watchTitleArg(context.Background(), watchTitleID("tt0133093"), 1); err != nil || arg != "tt0133093" {
		t.Errorf("film = %q %v", arg, err)
	}
}

func TestWatchRoomPlaysAFilm(t *testing.T) {
	b, _ := watchBot(t, "https://cdn.example/ep.m3u8")
	b.store.update(func(d *storeData) { d.WatchTickets = map[string]int{"a": 1} })
	oldTitle, oldResolve := watchTitleByID, watchResolveTitle
	t.Cleanup(func() { watchTitleByID, watchResolveTitle = oldTitle, oldResolve })
	id := watchTitleID("tt0133093")
	watchTitleByID = func(got int) (watchShow, error) {
		return watchShow{ID: got, Title: "The Matrix", Format: "MOVIE", Episodes: 1, Year: 1999}, nil
	}
	asked := make(chan int, 2)
	watchResolveTitle = func(_ context.Context, _ string, got, ep int) (streamInfo, error) {
		asked <- got
		sub := "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte("1\n00:00:01,000 --> 00:00:02,000\nWake up\n"))
		return streamInfo{Episode: ep, Provider: "vidsrc", Mode: "sub", Streams: []streamLink{{URL: "https://cdn.example/film.m3u8",
			Subtitles: []streamSubtitle{{URL: sub, Language: "en", Label: "English"}}}}}, nil
	}
	watchPost(b, (*bot).watchOpen, "a", watchIn{Room: "r", Channel: "vc"}, nil)
	if code := watchPost(b, (*bot).watchPick, "a", watchIn{Room: "r", Show: id, Ep: 2}, nil); code != http.StatusBadRequest {
		t.Fatalf("a film has one episode: %d", code)
	}
	if code := watchPost(b, (*bot).watchPick, "a", watchIn{Room: "r", Show: id, Ep: 1}, nil); code != 200 {
		t.Fatalf("pick: %d", code)
	}
	waitLoaded(t, b)
	if got := <-asked; got != id {
		t.Fatalf("resolved %d", got)
	}
	b.watch.mu.Lock()
	r := b.watch.only()
	if r.phase != "play" || r.show.Title != "The Matrix" || len(r.streams) != 1 || r.show.tracked() {
		b.watch.mu.Unlock()
		t.Fatalf("room: %q %q %+v", r.phase, r.errText, r.streams)
	}
	media := r.media
	b.watch.mu.Unlock()
	body, _, err := media.fetch(context.Background(), r.streams[0].Subtitles[0].URL, 1<<20)
	if err != nil || toWebVTT(string(body)) != "WEBVTT\n\n1\n00:00:01.000 --> 00:00:02.000\nWake up\n" {
		t.Errorf("inline subtitles: %q %v", body, err)
	}
}

func TestWatchSearchFindsTitlesWhenAsked(t *testing.T) {
	b, _ := watchBot(t, "https://cdn.example/ep.m3u8")
	old := watchTitleSearch
	t.Cleanup(func() { watchTitleSearch = old })
	watchTitleSearch = func(q string) ([]watchShow, error) {
		return []watchShow{{ID: watchTitleID("tt0133093"), Title: "The Matrix", Format: "MOVIE", Cover: "https://m.media-amazon.com/images/M/a.jpg"}}, nil
	}
	req := httptest.NewRequest(http.MethodGet, "/activity/api/watch/search?q=matrix&kind=titles", nil)
	req.Header.Set("Authorization", "Bearer "+b.acts.add("a"))
	rec := httptest.NewRecorder()
	b.watchSearchAPI(rec, req)
	var got []watchShow
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got) != 1 || got[0].Title != "The Matrix" ||
		got[0].Cover != "api/watch/cover?u=https%3A%2F%2Fm.media-amazon.com%2Fimages%2FM%2Fa.jpg" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

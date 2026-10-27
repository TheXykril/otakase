package main

import (
	"context"
	"testing"
	"time"
)

func TestWatchMovieQuery(t *testing.T) {
	for q, want := range map[string]bool{"otakase": true, "  OtaKase ": true, "otakase movie": false, "otak": false} {
		if got := isWatchMovieQuery(q); got != want {
			t.Errorf("%q: %v", q, got)
		}
	}
	if s := watchMovieShow(); !s.isMovie() || s.tracked() || s.lastEpisode() != 1 {
		t.Errorf("movie show: %+v", s)
	}
}

// The host picks the film: the helper draws it with their AniList access,
// the room plays it, and nothing is counted or saved on anyone's lists.
func TestWatchRoomPlaysTheMovie(t *testing.T) {
	b, now := watchBot(t, "https://cdn.example/ep.mp4")
	b.store.update(func(d *storeData) {
		d.WatchTickets = map[string]int{"a": 1}
		d.AniList = map[string]anilistLink{"a": {ID: 11, Verified: true, Token: "ta"}}
	})
	type call struct{ presenter, token string }
	calls := make(chan call, 1)
	old := watchStartMovie
	watchStartMovie = func(_ context.Context, _, presenter, token string, progress func(string)) (*watchTorrent, string, error) {
		progress("Developing the film 50%")
		calls <- call{presenter, token}
		return nil, "http://127.0.0.1:4567/playlist.m3u8", nil
	}
	t.Cleanup(func() { watchStartMovie = old })

	watchPost(b, (*bot).watchOpen, "a", watchIn{Room: "r", Channel: "vc"}, nil)
	if code := watchPost(b, (*bot).watchPick, "a", watchIn{Room: "r", Show: watchMovieID, Ep: 1}, nil); code != 200 {
		t.Fatalf("pick: %d", code)
	}
	waitLoaded(t, b)
	got := <-calls
	if got.token != "ta" {
		t.Errorf("helper got %+v", got)
	}
	r := b.watch.only()
	b.watch.mu.Lock()
	defer b.watch.mu.Unlock()
	if r.phase != "play" || len(r.streams) != 1 || r.streams[0].URL != "http://127.0.0.1:4567/playlist.m3u8" || r.show.Title != "Otakase: The Movie" {
		t.Fatalf("room: phase %q err %q streams %+v", r.phase, r.errText, r.streams)
	}
	r.dur = 100
	m := r.members["a"]
	m.watched = 95
	later := now.Add(95 * time.Second)
	r.setPos(95, true, later)
	if _, ok := r.resumeOf("a", later); ok {
		t.Error("a position was kept for the film")
	}
	if ep := r.countAsk("a", later); ep != 0 {
		t.Errorf("asked to count episode %d of the film", ep)
	}
}

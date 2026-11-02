package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEpisodeImagesAllowTVDBStillsOnly(t *testing.T) {
	if got := episodeImage("https://artworks.thetvdb.com/banners/v4/episode/1/screencap/a.jpg"); !strings.HasPrefix(got, "api/watch/cover?u=") {
		t.Errorf("TVDB still dropped: %q", got)
	}
	for _, bad := range []string{"http://artworks.thetvdb.com/a.jpg", "https://evil.example/a.jpg", "https://artworks.thetvdb.com:8443/a.jpg"} {
		if got := episodeImage(bad); got != "" {
			t.Errorf("%s allowed as %q", bad, got)
		}
	}
	rec := httptest.NewRecorder()
	proxyAniList(rec, httptest.NewRequest("GET", "/", nil), "https://evil.example/a.jpg")
	if rec.Code != 404 {
		t.Errorf("proxy served an unknown host: %d", rec.Code)
	}
}

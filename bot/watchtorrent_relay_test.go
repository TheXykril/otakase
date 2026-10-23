package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWatchTorrentCacheIsPerEpisode(t *testing.T) {
	body := "first episode"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.Write([]byte(body))
	}))
	defer upstream.Close()
	cache := newSegCache(1024)
	first, _, _ := newWatchMedia(streamLink{URL: upstream.URL + "/playlist.m3u8"}, false)
	second, _, _ := newWatchMedia(streamLink{URL: upstream.URL + "/playlist.m3u8"}, true)
	url := upstream.URL + "/seg0.ts"
	a, _, err := first.segment(url, cache)
	if err != nil {
		t.Fatal(err)
	}
	body = "second episode"
	b, _, err := second.segment(url, cache)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != "first episode" || string(b) != "second episode" {
		t.Fatalf("cross-episode cache reuse: %q %q", a, b)
	}
}

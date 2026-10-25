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

func TestWatchTorrentSubtitlesAreNotCached(t *testing.T) {
	body := "WEBVTT\n\nNOTE otakase-partial\n\n00:01.000 --> 00:02.000\nOne\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer upstream.Close()
	cache := newSegCache(1 << 20)
	m, _, subs := newWatchMedia(streamLink{URL: upstream.URL + "/playlist.m3u8", Subtitles: []streamSubtitle{{URL: upstream.URL + "/subs/en.vtt"}}}, false)
	get := func() string {
		res := httptest.NewRecorder()
		m.serve(res, httptest.NewRequest(http.MethodGet, "/"+subs[0], nil), subs[0], cache)
		if res.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("growing subtitles may be cached by the browser")
		}
		return res.Body.String()
	}
	if got := get(); got != body {
		t.Fatalf("first answer %q", got)
	}
	body = "WEBVTT\n\n00:01.000 --> 00:02.000\nOne\n\n00:09.000 --> 00:10.000\nTwo\n"
	if got := get(); got != body {
		t.Fatalf("relay kept the partial file: %q", got)
	}
}

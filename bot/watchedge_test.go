package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEdgePlaylist(t *testing.T) {
	master := "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",URI=\"2.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=1\n3.m3u8\n"
	media := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"4.ts\"\n#EXTINF:6,\n5.ts\n#EXTINF:6,\n6.ts\n#EXT-X-ENDLIST\n"
	edge := watchEdge{prefix: "/.proxy/cdn"}

	if out, n := edgePlaylist([]byte(master), "k", watchEdge{}); string(out) != master || n != 0 {
		t.Fatalf("no edge changed the playlist: %q", out)
	}
	if out, n := edgePlaylist([]byte(master), "k", edge); string(out) != master || n != 0 {
		t.Fatalf("master through the edge: %q %d", out, n)
	}
	out, n := edgePlaylist([]byte(media), "k", edge)
	want := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"4.ts\"\n#EXTINF:6,\n/.proxy/cdn/activity/api/watch/media/k/5.ts\n#EXTINF:6,\n/.proxy/cdn/activity/api/watch/media/k/6.ts\n#EXT-X-ENDLIST\n"
	if string(out) != want || n != 2 {
		t.Fatalf("segments through the edge: %d %q", n, out)
	}
	out, n = edgePlaylist([]byte(master), "k", watchEdge{direct: true})
	if !strings.Contains(string(out), `URI="2.m3u8?direct=1"`) || !strings.Contains(string(out), "\n3.m3u8?direct=1\n") || n != 0 {
		t.Fatalf("direct carried on: %q", out)
	}
	if out, _ := edgePlaylist([]byte(media), "k", watchEdge{direct: true}); string(out) != media {
		t.Fatalf("direct segments changed: %q", out)
	}
}

func TestWatchEdgeBudget(t *testing.T) {
	b, now := watchBot(t, "https://cdn.example/ep.m3u8")
	req := httptest.NewRequest("GET", "/x", nil)
	if e := b.watchEdgeFor(req); e.prefix != "" {
		t.Fatal("edge on without watch_cdn")
	}
	b.cfg.WatchCDN = "/.proxy/cdn/"
	b.store.update(func(d *storeData) { d.Settings = map[string]string{"watch_cdn_daily": "3"} })
	e := b.watchEdgeFor(req)
	if e.prefix != "/.proxy/cdn" {
		t.Fatalf("edge: %+v", e)
	}
	e.count(2)
	if e = b.watchEdgeFor(req); e.prefix == "" {
		t.Fatal("off before the budget")
	}
	e.count(2)
	if e = b.watchEdgeFor(req); e.prefix != "" {
		t.Fatal("still on past the budget")
	}
	*now = now.Add(24 * time.Hour)
	if e = b.watchEdgeFor(req); e.prefix == "" {
		t.Fatal("budget didn't reset the next day")
	}
	if e = b.watchEdgeFor(httptest.NewRequest("GET", "/x?direct=1", nil)); e.prefix != "" || !e.direct {
		t.Fatalf("player asked for the bot: %+v", e)
	}
	b.store.update(func(d *storeData) { d.Settings["watch_cdn_daily"] = "0" })
	if e = b.watchEdgeFor(req); e.prefix != "" {
		t.Fatal("watch_cdn_daily 0 left it on")
	}
}

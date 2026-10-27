package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestEdgePlaylist(t *testing.T) {
	master := "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",URI=\"2.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=1\n3.m3u8\n"
	media := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"4.ts\"\n#EXTINF:6,\n5.ts\n#EXTINF:6,\n6.ts\n#EXT-X-ENDLIST\n"
	edge := watchEdge{prefix: "/.proxy/cdn"}

	if out, n, _ := edgePlaylist([]byte(master), "k", watchEdge{}); string(out) != master || n != 0 {
		t.Fatalf("no edge changed the playlist: %q", out)
	}
	if out, n, _ := edgePlaylist([]byte(master), "k", edge); string(out) != master || n != 0 {
		t.Fatalf("master through the edge: %q %d", out, n)
	}
	out, n, secs := edgePlaylist([]byte(media), "k", edge)
	want := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"4.ts\"\n#EXTINF:6,\n/.proxy/cdn/activity/api/watch/media/k/5.ts\n#EXTINF:6,\n/.proxy/cdn/activity/api/watch/media/k/6.ts\n#EXT-X-ENDLIST\n"
	if string(out) != want || n != 2 || secs != 12 {
		t.Fatalf("segments through the edge: %d %q", n, out)
	}
	out, n, _ = edgePlaylist([]byte(master), "k", watchEdge{direct: true})
	if !strings.Contains(string(out), `URI="2.m3u8?direct=1"`) || !strings.Contains(string(out), "\n3.m3u8?direct=1\n") || n != 0 {
		t.Fatalf("direct carried on: %q", out)
	}
	if out, _, _ := edgePlaylist([]byte(media), "k", watchEdge{direct: true}); string(out) != media {
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
	add := func(n float64) {
		b.watch.mu.Lock()
		b.edgeAdd(n, watchNow())
		b.watch.mu.Unlock()
	}
	add(2)
	if e = b.watchEdgeFor(req); e.prefix == "" {
		t.Fatal("off before the budget")
	}
	add(2)
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

func TestEdgeJoinSegments(t *testing.T) {
	ts := func(b byte) []byte {
		d := make([]byte, 188*3)
		for k := 0; k < len(d); k += 188 {
			d[k], d[k+1] = 0x47, b
		}
		return d
	}
	var fetched atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetched.Add(1)
		w.Header().Set("Content-Type", "video/mp2t")
		if r.URL.Path == "/mp4.ts" {
			w.Write(append([]byte("\x00\x00\x00\x18ftypisom"), make([]byte, 600)...))
			return
		}
		w.Write(ts(r.URL.Path[1]))
	}))
	defer up.Close()
	m, _, _ := newWatchMedia(streamLink{URL: up.URL + "/x.m3u8", HLS: true}, false)
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-TARGETDURATION:6\n")
	for k, c := range "abcdefg" {
		if k == 5 {
			b.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		fmt.Fprintf(&b, "#EXTINF:5.5,\n%s\n", m.add(up.URL+"/"+string(c)+".ts", "seg"))
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	out := string(m.joinSegments([]byte(b.String())))

	// Three, then two up to the discontinuity, then two after it; the
	// target duration covers the longest.
	var lens []string
	var ids []string
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, "#EXTINF:"):
			lens = append(lens, l)
		case l != "" && !strings.HasPrefix(l, "#"):
			ids = append(ids, l)
		}
	}
	if strings.Join(lens, " ") != "#EXTINF:16.500, #EXTINF:11.000, #EXTINF:11.000," || !strings.Contains(out, "#EXT-X-TARGETDURATION:17\n") ||
		!strings.Contains(out, "\n#EXT-X-DISCONTINUITY\n#EXTINF:11.000,\n") || !strings.HasSuffix(out, "#EXT-X-ENDLIST\n") {
		t.Fatalf("joined:\n%s", out)
	}
	cache := newSegCache(1 << 20)
	get := func(id string) (int, []byte) {
		r := httptest.NewRequest("GET", "/x", nil)
		w := httptest.NewRecorder()
		m.serveVia(w, r, id, cache, watchEdge{})
		return w.Code, w.Body.Bytes()
	}
	code, data := get(ids[0])
	want := append(append(ts('a'), ts('b')...), ts('c')...)
	if code != 200 || !bytes.Equal(data, want) {
		t.Fatalf("first joined segment: %d, %d bytes", code, len(data))
	}
	// Each part comes from the segment cache: asking again fetches nothing.
	n := fetched.Load()
	get(ids[0])
	if fetched.Load() != n {
		t.Fatal("parts fetched again")
	}

	// Encrypted, byte-range, fMP4 or unfinished playlists stay as they are.
	for _, tag := range []string{"#EXT-X-KEY:METHOD=AES-128,URI=\"k\"\n", "#EXT-X-MAP:URI=\"i.mp4\"\n", "#EXT-X-BYTERANGE:100@0\n"} {
		in := strings.Replace(b.String(), "#EXT-X-TARGETDURATION:6\n", "#EXT-X-TARGETDURATION:6\n"+tag, 1)
		if got := string(m.joinSegments([]byte(in))); got != in {
			t.Errorf("joined with %q", tag)
		}
	}
	if in := strings.Replace(b.String(), "#EXT-X-ENDLIST\n", "", 1); string(m.joinSegments([]byte(in))) != in {
		t.Error("joined an unfinished playlist")
	}
	// A part that isn't transport stream fails the request, so the player
	// goes back to the bot.
	id := m.add("join:"+m.add(up.URL+"/a.ts", "seg")+","+m.add(up.URL+"/mp4.ts", "seg"), "join")
	if code, _ := get(id); code != http.StatusBadGateway {
		t.Fatalf("fMP4 part: %d", code)
	}
}

func TestEdgeCountsWhatPlays(t *testing.T) {
	b, now := watchBot(t, "https://cdn.example/ep.m3u8")
	r := &watchRoom{phase: "play", playing: true, media: &watchMedia{edgeSeg: 15},
		members: map[string]*watchMember{"a": {}, "b": {}}}
	b.watch.mu.Lock()
	defer b.watch.mu.Unlock()
	// Two people watching a minute of 15s segments: 8, plus the slack.
	b.edgeTick(r, 60, *now)
	if b.watch.edge.segs != 8*edgeSlack {
		t.Fatalf("a minute: %v", b.watch.edge.segs)
	}
	r.playing = false
	b.edgeTick(r, 60, *now)
	r.playing, r.media.edgeSeg = true, 0
	b.edgeTick(r, 60, *now)
	if b.watch.edge.segs != 8*edgeSlack {
		t.Fatalf("paused or off the edge still counted: %v", b.watch.edge.segs)
	}
}

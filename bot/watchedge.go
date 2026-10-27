package main

import (
	"bytes"
	"net/http"
	"strings"
	"time"
)

// The edge cache (watch_cdn, off when empty). Viewers fetch video segments
// through a Cloudflare Worker (deploy/edge-worker.js) on a Discord URL
// mapping instead of from the bot: the Worker keeps each segment in
// Cloudflare's cache and asks the bot only on a miss, so the bot sends a
// segment a few times however many are watching. Playlists, subtitles and
// whole files still come from the bot.
//
// The Worker runs on Cloudflare's free plan, which stops answering past
// 100,000 requests a day instead of billing. The bot counts the segments it
// hands out through the Worker per UTC day and goes back to serving them
// itself at /settings watch_cdn_daily, and a player that can't load a
// segment through the Worker reloads with ?direct=1, which keeps it on the
// bot.

// watchEdge is how one playlist request is answered.
type watchEdge struct {
	prefix string    // where segment addresses point; empty serves them from the bot
	direct bool      // the player asked for the bot: carry it to the playlists below
	count  func(int) // adds segments handed to the Worker to today's count
}

// edgeUse is the Worker's count for one UTC day.
type edgeUse struct {
	day  string
	segs int
}

// watchEdgeFor picks how r's playlist is answered.
func (b *bot) watchEdgeFor(r *http.Request) watchEdge {
	if r.URL.Query().Get("direct") == "1" {
		return watchEdge{direct: true}
	}
	prefix := strings.TrimRight(b.cfg.WatchCDN, "/")
	budget := b.settingInt("watch_cdn_daily")
	if prefix == "" || budget <= 0 {
		return watchEdge{}
	}
	day := watchNow().UTC().Format(time.DateOnly)
	b.watch.mu.Lock()
	defer b.watch.mu.Unlock()
	if b.watch.edge.day != day {
		b.watch.edge = edgeUse{day: day}
	}
	if b.watch.edge.segs >= budget {
		return watchEdge{}
	}
	return watchEdge{prefix: prefix, count: func(n int) {
		b.watch.mu.Lock()
		if b.watch.edge.day == day {
			b.watch.edge.segs += n
		}
		b.watch.mu.Unlock()
	}}
}

// edgePlaylist points a rewritten playlist's segments at the Worker, or
// carries ?direct=1 to its playlists. It returns how many segments now go
// through the Worker.
func edgePlaylist(body []byte, key string, e watchEdge) ([]byte, int) {
	if e.prefix == "" && !e.direct {
		return body, 0
	}
	lines := bytes.Split(body, []byte("\n"))
	n := 0
	for k, l := range lines {
		line := string(l)
		switch {
		case e.direct && strings.HasPrefix(line, "#EXT-X-MEDIA"):
			lines[k] = []byte(hlsURIAttr.ReplaceAllStringFunc(line, func(attr string) string {
				ref := hlsURIAttr.FindStringSubmatch(attr)[1]
				if !strings.HasSuffix(ref, ".m3u8") {
					return attr
				}
				return `URI="` + ref + `?direct=1"`
			}))
		case line == "" || strings.HasPrefix(line, "#"):
		case e.direct && strings.HasSuffix(line, ".m3u8"):
			lines[k] = []byte(line + "?direct=1")
		case e.prefix != "" && strings.HasSuffix(line, ".ts"):
			lines[k] = []byte(e.prefix + "/activity/" + watchMediaPath(key, line))
			n++
		}
	}
	return bytes.Join(lines, []byte("\n")), n
}

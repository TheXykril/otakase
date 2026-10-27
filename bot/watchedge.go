package main

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
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
// 100,000 requests a day instead of billing. Every segment a player fetches
// through it is one request, cached or not, so the bot does two things:
//
//   - It joins up to three segments into one (joinSegments), a third of the
//     requests for the same video. Transport stream segments play the same
//     joined end to end.
//   - It estimates the day's requests from what is actually played: each
//     member's seconds watched over the segment length (edgeTick). Counting
//     every segment in each playlist it hands out would charge a whole
//     episode for a viewer who joins and watches a minute.
//
// At /settings watch_cdn_daily it goes back to serving segments itself, and
// a player that can't load a segment through the Worker reloads with
// ?direct=1, which keeps it on the bot.

const (
	edgeJoinMax  = 3  // segments joined into one request
	edgeJoinSecs = 20 // longest a joined segment may play
	// edgeSlack covers requests beyond plain playing: the buffer filled
	// ahead on join and after each seek, and retries.
	edgeSlack = 1.25
)

// watchEdge is how one playlist request is answered.
type watchEdge struct {
	prefix string // where segment addresses point; empty serves them from the bot
	direct bool   // the player asked for the bot: carry it to the playlists below
}

// edgeUse is the Worker's estimated request count for one UTC day.
type edgeUse struct {
	day  string
	segs float64
}

// edgeAdd adds n requests to today's count. Hold b.watch.mu.
func (b *bot) edgeAdd(n float64, now time.Time) {
	day := now.UTC().Format(time.DateOnly)
	if b.watch.edge.day != day {
		b.watch.edge = edgeUse{day: day}
	}
	b.watch.edge.segs += n
}

// edgeTick counts the segments a room's members played through the edge
// cache in the last elapsed seconds. Hold b.watch.mu.
func (b *bot) edgeTick(r *watchRoom, elapsed float64, now time.Time) {
	if r.media == nil || elapsed <= 0 || r.phase != "play" || !r.playing {
		return
	}
	r.media.mu.Lock()
	seg := r.media.edgeSeg
	r.media.mu.Unlock()
	if seg <= 0 {
		return
	}
	b.edgeAdd(float64(len(r.members))*elapsed/seg*edgeSlack, now)
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
	b.watch.mu.Lock()
	defer b.watch.mu.Unlock()
	b.edgeAdd(0, watchNow())
	if b.watch.edge.segs >= float64(budget) {
		return watchEdge{}
	}
	return watchEdge{prefix: prefix}
}

// edgePlaylist points a rewritten playlist's segments at the Worker, or
// carries ?direct=1 to its playlists. It returns how many segments now go
// through the Worker and how long they play together.
func edgePlaylist(body []byte, key string, e watchEdge) ([]byte, int, float64) {
	if e.prefix == "" && !e.direct {
		return body, 0, 0
	}
	lines := bytes.Split(body, []byte("\n"))
	n, secs, dur := 0, 0.0, 0.0
	for k, l := range lines {
		line := string(l)
		switch {
		case strings.HasPrefix(line, "#EXTINF:"):
			dur = extinf(line)
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
			secs += dur
		}
	}
	return bytes.Join(lines, []byte("\n")), n, secs
}

// extinf reads a segment's length from its #EXTINF line, 0 when it has none.
func extinf(line string) float64 {
	v, _, _ := strings.Cut(strings.TrimPrefix(line, "#EXTINF:"), ",")
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || f < 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return f
}

// joinSegments joins runs of up to edgeJoinMax segments of a rewritten
// media playlist into one, each an id of its own. Only a finished playlist
// of plain transport stream segments is joined: encryption, byte ranges and
// fMP4 need each segment on its own, and tags between segments (a
// discontinuity, a date) end a run.
func (m *watchMedia) joinSegments(body []byte) []byte {
	text := string(body)
	if !strings.Contains(text, "#EXTINF:") || !strings.Contains(text, "#EXT-X-ENDLIST") ||
		strings.Contains(text, "#EXT-X-MAP") || strings.Contains(text, "#EXT-X-BYTERANGE") ||
		strings.Contains(text, "#EXT-X-KEY") && !strings.Contains(text, "METHOD=NONE") {
		return body
	}
	type seg struct {
		id  string
		dur float64
	}
	var out []string
	var run []seg
	longest := 0.0
	flush := func() {
		if len(run) == 0 {
			return
		}
		total, ids := 0.0, make([]string, len(run))
		for k, s := range run {
			total += s.dur
			ids[k] = s.id
		}
		id := run[0].id
		if len(run) > 1 {
			id = m.add("join:"+strings.Join(ids, ","), "join")
		}
		longest = math.Max(longest, total)
		out = append(out, "#EXTINF:"+strconv.FormatFloat(total, 'f', 3, 64)+",", id)
		run = run[:0]
	}
	dur, inf := 0.0, false
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "#EXTINF:"):
			dur, inf = extinf(line), true
		case line == "" || strings.HasPrefix(line, "#"):
			flush()
			out = append(out, line)
		case inf && strings.HasSuffix(line, ".ts"):
			total := dur
			for _, s := range run {
				total += s.dur
			}
			if len(run) == edgeJoinMax || len(run) > 0 && total > edgeJoinSecs {
				flush()
			}
			run = append(run, seg{line, dur})
			inf = false
		default:
			return body
		}
	}
	flush()
	joined := strings.Join(out, "\n") + "\n"
	// The target duration must cover the longest segment.
	return []byte(hlsTarget.ReplaceAllStringFunc(joined, func(tag string) string {
		if t, err := strconv.Atoi(hlsTarget.FindStringSubmatch(tag)[1]); err == nil && float64(t) >= math.Round(longest) {
			return tag
		}
		return "#EXT-X-TARGETDURATION:" + strconv.Itoa(int(math.Ceil(longest)))
	}))
}

var hlsTarget = regexp.MustCompile(`#EXT-X-TARGETDURATION:(\d+)`)

// joined serves a joined segment: its parts end to end, each from the
// segment cache. Parts that aren't transport stream can't be joined, so
// the request fails and the player goes back to the bot.
func (m *watchMedia) joined(u string, cache *segCache) ([]byte, error) {
	ids := strings.Split(strings.TrimPrefix(u, "join:"), ",")
	parts := make([][]byte, len(ids))
	errs := make([]error, len(ids))
	var wg sync.WaitGroup
	for k, id := range ids {
		pu, kind, ok := m.lookup(id)
		if !ok || kind != "seg" {
			return nil, fmt.Errorf("joined segment %s: unknown part %s", u, id)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			var typ string
			parts[k], typ, errs[k] = m.segment(pu, cache)
			if errs[k] == nil && typ != "video/mp2t" {
				errs[k] = fmt.Errorf("joined segment part %s is %s", id, typ)
			}
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	go m.prefetch(ids[len(ids)-1], cache)
	return bytes.Join(parts, nil), nil
}

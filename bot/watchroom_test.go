package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

// watchBot is a bot with a fixed clock, a fake voice channel, a fake
// source and no background clock.
func watchBot(t *testing.T, streamURL string) (*bot, *time.Time) {
	t.Helper()
	b := testGamesBot(t)
	b.cfg.GuildID = "g"
	now := time.Date(2026, 10, 23, 20, 0, 0, 0, time.UTC)
	oldNow, oldCh, oldClock, oldResolve, oldShow := watchNow, watchChannel, watchClock, watchResolve, watchShowByID
	oldAdmin, oldAhead := isGameAdmin, watchAhead
	watchAhead = func(func()) {}
	watchNow = func() time.Time { return now }
	watchChannel = func(_ *bot, id string) *discordgo.Channel {
		switch id {
		case "vc":
			return &discordgo.Channel{ID: id, GuildID: "g", Name: "lounge"}
		case "nsfw":
			return &discordgo.Channel{ID: id, GuildID: "g", Name: "late", NSFW: true}
		}
		return nil
	}
	watchClock = func(*bot, *watchRoom) {}
	watchResolve = func(_ context.Context, _ string, id, ep int, dub, adult bool) (streamInfo, error) {
		return streamInfo{AnilistID: id, Episode: ep, Streams: []streamLink{{URL: streamURL,
			Subtitles: []streamSubtitle{{URL: streamURL + "/subs.srt", Language: "en", Label: "English"}}}}}, nil
	}
	watchShowByID = func(id int) (watchShow, error) {
		return watchShow{ID: id, MalID: id + 1000, Title: fmt.Sprintf("Show %d", id), Episodes: 12, Adult: id == 99}, nil
	}
	isGameAdmin = func(_ *bot, user string) bool { return user == "staff" }
	gameAdmins.Lock()
	gameAdmins.m = nil
	gameAdmins.Unlock()
	t.Cleanup(func() {
		watchNow, watchChannel, watchClock, watchResolve, watchShowByID = oldNow, oldCh, oldClock, oldResolve, oldShow
		isGameAdmin, watchAhead = oldAdmin, oldAhead
	})
	return b, &now
}

func watchPost(b *bot, fn func(*bot, http.ResponseWriter, *http.Request), user string, in watchIn, out any) int {
	return togetherPost(b, fn, user, in, out)
}

// waitLoaded waits for the episode lookup that runs on its own.
func waitLoaded(t *testing.T, b *bot) {
	t.Helper()
	for k := 0; k < 200; k++ {
		b.watch.mu.Lock()
		phase := b.watch.cur.phase
		b.watch.mu.Unlock()
		if phase != "loading" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("episode never loaded")
}

func TestWatchRoomTicketsAndOneAtATime(t *testing.T) {
	b, now := watchBot(t, "https://cdn.example/ep.m3u8")
	var v watchView
	if code := watchPost(b, (*bot).watchOpen, "a", watchIn{Room: "r1", Channel: "vc"}, nil); code != http.StatusPaymentRequired {
		t.Fatalf("no ticket: %d", code)
	}
	if code := watchPost(b, (*bot).watchOpen, "staff", watchIn{Room: "r1", Channel: "vc"}, nil); code != http.StatusPaymentRequired {
		t.Fatalf("staff need a ticket too: %d", code)
	}
	b.store.update(func(d *storeData) { d.WatchTickets = map[string]int{"a": 1, "z": 1} })
	if code := watchPost(b, (*bot).watchOpen, "a", watchIn{Room: "r1", Channel: "nowhere"}, nil); code != http.StatusBadRequest {
		t.Fatalf("not a server voice channel: %d", code)
	}
	if code := watchPost(b, (*bot).watchOpen, "a", watchIn{Room: "r1", Channel: "vc"}, &v); code != 200 || !v.IsHost || v.Phase != "pick" {
		t.Fatalf("open: %d %+v", code, v)
	}
	// Opening doesn't spend the ticket yet.
	if b.watchTickets("a") != 1 {
		t.Fatal("ticket spent before anything played")
	}
	// Another call can't open a second room.
	if code := watchPost(b, (*bot).watchOpen, "z", watchIn{Room: "r2", Channel: "vc"}, nil); code != http.StatusConflict {
		t.Fatalf("second room: %d", code)
	}
	*now = now.Add(time.Second)
	if code := watchPost(b, (*bot).watchOpen, "b", watchIn{Room: "r1", Channel: "vc"}, &v); code != 200 || v.IsHost || len(v.Members) != 2 {
		t.Fatalf("join: %d %+v", code, v)
	}
	if code := watchPost(b, (*bot).watchPick, "b", watchIn{Room: "r1", Show: 5, Ep: 1}, nil); code != http.StatusForbidden {
		t.Fatalf("only the host picks: %d", code)
	}
	if code := watchPost(b, (*bot).watchPick, "a", watchIn{Room: "r1", Show: 99, Ep: 1}, nil); code != http.StatusForbidden {
		t.Fatalf("18+ outside an age-restricted channel: %d", code)
	}
	if code := watchPost(b, (*bot).watchPick, "a", watchIn{Room: "r1", Show: 5, Ep: 13}, nil); code != http.StatusBadRequest {
		t.Fatalf("episode past the end: %d", code)
	}
	if code := watchPost(b, (*bot).watchPick, "a", watchIn{Room: "r1", Show: 5, Ep: 3}, &v); code != 200 || v.Phase != "loading" {
		t.Fatalf("pick: %d %+v", code, v)
	}
	waitLoaded(t, b)
	b.watch.mu.Lock()
	v = b.watchViewOf(b.watch.cur, "b", false, watchNow())
	b.watch.mu.Unlock()
	if v.Phase != "play" || !v.Playing || !v.HLS || !strings.HasPrefix(v.Media, "api/watch/media/") || len(v.Subs) != 1 || v.Ends == 0 {
		t.Fatalf("playing: %+v", v)
	}
	if b.watchTickets("a") != 0 {
		t.Fatal("the ticket is spent once the first episode plays")
	}
	// The room is full at ten.
	for k := 0; k < b.watchMax()-2; k++ {
		watchPost(b, (*bot).watchOpen, fmt.Sprint("m", k), watchIn{Room: "r1", Channel: "vc"}, nil)
	}
	if code := watchPost(b, (*bot).watchOpen, "late", watchIn{Room: "r1", Channel: "vc"}, nil); code != http.StatusForbidden {
		t.Fatalf("eleventh person: %d", code)
	}
	// Four hours after it started the room closes.
	*now = now.Add(watchRoomLength + time.Minute)
	for _, id := range append([]string{"a", "b"}, "m0") {
		b.watch.cur.members[id].seen = *now
	}
	if b.watchStep(b.watch.cur, *now) || b.watch.cur != nil {
		t.Fatal("room still open after four hours")
	}
	var logs []watchRoomLog
	for k := 0; k < 100 && len(logs) == 0; k++ {
		b.store.view(func(d *storeData) { logs = d.WatchRooms })
		time.Sleep(5 * time.Millisecond)
	}
	if len(logs) != 1 || logs[0].Episodes != 1 || logs[0].Owner != "a" {
		t.Fatalf("room log %+v", logs)
	}
	// Now another call can open one.
	if code := watchPost(b, (*bot).watchOpen, "z", watchIn{Room: "r2", Channel: "nsfw"}, &v); code != 200 || !v.Adult {
		t.Fatalf("after: %d %+v", code, v)
	}
}

func TestWatchRoomControlsAndRequests(t *testing.T) {
	b, now := watchBot(t, "https://cdn.example/ep.mp4")
	b.store.update(func(d *storeData) { d.WatchTickets = map[string]int{"a": 1} })
	watchPost(b, (*bot).watchOpen, "a", watchIn{Room: "r", Channel: "vc"}, nil)
	*now = now.Add(time.Second)
	watchPost(b, (*bot).watchOpen, "b", watchIn{Room: "r", Channel: "vc"}, nil)
	*now = now.Add(time.Second)
	watchPost(b, (*bot).watchOpen, "staff", watchIn{Room: "r", Channel: "vc"}, nil)
	watchPost(b, (*bot).watchPick, "a", watchIn{Room: "r", Show: 5, Ep: 1}, nil)
	waitLoaded(t, b)
	r := b.watch.cur

	// The host's page reports where it is; the room follows it.
	watchPost(b, (*bot).watchBeat, "a", watchIn{Room: "r", Pos: 100, Playing: true, Dur: 1400}, nil)
	*now = now.Add(10 * time.Second)
	if p := r.position(*now); p < 109.9 || p > 110.1 || r.dur != 1400 {
		t.Fatalf("position %v dur %v", p, r.dur)
	}
	// Members can't control, only ask.
	if code := watchPost(b, (*bot).watchCtl, "b", watchIn{Room: "r", Act: "pause"}, nil); code != http.StatusForbidden {
		t.Fatalf("member control: %d", code)
	}
	var v watchView
	if code := watchPost(b, (*bot).watchAskAPI, "b", watchIn{Room: "r", Kind: "pause"}, &v); code != 200 || len(v.Asks) != 1 {
		t.Fatalf("ask: %d %+v", code, v)
	}
	if code := watchPost(b, (*bot).watchAskAPI, "b", watchIn{Room: "r", Kind: "skip"}, nil); code != http.StatusTooManyRequests {
		t.Fatalf("second ask while one waits: %d", code)
	}
	if code := watchPost(b, (*bot).watchAskAPI, "a", watchIn{Room: "r", Kind: "pause"}, nil); code != http.StatusBadRequest {
		t.Fatalf("host asking: %d", code)
	}
	watchPost(b, (*bot).watchState, "a", watchIn{}, nil)
	b.watch.mu.Lock()
	v = b.watchViewOf(r, "a", false, *now)
	b.watch.mu.Unlock()
	if len(v.Asks) != 1 || v.Asks[0].Kind != "pause" {
		t.Fatalf("host sees %+v", v.Asks)
	}
	if code := watchPost(b, (*bot).watchAnswer, "a", watchIn{Room: "r", ID: v.Asks[0].ID, OK: true}, &v); code != 200 || v.Playing || len(v.Asks) != 0 {
		t.Fatalf("answer: %d %+v", code, v)
	}
	// Skip the opening jumps ahead from where it is.
	watchPost(b, (*bot).watchCtl, "a", watchIn{Room: "r", Act: "skip"}, &v)
	if v.Pos < 194 || v.Pos > 196 {
		t.Fatalf("skip to %v", v.Pos)
	}
	// Staff take over; the old host is now a member.
	if code := watchPost(b, (*bot).watchTake, "b", watchIn{Room: "r"}, nil); code != http.StatusForbidden {
		t.Fatalf("member take: %d", code)
	}
	watchPost(b, (*bot).watchTake, "staff", watchIn{Room: "r"}, &v)
	if v.Host != "staff" {
		t.Fatalf("host %q", v.Host)
	}
	// The host goes quiet: the longest in the room takes over.
	*now = now.Add(watchGone + time.Second)
	r.members["a"].seen, r.members["b"].seen = *now, *now
	b.watch.mu.Lock()
	r.prune(*now)
	b.watch.mu.Unlock()
	if r.host != "a" {
		t.Fatalf("after staff left host is %q", r.host)
	}
	// Next episode.
	watchPost(b, (*bot).watchCtl, "a", watchIn{Room: "r", Act: "next"}, &v)
	waitLoaded(t, b)
	if r.ep != 2 || r.phase != "play" {
		t.Fatalf("next: ep %d %s", r.ep, r.phase)
	}
	// A link that won't play moves on; with none left it says so.
	var bv watchView
	watchPost(b, (*bot).watchCtl, "a", watchIn{Room: "r", Act: "broken"}, &bv)
	if bv.Phase != "error" || bv.Media != "" {
		t.Fatalf("broken: %+v", v)
	}
}

func TestWatchRoomCountsWatchers(t *testing.T) {
	b, now := watchBot(t, "https://cdn.example/ep.mp4")
	b.store.update(func(d *storeData) {
		d.WatchTickets = map[string]int{"a": 1}
		d.AniList = map[string]anilistLink{"a": {ID: 1, Verified: true, Token: "ta"}, "b": {ID: 2, Verified: true, Token: "tb"}}
		d.WatchNoSync = map[string]bool{"b": true}
	})
	var mu sync.Mutex
	var got []string
	old := anilistProgress
	anilistProgress = func(token string, show watchShow, ep int) (bool, error) {
		mu.Lock()
		got = append(got, fmt.Sprintf("%s:%d:%d", token, show.ID, ep))
		mu.Unlock()
		return true, nil
	}
	t.Cleanup(func() { anilistProgress = old })

	watchPost(b, (*bot).watchOpen, "a", watchIn{Room: "r", Channel: "vc"}, nil)
	watchPost(b, (*bot).watchOpen, "b", watchIn{Room: "r", Channel: "vc"}, nil)
	watchPost(b, (*bot).watchPick, "a", watchIn{Room: "r", Show: 7, Ep: 4}, nil)
	waitLoaded(t, b)
	r := b.watch.cur
	watchPost(b, (*bot).watchBeat, "a", watchIn{Room: "r", Pos: 0, Playing: true, Dur: 100}, nil)
	for k := 0; k < 45; k++ {
		*now = now.Add(watchTick)
		for _, m := range r.members {
			m.seen = *now
		}
		b.watchStep(r, *now)
	}
	for k := 0; k < 100; k++ {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0] != "ta:7:4" {
		t.Fatalf("progress updates %v (b switched it off)", got)
	}
}

func TestTrimMaster(t *testing.T) {
	master := `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="a",NAME="English",LANGUAGE="eng",URI="en/p.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="a",NAME="Japanese",DEFAULT=YES,LANGUAGE="jpn",URI="ja/p.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=900000,RESOLUTION=640x360,AUDIO="a"
360/p.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080,AUDIO="a"
1080/p.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2500000,RESOLUTION=1280x720,AUDIO="a"
720/p.m3u8
#EXT-X-I-FRAME-STREAM-INF:BANDWIDTH=100000,URI="iframe.m3u8"
`
	out := trimMaster(master, 720, false)
	if !strings.Contains(out, "720/p.m3u8") || strings.Contains(out, "1080/") || strings.Contains(out, "360/") ||
		!strings.Contains(out, `NAME="Japanese"`) || !strings.Contains(out, `NAME="English"`) || strings.Contains(out, "iframe") {
		t.Fatalf("trimmed:\n%s", out)
	}
	// A dub prefers English while keeping Japanese available to other viewers.
	if out := trimMaster(master, 720, true); !strings.Contains(out, `NAME="English",LANGUAGE="eng",URI="en/p.m3u8",DEFAULT=YES`) || !strings.Contains(out, "Japanese") {
		t.Fatalf("dub:\n%s", out)
	}
	// Every variant over the cap: the smallest stays.
	out = trimMaster("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080\nhi.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=3000000,RESOLUTION=1280x1080\nlo.m3u8\n", 720, false)
	if !strings.Contains(out, "lo.m3u8") || strings.Contains(out, "hi.m3u8") {
		t.Fatalf("over the cap:\n%s", out)
	}
}

func TestWatchRoomLanguageReleases(t *testing.T) {
	b := &bot{}
	main := "https://video.example/en/playlist.m3u8"
	room := &watchRoom{
		spent: true,
		dub: true,
		streams: []streamLink{{URL: main}},
		audioReleases: map[string]streamLink{
			"en": {URL: main},
			"ja": {URL: "https://video.example/ja/playlist.m3u8"},
			"de": {URL: "https://video.example/de/playlist.m3u8"},
		},
	}
	if !b.startStream(room, 0) {
		t.Fatal("episode did not start")
	}
	for _, lang := range []string{"dub", "ja", "de", "sub"} {
		if room.audioEntries[lang] == "" { t.Fatalf("missing %s audio playlist", lang) }
	}
	if len(room.extraAudioMedia) != 2 {
		t.Fatalf("expected Japanese and German relays, got %d", len(room.extraAudioMedia))
	}
	if room.audioEntries["sub"] != room.audioEntries["ja"] {
		t.Fatal("legacy sub alias did not follow Japanese release")
	}
	if room.audioEntries["dub"] == room.audioEntries["ja"] {
		t.Fatal("separate audio release reused current playlist")
	}
	room.finishMedia()
	if len(room.extraAudioMedia) > 0 {
		t.Fatal("old language media remained after episode finished")
	}
}

func TestWatchAudioLanguage(t *testing.T) {
	for _, lang := range []string{"ja", "en", "de", "sub", "dub", "track-7"} {
		if !watchAudioLanguage(lang) { t.Errorf("rejected %q", lang) }
	}
	for _, lang := range []string{"", "ja/../../x", "EN", "track-9999", "track-abc", "en-us"} {
		if watchAudioLanguage(lang) { t.Errorf("accepted invalid route %q", lang) }
	}
}

func TestTrimMasterKeepsAudioFromOtherGroups(t *testing.T) {
	master := `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="primary",NAME="English",LANGUAGE="en",DEFAULT=YES,URI="en/audio.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="alternate",NAME="Japanese",LANGUAGE="ja",URI="ja/audio.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="alternate",NAME="German",LANGUAGE="de",URI="de/audio.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="alternate",NAME="English Duplicate",LANGUAGE="en",URI="en/other.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=1200000,RESOLUTION=1280x720,AUDIO="primary"
video/main.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=3000000,RESOLUTION=1920x1080,AUDIO="alternate"
video/hd.m3u8
`
	out := trimMaster(master, 720, false)
	for _, path := range []string{"en/audio.m3u8", "ja/audio.m3u8", "de/audio.m3u8", "video/main.m3u8"} {
		if !strings.Contains(out, path) {
			t.Fatalf("audio rendition %q was dropped:\n%s", path, out)
		}
	}
	if strings.Contains(out, "en/other.m3u8") || strings.Contains(out, "video/hd.m3u8") ||
		strings.Contains(out, `GROUP-ID="alternate"`) {
		t.Fatalf("master contains duplicate language or incorrect audio group:\n%s", out)
	}
}

func TestToWebVTT(t *testing.T) {
	srt := "1\r\n00:00:01,500 --> 00:00:03,000\r\nHello\r\n"
	if got := toWebVTT(srt); !strings.HasPrefix(got, "WEBVTT") || !strings.Contains(got, "00:00:01.500 --> 00:00:03.000") {
		t.Errorf("srt: %q", got)
	}
	ass := "[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n" +
		"Dialogue: 0,0:01:02.30,0:01:04.05,Default,,0,0,0,,{\\i1}Hi,\\Nthere\n"
	if got := toWebVTT(ass); !strings.Contains(got, "00:01:02.300 --> 00:01:04.050\nHi,\nthere") {
		t.Errorf("ass: %q", got)
	}
}

// TestWatchRelay serves a playlist and its segments through the relay: one
// fetch per segment however many ask, and nothing the playlist didn't name.
func TestWatchRelay(t *testing.T) {
	var hits sync.Map
	var refs atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := hits.LoadOrStore(r.URL.Path, new(atomic.Int32))
		n.(*atomic.Int32).Add(1)
		if r.Header.Get("Referer") == "https://site.example/" {
			refs.Add(1)
		}
		switch r.URL.Path {
		case "/master.m3u8":
			io.WriteString(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=2000000,RESOLUTION=1280x720\nv/720.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=6000000,RESOLUTION=1920x1080\nv/1080.m3u8\n")
		case "/v/720.m3u8":
			io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXT-X-KEY:METHOD=AES-128,URI=\"../key.bin\"\n#EXTINF:4,\nseg0.ts\n#EXTINF:4,\nhttps://other.example/seg1.ts\n#EXT-X-ENDLIST\n")
		case "/v/seg0.ts":
			w.Header().Set("Content-Type", "video/mp2t")
			io.WriteString(w, "SEGMENT0")
		case "/key.bin":
			io.WriteString(w, "0123456789abcdef")
		default:
			http.NotFound(w, r)
		}
	}))
	defer up.Close()

	b, _ := watchBot(t, up.URL+"/master.m3u8")
	b.store.update(func(d *storeData) { d.WatchTickets = map[string]int{"a": 1} })
	watchPost(b, (*bot).watchOpen, "a", watchIn{Room: "r", Channel: "vc"}, nil)
	watchPost(b, (*bot).watchPick, "a", watchIn{Room: "r", Show: 1, Ep: 1}, nil)
	waitLoaded(t, b)
	b.watch.mu.Lock()
	b.watch.cur.media.link.Referrer = "https://site.example/"
	media := b.watch.cur.media
	entry := b.watch.cur.entry
	b.watch.mu.Unlock()

	get := func(id string) (int, string) {
		r := httptest.NewRequest("GET", "/x", nil)
		r.SetPathValue("key", media.key)
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		b.watchMediaAPI(w, r)
		return w.Code, w.Body.String()
	}
	code, master := get(entry)
	if code != 200 || strings.Contains(master, "1080") || strings.Contains(master, "http") {
		t.Fatalf("master %d:\n%s", code, master)
	}
	variant := strings.TrimSpace(master[strings.LastIndex(strings.TrimSpace(master), "\n")+1:])
	code, list := get(variant)
	if code != 200 || strings.Contains(list, "http") || !strings.Contains(list, `URI="`) {
		t.Fatalf("variant %d:\n%s", code, list)
	}
	var segs []string
	for _, l := range strings.Split(list, "\n") {
		if l != "" && !strings.HasPrefix(l, "#") {
			segs = append(segs, l)
		}
	}
	var wg sync.WaitGroup
	for k := 0; k < 5; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if code, body := get(segs[0]); code != 200 || body != "SEGMENT0" {
				t.Errorf("segment %d %q", code, body)
			}
		}()
	}
	wg.Wait()
	if n, _ := hits.Load("/v/seg0.ts"); n.(*atomic.Int32).Load() != 1 {
		t.Errorf("segment fetched %d times", n.(*atomic.Int32).Load())
	}
	if refs.Load() == 0 {
		t.Error("the link's referrer wasn't sent")
	}
	if code, _ := get("999.ts"); code != http.StatusNotFound {
		t.Errorf("unknown id: %d", code)
	}
	r := httptest.NewRequest("GET", "/x", nil)
	r.SetPathValue("key", "wrong")
	r.SetPathValue("id", entry)
	w := httptest.NewRecorder()
	b.watchMediaAPI(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("wrong key: %d", w.Code)
	}
	if _, err := url.Parse(entry); err != nil {
		t.Error(err)
	}
}

func TestUnmaskSegment(t *testing.T) {
	ts := make([]byte, 188*4)
	for k := 0; k < len(ts); k += 188 {
		ts[k] = 0x47
	}
	if d, typ := unmaskSegment(ts, "image/jpeg"); typ != "video/mp2t" || len(d) != len(ts) {
		t.Errorf("plain ts as jpeg: %s %d", typ, len(d))
	}
	masked := append([]byte("\x89PNG\r\n\x1a\nIHDR fake picture"), ts...)
	if d, typ := unmaskSegment(masked, "image/png"); typ != "video/mp2t" || len(d) != len(ts) {
		t.Errorf("ts behind a png header: %s %d", typ, len(d))
	}
	if _, typ := unmaskSegment([]byte("not video"), "video/mp4"); typ != "video/mp4" {
		t.Errorf("left alone: %s", typ)
	}
}

func TestParseStreamInfo(t *testing.T) {
	out := "Config file not found. Creating default config...\n{\"anilist_id\":1,\"episode\":2,\"provider\":\"x\",\"mode\":\"sub\",\"streams\":[{\"url\":\"https://a/b.m3u8\"}]}\n"
	info, err := parseStreamInfo(out)
	if err != nil || info.Episode != 2 || info.Streams[0].URL != "https://a/b.m3u8" {
		t.Fatalf("%+v %v", info, err)
	}
	if _, err := parseStreamInfo("nope\n"); err == nil {
		t.Error("no JSON should fail")
	}
}

func TestParseStreamInfoAudioVariants(t *testing.T) {
	info, err := parseStreamInfo(`{"anilist_id":1,"episode":2,"provider":"kickassanime","mode":"dub","streams":[{"url":"https://a/en.m3u8"}],"audio_variants":{"de":{"url":"https://a/de.m3u8","referrer":"https://kaa.lt/"}}}`)
	if err != nil || info.AudioVariants["de"].URL != "https://a/de.m3u8" ||
		info.AudioVariants["de"].Referrer != "https://kaa.lt/" {
		t.Fatalf("missing language release metadata: %+v, %v", info, err)
	}
}

func TestWatchTorrentOnlyStreamInfo(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef01234567"
	info, err := parseStreamInfo(`{"anilist_id":123,"episode":1,"mode":"sub","streams":[],"torrents":[{"info_hash":"` + hash + `"}]}`)
	if err != nil || len(info.Streams) != 0 || len(info.Torrents) != 1 || info.Torrents[0].InfoHash != hash {
		t.Fatalf("torrent-only info: %+v, %v", info, err)
	}
	if _, err := parseStreamInfo(`{"anilist_id":123,"episode":1,"streams":[]}`); err == nil {
		t.Fatal("source-free episode should fail")
	}
}

func TestWatchTorrentRejectsBadHash(t *testing.T) {
	if _, _, err := startWatchTorrent(context.Background(), "/nonexistent-cli", "not-a-hash", "sub"); err == nil {
		t.Fatal("invalid torrent hash should fail before launching helper")
	}
}

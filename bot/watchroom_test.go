package main

import (
	"context"
	"encoding/json"
	"errors"
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
	oldAdmin, oldAhead, oldSkipLookup := isGameAdmin, watchAhead, watchSkipLookup
	watchAhead = func(func()) {}
	watchSkipLookup = func(context.Context, int, int) (watchSkipIntervals, error) { return watchSkipIntervals{}, nil }
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
		isGameAdmin, watchAhead, watchSkipLookup = oldAdmin, oldAhead, oldSkipLookup
	})
	// Runs first: stop the room's lookups before the fakes above go back.
	t.Cleanup(func() {
		b.watch.mu.Lock()
		for _, r := range b.watch.rooms {
			r.finishMedia()
		}
		b.watch.mu.Unlock()
		b.watch.bg.Wait()
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
		phase := b.watch.only().phase
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
	// Another call can't open a room past watch_rooms_max.
	b.store.update(func(d *storeData) { d.Settings = map[string]string{"watch_rooms_max": "1"} })
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
	watchAllReady(b)
	b.watch.mu.Lock()
	v = b.watchViewOf(b.watch.only(), "b", false, watchNow())
	b.watch.mu.Unlock()
	if v.Phase != "play" || !v.Playing || !v.HLS || !strings.HasPrefix(v.Media, "api/watch/media/") || len(v.Subs) != 1 || v.Ends == 0 {
		t.Fatalf("playing: %+v", v)
	}
	if b.watchTickets("a") != 0 {
		t.Fatal("the ticket is spent once the first episode plays")
	}
	// The room is full at ten.
	for k := 0; k < b.watchMax("")-2; k++ {
		watchPost(b, (*bot).watchOpen, fmt.Sprint("m", k), watchIn{Room: "r1", Channel: "vc"}, nil)
	}
	if code := watchPost(b, (*bot).watchOpen, "late", watchIn{Room: "r1", Channel: "vc"}, nil); code != http.StatusForbidden {
		t.Fatalf("eleventh person: %d", code)
	}
	// Four hours after it started the room closes.
	*now = now.Add(watchRoomLength + time.Minute)
	for _, id := range append([]string{"a", "b"}, "m0") {
		b.watch.only().members[id].seen = *now
	}
	if b.watchStep(b.watch.only(), *now) || b.watch.only() != nil {
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
	watchAllReady(b)
	r := b.watch.only()

	// The host's page reports where it is; the room follows it.
	watchPost(b, (*bot).watchBeat, "a", watchIn{Room: "r", Pos: 100, Playing: true, Dur: 1400}, nil)
	*now = now.Add(10 * time.Second)
	if p := r.position(*now); p < 109.9 || p > 110.1 || r.dur != 1400 {
		t.Fatalf("position %v dur %v", p, r.dur)
	}
	// A host player still loading after a jump keeps the room playing, and
	// a seek right after it doesn't pause the room either.
	watchPost(b, (*bot).watchBeat, "a", watchIn{Room: "r", Pos: 300, Playing: true, Buffering: true, Dur: 1400}, nil)
	if !r.playing || !r.members["a"].stuck {
		t.Fatalf("buffering host: playing %v stuck %v", r.playing, r.members["a"].stuck)
	}
	watchPost(b, (*bot).watchCtl, "a", watchIn{Room: "r", Act: "seek", Pos: 320}, nil)
	if r.hold == 0 || r.holdPos != 320 {
		t.Fatalf("seek while buffering should wait to play: hold %d at %v", r.hold, r.holdPos)
	}
	watchAllReady(b)
	if !r.playing {
		t.Fatal("room stayed paused after the seek")
	}
	watchPost(b, (*bot).watchBeat, "a", watchIn{Room: "r", Pos: 100, Playing: true, Dur: 1400}, nil)
	*now = now.Add(10 * time.Second)

	// Members can't control, only ask.
	if code := watchPost(b, (*bot).watchCtl, "b", watchIn{Room: "r", Act: "pause"}, nil); code != http.StatusForbidden {
		t.Fatalf("member control: %d", code)
	}
	var v watchView
	if code := watchPost(b, (*bot).watchAskAPI, "b", watchIn{Room: "r", Kind: "pause"}, &v); code != 200 || len(v.Asks) != 1 {
		t.Fatalf("ask: %d %+v", code, v)
	}
	if code := watchPost(b, (*bot).watchAskAPI, "b", watchIn{Room: "r", Kind: "forward"}, nil); code != http.StatusTooManyRequests {
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
	// The host's pop-up counts down to when the request lapses.
	b.watch.mu.Lock()
	later := b.watchViewOf(r, "a", false, now.Add(10*time.Second))
	b.watch.mu.Unlock()
	if v.Asks[0].Left != watchAskTTL.Milliseconds() || later.Asks[0].Left != (watchAskTTL-10*time.Second).Milliseconds() {
		t.Fatalf("time left: %d then %d", v.Asks[0].Left, later.Asks[0].Left)
	}
	if code := watchPost(b, (*bot).watchAnswer, "a", watchIn{Room: "r", ID: v.Asks[0].ID, OK: true}, &v); code != 200 || v.Playing || len(v.Asks) != 0 {
		t.Fatalf("answer: %d %+v", code, v)
	}
	// Manual forward seeks ten seconds, independently of opening timestamps.
	watchPost(b, (*bot).watchCtl, "a", watchIn{Room: "r", Act: "forward"}, &v)
	if v.Pos < 119.9 || v.Pos > 120.1 {
		t.Fatalf("+10s jumped to %v", v.Pos)
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
	r := b.watch.only()
	// Both players load the episode, and the ready check lets it play.
	hold := r.hold
	watchPost(b, (*bot).watchReady, "a", watchIn{Room: "r", ID: hold}, nil)
	watchPost(b, (*bot).watchReady, "b", watchIn{Room: "r", ID: hold}, nil)
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
	// 720p plays, 360p stays as a step down for slow connections, 1080p goes.
	if !strings.Contains(out, "720/p.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=900000,RESOLUTION=640x360,AUDIO=\"a\"\n360/p.m3u8") || strings.Contains(out, "1080/") ||
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
		spent:   true,
		dub:     true,
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
		if room.audioEntries[lang] == "" {
			t.Fatalf("missing %s audio playlist", lang)
		}
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

func TestWatchRoomAddsLanguagesAfterPlaybackStarts(t *testing.T) {
	main := "https://video.example/en/playlist.m3u8"
	r := &watchRoom{spent: true, gen: 1, dub: true, provider: "kickassanime",
		streams: []streamLink{{URL: main}}}
	b := &bot{}
	b.watch.add(r)
	if !b.startStream(r, 0) {
		t.Fatal("episode did not start")
	}
	old := watchResolveAudio
	watchResolveAudio = func(_ context.Context, _ string, _, _ int, _, _ bool) (streamInfo, error) {
		return streamInfo{Provider: "kickassanime", AudioVariants: map[string]streamLink{
			"ja": {URL: "https://video.example/ja/playlist.m3u8"},
			"de": {URL: "https://video.example/de/playlist.m3u8"},
		}}, nil
	}
	t.Cleanup(func() { watchResolveAudio = old })
	b.findWatchLanguages(context.Background(), r, 1, "", 1, 1, false, true, "")
	for _, lang := range []string{"ja", "de", "sub", "dub"} {
		if r.audioEntries[lang] == "" {
			t.Fatalf("missing %s after background discovery", lang)
		}
	}
	if len(r.extraAudioMedia) != 2 {
		t.Fatalf("got %d language relays", len(r.extraAudioMedia))
	}
	previous := r.audioEntries["de"]
	r.gen++
	r.audioEntries = map[string]string{}
	b.findWatchLanguages(context.Background(), r, 1, "", 1, 1, false, true, "")
	if r.audioEntries["de"] != "" || previous == "" {
		t.Fatal("stale language lookup modified a new episode")
	}
}

func TestTrimMasterPreservesAllMultilingualTracks(t *testing.T) {
	// The selected KickAssAnime episode uses ten renditions in one HLS group.
	master := `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="stereo",NAME="Japanese",DEFAULT=YES,LANGUAGE="jpn",URI="ja/playlist.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="stereo",NAME="English",LANGUAGE="eng",URI="en/playlist.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="stereo",NAME="German",LANGUAGE="ger",URI="de/playlist.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="stereo",NAME="Spanish",LANGUAGE="spa",URI="es/playlist.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="stereo",NAME="French",LANGUAGE="fre",URI="fr/playlist.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="stereo",NAME="Hindi",LANGUAGE="hin",URI="hi/playlist.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="stereo",NAME="Italian",LANGUAGE="ita",URI="it/playlist.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="stereo",NAME="Portuguese",LANGUAGE="por",URI="pt/playlist.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="stereo",NAME="Tamil",LANGUAGE="tam",URI="ta/playlist.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="stereo",NAME="Telugu",LANGUAGE="tel",URI="te/playlist.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=3900000,RESOLUTION=1280x720,AUDIO="stereo"
720/playlist.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=7500000,RESOLUTION=1920x1080,AUDIO="stereo"
1080/playlist.m3u8
`
	for _, dub := range []bool{false, true} {
		out := trimMaster(master, 720, dub)
		if strings.Count(out, "#EXT-X-MEDIA:TYPE=AUDIO") != 10 || !strings.Contains(out, "720/playlist.m3u8") {
			t.Fatalf("lost HLS renditions: %s", out)
		}
		want := `NAME="Japanese",DEFAULT=YES`
		if dub {
			want = `NAME="English",LANGUAGE="eng",URI="en/playlist.m3u8",DEFAULT=YES`
		}
		if !strings.Contains(out, want) {
			t.Fatalf("wrong default audio: %s", out)
		}
	}
}

func TestWatchAudioLanguage(t *testing.T) {
	for _, lang := range []string{"ja", "en", "de", "sub", "dub", "track-7"} {
		if !watchAudioLanguage(lang) {
			t.Errorf("rejected %q", lang)
		}
	}
	for _, lang := range []string{"", "ja/../../x", "EN", "track-9999", "track-abc", "en-us"} {
		if watchAudioLanguage(lang) {
			t.Errorf("accepted invalid route %q", lang)
		}
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
	b.watch.bg.Wait() // the audio probe reads the link being changed below
	b.watch.mu.Lock()
	b.watch.only().media.link.Referrer = "https://site.example/"
	media := b.watch.only().media
	entry := b.watch.only().entry
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

	// With the edge cache on, segments point at it; ?direct=1 keeps them
	// on the bot, cached playlist or not.
	b.cfg.WatchCDN = "/.proxy/cdn"
	if _, list := get(variant); !strings.Contains(list, "\n/.proxy/cdn/activity/api/watch/media/"+media.key+"/"+segs[0]+"\n") ||
		strings.Contains(list, `URI="/.proxy`) {
		t.Fatalf("variant through the edge:\n%s", list)
	}
	r = httptest.NewRequest("GET", "/x?direct=1", nil)
	r.SetPathValue("key", media.key)
	r.SetPathValue("id", variant)
	w = httptest.NewRecorder()
	b.watchMediaAPI(w, r)
	if strings.Contains(w.Body.String(), "/.proxy/cdn") {
		t.Fatalf("direct variant:\n%s", w.Body.String())
	}
	b.watch.mu.Lock()
	used := b.watch.edge.segs
	b.watch.mu.Unlock()
	if used != len(segs) {
		t.Errorf("edge count %d, want %d", used, len(segs))
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
	if _, _, err := startWatchTorrent(context.Background(), "/nonexistent-cli", "not-a-hash", "sub", nil); err == nil {
		t.Fatal("invalid torrent hash should fail before launching helper")
	}
}

// TestHLSLanguagesForNativePlayback verifies the server, not the browser,
// enumerates real language renditions and builds independently playable masters.
func TestHLSLanguagesForNativePlayback(t *testing.T) {
	master := `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="stereo",NAME="English",LANGUAGE="eng",DEFAULT=YES,URI="en/audio.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="stereo",NAME="Japanese",LANGUAGE="jpn",DEFAULT=NO,URI="ja/audio.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="stereo",NAME="German",LANGUAGE="ger",DEFAULT=NO,URI="de/audio.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="stereo",NAME="No address",LANGUAGE="spa",DEFAULT=NO
#EXT-X-STREAM-INF:BANDWIDTH=2500000,RESOLUTION=1280x720,AUDIO="stereo"
720/index.m3u8
`
	codes := hlsAvailableAudio(master)
	if got := strings.Join(codes, ","); got != "en,ja,de" {
		t.Fatalf("advertised HLS languages = %q", got)
	}
	for _, tt := range []struct{ language, expected string }{
		{"en", `NAME="English",LANGUAGE="eng",DEFAULT=YES`},
		{"ja", `NAME="Japanese",LANGUAGE="jpn",DEFAULT=YES`},
		{"de", `NAME="German",LANGUAGE="ger",DEFAULT=YES`},
	} {
		m, _, _ := newWatchMedia(streamLink{URL: "https://media.example/master.m3u8"}, false)
		m.audio = tt.language
		base, _ := url.Parse(m.link.URL)
		out := m.rewritePlaylist(master, base)
		if !strings.Contains(out, tt.expected) || strings.Count(out, "DEFAULT=YES") != 1 {
			t.Fatalf("%s master did not select one correct default: %s", tt.language, out)
		}
	}
	if got := hlsAvailableAudio("#EXTM3U\n#EXTINF:10,\na.ts\n"); len(got) != 0 {
		t.Fatalf("media playlist reported %v", got)
	}
}

func TestWatchHLSLanguagesReachRoom(t *testing.T) {
	master := `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="stereo",NAME="English",LANGUAGE="eng",DEFAULT=YES,URI="en/audio.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="stereo",NAME="Japanese",LANGUAGE="jpn",DEFAULT=NO,URI="ja/audio.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=1500000,RESOLUTION=1280x720,AUDIO="stereo"
720/index.m3u8
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, master)
	}))
	defer server.Close()
	b := &bot{}
	link := streamLink{URL: server.URL + "/master.m3u8"}
	media, entry, _ := newWatchMedia(link, true)
	room := &watchRoom{gen: 7, phase: "play", media: media, entry: entry,
		streams: []streamLink{link}, audioEntries: map[string]string{"dub": watchMediaPath(media.key, entry)}}
	b.watch.add(room)
	b.findWatchHLSTracks(context.Background(), room, 7, media)
	for _, code := range []string{"en", "ja"} {
		if room.audioEntries[code] == "" || room.extraAudioMedia[code] == nil ||
			room.extraAudioMedia[code].audio != code {
			t.Fatalf("language %q not exposed through watch relay", code)
		}
	}
	if room.audioEntries["dub"] == "" {
		t.Fatal("existing dub fallback was lost")
	}
	// A stale callback must not modify the next episode.
	room.gen++
	room.audioEntries = map[string]string{}
	b.findWatchHLSTracks(context.Background(), room, 7, media)
	if len(room.audioEntries) != 0 {
		t.Fatal("stale HLS probe updated a new generation")
	}
}

// A lookup called off by its room must not hand an empty answer to another
// room waiting on the same episode; the waiter looks it up itself.
func TestWatchInfosWaiterOutlivesCanceledLookup(t *testing.T) {
	old := watchResolve
	t.Cleanup(func() { watchResolve = old })
	started := make(chan struct{})
	var calls atomic.Int32
	watchResolve = func(ctx context.Context, _ string, id, ep int, _, _ bool) (streamInfo, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-ctx.Done()
			return streamInfo{}, ctx.Err()
		}
		return streamInfo{AnilistID: id, Episode: ep, Streams: []streamLink{{URL: "https://video.example/a.m3u8"}}}, nil
	}
	var infos watchInfos
	first, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		infos.get(first, "", 1, 2, false, false)
	}()
	<-started
	type answer struct {
		info streamInfo
		err  error
	}
	got := make(chan answer, 1)
	go func() {
		info, err := infos.get(context.Background(), "", 1, 2, false, false)
		got <- answer{info, err}
	}()
	time.Sleep(20 * time.Millisecond) // let the second caller start waiting
	cancel()
	<-done
	a := <-got
	if a.err != nil || len(a.info.Streams) != 1 {
		t.Fatalf("waiter got %+v, %v", a.info, a.err)
	}
	if calls.Load() != 2 {
		t.Fatalf("%d lookups", calls.Load())
	}
}

// A KickAssAnime episode asks for its other languages by the show id the
// first lookup found, and those carry the dub too, so nothing searches for
// the show again. The next episode is looked up only after that.
func TestWatchEpisodeStartLookupsRunOnce(t *testing.T) {
	b, _ := watchBot(t, "https://cdn.example/ep.m3u8")
	var mu sync.Mutex
	var order []string
	note := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}
	ahead := make(chan struct{})
	watchAhead = func(fn func()) { note("ahead"); close(ahead) }
	watchResolve = func(_ context.Context, _ string, id, ep int, dub, _ bool) (streamInfo, error) {
		note(fmt.Sprintf("resolve %d dub=%v", ep, dub))
		return streamInfo{AnilistID: id, Episode: ep, Provider: "kickassanime", ProviderID: "show-1", Mode: "sub",
			Streams: []streamLink{{URL: "https://cdn.example/ja.m3u8"}}}, nil
	}
	oldAudio, oldLanguages := watchResolveAudio, watchResolveLanguages
	t.Cleanup(func() { watchResolveAudio, watchResolveLanguages = oldAudio, oldLanguages })
	watchResolveAudio = func(context.Context, string, int, int, bool, bool) (streamInfo, error) {
		note("full lookup")
		return streamInfo{}, errors.New("unexpected")
	}
	watchResolveLanguages = func(_ context.Context, _, provider, id string, ep int) (streamInfo, error) {
		note(fmt.Sprintf("languages %s %s %d", provider, id, ep))
		return streamInfo{Provider: "kickassanime", AudioVariants: map[string]streamLink{
			"en": {URL: "https://cdn.example/en.m3u8"},
			"ja": {URL: "https://cdn.example/ja.m3u8"}, // the playing release itself
		}}, nil
	}
	b.store.update(func(d *storeData) { d.WatchTickets = map[string]int{"a": 1} })
	if code := watchPost(b, (*bot).watchOpen, "a", watchIn{Room: "r1", Channel: "vc"}, nil); code != 200 {
		t.Fatalf("open: %d", code)
	}
	if code := watchPost(b, (*bot).watchPick, "a", watchIn{Room: "r1", Show: 5, Ep: 3}, nil); code != 200 {
		t.Fatalf("pick: %d", code)
	}
	waitLoaded(t, b)
	select {
	case <-ahead:
	case <-time.After(5 * time.Second):
		t.Fatal("next episode never looked up")
	}
	mu.Lock()
	got := strings.Join(order, "; ")
	mu.Unlock()
	if want := "resolve 3 dub=false; languages kickassanime show-1 3; ahead"; got != want {
		t.Fatalf("lookups %q, want %q", got, want)
	}
	b.watch.mu.Lock()
	defer b.watch.mu.Unlock()
	room := b.watch.only()
	if room.audioEntries["dub"] == "" || room.audioEntries["dub"] != room.audioEntries["en"] {
		t.Fatalf("dub not taken from the language lookup: %v", room.audioEntries)
	}
	if _, dup := room.audioReleases["ja"]; dup {
		t.Fatal("the playing release was offered again as a language")
	}
}

func TestWatchLanguagesFallBackForOldCLI(t *testing.T) {
	main := "https://video.example/en/playlist.m3u8"
	r := &watchRoom{spent: true, gen: 1, dub: true, provider: "kickassanime", streams: []streamLink{{URL: main}}}
	b := &bot{}
	b.watch.add(r)
	if !b.startStream(r, 0) {
		t.Fatal("episode did not start")
	}
	oldAudio, oldLanguages := watchResolveAudio, watchResolveLanguages
	t.Cleanup(func() { watchResolveAudio, watchResolveLanguages = oldAudio, oldLanguages })
	watchResolveLanguages = func(context.Context, string, string, string, int) (streamInfo, error) {
		return streamInfo{}, errWatchCLITooOld
	}
	full := 0
	watchResolveAudio = func(context.Context, string, int, int, bool, bool) (streamInfo, error) {
		full++
		return streamInfo{Provider: "kickassanime", AudioVariants: map[string]streamLink{"de": {URL: "https://video.example/de/playlist.m3u8"}}}, nil
	}
	b.findWatchLanguages(context.Background(), r, 1, "", 1, 1, false, true, "show-1")
	if full != 1 || r.audioEntries["de"] == "" {
		t.Fatalf("old CLI fallback: %d full lookups, %v", full, r.audioEntries)
	}
}

func TestWatchRoomReturnsTicketWhenNothingPlayed(t *testing.T) {
	for _, played := range []float64{0, 31} {
		b, now := watchBot(t, "https://cdn.example/ep.mp4")
		b.store.update(func(d *storeData) { d.WatchTickets = map[string]int{"a": 1} })
		watchPost(b, (*bot).watchOpen, "a", watchIn{Room: "r", Channel: "vc"}, nil)
		watchPost(b, (*bot).watchPick, "a", watchIn{Room: "r", Show: 5, Ep: 1}, nil)
		waitLoaded(t, b)
		r := b.watch.only()
		if b.watchTickets("a") != 0 {
			t.Fatal("ticket not spent when the episode started")
		}
		watchPost(b, (*bot).watchBeat, "a", watchIn{Room: "r", Pos: 0, Playing: true, Dur: 1400}, nil)
		watchPost(b, (*bot).watchBeat, "a", watchIn{Room: "r", Pos: played, Playing: true, Dur: 1400}, nil)
		// The source fails: the room ends without the episode playing.
		watchPost(b, (*bot).watchCtl, "a", watchIn{Room: "r", Act: "broken"}, nil)
		b.watch.mu.Lock()
		b.closeWatchRoom(r, *now)
		b.watch.mu.Unlock()
		want := 1
		if played > 0 {
			want = 0
		}
		deadline := time.Now().Add(2 * time.Second)
		for b.watchTickets("a") != want && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if got := b.watchTickets("a"); got != want {
			t.Fatalf("played %v s: %d tickets left, want %d", played, got, want)
		}
		b.watch.bg.Wait() // the next bot replaces the lookups this one still runs
	}
}

func TestWatchRoomPerks(t *testing.T) {
	for _, opener := range []string{"champ", "staff"} {
		t.Run(opener, func(t *testing.T) {
			b, now := watchBot(t, "https://cdn.example/ep.mp4")
			b.store.update(func(d *storeData) { d.Champions = []string{"champ"} })
			var v watchView
			max := map[string]int{"champ": 20, "staff": 30}[opener]
			if code := watchPost(b, (*bot).watchOpen, opener, watchIn{Room: "r", Channel: "vc"}, &v); code != 200 || v.Max != max {
				t.Fatalf("opens without a ticket: %d %+v", code, v)
			}
			watchPost(b, (*bot).watchPick, opener, watchIn{Room: "r", Show: 5, Ep: 1}, nil)
			waitLoaded(t, b)
			b.watch.mu.Lock()
			v = b.watchViewOf(b.watch.only(), opener, false, watchNow())
			b.watch.mu.Unlock()
			if v.Phase != "play" || v.Ends != now.Add(watchPerkLength).UnixMilli() {
				t.Fatalf("playing for 8 hours: %+v", v)
			}
			for k := 0; k < max-1; k++ {
				if code := watchPost(b, (*bot).watchOpen, fmt.Sprint("m", k), watchIn{Room: "r", Channel: "vc"}, nil); code != 200 {
					t.Fatalf("member %d of %d: %d", k+2, max, code)
				}
			}
			if code := watchPost(b, (*bot).watchOpen, "late", watchIn{Room: "r", Channel: "vc"}, nil); code != http.StatusForbidden {
				t.Fatalf("person %d: %d", max+1, code)
			}
			// Still open past four hours, closed after eight.
			r := b.watch.only()
			*now = now.Add(watchRoomLength + time.Hour)
			r.members[opener].seen = *now
			if !b.watchStep(r, *now) || b.watch.only() == nil {
				t.Fatal("closed after four hours")
			}
			*now = now.Add(3*time.Hour + time.Minute)
			r.members[opener].seen = *now
			if b.watchStep(r, *now) || b.watch.only() != nil {
				t.Fatal("still open after eight hours")
			}
			time.Sleep(20 * time.Millisecond)
			if n := b.watchTickets(opener); n != 0 {
				t.Fatalf("got %d tickets from a free room", n)
			}
		})
	}
}

// only is the one open room, for tests that run a single room; nil when
// none is open.
func (w *watchRooms) only() *watchRoom {
	if len(w.rooms) > 1 {
		panic("more than one watch room open")
	}
	for _, r := range w.rooms {
		return r
	}
	return nil
}

func TestWatchRoomsAtOnce(t *testing.T) {
	b, now := watchBot(t, "https://cdn.example/ep.mp4")
	b.store.update(func(d *storeData) {
		d.WatchTickets = map[string]int{"a": 1}
		d.Champions = []string{"champ"}
		d.Settings = map[string]string{"watch_rooms_max": "2"}
	})
	status := func(user, room string) map[string]any {
		r := httptest.NewRequest("GET", "/x?room="+room, nil)
		r.Header.Set("Authorization", "Bearer "+b.acts.add(user))
		w := httptest.NewRecorder()
		b.watchStatus(w, r)
		out := map[string]any{}
		json.NewDecoder(w.Body).Decode(&out)
		return out
	}
	room := func(id string) *watchRoom {
		b.watch.mu.Lock()
		defer b.watch.mu.Unlock()
		return b.watch.room(id)
	}
	if code := watchPost(b, (*bot).watchOpen, "a", watchIn{Room: "r1", Channel: "vc"}, nil); code != 200 {
		t.Fatalf("first room: %d", code)
	}
	*now = now.Add(time.Second)
	if code := watchPost(b, (*bot).watchOpen, "staff", watchIn{Room: "r2", Channel: "nsfw"}, nil); code != 200 {
		t.Fatalf("second room: %d", code)
	}
	if code := watchPost(b, (*bot).watchOpen, "champ", watchIn{Room: "r3", Channel: "vc"}, nil); code != http.StatusConflict {
		t.Fatalf("third room past the limit: %d", code)
	}
	// Each room keeps its own people and picks.
	watchPost(b, (*bot).watchOpen, "b", watchIn{Room: "r1", Channel: "vc"}, nil)
	if code := watchPost(b, (*bot).watchPick, "staff", watchIn{Room: "r1", Show: 5, Ep: 1}, nil); code != http.StatusForbidden {
		t.Fatalf("host of another room picks here: %d", code)
	}
	r1, r2 := room("r1"), room("r2")
	if len(r1.members) != 2 || len(r2.members) != 1 || r1.host != "a" || r2.host != "staff" || r2.perks != "staff" || r1.perks != "" {
		t.Fatalf("rooms mixed up: %+v %+v", r1.members, r2.members)
	}

	st := status("x", "elsewhere")
	if st["full"] != true || st["here"] != nil || st["rooms_max"] != float64(2) || len(st["rooms"].([]any)) != 2 {
		t.Fatalf("both rooms elsewhere: %v", st)
	}
	if first := st["rooms"].([]any)[0].(map[string]any); first["channel"] != "vc" || first["people"] != float64(2) {
		t.Fatalf("oldest room first: %v", first)
	}
	st = status("a", "r1")
	if st["full"] != false || st["here"] != true || st["people"] != float64(2) || len(st["rooms"].([]any)) != 1 {
		t.Fatalf("own room: %v", st)
	}
	if other := st["rooms"].([]any)[0].(map[string]any); other["adult"] != true || other["host"] != nil {
		t.Fatalf("18+ room elsewhere leaked: %v", other)
	}

	// One room closing leaves the other running and makes space again.
	// r2 has been playing, r1 never started.
	b.watch.mu.Lock()
	r2.started = *now
	b.watch.mu.Unlock()
	*now = now.Add(watchPickWait + time.Minute)
	r2.members["staff"].seen = *now
	if b.watchStep(r1, *now) {
		t.Fatal("unused room still open")
	}
	if room("r1") != nil || room("r2") != r2 || !b.watchStep(r2, *now) {
		t.Fatal("closing one room touched the other")
	}
	if code := watchPost(b, (*bot).watchOpen, "champ", watchIn{Room: "r3", Channel: "vc"}, nil); code != 200 {
		t.Fatalf("room after one closed: %d", code)
	}
	if status("x", "elsewhere")["full"] != true {
		t.Fatal("full again")
	}
}

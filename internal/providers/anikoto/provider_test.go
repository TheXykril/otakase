package anikoto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/thexykril/otakase/internal/providers"
)

// encryptForTest is megaplay's side of decryptMegaplay.
func encryptForTest(t *testing.T, plain string) string {
	t.Helper()
	block, err := aes.NewCipher(padKey(megaplayKey, 32))
	if err != nil {
		t.Fatal(err)
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	data := append([]byte(plain), bytes.Repeat([]byte{byte(pad)}, pad)...)
	out := make([]byte, len(data))
	cipher.NewCBCEncrypter(block, padKey(megaplayIV, aes.BlockSize)).CryptBlocks(out, data)
	return base64.RawURLEncoding.EncodeToString(out)
}

type stub struct {
	server     *httptest.Server
	serverGets atomic.Int32
	// failFirstServer makes the first server's embed page fail, so the
	// second server has to be used.
	failFirstServer bool
}

func ajaxReply(w http.ResponseWriter, result any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"status": 200, "result": result})
}

// stubHost serves the site's endpoints and a megaplay embed in the shapes the
// real ones use. Episode 2 exists in sub only.
func stubHost(t *testing.T) (*Provider, *stub) {
	t.Helper()
	s := &stub{}
	enc := encryptForTest(t, `{"file":"https://cdn.invalid/master.m3u8"}`)
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := s.server.URL
		switch {
		case r.URL.Path == "/ajax/anime/search":
			ajaxReply(w, map[string]string{"html": `<div class="scaff items">  <a class="item" href="` + base + `/watch/frieren-c6fbj">
    <div class="info">
      <div class="name d-title" data-jp="Sousou no Frieren">Frieren: Beyond Journey&#39;s End</div>
      <div class="meta">
        <span class="dot rating">PG-13</span>
        <span class="dot">TV</span> <span class="dot">2023</span>
      </div>
    </div>
  </a></div>`})
		case r.URL.Path == "/filter":
			fmt.Fprint(w, `<div id="list-items" class="ani items">
<div class="item "> <div class="inner"> <div class="ani poster tip" data-tip="1"> <a href="`+base+`/watch/frieren-c6fbj/ep-1"> <div class="right">TV</div> </a> </div> <div class="info"> <div class="b1"> <a class="name d-title" href="`+base+`/watch/frieren-c6fbj/ep-1" data-jp="x">Frieren: Beyond Journey&#39;s End</a></div></div></div></div>
<div class="item "> <div class="inner"> <div class="ani poster tip" data-tip="2"> <a href="`+base+`/watch/frieren-season-2-cp7ym/ep-1"> <div class="right">TV</div> </a> </div> <div class="info"> <div class="b1"> <a class="name d-title" href="`+base+`/watch/frieren-season-2-cp7ym/ep-1" data-jp="x">Frieren Season 2</a></div></div></div></div>
</div>`)
		case r.URL.Path == "/watch/frieren-c6fbj":
			fmt.Fprint(w, `<div id="watch-main" class="watch-wrap" itemscope data-id="6180" data-url="x">`)
		case r.URL.Path == "/ajax/episode/list/6180":
			ajaxReply(w, `<ul class="ep-range">
<li><a href="#" data-id="1" data-num="2" data-slug="2" data-mal="52991" data-sub="1" data-dub="0" data-ids="EP2" class="">2</a></li>
<li><a href="#" data-id="2" data-num="1" data-slug="1" data-mal="52991" data-sub="1" data-dub="1" data-ids="EP1" class="active">1</a></li>
<li><a href="#" data-id="3" data-num="1.5" data-slug="1.5" data-sub="1" data-dub="0" data-ids="EP15">1.5</a></li>
</ul>`)
		case r.URL.Path == "/ajax/server/list":
			ep := r.URL.Query().Get("servers")
			ajaxReply(w, `<div class="servers"><div class="type" data-type="sub"><label>SUB</label><ul><li data-ep-id="1" data-sv-id="323" data-link-id="`+ep+`-sub-a">HD-1</li> <li data-ep-id="1" data-sv-id="e54" data-link-id="`+ep+`-sub-b">Vidstream-2</li> </ul>
</div><div class="type" data-type="dub"><label>DUB</label><ul><li data-ep-id="1" data-sv-id="323" data-link-id="`+ep+`-dub-a">HD-1</li> </ul>
</div></div>`)
		case r.URL.Path == "/ajax/server":
			s.serverGets.Add(1)
			get := r.URL.Query().Get("get")
			ajaxReply(w, map[string]any{
				"url":       base + "/stream/s-2/" + get,
				"skip_data": map[string][]int{"intro": {31, 111}, "outro": {1376, 1447}},
			})
		case strings.HasPrefix(r.URL.Path, "/stream/s-2/"):
			if s.failFirstServer && strings.HasSuffix(r.URL.Path, "-a") {
				http.Error(w, "gone", http.StatusBadGateway)
				return
			}
			fmt.Fprint(w, `<div id="megaplay-player" data-realid="2142" data-id="36396" data-mediaid="1774"></div>`)
		case r.URL.Path == "/stream/getSources":
			if r.URL.Query().Get("id") != "36396" || r.Header.Get("X-Requested-With") == "" {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tracks": []map[string]any{
					{"file": "https://cdn.invalid/thumbnails.vtt", "kind": "thumbnails"},
					{"file": "https://cdn.invalid/jp.vtt", "label": "Japanese", "kind": "captions"},
					{"file": "https://cdn.invalid/en.vtt", "label": "English", "kind": "captions", "default": true},
				},
				"intro": map[string]int{"start": 0, "end": 89},
				"outro": map[string]int{"start": 1460, "end": 1549},
				"enc":   enc,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.server.Close)
	return &Provider{client: &client{mirrors: []string{s.server.URL}, http: s.server.Client()}}, s
}

func TestSearchLabelsSeasonsDistinguishably(t *testing.T) {
	p, _ := stubHost(t)
	options, err := p.SearchAnime("frieren", "sub")
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 2 {
		t.Fatalf("want the quick result plus the one only the filter page has, got %+v", options)
	}
	if options[0].Key != "frieren-c6fbj" || options[0].Title != "Frieren: Beyond Journey's End" {
		t.Errorf("first option = %+v", options[0])
	}
	// A search for a long-running title returns seasons whose names barely
	// differ, so the label has to carry more than the name.
	if !strings.Contains(options[0].Label, "TV") || !strings.Contains(options[0].Label, "2023") {
		t.Errorf("label lacks format and year: %q", options[0].Label)
	}
	if options[1].Key != "frieren-season-2-cp7ym" {
		t.Errorf("second option = %+v", options[1])
	}
}

// Sub and dub share a row. A show carried in only one language must report
// nothing for the other, rather than a list that cannot play.
func TestEpisodesAreFilteredByLanguage(t *testing.T) {
	p, _ := stubHost(t)

	sub, err := p.EpisodesList("frieren-c6fbj", "sub")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(sub, ",") != "1,2" {
		t.Errorf("sub episodes = %v, want 1,2 sorted and without the 1.5 recap", sub)
	}
	dub, err := p.EpisodesList("frieren-c6fbj", "dub")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(dub, ",") != "1" {
		t.Errorf("dub episodes = %v, want only 1", dub)
	}
}

// Keys from the old AniList-keyed index must not be read as the site's own
// numbers, which name different shows.
func TestOldAniListKeysAreRefused(t *testing.T) {
	p, _ := stubHost(t)
	if _, err := p.EpisodesList("154587", "sub"); err == nil || !strings.Contains(err.Error(), "search again") {
		t.Errorf("an all-digit key should be refused with a hint to search again, got %v", err)
	}
	if _, err := p.EpisodesList("../admin", "sub"); err == nil {
		t.Error("a key that is not a slug should be refused")
	}
}

func TestStreamResolvesThroughMegaplay(t *testing.T) {
	p, s := stubHost(t)
	urls, hints, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "frieren-c6fbj", 1, "sub")
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 1 || urls[0] != "https://cdn.invalid/master.m3u8" {
		t.Fatalf("urls = %v", urls)
	}
	hint := hints[urls[0]]
	if hint.Headers["Referer"] != s.server.URL+"/" || hint.Referrer != s.server.URL+"/" {
		t.Errorf("the CDN needs megaplay's page as referrer, got %+v", hint)
	}
	if hint.Headers["User-Agent"] == "" {
		t.Error("no user-agent passed to the player")
	}
	if hint.Subtitle != "https://cdn.invalid/en.vtt" {
		t.Errorf("subtitle = %q, want the default English track", hint.Subtitle)
	}
	if len(hint.Subtitles) != 2 {
		t.Errorf("thumbnail track should not be offered as a subtitle: %+v", hint.Subtitles)
	}
}

func TestADeadServerFallsBackToTheNext(t *testing.T) {
	p, s := stubHost(t)
	s.failFirstServer = true
	urls, _, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "frieren-c6fbj", 1, "sub")
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 1 {
		t.Fatalf("urls = %v", urls)
	}
	if got := s.serverGets.Load(); got != 2 {
		t.Errorf("server lookups = %d, want 2", got)
	}
}

// A dub request for an episode only carried in sub has to fail, or it would
// play Japanese audio while labelled dub, and a cast would skip burning the
// subtitles that stream needs.
func TestADubRequestIsRefusedWhenTheShowHasNoDub(t *testing.T) {
	p, _ := stubHost(t)
	links, _, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{SubOrDub: "dub"}, "frieren-c6fbj", 2, "dub")
	if err == nil {
		t.Fatalf("a dub request for a sub-only episode returned %d links and no error", len(links))
	}
}

func TestADubRequestStillWorksWhenTheDubExists(t *testing.T) {
	p, _ := stubHost(t)
	links, _, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{SubOrDub: "dub"}, "frieren-c6fbj", 1, "dub")
	if err != nil {
		t.Fatalf("episode 1 has a dub, got %v", err)
	}
	if len(links) == 0 {
		t.Error("no links for an episode that does have a dub")
	}
}

// The skip range comes with the stream, so asking for it right after playing
// must not resolve everything again.
func TestSkipRangeReusesTheResolvedStream(t *testing.T) {
	p, s := stubHost(t)
	if _, _, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "frieren-c6fbj", 1, "sub"); err != nil {
		t.Fatal(err)
	}
	before := s.serverGets.Load()
	intro, outro, err := p.SkipRange("frieren-c6fbj", "sub", 1)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(intro) != "[0 89]" || fmt.Sprint(outro) != "[1460 1549]" {
		t.Errorf("intro=%v outro=%v, want megaplay's own ranges", intro, outro)
	}
	if s.serverGets.Load() != before {
		t.Error("SkipRange resolved the stream a second time")
	}
}

func TestMirrorsAreTriedInOrder(t *testing.T) {
	p, s := stubHost(t)
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "error code: 502", http.StatusBadGateway)
	}))
	t.Cleanup(down.Close)
	p.client.mirrors = []string{down.URL, s.server.URL}

	if _, err := p.SearchAnime("frieren", "sub"); err != nil {
		t.Fatalf("the second mirror should have answered: %v", err)
	}
	if p.client.current != 1 {
		t.Errorf("the working mirror should be remembered, current = %d", p.client.current)
	}
}

func TestDecryptMegaplay(t *testing.T) {
	// A real getSources answer from October 2026.
	plain, err := decryptMegaplay("wdeBruh3qqn_i5wUNnyaPcXqidp1UWP84FfPHzGyKXA2hDZBfMCmZ4FLvs7_pQuH549Eptax8UOjAJyRIZfrRhUGUKy9OBeGh2yB_-m_JLAlLnWTLzYZC3_C5I4ltveYoiaU66Do9RgI9bCetmk_o87-sd66brXnWV1MbjhLnjw")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plain), `"file":"https://`) || !strings.HasSuffix(string(plain), `master.m3u8"}`) {
		t.Errorf("decrypted = %q", plain)
	}
	if _, err := decryptMegaplay("not base64!"); err == nil {
		t.Error("garbage should not decrypt")
	}
}

func TestPlainSourcesStillWork(t *testing.T) {
	file, err := sourceFile(megaplaySources{Sources: json.RawMessage(`{"file":"https://cdn.invalid/a.m3u8"}`)})
	if err != nil || file != "https://cdn.invalid/a.m3u8" {
		t.Errorf("file=%q err=%v", file, err)
	}
	if _, err := sourceFile(megaplaySources{}); err == nil {
		t.Error("an empty answer should be an error")
	}
}

func TestMalformedSkipSpansAreRejected(t *testing.T) {
	for _, span := range [][]int{nil, {}, {5}, {90, 90}, {120, 30}, {0, 0}, {1, 2, 3}} {
		if got := validSpan(span); got != nil {
			t.Errorf("span %v should have been rejected, got %v", span, got)
		}
	}
	if got := validSpan([]int{0, 89}); len(got) != 2 {
		t.Errorf("a valid span was rejected: %v", got)
	}
}

// Anything that is not dub is sub, which is the language every show has.
func TestCategoryNormalisation(t *testing.T) {
	for input, want := range map[string]string{
		"dub": "dub", "DUB": "dub", " Dub ": "dub",
		"sub": "sub", "": "sub", "nonsense": "sub",
	} {
		if got := normalizeCategory(input); got != want {
			t.Errorf("%q became %q, expected %q", input, got, want)
		}
	}
}

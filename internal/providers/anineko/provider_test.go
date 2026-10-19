package anineko

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/thexykril/otakase/internal/providerhost"
	"github.com/thexykril/otakase/internal/providers"
	"github.com/thexykril/otakase/internal/providers/substyle"
)

// tinyPNG is a complete 1x1 PNG, the wrapper vibeplayer puts on segments.
var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
	0x54, 0x78, 0xda, 0x63, 0x64, 0xf8, 0xcf, 0x50,
	0x0f, 0x00, 0x03, 0x86, 0x01, 0x80, 0x5a, 0x34,
	0x7d, 0x6b, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45,
	0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

// tsPayload stands in for an MPEG-TS packet: it starts with the sync byte.
var tsPayload = []byte{0x47, 0x40, 0x11, 0x10}

// fakeWeb serves every host the provider talks to from one test server,
// routed by "host/path". It records the Referer each route was asked with.
type fakeWeb struct {
	mu       sync.Mutex
	routes   map[string]func(w http.ResponseWriter, r *http.Request)
	referers map[string]string
}

// ServeHTTP answers from the route for the request's host and path, noting
// the Referer it came with; anything unrouted is a 404.
func (web *fakeWeb) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Host + r.URL.Path
	web.mu.Lock()
	route, routed := web.routes[key]
	web.referers[key] = r.Header.Get("Referer")
	web.mu.Unlock()
	if !routed || route == nil {
		http.NotFound(w, r)
		return
	}
	route(w, r)
}

func newFakeWeb(t *testing.T) *fakeWeb {
	t.Helper()
	web := &fakeWeb{routes: map[string]func(http.ResponseWriter, *http.Request){}, referers: map[string]string{}}
	server := httptest.NewServer(web)

	dialer := &net.Dialer{}
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, server.Listener.Addr().String())
		},
	}}

	previousClient, previousBase := providerhost.HTTPClient, baseURL
	providerhost.HTTPClient = func() *http.Client { return client }
	baseURL = "http://anineko.test"
	t.Cleanup(func() {
		providerhost.HTTPClient, baseURL = previousClient, previousBase
		resetVibeProxyForTest()
		server.Close()
	})
	return web
}

func (web *fakeWeb) text(key, body string) {
	web.routes[key] = func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }
}

func (web *fakeWeb) referer(key string) string {
	web.mu.Lock()
	defer web.mu.Unlock()
	return web.referers[key]
}

// fixedSubStyle makes substyle answer as if the user had set style, with no
// prompt available.
func fixedSubStyle(t *testing.T, style string) {
	t.Helper()
	prompt, out, current := providerhost.PromptSelect, providerhost.Out, providerhost.CurrentSubStyle
	providerhost.PromptSelect = nil
	providerhost.Out = nil
	providerhost.CurrentSubStyle = func() string { return style }
	substyle.ResetForTest()
	t.Cleanup(func() {
		providerhost.PromptSelect, providerhost.Out, providerhost.CurrentSubStyle = prompt, out, current
		substyle.ResetForTest()
	})
}

func watchFixture(t *testing.T) string {
	t.Helper()
	page, err := os.ReadFile("testdata/watch.html")
	if err != nil {
		t.Fatal(err)
	}
	return string(page)
}

func TestSearchAnime(t *testing.T) {
	web := newFakeWeb(t)
	web.text("anineko.test/ajax/search", `{"success":true,"results":[
		{"title":" One Piece ","url":"/watch/one-piece","image":"/img/op.jpg","meta":"TV"},
		{"title":"No link","url":"/genre/action","image":"","meta":""},
		{"title":"One Piece Film: Red","url":"https://anineko.to/watch/one-piece-film-red?ref=x","image":"//cdn.test/red.jpg","meta":""}
	]}`)

	results, err := (&Provider{}).SearchAnime("  one piece ", "sub")
	if err != nil {
		t.Fatal(err)
	}
	want := []providers.SelectionOption{
		{Key: "one-piece", Label: "One Piece — TV", Title: "One Piece", Thumbnail: "http://anineko.test/img/op.jpg"},
		{Key: "one-piece-film-red", Label: "One Piece Film: Red", Title: "One Piece Film: Red", Thumbnail: "https://cdn.test/red.jpg"},
	}
	if len(results) != len(want) {
		t.Fatalf("got %d results, want %d: %+v", len(results), len(want), results)
	}
	for i := range want {
		if results[i] != want[i] {
			t.Errorf("result %d = %+v, want %+v", i, results[i], want[i])
		}
	}
	if got := web.referer("anineko.test/ajax/search"); got != "http://anineko.test/" {
		t.Errorf("search referer = %q", got)
	}
}

func TestSearchAnimeFailures(t *testing.T) {
	cases := map[string]struct {
		body    string
		wantErr string
	}{
		"no results":   {`{"success":true,"results":[]}`, `no results for "frieren"`},
		"site refused": {`{"success":false}`, "anineko search failed"},
		"not json":     {`<html>`, "parse anineko search response"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			web := newFakeWeb(t)
			web.text("anineko.test/ajax/search", tc.body)
			_, err := (&Provider{}).SearchAnime("frieren", "sub")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
	if _, err := (&Provider{}).SearchAnime("  ", "sub"); err == nil {
		t.Fatal("empty query was accepted")
	}
}

func TestEpisodesList(t *testing.T) {
	web := newFakeWeb(t)
	web.text("anineko.test/watch/frieren", watchFixture(t))

	episodes, err := (&Provider{}).EpisodesList("frieren", "sub")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(episodes, ","); got != "1,2,3,12" {
		t.Fatalf("episodes = %s, want 1,2,3,12", got)
	}

	web.text("anineko.test/watch/empty", "<html></html>")
	if _, err := (&Provider{}).EpisodesList("empty", "sub"); err == nil || !strings.Contains(err.Error(), "no episodes found") {
		t.Fatalf("err = %v, want no episodes found", err)
	}
}

func TestServerGroups(t *testing.T) {
	groups := serverGroups(watchFixture(t))
	want := map[string][]string{
		"hsub": {"http://vibeplayer.test/e/hard3"},
		"sub": {
			"http://bibiemb.test/e/soft3?sub=https%3A%2F%2Fcdn.test%2Ffrieren-3.vtt",
			"http://vibeplayer.test/e/soft3?caption_1=https://cdn.test/frieren-3-alt.vtt",
		},
		"dub": {"http://unknown.test/e/dub3", "http://bibiemb.test/e/dub3"},
	}
	for lang, urls := range want {
		if strings.Join(groups[lang], " ") != strings.Join(urls, " ") {
			t.Errorf("%s = %q, want %q", lang, groups[lang], urls)
		}
	}
}

func TestPickServers(t *testing.T) {
	groups := serverGroups(watchFixture(t))

	fixedSubStyle(t, "hard")
	got, err := pickServers(groups, "sub", "")
	if err != nil || len(got) != 1 || got[0] != "http://vibeplayer.test/e/hard3" {
		t.Fatalf("hard: %q, %v", got, err)
	}

	fixedSubStyle(t, "soft")
	got, err = pickServers(groups, "sub", "")
	if err != nil || len(got) != 2 {
		t.Fatalf("soft: %q, %v", got, err)
	}

	got, err = pickServers(groups, "dub", "")
	if err != nil || len(got) != 2 {
		t.Fatalf("dub: %q, %v", got, err)
	}

	fixedSubStyle(t, "soft")
	if _, err := pickServers(map[string][]string{}, "sub", ""); err == nil {
		t.Fatal("an episode with no sub servers was accepted")
	}
}

func TestEmbedHost(t *testing.T) {
	cases := map[string]string{
		"https://bibiemb.xyz/e/abc":           "bibiemb",
		"https://BIBIEMB.site/e/abc":          "bibiemb",
		"https://vibeplayer.site/abc":         "vibeplayer",
		"https://vivibebe.site/e6693c8de8202": "vibeplayer",
		"https://vibebe.net/abc":              "vibeplayer",
		"https://vibes.example/abc":           "",
		"https://megacloud.tv/e/abc":          "",
		"not a url at all":                    "",
		"://broken":                           "",
	}
	for embed, want := range cases {
		if got := embedHost(embed); got != want {
			t.Errorf("embedHost(%q) = %q, want %q", embed, got, want)
		}
	}
}

func TestSubtitleFor(t *testing.T) {
	cases := []struct {
		name, embed, page, want string
	}{
		{"escaped in query", "https://bibiemb.test/e/1?sub=https%3A%2F%2Fcdn.test%2Fa.vtt", "", "https://cdn.test/a.vtt"},
		{"caption_1 key", "https://vibeplayer.test/e/1?caption_1=https://cdn.test/b.vtt", "", "https://cdn.test/b.vtt"},
		{"key case ignored", "https://vibeplayer.test/e/1?C1_FILE=https://cdn.test/c.vtt", "", "https://cdn.test/c.vtt"},
		{"query wins over page", "https://vibeplayer.test/e/1?sub=https://cdn.test/url.vtt", `const subtitle = "https://cdn.test/page.vtt";`, "https://cdn.test/url.vtt"},
		{"page variable", "https://vibeplayer.test/e/1", `const subtitle = "https://cdn.test/page.vtt";`, "https://cdn.test/page.vtt"},
		{"player track", "https://vibeplayer.test/e/1", `tracks: [{ file: "https://cdn.test/track.ass", kind: "captions" }]`, "https://cdn.test/track.ass"},
		{"blank variable falls to track", "https://vibeplayer.test/e/1", `const subtitle = " "; file: "https://cdn.test/t.srt"`, "https://cdn.test/t.srt"},
		{"nothing", "https://vibeplayer.test/e/1", `const src = "x";`, ""},
	}
	for _, tc := range cases {
		if got := subtitleFor(tc.embed, tc.page); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestResolveRef(t *testing.T) {
	const base = "https://cdn.test/a/b/master.m3u8?token=1"
	cases := map[string]string{
		"720p/index.m3u8":           "https://cdn.test/a/b/720p/index.m3u8",
		"/root/index.m3u8":          "https://cdn.test/root/index.m3u8",
		"https://other.test/x.m3u8": "https://other.test/x.m3u8",
		"seg.ts?sig=abc":            "https://cdn.test/a/b/seg.ts?sig=abc",
		"../up.m3u8":                "https://cdn.test/a/up.m3u8",
	}
	for ref, want := range cases {
		if got := resolveRef(base, ref); got != want {
			t.Errorf("resolveRef(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestBibiembPicksBestVariant(t *testing.T) {
	cases := map[string]struct {
		master string
		want   string
	}{
		"1080p listed after 720p": {
			"#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1,NAME=\"720p\"\n720p/index.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=2,NAME=\"1080p\"\n1080p/index.m3u8\n",
			"http://cdn.test/hls/1080p/index.m3u8",
		},
		"CRLF and 480p only": {
			"#EXTM3U\r\n#EXT-X-STREAM-INF:BANDWIDTH=1,NAME=\"240p\"\r\nhttp://other.test/240.m3u8\r\n#EXT-X-STREAM-INF:BANDWIDTH=1,NAME=\"480P\"\r\n/abs/480.m3u8\r\n",
			"http://cdn.test/abs/480.m3u8",
		},
		"unknown names take the first": {
			"#EXTM3U\n#EXT-X-STREAM-INF:NAME=\"auto\"\nauto.m3u8\n#EXT-X-STREAM-INF:NAME=\"best\"\nbest.m3u8\n",
			"http://cdn.test/hls/auto.m3u8",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			web := newFakeWeb(t)
			web.text("bibiemb.test/e/abc", `<script>const src = "http://cdn.test/hls/master.m3u8";</script>`)
			web.text("cdn.test/hls/master.m3u8", tc.master)

			s, err := resolveBibiemb(" http://bibiemb.test/e/abc ")
			if err != nil {
				t.Fatal(err)
			}
			if s.URL != tc.want || s.Referrer != "http://bibiemb.test/e/abc" {
				t.Fatalf("got %+v, want URL %q", s, tc.want)
			}
			if got := web.referer("cdn.test/hls/master.m3u8"); got != "http://bibiemb.test/e/abc" {
				t.Fatalf("master fetched with referer %q, want the embed", got)
			}
		})
	}
}

func TestBibiembFailures(t *testing.T) {
	web := newFakeWeb(t)
	web.text("bibiemb.test/e/nosrc", `<script>var x = 1;</script>`)
	web.text("bibiemb.test/e/novariants", `const src = "http://cdn.test/empty/master.m3u8"`)
	web.text("cdn.test/empty/master.m3u8", "#EXTM3U\n")

	if _, err := resolveBibiemb("http://bibiemb.test/e/nosrc"); err == nil || !strings.Contains(err.Error(), "master m3u8 not found") {
		t.Errorf("no src: %v", err)
	}
	if _, err := resolveBibiemb("http://bibiemb.test/e/novariants"); err == nil || !strings.Contains(err.Error(), "no bibiemb variants") {
		t.Errorf("no variants: %v", err)
	}
	if _, err := resolveBibiemb(""); err == nil {
		t.Error("empty embed was accepted")
	}
}

// serveVibeStream sets up a vibeplayer embed whose one variant has two
// PNG-wrapped segments, one relative and one absolute.
func serveVibeStream(web *fakeWeb, embedPath string) {
	web.text("vibeplayer.test"+embedPath, `const subtitle = "https://cdn.test/v.vtt";
const src = "http://cdn.test/public/stream/h3/master.m3u8";`)
	web.text("cdn.test/public/stream/h3/master.m3u8", "#EXTM3U\r\n#EXT-X-STREAM-INF:BANDWIDTH=1,NAME=\"720p\"\r\n720.m3u8\r\n")
	web.text("cdn.test/public/stream/h3/720.m3u8", "#EXTM3U\n#EXTINF:10.0,\n/public/seg/0.png\n#EXTINF:4.5,\nhttp://cdn.test/public/seg/1.png\n#EXT-X-ENDLIST\n")
	segment := append(append([]byte{}, tinyPNG...), tsPayload...)
	for _, key := range []string{"cdn.test/public/seg/0.png", "cdn.test/public/seg/1.png"} {
		web.routes[key] = func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(segment) }
	}
}

func fetchLocal(t *testing.T, rawURL string) []byte {
	t.Helper()
	resp, err := http.Get(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %d %s", rawURL, resp.StatusCode, body)
	}
	return body
}

func uriLines(playlist []byte) []string {
	var uris []string
	for _, line := range strings.Split(string(playlist), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			uris = append(uris, line)
		}
	}
	return uris
}

func TestVibeProxyUnwrapsSegments(t *testing.T) {
	web := newFakeWeb(t)
	serveVibeStream(web, "/e/hard3")

	s, err := resolveVibeplayer("http://vibeplayer.test/e/hard3")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.URL, "http://127.0.0.1:") || s.Subtitle != "https://cdn.test/v.vtt" {
		t.Fatalf("unexpected stream %+v", s)
	}

	master := fetchLocal(t, s.URL)
	if strings.Contains(string(master), "\r") {
		t.Fatalf("master kept CRLF: %q", master)
	}
	variants := uriLines(master)
	if len(variants) != 1 || !strings.HasSuffix(variants[0], "/720.m3u8") {
		t.Fatalf("master variants = %q", variants)
	}

	segments := uriLines(fetchLocal(t, variants[0]))
	if len(segments) != 2 || !strings.HasSuffix(segments[1], "/720.m3u8/seg/1") {
		t.Fatalf("variant segments = %q", segments)
	}
	for _, seg := range segments {
		if got := fetchLocal(t, seg); string(got) != string(tsPayload) {
			t.Fatalf("segment %s = %x, want the bare payload", seg, got)
		}
	}
	if got := web.referer("cdn.test/public/seg/0.png"); got != "http://vibeplayer.test/e/hard3" {
		t.Fatalf("segment fetched with referer %q, want the embed", got)
	}

	base := s.URL[:strings.Index(s.URL, "/stream/")]
	for _, path := range []string{"/stream/unknown/master.m3u8", "/stream/x", strings.TrimPrefix(segments[0], base)[:len("/stream/")+16] + "/720.m3u8/seg/9"} {
		resp, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, resp.StatusCode)
		}
	}
}

func TestStripPNG(t *testing.T) {
	wrapped := append(append([]byte{}, tinyPNG...), tsPayload...)
	if got := stripPNG(wrapped); string(got) != string(tsPayload) {
		t.Fatalf("got %x", got)
	}
	if got := stripPNG(tsPayload); string(got) != string(tsPayload) {
		t.Fatalf("plain data changed: %x", got)
	}
}

func TestGetEpisodeURLSoftSubs(t *testing.T) {
	web := newFakeWeb(t)
	fixedSubStyle(t, "soft")
	web.text("anineko.test/watch/frieren/ep-3", watchFixture(t))
	web.text("bibiemb.test/e/soft3", `const src = "http://cdn.test/hls/master.m3u8"`)
	web.text("cdn.test/hls/master.m3u8", "#EXTM3U\n#EXT-X-STREAM-INF:NAME=\"1080p\"\n1080p.m3u8\n")

	links, hints, err := (&Provider{}).GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "frieren", 3, "sub")
	if err != nil {
		t.Fatal(err)
	}
	want := "http://cdn.test/hls/1080p.m3u8"
	if len(links) != 1 || links[0] != want {
		t.Fatalf("links = %q", links)
	}
	hint := hints[want]
	if hint.Referrer != "http://bibiemb.test/e/soft3?sub=https%3A%2F%2Fcdn.test%2Ffrieren-3.vtt" || hint.Subtitle != "https://cdn.test/frieren-3.vtt" {
		t.Fatalf("hint = %+v", hint)
	}
}

func TestGetEpisodeURLHardSubsUsesProxy(t *testing.T) {
	web := newFakeWeb(t)
	fixedSubStyle(t, "hard")
	web.text("anineko.test/watch/frieren/ep-3", watchFixture(t))
	serveVibeStream(web, "/e/hard3")

	links, hints, err := (&Provider{}).GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "frieren", 3, "sub")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || !strings.HasSuffix(links[0], "/master.m3u8") || hints[links[0]].Referrer != "http://vibeplayer.test/e/hard3" {
		t.Fatalf("links = %q, hints = %+v", links, hints)
	}
}

func TestGetEpisodeURLDubSkipsUnknownHosts(t *testing.T) {
	web := newFakeWeb(t)
	web.text("anineko.test/watch/frieren/ep-3", watchFixture(t))
	web.text("bibiemb.test/e/dub3", `const src = "http://cdn.test/dub/master.m3u8"`)
	web.text("cdn.test/dub/master.m3u8", "#EXTM3U\n#EXT-X-STREAM-INF:NAME=\"720p\"\n720.m3u8\n")

	links, err := (&Provider{}).GetEpisodeURLForMode(providers.PlaybackConfig{}, "frieren", 3, "DUB")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0] != "http://cdn.test/dub/720.m3u8" {
		t.Fatalf("links = %q", links)
	}
}

func TestGetEpisodeURLFailures(t *testing.T) {
	web := newFakeWeb(t)
	fixedSubStyle(t, "soft")
	web.text("anineko.test/watch/frieren/ep-3", watchFixture(t))
	web.text("anineko.test/watch/nodub/ep-1", `<div data-id="sub"><div data-video="http://bibiemb.test/e/x"></div></div>`)

	p := &Provider{}
	cases := []struct {
		id      string
		ep      int
		mode    string
		wantErr string
	}{
		{"", 1, "sub", "empty show id"},
		{"frieren", 0, "sub", "invalid episode number 0"},
		{"nodub", 1, "dub", "no dub streams found for episode 1"},
		// Both soft servers fail: the embeds are not served.
		{"frieren", 3, "sub", "no playable streams resolved for episode 3"},
		{"missing", 1, "sub", "anineko request failed with status 404"},
	}
	for _, tc := range cases {
		_, _, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, tc.id, tc.ep, tc.mode)
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s ep %d %s: err = %v, want %q", tc.id, tc.ep, tc.mode, err, tc.wantErr)
		}
	}
}

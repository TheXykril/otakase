package animex

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/thexykril/otakase/internal/providers"
)

type stub struct {
	server  *httptest.Server
	sources atomic.Int32
}

// stubHost serves AniList and animex in the shapes the real ones use.
//
//	5114: 64 episodes on slug fma-v3mzo; yuki has sub and dub, zuna sub only,
//	      nero is listed but never asked.
//	1:    airing (episode 3 next) on slug airing-abc; yuki's dub is its sub
//	      stream behind a fresh token.
//	404:  not on animex.
func stubHost(t *testing.T) (*Provider, *stub) {
	t.Helper()
	s := &stub{}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/graphql":
			body, _ := io.ReadAll(r.Body)
			switch {
			case strings.Contains(string(body), "Page("):
				fmt.Fprint(w, `{"data":{"Page":{"media":[
{"id":5114,"idMal":5114,"title":{"romaji":"Hagane no Renkinjutsushi: FULLMETAL ALCHEMIST","english":"Fullmetal Alchemist: Brotherhood"},"format":"TV","seasonYear":2009,"episodes":64},
{"id":7,"idMal":null,"title":{"romaji":"No MAL entry","english":null},"format":"ONA","seasonYear":2024,"episodes":1}]}}}`)
			case strings.Contains(string(body), `"id":5114`):
				fmt.Fprint(w, `{"data":{"Media":{"episodes":64,"status":"FINISHED","nextAiringEpisode":null}}}`)
			case strings.Contains(string(body), `"id":1}`):
				fmt.Fprint(w, `{"data":{"Media":{"episodes":null,"status":"RELEASING","nextAiringEpisode":{"episode":3}}}}`)
			default:
				fmt.Fprint(w, `{"errors":[{"message":"Not Found.","status":404}],"data":{"Media":null}}`)
			}
		case r.URL.Path == "/anime/5114/__data.json":
			fmt.Fprint(w, `{"type":"data","nodes":[null,{"type":"data","data":[{"animeInfo":1,"anilistId":2},{"id":2,"slug":4,"title":3},5114,"Fullmetal Alchemist: Brotherhood","fma-v3mzo"],"uses":{}}]}`)
		case r.URL.Path == "/anime/1/__data.json":
			fmt.Fprint(w, `{"type":"data","nodes":[null,{"type":"data","data":[{"animeInfo":1},{"slug":2},"airing-abc"]}]}`)
		case strings.HasSuffix(r.URL.Path, "/__data.json"):
			fmt.Fprint(w, `{"type":"data","nodes":[null,{"type":"data","data":[{"animeInfo":-1}]}]}`)
		case r.URL.Path == "/rest/api/servers":
			if q.Get("epNum") == "999" {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"error":"episode servers not found"}`)
				return
			}
			fmt.Fprint(w, `{"subProviders":[{"id":"nero"},{"id":"yuki"},{"id":"zuna"}],"dubProviders":[{"id":"nero"},{"id":"yuki"}]}`)
		case r.URL.Path == "/rest/api/sources":
			s.sources.Add(1)
			server, lang, slug := q.Get("providerId"), q.Get("type"), q.Get("id")
			if server == "nero" {
				t.Errorf("nero was asked")
			}
			video := lang
			if slug == "airing-abc" {
				video = "sub"
			}
			token := s.sources.Load()
			switch server {
			case "yuki":
				fmt.Fprintf(w, `{"sources":[{"url":"https://yuki.invalid/anime/%s/%s/master.m3u8?token=%d","type":"video/mpegurl"}],
"tracks":[{"url":"https://yuki.invalid/preview/thumbnails.vtt","lang":"","label":"","kind":"captions"},
{"url":"https://yuki.invalid/subtitles/ara-17.vtt","lang":"arabic (arabic - [full])","label":"Arabic (Arabic - [Full])","kind":"captions"},
{"url":"https://yuki.invalid/subtitles/eng-4.vtt","lang":"english (english - [full])","label":"English (English - [Full])","kind":"captions","default":true}],
"chapters":[{"title":"Intro","start":60,"end":146},{"title":"Outro","start":1340,"end":1432}],
"headers":{"Referer":"https://yuki-ref.invalid/","Origin":"https://yuki-ref.invalid"}}`, slug, video, token)
			case "zuna":
				if lang == "dub" {
					w.WriteHeader(http.StatusNotFound)
					fmt.Fprint(w, `{"error":"requested type not available"}`)
					return
				}
				fmt.Fprintf(w, `{"sources":[{"url":"https://zuna.invalid/p/%s/master.m3u8","type":"video/mpegurl"}],
"tracks":[{"url":"https://zuna.invalid/subs/en.vtt","lang":"en","label":"English","kind":"captions","default":true}],
"chapters":[],"headers":{"Referer":"https://zuna-ref.invalid/"}}`, slug)
			default:
				fmt.Fprint(w, `{"error":"unknown provider"}`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.server.Close)
	c := newClient()
	c.site = s.server.URL
	c.api = s.server.URL
	c.anilist = s.server.URL + "/graphql"
	return &Provider{client: c}, s
}

func TestSearchKeysByAnilistIDAndCarriesMalID(t *testing.T) {
	p, _ := stubHost(t)
	got, err := p.SearchAnime("fullmetal", "sub")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key != "5114" || got[0].Title != "Fullmetal Alchemist: Brotherhood" {
		t.Fatalf("results: %+v", got)
	}
	if hit, ok := got[0].ExtraData.(providers.SearchHit); !ok || hit.MalID != 5114 || hit.Episodes != 64 {
		t.Errorf("search hit: %+v", got[0].ExtraData)
	}
	if got[1].Key != "7" || got[1].Label != "No MAL entry (ONA, 2024)" {
		t.Errorf("show without a MAL id: %+v", got[1])
	}
}

func TestEpisodesListCountsAiredEpisodes(t *testing.T) {
	p, _ := stubHost(t)
	eps, err := p.EpisodesList("5114", "sub")
	if err != nil || len(eps) != 64 || eps[0] != "1" || eps[63] != "64" {
		t.Fatalf("finished show: %d episodes, %v", len(eps), err)
	}
	eps, err = p.EpisodesList("1", "sub")
	if err != nil || len(eps) != 2 {
		t.Fatalf("airing show: %v, %v", eps, err)
	}
	if _, err := p.EpisodesList("404", "sub"); err == nil || !strings.Contains(err.Error(), "not on animex") {
		t.Errorf("show not on animex: %v", err)
	}
	if _, err := p.EpisodesList("abc", "sub"); err == nil {
		t.Error("bad id accepted")
	}
}

func TestSubStreamsFromEachServerWithTheirHeaders(t *testing.T) {
	p, _ := stubHost(t)
	urls, hints, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "5114", 1, "sub")
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 2 || !strings.HasPrefix(urls[0], "https://yuki.invalid/anime/fma-v3mzo/sub/master.m3u8") || urls[1] != "https://zuna.invalid/p/fma-v3mzo/master.m3u8" {
		t.Fatalf("urls: %v", urls)
	}
	yuki := hints[urls[0]]
	if yuki.Referrer != "https://yuki-ref.invalid/" || yuki.Headers["Origin"] != "https://yuki-ref.invalid" || yuki.Headers["User-Agent"] == "" {
		t.Errorf("yuki headers: %+v", yuki)
	}
	if yuki.Subtitle != "https://yuki.invalid/subtitles/eng-4.vtt" {
		t.Errorf("yuki subtitle: %q", yuki.Subtitle)
	}
	var langs []string
	for _, s := range yuki.Subtitles {
		langs = append(langs, s.Language)
	}
	if !slices.Equal(langs, []string{"Arabic", "English"}) {
		t.Errorf("yuki tracks (thumbnails must be left out): %v", langs)
	}
	if yuki.HLS {
		t.Error("a .m3u8 address needs no HLS mark")
	}
	if z := hints[urls[1]]; z.Referrer != "https://zuna-ref.invalid/" || z.Subtitle != "https://zuna.invalid/subs/en.vtt" {
		t.Errorf("zuna hint: %+v", z)
	}
}

func TestDubOnlyFromServersThatListItAndNotLoadingSubtitles(t *testing.T) {
	p, _ := stubHost(t)
	urls, hints, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "5114", 1, "dub")
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 1 || !strings.Contains(urls[0], "/fma-v3mzo/dub/") {
		t.Fatalf("urls: %v", urls)
	}
	if h := hints[urls[0]]; h.Subtitle != "" || len(h.Subtitles) == 0 {
		t.Errorf("dub subtitles should be offered, not loaded: %+v", h)
	}
}

func TestDubThatIsTheSubStreamIsRefused(t *testing.T) {
	p, _ := stubHost(t)
	if _, err := p.GetEpisodeURLForMode(providers.PlaybackConfig{}, "1", 1, "dub"); err == nil || !strings.Contains(err.Error(), "the dub is the sub stream") {
		t.Fatalf("fake dub: %v", err)
	}
	if _, err := p.EpisodesList("1", "dub"); err == nil {
		t.Error("show without a real dub listed in dub")
	}
}

func TestMissingEpisodeIsAnError(t *testing.T) {
	p, _ := stubHost(t)
	if _, err := p.GetEpisodeURL(providers.PlaybackConfig{SubOrDub: "sub"}, "5114", 999); err == nil || !strings.Contains(err.Error(), "episode servers not found") {
		t.Fatalf("missing episode: %v", err)
	}
}

func TestSkipRangeFromChapters(t *testing.T) {
	p, _ := stubHost(t)
	intro, outro, err := p.SkipRange("5114", "sub", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(intro, []int{60, 146}) || !slices.Equal(outro, []int{1340, 1432}) {
		t.Errorf("intro %v outro %v", intro, outro)
	}
}

func TestSourcesAreCachedForAPlay(t *testing.T) {
	p, s := stubHost(t)
	for range 3 {
		if _, err := p.GetEpisodeURL(providers.PlaybackConfig{SubOrDub: "sub"}, "5114", 1); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := p.SkipRange("5114", "sub", 1); err != nil {
		t.Fatal(err)
	}
	if n := s.sources.Load(); n != 2 {
		t.Errorf("source requests: got %d, want 2 (one per server)", n)
	}
}

func TestSlugFromData(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{"info before slug", `{"nodes":[null,{"type":"data","data":[{"animeInfo":1},{"slug":2},"a-b"]}]}`, "a-b"},
		{"skip node without info", `{"nodes":[{"type":"data","data":[{"x":1},2]},{"type":"data","data":[{"animeInfo":1},{"slug":2},"c"]}]}`, "c"},
		{"index out of range", `{"nodes":[{"type":"data","data":[{"animeInfo":1},{"slug":9}]}]}`, ""},
		{"slug not a string", `{"nodes":[{"type":"data","data":[{"animeInfo":1},{"slug":2},5]}]}`, ""},
		{"not json", `<html>`, ""},
	} {
		got, err := slugFromData([]byte(tc.body))
		if got != tc.want || (tc.want == "" && err == nil) {
			t.Errorf("%s: got %q, %v", tc.name, got, err)
		}
	}
}

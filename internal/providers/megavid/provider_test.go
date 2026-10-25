package megavid

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/thexykril/otakase/internal/providers"
)

type stub struct {
	server  *httptest.Server
	sources atomic.Int32
	tokens  atomic.Int32
}

func writeSource(w http.ResponseWriter, src map[string]any) {
	_ = json.NewEncoder(w).Encode(src)
}

// stubHost serves AniList and megavid in the shapes the real ones use.
//
//	52991: 28 episodes, sub and dub, both copies.
//	63157: airing (episode 5 next), its dub is the sub stream behind a
//	       per-request token, as megavid answers a show with no dub.
func stubHost(t *testing.T) (*Provider, *stub) {
	t.Helper()
	previous := lookupHost
	lookupHost = func(_ context.Context, host string) ([]string, error) {
		if host == "gone.invalid" {
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
		return []string{"192.0.2.1"}, nil
	}
	t.Cleanup(func() { lookupHost = previous })
	s := &stub{}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/graphql" {
			body, _ := io.ReadAll(r.Body)
			switch {
			case strings.Contains(string(body), "Page("):
				fmt.Fprint(w, `{"data":{"Page":{"media":[
{"idMal":52991,"title":{"romaji":"Sousou no Frieren","english":"Frieren: Beyond Journey's End"},"format":"TV","seasonYear":2023,"episodes":28},
{"idMal":null,"title":{"romaji":"No MAL entry","english":null},"format":"ONA","seasonYear":2024,"episodes":1},
{"idMal":59978,"title":{"romaji":"Sousou no Frieren 2nd Season","english":null},"format":"TV","seasonYear":2026,"episodes":10}]}}}`)
			case strings.Contains(string(body), `"idMal":52991`):
				fmt.Fprint(w, `{"data":{"Media":{"episodes":28,"status":"FINISHED","nextAiringEpisode":null}}}`)
			case strings.Contains(string(body), `"idMal":63157`):
				fmt.Fprint(w, `{"data":{"Media":{"episodes":null,"status":"RELEASING","nextAiringEpisode":{"episode":5}}}}`)
			case strings.Contains(string(body), `"idMal":429`):
				fmt.Fprint(w, `{"errors":[{"message":"Too Many Requests.","status":429}],"data":null}`)
			default:
				fmt.Fprint(w, `{"errors":[{"message":"Not Found.","status":404}],"data":{"Media":null}}`)
			}
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/source") {
			http.NotFound(w, r)
			return
		}
		s.sources.Add(1)
		own := r.URL.Query().Get("noax") == "1"
		switch {
		case strings.HasPrefix(r.URL.Path, "/mal/52991/1/"):
			lang := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/mal/52991/1/"), "/source")
			stream := "https://relay.invalid/" + lang + "/master.txt"
			if r.URL.Query().Get("gone") == "1" {
				stream = "https://gone.invalid/" + lang + "/master.txt"
			}
			if own {
				stream = "https://own.invalid/" + lang + "/playlist.m3u8"
			}
			writeSource(w, map[string]any{
				"status": "ok", "source": stream, "type": "hls",
				"tracks": []map[string]any{
					{"file": "https://subs.invalid/release.vtt", "label": "Subtitles", "kind": "captions"},
					{"file": "https://subs.invalid/ar.vtt", "label": "Arabic", "kind": "captions"},
					{"file": "https://subs.invalid/en.vtt", "label": "English", "kind": "captions"},
					{"file": "https://subs.invalid/thumbs.vtt", "kind": "thumbnails"},
				},
				"chapters": []map[string]any{{"title": "Intro", "start": 0, "end": 89.4}, {"title": "Outro", "start": 1460, "end": 1549}},
			})
		case strings.HasPrefix(r.URL.Path, "/mal/63157/1/"):
			n := s.tokens.Add(1)
			writeSource(w, map[string]any{
				"status": "ok", "type": "hls", "tracks": []any{}, "chapters": []any{},
				"source": fmt.Sprintf("https://megavid.invalid/vid/VIDEO63157/token%d", n),
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			writeSource(w, map[string]any{"status": "missing", "code": 404, "message": "We couldn't find this episode."})
		}
	}))
	t.Cleanup(s.server.Close)
	return &Provider{client: &client{base: s.server.URL, anilist: s.server.URL + "/graphql", http: s.server.Client()}}, s
}

func TestSearchIsKeyedByMALID(t *testing.T) {
	p, _ := stubHost(t)
	options, err := p.SearchAnime("frieren", "sub")
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 2 {
		t.Fatalf("a result without a MAL id should be dropped, got %+v", options)
	}
	first := options[0]
	if first.Key != "52991" || first.Title != "Frieren: Beyond Journey's End" || first.Label != "Frieren: Beyond Journey's End (TV, 2023)" {
		t.Errorf("first option = %+v", first)
	}
	if hit, ok := first.ExtraData.(providers.SearchHit); !ok || hit.MalID != 52991 || hit.Episodes != 28 {
		t.Errorf("the MAL id must travel with the result so the app can match it exactly: %#v", first.ExtraData)
	}
	if options[1].Title != "Sousou no Frieren 2nd Season" {
		t.Errorf("a show without an English title should use its romaji one: %+v", options[1])
	}
}

func TestEpisodesFollowAiring(t *testing.T) {
	p, _ := stubHost(t)
	all, err := p.EpisodesList("52991", "sub")
	if err != nil || len(all) != 28 || all[0] != "1" || all[27] != "28" {
		t.Errorf("finished show: %v, %v", all, err)
	}
	airing, err := p.EpisodesList("63157", "sub")
	if err != nil || len(airing) != 4 {
		t.Errorf("an airing show lists what has aired (4 before episode 5): %v, %v", airing, err)
	}
	if _, err := p.EpisodesList("1", "sub"); err == nil {
		t.Error("a MAL id AniList does not know should be an error")
	}
	if _, err := p.EpisodesList("frieren-c6fbj", "sub"); err == nil {
		t.Error("a key that is not a MAL id should be an error")
	}
}

func TestRateLimitIsReportedAsSuch(t *testing.T) {
	p, _ := stubHost(t)
	_, err := p.EpisodesList("429", "sub")
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("err = %v, want the rate limit error", err)
	}
}

func TestStreamsOwnCopyFirst(t *testing.T) {
	p, _ := stubHost(t)
	urls, hints, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "52991", 1, "sub")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://own.invalid/sub/playlist.m3u8", "https://relay.invalid/sub/master.txt"}
	if strings.Join(urls, " ") != strings.Join(want, " ") {
		t.Fatalf("urls = %v, want %v", urls, want)
	}
	hint := hints[urls[0]]
	if hint.Headers["Referer"] != referer || hint.Headers["User-Agent"] == "" {
		t.Errorf("headers = %v", hint.Headers)
	}
	if !hint.HLS {
		t.Error("every copy is a playlist and must say so: some are named /vid/<video>/<token>")
	}
	if hint.Subtitle != "https://subs.invalid/en.vtt" {
		t.Errorf("subtitle = %q, want the English track", hint.Subtitle)
	}
	if len(hint.Subtitles) != 3 {
		t.Errorf("tracks = %+v, want the three captions and not the thumbnails", hint.Subtitles)
	}
	if hint.Subtitles[0].Language != "English" {
		t.Errorf("the unnamed release track is English: %+v", hint.Subtitles[0])
	}
}

func TestADubIsNotPreloadedWithSubtitles(t *testing.T) {
	p, _ := stubHost(t)
	urls, hints, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "52991", 1, "dub")
	if err != nil {
		t.Fatal(err)
	}
	if urls[0] != "https://own.invalid/dub/playlist.m3u8" {
		t.Errorf("urls = %v", urls)
	}
	if hints[urls[0]].Subtitle != "" || len(hints[urls[0]].Subtitles) == 0 {
		t.Errorf("a dub should offer captions without loading one: %+v", hints[urls[0]])
	}
}

// megavid answers a dub request for a show with no dub with the sub stream.
// Played, that is Japanese audio labelled dub, so it has to be refused --
// even though each answer carries a fresh token and the URLs never match.
func TestASubStreamUnderADubLabelIsRefused(t *testing.T) {
	p, _ := stubHost(t)
	if links, _, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "63157", 1, "dub"); err == nil {
		t.Fatalf("got %v for a show with no dub", links)
	}
	if _, err := p.EpisodesList("63157", "dub"); err == nil {
		t.Error("the dub episode list of a show with no dub should be an error")
	}
	urls, _, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "63157", 1, "sub")
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 1 {
		t.Errorf("both copies name the same video and should collapse to one: %v", urls)
	}
}

func TestAMissingEpisodeIsAnError(t *testing.T) {
	p, _ := stubHost(t)
	_, _, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "52991", 99, "sub")
	if err == nil || !strings.Contains(err.Error(), "couldn't find") {
		t.Errorf("err = %v, want megavid's own message", err)
	}
}

func TestSkipRangeComesFromChapters(t *testing.T) {
	p, s := stubHost(t)
	if _, _, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "52991", 1, "sub"); err != nil {
		t.Fatal(err)
	}
	before := s.sources.Load()
	intro, outro, err := p.SkipRange("52991", "sub", 1)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(intro) != "[0 89]" || fmt.Sprint(outro) != "[1460 1549]" {
		t.Errorf("intro=%v outro=%v", intro, outro)
	}
	if s.sources.Load() != before {
		t.Error("the skip range should reuse the stream just resolved")
	}
}

func TestStreamIdentity(t *testing.T) {
	a := streamIdentity("https://megavid.buzz/vid/VIDEO/tokenA")
	b := streamIdentity("https://megavid.buzz/vid/VIDEO/tokenB")
	if a != b {
		t.Errorf("tokens should not change the identity: %q vs %q", a, b)
	}
	if streamIdentity("https://cp.megavid.buzz/hls/x/playlist.m3u8") == streamIdentity("https://cp.megavid.buzz/hls/y/playlist.m3u8") {
		t.Error("different videos must differ")
	}
}

func TestValidSpan(t *testing.T) {
	for _, span := range [][2]float64{{90, 90}, {120, 30}, {-5, 10}} {
		if got := validSpan(span[0], span[1]); got != nil {
			t.Errorf("%v should be rejected, got %v", span, got)
		}
	}
}

// A copy on a host with no DNS records is left out rather than handed to the
// player to fail on.
func TestACopyOnAMissingHostIsLeftOut(t *testing.T) {
	p, _ := stubHost(t)
	previous := copies
	copies = []string{"noax=1", "gone=1"}
	t.Cleanup(func() { copies = previous })

	urls, _, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "52991", 1, "sub")
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 1 || urls[0] != "https://own.invalid/sub/playlist.m3u8" {
		t.Errorf("urls = %v, want only the copy whose host exists", urls)
	}
}

package anikoto

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/thexykril/otakase/internal/providers"
)

// stubHost serves the three endpoints with the shapes the real one returns.
func stubHost(t *testing.T) *Provider {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/search":
			_ = json.NewEncoder(w).Encode(searchResponse{Results: []searchResult{
				{ID: 154587, Name: "Frieren", Format: "TV", Year: 2023},
				{ID: 0, Name: "broken"},
			}})
		case strings.HasPrefix(r.URL.Path, "/episodes/"):
			_ = json.NewEncoder(w).Encode([]episode{
				{ID: "watch/anikoto/154587/sub/2", Number: 2, Category: "sub"},
				{ID: "watch/anikoto/154587/sub/1", Number: 1, Category: "sub"},
				{ID: "watch/anikoto/154587/dub/1", Number: 1, Category: "dub"},
				{ID: "dupe", Number: 1, Category: "sub"},
			})
		case r.URL.Path == "/link":
			_ = json.NewEncoder(w).Encode(linkResponse{
				Streams: []stream{
					{Type: "hls", URL: "https://cdn.invalid/backup.m3u8", Quality: "720p", Server: "HD-1", Priority: 2},
					{Type: "hls", URL: "https://cdn.invalid/main.m3u8", Quality: "1080p", Server: "Vidstream-2",
						Priority: 1, Default: true, Referer: "https://megaplay.buzz/",
						HTTPHeaders: map[string]string{"Referer": "https://megaplay.buzz/", "User-Agent": "Mozilla/5.0"}},
				},
				Subtitles: []subtitle{
					{File: "https://cdn.invalid/jp.vtt", Label: "Japanese", Language: "japanese"},
					{File: "https://cdn.invalid/en.vtt", Label: "English", Language: "english", Default: true},
				},
				Intro: []int{0, 89},
				Outro: []int{1460, 1549},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return &Provider{client: &client{baseURL: server.URL, http: server.Client()}}
}

func TestSearchLabelsSeasonsDistinguishably(t *testing.T) {
	options, err := stubHost(t).SearchAnime("frieren", "sub")
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 1 {
		t.Fatalf("a result with no id should be dropped, got %d options", len(options))
	}
	if options[0].Key != "154587" {
		t.Errorf("the key should be the AniList id, got %q", options[0].Key)
	}
	// A search for a long-running title returns seasons whose names barely
	// differ, so the label has to carry more than the name.
	if !strings.Contains(options[0].Label, "TV") || !strings.Contains(options[0].Label, "2023") {
		t.Errorf("label lacks format and year: %q", options[0].Label)
	}
}

// Sub and dub are separate entries for the same number. A show carried in only
// one language must report nothing for the other, rather than a list that
// cannot play.
func TestEpisodesAreFilteredByLanguage(t *testing.T) {
	p := stubHost(t)

	sub, err := p.EpisodesList("154587", "sub")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sub, ","); got != "1,2" {
		t.Errorf("sub episodes should be deduplicated and sorted, got %q", got)
	}

	dub, err := p.EpisodesList("154587", "dub")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(dub, ","); got != "1" {
		t.Errorf("dub should carry only its own episodes, got %q", got)
	}
}

// The CDN rejects requests without a referrer and a browser user-agent, and the
// host states both. Losing them means every segment returns 403.
func TestStreamHeadersSurviveToThePlayer(t *testing.T) {
	urls, hints, err := stubHost(t).GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "154587", 1, "sub")
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 2 {
		t.Fatalf("expected both streams, got %d", len(urls))
	}
	// The host's own preference comes first, so falling back goes the right way.
	if !strings.Contains(urls[0], "main.m3u8") {
		t.Errorf("the default stream should be first, got %q", urls[0])
	}

	hint := hints[urls[0]]
	if hint.Headers["Referer"] != "https://megaplay.buzz/" {
		t.Errorf("referer did not survive: %+v", hint.Headers)
	}
	if hint.Headers["User-Agent"] == "" {
		t.Error("the user-agent the CDN requires was dropped")
	}
	if hint.Subtitle != "https://cdn.invalid/en.vtt" {
		t.Errorf("expected the default English track, got %q", hint.Subtitle)
	}
}

// A stream without stated headers must still carry a referrer, or it 403s.
func TestStreamWithoutStatedHeadersStillGetsAReferrer(t *testing.T) {
	_, hints, err := stubHost(t).GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "154587", 1, "sub")
	if err != nil {
		t.Fatal(err)
	}
	hint := hints["https://cdn.invalid/backup.m3u8"]
	if hint.Headers["Referer"] == "" {
		t.Error("a stream with no stated headers was left without a referrer")
	}
}

// Skip ranges come back from the same request that resolves the stream.
func TestSkipRangeIsReadFromTheStreamResponse(t *testing.T) {
	intro, outro, err := stubHost(t).SkipRange("154587", "sub", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(intro) != 2 || intro[0] != 0 || intro[1] != 89 {
		t.Errorf("intro not read correctly: %v", intro)
	}
	if len(outro) != 2 || outro[0] != 1460 {
		t.Errorf("outro not read correctly: %v", outro)
	}
}

// A malformed span would seek the player somewhere wrong, which is worse than
// not skipping.
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

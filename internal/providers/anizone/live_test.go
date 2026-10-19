package anizone

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/thexykril/otakase/internal/providerhost"
	"github.com/thexykril/otakase/internal/providers"
)

// Live tests talk to the real host, so they are gated on an environment
// variable and are not part of any build: a provider being down must not fail
// a build, which is the rule the rest of this project's live tests follow.
func liveOrSkip(t *testing.T) {
	t.Helper()
	if os.Getenv("OTAKASE_LIVE_ANIZONE") == "" {
		t.Skip("set OTAKASE_LIVE_ANIZONE=1 to run tests that fetch from anizone.to")
	}
	if providerhost.HTTPClient == nil {
		providerhost.HTTPClient = func() *http.Client { return &http.Client{Timeout: 30 * time.Second} }
	}
}

// The whole provider, end to end: a search that finds a known show, an
// episode list, and a stream URL that the site will actually serve.
func TestLiveResolvesAnEpisode(t *testing.T) {
	liveOrSkip(t)
	provider := &Provider{}

	results, err := provider.SearchAnime("frieren", "sub")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("search found nothing for a show anizone certainly carries")
	}
	t.Logf("search returned %d results, first: %s (%s)", len(results), results[0].Title, results[0].Key)

	episodes, err := provider.EpisodesList(results[0].Key, "sub")
	if err != nil {
		t.Fatalf("episode list: %v", err)
	}
	if len(episodes) == 0 {
		t.Fatal("no episodes listed")
	}
	t.Logf("%d episodes listed", len(episodes))

	links, hints, err := provider.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{SubOrDub: "sub"}, results[0].Key, 1, "sub")
	if err != nil {
		t.Fatalf("episode url: %v", err)
	}
	if len(links) == 0 || !strings.Contains(links[0], ".m3u8") {
		t.Fatalf("expected an HLS stream, got %v", links)
	}
	t.Logf("stream: %s", links[0])

	hint := hints[links[0]]
	if hint.Referrer == "" {
		t.Error("no referrer hint: the CDN will reject the stream without one")
	}
	if hint.Subtitle == "" {
		t.Error("no subtitle track: anizone is softsub only, so one is expected")
	} else {
		t.Logf("subtitle: %s", hint.Subtitle)
	}
}

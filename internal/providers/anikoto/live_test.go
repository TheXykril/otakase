package anikoto

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/thexykril/otakase/internal/providers"
)

// TestAnikotoLive exercises the real host end to end: search, episode list,
// stream resolution, and then actually fetching the manifest with the headers
// the host says it needs. Resolving a URL is not the same as it playing, and
// this project has been caught by that difference before.
//
// Guarded because it depends on a third party being up, which cannot gate a build.
func TestAnikotoLive(t *testing.T) {
	if os.Getenv("CURD_LIVE_ANIKOTO") != "1" {
		t.Skip("set CURD_LIVE_ANIKOTO=1 to run against the real host")
	}
	p := &Provider{}

	results, err := p.SearchAnime("frieren", "sub")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	t.Logf("search returned %d results, first: %s (%s)", len(results), results[0].Label, results[0].Key)

	showID := results[0].Key
	episodes, err := p.EpisodesList(showID, "sub")
	if err != nil {
		t.Fatalf("episodes: %v", err)
	}
	t.Logf("%s has %d sub episodes", showID, len(episodes))

	if dub, err := p.EpisodesList(showID, "dub"); err == nil {
		t.Logf("%s has %d dub episodes", showID, len(dub))
	} else {
		t.Logf("no dub: %v", err)
	}

	urls, hints, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, showID, 1, "sub")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	t.Logf("resolved %d streams, first: %.70s", len(urls), urls[0])

	hint := hints[urls[0]]
	t.Logf("headers: %v, subtitle: %.60s", hint.Headers, hint.Subtitle)

	if intro, outro, err := p.SkipRange(showID, "sub", 1); err == nil {
		t.Logf("skip ranges: intro=%v outro=%v", intro, outro)
	}

	// The manifest has to be reachable with those headers, or the stream is
	// only theoretically playable.
	req, err := http.NewRequest(http.MethodGet, urls[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range hint.Headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("fetching the manifest: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("manifest returned %d, so the stream would not play", resp.StatusCode)
	}
	if !strings.Contains(string(body), "#EXTM3U") {
		t.Fatalf("what came back is not an HLS manifest: %.120s", body)
	}
	t.Logf("manifest fetched and valid (%d bytes read)", len(body))
}

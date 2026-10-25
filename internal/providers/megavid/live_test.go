package megavid

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/thexykril/otakase/internal/providers"
)

// TestMegavidLive goes through search, the episode list, stream resolution and
// fetching the manifest with the headers given for it.
//
// Guarded because it depends on a third party being up, which cannot gate a build.
func TestMegavidLive(t *testing.T) {
	if os.Getenv("OTAKASE_LIVE_MEGAVID") != "1" {
		t.Skip("set OTAKASE_LIVE_MEGAVID=1 to run against the real host")
	}
	p := &Provider{}

	results, err := p.SearchAnime("frieren", "sub")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	t.Logf("search returned %d results, first: %s (%s)", len(results), results[0].Label, results[0].Key)
	showID := "52991" // Frieren

	episodes, err := p.EpisodesList(showID, "sub")
	if err != nil {
		t.Fatalf("episodes: %v", err)
	}
	t.Logf("%s has %d episodes", showID, len(episodes))
	if dub, err := p.EpisodesList(showID, "dub"); err != nil {
		t.Errorf("Frieren has a dub: %v", err)
	} else {
		t.Logf("%d dub episodes", len(dub))
	}

	for _, mode := range []string{"sub", "dub"} {
		urls, hints, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, showID, 1, mode)
		if err != nil {
			t.Fatalf("resolve %s: %v", mode, err)
		}
		t.Logf("%s: %d streams, first %.80s, subtitle %.60s", mode, len(urls), urls[0], hints[urls[0]].Subtitle)

		req, _ := http.NewRequest(http.MethodGet, urls[0], nil)
		for key, value := range hints[urls[0]].Headers {
			req.Header.Set(key, value)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("fetching the %s manifest: %v", mode, err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "#EXTM3U") {
			t.Fatalf("%s manifest: status %d, %.120s", mode, resp.StatusCode, body)
		}
	}

	intro, outro, err := p.SkipRange(showID, "sub", 1)
	t.Logf("skip ranges: intro=%v outro=%v err=%v", intro, outro, err)

	// Keroro Gunsou☆ (2026) has no English dub; megavid answers the dub with
	// the sub stream, which must be refused.
	if _, _, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "63157", 1, "dub"); err == nil {
		t.Error("a show with no dub resolved a dub stream")
	}
}

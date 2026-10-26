package animex

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/thexykril/otakase/internal/providers"
)

// TestAnimexLive goes through search, the episode list, stream resolution and
// fetching each manifest with the headers given for it, in sub and dub.
//
// Guarded because it depends on a third party being up, which cannot gate a build.
func TestAnimexLive(t *testing.T) {
	if os.Getenv("OTAKASE_LIVE_ANIMEX") != "1" {
		t.Skip("set OTAKASE_LIVE_ANIMEX=1 to run against the real host")
	}
	p := &Provider{}

	results, err := p.SearchAnime("fullmetal alchemist brotherhood", "sub")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	t.Logf("search returned %d results, first: %s (%s)", len(results), results[0].Label, results[0].Key)
	showID := "5114" // Fullmetal Alchemist: Brotherhood

	episodes, err := p.EpisodesList(showID, "sub")
	if err != nil {
		t.Fatalf("episodes: %v", err)
	}
	t.Logf("%d episodes", len(episodes))

	for _, mode := range []string{"sub", "dub"} {
		urls, hints, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, showID, 1, mode)
		if err != nil {
			t.Errorf("%s: %v", mode, err)
			continue
		}
		played := 0
		for _, u := range urls {
			hint := hints[u]
			req, _ := http.NewRequest(http.MethodGet, u, nil)
			for k, v := range hint.Headers {
				req.Header.Set(k, v)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Logf("%s %s: %v", mode, u, err)
				continue
			}
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			ok := resp.StatusCode == http.StatusOK && strings.HasPrefix(strings.TrimSpace(string(body)), "#EXTM3U")
			t.Logf("%s %s: HTTP %d, playlist %v, %d subtitle tracks", mode, u, resp.StatusCode, ok, len(hint.Subtitles))
			if ok {
				played++
			}
		}
		if played == 0 {
			t.Errorf("%s: no server answered with a playlist", mode)
		}
	}

	intro, outro, err := p.SkipRange(showID, "sub", 1)
	t.Logf("skip: intro %v outro %v err %v", intro, outro, err)
}

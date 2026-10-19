package movies

import (
	"os"
	"testing"
)

// Live tests talk to the real site and video hosts, so they are gated on an
// environment variable like the rest of this project's live tests: the site
// being down must not fail a build.
func liveOrSkip(t *testing.T) {
	t.Helper()
	if os.Getenv("OTAKASE_LIVE_MOVIES") == "" {
		t.Skip("set OTAKASE_LIVE_MOVIES=1 to run tests that fetch from 8filmai")
	}
}

// End to end: a search through the entry domain, a movie page, and a stream
// from at least one of its servers.
func TestLiveResolvesAMovie(t *testing.T) {
	liveOrSkip(t)
	site := NewSite(DefaultSiteURL, "", nil)
	found, err := site.Search("alkis")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 {
		t.Fatal("search found nothing")
	}
	page, err := site.Page(found[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: servers %q", page.Label(), page.Servers)
	for _, source := range site.Sources(page) {
		stream, err := source.Resolve()
		if err != nil {
			t.Logf("%s: %v", source.Server, err)
			continue
		}
		t.Logf("%s: %s", source.Server, stream.URL)
		return
	}
	t.Fatal("no server resolved")
}

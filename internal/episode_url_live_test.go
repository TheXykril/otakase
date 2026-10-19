package internal

import (
	"os"
	"strings"
	"testing"
)

// marchLionAnikotoID is March Comes in Like a Lion on anikoto.
const marchLionAnikotoID = "NPpCgzb2MbnpiyxZ3"

// liveAnikotoEpisode resolves episode 1 of marchLionAnikotoID live, in mode.
func liveAnikotoEpisode(t *testing.T, mode string) ([]string, error) {
	t.Helper()
	if os.Getenv("OTAKASE_LIVE_ALLANIME_TEST") != "1" {
		t.Skip("set OTAKASE_LIVE_ALLANIME_TEST=1")
	}
	withAllProvidersEnabledForTest(t)

	anikoto, err := ProviderByName("anikoto")
	if err != nil {
		t.Fatalf("anikoto provider: %v", err)
	}
	byMode, ok := anikoto.(ProviderModeResolver)
	if !ok {
		t.Fatal("anikoto does not resolve by audio")
	}
	return byMode.GetEpisodeURLForMode(Config{SubOrDub: mode}, marchLionAnikotoID, 1, mode)
}

// The show has no playable dub there, and the fast4speed links it does list
// for one do not play; the lookup has to say so instead of returning them.
func TestAnikotoMarchLionDubEp1Live(t *testing.T) {
	links, err := liveAnikotoEpisode(t, "dub")
	if err != nil {
		return // the answer wanted: no dub
	}
	for _, link := range links {
		if strings.Contains(link, "fast4speed.rsvp") {
			t.Fatalf("dub should not return unreliable fast4speed links, got %s", link)
		}
	}
	t.Fatalf("expected dub lookup to fail without playable Anikoto sources, got links: %#v", links)
}

// The sub plays from one of the hosts known to work, never fast4speed.
func TestAnikotoMarchLionSubEp1Live(t *testing.T) {
	links, err := liveAnikotoEpisode(t, "sub")
	switch {
	case err != nil:
		t.Fatalf("resolving the sub: %v", err)
	case len(links) == 0:
		t.Fatal("the sub resolved to no links")
	}

	best := BestLink(links)
	if strings.Contains(best, "fast4speed.rsvp") {
		t.Fatalf("picked fast4speed over the wixstatic hosts: %s", best)
	}
	trusted := []string{"video.wixstatic.com", "repackager.wixmp.com", "sharepoint.com"}
	for _, host := range trusted {
		if strings.Contains(best, host) {
			return
		}
	}
	t.Fatalf("picked a link from no known host: %s", best)
}

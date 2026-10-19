package internal

import (
	"os"
	"testing"
)

// TestAnimeSkipAutoClientIDLive checks the assumption the whole feature rests
// on: that Anime-Skip still publishes a client id on its playground page, and
// that the id it publishes is accepted by the API.
//
// It is gated because it talks to the real service. Run it with
// OTAKASE_LIVE_ANIME_SKIP=1 when the page's shape is in question.
func TestAnimeSkipAutoClientIDLive(t *testing.T) {
	if os.Getenv("OTAKASE_LIVE_ANIME_SKIP") != "1" {
		t.Skip("set OTAKASE_LIVE_ANIME_SKIP=1 to check against the real service")
	}

	resolver := newAutoClientID(t.TempDir())
	clientID, err := resolver.ClientID()
	if err != nil {
		t.Fatalf("no client id published: %v", err)
	}
	t.Logf("published client id: %s", clientID)

	// Frieren, which Anime-Skip has timings for.
	times, found, err := newAnimeSkipSourceFrom(resolver).Lookup(SkipRef{AniListID: 154587, Episode: 1})
	if err != nil {
		t.Fatalf("the published id was not accepted: %v", err)
	}
	t.Logf("found=%v op=%+v ed=%+v", found, times.Op, times.Ed)
	if !found {
		t.Error("the id worked but no timings came back; the query may have drifted")
	}
}

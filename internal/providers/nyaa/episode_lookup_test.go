package nyaa

import (
	"slices"
	"testing"
	"time"
)

// seedFeed puts releases in the feed cache under query, so a lookup reads them
// instead of asking the index.
func seedFeed(t *testing.T, feed, query string, releases []Release) {
	t.Helper()
	feedMu.Lock()
	feedCache[feed+query] = cachedFeed{releases: releases, fetched: time.Now()}
	feedMu.Unlock()
	t.Cleanup(func() {
		feedMu.Lock()
		delete(feedCache, feed+query)
		feedMu.Unlock()
	})
}

// The feed holds only the newest releases. For Rokudou no Onna-tachi that was
// episodes 5 to 12 from half a dozen groups, so episode 1 was reported as not
// indexed while the index had several seeded copies of it.
func TestEpisodeMissingFromFeedIsSearchedByNumber(t *testing.T) {
	const show = "Rokudou no Onna-tachi"
	p := New()
	seedFeed(t, p.feed, show, []Release{
		parseSeeded("[SubsPlease] Rokudou no Onna-tachi - 12 (1080p) [1639C2DC].mkv", 40),
		parseSeeded("[SubsPlease] Rokudou no Onna-tachi - 05 (1080p) [00000000].mkv", 12),
	})
	seedFeed(t, p.feed, show+" 01", []Release{
		parseSeeded("[SubsPlease] Rokudou no Onna-tachi - 01 (1080p) [B01742B8].mkv", 4),
	})
	seedFeed(t, p.feed, show+" S01E01", nil)

	releases, err := p.releasesForEpisode(show, 1, "sub")
	if err != nil {
		t.Fatal(err)
	}
	if !hasEpisode(releases, 1) {
		t.Fatalf("episode 1 not found in %d releases", len(releases))
	}
}

// An episode the feed already carries costs no second request.
func TestEpisodeInFeedIsNotSearchedAgain(t *testing.T) {
	const show = "Saijo no Osewa"
	p := New()
	seedFeed(t, p.feed, show, subOnlySeason())

	releases, err := p.releasesForEpisode(show, 10, "sub")
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != len(subOnlySeason()) {
		t.Fatalf("got %d releases, want the feed's %d", len(releases), len(subOnlySeason()))
	}
}

func TestEpisodeQueriesNumberLikeReleaseGroups(t *testing.T) {
	cases := map[string][]string{
		"Show":            {"Show 01", "Show S01E01"},
		"Show S2":         {"Show 01", "Show S02E01"},
		"Show 2nd Season": {"Show 01", "Show S02E01"},
	}
	for id, want := range cases {
		if got := episodeQueries(id, 1); !slices.Equal(got, want) {
			t.Errorf("episodeQueries(%q, 1) = %q, want %q", id, got, want)
		}
	}
	if got := episodeQueries("Show", 101)[0]; got != "Show 101" {
		t.Errorf("episode 101 searched as %q", got)
	}
}

func hasEpisode(releases []Release, epNo int) bool {
	for _, release := range playableReleases(releases) {
		if release.Episode == epNo {
			return true
		}
	}
	return false
}

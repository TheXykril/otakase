package internal

import (
	"errors"
	"strings"
	"testing"
)

type stubSkipSource struct {
	name  string
	times SkipTimes
	found bool
	err   error
	calls *int
}

func (s stubSkipSource) Name() string { return s.name }
func (s stubSkipSource) Lookup(SkipRef) (SkipTimes, bool, error) {
	if s.calls != nil {
		*s.calls++
	}
	return s.times, s.found, s.err
}

func span(start, end int) Skip { return Skip{Start: start, End: end} }

// The point of a hybrid: AniSkip often knows an opening and not an ending, and
// a provider that ships timings often knows both. Taking each half from
// whichever source has it means a half-answer completes rather than being
// thrown away.
func TestSkipHalvesCanComeFromDifferentSources(t *testing.T) {
	openingOnly := stubSkipSource{name: "aniskip", found: true, times: SkipTimes{Op: span(0, 90)}}
	endingOnly := stubSkipSource{name: "anime-skip", found: true, times: SkipTimes{Ed: span(1300, 1400)}}

	got := ResolveSkipTimes(SkipRef{Episode: 1}, openingOnly, endingOnly)

	if got.Times.Op != span(0, 90) {
		t.Errorf("opening not taken from the first source: %+v", got.Times.Op)
	}
	if got.Times.Ed != span(1300, 1400) {
		t.Errorf("ending not taken from the second source: %+v", got.Times.Ed)
	}
	if got.OpSource != "aniskip" || got.EdSource != "anime-skip" {
		t.Errorf("attribution wrong: op=%s ed=%s", got.OpSource, got.EdSource)
	}
	if !strings.Contains(got.Describe(), "aniskip") || !strings.Contains(got.Describe(), "anime-skip") {
		t.Errorf("the summary should name both sources: %q", got.Describe())
	}
}

// The first source to answer wins; a later one must not overwrite it.
func TestEarlierSourcesWin(t *testing.T) {
	first := stubSkipSource{name: "provider", found: true, times: SkipTimes{Op: span(0, 89), Ed: span(1460, 1549)}}
	second := stubSkipSource{name: "aniskip", found: true, times: SkipTimes{Op: span(5, 100), Ed: span(1, 2)}}

	got := ResolveSkipTimes(SkipRef{Episode: 1}, first, second)
	if got.Times.Op != span(0, 89) || got.Times.Ed != span(1460, 1549) {
		t.Errorf("a later source overwrote an earlier answer: %+v", got.Times)
	}
}

// Once both halves are known there is nothing left to ask, and asking anyway
// costs a network round trip per episode.
func TestResolutionStopsOnceBothHalvesAreKnown(t *testing.T) {
	calls := 0
	complete := stubSkipSource{name: "provider", found: true, times: SkipTimes{Op: span(0, 89), Ed: span(1460, 1549)}}
	later := stubSkipSource{name: "aniskip", calls: &calls}

	ResolveSkipTimes(SkipRef{Episode: 1}, complete, later)
	if calls != 0 {
		t.Errorf("a later source was queried needlessly %d times", calls)
	}
}

// A service being down must not stop the next one being asked.
func TestAFailingSourceDoesNotStopTheChain(t *testing.T) {
	broken := stubSkipSource{name: "anime-skip", err: errors.New("service unavailable")}
	working := stubSkipSource{name: "aniskip", found: true, times: SkipTimes{Op: span(0, 90)}}

	got := ResolveSkipTimes(SkipRef{Episode: 1}, broken, working)
	if !got.Found() {
		t.Fatal("a failure in the first source lost the answer from the second")
	}
	if got.OpSource != "aniskip" {
		t.Errorf("expected the working source to supply the opening, got %q", got.OpSource)
	}
	if len(got.Errors) != 1 {
		t.Errorf("the failure should be recorded, got %v", got.Errors)
	}
}

// A span that is reversed, negative or empty would seek the player somewhere
// wrong. Not skipping is better than skipping to the wrong place.
func TestBrokenSpansAreRejected(t *testing.T) {
	junk := stubSkipSource{name: "junk", found: true, times: SkipTimes{
		Op: span(100, 30), // reversed
		Ed: span(-5, -1),  // negative
	}}
	good := stubSkipSource{name: "good", found: true, times: SkipTimes{Op: span(0, 90), Ed: span(1300, 1400)}}

	got := ResolveSkipTimes(SkipRef{Episode: 1}, junk, good)
	if got.OpSource != "good" || got.EdSource != "good" {
		t.Errorf("broken spans were accepted: op=%s ed=%s %+v", got.OpSource, got.EdSource, got.Times)
	}
}

// Knowing nothing is the ordinary case for most services and most episodes.
func TestNoSourceKnowingAnythingIsNotAnError(t *testing.T) {
	got := ResolveSkipTimes(SkipRef{Episode: 1},
		stubSkipSource{name: "a"}, stubSkipSource{name: "b"})

	if got.Found() {
		t.Error("nothing was found, yet it reported success")
	}
	if !strings.Contains(got.Describe(), "a, b") {
		t.Errorf("the summary should say what was tried: %q", got.Describe())
	}
	if len(got.Errors) != 0 {
		t.Errorf("knowing nothing is not an error: %v", got.Errors)
	}
}

// A nil source in the chain must not panic; configuration can leave gaps.
func TestNilSourcesAreSkipped(t *testing.T) {
	got := ResolveSkipTimes(SkipRef{Episode: 1}, nil,
		stubSkipSource{name: "aniskip", found: true, times: SkipTimes{Op: span(0, 90)}}, nil)
	if got.OpSource != "aniskip" {
		t.Errorf("a nil source broke the chain: %+v", got)
	}
}

// Without a client id, Anime-Skip cannot be asked at all -- and must say so
// quietly rather than erroring on every episode.
func TestAnimeSkipIsQuietWithoutAClientID(t *testing.T) {
	source := newAnimeSkipSource("")
	times, found, err := source.Lookup(SkipRef{AniListID: 154587, Episode: 1})
	if err != nil {
		t.Errorf("a missing client id should not be an error: %v", err)
	}
	if found || usableSpan(times.Op) {
		t.Error("it claimed to know something without being able to ask")
	}
}

// AniSkip keys on MyAnimeList ids; a show tracked only on AniList has none.
func TestAniSkipIsQuietWithoutAMalID(t *testing.T) {
	_, found, err := aniSkipSource{}.Lookup(SkipRef{AniListID: 154587, Episode: 1})
	if err != nil {
		t.Errorf("no MAL id should not be an error: %v", err)
	}
	if found {
		t.Error("it claimed to know something without an id to ask about")
	}
}

// Anime-Skip records single points in time with a type, not ranges: "Intro" at
// 93s means the opening starts there, and the next marker of any kind is where
// it ends. This is the real shape returned for Frieren.
func TestAnimeSkipMarkersBecomeSpans(t *testing.T) {
	type stamp = struct {
		At   float64 `json:"at"`
		Type struct {
			Name string `json:"name"`
		} `json:"type"`
	}
	mark := func(at float64, name string) stamp {
		s := stamp{At: at}
		s.Type.Name = name
		return s
	}

	// Episode 6: the opening does not start at zero, and credits are followed
	// by a preview, so both spans end at the next marker rather than the end.
	got := timestampsToSkips([]stamp{
		mark(0, "Canon"), mark(93, "Intro"), mark(183, "Canon"),
		mark(1340, "Credits"), mark(1430, "Preview"),
	})
	if got.Op != span(93, 183) {
		t.Errorf("opening should run to the next marker, got %+v", got.Op)
	}
	if got.Ed != span(1340, 1430) {
		t.Errorf("ending should run to the next marker, got %+v", got.Ed)
	}

	// Episode 8: the opening starts at zero.
	got = timestampsToSkips([]stamp{
		mark(0, "Intro"), mark(90, "Canon"), mark(1340, "Credits"), mark(1430, "Preview"),
	})
	if got.Op != span(0, 90) {
		t.Errorf("an opening at zero was not read: %+v", got.Op)
	}

	// Markers are not guaranteed to arrive in order.
	got = timestampsToSkips([]stamp{
		mark(1430, "Preview"), mark(0, "Intro"), mark(1340, "Credits"), mark(90, "Canon"),
	})
	if got.Op != span(0, 90) || got.Ed != span(1340, 1430) {
		t.Errorf("out-of-order markers were misread: %+v", got)
	}

	// A trailing marker has nothing to pair with and must not become a span
	// running to zero.
	got = timestampsToSkips([]stamp{mark(1340, "Credits")})
	if usableSpan(got.Ed) {
		t.Errorf("a marker with nothing after it became a span: %+v", got.Ed)
	}
}

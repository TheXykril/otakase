package internal

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// theintrodb leaves an end off when the opening starts with the episode, and a
// start off when the credits run to the end.
func TestIntroDBReadsTheSpansItActuallySends(t *testing.T) {
	// Frieren episode 1, as the service answers today.
	times := introDBTimes([]byte(`{"tmdb_id":209867,"type":"tv","season":1,"episode":1,
		"intro":[{"start_ms":null,"end_ms":90058}],
		"credits":[{"start_ms":1460042,"end_ms":1550042}],
		"preview":[{"start_ms":1550394,"end_ms":null}]}`))

	if times.Op.Start != 0 || times.Op.End != 90 {
		t.Errorf("an opening with no start should begin at zero, got %+v", times.Op)
	}
	if times.Ed.Start != 1460 || times.Ed.End != 1550 {
		t.Errorf("the ending came out as %+v", times.Ed)
	}
}

// Credits with no end are credits that run to the end of the episode. The
// episode length is not in this answer, so skipping to the end is a guess --
// and a guess here throws the viewer out of the episode.
func TestIntroDBLeavesUnboundedCreditsAlone(t *testing.T) {
	times := introDBTimes([]byte(`{"intro":[{"start_ms":228650,"end_ms":246025}],
		"credits":[{"start_ms":3431000,"end_ms":null}]}`))

	if times.Op.Start != 229 || times.Op.End != 246 {
		t.Errorf("the opening came out as %+v", times.Op)
	}
	if usableSpan(times.Ed) {
		t.Errorf("an ending with no end was offered anyway: %+v", times.Ed)
	}
}

func TestIntroDBIsQuietWithoutAMapping(t *testing.T) {
	source := newIntroDBSource(t.TempDir())
	source.endpoint = "http://127.0.0.1:1" // must not be reached

	// No AniList id: nothing to map, so nothing to ask.
	if _, found, err := source.Lookup(SkipRef{Episode: 1}); found || err != nil {
		t.Errorf("without an AniList id: found=%v err=%v", found, err)
	}
}

// A show that is not in the table, or a table still being built, is "cannot
// answer" -- the other sources carry on and nothing is logged as broken.
func TestIntroDBAsksOnlyForShowsItCanPlace(t *testing.T) {
	var asked int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		fmt.Fprint(w, `{"intro":[{"start_ms":0,"end_ms":90000}]}`)
	}))
	t.Cleanup(server.Close)

	source := newIntroDBSource(t.TempDir())
	source.endpoint = server.URL
	source.http = server.Client()
	// A mapping that knows one show and nothing else.
	source.mapping.mu.Lock()
	source.mapping.shows = map[int]tmdbShow{154587: {ID: 209867, Season: 1}}
	source.mapping.loaded = true
	source.mapping.mu.Unlock()

	if _, found, _ := source.Lookup(SkipRef{AniListID: 999999, Episode: 1}); found {
		t.Error("answered for a show it cannot place")
	}
	if asked != 0 {
		t.Errorf("asked the service %d times about a show it cannot place", asked)
	}

	times, found, err := source.Lookup(SkipRef{AniListID: 154587, Episode: 1})
	if err != nil || !found || times.Op.End != 90 {
		t.Errorf("a mapped show did not resolve: %+v found=%v err=%v", times, found, err)
	}
}

// A mapping without a season would send episode 1 of a second cour to episode 1
// of the first, which is worse than no skip times at all.
func TestAShowWithoutASeasonIsNotMapped(t *testing.T) {
	mapping := newTMDBMapping(t.TempDir())
	mapping.mu.Lock()
	mapping.shows = map[int]tmdbShow{
		154587: {ID: 209867, Season: 1},
		21:     {ID: 37854, Season: 0},
	}
	mapping.loaded = true
	mapping.mu.Unlock()

	if _, ok := mapping.Show(154587); !ok {
		t.Error("a fully mapped show was refused")
	}
	if _, ok := mapping.Show(21); ok {
		t.Error("a show with no season was mapped anyway")
	}
}

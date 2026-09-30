package internal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAniSkipRecapIsReadAsARecap(t *testing.T) {
	results := []skipResult{
		{Interval: skipInterval{StartTime: 0, EndTime: 75}, SkipType: "recap"},
		{Interval: skipInterval{StartTime: 75, EndTime: 165}, SkipType: "mixed-op", SkipID: "op"},
	}
	times, ids := aniSkipTimesFrom(results, 0)
	if times.Recap.Start != 0 || times.Recap.End != 75 {
		t.Errorf("recap = %+v, want 0-75", times.Recap)
	}
	if times.Op.End != 165 || ids.Op != "op" {
		t.Errorf("a mixed opening was not read as the opening: %+v %q", times.Op, ids.Op)
	}
}

func TestResolveTakesARecapFromWhereItTurnsUp(t *testing.T) {
	provider := stubSkipSource{name: "provider", found: true, times: SkipTimes{Op: span(80, 170)}}
	aniskip := stubSkipSource{name: "aniskip", found: true, times: SkipTimes{Recap: span(0, 75), Ed: span(1300, 1390)}}

	got := ResolveSkipTimes(SkipRef{Episode: 1}, provider, aniskip)
	if got.RecapSource != "aniskip" || got.Times.Recap.End != 75 {
		t.Fatalf("recap = %+v from %q", got.Times.Recap, got.RecapSource)
	}
	if !strings.Contains(got.Describe(), "recap 0-75s from aniskip") {
		t.Errorf("the log line leaves the recap out: %s", got.Describe())
	}
}

func TestARecapAloneCountsAsFound(t *testing.T) {
	if !(SkipResolution{Times: SkipTimes{Recap: span(0, 75)}}).Found() {
		t.Fatal("a recap on its own was reported as nothing found")
	}
}

func TestSkipSeekTarget(t *testing.T) {
	times := SkipTimes{Recap: span(10, 80), Op: span(80, 170), Ed: span(1300, 1390)}
	all := &Config{SkipOp: true, SkipEd: true, SkipRecap: true}

	cases := []struct {
		name     string
		position int
		config   *Config
		want     int
		ok       bool
	}{
		{"entering the recap", 11, all, 80, true},
		{"entering the opening", 81, all, 170, true},
		{"entering the ending", 1301, all, 1390, true},
		{"well inside the opening, after seeking back", 120, all, 0, false},
		{"recap with SkipRecap off", 11, &Config{SkipOp: true, SkipEd: true}, 0, false},
		{"opening with SkipOp off", 81, &Config{SkipRecap: true}, 0, false},
		{"no config", 81, nil, 0, false},
	}
	for _, c := range cases {
		got, ok := SkipSeekTarget(times, c.position, c.config)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: got %d,%v want %d,%v", c.name, got, ok, c.want, c.ok)
		}
	}

	// A span never resolved is zero, not one that starts at the beginning.
	if _, ok := SkipSeekTarget(SkipTimes{}, 1, all); ok {
		t.Error("an empty span was skipped")
	}
}

func TestCastSkipsTheRecapUnderSkipRecap(t *testing.T) {
	times := SkipTimes{Recap: span(0, 75)}
	if spans := castSpansFor(times, &Config{SkipRecap: true}); len(spans) != 1 || spans[0].End != 75 {
		t.Fatalf("spans = %+v, want the recap", spans)
	}
	if spans := castSpansFor(times, &Config{SkipOp: true, SkipEd: true}); len(spans) != 0 {
		t.Fatalf("the recap was skipped with SkipRecap off: %+v", spans)
	}
}

func withAniListAnswer(t *testing.T, body string) *int {
	t.Helper()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	old := untrackedAniListEndpoint
	untrackedAniListEndpoint = server.URL
	t.Cleanup(func() { untrackedAniListEndpoint = old })
	return &calls
}

func TestUntrackedShowGetsBothIDsFromAnExactTitle(t *testing.T) {
	withAniListAnswer(t, `{"data":{"Page":{"media":[
		{"id":2,"idMal":20,"title":{"romaji":"Frieren Season 2","english":"Frieren: Beyond Journey's End Season 2"}},
		{"id":1,"idMal":10,"title":{"romaji":"Sousou no Frieren","english":"Frieren: Beyond Journey’s End"}}
	]}}}`)

	anime := &Anime{Title: AnimeTitle{English: "Frieren: Beyond Journey's End", Romaji: "Frieren: Beyond Journey's End"}}
	ensureUntrackedIDs(anime)
	if anime.MalId != 10 || anime.AnilistId != 1 {
		t.Fatalf("ids = MAL %d, AniList %d; want 10 and 1", anime.MalId, anime.AnilistId)
	}
}

func TestUntrackedShowWithoutAnExactMatchGetsNoIDs(t *testing.T) {
	withAniListAnswer(t, `{"data":{"Page":{"media":[
		{"id":2,"idMal":20,"title":{"romaji":"Frieren Season 2","english":"Frieren Season 2"}}
	]}}}`)

	anime := &Anime{Title: AnimeTitle{English: "Frieren", Romaji: "Frieren"}}
	ensureUntrackedIDs(anime)
	if anime.MalId != 0 || anime.AnilistId != 0 {
		t.Fatalf("a near miss was taken: MAL %d, AniList %d", anime.MalId, anime.AnilistId)
	}
}

func TestUntrackedIDsAlreadyKnownAreNotLookedUp(t *testing.T) {
	calls := withAniListAnswer(t, `{}`)
	anime := &Anime{MalId: 5, AnilistId: 6, Title: AnimeTitle{English: "Anything"}}
	ensureUntrackedIDs(anime)
	if *calls != 0 || anime.MalId != 5 || anime.AnilistId != 6 {
		t.Fatalf("calls=%d ids=%d/%d", *calls, anime.MalId, anime.AnilistId)
	}
}

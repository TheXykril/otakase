package internal

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/thexykril/otakase/internal/providers"
)

// AniSkip keeps an entry for every cut of an episode anyone timed. One timed
// against a cut without the cold open skipped 0:00-1:00 of a file whose
// opening starts at 2:45.
func TestAniSkipEntryForAnotherCutIsNotUsed(t *testing.T) {
	results := []skipResult{
		{Interval: skipInterval{StartTime: 0, EndTime: 60}, SkipType: "op", SkipID: "short-cut", EpisodeLength: 1255},
		{Interval: skipInterval{StartTime: 165, EndTime: 255}, SkipType: "op", SkipID: "this-cut", EpisodeLength: 1421},
	}
	times, ids := aniSkipTimesFrom(aniSkipResultsForLength(results, 1420), 0)

	if times.Op.Start != 165 || times.Op.End != 255 || ids.Op != "this-cut" {
		t.Fatalf("opening = %+v (%s), want 165-255 from this-cut", times.Op, ids.Op)
	}
}

func TestAniSkipOnlyEntryForAnotherCutSkipsNothing(t *testing.T) {
	results := []skipResult{
		{Interval: skipInterval{StartTime: 0, EndTime: 60}, SkipType: "op", EpisodeLength: 1255},
	}
	times, _ := aniSkipTimesFrom(aniSkipResultsForLength(results, 1420), 0)
	if usableSpan(times.Op) {
		t.Fatalf("an entry for another cut was used: %+v", times.Op)
	}
}

func TestAniSkipClosestLengthWins(t *testing.T) {
	results := []skipResult{
		{Interval: skipInterval{StartTime: 90, EndTime: 180}, SkipType: "op", SkipID: "far", EpisodeLength: 1435},
		{Interval: skipInterval{StartTime: 88, EndTime: 178}, SkipType: "op", SkipID: "near", EpisodeLength: 1421},
	}
	_, ids := aniSkipTimesFrom(aniSkipResultsForLength(results, 1420), 0)
	if ids.Op != "near" {
		t.Fatalf("picked %q, want the entry closest in length", ids.Op)
	}
}

func TestAniSkipEntryPastTheEndIsNotUsed(t *testing.T) {
	results := []skipResult{
		// v1 answers carry no length; an ending past this file's end still
		// says it was timed against something longer.
		{Interval: skipInterval{StartTime: 1400, EndTime: 1490}, SkipType: "ed"},
	}
	times, _ := aniSkipTimesFrom(aniSkipResultsForLength(results, 1420), 0)
	if usableSpan(times.Ed) {
		t.Fatalf("an ending past the end of the file was used: %+v", times.Ed)
	}
}

func TestAniSkipUnknownLengthKeepsEverything(t *testing.T) {
	results := []skipResult{
		{Interval: skipInterval{StartTime: 0, EndTime: 60}, SkipType: "op", EpisodeLength: 1255},
	}
	if got := aniSkipResultsForLength(results, 0); !reflect.DeepEqual(got, results) {
		t.Fatalf("with no length known the entries changed: %+v", got)
	}
}

func withAniSkipServers(t *testing.T, v2, v1 http.HandlerFunc) {
	t.Helper()
	v2Server := httptest.NewServer(v2)
	v1Server := httptest.NewServer(v1)
	t.Cleanup(v2Server.Close)
	t.Cleanup(v1Server.Close)
	oldV2, oldV1 := aniSkipReadBase, aniSkipV1Base
	aniSkipReadBase, aniSkipV1Base = v2Server.URL, v1Server.URL
	t.Cleanup(func() { aniSkipReadBase, aniSkipV1Base = oldV2, oldV1 })
}

func TestAniSkipV2ResultsCarryTheirLength(t *testing.T) {
	withAniSkipServers(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "episodeLength=0") {
			t.Errorf("v2 was not asked for every length: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"found":true,"results":[{"interval":{"startTime":165.2,"endTime":255.4},"skipType":"op","skipId":"a","episodeLength":1421.5}]}`))
	}, func(w http.ResponseWriter, r *http.Request) {
		t.Error("v1 was asked although v2 answered")
	})

	results, found, err := getAniSkipResults(1, 2)
	if err != nil || !found || len(results) != 1 {
		t.Fatalf("results=%+v found=%v err=%v", results, found, err)
	}
	if results[0].EpisodeLength != 1421.5 || results[0].SkipID != "a" || results[0].Interval.StartTime != 165.2 {
		t.Fatalf("result = %+v", results[0])
	}
}

func TestAniSkipV2NotFoundIsNotAFailure(t *testing.T) {
	withAniSkipServers(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"found":false,"results":[]}`))
	}, func(w http.ResponseWriter, r *http.Request) {
		t.Error("v1 was asked although v2 said there is nothing")
	})

	if _, found, err := getAniSkipResults(1, 2); found || err != nil {
		t.Fatalf("found=%v err=%v, want nothing found and no error", found, err)
	}
}

func TestAniSkipFallsBackToV1(t *testing.T) {
	withAniSkipServers(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"found":true,"results":[{"interval":{"start_time":3,"end_time":93},"skip_type":"op","skip_id":"v1"}]}`))
	})

	results, found, err := getAniSkipResults(1, 2)
	if err != nil || !found || len(results) != 1 || results[0].SkipID != "v1" {
		t.Fatalf("results=%+v found=%v err=%v", results, found, err)
	}
}

// A lookup can outlast the episode it was for. Its answer must not become the
// next episode's timings.
func TestApplySkipTimesDropsAnswerForAnEpisodeNoLongerPlaying(t *testing.T) {
	anime := &Anime{}
	anime.Ep.Number = 3
	anime.Ep.SkipTimes = SkipTimes{Op: Skip{Start: 100, End: 190}}

	got := ApplySkipTimes(anime, 2, nil, nil)
	if got.Found() || len(got.Attempted) != 0 {
		t.Fatalf("resolution for episode 2 was reported: %+v", got)
	}
	if anime.Ep.SkipTimes.Op.End != 190 {
		t.Fatalf("episode 3's times were replaced by episode 2's lookup: %+v", anime.Ep.SkipTimes)
	}
}

type skipRangingStub struct{ gotID string }

func (s *skipRangingStub) Name() string { return "stub" }
func (s *skipRangingStub) SearchAnime(string, string) ([]providers.SelectionOption, error) {
	return nil, nil
}
func (s *skipRangingStub) EpisodesList(string, string) ([]string, error) { return nil, nil }
func (s *skipRangingStub) GetEpisodeURL(providers.PlaybackConfig, string, int) ([]string, error) {
	return nil, nil
}
func (s *skipRangingStub) SkipRange(id, mode string, epNo int) ([]int, []int, error) {
	s.gotID = id
	return []int{80, 170}, nil, nil
}

// The skip lookup asks the provider it is handed for SkipRange, and every
// provider it is handed is wrapped in the adapter. Without the adapter passing
// it on, a provider's own timings were never asked for.
func TestProviderAdapterPassesOnSkipRange(t *testing.T) {
	stub := &skipRangingStub{}
	ranger, ok := any(wrapProvider(stub)).(skipRangingProvider)
	if !ok {
		t.Fatal("the adapter does not offer SkipRange")
	}
	times, found, err := providerSkipSource{provider: ranger}.Lookup(SkipRef{ProviderID: "show-1", Episode: 1})
	if err != nil || !found || times.Op.Start != 80 || stub.gotID != "show-1" {
		t.Fatalf("times=%+v found=%v err=%v id=%q", times, found, err, stub.gotID)
	}
}

// Headers an instance was started with go with every file loaded after. The
// next episode must be asked for with its own headers only.
func TestResetMPVStreamHeadersReplacesThePreviousEpisodes(t *testing.T) {
	var sent [][]interface{}
	send := func(_ string, command []interface{}) (interface{}, error) {
		sent = append(sent, command)
		return nil, nil
	}
	resetMPVStreamHeaders(send, "sock", map[string]string{"Referer": "https://b.example/", "Origin": "https://b.example"})

	want := [][]interface{}{
		{"change-list", "http-header-fields", "clr", ""},
		{"change-list", "http-header-fields", "append", "Origin: https://b.example"},
		{"change-list", "http-header-fields", "append", "Referer: https://b.example/"},
	}
	if !reflect.DeepEqual(sent, want) {
		t.Fatalf("sent %v, want %v", sent, want)
	}

	sent = nil
	resetMPVStreamHeaders(send, "sock", nil)
	if len(sent) != 1 {
		t.Fatalf("an episode with no headers should only clear them, sent %v", sent)
	}
}

func TestUserHeaderArgsAreLeftAlone(t *testing.T) {
	if !hasMPVHeaderArg([]string{"--http-header-fields=X-Test: 1"}) {
		t.Error("--http-header-fields was not noticed")
	}
	if !hasMPVHeaderArg([]string{"--http-header-fields-append=X-Test: 1"}) {
		t.Error("--http-header-fields-append was not noticed")
	}
	if hasMPVHeaderArg([]string{"--sub-file=x.srt"}) {
		t.Error("an unrelated argument was taken for headers")
	}
}

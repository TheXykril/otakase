package internal

import (
	"reflect"
	"testing"

	"github.com/thexykril/otakase/internal/providers"
)

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

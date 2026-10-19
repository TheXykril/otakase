package providers_test

import (
	"fmt"
	"testing"
	"time"
)

// Films are listed by hosts as one-episode shows, or not at all. Run with:
//
//	OTAKASE_LIVE_MOVIES=1 go test ./internal/providers -run TestLiveProviderMovies -v -timeout 30m

var movieCases = []liveCase{
	{name: "your_name", query: "your name", wantInTitle: "your name"},
	{name: "your_name_romaji", query: "kimi no na wa", wantInTitle: "your name"},
	{name: "suzume", query: "suzume", wantInTitle: "suzume"},
	{name: "spirited_away", query: "spirited away", wantInTitle: "spirited"},
	{name: "silent_voice", query: "a silent voice", wantInTitle: "silent"},
	{name: "weathering", query: "weathering with you", wantInTitle: "weather"},
	{name: "mugen_train", query: "demon slayer mugen train", wantInTitle: "mugen"},
	{name: "jjk_0", query: "jujutsu kaisen 0", wantInTitle: "jujutsu kaisen 0"},
	{name: "film_red", query: "one piece film red", wantInTitle: "red"},
	{name: "howl", query: "howl's moving castle", wantInTitle: "howl"},
	{name: "grave_fireflies", query: "grave of the fireflies", wantInTitle: "grave"},
	{name: "akira", query: "akira", wantInTitle: "akira"},
	{name: "perfect_blue", query: "perfect blue", wantInTitle: "perfect"},
	{name: "promare", query: "promare", wantInTitle: "promare"},
	{name: "look_back", query: "look back", wantInTitle: "look back"},
	{name: "reze_arc", query: "chainsaw man reze arc", wantInTitle: "chainsaw"},
	{name: "paprika", query: "paprika", wantInTitle: "paprika"},
	{name: "girl_who_leapt", query: "girl who leapt through time", wantInTitle: "leapt"},
}

func TestLiveProviderMovies(t *testing.T) {
	requireLive(t, "OTAKASE_LIVE_MOVIES", "live movie provider tests")

	tallies := map[string]*flowTally{}
	for _, p := range edgeProviders() {
		tallies[p.name] = &flowTally{}
		for _, tc := range movieCases {
			res := runEdgeCase(p, tc)
			tallies[p.name].add(res)

			episodes := "no-list"
			if res.episodesOK {
				episodes = fmt.Sprintf("%deps", res.epCount)
			}
			t.Logf("[%s] %-20s %-30s search=%s match=%s eps=%s stream=%s picked=%q",
				res.provider, res.caseName, res.query,
				boolMark(res.searchOK), boolMark(res.matchOK), episodes, boolMark(res.streamOK),
				truncate(res.pickedTitle, 40))
			time.Sleep(200 * time.Millisecond)
		}
	}

	for name, tally := range tallies {
		t.Logf("%s movies: %s", name, tally)
	}
}

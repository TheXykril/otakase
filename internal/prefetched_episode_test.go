package internal

import "testing"

func TestUsePrefetchedEpisodeTakesTheSubtitlesWithTheLinks(t *testing.T) {
	anime := &Anime{}
	anime.Ep.Number = 2
	anime.Ep.SubtitleURL = "https://cdn.example/ep1.vtt"
	anime.Ep.NextEpisode = NextEpisode{
		Number: 2,
		Links:  []string{"https://cdn.example/ep2.m3u8"},
		LinkHints: map[string]StreamPlaybackHint{
			"https://cdn.example/ep2.m3u8": {Subtitle: "https://cdn.example/ep2.vtt"},
		},
	}

	if !UsePrefetchedEpisode(anime) {
		t.Fatal("the prefetched episode was not used")
	}
	if got := anime.Ep.SubtitleURL; got != "https://cdn.example/ep2.vtt" {
		t.Fatalf("SubtitleURL = %q, want episode 2's", got)
	}
}

func TestUsePrefetchedEpisodeDropsSubtitlesTheNextEpisodeDoesNotHave(t *testing.T) {
	anime := &Anime{}
	anime.Ep.Number = 2
	anime.Ep.SubtitleURL = "https://cdn.example/ep1.vtt"
	anime.Ep.NextEpisode = NextEpisode{Number: 2, Links: []string{"https://cdn.example/ep2.m3u8"}}

	UsePrefetchedEpisode(anime)
	if got := anime.Ep.SubtitleURL; got != "" {
		t.Fatalf("SubtitleURL = %q, want none", got)
	}
}

func TestUsePrefetchedEpisodeIgnoresAnotherEpisode(t *testing.T) {
	anime := &Anime{}
	anime.Ep.Number = 3
	anime.Ep.NextEpisode = NextEpisode{Number: 2, Links: []string{"https://cdn.example/ep2.m3u8"}}

	if UsePrefetchedEpisode(anime) {
		t.Fatal("episode 2's links were used for episode 3")
	}
}

func TestApplySkipTimesForgetsThePreviousEpisodesTimes(t *testing.T) {
	anime := &Anime{}
	anime.Ep.Number = 2
	anime.Ep.SkipTimes = SkipTimes{Op: Skip{Start: 0, End: 60}}

	// No MyAnimeList id and no provider: no source can answer.
	ApplySkipTimes(anime, 2, nil, nil)
	if anime.Ep.SkipTimes != (SkipTimes{}) {
		t.Fatalf("SkipTimes = %+v, want none", anime.Ep.SkipTimes)
	}
}

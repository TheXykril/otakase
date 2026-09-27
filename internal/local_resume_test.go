package internal

import "testing"

// The history your own file held when a resume silently did nothing: three rows
// for one show, written by three providers, one of them a whole episode ahead.
func historyWithThreeProviders() []Anime {
	rows := make([]Anime, 3)

	rows[0].AnilistId = 201514
	rows[0].ProviderName = "nyaa"
	rows[0].Ep.Number = 11
	rows[0].Ep.Player.PlaybackTime = 1

	rows[1].AnilistId = 201514
	rows[1].ProviderName = "anikoto"
	rows[1].Ep.Number = 10
	rows[1].Ep.Player.PlaybackTime = 548

	rows[2].AnilistId = 201514
	rows[2].ProviderName = "anizone"
	rows[2].Ep.Number = 10
	rows[2].Ep.Player.PlaybackTime = 17

	return rows
}

// LocalFindAnime answers with the furthest-ahead row, so the position used was
// the one second in episode 11 rather than the nine minutes in the episode about
// to play. The resume gate then rejected one second as too early to bother with,
// and nothing resumed and nothing said why.
func TestTheResumePositionComesFromTheEpisodeBeingPlayed(t *testing.T) {
	row := LocalFindEpisode(historyWithThreeProviders(), 201514, 10, "anikoto")
	if row == nil {
		t.Fatal("no row for an episode the history has")
	}
	if row.Ep.Player.PlaybackTime != 548 {
		t.Fatalf("resumed from %ds, want 548", row.Ep.Player.PlaybackTime)
	}
}

// Two providers have a position in this episode, and they are positions in
// different encodes of it. The one about to play is the one that means anything.
func TestTheResumePositionComesFromTheProviderAboutToPlay(t *testing.T) {
	row := LocalFindEpisode(historyWithThreeProviders(), 201514, 10, "anizone")
	if row == nil {
		t.Fatal("no row for an episode the history has")
	}
	if row.Ep.Player.PlaybackTime != 17 {
		t.Fatalf("resumed from %ds, want anizone's 17", row.Ep.Player.PlaybackTime)
	}
}

func TestAnUnknownProviderFallsBackToTheFurthestPositionInThatEpisode(t *testing.T) {
	// A provider with no row of its own, which a remap leaves behind. The episode
	// still matters more than the provider: some position in the right episode
	// beats none.
	row := LocalFindEpisode(historyWithThreeProviders(), 201514, 10, "kickassanime")
	if row == nil {
		t.Fatal("no row for an episode the history has")
	}
	if row.Ep.Player.PlaybackTime != 548 {
		t.Fatalf("fell back to %ds, want the furthest 548", row.Ep.Player.PlaybackTime)
	}
}

func TestAnEpisodeWithNoHistoryHasNoResumePosition(t *testing.T) {
	// Nothing rather than the nearest thing: carrying another episode's position
	// resumes one the viewer never started.
	if row := LocalFindEpisode(historyWithThreeProviders(), 201514, 12, "anikoto"); row != nil {
		t.Fatalf("episode 12 resumed from %ds", row.Ep.Player.PlaybackTime)
	}
}

func TestAnotherShowsPositionIsNeverUsed(t *testing.T) {
	if row := LocalFindEpisode(historyWithThreeProviders(), 999999, 10, "anikoto"); row != nil {
		t.Fatal("a different show's row was returned")
	}
}

func TestTheProviderNameIsMatchedWhateverItsCase(t *testing.T) {
	// Provider names reach this from the config, the history file and the
	// registry, and they do not all agree on case.
	row := LocalFindEpisode(historyWithThreeProviders(), 201514, 10, "AniZone")
	if row == nil || row.Ep.Player.PlaybackTime != 17 {
		t.Fatalf("a differently-cased provider name did not match its own row: %+v", row)
	}
}

package internal

import (
	"testing"
)

func at(seconds float64) *float64 { return &seconds }

// The marker keeps times for the episode being watched. Carrying marks into the
// next one would file them against the wrong episode.
func TestChangingEpisodeForgetsTheMarks(t *testing.T) {
	marker := &skipMarker{malID: 52991, episode: 1, applied: SkipIDs{Op: "an-id"}}
	marker.opStart, marker.opEnd = at(3), at(93)

	activeSkipMarkerMu.Lock()
	previous := activeSkipMarker
	activeSkipMarker = marker
	activeSkipMarkerMu.Unlock()
	t.Cleanup(func() {
		activeSkipMarkerMu.Lock()
		activeSkipMarker = previous
		activeSkipMarkerMu.Unlock()
	})

	next := &Anime{MalId: 52991}
	next.Ep.Number = 2
	UpdateSkipMarker(next, SkipIDs{Ed: "another-id"})

	marker.mu.Lock()
	defer marker.mu.Unlock()
	if marker.episode != 2 {
		t.Errorf("the marker is still on episode %d", marker.episode)
	}
	if marker.opStart != nil || marker.opEnd != nil {
		t.Error("marks from the last episode survived into this one")
	}
	if marker.applied.Op != "" || marker.applied.Ed != "another-id" {
		t.Errorf("the entry a vote refers to did not follow the episode: %+v", marker.applied)
	}
}

// A vote means the skip the viewer is at, or just watched happen. Voting on
// something minutes away is voting on the wrong entry.
func TestAVoteFindsTheEntryTheViewerMeans(t *testing.T) {
	anime := &Anime{}
	anime.Ep.SkipTimes = SkipTimes{Op: Skip{Start: 3, End: 93}, Ed: Skip{Start: 1417, End: 1507}}
	previous := GetGlobalAnime()
	SetGlobalAnime(anime)
	t.Cleanup(func() { SetGlobalAnime(previous) })

	marker := &skipMarker{applied: SkipIDs{Op: "op-id", Ed: "ed-id"}}

	cases := []struct {
		name     string
		position float64
		wantID   string
	}{
		{"inside the opening", 50, "op-id"},
		{"just after the opening was skipped", 100, "op-id"},
		{"inside the ending", 1450, "ed-id"},
		{"in the middle of the episode", 700, ""},
	}
	for _, test := range cases {
		gotID, _ := marker.entryNear(test.position)
		if gotID != test.wantID {
			t.Errorf("%s: voted on %q, want %q", test.name, gotID, test.wantID)
		}
	}
}

// An entry that did not come from AniSkip has no id, so there is nothing to
// vote on -- and saying so is better than voting on the other half.
func TestThereIsNothingToVoteOnWithoutAnID(t *testing.T) {
	anime := &Anime{}
	anime.Ep.SkipTimes = SkipTimes{Op: Skip{Start: 3, End: 93}, Ed: Skip{Start: 1417, End: 1507}}
	previous := GetGlobalAnime()
	SetGlobalAnime(anime)
	t.Cleanup(func() { SetGlobalAnime(previous) })

	// The opening came from the provider; only the ending came from AniSkip.
	marker := &skipMarker{applied: SkipIDs{Ed: "ed-id"}}
	if gotID, _ := marker.entryNear(50); gotID != "" {
		t.Errorf("voted on %q for a skip AniSkip did not supply", gotID)
	}
	if gotID, _ := marker.entryNear(1450); gotID != "ed-id" {
		t.Errorf("the ending was not votable: %q", gotID)
	}
}

// Each result says which it is. Taking the first as the opening and the last as
// the ending sends the player to the credits a minute into an episode that has
// only an ending on file.
func TestAnEndingOnItsOwnIsNotReadAsAnOpening(t *testing.T) {
	results := []skipResult{
		{Interval: skipInterval{StartTime: 1417, EndTime: 1507}, SkipType: "ed", SkipID: "ed-id"},
	}
	times, ids := aniSkipTimesFrom(results, 0)

	if usableSpan(times.Op) {
		t.Errorf("an ending was filed as the opening: %+v", times.Op)
	}
	if times.Ed.Start != 1417 || times.Ed.End != 1507 || ids.Ed != "ed-id" {
		t.Errorf("the ending came out as %+v / %+v", times.Ed, ids)
	}
}

func TestBothHalvesKeepTheirOwnID(t *testing.T) {
	results := []skipResult{
		{Interval: skipInterval{StartTime: 3, EndTime: 93}, SkipType: "op", SkipID: "op-id"},
		{Interval: skipInterval{StartTime: 1417, EndTime: 1507}, SkipType: "ed", SkipID: "ed-id"},
	}
	times, ids := aniSkipTimesFrom(results, 0)

	if times.Op.End != 93 || times.Ed.End != 1507 {
		t.Errorf("times came out as %+v", times)
	}
	if ids.Op != "op-id" || ids.Ed != "ed-id" {
		t.Errorf("ids came out as %+v", ids)
	}
}

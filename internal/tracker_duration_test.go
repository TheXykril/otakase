package internal

import "testing"

// The cast panel showed --:-- for the total for every episode of a whole
// season: the receiver reports dur=-1 until the remux writes EXT-X-ENDLIST,
// and Ep.Duration -- the panel's only other source -- was never filled in
// from the tracker, which knew the length all along.
func TestTheTrackerFillsInAnEpisodeLengthNothingHasMeasuredYet(t *testing.T) {
	got := trackerEpisodeDuration(0, Media{Duration: 24})
	if got != 24*60 {
		t.Fatalf("a 24 minute episode became %d seconds, want %d", got, 24*60)
	}
}

func TestAMeasuredEpisodeLengthSurvivesTheTrackerEstimate(t *testing.T) {
	// 23:41 as mpv or a finished remux would report it: the real length of the
	// file in hand, which must not be rounded back up to the tracker's average.
	const measured = 1421
	if got := trackerEpisodeDuration(measured, Media{Duration: 24}); got != measured {
		t.Fatalf("a measured %d seconds became %d", measured, got)
	}
}

func TestATrackerWithNoEpisodeLengthLeavesTheDurationUnknown(t *testing.T) {
	if got := trackerEpisodeDuration(0, Media{}); got != 0 {
		t.Fatalf("an unknown length became %d seconds, want 0", got)
	}
}

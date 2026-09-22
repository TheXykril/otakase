package cast

import "testing"

// The completion threshold has to mean the same thing casting as it does in
// mpv, or an episode watched on the TV is tracked differently from one watched
// at the desk.
func TestShouldMarkCompleteAtTheThreshold(t *testing.T) {
	cases := []struct {
		name      string
		progress  Progress
		threshold int
		want      bool
	}{
		{"just started", Progress{Position: 10, Duration: 1400}, 85, false},
		{"most of the way", Progress{Position: 1200, Duration: 1400}, 85, true},
		{"exactly at it", Progress{Position: 1190, Duration: 1400}, 85, true},
		{"finished", Progress{Position: 1400, Duration: 1400}, 85, true},
		// The device reports zero duration before it has loaded the stream;
		// dividing by it would mark an episode watched the moment it starts.
		{"duration not known yet", Progress{Position: 0, Duration: 0}, 85, false},
		// A threshold of zero means "never mark from progress", not "always".
		{"threshold disabled", Progress{Position: 1400, Duration: 1400}, 0, false},
	}

	for _, test := range cases {
		if got := ShouldMarkComplete(test.progress, test.threshold); got != test.want {
			t.Errorf("%s: ShouldMarkComplete = %v, want %v", test.name, got, test.want)
		}
	}
}

// Skipping on a cast device is a seek, decided from the polled position rather
// than from mpv's own clock.
func TestNextSkipFindsTheSpanThePositionIsInside(t *testing.T) {
	spans := []Span{{Start: 90, End: 180}, {Start: 1300, End: 1390}}

	if target, ok := NextSkip(120, spans); !ok || target != 180 {
		t.Errorf("inside the opening: got %v, %v; want 180, true", target, ok)
	}
	if target, ok := NextSkip(1350, spans); !ok || target != 1390 {
		t.Errorf("inside the ending: got %v, %v; want 1390, true", target, ok)
	}
	if _, ok := NextSkip(600, spans); ok {
		t.Error("the middle of an episode is not a skip")
	}
}

// A span already passed must not pull the viewer backwards, and one that has
// not started yet must not pull them forwards.
func TestNextSkipIgnoresSpansNotBeingPlayed(t *testing.T) {
	spans := []Span{{Start: 90, End: 180}}

	if _, ok := NextSkip(10, spans); ok {
		t.Error("seeking before the opening has started jumps the viewer forward")
	}
	if _, ok := NextSkip(400, spans); ok {
		t.Error("seeking after the opening has ended jumps the viewer backward")
	}
	// The span's own end is where a skip lands. Treating it as inside would
	// seek to where the player already is, forever.
	if _, ok := NextSkip(180, spans); ok {
		t.Error("the end of a span is not inside it")
	}
}

// An empty or zero-width span means "not known", not "skip to the start".
func TestNextSkipIgnoresUnknownSpans(t *testing.T) {
	if _, ok := NextSkip(50, nil); ok {
		t.Error("no spans is not a skip")
	}
	if _, ok := NextSkip(0, []Span{{Start: 0, End: 0}}); ok {
		t.Error("a zero-width span is not a skip")
	}
}

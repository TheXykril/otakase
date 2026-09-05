package internal

import (
	"testing"
	"time"
)

func entryAt(title string, updated time.Time) Entry {
	e := Entry{UpdatedAt: updated}
	e.Media.Title.English = title
	e.Media.ID = len(title)
	return e
}

// A continue-watching list should lead with what you were last watching. The
// text menu applied no sort at all and the grid sorted alphabetically, so
// neither did.
func TestSortEntriesByRecency(t *testing.T) {
	now := time.Now()
	entries := []Entry{
		entryAt("Older", now.Add(-72*time.Hour)),
		entryAt("Newest", now),
		entryAt("Middle", now.Add(-24*time.Hour)),
	}

	sorted := sortEntriesByRecency(entries, &CurdConfig{})
	want := []string{"Newest", "Middle", "Older"}
	for i, w := range want {
		if got := sorted[i].Media.Title.English; got != w {
			t.Fatalf("position %d = %q, want %q", i, got, w)
		}
	}

	// The input must not be reordered in place: callers reuse the cached list.
	if entries[0].Media.Title.English != "Older" {
		t.Fatal("sortEntriesByRecency mutated its input")
	}
}

// Entries with no timestamp (or equal ones) must still land in a stable, sane
// order rather than shuffling between runs.
func TestSortEntriesByRecencyFallsBackToTitle(t *testing.T) {
	var zero time.Time
	entries := []Entry{
		entryAt("Zeta", zero),
		entryAt("alpha", zero),
		entryAt("Beta", zero),
	}

	sorted := sortEntriesByRecency(entries, &CurdConfig{})
	want := []string{"alpha", "Beta", "Zeta"}
	for i, w := range want {
		if got := sorted[i].Media.Title.English; got != w {
			t.Fatalf("position %d = %q, want %q", i, got, w)
		}
	}
}

func TestFormatTimeUntilAiring(t *testing.T) {
	cases := map[int]string{
		0:          "",
		-5:         "",
		30:         "next in <1m",
		45 * 60:    "next in 45m",
		5 * 3600:   "next in 5h",
		23 * 3600:  "next in 23h",
		24 * 3600:  "next in 1d",
		6 * 86400:  "next in 6d",
		13 * 86400: "next in 13d",
	}
	for seconds, want := range cases {
		if got := formatTimeUntilAiring(seconds); got != want {
			t.Fatalf("formatTimeUntilAiring(%d) = %q, want %q", seconds, got, want)
		}
	}
}

// A resume point is only worth showing when you are meaningfully part way in.
func TestFormatResumePosition(t *testing.T) {
	cases := []struct {
		name             string
		playback, length int
		want             string
	}{
		// Two seconds into an episode is noise.
		{"barely started", 2, 24, ""},
		{"under the threshold", 59, 24, ""},
		{"a minute in", 60, 24, "resume 1:00"},
		// 22:12 of a 23 minute episode: effectively finished.
		{"almost finished", 1332, 23, ""},
		{"halfway", 700, 24, "resume 11:40"},
		{"long episode", 3725, 90, "resume 1:02:05"},
		// No known length: fall back to the minimum threshold alone.
		{"unknown length", 700, 0, "resume 11:40"},
	}

	for _, tc := range cases {
		if got := formatResumePosition(tc.playback, tc.length); got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Resume is something to act on now; a countdown is something to come back for.
// Showing both would crowd the row, so resume wins.
func TestEntryStatusNotePrefersResume(t *testing.T) {
	entry := Entry{}
	entry.Media.NextAiringEpisode = &NextAiringEpisodeInfo{Episode: 10, TimeUntilAiring: 6 * 86400}

	if got := entryStatusNote(entry, "resume 11:40"); got != "resume 11:40" {
		t.Fatalf("got %q, want the resume point", got)
	}
	if got := entryStatusNote(entry, ""); got != "next in 6d" {
		t.Fatalf("got %q, want the countdown", got)
	}

	// Nothing to say about a finished show with no resume point.
	if got := entryStatusNote(Entry{}, ""); got != "" {
		t.Fatalf("got %q, want no note", got)
	}
}

func TestWithEpisodeCountsAndNote(t *testing.T) {
	entry := entryWith(0, 24, 22, "RELEASING")
	entry.Media.NextAiringEpisode.TimeUntilAiring = 6 * 86400

	got := WithEpisodeCountsAndNote("Slime S4", entry, "")
	if got != "Slime S4 · 0/24 (21 aired) · next in 6d" {
		t.Fatalf("got %q", got)
	}

	got = WithEpisodeCountsAndNote("Slime S4", entry, "resume 11:40")
	if got != "Slime S4 · 0/24 (21 aired) · resume 11:40" {
		t.Fatalf("got %q", got)
	}

	// A show with nothing to report keeps its bare title.
	if got := WithEpisodeCountsAndNote("Unknown", entryWith(0, 0, 0, "NOT_YET_RELEASED"), ""); got != "Unknown" {
		t.Fatalf("got %q", got)
	}
}

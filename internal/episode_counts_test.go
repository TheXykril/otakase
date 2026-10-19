package internal

import (
	"strings"
	"testing"
)

func entryWith(watched, total, nextAiring int, status string) Entry {
	entry := Entry{Progress: watched}
	entry.Media.Episodes = total
	entry.Media.Status = status
	if nextAiring > 0 {
		entry.Media.NextAiringEpisode = &NextAiringEpisodeInfo{Episode: nextAiring}
	}
	return entry
}

// Every shape below is taken from a real AniList list.
func TestEpisodeProgressSummary(t *testing.T) {
	cases := []struct {
		name  string
		entry Entry
		want  string
	}{
		{
			// Slime S4: airing, nothing watched, 21 of 24 out.
			name:  "airing and behind",
			entry: entryWith(0, 24, 22, "RELEASING"),
			want:  "0/24 (21 aired)",
		},
		{
			// Rich Girl Caretaker: caught up on everything aired so far.
			name:  "airing and caught up",
			entry: entryWith(9, 12, 10, "RELEASING"),
			want:  "9/12 (9 aired)",
		},
		{
			// A finished show needs no aired count: it equals the total.
			name:  "finished",
			entry: entryWith(3, 12, 0, "FINISHED"),
			want:  "3/12",
		},
		{
			// Imaizumi's: airing, AniList has no episode count yet.
			name:  "airing with no announced total",
			entry: entryWith(6, 0, 0, "RELEASING"),
			want:  "6 watched",
		},
		{
			// No total, but we know what has aired.
			name:  "no total but aired is known",
			entry: entryWith(1100, 0, 1178, "RELEASING"),
			want:  "1100/1177 aired",
		},
		{
			name:  "not yet released",
			entry: entryWith(0, 0, 0, "NOT_YET_RELEASED"),
			want:  "",
		},
		{
			// Aired equal to total adds nothing, so it is omitted.
			name:  "airing but every episode is out",
			entry: entryWith(11, 12, 13, "RELEASING"),
			want:  "11/12",
		},
	}

	for _, tc := range cases {
		if got := episodeProgressFor(tc.entry).Summary(); got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestWithEpisodeCountsLeavesUnknownEntriesAlone(t *testing.T) {
	title := "Tsuki ga Michibiku Isekai Douchuu 3rd Season"
	if got := WithEpisodeCounts(title, entryWith(0, 0, 0, "NOT_YET_RELEASED")); got != title {
		t.Fatalf("got %q, want the bare title", got)
	}
}

// The counts must land inside the dimmed metadata span, not be left half at full
// strength -- which is what an interior "·" would cause.
func TestEpisodeCountsDimEntirelyInRofiRows(t *testing.T) {
	rofiMetaColor = "#4b4e55"

	label := WithEpisodeCounts("Rich Girl Caretaker", entryWith(9, 12, 10, "RELEASING"))
	title, meta := splitRofiLabel(label)

	if title != "Rich Girl Caretaker" {
		t.Fatalf("title = %q, want the bare title", title)
	}
	if !strings.Contains(meta, "9/12") || !strings.Contains(meta, "9 aired") {
		t.Fatalf("expected the whole count block in the metadata, got %q", meta)
	}

	row := rofiRowMarkup(label)
	// Everything after the title should be inside one span.
	if strings.Count(row, "<span") != 1 {
		t.Fatalf("expected a single dimmed span, got %q", row)
	}
	if !strings.HasPrefix(row, "Rich Girl Caretaker <span") {
		t.Fatalf("expected only the title outside the span, got %q", row)
	}
}

// Selections are matched back after markup is stripped, so the label must
// round-trip intact.
func TestEpisodeCountLabelsRoundTrip(t *testing.T) {
	rofiMetaColor = "#4b4e55"

	for _, entry := range []Entry{
		entryWith(0, 24, 22, "RELEASING"),
		entryWith(3, 12, 0, "FINISHED"),
		entryWith(6, 0, 0, "RELEASING"),
	} {
		label := WithEpisodeCounts("Some Show", entry)
		rendered := rofiRowMarkup(label)
		recovered := unescapePango(strings.TrimSpace(pangoStrip.ReplaceAllString(rendered, "")))
		if recovered != label {
			t.Fatalf("round trip failed:\n  label     %q\n  recovered %q", label, recovered)
		}
	}
}

package internal

import (
	"strings"
	"testing"

	"github.com/thexykril/otakase/internal/rofitheme"
)

// The poster grid clips a label to the column width, so what rofi hands back on
// a long title is not the label the option carries:
//
//	option:   "That Time I Got Reincarnated as a Slime Season 4 · 0/24 (21 aired)"
//	rofi:     "That Time I Got Reincarn… · 0/24 (21 aired)"
//
// Matching the returned string against the option list therefore failed for
// exactly the shows with the longest names, and the menu died with "selection
// not found in options". Rows are addressed by index instead.
func TestPreviewSelectionMatchesTruncatedLabel(t *testing.T) {
	long := SelectionOption{
		Key:   "108465",
		Label: "That Time I Got Reincarnated as a Slime Season 4 · 0/24 (21 aired)",
	}
	rows := []SelectionOption{
		{Key: "1", Label: "Uzaki-chan Wants to Hang Out! · 3/12"},
		long,
	}

	rendered := GridRowMarkup(long.Label, rofitheme.GridLabelCapacity)
	stripped := pangoStrip.ReplaceAllString(rendered, "")
	if stripped == long.Label {
		t.Fatal("expected the grid to clip this label; the test no longer covers the bug")
	}

	got, err := parsePreviewSelectionIndex("1\n", rows)
	if err != nil {
		t.Fatalf("expected the truncated row to resolve, got %v", err)
	}
	if got.Key != long.Key {
		t.Fatalf("resolved the wrong row: %+v", got)
	}
}

// A row whose cover fails to download is skipped when the menu is written, so
// the row table must be built from what was actually emitted. Indexing into the
// unfiltered option list would resolve every row after the gap to its neighbour.
func TestPreviewRowsSkipUncachedCovers(t *testing.T) {
	options := []SelectionOption{
		{Key: "a", Label: "Alpha"},
		{Key: "b", Label: "Beta"},
		{Key: "c", Label: "Gamma"},
	}

	var input strings.Builder
	rows := writePreviewRows(&input, options, func(opt SelectionOption) (string, bool) {
		return "/cache/" + opt.Key + ".jpg", opt.Key != "b"
	})

	if len(rows) != 2 {
		t.Fatalf("expected the uncached row to be dropped, got %+v", rows)
	}
	got, err := parsePreviewSelectionIndex("1", rows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Key != "c" {
		t.Fatalf("index 1 should be Gamma once Beta is dropped, got %+v", got)
	}
	if strings.Contains(input.String(), "Beta") {
		t.Fatal("the uncached row must not be written to rofi")
	}
}

func TestPreviewSelectionSentinels(t *testing.T) {
	rows := []SelectionOption{
		{Key: "a", Label: "Alpha"},
		{Key: "add_new", Label: "Add new anime"},
		{Key: "-2", Label: "Back"},
		{Key: "-1", Label: "Quit"},
	}

	for _, tc := range []struct{ raw, want string }{
		{"1", "add_new"},
		{"2", "-2"},
		{"3", "-1"},
	} {
		got, err := parsePreviewSelectionIndex(tc.raw, rows)
		if err != nil {
			t.Fatalf("%q: %v", tc.raw, err)
		}
		if got.Key != tc.want {
			t.Fatalf("%q: expected %q, got %q", tc.raw, tc.want, got.Key)
		}
	}
}

// Escape closes rofi with no output; an out-of-range index would be a bug in the
// row table. Neither should abort the session -- both mean "go back".
func TestPreviewSelectionTreatsNoChoiceAsBack(t *testing.T) {
	rows := []SelectionOption{{Key: "a", Label: "Alpha"}}
	for _, raw := range []string{"", "  \n", "9", "-1", "not-a-number"} {
		got, err := parsePreviewSelectionIndex(raw, rows)
		if err != nil {
			t.Fatalf("%q: unexpected error %v", raw, err)
		}
		if got.Key != "-2" {
			t.Fatalf("%q: expected Back, got %+v", raw, got)
		}
	}
}

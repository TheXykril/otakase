package internal

import "testing"

func TestWatchedElsewhereStatus(t *testing.T) {
	cases := []struct {
		progress, total int
		status          string
		recorded        int
	}{
		{5, 12, "CURRENT", 5},
		{0, 12, "CURRENT", 0},
		{12, 12, "COMPLETED", 12},
		// More than there are is every episode.
		{15, 12, "COMPLETED", 12},
		// An airing show with no count yet can only be in progress.
		{40, 0, "CURRENT", 40},
	}
	for _, tc := range cases {
		status, recorded := watchedElsewhereStatus(tc.progress, tc.total)
		if status != tc.status || recorded != tc.recorded {
			t.Fatalf("watchedElsewhereStatus(%d, %d) = %s %d, want %s %d",
				tc.progress, tc.total, status, recorded, tc.status, tc.recorded)
		}
	}
}

package internal

import "testing"

func TestMilestones(t *testing.T) {
	cases := []struct {
		name             string
		total, last      int
		seen             bool
		wantPlay, wantRe int
	}{
		{"first look records quietly", 640, 0, false, 0, 500},
		{"first look under 50", 40, 0, false, 0, 0},
		{"crossing 50", 50, 0, true, 50, 50},
		{"crossing 100", 100, 0, true, 100, 100},
		{"still between", 200, 100, true, 0, 100},
		{"crossing 250", 260, 100, true, 250, 250},
		{"crossing 500", 501, 250, true, 500, 500},
		{"jumping two at once plays the higher", 1200, 100, true, 1000, 1000},
		{"never plays twice", 1300, 1000, true, 0, 1000},
		{"the last one", 10400, 5000, true, 10000, 10000},
		{"a smaller total after an edit keeps the record", 90, 100, true, 0, 100},
	}
	for _, c := range cases {
		play, record := milestoneToCelebrate(c.total, c.last, c.seen)
		if play != c.wantPlay || record != c.wantRe {
			t.Errorf("%s: play %d record %d, want %d %d", c.name, play, record, c.wantPlay, c.wantRe)
		}
	}
}

package internal

import (
	"testing"
	"time"

	"github.com/thexykril/otakase/internal/theme"
)

// lateNight makes it late (or not) for the rest of the test.
func lateNight(t *testing.T, late bool) {
	t.Helper()
	previous := lateNightNow
	lateNightNow = func() bool { return late }
	t.Cleanup(func() { lateNightNow = previous })
}

// Three in the morning until six is late; the hour either side is not.
func TestLateNightIsThreeUntilSix(t *testing.T) {
	cases := map[int]bool{0: false, 2: false, 3: true, 4: true, 5: true, 6: false, 15: false, 23: false}
	for hour, want := range cases {
		at := time.Date(2026, 10, 22, hour, 30, 0, 0, time.Local)
		if got := isLateNight(at); got != want {
			t.Errorf("%02d:30: late night = %v, want %v", hour, got, want)
		}
	}
}

// During the day the prompt is exactly what it always was.
func TestNoSleepRowDuringTheDay(t *testing.T) {
	lateNight(t, false)
	choices := withSleepOption([]SelectionOption{{Key: "yes"}})
	if len(choices) != 1 || choices[0].Key != "yes" {
		t.Fatalf("daytime choices = %+v, want only yes", choices)
	}
}

// Late at night sleep is offered, under the next episode rather than in its
// place, so enter still does what it always did.
func TestSleepRowSitsUnderTheNextEpisode(t *testing.T) {
	lateNight(t, true)
	choices := withSleepOption([]SelectionOption{{Key: "yes"}})
	if len(choices) != 2 || choices[0].Key != "yes" || choices[1].Key != sleepKey {
		t.Fatalf("late choices = %+v, want yes then sleep", choices)
	}
	if choices[1].Label != "Sleep (recommended)" {
		t.Fatalf("sleep label = %q", choices[1].Label)
	}
}

func TestSleepGoodbyeCoversEveryLine(t *testing.T) {
	for i := range sleepGoodbyes {
		if got := sleepGoodbye(rolls(i)); got != sleepGoodbyes[i] {
			t.Fatalf("roll %d gave %q, want %q", i, got, sleepGoodbyes[i])
		}
	}
}

// Dimming only happens late, and then turns every colour down.
func TestMenusDimOnlyLateAtNight(t *testing.T) {
	palette := theme.Builtin()

	lateNight(t, false)
	if got := dimForLateNight(palette); got != palette {
		t.Fatal("the palette changed during the day")
	}

	lateNight(t, true)
	dimmed := dimForLateNight(palette)
	if dimmed.Foreground == palette.Foreground || dimmed.Accent == palette.Accent {
		t.Fatalf("late palette not dimmed: fg %s→%s accent %s→%s",
			palette.Foreground, dimmed.Foreground, palette.Accent, dimmed.Accent)
	}
}

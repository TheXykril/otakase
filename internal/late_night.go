package internal

import (
	"sync/atomic"
	"time"

	"github.com/thexykril/otakase/internal/icons"
	"github.com/thexykril/otakase/internal/theme"
)

// Late at night otakase gets a little quieter: the menus dim, and the
// next-episode prompt offers to call it a night. Nothing is taken away -- the
// next episode is still the first row -- it only says what a friend would.

// lateNightStart and lateNightEnd bound the hours, local time, that count as
// late: from three in the morning until six.
const (
	lateNightStart = 3
	lateNightEnd   = 6
)

// isLateNight reports whether t falls in the late-night hours.
func isLateNight(t time.Time) bool {
	hour := t.Hour()
	return hour >= lateNightStart && hour < lateNightEnd
}

// lateNightNow answers for the current time. Tests replace it, so a suite run
// at four in the morning behaves like one run at noon.
var lateNightNow = func() bool { return isLateNight(time.Now()) }

// sleepKey is the key of the sleep row in a next-episode prompt.
const sleepKey = "sleep"

// withSleepOption adds the sleep row under the given choices when it is late.
func withSleepOption(choices []SelectionOption) []SelectionOption {
	if !lateNightNow() {
		return choices
	}
	return append(choices, SelectionOption{Key: sleepKey, Label: "Sleep (recommended)", Icon: icons.Sleep})
}

// askNextEpisode shows a next-episode prompt. Late at night it keeps the rows
// in the order given, so the next episode stays where the cursor starts and
// sleep is the row under it, not something a habitual enter lands on.
func askNextEpisode(choices []SelectionOption) (SelectionOption, error) {
	choices = withSleepOption(choices)
	if len(choices) > 1 {
		return DynamicSelectPreserveOrder(choices)
	}
	return DynamicSelect(choices)
}

// sleepChosen is set once the viewer picks sleep, so the goodbye on exit is
// the one for going to bed.
var sleepChosen atomic.Bool

// chooseSleep records that the viewer took the hint.
func chooseSleep() {
	Log("Late night: viewer chose sleep")
	sleepChosen.Store(true)
}

// sleepGoodbyes replace the usual exit greeting after sleep was picked.
var sleepGoodbyes = []string{
	"Good choice. Oyasumi.",
	"Good choice. Oyasumi nasai.",
	"Wise. The next episode will still be here tomorrow. Oyasumi.",
}

// sleepGoodbye picks the line. roll is rand.IntN, as for exitGreeting.
func sleepGoodbye(roll func(int) int) string {
	return sleepGoodbyes[roll(len(sleepGoodbyes))]
}

// lateNightDimAmount is how far every colour is pulled toward black.
const lateNightDimAmount = 0.25

// dimForLateNight turns the palette down a notch when it is late.
func dimForLateNight(palette theme.Palette) theme.Palette {
	if !lateNightNow() {
		return palette
	}
	Log("Late night: dimming the menus")
	return theme.Dimmed(palette, lateNightDimAmount)
}

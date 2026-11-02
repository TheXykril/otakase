package internal

import (
	"sync/atomic"
)

// Late at night otakase gets a little quieter: the menus dim, and the
// next-episode prompt offers to call it a night. Nothing is taken away -- the
// next episode is still the first row -- it only says what a friend would.

// lateNightStart and lateNightEnd bound the hours, local time, that count as
// late: from three in the morning until six.
const ()

// sleepChosen is set once the viewer picks sleep, so the goodbye on exit is
// the one for going to bed.
var sleepChosen atomic.Bool

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

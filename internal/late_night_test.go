package internal

import (
	"testing"
)

func TestSleepGoodbyeCoversEveryLine(t *testing.T) {
	for i := range sleepGoodbyes {
		if got := sleepGoodbye(rolls(i)); got != sleepGoodbyes[i] {
			t.Fatalf("roll %d gave %q, want %q", i, got, sleepGoodbyes[i])
		}
	}
}

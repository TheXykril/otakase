package internal

import (
	"testing"
)

// LeaveAppScreen runs on every exit path, including ones that never switched
// to the alternate screen, so it has to leave the flag off either way.
func TestLeaveAppScreenTurnsAltBufferOff(t *testing.T) {
	saved := inAppScreen
	t.Cleanup(func() { inAppScreen = saved })

	for _, active := range []bool{false, true} {
		inAppScreen = active
		LeaveAppScreen()
		if inAppScreen {
			t.Errorf("starting from active=%v, the alternate screen is still marked active", active)
		}
	}
}

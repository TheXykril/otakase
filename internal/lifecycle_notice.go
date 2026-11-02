package internal

import (
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/gen2brain/beeep"

	"github.com/thexykril/otakase/internal/appicon"
)

// Launched from a keybind, otakase shows nothing until its first menu, and the
// update check at launch can hold that menu back for a couple of seconds. A
// notification the moment it starts answers "did that work?", and a matching
// one when it is gone answers "is it still running?".
//
// The closing notice belongs to the tool as a whole, not to a process. A cast
// started from rofi hands the episode to a terminal window and the menu
// process exits straight away; announcing a close there would be wrong, since
// the cast is still going. The window says it instead, when it ends.

const (
	// notifyTagLifecycle keeps the closing notice apart from the main one, so
	// an error reported on the way out is not replaced by "closed".
	notifyTagLifecycle = "otakase-lifecycle"
	// lifecycleNoticeExpire is long enough to read and short enough not to
	// linger after the menu is up.
	lifecycleNoticeExpire = 4 * time.Second
)

var (
	lifecycleMu sync.Mutex
	// closeNoticeArmed is whether this process owes a notice when it exits.
	closeNoticeArmed bool

	// lifecycleNotify shows one notice; swapped out in tests.
	lifecycleNotify = sendLifecycleNotice
)

// disarmCloseNotice drops the notice once another process carries on the
// session, as the cast window does after a handoff.
func disarmCloseNotice() {
	lifecycleMu.Lock()
	closeNoticeArmed = false
	lifecycleMu.Unlock()
}

// announceClose sends the closing notice, at most once, if one is owed.
func announceClose() {
	lifecycleMu.Lock()
	armed := closeNoticeArmed
	closeNoticeArmed = false
	lifecycleMu.Unlock()
	if armed {
		lifecycleNotify(notifyTagLifecycle, fmt.Sprintf("%s closed", DisplayName))
	}
}

func sendLifecycleNotice(tag, message string) {
	if runtime.GOOS == "linux" {
		if err := sendLinuxNotificationFor(tag, "", message, lifecycleNoticeExpire); err != nil {
			Log(fmt.Sprintf("Failed to send notification: %v", err))
		}
		return
	}
	if err := beeep.Notify(DisplayName, message, appicon.Path()); err != nil {
		Log(fmt.Sprintf("Failed to send notification: %v", err))
	}
}

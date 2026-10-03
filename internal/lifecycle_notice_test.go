package internal

import (
	"strings"
	"testing"
)

func captureLifecycleNotices(t *testing.T) *[]string {
	t.Helper()
	var sent []string
	previous := lifecycleNotify
	lifecycleNotify = func(tag, message string) { sent = append(sent, tag+"|"+message) }
	disarmCloseNotice()
	t.Cleanup(func() {
		lifecycleNotify = previous
		disarmCloseNotice()
	})
	return &sent
}

// A keybind launch says it started, and says so again when it is gone.
func TestARofiLaunchAnnouncesStartAndClose(t *testing.T) {
	sent := captureLifecycleNotices(t)

	AnnounceLaunch(&Config{RofiSelection: true})
	announceClose()

	if len(*sent) != 2 {
		t.Fatalf("sent %q, want a launch and a close notice", *sent)
	}
	if !strings.HasSuffix((*sent)[0], "launched") || !strings.HasSuffix((*sent)[1], "closed") {
		t.Fatalf("sent %q, want launched then closed", *sent)
	}
	// The close notice must not replace an error shown on the main tag.
	if !strings.HasPrefix((*sent)[1], notifyTagLifecycle+"|") {
		t.Fatalf("close notice %q went out on the main tag", (*sent)[1])
	}
}

// A terminal shows the launch and its own goodbye; no notifications.
func TestATerminalLaunchSendsNoNotices(t *testing.T) {
	sent := captureLifecycleNotices(t)

	AnnounceLaunch(&Config{RofiSelection: false})
	AnnounceLaunch(nil)
	announceClose()

	if len(*sent) != 0 {
		t.Fatalf("sent %q in terminal mode", *sent)
	}
}

// Handing a cast to its window ends the menu process, but otakase is still
// running there: no close notice until the window itself ends.
func TestACastHandoffDefersTheCloseNoticeToTheWindow(t *testing.T) {
	sent := captureLifecycleNotices(t)

	AnnounceLaunch(&Config{RofiSelection: true})
	disarmCloseNotice() // the handoff succeeded
	announceClose()     // the menu process exits
	if len(*sent) != 1 {
		t.Fatalf("sent %q, want only the launch notice before the window ends", *sent)
	}

	ArmCloseNotice() // the cast window starts
	announceClose()  // and ends
	if len(*sent) != 2 || !strings.HasSuffix((*sent)[1], "closed") {
		t.Fatalf("sent %q, want a close notice when the window ends", *sent)
	}
}

// Exit paths can stack (Exit reaching exitWithRestore after a signal); the
// notice goes out once.
func TestTheCloseNoticeIsSentOnce(t *testing.T) {
	sent := captureLifecycleNotices(t)

	ArmCloseNotice()
	announceClose()
	announceClose()
	if len(*sent) != 1 {
		t.Fatalf("sent %q, want one close notice", *sent)
	}
}

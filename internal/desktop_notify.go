package internal

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/thexykril/otakase/internal/appicon"
)

// A message that updates -- a slow step counting its seconds, a download's
// progress -- should change one notification in place, not stack a new one
// every few seconds. Desktops disagree on how to ask for that, so every
// notification carries all three ways:
//
//   - the replace id notify-send prints with -p and takes back with -r, which
//     is the freedesktop spec and works on any compliant daemon (GNOME, KDE,
//     mako, dunst, swaync);
//   - x-canonical-private-synchronous, which dunst and notify-osd honour;
//   - x-dunst-stack-tag, dunst's own name for the same thing.
//
// The id is kept per tag, so the update notice and the main one do not replace
// each other.

const notifyTagMain = "otakase-notification"

var (
	notifyMu         sync.Mutex
	notifyReplaceIDs = map[string]string{}
	// notifyNoPrintID is set once notify-send has rejected -p (versions before
	// 0.7.9), after which the hints alone have to do.
	notifyNoPrintID bool

	// notifySendRun runs notify-send and returns its standard output.
	notifySendRun = func(args []string) (string, error) {
		out, err := exec.Command("notify-send", args...).Output()
		return string(out), err
	}
)

// sendLinuxNotification shows message under tag, replacing the last one sent
// under the same tag. An empty icon is the app icon.
func sendLinuxNotification(tag, icon, message string) error {
	return sendLinuxNotificationFor(tag, icon, message, 0)
}

// sendLinuxNotificationFor is sendLinuxNotification with an expiry; zero
// leaves it to the daemon.
func sendLinuxNotificationFor(tag, icon, message string, expire time.Duration) error {
	notifyMu.Lock()
	defer notifyMu.Unlock()

	base := []string{
		"-a", DisplayName,
		"-h", "string:x-canonical-private-synchronous:" + tag,
		"-h", "string:x-dunst-stack-tag:" + tag,
	}
	if icon == "" {
		icon = appicon.Path()
	}
	if icon != "" {
		base = append(base, "-i", icon)
	}
	if expire > 0 {
		base = append(base, "-t", strconv.Itoa(int(expire.Milliseconds())))
	}

	if !notifyNoPrintID {
		args := append([]string(nil), base...)
		args = append(args, "-p")
		if id := notifyReplaceIDs[tag]; id != "" {
			args = append(args, "-r", id)
		}
		args = append(args, DisplayName, message)
		out, err := notifySendRun(args)
		if err == nil {
			if id := strings.TrimSpace(out); id != "" {
				notifyReplaceIDs[tag] = id
			}
			return nil
		}
		// An old notify-send exits on the unknown option without showing
		// anything. Retry without it, and only stop asking for ids when that
		// works: a daemon that is not running fails both ways and should not
		// cost the id once it is back.
		if _, retryErr := notifySendRun(append(base, DisplayName, message)); retryErr != nil {
			return retryErr
		}
		notifyNoPrintID = true
		Log(fmt.Sprintf("notify-send rejected -p (%v); updating notifications by hint only", err))
		return nil
	}

	_, err := notifySendRun(append(base, DisplayName, message))
	return err
}

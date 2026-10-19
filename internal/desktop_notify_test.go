package internal

import (
	"errors"
	"slices"
	"testing"
)

func captureNotifySend(t *testing.T, run func(args []string) (string, error)) *[][]string {
	t.Helper()
	var calls [][]string
	previousRun := notifySendRun
	notifyMu.Lock()
	previousIDs, previousNoPrint := notifyReplaceIDs, notifyNoPrintID
	notifyReplaceIDs, notifyNoPrintID = map[string]string{}, false
	notifyMu.Unlock()
	notifySendRun = func(args []string) (string, error) {
		calls = append(calls, args)
		return run(args)
	}
	t.Cleanup(func() {
		notifySendRun = previousRun
		notifyMu.Lock()
		notifyReplaceIDs, notifyNoPrintID = previousIDs, previousNoPrint
		notifyMu.Unlock()
	})
	return &calls
}

func argAfter(args []string, flag string) string {
	if i := slices.Index(args, flag); i >= 0 && i+1 < len(args) {
		return args[i+1]
	}
	return ""
}

// A repeated message replaces the one before it rather than stacking.
func TestNotificationReplacesThePreviousOne(t *testing.T) {
	calls := captureNotifySend(t, func([]string) (string, error) { return "42\n", nil })

	if err := sendLinuxNotification(notifyTagMain, "", "Searching… (2s)"); err != nil {
		t.Fatal(err)
	}
	if err := sendLinuxNotification(notifyTagMain, "", "Searching… (5s)"); err != nil {
		t.Fatal(err)
	}

	first, second := (*calls)[0], (*calls)[1]
	if !slices.Contains(first, "-p") || slices.Contains(first, "-r") {
		t.Errorf("first notification should ask for an id only: %v", first)
	}
	if got := argAfter(second, "-r"); got != "42" {
		t.Errorf("second notification should replace id 42, got %q in %v", got, second)
	}
	if !slices.Contains(second, "string:x-dunst-stack-tag:"+notifyTagMain) ||
		!slices.Contains(second, "string:x-canonical-private-synchronous:"+notifyTagMain) {
		t.Errorf("expected both stacking hints: %v", second)
	}
}

// Different tags keep their own notifications.
func TestNotificationTagsDoNotReplaceEachOther(t *testing.T) {
	calls := captureNotifySend(t, func([]string) (string, error) { return "7", nil })

	_ = sendLinuxNotification(notifyTagMain, "", "a")
	_ = sendLinuxNotification("otakase-update", "", "b")

	if slices.Contains((*calls)[1], "-r") {
		t.Errorf("a different tag must not replace the main notification: %v", (*calls)[1])
	}
}

// An old notify-send without -p still shows the message, by hint alone.
func TestNotificationFallsBackWithoutPrintID(t *testing.T) {
	calls := captureNotifySend(t, func(args []string) (string, error) {
		if slices.Contains(args, "-p") {
			return "", errors.New("exit status 1")
		}
		return "", nil
	})

	if err := sendLinuxNotification(notifyTagMain, "", "hello"); err != nil {
		t.Fatalf("expected the retry to succeed, got %v", err)
	}
	_ = sendLinuxNotification(notifyTagMain, "", "again")

	if len(*calls) != 3 {
		t.Fatalf("expected one failed try, one retry, then hint-only: %v", *calls)
	}
	if slices.Contains((*calls)[2], "-p") {
		t.Errorf("should stop asking for an id once -p is rejected: %v", (*calls)[2])
	}
}

// A daemon that is down fails both ways; that must not turn ids off for good.
func TestNotificationKeepsPrintIDWhenDaemonIsDown(t *testing.T) {
	captureNotifySend(t, func([]string) (string, error) { return "", errors.New("exit status 1") })

	if err := sendLinuxNotification(notifyTagMain, "", "hello"); err == nil {
		t.Fatal("expected an error")
	}
	notifyMu.Lock()
	defer notifyMu.Unlock()
	if notifyNoPrintID {
		t.Error("a failed retry should leave -p on")
	}
}

// A progress notification expires on its own once nothing refreshes it.
func TestNotificationExpiryIsPassedOn(t *testing.T) {
	calls := captureNotifySend(t, func([]string) (string, error) { return "1", nil })

	_ = sendLinuxNotificationFor(notifyTagMain, "", "Searching… (2s)", busyNotifyExpire)
	_ = sendLinuxNotification(notifyTagMain, "", "Done")

	if got := argAfter((*calls)[0], "-t"); got != "5000" {
		t.Errorf("expected a 5000ms expiry, got %q in %v", got, (*calls)[0])
	}
	if slices.Contains((*calls)[1], "-t") {
		t.Errorf("an ordinary message should keep the daemon's timeout: %v", (*calls)[1])
	}
}

package internal

import "testing"

// otakase launched from rofi is a fresh process per action -- opening the
// menu, handing off a cast, adding a show -- and every one of them reaches
// Exit(). Showing the farewell there turned each of those into a desktop
// notification, so a viewer doing something as small as picking a show got
// told to have a great day, over and over, long before they were done.
func TestTheGreetingIsDroppedInRofiMode(t *testing.T) {
	if exitGreetingWanted(&Config{RofiSelection: true}) {
		t.Fatal("the greeting was wanted in rofi mode")
	}
}

// A real terminal session keeps it: there is exactly one exit to say goodbye
// on, and nothing turns the line into a notification.
func TestTheGreetingSurvivesInTerminalMode(t *testing.T) {
	if !exitGreetingWanted(&Config{RofiSelection: false}) {
		t.Fatal("the greeting was dropped in terminal mode")
	}
}

// Exit() is reachable before SetGlobalConfig has run, and Out() treats a nil
// config the same as a terminal one -- RofiSelection defaults false either
// way, so the greeting should not disappear just because nothing has set a
// config yet.
func TestTheGreetingSurvivesWithNoConfigYet(t *testing.T) {
	if !exitGreetingWanted(nil) {
		t.Fatal("the greeting was dropped with no config set")
	}
}

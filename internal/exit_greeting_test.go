package internal

import "testing"

// otakase launched from rofi is a fresh process per action -- opening the
// menu, handing off a cast, adding a show -- and every one of them reaches
// Exit(). Showing the farewell there turned each of those into a desktop
// notification, so a viewer doing something as small as picking a show got
// a goodbye, over and over, long before they were done.
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

// rolls answers each call with the next of the given numbers.
func rolls(n ...int) func(int) int {
	return func(int) int { r := n[0]; n = n[1:]; return r }
}

// The rare lines only show on the one roll out of rareExitGreetingOdds;
// every other exit gets one of the usual goodbyes.
func TestTheRareGreetingsNeedTheLuckyRoll(t *testing.T) {
	for i := range rareExitGreetings {
		if got, rare := exitGreeting(rolls(0, i)); got != rareExitGreetings[i] || !rare {
			t.Fatalf("lucky roll %d gave %q, want %q", i, got, rareExitGreetings[i])
		}
	}
	for i := range exitGreetings {
		if got, rare := exitGreeting(rolls(1, i)); got != exitGreetings[i] || rare {
			t.Fatalf("roll %d gave %q, want %q", i, got, exitGreetings[i])
		}
	}
}

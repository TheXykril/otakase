package internal

import (
	"errors"

	"testing"
	"time"
)

// withCastWindowNonInteractive marks this process as the terminal a rofi cast was
// handed off to, and puts the global config back afterwards.
//
// The flag is read from the global because that is what a spawned session sets:
// RunCastSession mutates the config main handed it, and main registered that same
// pointer as the global before the handoff.
func withCastWindowNonInteractive(t *testing.T) {
	t.Helper()
	previous := GetGlobalConfig()
	SetGlobalConfig(&Config{CastNonInteractive: true})
	t.Cleanup(func() { SetGlobalConfig(previous) })
}

// withoutCastWindow is the ordinary case, for the tests that pin the behaviour
// did not change for a viewer who is actually there.
func withoutCastWindow(t *testing.T) {
	t.Helper()
	previous := GetGlobalConfig()
	SetGlobalConfig(&Config{})
	t.Cleanup(func() { SetGlobalConfig(previous) })
}

// A spawned cast window has nobody at the keyboard. A menu that opens there
// blocks the season on an answer nobody is going to give, and its keystrokes
// would be split with the process-global cast reader anyway -- so the menu must
// refuse to draw at all.
//
// This is the net under every prompt that takes a declared default. The four
// reachable ones branch before they get here; this is what a fifth would hit.
func TestMenuRefusesToDrawInACastWindow(t *testing.T) {
	withCastWindowNonInteractive(t)

	if _, err := DynamicSelect([]SelectionOption{{Key: "yes", Label: "Yes"}}); err != ErrCastNonInteractive {
		t.Errorf("DynamicSelect error = %v, want ErrCastNonInteractive", err)
	}
	if _, err := DynamicSelectPreserveOrder([]SelectionOption{{Key: "yes", Label: "Yes"}}); err != ErrCastNonInteractive {
		t.Errorf("DynamicSelectPreserveOrder error = %v, want ErrCastNonInteractive", err)
	}
}

// promptCancelable builds its own Bubble Tea program rather than going through
// dynamicSelectInternal, so it needs refusing in its own right. It answers
// cancelled, which every caller already reads as "back out".
func TestTextPromptIsCancelledInACastWindow(t *testing.T) {
	withCastWindowNonInteractive(t)

	value, cancelled, err := promptCancelable(&Config{}, "Season", "Episode number?", "a number")
	if err != nil {
		t.Fatalf("promptCancelable: %v", err)
	}
	if !cancelled {
		t.Error("a text prompt in a cast window was not cancelled")
	}
	if value != "" {
		t.Errorf("value = %q, want empty on a cancelled prompt", value)
	}
}

// The safety net must not fire for an ordinary run, or every menu in the program
// would break.
func TestMenuStillDrawsWithoutTheCastWindowFlag(t *testing.T) {
	withoutCastWindow(t)

	// Reaching the error here is what matters; a real menu needs a terminal, so
	// the assertion is only that the guard did not short-circuit it.
	if _, err := DynamicSelect([]SelectionOption{{Key: "yes", Label: "Yes"}}); err == ErrCastNonInteractive {
		t.Error("the cast window guard fired on an ordinary run")
	}
}

// An exact title and episode-count match is the strongest signal the mapper
// produces, and the alternative in a cast window is a manual menu that cannot be
// shown -- which would end the episode. So it is taken.
func TestConfirmProviderMatchIsTakenInACastWindow(t *testing.T) {
	withCastWindowNonInteractive(t)

	withPromptSelect(t, func([]SelectionOption) (SelectionOption, error) {
		t.Error("a menu was opened to confirm a provider match with no viewer")
		return SelectionOption{}, nil
	})

	if !confirmProviderMatch(nil, SelectionOption{Label: "Example"}, "title and episode count") {
		t.Error("an exact provider match was refused in a cast window")
	}
}

// And a viewer who is there is still asked, unchanged.
func TestConfirmProviderMatchStillAsksWithAViewer(t *testing.T) {
	withoutCastWindow(t)

	withPromptSelect(t, func(options []SelectionOption) (SelectionOption, error) {
		if options[0].Key != "use" || options[1].Key != "manual" {
			t.Fatalf("unexpected options: %+v", options)
		}
		return SelectionOption{Key: "manual"}, nil
	})

	if confirmProviderMatch(nil, SelectionOption{Label: "Example"}, "title") {
		t.Error("the viewer declined the match but it was used anyway")
	}
}

// The recovery menu has exactly one answer that needs no viewer, and it is the
// one that can rescue a stale provider mapping.
func TestRecoveryMenuAnswersItselfInACastWindow(t *testing.T) {
	withCastWindowNonInteractive(t)

	withPromptSelect(t, func([]SelectionOption) (SelectionOption, error) {
		t.Error("the playback recovery menu opened with no viewer")
		return SelectionOption{}, nil
	})

	anime := &Anime{AnilistId: 1, Ep: Episode{Number: 5}}
	if got := promptEpisodeLinkFailureRecovery(&Config{}, anime, errors.New("fail"), false); got != "remap" {
		t.Errorf("recovery answer = %q, want remap", got)
	}
}

// A viewer who is there gets the menu, unchanged.
func TestRecoveryMenuStillAsksWithAViewer(t *testing.T) {
	withoutCastWindow(t)

	withPromptSelect(t, func([]SelectionOption) (SelectionOption, error) {
		return SelectionOption{Key: "back"}, nil
	})

	anime := &Anime{AnilistId: 1, Ep: Episode{Number: 5}}
	if got := promptEpisodeLinkFailureRecovery(&Config{}, anime, errors.New("fail"), false); got != "back" {
		t.Errorf("recovery answer = %q, want back", got)
	}
}

// The one that matters most. resolveEpisodeLinksWithRecovery is a bare for {} in
// which "remap" continues, so today only a viewer eventually backing out ends it.
// A cast window answers the menu itself, and an answer that repeats forever is a
// worse hang than the prompt it replaced.
//
// Without the bound this test does not fail -- it hangs, and takes the suite with
// it. That is the point of writing it.
func TestRecoveryLoopIsBoundedWithNoViewer(t *testing.T) {
	withCastWindowNonInteractive(t)

	// Every resolve fails in both languages, and a search result is always
	// available, so a remap "succeeds" and the loop is allowed to continue. Only
	// the bound can stop it.
	provider := &stackStubProvider{
		name: "anikoto",
		episodeErrors: map[string]map[string]error{
			"anikoto-id": {
				"sub": errors.New("no sub"),
				"dub": errors.New("no dub"),
			},
		},
		searchResults: map[string][]SelectionOption{
			"sub": {{Title: "Example", Key: "anikoto-id"}},
			"dub": {{Title: "Example", Key: "anikoto-id"}},
		},
	}
	withProviderFactories(t, provider)

	// No menu may be reached at all in a cast window.
	withPromptSelect(t, func(options []SelectionOption) (SelectionOption, error) {
		t.Errorf("a menu opened in a cast window: %+v", options)
		return SelectionOption{Key: "back"}, nil
	})

	// AutoAudioFallback off, so the first thing the loop reaches is the recovery
	// answer this change introduced.
	config := &Config{Provider: `["anikoto"]`, SubOrDub: "sub", AutoAudioFallback: false}
	anime := &Anime{
		Title:        AnimeTitle{Romaji: "Example"},
		ProviderName: "anikoto",
		ProviderId:   "anikoto-id",
		Ep:           Episode{Number: 3},
	}

	done := make(chan struct{})
	var ok bool
	go func() {
		_, ok = resolveEpisodeLinksWithRecovery(config, anime, nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("resolveEpisodeLinksWithRecovery did not terminate with no viewer: " +
			"the automatic remap is repeating without a bound")
	}

	if ok {
		t.Error("a resolve that never succeeds reported success")
	}
	// One remap is allowed, so the recovery path is entered at most twice: the
	// attempt, and the arrival that spends it.
	if rounds := countCalls(provider.calls, "anikoto:sub"); rounds > 2 {
		t.Errorf("preferred resolve attempted %d times, want at most 2", rounds)
	}
}

// countCalls reports how many times one provider call appears.
func countCalls(calls []string, want string) int {
	n := 0
	for _, c := range calls {
		if c == want {
			n++
		}
	}
	return n
}

// The rating countdown declines when nobody answers. A menu that blocks the
// season on an absent viewer is the failure this whole change exists to remove.
//
// castControlsBegin returning true is the terminal-present case, so the countdown
// genuinely runs and runs out. Returning false here would make the test pass for
// the wrong reason -- declining at once because there was no keyboard, rather than
// declining because the window went unanswered.
func TestCastAwaitYesNoDeclinesWhenNobodyAnswers(t *testing.T) {
	resetCastControlsForTest(t)
	castControlsBegin = func(*Config) bool { return true }

	started := time.Now()
	if castAwaitYesNo(&Config{}, &Anime{}, "Rate this anime?", 200*time.Millisecond) {
		t.Error("the rating countdown accepted with no keypress")
	}
	// It waited, rather than declining immediately.
	if elapsed := time.Since(started); elapsed < 100*time.Millisecond {
		t.Errorf("declined after %v, want the countdown to actually run", elapsed)
	}
}

// No terminal means no viewer, so it declines at once rather than sitting out a
// countdown nobody can see. castControlsBegin returning false is the no-keyboard
// case, which is what startCastControls reports as unavailable.
func TestCastAwaitYesNoDeclinesWithNoTerminal(t *testing.T) {
	resetCastControlsForTest(t)
	castControlsBegin = func(*Config) bool { return false }

	started := time.Now()
	if castAwaitYesNo(&Config{}, &Anime{}, "Rate this anime?", 10*time.Second) {
		t.Error("the rating countdown accepted with no keyboard to answer with")
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("waited %v with no terminal, want an immediate decline", elapsed)
	}
}

// A keypress accepts, delivered the way the reader delivers one. Taking a
// subscription is what makes this possible at all: without a live subscription
// the process-global reader would consume the byte and drop it, and the viewer
// could press every key without the cast noticing.
func TestCastAwaitYesNoAcceptsAKeypress(t *testing.T) {
	resetCastControlsForTest(t)
	castControlsBegin = func(*Config) bool { return true }

	go func() {
		time.Sleep(30 * time.Millisecond)
		castDeliverCommand(castCmdPauseToggle) // any key will do
	}()

	if !castAwaitYesNo(&Config{}, &Anime{}, "Rate this anime?", 10*time.Second) {
		t.Error("the rating countdown ignored a keypress")
	}
}

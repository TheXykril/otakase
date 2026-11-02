package internal

import (
	"errors"

	"testing"
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

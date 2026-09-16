package internal

import (
	"os"
	"strings"
	"testing"

	"github.com/thexykril/otakase/internal/theme"
)

func init() { ApplyTheme(theme.Builtin()) }

// withStdin feeds a line to the prompt the way a person would.
func withStdin(t *testing.T, input string, run func()) {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdin
	os.Stdin = read
	t.Cleanup(func() { os.Stdin = original; read.Close() })

	go func() {
		_, _ = write.WriteString(input)
		write.Close()
	}()
	run()
}

// Backing out of a question is the most ordinary thing to want. It used to
// close the program: the prompt reported an empty answer as an error and the
// caller turned that into quitting.
func TestEmptyAnswerCancelsRatherThanFailing(t *testing.T) {
	withStdin(t, "\n", func() {
		value, cancelled, err := promptCancelable(&CurdConfig{}, "Untracked", "Search", "empty to go back")
		if err != nil {
			t.Errorf("an empty answer should not be an error: %v", err)
		}
		if !cancelled {
			t.Error("an empty answer should cancel")
		}
		if value != "" {
			t.Errorf("expected no value, got %q", value)
		}
	})
}

// Whitespace is still empty. Someone backing out may well hit space first.
func TestWhitespaceOnlyAnswerAlsoCancels(t *testing.T) {
	withStdin(t, "   \n", func() {
		_, cancelled, err := promptCancelable(&CurdConfig{}, "Untracked", "Search", "")
		if err != nil || !cancelled {
			t.Errorf("whitespace should cancel, got cancelled=%v err=%v", cancelled, err)
		}
	})
}

// A real answer comes back trimmed and uncancelled.
func TestAnAnswerIsReturned(t *testing.T) {
	withStdin(t, "  frieren  \n", func() {
		value, cancelled, err := promptCancelable(&CurdConfig{}, "Untracked", "Search", "")
		if err != nil || cancelled {
			t.Fatalf("a real answer was treated as cancelling: %v %v", cancelled, err)
		}
		if value != "frieren" {
			t.Errorf("expected the answer trimmed, got %q", value)
		}
	})
}

// An episode number that is not a number should cost the typo, not the search
// already done.
func TestEpisodePromptAsksAgainAfterATypo(t *testing.T) {
	withStdin(t, "twelve\n12\n", func() {
		number, cancelled, err := promptEpisodeCancelable(&CurdConfig{}, "Untracked", "Which episode?", "")
		if err != nil || cancelled {
			t.Fatalf("a typo ended the prompt: cancelled=%v err=%v", cancelled, err)
		}
		if number != 12 {
			t.Errorf("expected 12, got %d", number)
		}
	})
}

// And an empty answer there backs out too.
func TestEpisodePromptCancels(t *testing.T) {
	withStdin(t, "\n", func() {
		_, cancelled, err := promptEpisodeCancelable(&CurdConfig{}, "Untracked", "Which episode?", "")
		if err != nil || !cancelled {
			t.Errorf("an empty episode number should cancel, got cancelled=%v err=%v", cancelled, err)
		}
	})
}

// The prompt is drawn in the same frame as the menus, so stepping into it does
// not look like leaving the program.
func TestPromptIsFramedLikeTheMenu(t *testing.T) {
	out := renderPrompt("Untracked", "Search for an anime", "empty to go back")
	for _, want := range []string{DisplayName, "Untracked", "Search for an anime", "empty to go back", "─"} {
		if !strings.Contains(out, want) {
			t.Errorf("the prompt is missing %q:\n%s", want, out)
		}
	}
	// The way out has to be visible, since there is no other sign of it.
	if !strings.Contains(out, "go back") {
		t.Error("the prompt does not say how to leave")
	}
}

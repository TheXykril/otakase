package internal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/thexykril/otakase/internal/theme"
)

func init() { ApplyTheme(theme.Builtin()) }

// A typo should cost the typo, not the search already done.
func TestEpisodeAsksAgainAfterATypo(t *testing.T) {
	answers := []string{"twelve", "0", "12"}
	i := 0
	number, cancelled, err := episodeFromAnswers(func() (string, bool, error) {
		answer := answers[i]
		i++
		return answer, false, nil
	})
	if err != nil || cancelled {
		t.Fatalf("a typo ended the prompt: cancelled=%v err=%v", cancelled, err)
	}
	if number != 12 {
		t.Errorf("expected 12, got %d", number)
	}
	if i != len(answers) {
		t.Errorf("expected all %d answers to be read, read %d", len(answers), i)
	}
}

// Backing out of the episode question leaves, rather than looping on nothing.
func TestEpisodeCancels(t *testing.T) {
	_, cancelled, err := episodeFromAnswers(func() (string, bool, error) {
		return "", true, nil
	})
	if err != nil || !cancelled {
		t.Errorf("cancelling should leave, got cancelled=%v err=%v", cancelled, err)
	}
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

// Escape has to work, and it cannot at a line-based prompt: a terminal in its
// normal mode hands over nothing until enter, so an escape typed there arrives
// buried in the answer. The prompt reads keys for this reason alone.
func TestEscapeCancelsThePrompt(t *testing.T) {
	m := newPromptModel("Untracked", "Search", "esc to go back", "frieren")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	answered := updated.(promptModel)

	if cmd == nil {
		t.Error("escape did not end the prompt")
	}
	if !answered.cancelled {
		t.Error("escape should cancel")
	}
	// Whatever was typed is discarded: escape means "never mind", not "search
	// for what I had so far".
	if answered.value() != "" {
		t.Errorf("escape kept the answer %q", answered.value())
	}
}

// Enter on something typed answers the question.
func TestEnterAnswersThePrompt(t *testing.T) {
	m := newPromptModel("Untracked", "Search", "", "  frieren  ")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	answered := updated.(promptModel)

	if cmd == nil {
		t.Error("enter did not end the prompt")
	}
	if answered.cancelled {
		t.Error("enter on a real answer should not cancel")
	}
	if answered.value() != "frieren" {
		t.Errorf("expected the answer trimmed, got %q", answered.value())
	}
}

// Both ways of backing out behave the same, rather than one of them meaning
// "search for nothing".
func TestEnterOnAnEmptyPromptAlsoCancels(t *testing.T) {
	for _, initial := range []string{"", "   "} {
		m := newPromptModel("Untracked", "Search", "", initial)
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if !updated.(promptModel).cancelled {
			t.Errorf("enter on %q should cancel", initial)
		}
	}
}

// Ctrl+C closes things everywhere else in the program and must here too.
func TestCtrlCCancelsThePrompt(t *testing.T) {
	m := newPromptModel("Untracked", "Search", "", "frieren")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !updated.(promptModel).cancelled {
		t.Error("ctrl+c should cancel")
	}
}

// Typing reaches the field rather than being swallowed by the key handling.
func TestTypingReachesThePromptField(t *testing.T) {
	m := newPromptModel("Untracked", "Search", "", "")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("frieren")})
	typed := updated.(promptModel)
	if typed.cancelled {
		t.Fatal("typing cancelled the prompt")
	}
	if got := typed.input.Value(); got != "frieren" {
		t.Errorf("the field holds %q", got)
	}
}

// A question that is really "change this" starts from what it would change, so
// correcting one word costs one word rather than retyping the line.
func TestAPrefilledPromptStartsFromItsDefault(t *testing.T) {
	m := newPromptModel("Provider", "Search providers under a different name", "esc to go back", "frieren")

	if got := m.input.Value(); got != "frieren" {
		t.Errorf("the field starts at %q", got)
	}
	if !strings.Contains(m.View(), "frieren") {
		t.Error("the prompt does not show the name being changed")
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	answered := updated.(promptModel)
	if answered.cancelled {
		t.Error("enter on a prefilled prompt should accept the default")
	}
	if answered.value() != "frieren" {
		t.Errorf("enter gave %q rather than the default", answered.value())
	}
}

// Progress is counted from zero: "none watched yet" is a real answer, and the
// only thing separating it from an episode number.
func TestProgressAcceptsZeroWhereAnEpisodeNumberDoesNot(t *testing.T) {
	if _, err := parseNonNegativeIntInput("0", "progress"); err != nil {
		t.Errorf("zero progress was rejected: %v", err)
	}
	if _, err := parsePositiveIntInput("0", "episode number"); err == nil {
		t.Error("episode zero was accepted")
	}
}

// Not rating something is the ordinary answer to a question that arrives
// unbidden after an episode ends. It used to be read as a score of nothing,
// fail to parse, and close the program.
func TestBackingOutOfTheScoreIsNotAScore(t *testing.T) {
	score, cancelled, err := valueFromAnswers(parseAnimeScore, func() (string, bool, error) {
		return "", true, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cancelled {
		t.Error("backing out should be reported as cancelled")
	}
	if score != 0 {
		t.Errorf("a cancelled question produced the score %v", score)
	}
}

func TestScoreMustBeWithinTen(t *testing.T) {
	for _, answer := range []string{"-1", "10.5", "eleven", ""} {
		if _, err := parseAnimeScore(answer); err == nil {
			t.Errorf("%q was accepted as a score", answer)
		}
	}
	for _, answer := range []string{"0", "7.5", " 10 "} {
		if _, err := parseAnimeScore(answer); err != nil {
			t.Errorf("%q was rejected: %v", answer, err)
		}
	}
}

// A typo costs the typo, not the work already done: the question is asked
// again rather than handed back as an error.
func TestAScoreTypoIsAskedAboutAgain(t *testing.T) {
	answers := []string{"eleven", "8"}
	asked := 0
	score, cancelled, err := valueFromAnswers(parseAnimeScore, func() (string, bool, error) {
		answer := answers[asked]
		asked++
		return answer, false, nil
	})
	if err != nil || cancelled {
		t.Fatalf("unexpected outcome: cancelled=%v err=%v", cancelled, err)
	}
	if asked != 2 {
		t.Errorf("the question was asked %d times", asked)
	}
	if score != 8 {
		t.Errorf("got the score %v", score)
	}
}

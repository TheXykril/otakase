package internal

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func parseNonNegativeIntInput(input, label string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(input))
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", label, err)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s cannot be negative", label)
	}
	return value, nil
}

func parsePositiveIntInput(input, label string) (int, error) {
	value, err := parseNonNegativeIntInput(input, label)
	if err != nil {
		return 0, err
	}
	if value == 0 {
		return 0, fmt.Errorf("%s must be greater than zero", label)
	}
	return value, nil
}

func isAffirmativeAnswer(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// renderPrompt draws a question in the same frame the menus use, so stepping
// from a list into a prompt does not look like leaving the program.
func renderPrompt(section, question, hint string) string {
	width := lipgloss.Width(question)
	if w := lipgloss.Width(hint); w > width {
		width = w
	}
	width += 4

	var b strings.Builder
	b.WriteString(renderBreadcrumb(section))
	b.WriteString("\n")
	b.WriteString(renderRule(width))
	b.WriteString("\n\n")
	b.WriteString(paneTitleStyle.Render(question))
	b.WriteString("\n")
	if hint != "" {
		b.WriteString(footerTextStyle.Render(hint))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(filterLabelStyle.Render("› "))
	return b.String()
}

// promptCancelable asks a question and treats escape, or an empty answer, as
// cancelling.
//
// The prompts this replaces reported an empty answer as an error, and every
// caller turned that error into quitting the program -- so pressing enter at a
// question you had opened by accident closed everything. Backing out of a
// question is the most ordinary thing to want, and it should cost nothing.
func promptCancelable(config *CurdConfig, section, question, hint string) (value string, cancelled bool, err error) {
	return promptCancelableWithDefault(config, section, question, hint, "")
}

// promptCancelableWithDefault is promptCancelable with the field already
// holding an answer.
//
// A question that is really "change this" starts from what it would change, so
// correcting one word costs one word rather than retyping the whole line.
func promptCancelableWithDefault(config *CurdConfig, section, question, hint, initial string) (value string, cancelled bool, err error) {
	if config != nil && config.RofiSelection {
		// rofi has its own frame; the hint goes in the prompt where it is the
		// only place it can be seen.
		input, rofiErr := GetUserInputFromRofi(question)
		if rofiErr != nil {
			return "", true, nil
		}
		input = strings.TrimSpace(input)
		return input, input == "", nil
	}

	model := newPromptModel(section, question, hint, initial)
	finished, err := tea.NewProgram(model).Run()
	if err != nil {
		return "", false, err
	}
	answered, ok := finished.(promptModel)
	if !ok {
		return "", true, nil
	}
	return answered.value(), answered.cancelled, nil
}

// promptEpisodeCancelable asks for an episode number, cancelling on an empty
// answer and asking again on one that is not a number.
func promptEpisodeCancelable(config *CurdConfig, section, question, hint string) (int, bool, error) {
	return episodeFromAnswers(func() (string, bool, error) {
		return promptCancelable(config, section, question, hint)
	})
}

// episodeFromAnswers turns repeated answers into an episode number, asking
// again after one that is not a number.
//
// The asking is a parameter so the retrying can be tested without a terminal:
// the prompt itself needs one, and the rule worth pinning -- that a typo costs
// the typo and not the search already done -- is in here, not in the reading.
func episodeFromAnswers(ask func() (string, bool, error)) (int, bool, error) {
	for {
		input, cancelled, err := ask()
		if err != nil || cancelled {
			return 0, true, err
		}
		number, parseErr := parsePositiveIntInput(input, "episode number")
		if parseErr == nil {
			return number, false, nil
		}
		CurdOut(fmt.Sprintf("%v — try again, or press escape to go back.", parseErr))
	}
}

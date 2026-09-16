package internal

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// promptModel asks one question.
//
// It reads keys rather than lines because escape cannot be seen otherwise: a
// terminal in its normal mode hands over nothing until enter is pressed, so an
// escape typed at a line-based prompt arrives as part of the answer, if at all.
type promptModel struct {
	input     textinput.Model
	section   string
	question  string
	hint      string
	cancelled bool
}

func newPromptModel(section, question, hint, initial string) promptModel {
	field := textinput.New()
	field.Prompt = "› "
	field.SetValue(initial)
	field.Focus()
	field.CharLimit = 200

	return promptModel{
		input:    field,
		section:  section,
		question: question,
		hint:     hint,
	}
}

func (m promptModel) Init() tea.Cmd { return textinput.Blink }

func (m promptModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := message.(tea.KeyMsg); ok {
		switch key.Type {
		case tea.KeyEsc, tea.KeyCtrlC:
			m.cancelled = true
			return m, tea.Quit
		case tea.KeyEnter:
			// An empty answer cancels as well, so both ways of backing out
			// behave the same rather than one of them meaning "search for
			// nothing".
			m.cancelled = strings.TrimSpace(m.input.Value()) == ""
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(message)
	return m, cmd
}

func (m promptModel) View() string {
	return renderPrompt(m.section, m.question, m.hint) + m.input.View() + "\n"
}

// value is the trimmed answer, empty when cancelled.
func (m promptModel) value() string {
	if m.cancelled {
		return ""
	}
	return strings.TrimSpace(m.input.Value())
}

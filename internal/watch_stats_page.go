package internal

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thexykril/otakase/internal/theme"
)

// ShowWatchStats opens the stats page and returns when the viewer leaves it.
// There is nothing to choose on it, so every way out leads back to the menu.
func ShowWatchStats(config *Config) {
	title, sections := watchStatsPage(config)
	if config != nil && config.RofiSelection {
		showWatchStatsRofi(title, renderStatsPlain(sections))
		return
	}
	showWatchStatsTerminal(title, sections)
}

// watchStatsPage fetches and lays out the page: a heading naming where the
// numbers came from, and the sections beneath it.
func watchStatsPage(config *Config) (string, []statsSection) {
	if statsTrackerKey(config) == TrackingRemoteNone {
		return "Stats", []statsSection{{Note: statsNeedsTracker}}
	}

	source := RemoteTrackingDisplayName(config)
	if config != nil && !config.RofiSelection {
		// Paging AniList's history takes a moment; an empty screen for that
		// long reads as a hang.
		Out(fmt.Sprintf("Reading stats from %s…", source))
	}
	stats, err := fetchWatchStats(config)
	if err != nil {
		Log(fmt.Sprintf("Stats: %v", err))
		return "Stats", []statsSection{{Note: statsErrorMessage(source, err)}}
	}
	return "Stats · " + stats.Source, buildStatsSections(stats, time.Now())
}

// showWatchStatsRofi puts the page in rofi's message area above the Back and
// Quit rows every rofi menu gets, the same way release notes are shown.
func showWatchStatsRofi(title, body string) {
	palette := theme.Active()
	message := `<span foreground="` + palette.Accent + `"><b>` + escapePango(title) + `</b></span>` + "\n\n" +
		// Monospace so the columns of labels and values line up.
		`<tt>` + escapePango(body) + `</tt>`
	selected, err := RofiSelectWithMessage(nil, false, "Stats", message)
	if err != nil {
		Log(fmt.Sprintf("Stats: rofi: %v", err))
		return
	}
	if SelectionMeansQuit(NormalizeSelectionKey(selected)) {
		Exit(nil)
	}
}

func showWatchStatsTerminal(title string, sections []statsSection) {
	if _, err := tea.NewProgram(statsPageModel{title: title, sections: sections}).Run(); err != nil {
		// The page is a courtesy; failing to draw it must not end the session.
		Log(fmt.Sprintf("Stats: terminal page: %v", err))
		Out(renderStatsPlain(sections))
	}
}

// statsPageModel is a page of text that any key dismisses.
type statsPageModel struct {
	title    string
	sections []statsSection
	width    int
}

func (m statsPageModel) Init() tea.Cmd { return nil }

func (m statsPageModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
	case tea.KeyMsg:
		return m, tea.Quit
	}
	return m, nil
}

func (m statsPageModel) View() string {
	width := m.width
	if width <= 0 {
		width = 60
	}
	var b strings.Builder
	b.WriteString(renderHeader(m.title, width))
	b.WriteString("\n")
	b.WriteString(renderRule(width))
	b.WriteString("\n\n")
	// The same text rofi shows, with the headings and notes told apart by
	// colour; the rows are left plain so the values are what stands out.
	labelWidth := statsLabelWidth(m.sections)
	for i, section := range m.sections {
		if i > 0 {
			b.WriteString("\n")
		}
		if section.Title != "" {
			b.WriteString(paneTitleStyle.Render(section.Title) + "\n")
		}
		if len(section.Rows) > 0 {
			b.WriteString(renderStatsRows(section.Rows, labelWidth) + "\n")
		}
		if section.Note != "" {
			b.WriteString(paneMetaStyle.Render(section.Note) + "\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(renderKeyHints([]keyHint{{Key: "any key", Label: "back"}}, width))
	b.WriteString("\n")
	return b.String()
}

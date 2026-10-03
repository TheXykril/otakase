package internal

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/thexykril/otakase/internal/icons"
	"github.com/thexykril/otakase/internal/theme"
)

// The update prompt is a short summary over the choices, with the full
// changelog one row away. The summary is a handful of headlines so the
// choices stay on screen however many releases were skipped; reading
// everything is a choice of its own, on a page that scrolls.

const (
	updateSummaryItems = 3
	// rofiChangelogWidth is how many characters of the monospace menu font
	// fit on one row of the 780px update menu, so long entries wrap instead
	// of running off the edge.
	rofiChangelogWidth = 84
)

// updateActionOptions are the choices, in order: Update now is first.
func updateActionOptions(state updatePendingState) []SelectionOption {
	latest := normalizeReleaseVersion(state.LatestVersion)
	update := icons.Label(icons.Download, "Update now")
	if latest != "" {
		update += " · " + latest
	}
	options := []SelectionOption{
		{Key: "update", Label: update},
		{Key: "changelog", Label: icons.Label(icons.Changelog, "Read the changelog")},
	}
	if strings.TrimSpace(state.HTMLURL) != "" {
		options = append(options, SelectionOption{Key: "release", Label: icons.Label(icons.OpenLink, "Open the release page")})
	}
	return append(options,
		SelectionOption{Key: "later", Label: icons.Label(icons.Clock, "Remind me later") + " · tomorrow"},
		SelectionOption{Key: "skip", Label: icons.Label(icons.Skip, "Skip this version")},
		SelectionOption{Key: "disable", Label: icons.Label(icons.BellOff, "Stop checking for updates")},
		SelectionOption{Key: "continue", Label: icons.Label(icons.Play, "Continue without updating")},
	)
}

// updateReleases is the changelog to show for a stored update. A state saved
// by an older otakase has only the raw release notes, which are read the same
// way the release-notes fallback is.
func updateReleases(state updatePendingState) []changelogRelease {
	if len(state.Changelog) > 0 {
		return state.Changelog
	}
	if strings.TrimSpace(state.ReleaseNotes) == "" {
		return nil
	}
	return []changelogRelease{parseReleaseBody(normalizeReleaseVersion(state.LatestVersion), state.ReleaseNotes)}
}

// updateHeading is the prompt's title and the line under it.
func updateHeading(currentVersion string, state updatePendingState, releases []changelogRelease) (title, sub string) {
	title = fmt.Sprintf("%s %s is available", DisplayName, normalizeReleaseVersion(state.LatestVersion))
	sub = "You have " + normalizeReleaseVersion(currentVersion)
	if len(releases) > 1 {
		sub += fmt.Sprintf(" · %d releases since", len(releases))
	}
	return title, sub
}

func moreChangesLine(more int) string {
	switch more {
	case 0:
		return ""
	case 1:
		return "and 1 more change in the changelog"
	default:
		return fmt.Sprintf("and %d more changes in the changelog", more)
	}
}

// updateRofiMessage is the summary above the rofi choices.
func updateRofiMessage(currentVersion string, state updatePendingState, releases []changelogRelease) string {
	p := theme.Active()
	title, sub := updateHeading(currentVersion, state, releases)
	var b strings.Builder
	b.WriteString(`<span foreground="` + p.Foreground + `" size="large"><b>` + escapePango(title) + `</b></span>` + "\n")
	b.WriteString(`<span foreground="` + p.Muted + `">` + escapePango(sub) + `</span>`)
	headlines, more := changelogSummary(releases, updateSummaryItems)
	if len(headlines) > 0 {
		b.WriteString("\n")
	}
	for _, headline := range headlines {
		b.WriteString("\n" + `<span foreground="` + p.Accent + `">•</span> <span foreground="` + p.Foreground + `">` + escapePango(headline) + `</span>`)
	}
	if line := moreChangesLine(more); line != "" {
		b.WriteString("\n" + `<span foreground="` + p.Muted + `">` + escapePango(line) + `</span>`)
	}
	return b.String()
}

// changelogRofiRows lays the changelog out one rofi row per line: a version
// heading, its sections, and each change wrapped to the menu's width with its
// lead in full strength and the rest dimmed.
func changelogRofiRows(releases []changelogRelease) []string {
	p := theme.Active()
	span := func(color, text string) string {
		return `<span foreground="` + color + `">` + text + `</span>`
	}
	var rows []string
	for i, release := range releases {
		if i > 0 {
			rows = append(rows, " ")
		}
		heading := span(p.Accent, "<b>"+escapePango(icons.Label(icons.Tag, release.Version))+"</b>")
		if date := formatChangelogDate(release.Date); date != "" {
			heading += "  " + span(p.Muted, escapePango(date))
		}
		rows = append(rows, heading)
		for _, section := range release.Sections {
			if section.Title != "" {
				rows = append(rows, "  "+span(p.Muted, "<b>"+escapePango(section.Title)+"</b>"))
			}
			for _, item := range section.Items {
				rows = append(rows, changelogItemRofiRows(item, p)...)
			}
		}
	}
	return rows
}

func changelogItemRofiRows(item changelogItem, p theme.Palette) []string {
	lines := wrapWords(item.joined(), rofiChangelogWidth-4)
	leadLeft := len([]rune(item.Lead))
	rows := make([]string, 0, len(lines))
	for i, line := range lines {
		prefix := "    "
		if i == 0 {
			prefix = `  <span foreground="` + p.Accent + `">•</span> `
		}
		// The lead can wrap across rows; whatever of it is on this row is
		// bold, and the rest of the row is the dimmer body text.
		runes := []rune(line)
		cut := leadLeft
		if cut > len(runes) {
			cut = len(runes)
		}
		var row strings.Builder
		row.WriteString(prefix)
		if cut > 0 {
			row.WriteString(`<span foreground="` + p.Foreground + `"><b>` + escapePango(string(runes[:cut])) + `</b></span>`)
		}
		if rest := string(runes[cut:]); rest != "" {
			row.WriteString(`<span foreground="` + p.Muted + `">` + escapePango(rest) + `</span>`)
		}
		rows = append(rows, row.String())
		// The space wrapWords dropped between this row and the next.
		leadLeft -= len(runes) + 1
		if leadLeft < 0 {
			leadLeft = 0
		}
	}
	return rows
}

// errQuitFromPage is what a page returns when its Quit row was picked.
var errQuitFromPage = errors.New("quit")

// showRofiPage shows rows of markup with nothing to choose among them, for
// reading: any row, Back, or Escape returns. The rows are tighter than a
// menu's, since they are lines of one text rather than separate choices.
func showRofiPage(rows []string, prompt, message string) error {
	defer suspendBusy()()
	EndStartupProgress()

	all := append(append([]string(nil), rows...), "Back", "Quit")
	configPath := filepath.Join(GetStoragePath(), "selectanime.rasi")
	args := []string{"-dmenu", "-theme", configPath, "-i", "-no-custom", "-markup-rows", "-format", "i", "-p", prompt}
	args = append(args, rofiVersionThemeArgs()...)
	args = append(args, "-theme-str", `listview { lines: 16; } element { padding: 2px 12px; }`)
	if strings.TrimSpace(message) != "" {
		args = append(args, "-mesg", message)
	}
	cmd := exec.Command("rofi", args...)
	cmd.Stdin = strings.NewReader(strings.Join(all, "\n"))
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil
		}
		return fmt.Errorf("failed to run Rofi: %w", err)
	}
	if index, err := strconv.Atoi(strings.TrimSpace(stdout.String())); err == nil && index == len(all)-1 {
		return errQuitFromPage
	}
	return nil
}

// openInBrowser opens a link with the desktop's default handler.
func openInBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// chooseUpdateActionRofi runs the rofi prompt until a choice that ends it.
func chooseUpdateActionRofi(currentVersion string, state updatePendingState) (SelectionOption, error) {
	releases := updateReleases(state)
	message := updateRofiMessage(currentVersion, state, releases)
	options := updateActionOptions(state)
	for {
		selected, err := RofiSelectWithMessage(options, false, "Update", message)
		if err != nil {
			return SelectionOption{}, err
		}
		selected = NormalizeSelectionKey(selected)
		switch selected.Key {
		case "changelog":
			title, _ := updateHeading(currentVersion, state, releases)
			header := `<span foreground="` + theme.Active().Foreground + `"><b>` + escapePango("Changelog · "+title) + `</b></span>`
			rows := changelogRofiRows(releases)
			if len(rows) == 0 {
				rows = []string{escapePango("No changelog was published for this release.")}
			}
			if err := showRofiPage(rows, "Changelog", header); errors.Is(err, errQuitFromPage) {
				return SelectionOption{Key: "-1", Label: "Quit"}, nil
			} else if err != nil {
				Log(fmt.Sprintf("Update changelog: %v", err))
			}
		case "release":
			if err := openInBrowser(state.HTMLURL); err != nil {
				Log(fmt.Sprintf("Opening the release page: %v", err))
			}
		default:
			return selected, nil
		}
	}
}

// promptUpdateTerminal runs the terminal prompt; tests replace it.
var promptUpdateTerminal = runUpdatePromptTerminal

func runUpdatePromptTerminal(currentVersion string, state updatePendingState) (SelectionOption, error) {
	// Like every terminal menu: none in a cast window nobody is typing in.
	if castWindowNonInteractive() {
		return SelectionOption{}, ErrCastNonInteractive
	}
	defer suspendBusy()()
	EndStartupProgress()
	model := newUpdatePromptModel(currentVersion, state)
	final, err := tea.NewProgram(model).Run()
	if err != nil {
		return SelectionOption{}, err
	}
	result := final.(updatePromptModel)
	return result.chosen, nil
}

// updatePromptModel is the terminal prompt: the summary and the choices, and
// a scrolling changelog page in place of them when asked for.
type updatePromptModel struct {
	title, sub string
	headlines  []string
	more       int
	releases   []changelogRelease
	options    []SelectionOption
	releaseURL string

	selected      int
	width, height int
	reading       bool
	page          viewport.Model
	chosen        SelectionOption
	notice        string
}

func newUpdatePromptModel(currentVersion string, state updatePendingState) updatePromptModel {
	releases := updateReleases(state)
	title, sub := updateHeading(currentVersion, state, releases)
	headlines, more := changelogSummary(releases, updateSummaryItems)
	return updatePromptModel{
		title:      title,
		sub:        sub,
		headlines:  headlines,
		more:       more,
		releases:   releases,
		options:    updateActionOptions(state),
		releaseURL: state.HTMLURL,
		width:      80,
		height:     24,
	}
}

func (m updatePromptModel) Init() tea.Cmd { return nil }

func (m updatePromptModel) contentWidth() int {
	width := m.width - 2
	if width > 100 {
		width = 100
	}
	if width < 40 {
		width = 40
	}
	return width
}

func (m updatePromptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.reading {
			m.page = m.newPage()
		}
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.chosen = SelectionOption{Key: "-1", Label: "Quit"}
			return m, tea.Quit
		}
		if m.reading {
			switch msg.String() {
			case "esc", "q", "backspace", "left", "h", "enter":
				m.reading = false
				return m, nil
			}
			var cmd tea.Cmd
			m.page, cmd = m.page.Update(msg)
			return m, cmd
		}
		m.notice = ""
		switch msg.String() {
		case "up", "k", "shift+tab":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j", "tab":
			if m.selected < len(m.options)-1 {
				m.selected++
			}
		case "esc", "q":
			m.chosen = SelectionOption{Key: "continue"}
			return m, tea.Quit
		case "enter":
			option := m.options[m.selected]
			switch option.Key {
			case "changelog":
				m.reading = true
				m.page = m.newPage()
				return m, nil
			case "release":
				if err := openInBrowser(m.releaseURL); err != nil {
					Log(fmt.Sprintf("Opening the release page: %v", err))
					m.notice = "Could not open a browser: " + m.releaseURL
				} else {
					m.notice = "Opened the release page in your browser."
				}
				return m, nil
			}
			m.chosen = option
			return m, tea.Quit
		}
	}
	return m, nil
}

// newPage builds the changelog viewport for the current terminal size.
func (m updatePromptModel) newPage() viewport.Model {
	width := m.contentWidth()
	// Header, rule, a blank line, and the key hints with their gap.
	height := m.height - 6
	if height < 5 {
		height = 5
	}
	page := viewport.New(width, height)
	page.SetContent(m.changelogText(width))
	return page
}

// changelogText is the terminal changelog: the same layout as the rofi rows.
func (m updatePromptModel) changelogText(width int) string {
	p := theme.Active()
	versionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(p.Accent)).Bold(true)
	sectionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(p.Muted)).Bold(true)
	leadStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(p.Foreground)).Bold(true)
	bodyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(p.Muted))
	bullet := lipgloss.NewStyle().Foreground(lipgloss.Color(p.Accent)).Render("•")

	if len(m.releases) == 0 {
		return bodyStyle.Render("No changelog was published for this release.")
	}
	var b strings.Builder
	for i, release := range m.releases {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(versionStyle.Render(icons.Label(icons.Tag, release.Version)))
		if date := formatChangelogDate(release.Date); date != "" {
			b.WriteString("  " + bodyStyle.Render(date))
		}
		b.WriteString("\n")
		for _, section := range release.Sections {
			if section.Title != "" {
				b.WriteString("  " + sectionStyle.Render(section.Title) + "\n")
			}
			for _, item := range section.Items {
				leadLeft := len([]rune(item.Lead))
				for j, line := range wrapWords(item.joined(), width-4) {
					prefix := "    "
					if j == 0 {
						prefix = "  " + bullet + " "
					}
					runes := []rune(line)
					cut := leadLeft
					if cut > len(runes) {
						cut = len(runes)
					}
					b.WriteString(prefix + leadStyle.Render(string(runes[:cut])) + bodyStyle.Render(string(runes[cut:])) + "\n")
					leadLeft -= len(runes) + 1
					if leadLeft < 0 {
						leadLeft = 0
					}
				}
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m updatePromptModel) View() string {
	width := m.contentWidth()
	var top strings.Builder
	var hints []keyHint
	if m.reading {
		top.WriteString(renderHeader("Update › Changelog", width) + "\n")
		top.WriteString(renderRule(width) + "\n\n")
		top.WriteString(m.page.View())
		hints = []keyHint{{Key: "↑/↓", Label: "scroll"}, {Key: "esc", Label: "back"}}
	} else {
		p := theme.Active()
		top.WriteString(renderHeader("Update", width) + "\n")
		top.WriteString(renderRule(width) + "\n\n")
		top.WriteString(paneTitleStyle.Render(m.title) + "\n")
		top.WriteString(paneMetaStyle.Render(m.sub) + "\n")
		if len(m.headlines) > 0 {
			top.WriteString("\n")
			bullet := lipgloss.NewStyle().Foreground(lipgloss.Color(p.Accent)).Render("•")
			for _, headline := range m.headlines {
				top.WriteString(bullet + " " + truncate(headline, width-2) + "\n")
			}
			if line := moreChangesLine(m.more); line != "" {
				top.WriteString(paneMetaStyle.Render(line) + "\n")
			}
		}
		top.WriteString("\n")
		for i, option := range m.options {
			label, meta := splitRofiLabel(option.Label)
			row := truncate(label, width-4)
			if meta != "" {
				row += " " + paneMetaStyle.Render(meta)
			}
			if i == m.selected {
				top.WriteString(selectedItemStyle.Render(row) + "\n")
			} else {
				top.WriteString(regularItemStyle.Render(row) + "\n")
			}
		}
		if m.notice != "" {
			top.WriteString("\n" + paneMetaStyle.Render(m.notice) + "\n")
		}
		hints = []keyHint{{Key: "↵", Label: "select"}, {Key: "↑/↓", Label: "move"}, {Key: "esc", Label: "continue"}}
	}
	body := top.String()
	bar := renderKeyHints(hints, width)
	gap := m.height - lipgloss.Height(body) - lipgloss.Height(bar) - 1
	if gap < 1 {
		gap = 1
	}
	return body + strings.Repeat("\n", gap) + bar
}

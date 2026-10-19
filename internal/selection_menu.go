package internal

import (
	"bytes"
	"cmp"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/thexykril/otakase/internal/icons"
	"github.com/thexykril/otakase/internal/rofitheme"
	"github.com/thexykril/otakase/internal/theme"
)

// The answers every menu can give besides one of its own rows. Callers check
// for them by key, through SelectionMeansQuit and SelectionMeansBack.
var (
	quitSelection = SelectionOption{Key: "-1", Label: "Quit"}
	backSelection = SelectionOption{Key: "-2", Label: "Back"}
)

// cancelSelection is what backing out of a menu means: quitting on the main
// list, which has nothing behind it, and going back everywhere else.
func cancelSelection(home bool) SelectionOption {
	if home {
		return quitSelection
	}
	return backSelection
}

type (
	// selectionModel is the terminal menu: a list that filters as you type and scrolls
	// to keep the highlighted row on screen.
	selectionModel struct {
		allOptions      []SelectionOption // every row the caller supplied
		filteredKeys    []SelectionOption // the rows the filter lets through, as shown
		selected        int               // index into filteredKeys
		firstVisibleRow int               // first row of filteredKeys on screen

		filter string
		// Vim keys only: "/" was pressed, so letters type into the filter
		// instead of moving the highlight.
		filterActive bool

		terminalWidth, terminalHeight int

		addNewOption  bool // pin an "Add new anime" row under the list
		isHomeMenu    bool // escape quits instead of going back
		preserveOrder bool // keep the caller's order (action menus lead with the main action)

		// layout is the chrome around the list: tabs, an actions footer, a detail
		// pane. Its zero value draws none of them, which is how every caller that
		// has not opted in keeps the interface it had.
		layout  menuLayout
		loadTab func(key string) []SelectionOption
		metaOf  func(option SelectionOption) []string
	}

	// optionsReloadedMsg hands the running menu a list rebuilt in the background.
	optionsReloadedMsg struct{ options []SelectionOption }
)

type (
	// SelectionRefreshConfig lets a menu take a newer list while it is open,
	// along with the extras only the main list uses.
	SelectionRefreshConfig struct {
		Updates      <-chan AnimeList                  // lists fetched in the background
		BuildOptions func(AnimeList) []SelectionOption // turns one into menu rows
		// Refreshed closes when the background list refresh has finished. Rofi
		// cannot change a menu it is showing, so it waits a moment for this before
		// opening rather than reopening once the fresh list lands.
		Refreshed <-chan struct{}

		// Categories, when set, draws a tab bar and lets Tab move between them
		// without leaving the list. LoadCategory supplies the entries for one, and
		// is expected to be cheap: the whole list is already in memory, so
		// switching a category is a filter rather than a fetch.
		Categories     []Tab
		ActiveCategory string
		LoadCategory   func(key string) []SelectionOption

		// Actions appear along the bottom and end the menu with that action as the
		// result when their key is pressed.
		Actions []FooterAction

		// Main marks the main list, the first thing otakase shows. Nothing is
		// behind it, so escape quits rather than going back, and in rofi it gets
		// the toolbar that stands in for the terminal's tabs and bottom bar.
		Main bool
		// Prompt names the list in rofi's prompt.
		Prompt string
	}

	// PreviewSelectionRefreshConfig is SelectionRefreshConfig for the poster grid.
	PreviewSelectionRefreshConfig struct {
		Updates      <-chan AnimeList
		BuildOptions func(AnimeList) map[string]RofiSelectPreview // keyed by row key
		Refreshed    <-chan struct{}

		// Main, Prompt, Categories and Actions are as in SelectionRefreshConfig:
		// the poster grid is the main list in rofi when posters are on.
		Main       bool
		Prompt     string
		Categories []Tab
		Actions    []FooterAction
	}
)

// Menu styles are derived from the active palette rather than fixed, so otakase
// matches the desktop theme on Omarchy. ApplyTheme rebuilds them; the values here
// are the builtin palette so the package is usable before it is called.
var (
	titleStyle          lipgloss.Style
	filterLabelStyle    lipgloss.Style
	filterTextStyle     lipgloss.Style
	selectedItemStyle   lipgloss.Style
	regularItemStyle    lipgloss.Style
	noMatchesStyle      lipgloss.Style
	newEpisodeItemStyle lipgloss.Style

	rofiNewEpisodeColor string
	rofiMetaColor       string
	rofiResumeColor     string
	rofiContinueColor   string
)

func init() {
	ApplyTheme(theme.Builtin())
}

// ApplyTheme rebuilds the menu styles from a palette.
func ApplyTheme(palette theme.Palette) {
	color := func(value string) lipgloss.Color { return lipgloss.Color(value) }

	// The cast control panel, so it follows the same palette as the menus.
	castPanelBorderStyle = lipgloss.NewStyle().Foreground(color(palette.Border()))
	castPanelTitleStyle = lipgloss.NewStyle().Foreground(color(palette.Accent)).Bold(true)
	castPanelDimStyle = lipgloss.NewStyle().Foreground(color(palette.Muted))
	castPanelBarStyle = lipgloss.NewStyle().Foreground(color(palette.Accent))
	castPanelSkipStyle = lipgloss.NewStyle().Foreground(color(palette.Magenta))
	castPanelPlayStyle = lipgloss.NewStyle().Foreground(color(palette.Green)).Bold(true)
	castPanelPauseStyle = lipgloss.NewStyle().Foreground(color(palette.Yellow)).Bold(true)

	titleStyle = lipgloss.NewStyle().Foreground(color(palette.Accent)).Bold(true)
	filterLabelStyle = lipgloss.NewStyle().Foreground(color(palette.Magenta)).Bold(true)
	filterTextStyle = lipgloss.NewStyle().Foreground(color(palette.Green))
	regularItemStyle = lipgloss.NewStyle().Foreground(color(palette.Foreground)).Padding(0, 1)
	noMatchesStyle = lipgloss.NewStyle().Foreground(color(palette.Red)).Italic(true)
	newEpisodeItemStyle = lipgloss.NewStyle().Foreground(color(palette.Green))

	// The highlighted row is the plain row inverted, with a bar down its left edge.
	selectedItemStyle = lipgloss.NewStyle().Bold(true).Padding(0, 1).
		Foreground(color(palette.SelectionForeground())).
		Background(color(palette.SelectionBackground())).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(color(palette.Accent))

	rofiNewEpisodeColor = palette.Green
	rofiMetaColor = palette.MetaText()
	rofiResumeColor = palette.ResumeText()
	rofiContinueColor = palette.ContinueText()

	applyLayoutTheme(palette)
}

func (selectionModel) Init() tea.Cmd { return nil }

// moveBy moves the highlight one row in the direction of delta, scrolling when
// it would leave the screen. The ends of the list are hard stops.
func (m *selectionModel) moveBy(delta int) {
	if next := m.selected + delta; next >= 0 && next < len(m.filteredKeys) {
		m.selected = next
	}
	switch {
	case delta < 0 && m.selected < m.firstVisibleRow:
		m.firstVisibleRow--
	case delta > 0 && m.selected >= m.firstVisibleRow+m.rowsThatFit():
		m.firstVisibleRow++
	}
}

// finish ends the menu with option as its only row, which is what the caller
// reads back as the answer.
func (m *selectionModel) finish(option SelectionOption) tea.Cmd {
	m.filteredKeys = []SelectionOption{option}
	m.selected = 0
	return tea.Quit
}

// confirm ends the menu on the highlighted row. With nothing listed there is
// nothing to confirm, and the menu stays open.
func (m *selectionModel) confirm() tea.Cmd {
	picked, ok := m.highlighted()
	if !ok {
		return nil
	}
	if picked.Key == "add_new" {
		Out("Adding a new anime...")
		// Callers look for this label, not the sentinel key.
		m.filteredKeys[m.selected] = SelectionOption{Key: "0", Label: "add_new"}
	}
	return tea.Quit
}

// refilter applies a changed filter and starts again from the top, since the
// old position means nothing in the new list.
func (m *selectionModel) refilter() {
	m.applyFilter()
	m.selected, m.firstVisibleRow = 0, 0
}

// typeIntoFilter adds a printable ASCII key to the filter. Anything longer is
// a named key ("enter", "ctrl+x") and is not text.
func (m *selectionModel) typeIntoFilter(key string) {
	if len(key) != 1 || key < " " || key > "~" {
		return
	}
	m.filter += key
	m.refilter()
}

func (m *selectionModel) eraseFromFilter() {
	if m.filter == "" {
		return
	}
	m.filter = m.filter[:len(m.filter)-1]
	m.refilter()
}

// moveForKey handles the keys that move the highlight in every key scheme,
// and reports whether key was one of them.
func (m *selectionModel) moveForKey(key string) bool {
	switch key {
	case "down", "tab", "ctrl+n":
		m.moveBy(1)
	case "up", "shift+tab", "ctrl+p":
		m.moveBy(-1)
	default:
		return false
	}
	return true
}

func (m *selectionModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.terminalWidth, m.terminalHeight = message.Width, message.Height
	case optionsReloadedMsg:
		m.replaceOptions(message.options)
	case tea.KeyMsg:
		return m, m.handleKey(message.String())
	}
	return m, nil
}

func (m *selectionModel) handleKey(key string) tea.Cmd {
	if key == "ctrl+c" {
		return m.finish(quitSelection)
	}

	// Tab and the arrows move between categories, but only where categories
	// exist. Every menu without them keeps Tab as "next item", which is what
	// it has always done here and what people's hands expect.
	//
	// Left and right are unbound in the default key scheme and are the
	// obvious way to move along a row of tabs, so they are accepted too.
	// Under vim keys they already mean up and down, and are left alone.
	switchKey := key == "tab" || key == "shift+tab"
	if !VimKeysEnabled(nil) && (key == "left" || key == "right") {
		switchKey = true
	}
	// A footer action ends the menu with that action as the result, the
	// same way choosing it from a list would.
	if action, ok := m.layout.actionForKey(key); ok {
		Log(fmt.Sprintf("Menu action: %s via %s", action.Key, action.Hint))
		return m.finish(SelectionOption{Key: action.Key, Label: action.Label})
	}

	if m.layout.hasTabs() && switchKey {
		delta := 1
		if key == "shift+tab" || key == "left" {
			delta = -1
		}
		m.layout = m.layout.cycleTab(delta)
		if m.loadTab != nil {
			loaded := m.loadTab(m.layout.activeTabKey())
			m.replaceOptions(loaded)
			Log(fmt.Sprintf("Category tabs: switched to %q, %d entries",
				m.layout.activeTabKey(), len(loaded)))
		}
		m.selected = 0
		m.firstVisibleRow = 0
		return nil
	}

	switch {
	case !VimKeysEnabled(nil):
		return m.handleTypingKey(key)
	case m.filterActive:
		return m.handleSearchKey(key)
	default:
		return m.handleNormalKey(key)
	}
}

// handleTypingKey is the default scheme: every printable key goes straight
// into the filter, so only named keys can move or confirm.
func (m *selectionModel) handleTypingKey(key string) tea.Cmd {
	switch key {
	case "esc":
		return m.finish(cancelSelection(m.isHomeMenu))
	case "enter":
		return m.confirm()
	case "backspace":
		m.eraseFromFilter()
	default:
		if !m.moveForKey(key) {
			m.typeIntoFilter(key)
		}
	}
	return nil
}

// handleNormalKey is vim's normal mode: letters are motions and nothing types
// into the filter until "/" (or "?") opens it.
func (m *selectionModel) handleNormalKey(key string) tea.Cmd {
	switch key {
	case "/", "?":
		m.filterActive = true
	case "esc":
		return m.finish(cancelSelection(m.isHomeMenu))
	case "enter":
		return m.confirm()
	case "j", "l", "right":
		m.moveBy(1)
	case "k", "h", "left":
		m.moveBy(-1)
	default:
		m.moveForKey(key)
	}
	return nil
}

// handleSearchKey is vim's search mode, after "/". As in vim, every printable
// key is part of the query, hjkl included; the arrows still move through the
// results, and escape closes the search but keeps what it matched.
func (m *selectionModel) handleSearchKey(key string) tea.Cmd {
	switch key {
	case "esc":
		m.filterActive = false
	case "enter":
		return m.confirm()
	case "backspace":
		m.eraseFromFilter()
	case "right":
		m.moveBy(1)
	case "left":
		m.moveBy(-1)
	default:
		if !m.moveForKey(key) {
			m.typeIntoFilter(key)
		}
	}
	return nil
}

// replaceOptions swaps in a new list and keeps the highlight on the same entry
// when it is still there, so a refresh landing mid-scroll does not move it.
func (m *selectionModel) replaceOptions(fresh []SelectionOption) {
	previous, _ := m.highlighted()
	fallback := m.selected

	m.allOptions = withoutAddNewSentinel(fresh)
	m.applyFilter()
	if len(m.filteredKeys) == 0 {
		m.selected, m.firstVisibleRow = 0, 0
		return
	}

	m.selected = max(findSelectionIndex(m.filteredKeys, previous.Key, previous.Label, fallback), 0)
	// Scroll the least amount that brings the highlight back on screen.
	visible := m.rowsThatFit()
	m.firstVisibleRow = min(m.firstVisibleRow, m.selected)
	if m.selected-m.firstVisibleRow >= visible {
		m.firstVisibleRow = m.selected - visible + 1
	}
	m.firstVisibleRow = max(m.firstVisibleRow, 0)
}

// View draws the menu: header, the filter when there is one, as many rows as
// fit, and the key hints along the bottom.
func (m selectionModel) View() string {
	if m.terminalWidth > 0 && m.terminalHeight > 0 &&
		(m.terminalWidth < minMenuWidth || m.terminalHeight < minMenuHeight) {
		return renderTooSmallNotice(m.terminalWidth, m.terminalHeight)
	}

	contentWidth := m.contentWidth()
	paneWidth := m.paneWidth(contentWidth)
	listWidth := contentWidth - paneWidth

	body := m.renderFilterLine() + m.renderRows(listWidth)

	if bar := renderTabBar(m.layout, contentWidth); bar != "" {
		body = bar + "\n\n" + body
	}

	if paneWidth > 0 {
		pane := renderSidePane(m.paneTitle(), m.paneMeta(), paneWidth, lipgloss.Height(body))
		if pane != "" {
			left := lipgloss.NewStyle().Width(listWidth).Render(body)
			body = lipgloss.JoinHorizontal(lipgloss.Top, left, pane)
		}
	}

	// Header, rule, body, and the key hints along the bottom edge.
	header := renderHeader(m.sectionLabel(), contentWidth)
	top := header + "\n" + renderRule(contentWidth) + "\n" + body
	hints := renderKeyHints(m.keyHints(), contentWidth)

	return m.fillTerminal(top, hints, contentWidth)
}

// renderFilterLine is the filter line, shown only once there is something in
// it or the user has asked to search. An empty "Filter:" on every screen is a
// row spent saying nothing.
func (m selectionModel) renderFilterLine() string {
	if m.filter == "" && !m.filterActive {
		return ""
	}
	text := m.filter
	if m.filterActive {
		text += "▌"
	}
	return filterLabelStyle.Render("/ ") + filterTextStyle.Render(text) + "\n\n"
}

// renderRows draws the rows currently scrolled into view.
func (m selectionModel) renderRows(width int) string {
	if len(m.filteredKeys) == 0 {
		return noMatchesStyle.Render("No matches found.") + "\n"
	}
	end := min(m.firstVisibleRow+m.rowsThatFit(), len(m.filteredKeys))
	var b strings.Builder
	for i := m.firstVisibleRow; i < end; i++ {
		style := regularItemStyle
		if i == m.selected {
			style = selectedItemStyle
		}
		b.WriteString(style.Render(rowLabel(m.filteredKeys[i], width)) + "\n")
	}
	return b.String()
}

// rowLabel is one row's text, cut to fit width.
//
// The marker goes inside the row rather than before it. Rendered outside, it
// landed to the left of the selection's border, so a highlighted new episode
// read as "[NEW]| Title" with the bar stranded in the middle.
// Cut rather than wrap. A row that wraps is two lines for one entry, which
// breaks both the count of what fits on screen and the alignment of
// everything beside it.
func rowLabel(option SelectionOption, width int) string {
	if option.HasNewEpisodes {
		return newEpisodeItemStyle.Render("[NEW]") + " " + truncate(option.Label, width-11)
	}
	icon := optionIcon(option).String()
	return icon + truncate(option.Label, width-4-len([]rune(icon)))
}

// contentWidth is how wide the menu draws.
//
// It follows the terminal so a resize relays everything, rather than being
// whatever the longest row happened to be -- which left the detail pane
// wherever the text ended and moved it every time the selection changed. It is
// capped because a full-width line of text on a very wide terminal is hard to
// read, and floored so the columns still fit on a narrow one.
func (m selectionModel) contentWidth() int {
	if m.terminalWidth <= 0 {
		// No size reported yet. Bubble Tea sends one on start, so this is the
		// first frame only; a fixed width keeps that frame sane rather than
		// letting it depend on whatever the longest row happens to be.
		return 100
	}
	width := m.terminalWidth - 2
	if width > 160 {
		width = 160
	}
	if width < minMenuWidth {
		width = minMenuWidth
	}
	return width
}

// paneWidth is how much of the frame the detail column takes, or zero when
// there is no room for one. A third, within reason: narrower and the titles in
// it wrap to nothing useful, wider and the list starts losing its own text.
func (m selectionModel) paneWidth(contentWidth int) int {
	if !m.layout.pane || contentWidth <= 0 {
		return 0
	}
	width := contentWidth / 3
	if width > 40 {
		width = 40
	}
	if width < 20 {
		return 0
	}
	return width
}

// fillTerminal pushes the hints to the bottom edge and centres the whole block,
// so the menu occupies the terminal instead of huddling in its top-left corner.
func (m selectionModel) fillTerminal(top, hints string, contentWidth int) string {
	frame := top
	if hints != "" {
		gap := 0
		if m.terminalHeight > 0 {
			gap = m.terminalHeight - lipgloss.Height(top) - lipgloss.Height(hints) - 1
		}
		if gap < 1 {
			gap = 1
		}
		frame = top + strings.Repeat("\n", gap) + hints
	}
	if m.terminalWidth <= contentWidth {
		return frame
	}
	// Indent every line equally rather than styling the block to a width:
	// the block contains a joined pane whose own padding would otherwise be
	// recomputed and shift the columns apart.
	pad := strings.Repeat(" ", (m.terminalWidth-contentWidth)/2)
	lines := strings.Split(frame, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = pad + line
		}
	}
	return strings.Join(lines, "\n")
}

// sectionLabel is what the breadcrumb says after the program's name: the
// category being looked at, or nothing when the menu has no categories.
func (m selectionModel) sectionLabel() string {
	if !m.layout.hasTabs() {
		return ""
	}
	for _, tab := range m.layout.tabs {
		if tab.Key == m.layout.activeTabKey() {
			return tab.Label
		}
	}
	return ""
}

// keyHints are the keys worth showing, in the order they are most used.
// Only keys that do something here are listed: offering one that does nothing
// is worse than offering none, because it invites a press that goes nowhere.
func (m selectionModel) keyHints() []keyHint {
	hints := []keyHint{{Key: "↵", Label: "select"}}
	if m.layout.hasTabs() {
		key := "←/→"
		if VimKeysEnabled(nil) {
			key = "tab"
		}
		hints = append(hints, keyHint{Key: key, Label: "category"})
	}
	if VimKeysEnabled(nil) {
		hints = append(hints, keyHint{Key: "j/k", Label: "move"}, keyHint{Key: "/", Label: "search"})
	} else {
		hints = append(hints, keyHint{Key: "↑/↓", Label: "move"}, keyHint{Key: "type", Label: "filter"})
	}
	for _, action := range m.layout.footer {
		if action.Hint == "" {
			continue
		}
		hints = append(hints, keyHint{Key: shortKeyLabel(action.Hint), Label: action.Label})
	}
	// Nothing is behind the main list, so escape leaves the program. Saying
	// "back" there would promise a screen that does not exist.
	if m.isHomeMenu {
		return append(hints, keyHint{Key: "esc", Label: "quit"})
	}
	return append(hints, keyHint{Key: "esc", Label: "back"})
}

// paneTitle, paneposter and paneMeta describe the highlighted entry. Each
// returns empty when nothing is highlighted or no supplier was given, so the
// pane simply renders with less in it rather than the view having to branch.
func (m selectionModel) highlighted() (SelectionOption, bool) {
	if m.selected < 0 || m.selected >= len(m.filteredKeys) {
		return SelectionOption{}, false
	}
	return m.filteredKeys[m.selected], true
}

func (m selectionModel) paneTitle() string {
	option, ok := m.highlighted()
	if !ok {
		return ""
	}
	if option.Title != "" {
		return option.Title
	}
	return option.Label
}

func (m selectionModel) paneMeta() []string {
	option, ok := m.highlighted()
	if !ok || m.metaOf == nil {
		return nil
	}
	return m.metaOf(option)
}

// rowsThatFit is how many rows of the list fit on screen, never fewer
// than one.
func (m selectionModel) rowsThatFit() int {
	// Four rows for the filter line and the spacing around the list.
	rows := m.terminalHeight - 4

	// The chrome added around the list takes rows of its own. Without counting
	// them the menu is taller than the terminal, which scrolls, and what
	// scrolls off the top is the tab bar -- so on a long list the categories
	// became invisible exactly where they are most useful.
	// The frame around the list: breadcrumb, its rule, and the key hints with
	// the blank line above them. Without counting these the menu is taller than
	// the terminal, it scrolls, and what scrolls away is the header.
	rows -= 4
	if m.layout.hasTabs() {
		rows -= 2 // the tabs, and the blank line beneath them
	}
	return max(rows, 1)
}

// filterText is what the filter matches a row against. New-episode rows carry
// their marker, so typing "[new" narrows the list to them.
func filterText(option SelectionOption) string {
	if option.HasNewEpisodes {
		return "[NEW]" + option.Label
	}
	return option.Label
}

// applyFilter rebuilds filteredKeys from allOptions and the filter, then pins
// the Back, Add new and Quit rows beneath the result.
func (m *selectionModel) applyFilter() {
	needle := strings.ToLower(m.filter)
	var rows []SelectionOption
	for _, option := range m.allOptions {
		// The add-new entry is pinned below from addNewOption, so letting it
		// through here as well would list it twice. Guarding at the point of
		// use makes the rule hold however the options were set, rather than
		// only when they came through the one path that strips it.
		if option.Key == "add_new" {
			continue
		}
		if strings.Contains(strings.ToLower(filterText(option)), needle) {
			rows = append(rows, option)
		}
	}

	// Lists of titles read best alphabetically. The home menu (recognised by
	// its ALL and CURRENT rows) and action menus have an order of their own.
	ordered := m.preserveOrder || slices.ContainsFunc(m.allOptions, func(option SelectionOption) bool {
		return option.Key == "ALL" || option.Key == "CURRENT"
	})
	if !ordered {
		slices.SortStableFunc(rows, func(a, b SelectionOption) int {
			return strings.Compare(a.Label, b.Label)
		})
	}
	// Continue-watching rows lead the list whatever its order: they are the
	// shows most likely wanted.
	sort.SliceStable(rows, func(i, j int) bool {
		_, iRow := resumeRowAnilistID(rows[i].Key)
		_, jRow := resumeRowAnilistID(rows[j].Key)
		return iRow && !jRow
	})

	m.filteredKeys = append(rows, m.pinnedRows()...)
}

// pinnedRows are the rows every menu ends with, minus any the filter rules out.
//
// They obey the filter like any other row so that Enter after typing "quit"
// lands on Quit rather than on a Back still pinned above it. For the same
// reason a filter that matches Back lifts it above Add new.
func (m *selectionModel) pinnedRows() []SelectionOption {
	needle := strings.ToLower(strings.TrimSpace(m.filter))
	matches := func(label string) bool {
		return strings.Contains(strings.ToLower(label), needle)
	}
	addNew := SelectionOption{Key: "add_new", Label: "Add new anime"}
	showBack := !m.isHomeMenu && matches(backSelection.Label)

	var rows []SelectionOption
	if showBack && needle != "" {
		rows = append(rows, backSelection)
	}
	if m.addNewOption && matches(addNew.Label) {
		rows = append(rows, addNew)
	}
	if showBack && needle == "" {
		rows = append(rows, backSelection)
	}
	if matches(quitSelection.Label) {
		rows = append(rows, quitSelection)
	}
	return rows
}

// SelectionMeansQuit reports whether a menu answer is Quit, by key or label.
func SelectionMeansQuit(option SelectionOption) bool {
	return option.Key == quitSelection.Key || strings.EqualFold(strings.TrimSpace(option.Label), "quit")
}

// SelectionMeansBack reports whether a menu answer is Back, by key or by any of
// the labels menus have used for it.
func SelectionMeansBack(option SelectionOption) bool {
	if option.Key == backSelection.Key || strings.EqualFold(option.Key, "back") {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(option.Label)) {
	case "back", "back to menu", "back to list":
		return true
	}
	return false
}

// NormalizeSelectionKey maps any form of Quit or Back onto the one option
// callers compare against.
func NormalizeSelectionKey(option SelectionOption) SelectionOption {
	switch {
	case SelectionMeansQuit(option):
		return quitSelection
	case SelectionMeansBack(option):
		return backSelection
	}
	return option
}

// findSelectionIndex finds where the previously highlighted entry went after a
// list changed: by key, then by label, and failing both the old position,
// pulled back inside the list. It is -1 only for an empty list.
func findSelectionIndex(options []SelectionOption, previousKey, previousLabel string, fallbackIndex int) int {
	sameEntry := []func(SelectionOption) bool{
		func(o SelectionOption) bool { return previousKey != "" && o.Key == previousKey },
		func(o SelectionOption) bool { return previousLabel != "" && o.Label == previousLabel },
	}
	for _, same := range sameEntry {
		if i := slices.IndexFunc(options, same); i >= 0 {
			return i
		}
	}
	return min(max(fallbackIndex, 0), len(options)-1)
}

func previewOptionsToSortedSelection(previews map[string]RofiSelectPreview) []SelectionOption {
	type ranked struct {
		option SelectionOption
		rank   int
	}

	entries := make([]ranked, 0, len(previews))
	for key, preview := range previews {
		entries = append(entries, ranked{
			option: SelectionOption{
				Label:          preview.Title,
				Title:          preview.Title,
				Key:            key,
				Thumbnail:      preview.CoverImage,
				HasNewEpisodes: preview.HasNewEpisodes,
			},
			rank: preview.Rank,
		})
	}

	// Honour the order the list was built in -- most recently watched first --
	// rather than re-sorting alphabetically, which buried whatever you were part
	// way through and disagreed with the text menu.
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].rank != entries[j].rank {
			return entries[i].rank < entries[j].rank
		}
		return entries[i].option.Label < entries[j].option.Label
	})

	sorted := make([]SelectionOption, len(entries))
	for i, entry := range entries {
		sorted[i] = entry.option
	}
	return sorted
}

// rofiRefreshWait is how long a rofi menu holds back for the background list
// refresh. The menus open from the cached list, and rofi cannot change a menu
// already on screen: reloading it meant killing rofi and starting it again,
// which flashed the menu away and back at launch and dropped whatever had been
// typed. Waiting this long catches the usual refresh; one that lands later is
// shown the next time a menu opens.
var rofiRefreshWait = 1500 * time.Millisecond

// awaitRofiListRefresh waits briefly for the background refresh and returns the
// newest list it published, if any arrived since the menu was last built.
func awaitRofiListRefresh(updates <-chan AnimeList, refreshed <-chan struct{}) (AnimeList, bool) {
	if refreshed != nil {
		select {
		case <-refreshed:
		case <-time.After(rofiRefreshWait):
			Log("Anime list refresh still running; opening the menu with the cached list")
		}
	}
	var latest AnimeList
	got := false
	for {
		select {
		case list, ok := <-updates:
			if !ok {
				return latest, got
			}
			latest, got = list, true
		default:
			return latest, got
		}
	}
}

// DynamicSelectPreview shows the poster grid in rofi.
func DynamicSelectPreview(previews map[string]RofiSelectPreview, addNew bool) (SelectionOption, error) {
	return DynamicSelectPreviewWithRefresh(previews, addNew, nil)
}

func DynamicSelectPreviewWithRefresh(previews map[string]RofiSelectPreview, addNew bool, refreshConfig *PreviewSelectionRefreshConfig) (SelectionOption, error) {
	// A menu on screen answers "is it still working" by itself.
	defer suspendBusy()()

	if refreshConfig != nil {
		if list, ok := awaitRofiListRefresh(refreshConfig.Updates, refreshConfig.Refreshed); ok {
			previews = refreshConfig.BuildOptions(list)
		}
	}

	go preDownloadImages(previews, 14)

	var rofiInput strings.Builder
	selectionOptions := previewOptionsToSortedSelection(previews)

	rows := writePreviewRows(&rofiInput, selectionOptions, func(opt SelectionOption) (string, bool) {
		cachePath, err := downloadToCache(previews[opt.Key].CoverImage)
		if err != nil {
			Log(fmt.Sprintf("Error caching image: %v", err))
			return "", false
		}
		return cachePath, true
	})

	if addNew {
		rofiInput.WriteString("Add new anime\n")
		rows = append(rows, SelectionOption{Key: "add_new", Label: "Add new anime"})
	}
	main := refreshConfig != nil && refreshConfig.Main
	// The main grid has no Back or Quit tiles: nothing is behind it, and
	// its toolbar ends in Quit.
	if !main {
		rofiInput.WriteString("Back\n")
		rofiInput.WriteString("Quit\n")
		rows = append(rows, backSelection, quitSelection)
	}

	// A menu on screen is proof otakase started; anything still showing is stale.
	EndStartupProgress()

	configPath := filepath.Join(GetStoragePath(), "selectanimepreview.rasi")
	// NOTE: Need `-markup-rows` to enable pango
	// -format i returns the index of the chosen row. The label cannot be used:
	// the grid clips it to the column width, so what comes back for a long
	// title is not the string the option carries.
	args := []string{"-dmenu", "-theme", configPath, "-show-icons", "-markup-rows", "-p", "Select Anime", "-i", "-no-custom", "-format", "i"}
	args = append(args, rofiVersionThemeArgs()...)
	var toolbar rofiToolbar
	if main {
		toolbar = mainRofiToolbar(GetGlobalConfig(), refreshConfig.Categories, refreshConfig.Actions)
		args = append(args, toolbar.args([]string{"inputbar", "box-toolbar", "listview"})...)
		args = append(args, rofiPlaceholder(refreshConfig.Prompt)...)
	}
	cmd := exec.Command("rofi", args...)
	cmd.Stdin = strings.NewReader(rofiInput.String())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if pressed, ok := toolbar.pressed(err); ok {
			return pressed, nil
		}
		Log(fmt.Sprintf("Rofi stderr: %s", stderr.String()))
		Log(fmt.Sprintf("Rofi stdout: %s", stdout.String()))
		return backSelection, nil
	}
	return parsePreviewSelectionIndex(stdout.String(), rows)
}

// preDownloadImages fetches up to count covers ahead of the grid, so fewer of
// them are still downloading when rofi is about to draw.
func preDownloadImages(previews map[string]RofiSelectPreview, count int) {
	for _, preview := range previews {
		if count <= 0 {
			return
		}
		count--
		_, _ = downloadToCache(preview.CoverImage)
	}
}

// writePreviewRows renders the poster rows into the rofi input and returns the
// table those rows index into.
//
// The table has to be built here rather than reused from the option list: a row
// whose cover fails to download is skipped, and indexing into the unfiltered
// list would then resolve every row after the gap to its neighbour.
//
// cache returns the cached cover path for an option, and false to skip it.
func writePreviewRows(w *strings.Builder, options []SelectionOption, cache func(SelectionOption) (string, bool)) []SelectionOption {
	rows := make([]SelectionOption, 0, len(options))
	for _, opt := range options {
		cachePath, ok := cache(opt)
		if !ok {
			// A continue row comes from the watch history, which has no
			// cover with local tracking; it is still the show to pick up.
			if _, isRow := resumeRowAnilistID(opt.Key); !isRow {
				continue
			}
		}
		// Every row goes through the markup builder, not just the ones with new
		// episodes: it is what escapes pango and dims the counts, and skipping
		// it left ordinary rows unescaped.
		label := continueRowMarkup(opt, GridRowMarkup(opt.Label, rofitheme.GridLabelCapacity))
		if opt.HasNewEpisodes {
			label = fmt.Sprintf("<span foreground=\"%s\">[NEW]</span> %s", rofiNewEpisodeColor, label)
		}
		if cachePath == "" {
			w.WriteString(label + "\n")
		} else {
			w.WriteString(fmt.Sprintf("%s\x00icon\x1f%s\n", label, cachePath))
		}
		rows = append(rows, opt)
	}
	return rows
}

// parsePreviewSelectionIndex resolves rofi's chosen row index against the table
// written alongside the menu.
//
// Anything that is not a usable index -- no output because the menu was
// dismissed, or a row outside the table -- means "go back" rather than an error:
// closing the picker is a normal thing to do, and it must not end the session.
func parsePreviewSelectionIndex(rawSelection string, rows []SelectionOption) (SelectionOption, error) {
	index, err := strconv.Atoi(strings.TrimSpace(rawSelection))
	if err != nil || index < 0 || index >= len(rows) {
		return backSelection, nil
	}
	return rows[index], nil
}

// coverCachePath is where a cover lives once fetched. It is separated from the
// fetching so a caller can ask whether a poster is already on disk without
// reaching for the network -- which the drawing path must never do.
func coverCachePath(imageURL string) (string, error) {
	if strings.TrimSpace(imageURL) == "" {
		return "", fmt.Errorf("image URL is empty")
	}
	cacheDir := os.ExpandEnv("${HOME}/.cache/" + AppName + "/images")
	return filepath.Join(cacheDir, fmt.Sprintf("%x.jpg", md5.Sum([]byte(imageURL)))), nil
}

// downloadToCache returns the local path of a cover, fetching it first if it
// is not on disk yet.
func downloadToCache(coverURL string) (string, error) {
	path, err := coverCachePath(coverURL)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create cover cache: %w", err)
	}
	// An empty file is what a failed download leaves behind; fetch it again.
	info, statErr := os.Stat(path)
	switch {
	case statErr != nil:
	case info.Size() > 0:
		return path, nil
	default:
		_ = os.Remove(path)
	}
	if err := fetchCover(coverURL, path); err != nil {
		return "", err
	}
	return path, nil
}

// fetchCover downloads a cover to path. On failure the partial file is
// removed, so it is never mistaken for a cached cover.
func fetchCover(coverURL, path string) error {
	response, err := sharedHTTPClient.Get(coverURL)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download cover: HTTP %d", response.StatusCode)
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	_, err = io.Copy(file, response.Body)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
	}
	return err
}

// RofiSelectWithMessage shows a rofi menu with message as a banner above it
// (release notes, a diagnosis), rather than a separate notification.
func RofiSelectWithMessage(choices []SelectionOption, isHomeMenu bool, prompt, message string) (SelectionOption, error) {
	return rofiSelectInternal(choices, isHomeMenu, nil, prompt, message)
}

func rofiSelectInternal(choices []SelectionOption, isHomeMenu bool, refreshConfig *SelectionRefreshConfig, prompt, message string) (SelectionOption, error) {
	// A menu on screen answers "is it still working" by itself.
	defer suspendBusy()()

	if refreshConfig != nil {
		if list, ok := awaitRofiListRefresh(refreshConfig.Updates, refreshConfig.Refreshed); ok {
			choices = refreshConfig.BuildOptions(list)
		}
	}
	prompt = cmp.Or(strings.TrimSpace(prompt), "Select")

	// A menu on screen is proof otakase started; anything still showing is stale.
	EndStartupProgress()

	optionsString := buildRofiOptionsString(choices, isHomeMenu)
	configPath := filepath.Join(GetStoragePath(), "selectanime.rasi")
	args := []string{"-dmenu", "-theme", configPath, "-i", "-markup", "-markup-rows", "-p", prompt}
	args = append(args, rofiVersionThemeArgs()...)
	var toolbar rofiToolbar
	if refreshConfig != nil && refreshConfig.Main {
		toolbar = mainRofiToolbar(GetGlobalConfig(), refreshConfig.Categories, refreshConfig.Actions)
		args = append(args, toolbar.args([]string{"inputbar", "box-toolbar", "message", "listview"})...)
		args = append(args, rofiPlaceholder(refreshConfig.Prompt)...)
	}
	if msg := strings.TrimSpace(message); msg != "" {
		args = append(args, "-mesg", msg)
	}
	cmd := exec.Command("rofi", args...)
	cmd.Stdin = strings.NewReader(optionsString)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if pressed, ok := toolbar.pressed(err); ok {
		return pressed, nil
	}
	if err != nil {
		Log(fmt.Sprintf("Rofi stderr: %s", stderr.String()))
	}
	return parseRofiSelection(err, stdout.String(), choices, isHomeMenu)
}

// DynamicSelect shows a menu of choices sorted by label, in rofi or the
// terminal depending on the config.
func DynamicSelect(choices []SelectionOption) (SelectionOption, error) {
	return dynamicSelectInternal(choices, nil, false)
}

// DynamicSelectPreserveOrder is DynamicSelect without the sorting, for action
// menus whose first entry is the main action.
func DynamicSelectPreserveOrder(choices []SelectionOption) (SelectionOption, error) {
	return dynamicSelectInternal(choices, nil, true)
}

// DynamicSelectWithRefresh is DynamicSelect for a list that may be replaced
// while it is open.
func DynamicSelectWithRefresh(choices []SelectionOption, refreshConfig *SelectionRefreshConfig) (SelectionOption, error) {
	return dynamicSelectInternal(choices, refreshConfig, false)
}

// The menus the flows ask through. Tests replace them with scripted answers.
var (
	promptSelect        = DynamicSelect
	promptSelectOrdered = DynamicSelectPreserveOrder
)

// ErrCastNonInteractive is what a menu returns rather than draw itself in a
// window whose viewer is not at the keyboard.
//
// It exists as a last resort. Every prompt reachable from a spawned cast takes a
// declared default before it gets here, but that list was built by reading call
// sites, and reading call sites is how a function with no callers came to be
// listed as a live consent prompt. A prompt added later on any path should
// decline and say so, not block a season on a window nobody is watching.
var ErrCastNonInteractive = errors.New("selection: interactive menu unavailable in a spawned cast window")

// castWindowNonInteractive reports whether this process is a cast window opened
// for a viewer elsewhere, where a menu would have no one to answer it.
func castWindowNonInteractive() bool {
	config := GetGlobalConfig()
	return config != nil && config.CastNonInteractive
}

func dynamicSelectInternal(choices []SelectionOption, refreshConfig *SelectionRefreshConfig, keepOrder bool) (SelectionOption, error) {
	// A menu on screen answers "is it still working" by itself.
	defer suspendBusy()()

	// The main list has nothing behind it: escape quits rather than going back.
	home := refreshConfig != nil && refreshConfig.Main

	// Before the rofi branch, because a spawned cast forces RofiSelection off
	// and would otherwise fall through to a Bubble Tea program competing with
	// the cast reader for stdin.
	if castWindowNonInteractive() {
		Log("cast: skipped an interactive menu with no viewer at the keyboard")
		return SelectionOption{}, ErrCastNonInteractive
	}

	if cfg := GetGlobalConfig(); cfg != nil && cfg.RofiSelection {
		return rofiSelectInternal(choices, home, refreshConfig, "Select", "")
	}

	// The terminal launch's spinner ends at its first menu, as the rofi
	// notification does.
	EndStartupProgress()

	// The add-new sentinel comes out of the list and back as a flag, so it is
	// pinned below the rows instead of sorted in among them.
	menu := &selectionModel{
		allOptions: withoutAddNewSentinel(choices),
		addNewOption: slices.ContainsFunc(choices, func(o SelectionOption) bool {
			return o.Key == "add_new"
		}),
		isHomeMenu:    home,
		preserveOrder: keepOrder,
	}
	attachCategoryTabs(menu, refreshConfig)
	attachDetailPane(menu)
	menu.applyFilter()

	program := tea.NewProgram(menu)
	stop := make(chan struct{})
	go forwardRefreshes(program, refreshConfig, slices.Clone(choices), stop)
	finished, runErr := program.Run()
	close(stop)

	// Bubble Tea can exit with the cursor hidden and line wrapping off; turn
	// both back on before anything else prints.
	fmt.Print("\033[?25h\033[?7h")
	if runErr != nil {
		LeaveAppScreen()
		return SelectionOption{}, runErr
	}

	final, isMenu := finished.(*selectionModel)
	if !isMenu {
		return SelectionOption{}, fmt.Errorf("selection menu ended as %T", finished)
	}
	if picked, ok := final.highlighted(); ok {
		return NormalizeSelectionKey(picked), nil
	}
	// Nothing was left to pick, which only an empty list allows. That is the
	// user backing out.
	return cancelSelection(final.isHomeMenu), nil
}

// forwardRefreshes passes each list the background refresh publishes to the
// open menu, until stop closes or the updates end. A list that builds the same
// rows as the last one is dropped rather than redrawn.
func forwardRefreshes(program *tea.Program, refresh *SelectionRefreshConfig, last []SelectionOption, stop <-chan struct{}) {
	if refresh == nil || refresh.Updates == nil {
		return
	}
	for {
		select {
		case <-stop:
			return
		case list, ok := <-refresh.Updates:
			if !ok {
				return
			}
			rebuilt := refresh.BuildOptions(list)
			if reflect.DeepEqual(last, rebuilt) {
				continue
			}
			last = rebuilt
			program.Send(optionsReloadedMsg{options: rebuilt})
		}
	}
}

// rofiVersionThemeArgs puts the running version at the right end of the
// search bar, in the theme's dimmed colour. Naming the input bar's children
// replaces the theme's list, so the app icon the theme puts in front of the
// entry is named here again, when it was written beside the themes.
func rofiVersionThemeArgs() []string {
	version := strings.NewReplacer(`"`, "", `\`, "").Replace(DisplayVersion())
	children := "entry, textbox-version"
	icon := filepath.Join(GetStoragePath(), rofitheme.IconFile)
	if info, err := os.Stat(icon); err == nil && !info.IsDir() && !strings.ContainsAny(icon, `"\`) {
		children = "icon-app, " + children
	}
	return []string{"-theme-str", fmt.Sprintf(
		`inputbar { children: [ %s ]; } `+
			`textbox-version { expand: false; content: "%s"; background-color: transparent; text-color: @muted; vertical-align: 0.5; markup: false; }`,
		children, version)}
}

// optionIcon is the icon a row is drawn with: its own, or the one every menu's
// Back, Quit and Add new rows share.
func optionIcon(opt SelectionOption) icons.Icon {
	if opt.Icon != 0 {
		return opt.Icon
	}
	switch opt.Key {
	case "-2":
		return icons.Back
	case "-1":
		return icons.Quit
	case "add_new":
		return icons.Add
	}
	return 0
}

// buildRofiOptionsString is the rofi input for a text menu: one row per line,
// with Back and Quit appended except on the main list.
func buildRofiOptionsString(choices []SelectionOption, home bool) string {
	rows := make([]string, 0, len(choices)+2)
	for _, opt := range choices {
		rows = append(rows, rofiOptionRow(opt))
	}

	// The main list has neither: nothing is behind it, and its toolbar ends
	// in Quit.
	if !home {
		rows = append(rows, icons.Label(icons.Back, "Back"), icons.Label(icons.Quit, "Quit"))
	}
	return strings.Join(rows, "\n")
}

// rofiOptionRow is one row of a text menu in pango markup. A new episode's
// marker takes the place of the icon.
func rofiOptionRow(opt SelectionOption) string {
	if opt.HasNewEpisodes {
		return fmt.Sprintf("<span foreground=\"%s\">[NEW]</span> %s", rofiNewEpisodeColor, rofiRowMarkup(opt.Label))
	}
	return continueRowMarkup(opt, optionIcon(opt).String()+rofiRowMarkup(opt.Label))
}

// parseRofiSelection maps what rofi printed back to the option it came from.
// Rofi gives back the row's text, so this undoes the markup, the [NEW] marker
// and the icon that buildRofiOptionsString added.
func parseRofiSelection(err error, rawSelection string, choices []SelectionOption, home bool) (SelectionOption, error) {
	if err != nil {
		// Exit status 1 is rofi being dismissed, which is backing out.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return cancelSelection(home), nil
		}
		return SelectionOption{}, fmt.Errorf("failed to run Rofi: %w", err)
	}

	picked := cleanRofiSelection(rawSelection)
	typed := SelectionOption{Label: picked}
	switch {
	case picked == "":
		return cancelSelection(home), nil
	case SelectionMeansBack(typed):
		return backSelection, nil
	case SelectionMeansQuit(typed):
		return quitSelection, nil
	}

	// Compare case-insensitively and with runs of spaces collapsed: an emoji
	// in a label can come back with its spacing changed.
	squashed := strings.Join(strings.Fields(picked), " ")
	for _, choice := range choices {
		if strings.EqualFold(choice.Label, picked) || strings.EqualFold(strings.Join(strings.Fields(choice.Label), " "), squashed) {
			return NormalizeSelectionKey(choice), nil
		}
	}
	return SelectionOption{}, errors.New("selected option not found in original list")
}

// cleanRofiSelection strips what rofi echoes back around a row's label:
// terminal colour codes and pango markup some themes leave in, the [NEW]
// marker and the row's icon.
func cleanRofiSelection(raw string) string {
	text := ansiStrip.ReplaceAllString(strings.TrimSpace(raw), "")
	text = unescapePango(strings.TrimSpace(pangoStrip.ReplaceAllString(text, "")))
	text = strings.TrimPrefix(text, "[NEW] ")
	return strings.TrimSpace(icons.Strip(text))
}

// withoutAddNewSentinel removes the "add new" entry from a list of options.
//
// applyFilter appends it from the addNewOption flag, so any list that still
// carries it produces two. Stripping it here rather than at the one original
// call site covers every later replacement as well: a background refresh and a
// category switch both rebuild the options from a source that includes it, and
// both showed the entry twice before this.
func withoutAddNewSentinel(options []SelectionOption) []SelectionOption {
	cleaned := make([]SelectionOption, 0, len(options))
	for _, option := range options {
		if option.Key == "add_new" {
			continue
		}
		cleaned = append(cleaned, option)
	}
	return cleaned
}

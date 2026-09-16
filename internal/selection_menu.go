package internal

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/thexykril/otakase/internal/rofitheme"
	"github.com/thexykril/otakase/internal/theme"
)

// Model represents the application state for the selection prompt
type Model struct {
	filter         string
	filterActive   bool // when VimKeys is on: true after "/" enters search mode
	filteredKeys   []SelectionOption
	allOptions     []SelectionOption
	selected       int
	terminalWidth  int
	terminalHeight int
	scrollOffset   int
	addNewOption   bool
	isHomeMenu     bool // If true, ESC quits; if false, ESC goes back
	preserveOrder  bool // skip alphabetical sort (action menus with a fixed priority)

	// layout is the chrome around the list: tabs, an actions footer, a detail
	// pane. Its zero value draws none of them, which is how every caller that
	// has not opted in keeps the interface it had.
	layout  menuLayout
	loadTab func(key string) []SelectionOption
	metaOf  func(option SelectionOption) []string
}

type optionsRefreshedMsg struct {
	options []SelectionOption
}

type SelectionRefreshConfig struct {
	Updates      <-chan AnimeList
	BuildOptions func(AnimeList) []SelectionOption

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
}

type PreviewSelectionRefreshConfig struct {
	Updates      <-chan AnimeList
	BuildOptions func(AnimeList) map[string]RofiSelectPreview
}

// Menu styles are derived from the active palette rather than fixed, so Curd
// matches the desktop theme on Omarchy. ApplyTheme rebuilds them; the values here
// are the builtin palette so the package is usable before it is called.
var (
	titleStyle          lipgloss.Style
	filterLabelStyle    lipgloss.Style
	filterTextStyle     lipgloss.Style
	selectedItemStyle   lipgloss.Style
	regularItemStyle    lipgloss.Style
	noMatchesStyle      lipgloss.Style
	quitHintStyle       lipgloss.Style
	newEpisodeItemStyle lipgloss.Style

	rofiNewEpisodeColor string
	rofiMetaColor       string
)

func init() {
	ApplyTheme(theme.Builtin())
}

// ApplyTheme rebuilds the menu styles from a palette.
func ApplyTheme(palette theme.Palette) {
	color := func(value string) lipgloss.Color { return lipgloss.Color(value) }

	titleStyle = lipgloss.NewStyle().
		Foreground(color(palette.Accent)).
		Bold(true)

	filterLabelStyle = lipgloss.NewStyle().
		Foreground(color(palette.Magenta)).
		Bold(true)

	filterTextStyle = lipgloss.NewStyle().
		Foreground(color(palette.Green))

	selectionBackground := palette.SelectionBackground()
	selectedItemStyle = lipgloss.NewStyle().
		Foreground(color(palette.SelectionForeground())).
		Background(color(selectionBackground)).
		Bold(true).
		Padding(0, 1).
		Border(lipgloss.NormalBorder(), false, false, false, true). // Left border only
		BorderForeground(color(palette.Accent))

	regularItemStyle = lipgloss.NewStyle().
		Foreground(color(palette.Foreground)).
		Padding(0, 1)

	noMatchesStyle = lipgloss.NewStyle().
		Foreground(color(palette.Red)).
		Italic(true)

	quitHintStyle = lipgloss.NewStyle().
		Foreground(color(palette.Muted))

	newEpisodeItemStyle = lipgloss.NewStyle().
		Foreground(color(palette.Green))

	rofiNewEpisodeColor = palette.Green
	rofiMetaColor = palette.Muted

	applyLayoutTheme(palette)
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return nil
}

func (m *Model) moveSelectionDown() {
	if m.selected < len(m.filteredKeys)-1 {
		m.selected++
	}
	if m.selected >= m.scrollOffset+m.visibleItemsCount() {
		m.scrollOffset++
	}
}

func (m *Model) moveSelectionUp() {
	if m.selected > 0 {
		m.selected--
	}
	if m.selected < m.scrollOffset {
		m.scrollOffset--
	}
}

func (m *Model) confirmSelection() tea.Cmd {
	if len(m.filteredKeys) == 0 {
		return nil
	}
	if m.filteredKeys[m.selected].Key == "add_new" {
		CurdOut("Adding a new anime...")
		m.filteredKeys[m.selected] = SelectionOption{Label: "add_new", Key: "0"}
	}
	return tea.Quit
}

func (m *Model) exitMenu() tea.Cmd {
	if m.isHomeMenu {
		m.filteredKeys = []SelectionOption{{Key: "-1", Label: "Quit"}}
	} else {
		m.filteredKeys = []SelectionOption{{Key: "-2", Label: "Back"}}
	}
	m.selected = 0
	return tea.Quit
}

// Update handles user input and updates the model
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle terminal resize messages
	if wsm, ok := msg.(tea.WindowSizeMsg); ok {
		m.terminalWidth = wsm.Width
		m.terminalHeight = wsm.Height
	}

	updateFilter := false
	vimKeys := VimKeysEnabled(nil)

	switch msg := msg.(type) {
	case optionsRefreshedMsg:
		m.replaceOptions(msg.options)
		return m, nil
	case tea.KeyMsg:
		key := msg.String()

		switch key {
		case "ctrl+c":
			m.filteredKeys = []SelectionOption{{Key: "-1", Label: "Quit"}}
			m.selected = 0
			return m, tea.Quit
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
			m.filteredKeys = []SelectionOption{{Key: action.Key, Label: action.Label}}
			m.selected = 0
			Log(fmt.Sprintf("Menu action: %s via %s", action.Key, action.Hint))
			return m, tea.Quit
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
			m.scrollOffset = 0
			return m, nil
		}

		// --- Vim-enabled selection: normal mode vs search mode ---
		// Normal: hjkl/arrows move. Search (after /): like vim's / — every
		// printable key including hjkl is part of the query; arrows/tab still move.
		if vimKeys {
			if m.filterActive {
				switch key {
				case "esc":
					// Leave search mode but keep the current filter applied.
					m.filterActive = false
					return m, nil
				case "enter":
					return m, m.confirmSelection()
				case "backspace":
					if len(m.filter) > 0 {
						m.filter = m.filter[:len(m.filter)-1]
						updateFilter = true
					}
				case "down", "tab", "ctrl+n":
					m.moveSelectionDown()
				case "up", "shift+tab", "ctrl+p":
					m.moveSelectionUp()
				// left/right: optional result navigation without stealing hjkl
				case "left":
					m.moveSelectionUp()
				case "right":
					m.moveSelectionDown()
				default:
					// hjkl and all other printables go into the search query.
					if len(key) == 1 && key >= " " && key <= "~" {
						m.filter += key
						updateFilter = true
					}
				}
				break
			}

			// Normal mode: motions only; "/" or "?" starts search.
			switch key {
			case "/", "?":
				m.filterActive = true
				return m, nil
			case "esc":
				return m, m.exitMenu()
			case "enter":
				return m, m.confirmSelection()
			case "down", "j", "tab", "ctrl+n", "l", "right":
				m.moveSelectionDown()
			case "up", "k", "shift+tab", "ctrl+p", "h", "left":
				m.moveSelectionUp()
			case "backspace":
				// Ignore — filter is only edited in search mode.
			default:
				// Do not type into filter in normal mode.
			}
			break
		}

		// --- Legacy behavior: every printable key filters immediately ---
		switch key {
		case "esc":
			return m, m.exitMenu()
		case "backspace":
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
				updateFilter = true
			}
		case "down", "tab", "ctrl+n":
			m.moveSelectionDown()
		case "up", "shift+tab", "ctrl+p":
			m.moveSelectionUp()
		case "enter":
			return m, m.confirmSelection()
		default:
			if len(key) == 1 && key >= " " && key <= "~" {
				m.filter += key
				updateFilter = true
			}
		}
	}

	if updateFilter {
		m.filterOptions()
		m.selected = 0     // Reset selection to the first item after filtering
		m.scrollOffset = 0 // Reset scrolling
	}

	return m, nil
}

func (m *Model) replaceOptions(options []SelectionOption) {
	previousIndex := m.selected
	previousKey := ""
	previousLabel := ""

	if m.selected >= 0 && m.selected < len(m.filteredKeys) {
		previousKey = m.filteredKeys[m.selected].Key
		previousLabel = m.filteredKeys[m.selected].Label
	}

	m.allOptions = withoutAddNewSentinel(options)
	m.filterOptions()

	if len(m.filteredKeys) == 0 {
		m.selected = 0
		m.scrollOffset = 0
		return
	}

	m.selected = findSelectionIndex(m.filteredKeys, previousKey, previousLabel, previousIndex)
	if m.selected < 0 {
		m.selected = 0
	}

	if m.selected < m.scrollOffset {
		m.scrollOffset = m.selected
	}

	visibleCount := m.visibleItemsCount()
	if visibleCount <= 0 {
		m.scrollOffset = 0
		return
	}

	if m.selected >= m.scrollOffset+visibleCount {
		m.scrollOffset = m.selected - visibleCount + 1
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

// View renders the UI and only shows as many options as fit in the terminal
func (m Model) View() string {
	if m.terminalWidth > 0 && m.terminalHeight > 0 &&
		(m.terminalWidth < minMenuWidth || m.terminalHeight < minMenuHeight) {
		return renderTooSmallNotice(m.terminalWidth, m.terminalHeight)
	}

	contentWidth := m.contentWidth()
	paneWidth := m.paneWidth(contentWidth)
	listWidth := contentWidth - paneWidth

	var b strings.Builder

	// A filter line, shown only once there is something in it or the user has
	// asked to search. An empty "Filter:" on every screen is a row spent
	// saying nothing.
	if m.filter != "" || m.filterActive {
		caret := ""
		if m.filterActive {
			caret = "▌"
		}
		b.WriteString(filterLabelStyle.Render("/ ") +
			filterTextStyle.Render(m.filter+caret) + "\n\n")
	}

	if len(m.filteredKeys) == 0 {
		b.WriteString(noMatchesStyle.Render("No matches found.") + "\n")
	} else {
		visibleItems := m.visibleItemsCount()
		start := m.scrollOffset
		end := start + visibleItems
		if end > len(m.filteredKeys) {
			end = len(m.filteredKeys)
		}

		// Render the options within the visible range
		for i := start; i < end; i++ {
			// The marker goes inside the row rather than before it. Rendered
			// outside, it landed to the left of the selection's border, so a
			// highlighted new episode read as "[NEW]| Title" with the bar
			// stranded in the middle.
			// Cut rather than wrap. A row that wraps is two lines for one
			// entry, which breaks both the count of what fits on screen and
			// the alignment of everything beside it.
			label := truncate(m.filteredKeys[i].Label, listWidth-4)
			if m.filteredKeys[i].HasNewEpisodes {
				label = newEpisodeItemStyle.Render("[NEW]") + " " +
					truncate(m.filteredKeys[i].Label, listWidth-11)
			}
			if i == m.selected {
				b.WriteString(selectedItemStyle.Render(label) + "\n")
			} else {
				b.WriteString(regularItemStyle.Render(label) + "\n")
			}
		}
	}

	body := b.String()

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
	header := renderBreadcrumb(m.sectionLabel())
	top := header + "\n" + renderRule(contentWidth) + "\n" + body
	hints := renderKeyHints(m.keyHints(), contentWidth)

	return m.fillTerminal(top, hints, contentWidth)
}

// contentWidth is how wide the menu draws.
//
// It follows the terminal so a resize relays everything, rather than being
// whatever the longest row happened to be -- which left the detail pane
// wherever the text ended and moved it every time the selection changed. It is
// capped because a full-width line of text on a very wide terminal is hard to
// read, and floored so the columns still fit on a narrow one.
func (m Model) contentWidth() int {
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
func (m Model) paneWidth(contentWidth int) int {
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
func (m Model) fillTerminal(top, hints string, contentWidth int) string {
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
func (m Model) sectionLabel() string {
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
func (m Model) keyHints() []keyHint {
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
	if m.isHomeMenu {
		return append(hints, keyHint{Key: "ctrl+c", Label: "quit"})
	}
	// With the menu skipped there is nothing behind this list, so escape leaves
	// the program. Saying "back" there would promise a screen that does not
	// exist.
	if config := GetGlobalConfig(); config != nil && config.CurrentCategory {
		return append(hints, keyHint{Key: "esc", Label: "quit"})
	}
	return append(hints, keyHint{Key: "esc", Label: "back"})
}

// paneTitle, paneposter and paneMeta describe the highlighted entry. Each
// returns empty when nothing is highlighted or no supplier was given, so the
// pane simply renders with less in it rather than the view having to branch.
func (m Model) highlighted() (SelectionOption, bool) {
	if m.selected < 0 || m.selected >= len(m.filteredKeys) {
		return SelectionOption{}, false
	}
	return m.filteredKeys[m.selected], true
}

func (m Model) paneTitle() string {
	option, ok := m.highlighted()
	if !ok {
		return ""
	}
	if option.Title != "" {
		return option.Title
	}
	return option.Label
}

func (m Model) paneMeta() []string {
	option, ok := m.highlighted()
	if !ok || m.metaOf == nil {
		return nil
	}
	return m.metaOf(option)
}

// visibleItemsCount calculates how many options fit in the terminal
func (m Model) visibleItemsCount() int {
	// Leave space for the filter and other UI elements
	count := m.terminalHeight - 4 // Adjust this number based on your terminal layout

	// The chrome added around the list takes rows of its own. Without counting
	// them the menu is taller than the terminal, which scrolls, and what
	// scrolls off the top is the tab bar -- so on a long list the categories
	// became invisible exactly where they are most useful.
	// The frame around the list: breadcrumb, its rule, and the key hints with
	// the blank line above them. Without counting these the menu is taller than
	// the terminal, it scrolls, and what scrolls away is the header.
	count -= 4
	if m.layout.hasTabs() {
		count -= 2 // the tabs, and the blank line beneath them
	}

	if count < 1 {
		return 1
	}
	return count
}

func displayLabel(opt SelectionOption) string {
	if opt.HasNewEpisodes {
		return "[NEW]" + opt.Label
	}
	return opt.Label
}

// filterOptions filters and sorts options based on the search term
func (m *Model) filterOptions() {
	m.filteredKeys = nil
	for _, opt := range m.allOptions {
		// The add-new entry is pinned below from addNewOption, so letting it
		// through here as well would list it twice. Guarding at the point of
		// use makes the rule hold however the options were set, rather than
		// only when they came through the one path that strips it.
		if opt.Key == "add_new" {
			continue
		}
		// Small function to also consider new episode from list
		if strings.Contains(strings.ToLower(displayLabel(opt)), strings.ToLower(m.filter)) {
			m.filteredKeys = append(m.filteredKeys, opt)
		}
	}

	// Sort alphabetically unless this is a home menu or an ordered action list.
	isMenu := m.preserveOrder
	if !isMenu {
		for _, opt := range m.allOptions {
			if opt.Key == "ALL" || opt.Key == "CURRENT" {
				isMenu = true
				break
			}
		}
	}

	if !isMenu {
		sort.Slice(m.filteredKeys, func(i, j int) bool {
			return m.filteredKeys[i].Label < m.filteredKeys[j].Label
		})
	}

	// Pin Back / Add new / Quit only when the filter is empty or matches their labels.
	// Previously Quit/Back were always forced visible, so "/quit" still showed Back first
	// and Enter could select the wrong row.
	filterLower := strings.ToLower(strings.TrimSpace(m.filter))
	pinMatches := func(label string) bool {
		if filterLower == "" {
			return true
		}
		return strings.Contains(strings.ToLower(label), filterLower)
	}
	backMatchesFilter := filterLower != "" && strings.Contains("back", filterLower)

	// If filter targets "back", pin it above Add new anime
	if !m.isHomeMenu && backMatchesFilter {
		m.filteredKeys = append(m.filteredKeys, SelectionOption{Label: "Back", Key: "-2"})
	}

	// Add new anime when unfiltered or filter matches
	if m.addNewOption && pinMatches("Add new anime") {
		m.filteredKeys = append(m.filteredKeys, SelectionOption{Label: "Add new anime", Key: "add_new"})
	}

	// Back in its default position when unfiltered (or filter matches "back" handled above)
	if !m.isHomeMenu && !backMatchesFilter && pinMatches("Back") {
		m.filteredKeys = append(m.filteredKeys, SelectionOption{Label: "Back", Key: "-2"})
	}

	// Quit last — only when unfiltered or filter matches "quit"
	if pinMatches("Quit") {
		m.filteredKeys = append(m.filteredKeys, SelectionOption{Label: "Quit", Key: "-1"})
	}
}

func detectHomeMenu(options []SelectionOption) bool {
	for _, opt := range options {
		if opt.Key == "ALL" || opt.Key == "CURRENT" {
			return true
		}
	}
	return false
}

// SelectionMeansQuit reports whether the user chose Quit (by key or label).
func SelectionMeansQuit(opt SelectionOption) bool {
	if opt.Key == "-1" {
		return true
	}
	label := strings.TrimSpace(opt.Label)
	return strings.EqualFold(label, "Quit") || strings.EqualFold(label, "quit")
}

// SelectionMeansBack reports whether the user chose Back / dismiss (by key or label).
func SelectionMeansBack(opt SelectionOption) bool {
	if opt.Key == "-2" || strings.EqualFold(opt.Key, "back") {
		return true
	}
	label := strings.ToLower(strings.TrimSpace(opt.Label))
	return label == "back" || label == "back to menu" || label == "back to list"
}

// NormalizeSelectionKey forces Quit/Back labels onto the canonical keys used by callers.
func NormalizeSelectionKey(opt SelectionOption) SelectionOption {
	if SelectionMeansQuit(opt) {
		return SelectionOption{Key: "-1", Label: "Quit"}
	}
	if SelectionMeansBack(opt) {
		return SelectionOption{Key: "-2", Label: "Back"}
	}
	return opt
}

func findSelectionIndex(options []SelectionOption, previousKey string, previousLabel string, fallbackIndex int) int {
	if previousKey != "" {
		for idx, option := range options {
			if option.Key == previousKey {
				return idx
			}
		}
	}

	if previousLabel != "" {
		for idx, option := range options {
			if option.Label == previousLabel {
				return idx
			}
		}
	}

	if fallbackIndex >= 0 && fallbackIndex < len(options) {
		return fallbackIndex
	}

	if len(options) == 0 {
		return -1
	}

	return min(fallbackIndex, len(options)-1)
}

func sortHomeMenuOptions(options []SelectionOption) []SelectionOption {
	config := GetGlobalConfig()
	if config == nil || strings.TrimSpace(config.MenuOrder) == "" {
		return options
	}

	menuOrder := strings.Split(config.MenuOrder, ",")
	optMap := make(map[string]SelectionOption)
	for _, opt := range options {
		optMap[opt.Key] = opt
	}

	sorted := make([]SelectionOption, 0, len(options))
	for _, key := range menuOrder {
		if opt, exists := optMap[key]; exists {
			sorted = append(sorted, opt)
			delete(optMap, key)
		}
	}

	for _, opt := range options {
		if _, exists := optMap[opt.Key]; exists {
			sorted = append(sorted, opt)
			delete(optMap, opt.Key)
		}
	}

	return sorted
}

func previewOptionsToSortedSelection(options map[string]RofiSelectPreview) []SelectionOption {
	type ranked struct {
		option SelectionOption
		rank   int
	}

	entries := make([]ranked, 0, len(options))
	for id, opt := range options {
		entries = append(entries, ranked{
			option: SelectionOption{
				Label:          opt.Title,
				Title:          opt.Title,
				Key:            id,
				Thumbnail:      opt.CoverImage,
				HasNewEpisodes: opt.HasNewEpisodes,
			},
			rank: opt.Rank,
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

	selectionOptions := make([]SelectionOption, 0, len(entries))
	for _, entry := range entries {
		selectionOptions = append(selectionOptions, entry.option)
	}

	return selectionOptions
}

func DynamicSelectPreview(options map[string]RofiSelectPreview, addnewoption bool) (SelectionOption, error) {
	return DynamicSelectPreviewWithRefresh(options, addnewoption, nil)
}

func DynamicSelectPreviewWithRefresh(options map[string]RofiSelectPreview, addnewoption bool, refreshConfig *PreviewSelectionRefreshConfig) (SelectionOption, error) {
	go preDownloadImages(options, 14)

	// Removed boilerplate check

	currentOptions := options

	for {
		var rofiInput strings.Builder
		selectionOptions := previewOptionsToSortedSelection(currentOptions)

		rows := writePreviewRows(&rofiInput, selectionOptions, func(opt SelectionOption) (string, bool) {
			cachePath, err := downloadToCache(currentOptions[opt.Key].CoverImage)
			if err != nil {
				Log(fmt.Sprintf("Error caching image: %v", err))
				return "", false
			}
			return cachePath, true
		})

		if addnewoption {
			rofiInput.WriteString("Add new anime\n")
			rows = append(rows, SelectionOption{Key: "add_new", Label: "Add new anime"})
		}
		rofiInput.WriteString("Back\n")
		rofiInput.WriteString("Quit\n")
		rows = append(rows,
			SelectionOption{Key: "-2", Label: "Back"},
			SelectionOption{Key: "-1", Label: "Quit"},
		)

		// A menu on screen is proof Curd started; anything still showing is stale.
		EndStartupProgress()

		configPath := filepath.Join(GetStoragePath(), "selectanimepreview.rasi")
		// NOTE: Need `-markup-rows` to enable pango
		// -format i returns the index of the chosen row. The label cannot be used:
		// the grid clips it to the column width, so what comes back for a long
		// title is not the string the option carries.
		cmd := exec.Command("rofi", "-dmenu", "-theme", configPath, "-show-icons", "-markup-rows", "-p", "Select Anime", "-i", "-no-custom", "-format", "i")
		cmd.Stdin = strings.NewReader(rofiInput.String())
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if refreshConfig == nil || refreshConfig.Updates == nil {
			if err := cmd.Run(); err != nil {
				Log(fmt.Sprintf("Rofi stderr: %s", stderr.String()))
				Log(fmt.Sprintf("Rofi stdout: %s", stdout.String()))
				return SelectionOption{Key: "-2", Label: "Back"}, nil
			}
			return parsePreviewSelectionIndex(stdout.String(), rows)
		}

		if err := cmd.Start(); err != nil {
			return SelectionOption{}, fmt.Errorf("failed to run Rofi preview menu: %w", err)
		}

		waitCh := make(chan error, 1)
		go func() {
			waitCh <- cmd.Wait()
		}()

		restartMenu := false

		for !restartMenu {
			select {
			case err := <-waitCh:
				if err != nil {
					Log(fmt.Sprintf("Rofi stderr: %s", stderr.String()))
					Log(fmt.Sprintf("Rofi stdout: %s", stdout.String()))
					return SelectionOption{Key: "-2", Label: "Back"}, nil
				}
				return parsePreviewSelectionIndex(stdout.String(), rows)
			case updatedList, ok := <-refreshConfig.Updates:
				if !ok {
					refreshConfig = nil
					continue
				}

				updatedOptions := refreshConfig.BuildOptions(updatedList)
				if reflect.DeepEqual(currentOptions, updatedOptions) {
					continue
				}

				currentOptions = updatedOptions
				restartMenu = true

				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				<-waitCh
			}
		}
	}
}

func preDownloadImages(options map[string]RofiSelectPreview, count int) {
	i := 0
	for _, option := range options {
		if i >= count {
			break
		}
		downloadToCache(option.CoverImage)
		i++
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
			continue
		}
		// Every row goes through the markup builder, not just the ones with new
		// episodes: it is what escapes pango and dims the counts, and skipping
		// it left ordinary rows unescaped.
		label := GridRowMarkup(opt.Label, rofitheme.GridLabelCapacity)
		if opt.HasNewEpisodes {
			label = fmt.Sprintf("<span foreground=\"%s\">[NEW]</span> %s", rofiNewEpisodeColor, label)
		}
		w.WriteString(fmt.Sprintf("%s\x00icon\x1f%s\n", label, cachePath))
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
	back := SelectionOption{Key: "-2", Label: "Back"}

	index, err := strconv.Atoi(strings.TrimSpace(rawSelection))
	if err != nil {
		return back, nil
	}
	if index < 0 || index >= len(rows) {
		return back, nil
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

func downloadToCache(imageURL string) (string, error) {
	if strings.TrimSpace(imageURL) == "" {
		return "", fmt.Errorf("image URL is empty")
	}

	cachePath, err := coverCachePath(imageURL)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Check if file already exists in cache
	if info, err := os.Stat(cachePath); err == nil {
		if info.Size() > 0 {
			return cachePath, nil
		}
		_ = os.Remove(cachePath)
	}

	// Download the image
	resp, err := sharedHTTPClient.Get(imageURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download image: status %d", resp.StatusCode)
	}

	file, err := os.Create(cachePath)
	if err != nil {
		return "", err
	}

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		file.Close()
		os.Remove(cachePath) // Clean up on error
		return "", err
	}
	if err := file.Close(); err != nil {
		os.Remove(cachePath)
		return "", err
	}

	return cachePath, nil
}

func RofiSelect(options []SelectionOption, isHomeMenu bool) (SelectionOption, error) {
	return RofiSelectWithRefresh(options, isHomeMenu, nil)
}

// RofiSelectWithMessage shows a Rofi dmenu with an optional -mesg banner (for
// release notes, error diagnosis, etc.) so callers do not need notify-send spam.
func RofiSelectWithMessage(options []SelectionOption, isHomeMenu bool, prompt, message string) (SelectionOption, error) {
	return rofiSelectInternal(options, isHomeMenu, nil, prompt, message)
}

func RofiSelectWithRefresh(options []SelectionOption, isHomeMenu bool, refreshConfig *SelectionRefreshConfig) (SelectionOption, error) {
	return rofiSelectInternal(options, isHomeMenu, refreshConfig, "Select", "")
}

func rofiSelectInternal(options []SelectionOption, isHomeMenu bool, refreshConfig *SelectionRefreshConfig, prompt, message string) (SelectionOption, error) {
	currentOptions := options
	if strings.TrimSpace(prompt) == "" {
		prompt = "Select"
	}

	for {
		// A menu on screen is proof Curd started; anything still showing is stale.
		EndStartupProgress()

		optionsString := buildRofiOptionsString(currentOptions, isHomeMenu)
		configPath := filepath.Join(GetStoragePath(), "selectanime.rasi")
		args := []string{"-dmenu", "-theme", configPath, "-i", "-markup", "-markup-rows", "-p", prompt}
		if msg := strings.TrimSpace(message); msg != "" {
			args = append(args, "-mesg", msg)
		}
		cmd := exec.Command("rofi", args...)
		cmd.Stdin = strings.NewReader(optionsString)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if refreshConfig == nil || refreshConfig.Updates == nil {
			err := cmd.Run()
			return parseRofiSelection(err, stdout.String(), currentOptions, isHomeMenu)
		}

		if err := cmd.Start(); err != nil {
			return SelectionOption{}, fmt.Errorf("failed to run Rofi: %w", err)
		}

		waitCh := make(chan error, 1)
		go func() {
			waitCh <- cmd.Wait()
		}()

		restartMenu := false

		for !restartMenu {
			select {
			case err := <-waitCh:
				if err != nil {
					Log(fmt.Sprintf("Rofi stderr: %s", stderr.String()))
				}
				return parseRofiSelection(err, stdout.String(), currentOptions, isHomeMenu)
			case updatedList, ok := <-refreshConfig.Updates:
				if !ok {
					refreshConfig = nil
					continue
				}

				updatedOptions := refreshConfig.BuildOptions(updatedList)
				if reflect.DeepEqual(currentOptions, updatedOptions) {
					continue
				}

				currentOptions = updatedOptions
				restartMenu = true

				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				<-waitCh
			}
		}
	}
}

func DynamicSelectFromSlice(options []SelectionOption) (SelectionOption, error) {
	return dynamicSelectInternal(options, nil, false)
}

// DynamicSelect displays a simple selection prompt without extra features
func DynamicSelect(options []SelectionOption) (SelectionOption, error) {
	return dynamicSelectInternal(options, nil, false)
}

// DynamicSelectPreserveOrder is like DynamicSelect but keeps the caller's option order
// (no alphabetical sort). Use for action menus where the first item is the primary action.
func DynamicSelectPreserveOrder(options []SelectionOption) (SelectionOption, error) {
	return dynamicSelectInternal(options, nil, true)
}

var promptSelect = DynamicSelect

// promptSelectOrdered is used for action menus where option order matters.
// Tests may replace this the same way as promptSelect.
var promptSelectOrdered = DynamicSelectPreserveOrder

func DynamicSelectWithRefresh(options []SelectionOption, refreshConfig *SelectionRefreshConfig) (SelectionOption, error) {
	return dynamicSelectInternal(options, refreshConfig, false)
}

func dynamicSelectInternal(options []SelectionOption, refreshConfig *SelectionRefreshConfig, preserveOrder bool) (SelectionOption, error) {
	isHomeMenu := detectHomeMenu(options)

	if isHomeMenu {
		options = sortHomeMenuOptions(options)
	}

	if config := GetGlobalConfig(); config != nil && config.RofiSelection {
		return RofiSelectWithRefresh(options, isHomeMenu, refreshConfig)
	}

	// Separate out the "add_new" sentinel so it is never sorted alphabetically.
	// The addNewOption flag causes filterOptions() to append it after the sort.
	hasAddNew := false
	for _, opt := range options {
		if opt.Key == "add_new" {
			hasAddNew = true
			break
		}
	}
	cleanOptions := withoutAddNewSentinel(options)

	model := &Model{
		allOptions:    cleanOptions,
		isHomeMenu:    isHomeMenu,
		addNewOption:  hasAddNew,
		preserveOrder: preserveOrder,
	}
	attachCategoryTabs(model, refreshConfig)
	attachDetailPane(model)
	model.filterOptions()

	p := tea.NewProgram(model)
	stopRefresh := make(chan struct{})

	if refreshConfig != nil && refreshConfig.Updates != nil {
		go func(lastOptions []SelectionOption) {
			currentOptions := lastOptions
			for {
				select {
				case <-stopRefresh:
					return
				case updatedList, ok := <-refreshConfig.Updates:
					if !ok {
						return
					}

					updatedOptions := refreshConfig.BuildOptions(updatedList)
					if reflect.DeepEqual(currentOptions, updatedOptions) {
						continue
					}

					currentOptions = updatedOptions
					p.Send(optionsRefreshedMsg{options: updatedOptions})
				}
			}
		}(append([]SelectionOption(nil), options...))
	}

	finalModel, err := p.Run()
	close(stopRefresh)

	// Bubbletea may leave the terminal in raw mode; always reset cursor state.
	fmt.Print("\033[?25h")
	fmt.Print("\033[?7h")
	if err != nil {
		RestoreScreen()
		return SelectionOption{}, err
	}

	finalSelectionModel, ok := finalModel.(*Model)
	if !ok {
		return SelectionOption{}, fmt.Errorf("unexpected model type")
	}

	if finalSelectionModel.selected >= 0 && finalSelectionModel.selected < len(finalSelectionModel.filteredKeys) {
		return NormalizeSelectionKey(finalSelectionModel.filteredKeys[finalSelectionModel.selected]), nil
	}
	// Empty list / out of range — treat as cancel (Back for submenus, Quit on home).
	if finalSelectionModel.isHomeMenu {
		return SelectionOption{Key: "-1", Label: "Quit"}, nil
	}
	return SelectionOption{Key: "-2", Label: "Back"}, nil
}

func buildRofiOptionsString(options []SelectionOption, isHomeMenu bool) string {
	optionsList := make([]string, 0, len(options)+2)
	for _, opt := range options {
		row := rofiRowMarkup(opt.Label)
		if opt.HasNewEpisodes {
			row = fmt.Sprintf("<span foreground=\"%s\">[NEW]</span> %s", rofiNewEpisodeColor, row)
		}
		optionsList = append(optionsList, row)
	}

	if !isHomeMenu {
		optionsList = append(optionsList, "Back")
	}
	optionsList = append(optionsList, "Quit")

	return strings.Join(optionsList, "\n")
}

func parseRofiSelection(err error, rawSelection string, options []SelectionOption, isHomeMenu bool) (SelectionOption, error) {
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok && exitError.ExitCode() == 1 {
			if isHomeMenu {
				return SelectionOption{Key: "-1", Label: "Quit"}, nil
			}
			return SelectionOption{Key: "-2", Label: "Back"}, nil
		}
		return SelectionOption{}, fmt.Errorf("failed to run Rofi: %v", err)
	}

	selected := strings.TrimSpace(rawSelection)
	// strip accidental pango noise if a theme echoes it.
	selected = unescapePango(strings.TrimSpace(pangoStrip.ReplaceAllString(
		ansiStrip.ReplaceAllString(selected, ""), "",
	)))
	selected = strings.TrimPrefix(selected, "[NEW] ")
	selected = strings.TrimSpace(selected)
	switch {
	case selected == "":
		if isHomeMenu {
			return SelectionOption{Key: "-1", Label: "Quit"}, nil
		}
		return SelectionOption{Key: "-2", Label: "Back"}, nil
	case strings.EqualFold(selected, "Back"), strings.EqualFold(selected, "Back to menu"), strings.EqualFold(selected, "Back to list"):
		return SelectionOption{Label: "Back", Key: "-2"}, nil
	case strings.EqualFold(selected, "Quit"):
		return SelectionOption{Label: "Quit", Key: "-1"}, nil
	}

	for _, opt := range options {
		if opt.Label == selected || strings.EqualFold(opt.Label, selected) {
			return NormalizeSelectionKey(opt), nil
		}
		// Match when emoji/spacing differs slightly (e.g. double-space after emoji).
		if strings.EqualFold(strings.Join(strings.Fields(opt.Label), " "), strings.Join(strings.Fields(selected), " ")) {
			return NormalizeSelectionKey(opt), nil
		}
	}

	return SelectionOption{}, fmt.Errorf("selected option not found in original list")
}

// withoutAddNewSentinel removes the "add new" entry from a list of options.
//
// filterOptions appends it from the addNewOption flag, so any list that still
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

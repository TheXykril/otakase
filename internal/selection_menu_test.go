package internal

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// withVimKeys runs a test under one key scheme and puts the config back after.
func withVimKeys(t *testing.T, on bool) {
	t.Helper()
	saved := GetGlobalConfig()
	SetGlobalConfig(&Config{VimKeys: on})
	t.Cleanup(func() { SetGlobalConfig(saved) })
}

func runeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

// press sends each message to the model in turn and returns the last command.
func press(t *testing.T, m *selectionModel, msgs ...tea.Msg) tea.Cmd {
	t.Helper()
	var cmd tea.Cmd
	for _, msg := range msgs {
		var next tea.Model
		next, cmd = m.Update(msg)
		if next.(*selectionModel) != m {
			t.Fatalf("Update returned a different model")
		}
	}
	return cmd
}

func typeText(t *testing.T, m *selectionModel, text string) {
	t.Helper()
	for _, r := range text {
		press(t, m, runeKey(r))
	}
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func keysOf(options []SelectionOption) []string {
	keys := make([]string, len(options))
	for i, option := range options {
		keys[i] = option.Key
	}
	return keys
}

// newMenu builds a menu the way dynamicSelectInternal does, in a terminal
// tall enough for every row unless the test shrinks it.
func newMenu(options ...SelectionOption) *selectionModel {
	m := &selectionModel{allOptions: options, terminalWidth: 100, terminalHeight: 40}
	m.applyFilter()
	return m
}

func numberedOptions(n int) []SelectionOption {
	options := make([]SelectionOption, n)
	for i := range options {
		options[i] = SelectionOption{Key: fmt.Sprint(i), Label: fmt.Sprintf("Show %02d", i)}
	}
	return options
}

func TestMenuTypingFiltersAndResetsPosition(t *testing.T) {
	withVimKeys(t, false)
	m := newMenu(
		SelectionOption{Key: "c", Label: "Charlie"},
		SelectionOption{Key: "a", Label: "Alpha"},
		SelectionOption{Key: "b", Label: "Bravo"},
	)
	if got := strings.Join(keysOf(m.filteredKeys), ","); got != "a,b,c,-2,-1" {
		t.Fatalf("unfiltered rows = %s, want sorted titles then Back and Quit", got)
	}

	press(t, m, tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyDown})
	typeText(t, m, "LPH")
	if m.filter != "LPH" || m.selected != 0 || m.firstVisibleRow != 0 {
		t.Fatalf("after typing: filter=%q selected=%d offset=%d", m.filter, m.selected, m.firstVisibleRow)
	}
	if got := strings.Join(keysOf(m.filteredKeys), ","); got != "a" {
		t.Fatalf("filter is case-insensitive and hides pinned rows that do not match; got %s", got)
	}

	press(t, m, tea.KeyMsg{Type: tea.KeyBackspace}, tea.KeyMsg{Type: tea.KeyBackspace},
		tea.KeyMsg{Type: tea.KeyBackspace}, tea.KeyMsg{Type: tea.KeyBackspace})
	if m.filter != "" || len(m.filteredKeys) != 5 {
		t.Fatalf("backspace past empty: filter=%q rows=%d", m.filter, len(m.filteredKeys))
	}
}

// Only printable ASCII is text; named keys never land in the filter.
func TestMenuIgnoresNonTextKeys(t *testing.T) {
	withVimKeys(t, false)
	m := newMenu(numberedOptions(3)...)
	press(t, m, tea.KeyMsg{Type: tea.KeyCtrlA}, tea.KeyMsg{Type: tea.KeyF1}, runeKey('é'))
	if m.filter != "" {
		t.Fatalf("filter = %q, want empty", m.filter)
	}
	press(t, m, tea.KeyMsg{Type: tea.KeySpace})
	if m.filter != " " {
		t.Fatalf("space should type into the filter, got %q", m.filter)
	}
}

func TestMenuMovementStopsAtTheEnds(t *testing.T) {
	withVimKeys(t, false)
	m := newMenu(numberedOptions(2)...) // plus Back and Quit

	press(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.selected != 0 {
		t.Fatalf("up at the top moved to %d", m.selected)
	}
	for _, key := range []tea.KeyType{tea.KeyDown, tea.KeyTab, tea.KeyCtrlN, tea.KeyDown, tea.KeyDown} {
		press(t, m, tea.KeyMsg{Type: key})
	}
	if m.selected != 3 {
		t.Fatalf("down past the bottom: selected=%d, want 3", m.selected)
	}
	press(t, m, tea.KeyMsg{Type: tea.KeyShiftTab}, tea.KeyMsg{Type: tea.KeyCtrlP})
	if m.selected != 1 {
		t.Fatalf("shift+tab and ctrl+p should move up: selected=%d", m.selected)
	}
	// Without tabs, left and right do nothing in the default scheme.
	press(t, m, tea.KeyMsg{Type: tea.KeyLeft}, tea.KeyMsg{Type: tea.KeyRight})
	if m.selected != 1 || m.filter != "" {
		t.Fatalf("left/right changed the menu: selected=%d filter=%q", m.selected, m.filter)
	}
}

func TestMenuScrollsToKeepTheHighlightOnScreen(t *testing.T) {
	withVimKeys(t, false)
	m := newMenu(numberedOptions(10)...)
	m.terminalHeight = 11 // eight rows of chrome leave three for the list
	if got := m.rowsThatFit(); got != 3 {
		t.Fatalf("rowsThatFit = %d, want 3", got)
	}

	for i := 1; i < len(m.filteredKeys); i++ {
		press(t, m, tea.KeyMsg{Type: tea.KeyDown})
		if want := max(0, m.selected-2); m.firstVisibleRow != want {
			t.Fatalf("at row %d: offset=%d, want %d", m.selected, m.firstVisibleRow, want)
		}
	}
	for m.selected > 0 {
		press(t, m, tea.KeyMsg{Type: tea.KeyUp})
		if m.selected < m.firstVisibleRow || m.selected >= m.firstVisibleRow+3 {
			t.Fatalf("row %d is off screen at offset %d", m.selected, m.firstVisibleRow)
		}
	}
	if m.firstVisibleRow != 0 {
		t.Fatalf("back at the top with offset %d", m.firstVisibleRow)
	}
}

func TestMenuViewShowsOnlyTheVisibleRows(t *testing.T) {
	withVimKeys(t, false)
	m := newMenu(numberedOptions(10)...)
	m.terminalHeight = 11
	press(t, m, tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyDown})

	view := m.View()
	for i := 1; i <= 3; i++ {
		if !strings.Contains(view, fmt.Sprintf("Show %02d", i)) {
			t.Errorf("Show %02d should be on screen:\n%s", i, view)
		}
	}
	for _, hidden := range []string{"Show 00", "Show 04"} {
		if strings.Contains(view, hidden) {
			t.Errorf("%s is scrolled out but drawn:\n%s", hidden, view)
		}
	}
}

func TestMenuViewSaysWhenNothingMatches(t *testing.T) {
	withVimKeys(t, false)
	m := newMenu(numberedOptions(3)...)
	m.isHomeMenu = true
	typeText(t, m, "zzz")
	if len(m.filteredKeys) != 0 {
		t.Fatalf("rows = %v", keysOf(m.filteredKeys))
	}
	if view := m.View(); !strings.Contains(view, "No matches found.") {
		t.Fatalf("no notice in view:\n%s", view)
	}
	if cmd := press(t, m, tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("enter with nothing listed should not end the menu")
	}
}

func TestVisibleItemsCountLeavesRoomForChrome(t *testing.T) {
	m := selectionModel{terminalHeight: 30}
	if got := m.rowsThatFit(); got != 22 {
		t.Errorf("plain menu: %d rows, want 22", got)
	}
	m.layout.tabs = []Tab{{Key: "a"}, {Key: "b"}}
	if got := m.rowsThatFit(); got != 20 {
		t.Errorf("with tabs: %d rows, want 20", got)
	}
	m.terminalHeight = 3
	if got := m.rowsThatFit(); got != 1 {
		t.Errorf("tiny terminal: %d rows, want 1", got)
	}
}

func TestMenuEscapeBacksOut(t *testing.T) {
	for _, vim := range []bool{false, true} {
		for _, home := range []bool{false, true} {
			withVimKeys(t, vim)
			m := newMenu(numberedOptions(2)...)
			m.isHomeMenu = home
			cmd := press(t, m, tea.KeyMsg{Type: tea.KeyEsc})
			want := "-2"
			if home {
				want = "-1"
			}
			if !isQuit(cmd) || len(m.filteredKeys) != 1 || m.filteredKeys[0].Key != want || m.selected != 0 {
				t.Errorf("vim=%v home=%v: quit=%v rows=%v", vim, home, isQuit(cmd), keysOf(m.filteredKeys))
			}
		}
	}
}

func TestMenuEnterPicksTheHighlightedRow(t *testing.T) {
	withVimKeys(t, false)
	m := newMenu(numberedOptions(3)...)
	cmd := press(t, m, tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyEnter})
	if !isQuit(cmd) {
		t.Fatal("enter did not end the menu")
	}
	if picked, _ := m.highlighted(); picked.Key != "1" {
		t.Fatalf("picked %+v", picked)
	}
}

// Choosing "Add new anime" answers with the label callers check for.
func TestMenuEnterOnAddNew(t *testing.T) {
	withVimKeys(t, false)
	m := newMenu(numberedOptions(1)...)
	m.addNewOption = true
	m.applyFilter()
	if got := strings.Join(keysOf(m.filteredKeys), ","); got != "0,add_new,-2,-1" {
		t.Fatalf("rows = %s", got)
	}
	cmd := press(t, m, tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyEnter})
	picked, _ := m.highlighted()
	if !isQuit(cmd) || picked.Label != "add_new" || picked.Key != "0" {
		t.Fatalf("picked %+v, quit=%v", picked, isQuit(cmd))
	}
}

func TestPinnedRowsFollowTheFilter(t *testing.T) {
	cases := []struct {
		filter string
		home   bool
		want   string
	}{
		{"", false, "add_new,-2,-1"},
		{"", true, "add_new,-1"},
		// "a" matches both Back and Add new; Back moves up so it is the row
		// reached first.
		{"a", false, "-2,add_new"},
		{" QUIT ", false, "-1"},
		{"back", true, ""},
	}
	for _, c := range cases {
		m := &selectionModel{filter: c.filter, isHomeMenu: c.home, addNewOption: true}
		m.applyFilter()
		if got := strings.Join(keysOf(m.filteredKeys), ","); got != c.want {
			t.Errorf("filter=%q home=%v: rows %s, want %s", c.filter, c.home, got, c.want)
		}
	}
}

func TestMenuOrderIsKeptForMenusThatHaveOne(t *testing.T) {
	options := []SelectionOption{{Key: "z", Label: "Zulu"}, {Key: "a", Label: "Alpha"}}

	ordered := &selectionModel{allOptions: options, preserveOrder: true, isHomeMenu: true}
	ordered.applyFilter()
	if got := strings.Join(keysOf(ordered.filteredKeys), ","); got != "z,a,-1" {
		t.Errorf("preserveOrder: %s", got)
	}

	home := &selectionModel{allOptions: append(options, SelectionOption{Key: "ALL", Label: "All"}), isHomeMenu: true}
	home.applyFilter()
	if got := strings.Join(keysOf(home.filteredKeys), ","); got != "z,a,ALL,-1" {
		t.Errorf("home menu: %s", got)
	}
}

func TestMenuRefreshKeepsTheHighlightedEntry(t *testing.T) {
	withVimKeys(t, false)
	m := newMenu(
		SelectionOption{Key: "a", Label: "Alpha"},
		SelectionOption{Key: "b", Label: "Bravo"},
	)
	press(t, m, tea.KeyMsg{Type: tea.KeyDown})

	press(t, m, optionsReloadedMsg{options: []SelectionOption{
		{Key: "0", Label: "Aardvark"},
		{Key: "b", Label: "Bravo, renamed"},
		{Key: "add_new", Label: "Add new anime"},
	}})
	if picked, _ := m.highlighted(); picked.Key != "b" {
		t.Fatalf("highlight moved to %+v", picked)
	}
	for _, option := range m.filteredKeys {
		if option.Key == "add_new" {
			t.Fatal("the add-new sentinel came through a refresh as a row")
		}
	}

	// With the entry gone the highlight keeps its position, which still
	// exists among the pinned rows.
	press(t, m, optionsReloadedMsg{})
	if len(m.filteredKeys) != 2 || m.selected != 1 || m.firstVisibleRow != 0 {
		t.Fatalf("empty refresh: rows=%v selected=%d offset=%d", keysOf(m.filteredKeys), m.selected, m.firstVisibleRow)
	}
}

func TestMenuRefreshScrollsToTheKeptEntry(t *testing.T) {
	withVimKeys(t, false)
	m := newMenu(numberedOptions(3)...)
	m.terminalHeight = 11
	press(t, m, tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyDown})

	// The kept entry sorts to the end of a longer list.
	fresh := []SelectionOption{{Key: "2", Label: "Show 99"}}
	for i := range 9 {
		fresh = append(fresh, SelectionOption{Key: fmt.Sprintf("n%d", i), Label: fmt.Sprintf("Show %02d", i)})
	}
	press(t, m, optionsReloadedMsg{options: fresh})
	if picked, _ := m.highlighted(); picked.Key != "2" {
		t.Fatalf("highlight on %+v", picked)
	}
	if m.selected < m.firstVisibleRow || m.selected >= m.firstVisibleRow+m.rowsThatFit() {
		t.Fatalf("row %d is off screen at offset %d", m.selected, m.firstVisibleRow)
	}
}

func TestMenuTracksTerminalSize(t *testing.T) {
	m := newMenu()
	if cmd := press(t, m, tea.WindowSizeMsg{Width: 120, Height: 33}); cmd != nil {
		t.Fatal("a resize should not produce a command")
	}
	if m.terminalWidth != 120 || m.terminalHeight != 33 {
		t.Fatalf("size = %dx%d", m.terminalWidth, m.terminalHeight)
	}
}

func TestVimSearchModeTypesAndEscapeKeepsTheQuery(t *testing.T) {
	withVimKeys(t, true)
	m := newMenu(
		SelectionOption{Key: "a", Label: "Alpha"},
		SelectionOption{Key: "b", Label: "Bravo"},
		SelectionOption{Key: "c", Label: "Charlie"},
	)

	press(t, m, tea.KeyMsg{Type: tea.KeyBackspace}, runeKey('x'))
	if m.filter != "" {
		t.Fatalf("normal mode typed into the filter: %q", m.filter)
	}

	press(t, m, runeKey('?'))
	if !m.filterActive {
		t.Fatal("? should open search, as / does")
	}
	typeText(t, m, "lh")
	press(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if m.filter != "l" {
		t.Fatalf("filter = %q, want l", m.filter)
	}
	// Only Alpha and Charlie contain an l.
	press(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if m.selected != 1 {
		t.Fatalf("right should move down while searching, selected=%d", m.selected)
	}
	press(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	if m.selected != 0 {
		t.Fatalf("left should move up while searching, selected=%d", m.selected)
	}

	if cmd := press(t, m, tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		t.Fatal("escape in search mode should close the search, not the menu")
	}
	if m.filterActive || m.filter != "l" {
		t.Fatalf("after escape: active=%v filter=%q", m.filterActive, m.filter)
	}
	press(t, m, runeKey('j'))
	if m.selected != 1 || m.filter != "l" {
		t.Fatalf("j after escape should move: selected=%d filter=%q", m.selected, m.filter)
	}
}

func TestVimNormalModeMotions(t *testing.T) {
	withVimKeys(t, true)
	m := newMenu(numberedOptions(4)...)
	steps := []struct {
		key  tea.KeyMsg
		want int
	}{
		{runeKey('l'), 1},
		{tea.KeyMsg{Type: tea.KeyRight}, 2},
		{runeKey('h'), 1},
		{tea.KeyMsg{Type: tea.KeyLeft}, 0},
		{tea.KeyMsg{Type: tea.KeyTab}, 1},
		{runeKey('k'), 0},
	}
	for _, step := range steps {
		press(t, m, step.key)
		if m.selected != step.want {
			t.Fatalf("after %s: selected=%d, want %d", step.key, m.selected, step.want)
		}
	}
}

func TestMenuFooterActionEndsTheMenu(t *testing.T) {
	withVimKeys(t, false)
	m := newMenu(numberedOptions(2)...)
	m.layout.footer = []FooterAction{{Key: "CONTINUE", Label: "Continue", Hint: "ctrl+u"}}

	cmd := press(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})
	if !isQuit(cmd) || len(m.filteredKeys) != 1 || m.filteredKeys[0].Key != "CONTINUE" {
		t.Fatalf("quit=%v rows=%v", isQuit(cmd), keysOf(m.filteredKeys))
	}
}

func TestMenuTabKeysSwitchCategories(t *testing.T) {
	lists := map[string][]SelectionOption{
		"one": numberedOptions(5),
		"two": {{Key: "t", Label: "Two only"}},
	}
	for _, c := range []struct {
		vim  bool
		next tea.KeyMsg
	}{
		{false, tea.KeyMsg{Type: tea.KeyRight}},
		{false, tea.KeyMsg{Type: tea.KeyTab}},
		{true, tea.KeyMsg{Type: tea.KeyTab}},
	} {
		withVimKeys(t, c.vim)
		m := newMenu(lists["one"]...)
		m.layout.tabs = []Tab{{Key: "one"}, {Key: "two"}}
		m.loadTab = func(key string) []SelectionOption { return lists[key] }
		press(t, m, tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyDown})

		press(t, m, c.next)
		if m.layout.activeTabKey() != "two" || m.selected != 0 || m.firstVisibleRow != 0 || m.filteredKeys[0].Key != "t" {
			t.Errorf("vim=%v %s: tab=%q selected=%d rows=%v", c.vim, c.next, m.layout.activeTabKey(), m.selected, keysOf(m.filteredKeys))
		}
		press(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
		if m.layout.activeTabKey() != "one" {
			t.Errorf("vim=%v: shift+tab left the tab on %q", c.vim, m.layout.activeTabKey())
		}
	}
}

func TestParseRofiSelectionRecognisesBackAndQuit(t *testing.T) {
	options := []SelectionOption{{Key: "x", Label: "Frieren  ✨"}}
	cases := []struct {
		raw  string
		home bool
		want string
	}{
		{"", false, "-2"},
		{"", true, "-1"},
		{"back to list", true, "-2"},
		{"QUIT", false, "-1"},
		{"[NEW] frieren ✨", false, "x"},
		{"<b>Frieren</b>  ✨\n", false, "x"},
	}
	for _, c := range cases {
		got, err := parseRofiSelection(nil, c.raw, options, c.home)
		if err != nil || got.Key != c.want {
			t.Errorf("parseRofiSelection(%q, home=%v) = %+v, %v; want key %s", c.raw, c.home, got, err, c.want)
		}
	}
	if _, err := parseRofiSelection(nil, "Something else", options, false); err == nil {
		t.Error("an unknown row should be an error")
	}
}

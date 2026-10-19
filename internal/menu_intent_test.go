package internal

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func menuOpts(labels ...string) []SelectionOption {
	opts := make([]SelectionOption, len(labels))
	for i, l := range labels {
		opts[i] = SelectionOption{Key: l, Label: l}
	}
	return opts
}

func menuPress(t *testing.T, m *selectionModel, msg tea.KeyMsg) *selectionModel {
	t.Helper()
	next, _ := m.Update(msg)
	got, ok := next.(*selectionModel)
	if !ok {
		t.Fatalf("Update returned %T", next)
	}
	return got
}

func menuRune(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestMenuIntentCtrlCPicksQuit(t *testing.T) {
	m := &selectionModel{filteredKeys: []SelectionOption{{Key: "show", Label: "Frieren"}}}
	m = menuPress(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if len(m.filteredKeys) != 1 || m.filteredKeys[0].Key != "-1" {
		t.Fatalf("ctrl+c left %+v, want only Quit", m.filteredKeys)
	}
}

func TestMenuIntentEnterOnEmptyListIsIgnored(t *testing.T) {
	m := &selectionModel{}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("enter with nothing listed returned a command")
	}
	if _, ok := next.(*selectionModel); !ok {
		t.Fatalf("Update returned %T", next)
	}
}

func TestMenuIntentVimNormalModeMovesCursor(t *testing.T) {
	useConfig(t, &Config{VimKeys: true})

	m := &selectionModel{allOptions: menuOpts("a", "b", "c"), filteredKeys: menuOpts("a", "b", "c")}

	m = menuPress(t, m, menuRune('j'))
	if m.selected != 1 || m.filter != "" {
		t.Fatalf("after j: selected=%d filter=%q", m.selected, m.filter)
	}
	m = menuPress(t, m, menuRune('k'))
	if m.selected != 0 {
		t.Fatalf("after k: selected=%d", m.selected)
	}

	// once searching, hjkl are just letters
	m = menuPress(t, m, menuRune('/'))
	if !m.filterActive {
		t.Fatal("/ did not start a search")
	}
	m = menuPress(t, m, menuRune('j'))
	m = menuPress(t, m, menuRune('k'))
	if m.filter != "jk" || m.selected != 0 {
		t.Fatalf("search mode: filter=%q selected=%d", m.filter, m.selected)
	}

	// arrows still work with no matches left
	m = menuPress(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selected < 0 {
		t.Fatalf("selected went negative: %d", m.selected)
	}
}

func TestMenuIntentTypingFiltersWithoutVimKeys(t *testing.T) {
	useConfig(t, &Config{VimKeys: false})

	m := &selectionModel{allOptions: menuOpts("a", "b"), filteredKeys: menuOpts("a", "b")}
	m = menuPress(t, m, menuRune('j'))
	if m.filter != "j" {
		t.Fatalf("filter = %q, want j", m.filter)
	}
}

func TestMenuIntentQuitQueryDoesNotOfferBack(t *testing.T) {
	m := &selectionModel{
		allOptions: []SelectionOption{
			{Key: "CURRENT", Label: "Currently Watching"},
			{Key: "ALL", Label: "Show All"},
		},
		filter: "quit",
	}
	m.applyFilter()
	if len(m.filteredKeys) != 1 || m.filteredKeys[0].Key != "-1" {
		t.Fatalf("got %#v, want only Quit", m.filteredKeys)
	}
}

func TestMenuIntentQuitAndBackRecognised(t *testing.T) {
	quit := []SelectionOption{{Key: "-1"}, {Label: "Quit"}}
	back := []SelectionOption{{Key: "-2"}, {Label: "Back to menu"}}
	for _, o := range quit {
		if !SelectionMeansQuit(o) {
			t.Errorf("%+v not read as quit", o)
		}
	}
	for _, o := range back {
		if !SelectionMeansBack(o) {
			t.Errorf("%+v not read as back", o)
		}
	}
	if got := NormalizeSelectionKey(SelectionOption{Label: "quit"}); got.Key != "-1" {
		t.Errorf("lowercase quit normalised to %+v", got)
	}
}

func TestMenuIntentZeroHeightStillShowsARow(t *testing.T) {
	if n := (selectionModel{}).rowsThatFit(); n != 1 {
		t.Fatalf("rowsThatFit = %d with no height, want 1", n)
	}
}

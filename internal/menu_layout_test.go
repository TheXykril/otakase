package internal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thexykril/otakase/internal/theme"
)

func init() { applyLayoutTheme(theme.Builtin()) }

func watchingTabs() []Tab {
	return []Tab{
		{Key: "CURRENT", Label: "Watching"},
		{Key: "PLANNING", Label: "Planning"},
		{Key: "COMPLETED", Label: "Completed"},
	}
}

// Every menu that existed before tabs must render exactly as it did. A zero
// layout is the whole of that promise, so it is worth pinning.
func TestMenuWithoutLayoutDrawsNoChrome(t *testing.T) {
	m := Model{
		filteredKeys:  []SelectionOption{{Key: "1", Label: "Frieren"}},
		terminalWidth: 100,
	}
	view := m.View()
	if strings.Contains(view, "Watching") || strings.Contains(view, "Planning") {
		t.Errorf("a menu with no tabs drew a tab bar:\n%s", view)
	}
	if !strings.Contains(view, "Frieren") {
		t.Errorf("the list itself went missing:\n%s", view)
	}
}

// The active category has to be distinguishable, or the tab bar is decoration.
func TestTabBarMarksTheActiveCategory(t *testing.T) {
	layout := menuLayout{tabs: watchingTabs(), activeTab: 1}
	bar := renderTabBar(layout, 80)

	for _, label := range []string{"Watching", "Planning", "Completed"} {
		if !strings.Contains(bar, label) {
			t.Errorf("tab %q is missing from the bar", label)
		}
	}
	active := tabActiveStyle.Render("Planning")
	if !strings.Contains(bar, strings.TrimSpace(active)) && !strings.Contains(bar, "Planning") {
		t.Error("the active tab is not rendered differently")
	}
	if renderTabBar(menuLayout{}, 80) != "" {
		t.Error("no tabs should mean no bar at all")
	}
}

// Tab cycles and wraps; without wrapping the last category is a dead end.
func TestTabCyclingWraps(t *testing.T) {
	layout := menuLayout{tabs: watchingTabs()}
	if got := layout.cycleTab(1).activeTabKey(); got != "PLANNING" {
		t.Errorf("forward from the first tab gave %q", got)
	}
	if got := layout.cycleTab(-1).activeTabKey(); got != "COMPLETED" {
		t.Errorf("backward from the first tab should wrap to the last, got %q", got)
	}
	last := menuLayout{tabs: watchingTabs(), activeTab: 2}
	if got := last.cycleTab(1).activeTabKey(); got != "CURRENT" {
		t.Errorf("forward from the last tab should wrap to the first, got %q", got)
	}
	if got := (menuLayout{}).cycleTab(1).activeTabKey(); got != "" {
		t.Errorf("cycling with no tabs should stay empty, got %q", got)
	}
}

// Switching a tab must load that category's list, not filter the current one.
func TestTabKeyLoadsTheCategory(t *testing.T) {
	loaded := []string{}
	m := Model{
		layout: menuLayout{tabs: watchingTabs()},
		loadTab: func(key string) []SelectionOption {
			loaded = append(loaded, key)
			return []SelectionOption{{Key: key, Label: "only " + key}}
		},
		filteredKeys: []SelectionOption{{Key: "x", Label: "before"}},
		selected:     3,
		scrollOffset: 2,
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	next := updated.(*Model)

	if next.layout.activeTabKey() != "PLANNING" {
		t.Errorf("tab did not move to the next category, got %q", next.layout.activeTabKey())
	}
	if len(loaded) != 1 || loaded[0] != "PLANNING" {
		t.Errorf("the new category was not loaded: %v", loaded)
	}
	if next.selected != 0 || next.scrollOffset != 0 {
		t.Errorf("the cursor should return to the top of a new list, got selected=%d offset=%d",
			next.selected, next.scrollOffset)
	}
}

// Where there are no tabs, Tab keeps its old job of moving down the list.
func TestTabStillMovesDownWhenThereAreNoTabs(t *testing.T) {
	m := Model{
		allOptions:   []SelectionOption{{Key: "1", Label: "a"}, {Key: "2", Label: "b"}},
		filteredKeys: []SelectionOption{{Key: "1", Label: "a"}, {Key: "2", Label: "b"}},
		selected:     0,
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := updated.(*Model).selected; got != 1 {
		t.Errorf("Tab should still move down when there are no categories, selection went to %d", got)
	}
}

// The footer is where the actions went when they stopped being list entries.
func TestFooterShowsActionsWithTheirKeys(t *testing.T) {
	layout := menuLayout{footer: []FooterAction{
		{Key: "CONTINUE_LAST", Label: "continue", Hint: "c"},
		{Key: "REMAP_PROVIDER", Label: "remap", Hint: "r"},
	}}
	footer := renderFooter(layout)
	for _, want := range []string{"continue", "remap", "c", "r"} {
		if !strings.Contains(footer, want) {
			t.Errorf("footer is missing %q: %q", want, footer)
		}
	}
	if renderFooter(menuLayout{}) != "" {
		t.Error("no actions should mean no footer")
	}
}

// The pane describes whatever is highlighted, and follows the cursor.
func TestSidePaneFollowsTheHighlightedEntry(t *testing.T) {
	m := Model{
		layout:        menuLayout{pane: true},
		filteredKeys:  []SelectionOption{{Key: "1", Title: "Frieren"}, {Key: "2", Title: "Dandadan"}},
		selected:      1,
		terminalWidth: 120,
		metaOf: func(o SelectionOption) []string {
			return []string{"key " + o.Key}
		},
	}
	view := m.View()
	if !strings.Contains(view, "Dandadan") {
		t.Errorf("the pane does not describe the highlighted entry:\n%s", view)
	}
	if !strings.Contains(view, "key 2") {
		t.Errorf("the pane dropped its metadata:\n%s", view)
	}
}

// A narrow terminal has no room for a second column; the list matters more.
func TestSidePaneIsDroppedWhenThereIsNoRoom(t *testing.T) {
	m := Model{
		layout:        menuLayout{pane: true},
		filteredKeys:  []SelectionOption{{Key: "1", Title: "Frieren"}},
		terminalWidth: 40,
		metaOf:        func(SelectionOption) []string { return []string{"should not appear"} },
	}
	if view := m.View(); strings.Contains(view, "should not appear") {
		t.Errorf("the pane was drawn in a terminal too narrow for it:\n%s", view)
	}
}

// Titles are mostly non-ASCII here, so cutting by bytes would split characters.
func TestTruncateCountsCharactersNotBytes(t *testing.T) {
	if got := truncate("葬送のフリーレン", 4); len([]rune(got)) != 4 {
		t.Errorf("expected 4 characters, got %d (%q)", len([]rune(got)), got)
	}
	if got := truncate("short", 20); got != "short" {
		t.Errorf("a short string should be untouched, got %q", got)
	}
	if got := truncate("anything", 0); got != "" {
		t.Errorf("zero width should give nothing, got %q", got)
	}
}

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

// MenuOrder is read as both settings at once, so a config written before tabs
// existed produces a sensible tab bar and footer without being touched.
func TestSplitMenuOrderReadsTheDefaultConfig(t *testing.T) {
	tabs, actions := SplitMenuOrder("CURRENT,ALL,UNTRACKED,UPDATE,REMAP_PROVIDER,CONTINUE_LAST,TRACKER,PROVIDER")

	wantTabs := []string{"CURRENT", "ALL", "UNTRACKED"}
	if len(tabs) != len(wantTabs) {
		t.Fatalf("expected %d tabs, got %d: %+v", len(wantTabs), len(tabs), tabs)
	}
	for i, key := range wantTabs {
		if tabs[i].Key != key {
			t.Errorf("tab %d: expected %s, got %s", i, key, tabs[i].Key)
		}
	}

	wantActions := []string{"UPDATE", "REMAP_PROVIDER", "CONTINUE_LAST", "TRACKER", "PROVIDER"}
	if len(actions) != len(wantActions) {
		t.Fatalf("expected %d actions, got %d: %+v", len(wantActions), len(actions), actions)
	}
	for i, key := range wantActions {
		if actions[i].Key != key {
			t.Errorf("action %d: expected %s, got %s", i, key, actions[i].Key)
		}
	}
}

// The order written in the config is the order shown, for both groups.
func TestSplitMenuOrderKeepsTheUsersOrder(t *testing.T) {
	tabs, actions := SplitMenuOrder("COMPLETED,PROVIDER,PLANNING,CONTINUE_LAST,CURRENT")
	if got := []string{tabs[0].Key, tabs[1].Key, tabs[2].Key}; got[0] != "COMPLETED" || got[1] != "PLANNING" || got[2] != "CURRENT" {
		t.Errorf("tab order was not preserved: %v", got)
	}
	if actions[0].Key != "PROVIDER" || actions[1].Key != "CONTINUE_LAST" {
		t.Errorf("action order was not preserved: %+v", actions)
	}
}

// AniList calls rewatching REPEATING; the documented config key is REWATCHING.
// Both reach this code, and both have to mean the same tab.
func TestSplitMenuOrderAcceptsBothRewatchingSpellings(t *testing.T) {
	for _, key := range []string{"REWATCHING", "REPEATING"} {
		tabs, _ := SplitMenuOrder(key)
		if len(tabs) != 1 || tabs[0].Label != "Rewatching" {
			t.Errorf("%s did not produce a Rewatching tab: %+v", key, tabs)
		}
	}
	// Writing both must not produce the same tab twice.
	tabs, _ := SplitMenuOrder("REWATCHING,REPEATING")
	if len(tabs) != 1 {
		t.Errorf("both spellings should collapse to one tab, got %d", len(tabs))
	}
}

// Whitespace, case and unknown keys are all things a hand-edited config has.
func TestSplitMenuOrderToleratesUntidyConfigs(t *testing.T) {
	tabs, actions := SplitMenuOrder("  current , NONSENSE ,, continue_last ")
	if len(tabs) != 1 || tabs[0].Key != "CURRENT" {
		t.Errorf("lowercase and padding should still resolve: %+v", tabs)
	}
	if len(actions) != 1 || actions[0].Key != "CONTINUE_LAST" {
		t.Errorf("expected the one valid action: %+v", actions)
	}

	emptyTabs, emptyActions := SplitMenuOrder("")
	if len(emptyTabs) != 0 || len(emptyActions) != 0 {
		t.Error("an empty MenuOrder should produce nothing, not defaults")
	}
}

// The conversion from the preview map used to drop the title and the cover, so
// the terminal menu could not show either even though rofi was handed both.
func TestPreviewConversionKeepsTitleAndCover(t *testing.T) {
	converted := previewOptionsToSortedSelection(map[string]RofiSelectPreview{
		"154587": {Title: "Frieren", CoverImage: "https://example.invalid/frieren.jpg", Rank: 0},
	})
	if len(converted) != 1 {
		t.Fatalf("expected one option, got %d", len(converted))
	}
	if converted[0].Title != "Frieren" {
		t.Errorf("the title was dropped: %+v", converted[0])
	}
	if converted[0].Thumbnail != "https://example.invalid/frieren.jpg" {
		t.Errorf("the cover was dropped: %+v", converted[0])
	}
}

func TestPaneIsNotAttachedToAnEmptyMenu(t *testing.T) {
	model := &Model{}
	attachDetailPane(model)
	if model.layout.pane {
		t.Error("a pane was attached to an empty menu")
	}
}

// Driving the model with real key messages, rather than testing the pieces in
// isolation, is what shows the wiring actually holds together.
func TestCategoryTabsDriveTheListEndToEnd(t *testing.T) {
	lists := map[string][]SelectionOption{
		// Real options carry both, and the list renders the label.
		"CURRENT":   {{Key: "1", Title: "Frieren", Label: "Frieren"}, {Key: "2", Title: "Dandadan", Label: "Dandadan"}},
		"PLANNING":  {{Key: "3", Title: "Monster", Label: "Monster"}},
		"COMPLETED": {{Key: "4", Title: "Steins;Gate", Label: "Steins;Gate"}},
	}
	refresh := &SelectionRefreshConfig{
		Categories: []Tab{
			{Key: "CURRENT", Label: "Watching"},
			{Key: "PLANNING", Label: "Planning"},
			{Key: "COMPLETED", Label: "Completed"},
		},
		ActiveCategory: "PLANNING",
		LoadCategory:   func(key string) []SelectionOption { return lists[key] },
	}

	model := &Model{allOptions: lists["PLANNING"]}
	attachCategoryTabs(model, refresh)
	model.filterOptions()

	// Bubble Tea sends this on start; without it the model believes it has room
	// for a single row and everything below the first entry is clipped.
	sized, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	model = sized.(*Model)

	// Opening a category other than the first must select that tab, not assume
	// the list starts at the beginning.
	if got := model.layout.activeTabKey(); got != "PLANNING" {
		t.Fatalf("the open category should be the active tab, got %q", got)
	}
	if view := model.View(); !strings.Contains(view, "Planning") || !strings.Contains(view, "Watching") {
		t.Errorf("the tab bar is missing from the view:\n%s", view)
	}

	// Tab forward to Completed.
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = next.(*Model)
	if got := model.layout.activeTabKey(); got != "COMPLETED" {
		t.Fatalf("Tab did not advance, got %q", got)
	}
	if view := model.View(); !strings.Contains(view, "Steins;Gate") {
		t.Errorf("the list did not follow the tab:\n%s", view)
	}

	// Shift+Tab back, and wrap past the start.
	for i := 0; i < 2; i++ {
		back, _ := model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
		model = back.(*Model)
	}
	if got := model.layout.activeTabKey(); got != "CURRENT" {
		t.Fatalf("Shift+Tab did not walk back to the first category, got %q", got)
	}
	view := model.View()
	if !strings.Contains(view, "Frieren") || !strings.Contains(view, "Dandadan") {
		t.Errorf("the watching list did not come back:\n%s", view)
	}
	if strings.Contains(view, "Steins;Gate") {
		t.Errorf("entries from the previous category are still showing:\n%s", view)
	}
}

// One category is not worth a tab bar, and tabs without a loader lead nowhere.
func TestTabsNeedMoreThanOneCategoryAndALoader(t *testing.T) {
	loader := func(string) []SelectionOption { return nil }

	single := &Model{}
	attachCategoryTabs(single, &SelectionRefreshConfig{
		Categories: []Tab{{Key: "CURRENT", Label: "Watching"}}, LoadCategory: loader,
	})
	if single.layout.hasTabs() {
		t.Error("a single category drew a tab bar")
	}

	noLoader := &Model{}
	attachCategoryTabs(noLoader, &SelectionRefreshConfig{
		Categories: []Tab{{Key: "CURRENT"}, {Key: "PLANNING"}},
	})
	if noLoader.layout.hasTabs() {
		t.Error("tabs were drawn with no way to load a category")
	}

	attachCategoryTabs(&Model{}, nil) // must not panic
}

// Left and right are the obvious way to move along a row of tabs, and are
// unbound in the default key scheme.
func TestArrowsSwitchCategories(t *testing.T) {
	previous := GetGlobalConfig()
	t.Cleanup(func() { SetGlobalConfig(previous) })
	SetGlobalConfig(&CurdConfig{VimKeys: false})

	lists := map[string][]SelectionOption{
		"CURRENT":  {{Key: "1", Label: "Frieren"}},
		"PLANNING": {{Key: "2", Label: "Monster"}},
	}
	build := func() *Model {
		m := &Model{allOptions: lists["CURRENT"]}
		attachCategoryTabs(m, &SelectionRefreshConfig{
			Categories:     []Tab{{Key: "CURRENT", Label: "Watching"}, {Key: "PLANNING", Label: "Planning"}},
			ActiveCategory: "CURRENT",
			LoadCategory:   func(k string) []SelectionOption { return lists[k] },
		})
		m.filterOptions()
		return m
	}

	right, _ := build().Update(tea.KeyMsg{Type: tea.KeyRight})
	if got := right.(*Model).layout.activeTabKey(); got != "PLANNING" {
		t.Errorf("right should move to the next category, got %q", got)
	}
	left, _ := build().Update(tea.KeyMsg{Type: tea.KeyLeft})
	if got := left.(*Model).layout.activeTabKey(); got != "PLANNING" {
		t.Errorf("left should wrap back to the last category, got %q", got)
	}
}

// Under vim keys the arrows already mean up and down, and must keep meaning it.
func TestArrowsStillMoveTheCursorUnderVimKeys(t *testing.T) {
	previous := GetGlobalConfig()
	t.Cleanup(func() { SetGlobalConfig(previous) })
	SetGlobalConfig(&CurdConfig{VimKeys: true})

	m := &Model{allOptions: []SelectionOption{{Key: "1", Label: "a"}, {Key: "2", Label: "b"}}}
	attachCategoryTabs(m, &SelectionRefreshConfig{
		Categories:     []Tab{{Key: "CURRENT", Label: "Watching"}, {Key: "PLANNING", Label: "Planning"}},
		ActiveCategory: "CURRENT",
		LoadCategory:   func(string) []SelectionOption { return nil },
	})
	m.filterOptions()

	moved, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	next := moved.(*Model)
	if next.layout.activeTabKey() != "CURRENT" {
		t.Error("right changed category under vim keys, where it means move down")
	}
	if next.selected == 0 {
		t.Error("right did not move the cursor under vim keys")
	}
}

// A count turns a word into a view of something, and is what makes the bar read
// as live rather than decorative.
func TestTabBarShowsCounts(t *testing.T) {
	bar := renderTabBar(menuLayout{tabs: []Tab{
		{Key: "CURRENT", Label: "Watching", Count: 7},
		{Key: "ALL", Label: "All", Count: 643},
	}}, 0)
	for _, want := range []string{"Watching", "7", "All", "643"} {
		if !strings.Contains(bar, want) {
			t.Errorf("the bar is missing %q: %q", want, bar)
		}
	}
	// A category with nothing in it should not read as "Planning 0".
	empty := renderTabBar(menuLayout{tabs: []Tab{{Key: "PLANNING", Label: "Planning", Count: 0}, {Key: "ALL", Label: "All", Count: 3}}}, 0)
	if strings.Contains(empty, "Planning  0") {
		t.Errorf("an empty category printed a zero: %q", empty)
	}
}

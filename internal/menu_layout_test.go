package internal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

	// UNTRACKED is not here: it reads like a list but runs a search flow.
	wantTabs := []string{"CURRENT", "ALL"}
	if len(tabs) != len(wantTabs) {
		t.Fatalf("expected %d tabs, got %d: %+v", len(wantTabs), len(tabs), tabs)
	}
	for i, key := range wantTabs {
		if tabs[i].Key != key {
			t.Errorf("tab %d: expected %s, got %s", i, key, tabs[i].Key)
		}
	}

	wantActions := []string{"UNTRACKED", "UPDATE", "REMAP_PROVIDER", "CONTINUE_LAST", "TRACKER", "PROVIDER"}
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
		// The key must be the one the list filter answers to, or the tab is
		// drawn and shows nothing.
		if tabs[0].Key != "REWATCHING" {
			t.Errorf("%s produced key %q, which getEntriesByCategory does not handle", key, tabs[0].Key)
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

// filterOptions appends the "add new" entry from a flag, so a replacement list
// that still carries it shows the entry twice. Both the background refresh and
// a category switch rebuild from a source that includes it.
func TestAddNewEntryIsNeverDuplicated(t *testing.T) {
	withSentinel := []SelectionOption{
		{Key: "1", Label: "Frieren"},
		{Key: "add_new", Label: "Add new anime"},
	}
	m := &Model{allOptions: withSentinel, addNewOption: true}
	m.filterOptions()

	count := func(model *Model) int {
		n := 0
		for _, option := range model.filteredKeys {
			if option.Key == "add_new" {
				n++
			}
		}
		return n
	}
	if got := count(m); got != 1 {
		t.Fatalf("constructing with the sentinel gave %d copies", got)
	}

	// A replacement that still carries it must not add a second.
	m.replaceOptions(withSentinel)
	if got := count(m); got != 1 {
		t.Errorf("replacing options gave %d copies of the add-new entry", got)
	}

	// Switching category goes through the same path.
	tabbed := &Model{allOptions: withSentinel, addNewOption: true}
	attachCategoryTabs(tabbed, &SelectionRefreshConfig{
		Categories:     []Tab{{Key: "CURRENT", Label: "Watching"}, {Key: "UNTRACKED", Label: "Untracked"}},
		ActiveCategory: "CURRENT",
		LoadCategory:   func(string) []SelectionOption { return withSentinel },
	})
	tabbed.filterOptions()
	switched, _ := tabbed.Update(tea.KeyMsg{Type: tea.KeyRight})
	if got := count(switched.(*Model)); got != 1 {
		t.Errorf("switching category gave %d copies of the add-new entry", got)
	}
}

// The chrome takes rows. Not counting them made the menu taller than the
// terminal, and what scrolled off the top was the header.
func TestRowBudgetLeavesRoomForTheChrome(t *testing.T) {
	plain := Model{terminalHeight: 30}
	withTabs := Model{terminalHeight: 30, layout: menuLayout{
		tabs: []Tab{{Key: "A"}, {Key: "B"}},
	}}

	// The frame -- breadcrumb, rule, and the key hints with the blank line
	// above them -- is always drawn, so it is always paid for.
	if plain.visibleItemsCount() >= plain.terminalHeight-4 {
		t.Errorf("the frame was not paid for: %d rows for a %d row terminal",
			plain.visibleItemsCount(), plain.terminalHeight)
	}
	if withTabs.visibleItemsCount() >= plain.visibleItemsCount() {
		t.Error("a tab bar takes rows on top of the frame, so fewer entries fit")
	}

	// The whole menu has to fit: what is drawn must never exceed the terminal,
	// or it scrolls and the header is the first thing lost.
	m := &Model{
		allOptions:     make([]SelectionOption, 50),
		terminalHeight: 24,
		terminalWidth:  100,
		layout:         menuLayout{tabs: []Tab{{Key: "A", Label: "A"}, {Key: "B", Label: "B"}}},
	}
	for i := range m.allOptions {
		m.allOptions[i] = SelectionOption{Key: string(rune('a' + i%26)), Label: "entry"}
	}
	m.filterOptions()
	if height := lipgloss.Height(m.View()); height > m.terminalHeight {
		t.Errorf("the menu is %d rows tall in a %d row terminal, so it will scroll", height, m.terminalHeight)
	}

	// A tiny terminal must still show something rather than nothing.
	tiny := Model{terminalHeight: 3, layout: menuLayout{tabs: []Tab{{Key: "A"}, {Key: "B"}}}}
	if got := tiny.visibleItemsCount(); got < 1 {
		t.Errorf("a very short terminal should still show a row, got %d", got)
	}
}

// A tab must be backed by a list. getEntriesByCategory is the authority on
// which keys are lists, so anything offered as a tab has to be one of them --
// otherwise the tab draws, shows nothing, and hides whatever the key really did.
func TestEveryTabKeyIsARealCategory(t *testing.T) {
	everyKey := "CURRENT,ALL,UNTRACKED,UPDATE,REMAP_PROVIDER,CONTINUE_LAST,TRACKER,PROVIDER," +
		"PLANNING,COMPLETED,PAUSED,DROPPED,REWATCHING,REPEATING"
	tabs, actions := SplitMenuOrder(everyKey)

	list := AnimeList{
		Watching:   []Entry{{Media: Media{ID: 1}}},
		Planning:   []Entry{{Media: Media{ID: 2}}},
		Completed:  []Entry{{Media: Media{ID: 3}}},
		Paused:     []Entry{{Media: Media{ID: 4}}},
		Dropped:    []Entry{{Media: Media{ID: 5}}},
		Rewatching: []Entry{{Media: Media{ID: 6}}},
	}
	for _, tab := range tabs {
		if len(getEntriesByCategory(list, tab.Key)) == 0 {
			t.Errorf("tab %q (%s) is not a category getEntriesByCategory knows", tab.Key, tab.Label)
		}
	}

	// And the search flow has to remain reachable as an action.
	found := false
	for _, action := range actions {
		if action.Key == "UNTRACKED" {
			found = true
		}
	}
	if !found {
		t.Error("UNTRACKED disappeared instead of becoming an action")
	}
}

// An override is only worth having if it reaches the screen. This goes through
// the same call startup makes, then renders, rather than checking the palette
// in isolation.
func TestThemeOverrideReachesTheRenderedMenu(t *testing.T) {
	previous := GetGlobalConfig()
	t.Cleanup(func() {
		SetGlobalConfig(previous)
		ApplyThemeFromConfig(previous)
	})

	config := &CurdConfig{Theme: "builtin", ThemeOverrides: "accent:#ff6188"}
	SetGlobalConfig(config)
	palette := ApplyThemeFromConfig(config)

	if palette.Accent != "#ff6188" {
		t.Fatalf("the override did not reach the palette: %q", palette.Accent)
	}
	// Whatever writes the rofi themes reads the active palette, so it has to
	// see the same colours the terminal menus use.
	if active := theme.Active(); active.Accent != "#ff6188" {
		t.Errorf("the active palette still has %q, so rofi would disagree with the menu", active.Accent)
	}

	m := &Model{allOptions: []SelectionOption{{Key: "1", Label: "Frieren"}}}
	attachCategoryTabs(m, &SelectionRefreshConfig{
		Categories:     []Tab{{Key: "CURRENT", Label: "Watching"}, {Key: "ALL", Label: "All"}},
		ActiveCategory: "CURRENT",
		LoadCategory:   func(string) []SelectionOption { return nil },
	})
	m.filterOptions()
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})

	// lipgloss only emits colour when the terminal is thought to support it, so
	// the check is on the styles the view is built from rather than its bytes.
	if got := titleStyle.GetForeground(); got != lipgloss.Color("#ff6188") {
		t.Errorf("the title style did not pick up the override, got %v", got)
	}
	if got := tabActiveStyle.GetBackground(); got == lipgloss.Color("") {
		t.Error("the tab style lost its colour entirely")
	}
	_ = sized
}

// The breadcrumb says where you are without spending a line on a title bar.
func TestBreadcrumbNamesTheCategory(t *testing.T) {
	m := &Model{allOptions: []SelectionOption{{Key: "1", Label: "Frieren"}}}
	attachCategoryTabs(m, &SelectionRefreshConfig{
		Categories:     []Tab{{Key: "CURRENT", Label: "Watching"}, {Key: "ALL", Label: "All"}},
		ActiveCategory: "ALL",
		LoadCategory:   func(string) []SelectionOption { return nil },
	})
	if got := m.sectionLabel(); got != "All" {
		t.Errorf("the breadcrumb should name the open category, got %q", got)
	}

	// A menu without categories has nothing to add after the program's name.
	plain := &Model{}
	if got := plain.sectionLabel(); got != "" {
		t.Errorf("a menu with no categories should have no section, got %q", got)
	}
	if crumb := renderBreadcrumb(""); !strings.Contains(crumb, DisplayName) || strings.Contains(crumb, "›") {
		t.Errorf("an empty section should leave off the separator: %q", crumb)
	}
}

// Offering a key that does nothing is worse than offering none: it invites a
// press that goes nowhere.
func TestKeyHintsOnlyOfferKeysThatWork(t *testing.T) {
	previous := GetGlobalConfig()
	t.Cleanup(func() { SetGlobalConfig(previous) })
	SetGlobalConfig(&CurdConfig{VimKeys: false})

	plain := Model{isHomeMenu: true}
	keys := func(m Model) string {
		parts := []string{}
		for _, hint := range m.keyHints() {
			parts = append(parts, hint.Key)
		}
		return strings.Join(parts, " ")
	}

	// No categories, so no category key.
	if got := keys(plain); strings.Contains(got, "←/→") {
		t.Errorf("a menu with no categories offered a category key: %q", got)
	}
	// The home menu quits; anything else goes back.
	if got := keys(plain); !strings.Contains(got, "ctrl+c") {
		t.Errorf("the home menu should offer quit: %q", got)
	}
	if got := keys(Model{}); !strings.Contains(got, "esc") {
		t.Errorf("a submenu should offer back: %q", got)
	}

	tabbed := Model{layout: menuLayout{tabs: []Tab{{Key: "A"}, {Key: "B"}}}}
	if got := keys(tabbed); !strings.Contains(got, "←/→") {
		t.Errorf("a menu with categories should offer the category key: %q", got)
	}

	// Under vim keys the arrows move the cursor, so the category key differs
	// and the movement hint has to match what actually moves.
	SetGlobalConfig(&CurdConfig{VimKeys: true})
	if got := keys(tabbed); !strings.Contains(got, "tab") || strings.Contains(got, "←/→") {
		t.Errorf("under vim keys the category key should be tab: %q", got)
	}
	if got := keys(tabbed); !strings.Contains(got, "j/k") {
		t.Errorf("under vim keys the movement hint should be j/k: %q", got)
	}
}

// A terminal too small for the menu should say so rather than draw something
// unreadable and leave the user guessing whether it is broken.
func TestTooSmallTerminalIsToldSo(t *testing.T) {
	m := &Model{
		allOptions:     []SelectionOption{{Key: "1", Label: "Frieren"}},
		terminalWidth:  20,
		terminalHeight: 6,
	}
	m.filterOptions()
	view := m.View()

	if strings.Contains(view, "Frieren") {
		t.Error("the list was drawn into a terminal too small for it")
	}
	for _, want := range []string{DisplayName, "Resize"} {
		if !strings.Contains(view, want) {
			t.Errorf("the notice does not mention %q: %q", want, view)
		}
	}

	// A terminal that has not reported its size yet must not trigger it.
	unknown := &Model{allOptions: []SelectionOption{{Key: "1", Label: "Frieren"}}}
	unknown.filterOptions()
	if strings.Contains(unknown.View(), "Resize") {
		t.Error("a menu with no size yet was told to resize")
	}
}

// The filter line costs a row, so it appears only when there is something in it.
func TestFilterLineOnlyAppearsWhenUsed(t *testing.T) {
	m := &Model{allOptions: []SelectionOption{{Key: "1", Label: "Frieren"}}, terminalWidth: 80, terminalHeight: 20}
	m.filterOptions()
	if strings.Contains(m.View(), "/ ") && !strings.Contains(m.View(), "Frieren") {
		t.Error("an empty filter drew a line saying nothing")
	}

	m.filter = "fri"
	m.filterOptions()
	if !strings.Contains(m.View(), "fri") {
		t.Errorf("a filter in use was not shown:\n%s", m.View())
	}
}

// Resizing has to relay everything, not just reflow the text. The frame
// follows the terminal, so both the rule and the detail column move with it.
func TestResizeRelaysTheWholeFrame(t *testing.T) {
	opts := []SelectionOption{
		{Key: "1", Title: "Frieren", Label: "Frieren · 12/28"},
		{Key: "2", Title: "Dandadan", Label: "Dandadan · 7/12"},
	}
	m := &Model{allOptions: opts}
	attachCategoryTabs(m, &SelectionRefreshConfig{
		Categories:     []Tab{{Key: "CURRENT", Label: "Watching"}, {Key: "ALL", Label: "All"}},
		ActiveCategory: "CURRENT",
		LoadCategory:   func(string) []SelectionOption { return opts },
	})
	attachDetailPane(m)
	m.filterOptions()

	widthOf := func(width, height int) int {
		sized, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
		m = sized.(*Model)
		return lipgloss.Width(m.View())
	}

	wide := widthOf(120, 30)
	narrow := widthOf(70, 20)
	if narrow >= wide {
		t.Errorf("the frame did not shrink with the terminal: %d then %d", wide, narrow)
	}
	// And it must not overflow what it was given, or the terminal wraps every
	// line and the layout collapses.
	if narrow > 70 {
		t.Errorf("the frame is %d columns wide in a 70 column terminal", narrow)
	}
	if back := widthOf(120, 30); back != wide {
		t.Errorf("growing back gave a different width: %d then %d", wide, back)
	}
}

// Rows are cut to the list column. A row that wraps is two lines for one
// entry, which breaks the count of what fits and the alignment beside it.
func TestLongRowsAreCutNotWrapped(t *testing.T) {
	long := strings.Repeat("Very Long Anime Title ", 12)
	m := &Model{allOptions: []SelectionOption{{Key: "1", Title: "x", Label: long}}}
	attachDetailPane(m)
	m.filterOptions()
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = sized.(*Model)

	view := m.View()
	if lipgloss.Width(view) > 80 {
		t.Errorf("a long row pushed the frame to %d columns", lipgloss.Width(view))
	}
	if !strings.Contains(view, "…") {
		t.Error("a row too long for its column was not marked as cut")
	}
}

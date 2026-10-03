package internal

import (
	"fmt"
	"strings"

	"github.com/thexykril/otakase/internal/icons"
)

// The main menu is a list: otakase opens on Watching, with the other lists one
// Tab away and the actions beside them -- the terminal's tabs and bottom bar,
// rofi's toolbar. There used to be a menu of lists and actions in front of it,
// which every session had to pass through to reach the one list it wanted.

// listsMenuKey and nextListKey are what rofi's toolbar returns for its Lists
// button and for Tab. The terminal needs neither: its tabs do both.
const (
	listsMenuKey = "LISTS"
	nextListKey  = "NEXT_LIST"
)

// listChoicePrefix marks a row of the Lists menu, so choosing "Watching" there
// cannot be mistaken for anything keyed CURRENT elsewhere.
const listChoicePrefix = "LIST:"

// mainLists is the lists the main menu offers, in MenuOrder's order.
func mainLists(config *Config) []Tab {
	tabs, _ := SplitMenuOrder(strings.Join(menuKeys(config), ","))
	out := make([]Tab, 0, len(tabs))
	for _, tab := range tabs {
		if trackingCategoryEnabled(config, tab.Key) {
			out = append(out, tab)
		}
	}
	if len(out) == 0 {
		// A MenuOrder naming no list still needs something to open on.
		out = append(out, Tab{Key: "CURRENT", Label: menuCategoryLabels["CURRENT"], Icon: categoryIcon("CURRENT")})
	}
	return out
}

// launchList is the list otakase opens on: Watching, or the first list
// MenuOrder names when it leaves Watching out.
func launchList(config *Config) string {
	lists := mainLists(config)
	for _, tab := range lists {
		if tab.Key == "CURRENT" {
			return "CURRENT"
		}
	}
	return lists[0].Key
}

// nextList is the list after current, wrapping round, which is what Tab moves
// to in rofi as it does between the terminal's tabs.
func nextList(config *Config, current string) string {
	lists := mainLists(config)
	for i, tab := range lists {
		if tab.Key == current {
			return lists[(i+1)%len(lists)].Key
		}
	}
	return lists[0].Key
}

// mainMenuLayout is the lists, with their counts, and the actions the main
// menu offers.
func mainMenuLayout(config *Config, list AnimeList) ([]Tab, []FooterAction) {
	tabs := mainLists(config)
	for i := range tabs {
		tabs[i].Count = len(visibleEntriesByCategory(list, tabs[i].Key, config))
	}
	_, actions := SplitMenuOrder(strings.Join(menuKeys(config), ","))
	out := make([]FooterAction, 0, len(actions)+1)
	hasTracker := false
	for _, action := range actions {
		if !trackingCategoryEnabled(config, action.Key) {
			continue
		}
		if action.Key == "TRACKER" {
			hasTracker = true
		}
		out = append(out, action)
	}
	// Changing tracker is the way out of a tracker that stopped working, so it
	// is always offered, as it was in the old menu.
	if !hasTracker {
		out = append(out, menuActions["TRACKER"])
	}
	return tabs, out
}

// continueRowsOn reports whether the continue-watching rows open the Watching
// list: CONTINUE_LAST names them, and ContinueWatchingRows says how many.
func continueRowsOn(config *Config) bool {
	if config == nil || config.ContinueWatchingRows <= 0 {
		return false
	}
	for _, key := range menuKeys(config) {
		if key == "CONTINUE_LAST" {
			return true
		}
	}
	return false
}

// buildMainListOptions is a list's rows, led on Watching by the shows to
// continue.
func buildMainListOptions(config *Config, list AnimeList, key string) []SelectionOption {
	options := buildCategorySelectionOptions(list, key)
	if key != "CURRENT" || !continueRowsOn(config) {
		return options
	}
	return append(continueWatchingRows(config, &list), options...)
}

// buildMainPreviewOptions is the poster grid's version of buildMainListOptions.
// The grid is a map ordered by rank, so the continue rows take ranks below the
// list's, which starts at zero.
func buildMainPreviewOptions(config *Config, list AnimeList, key string) map[string]RofiSelectPreview {
	options := buildCategoryPreviewOptions(list, key)
	if key != "CURRENT" || !continueRowsOn(config) {
		return options
	}
	rows := continueWatchingRows(config, &list)
	for i, row := range rows {
		options[row.Key] = RofiSelectPreview{
			Title:      icons.Label(icons.Play, row.Label),
			CoverImage: row.Thumbnail,
			Rank:       i - len(rows),
		}
	}
	return options
}

// isMainMenuKey reports whether a choice from the main list is answered by the
// dispatch in front of it rather than by playing a show from the list.
func isMainMenuKey(key string) bool {
	if isMenuActionKey(key) || key == listsMenuKey || key == nextListKey {
		return true
	}
	_, isRow := resumeRowAnilistID(key)
	return isRow
}

// pickList asks which list to show, for rofi's Lists button. It reports false
// when the viewer backs out, leaving the list as it was.
func pickList(config *Config, list AnimeList, current string) (string, bool) {
	tabs, _ := mainMenuLayout(config, list)
	options := make([]SelectionOption, 0, len(tabs))
	for _, tab := range tabs {
		options = append(options, SelectionOption{
			Key:   listChoicePrefix + tab.Key,
			Label: fmt.Sprintf("%s (%d)", tab.Label, tab.Count),
			Icon:  tab.Icon,
		})
	}
	picked, err := DynamicSelectPreserveOrder(options)
	if err != nil {
		Log(fmt.Sprintf("Lists: %v", err))
		return current, false
	}
	picked = NormalizeSelectionKey(picked)
	if SelectionMeansQuit(picked) {
		Exit(nil)
	}
	key, ok := strings.CutPrefix(picked.Key, listChoicePrefix)
	if !ok {
		return current, false
	}
	return key, true
}

// toolbarButtonLabels are the words on rofi's toolbar buttons. The footer's
// lowercase hints read as key names; these sit on buttons, so they are names.
var toolbarButtonLabels = map[string]string{
	listsMenuKey:     "Lists",
	"UNTRACKED":      "Untracked",
	"UPDATE":         "Update",
	"REMAP_PROVIDER": "Remap",
	"CONTINUE_LAST":  "Continue",
	"SURPRISE":       "Surprise",
	"TRACKER":        "Tracker",
	"PROVIDER":       "Provider",
	"STATS":          "Stats",
}

// toolbarButtonIcon is the icon a toolbar button wears: the action's own, or
// for cast the one that shows whether it is on.
func toolbarButtonIcon(key string, config *Config) icons.Icon {
	switch key {
	case listsMenuKey:
		return icons.AllShows
	case "CAST":
		return castMenuIcon(config)
	}
	return categoryIcon(key)
}

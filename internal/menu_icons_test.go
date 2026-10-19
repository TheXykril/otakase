package internal

import (
	"strings"
	"testing"

	"github.com/thexykril/otakase/internal/icons"
)

// An icon is drawn in front of the row but is not part of the label, so the
// row rofi hands back still matches its option, and Back and Quit still read
// as Back and Quit.
func TestRofiRowsWithIconsMatchBackToTheirOptions(t *testing.T) {
	icons.Resolve(true)
	t.Cleanup(func() { icons.Resolve(false) })

	options := []SelectionOption{
		{Key: "CURRENT", Label: "Currently Watching", Icon: icons.Watching},
		{Key: "plain", Label: "No icon here"},
	}
	rows := strings.Split(buildRofiOptionsString(options, false), "\n")
	if len(rows) != 4 {
		t.Fatalf("rows = %q", rows)
	}
	if !strings.HasPrefix(rows[0], string(rune(icons.Watching))) {
		t.Errorf("first row has no icon: %q", rows[0])
	}
	if rows[1] != "No icon here" {
		t.Errorf("a row without an icon gained one: %q", rows[1])
	}

	for i, want := range []string{"CURRENT", "plain", "-2", "-1"} {
		got, err := parseRofiSelection(nil, rows[i], options, false)
		if err != nil {
			t.Fatalf("row %q: %v", rows[i], err)
		}
		if got.Key != want {
			t.Errorf("row %q matched %q, want %q", rows[i], got.Key, want)
		}
	}
}

func TestEveryMainMenuEntryHasAnIcon(t *testing.T) {
	for _, key := range []string{"CURRENT", "ALL", "UNTRACKED", "UPDATE", "REMAP_PROVIDER", "CONTINUE_LAST", "SURPRISE",
		"PLANNING", "COMPLETED", "PAUSED", "DROPPED", "REWATCHING", "TRACKER", "PROVIDER", "CAST", "STATS"} {
		if categoryIcon(key) == 0 {
			t.Errorf("%s has no icon", key)
		}
	}
}

func TestIconsOffLeaveRowsPlain(t *testing.T) {
	icons.Resolve(false)
	got := buildRofiOptionsString([]SelectionOption{{Key: "CURRENT", Label: "Currently Watching", Icon: icons.Watching}}, false)
	if got != "Currently Watching\nBack\nQuit" {
		t.Fatalf("got %q", got)
	}
}

// The terminal's category tabs carry the same icons as the main menu entries.
func TestTerminalTabsHaveIcons(t *testing.T) {
	tabs, _ := SplitMenuOrder("CURRENT,PLANNING,COMPLETED")
	for _, tab := range tabs {
		if tab.Icon != categoryIcon(tab.Key) || tab.Icon == 0 {
			t.Errorf("tab %s has icon %U", tab.Key, rune(tab.Icon))
		}
	}

	icons.Resolve(true)
	t.Cleanup(func() { icons.Resolve(false) })
	bar := renderTabBar(menuLayout{tabs: tabs}, 80)
	if !strings.Contains(bar, string(rune(icons.Planning))) {
		t.Errorf("the tab bar has no icons:\n%s", bar)
	}

	icons.Resolve(false)
	if bar := renderTabBar(menuLayout{tabs: tabs}, 80); strings.Contains(bar, string(rune(icons.Planning))) {
		t.Errorf("icons off, but the tab bar still has them:\n%s", bar)
	}
}

// With icons on, the cast button's state shows in its icon, not a checkbox.
func TestCastButtonIconFollowsItsState(t *testing.T) {
	icons.Resolve(true)
	for _, on := range []bool{false, true} {
		config := &Config{CastToDevice: on}
		want := string(rune(castMenuIcon(config)))
		if label := toolbarButtonLabel("CAST", config); !strings.HasPrefix(label, want) {
			t.Errorf("cast on=%v: button %q does not start with %U", on, label, rune(castMenuIcon(config)))
		}
	}
}

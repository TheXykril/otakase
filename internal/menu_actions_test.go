package internal

import (
	"strings"
	"testing"
)

func TestMenuKeysReadsBothSettings(t *testing.T) {
	config := &Config{MenuOrder: " current , all,update", MenuActions: "CAST,UPDATE,,stats"}
	if got := strings.Join(menuKeys(config), ","); got != "CURRENT,ALL,UPDATE,CAST,STATS" {
		t.Fatalf("keys = %s", got)
	}
	if keys := menuKeys(nil); len(keys) != 0 {
		t.Fatalf("nil config: %v", keys)
	}
}

func TestDefaultMenuSplitsListsFromActions(t *testing.T) {
	defaults := defaultConfigMap()
	for _, key := range strings.Split(defaults["MenuOrder"], ",") {
		if isMenuActionKey(key) {
			t.Errorf("MenuOrder default holds action %s", key)
		}
	}
	for _, key := range strings.Split(defaults["MenuActions"], ",") {
		if !isMenuActionKey(key) {
			t.Errorf("MenuActions default holds %s, not an action", key)
		}
	}
}

func TestOrderedCategoriesFollowsBothSettings(t *testing.T) {
	config := &Config{MenuOrder: "ALL", MenuActions: "CAST,CONTINUE_LAST"}
	keys := []string{}
	for _, option := range getOrderedCategories(config) {
		keys = append(keys, option.Key)
	}
	if got := strings.Join(keys, ","); got != "ALL,CAST,CONTINUE_LAST,TRACKER" {
		t.Fatalf("menu = %s", got)
	}
}

func TestSplitMenuActionsReplacesPastDefaults(t *testing.T) {
	defaults := defaultConfigMap()
	for _, past := range []string{
		"CURRENT,ALL,PLANNING,PAUSED,DROPPED,REWATCHING,UNTRACKED,UPDATE,REMAP_PROVIDER,CONTINUE_LAST,SURPRISE,TRACKER,PROVIDER,CAST,STATS",
		"CURRENT,ALL,PLANNING,PAUSED,DROPPED,REWATCHING,UNTRACKED,UPDATE,REMAP_PROVIDER,CONTINUE_LAST,TRACKER,PROVIDER,CAST",
		"CURRENT,ALL,UNTRACKED,UPDATE,REMAP_PROVIDER,CONTINUE_LAST,TRACKER,PROVIDER",
		"CURRENT,ALL,UNTRACKED,UPDATE,REMAP_PROVIDER,CONTINUE_LAST,SURPRISE,TRACKER,PROVIDER,STATS",
		"CURRENT,ALL,UNTRACKED,UPDATE,REMAP_PROVIDER,CONTINUE_LAST,TRACKER,PROVIDER,STATS,SURPRISE",
	} {
		m := map[string]string{"MenuOrder": past}
		if !splitMenuActions(m) {
			t.Fatalf("%s: not migrated", past)
		}
		if m["MenuOrder"] != defaults["MenuOrder"] || m["MenuActions"] != defaults["MenuActions"] {
			t.Errorf("%s: got MenuOrder=%s MenuActions=%s", past, m["MenuOrder"], m["MenuActions"])
		}
	}
}

func TestSplitMenuActionsKeepsAChosenMenu(t *testing.T) {
	m := map[string]string{"MenuOrder": "COMPLETED,CAST,CURRENT,CONTINUE_LAST"}
	if !splitMenuActions(m) {
		t.Fatal("not migrated")
	}
	if m["MenuOrder"] != "COMPLETED,CURRENT" || m["MenuActions"] != "CAST,CONTINUE_LAST" {
		t.Fatalf("got MenuOrder=%s MenuActions=%s", m["MenuOrder"], m["MenuActions"])
	}

	// No actions at all must not read as "the default actions".
	lists := map[string]string{"MenuOrder": "CURRENT,ALL"}
	splitMenuActions(lists)
	if lists["MenuActions"] != "TRACKER" {
		t.Fatalf("MenuActions = %q", lists["MenuActions"])
	}

	// Already split, or never chosen: left alone.
	done := map[string]string{"MenuOrder": "CURRENT,CAST", "MenuActions": ""}
	if splitMenuActions(done) || done["MenuOrder"] != "CURRENT,CAST" {
		t.Fatalf("rewrote a config that has MenuActions: %v", done)
	}
	if splitMenuActions(map[string]string{}) {
		t.Fatal("migrated a config without MenuOrder")
	}
}

// An upgrade from before MenuActions turns a default menu into the new one,
// and splits a chosen one, before MenuActions' default could be appended.
func TestUpgradeSplitsMenuActions(t *testing.T) {
	defaults := defaultConfigMap()
	text := migrateFrom(t, "26.5.0", "26.6.0",
		"MenuOrder=CURRENT,ALL,PLANNING,PAUSED,DROPPED,REWATCHING,UNTRACKED,UPDATE,REMAP_PROVIDER,CONTINUE_LAST,SURPRISE,TRACKER,PROVIDER,CAST,STATS\n")
	for _, want := range []string{"MenuOrder=" + defaults["MenuOrder"] + "\n", "MenuActions=" + defaults["MenuActions"] + "\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q after upgrade:\n%s", want, text)
		}
	}

	// From before SURPRISE and STATS: they are added, then moved with the rest.
	text = migrateFrom(t, "2.2.2", "26.6.0", "MenuOrder=ALL,CAST,CURRENT\n")
	for _, want := range []string{"MenuOrder=ALL,CURRENT\n", "MenuActions=CAST,SURPRISE,STATS\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q after upgrade:\n%s", want, text)
		}
	}
	if strings.Count(text, "MenuActions=") != 1 {
		t.Errorf("MenuActions written twice:\n%s", text)
	}
}

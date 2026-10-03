package internal

import (
	"os/exec"
	"strings"
	"testing"
)

func TestLaunchListIsWatching(t *testing.T) {
	if got := launchList(&Config{MenuOrder: "ALL,CURRENT"}); got != "CURRENT" {
		t.Errorf("launch = %s, want CURRENT", got)
	}
	// Without Watching, the first list named.
	if got := launchList(&Config{MenuOrder: "PLANNING,ALL", TrackingRemote: TrackingRemoteAniList}); got != "PLANNING" {
		t.Errorf("launch = %s, want PLANNING", got)
	}
	// Naming no list still opens somewhere.
	if got := launchList(&Config{MenuOrder: "CAST"}); got != "CURRENT" {
		t.Errorf("launch = %s, want CURRENT", got)
	}
}

func TestNextListWraps(t *testing.T) {
	config := &Config{MenuOrder: "CURRENT,PLANNING,ALL", TrackingRemote: TrackingRemoteAniList}
	for current, want := range map[string]string{"CURRENT": "PLANNING", "PLANNING": "ALL", "ALL": "CURRENT", "DROPPED": "CURRENT"} {
		if got := nextList(config, current); got != want {
			t.Errorf("after %s: %s, want %s", current, got, want)
		}
	}
}

func TestMainMenuKeys(t *testing.T) {
	for _, key := range []string{"CAST", "UPDATE", listsMenuKey, nextListKey, resumeRowPrefix + "21"} {
		if !isMainMenuKey(key) {
			t.Errorf("%s should be dispatched", key)
		}
	}
	for _, key := range []string{"21", "add_new", "CURRENT", ""} {
		if isMainMenuKey(key) {
			t.Errorf("%s should not be dispatched", key)
		}
	}
}

func TestContinueRowsNeedContinueLast(t *testing.T) {
	if !continueRowsOn(&Config{MenuActions: "UPDATE,CONTINUE_LAST", ContinueWatchingRows: 3}) {
		t.Error("CONTINUE_LAST with rows should show them")
	}
	if continueRowsOn(&Config{MenuActions: "UPDATE", ContinueWatchingRows: 3}) {
		t.Error("no CONTINUE_LAST, no rows")
	}
	if continueRowsOn(&Config{MenuActions: "CONTINUE_LAST", ContinueWatchingRows: 0}) {
		t.Error("ContinueWatchingRows=0 hides them")
	}
}

func TestToolbarButtonsAndKeys(t *testing.T) {
	config := &Config{MenuOrder: "CURRENT,ALL", MenuActions: "CONTINUE_LAST,CAST,UPDATE", ContinueWatchingRows: 5, TrackingRemote: TrackingRemoteAniList}
	tabs, actions := mainMenuLayout(config, AnimeList{})
	bar := mainRofiToolbar(config, tabs, actions)

	keys := []string{}
	for _, button := range bar.buttons {
		keys = append(keys, button.Key)
	}
	// Lists first; CONTINUE_LAST is the rows, not a button; TRACKER always.
	if got := strings.Join(keys, ","); got != "LISTS,CAST,UPDATE,TRACKER" {
		t.Fatalf("buttons = %s", got)
	}
	if last := bar.keys[len(bar.keys)-1]; last.Key != nextListKey || last.Binding != "Tab" {
		t.Errorf("Tab should move to the next list, got %+v", last)
	}

	args := strings.Join(bar.args([]string{"inputbar", "box-toolbar", "listview"}), "\n")
	for _, want := range []string{"-kb-custom-2\nControl+k", "-kb-custom-3\nControl+e", "-kb-custom-5\nTab", "action: \"kb-custom-2\";", "action: \"kb-cancel\";", "children: [ inputbar, box-toolbar, listview ];"} {
		if !strings.Contains(args, want) {
			t.Errorf("toolbar args lack %q:\n%s", want, args)
		}
	}
	// Each quoted value on its own line: rofi reads a string to the last quote
	// on its line.
	for _, line := range strings.Split(args, "\n") {
		if strings.Count(line, `"`)-strings.Count(line, `\"`) > 2 {
			t.Errorf("two strings on one rasi line: %q", line)
		}
	}
}

func TestToolbarWithRowsOffKeepsContinue(t *testing.T) {
	config := &Config{MenuOrder: "CURRENT", MenuActions: "CONTINUE_LAST", ContinueWatchingRows: 0}
	tabs, actions := mainMenuLayout(config, AnimeList{})
	bar := mainRofiToolbar(config, tabs, actions)
	if len(bar.buttons) == 0 || bar.buttons[0].Key != "CONTINUE_LAST" {
		t.Fatalf("buttons = %+v", bar.buttons)
	}
	// One list: no Lists button and no Tab.
	for _, key := range bar.keys {
		if key.Key == listsMenuKey || key.Key == nextListKey {
			t.Errorf("one list, but %s is bound", key.Key)
		}
	}
}

func TestToolbarPressedMapsExitCodes(t *testing.T) {
	bar := rofiToolbar{keys: []rofiButton{{Key: "LISTS"}, {Key: "CAST"}, {Key: nextListKey}}}
	exitWith := func(code int) error {
		return exec.Command("sh", "-c", "exit "+string(rune('0'+code/10))+string(rune('0'+code%10))).Run()
	}
	if got, ok := bar.pressed(exitWith(11)); !ok || got.Key != "CAST" {
		t.Errorf("exit 11 = %+v %v, want CAST", got, ok)
	}
	if got, ok := bar.pressed(exitWith(12)); !ok || got.Key != nextListKey {
		t.Errorf("exit 12 = %+v %v, want next list", got, ok)
	}
	for _, code := range []int{1, 13} {
		if _, ok := bar.pressed(exitWith(code)); ok {
			t.Errorf("exit %d should not be a button", code)
		}
	}
	if _, ok := bar.pressed(nil); ok {
		t.Error("a normal pick is not a button")
	}
}

func TestRofiThemeStringEscapes(t *testing.T) {
	if got := rofiThemeString(`Cast: On | "TV" \ x`); got != `Cast: On | \"TV\" \\ x` {
		t.Errorf("escaped = %s", got)
	}
}

package internal

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestClearLogFileStartsAnEmptyLog(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "not-yet", "otakase-debug.log")

	if err := ClearLogFile(logPath); err != nil {
		t.Fatalf("a missing folder should be created: %v", err)
	}
	if err := os.WriteFile(logPath, []byte("the last run"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ClearLogFile(logPath); err != nil {
		t.Fatalf("an existing log should be emptied: %v", err)
	}
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("the log should exist: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("the log kept %d bytes", info.Size())
	}
}

func TestExitErrorWorthShowing(t *testing.T) {
	refused := errors.New("command failed after 3 attempts: dial unix /tmp/mpv.sock: connect: connection refused")
	cases := []struct {
		name   string
		err    error
		socket string
		want   bool
	}{
		{name: "no error", want: false},
		{name: "error without a player", err: errors.New("no links"), want: true},
		{name: "player already gone", err: refused, socket: "/tmp/mpv.sock", want: false},
		{name: "another player's socket", err: refused, socket: "/tmp/other.sock", want: true},
		{name: "player failed otherwise", err: errors.New("broken pipe"), socket: "/tmp/mpv.sock", want: true},
	}
	for _, tc := range cases {
		if got := exitErrorWorthShowing(tc.err, tc.socket); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestStatusLabel(t *testing.T) {
	options := listStatusOptions()
	if got := statusLabel(options, "PAUSED"); got != "On Hold" {
		t.Errorf("PAUSED = %q", got)
	}
	if got := statusLabel(options, "REPEATING"); got != "Rewatching" {
		t.Errorf("REPEATING = %q", got)
	}
	for _, status := range []string{"", "SOMETHING_NEW"} {
		if got := statusLabel(options, status); got != "None" {
			t.Errorf("%q = %q, want None", status, got)
		}
	}
}

func TestGetEntriesByCategoryListsEachShowOnce(t *testing.T) {
	list := AnimeList{
		Watching: []Entry{
			{Media: Media{ID: 1}, Status: "CURRENT"},
			{Media: Media{ID: 1}, Status: "CURRENT"},
		},
		Rewatching: []Entry{{Media: Media{ID: 3}, Status: "REPEATING"}},
		Completed:  []Entry{{Media: Media{ID: 2}, Status: "COMPLETED"}, {Media: Media{ID: 3}, Status: "COMPLETED"}},
	}

	ids := func(entries []Entry) []int {
		var out []int
		for _, entry := range entries {
			out = append(out, entry.Media.ID)
		}
		return out
	}
	if got := ids(getEntriesByCategory(list, "ALL")); len(got) != 3 {
		t.Errorf("ALL should hold three shows once each, got %v", got)
	}
	if got := ids(getEntriesByCategory(list, "CURRENT")); len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Errorf("CURRENT should be Watching then Rewatching, got %v", got)
	}
	if got := ids(getEntriesByCategory(list, "COMPLETED")); len(got) != 2 {
		t.Errorf("COMPLETED = %v", got)
	}
	if got := getEntriesByCategory(list, "NOPE"); got == nil || len(got) != 0 {
		t.Errorf("an unknown list should be empty, got %#v", got)
	}
}

package internal

import (
	"testing"
)

func TestEveryMainMenuEntryHasAnIcon(t *testing.T) {
	for _, key := range []string{"CURRENT", "ALL", "UNTRACKED", "UPDATE", "REMAP_PROVIDER", "CONTINUE_LAST", "SURPRISE",
		"PLANNING", "COMPLETED", "PAUSED", "DROPPED", "REWATCHING", "TRACKER", "CAST", "STATS"} {
		if categoryIcon(key) == 0 {
			t.Errorf("%s has no icon", key)
		}
	}
}

package internal

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// Surprise Me: a random show from Plan to Watch, for when the list is long and
// choosing is the hard part. Starting it goes through the usual path, which
// already offers to move a planned show to Watching before episode 1.

// surprisePick chooses a random entry, avoiding the one just shown when there
// is anything else to choose. intn is rand.IntN, swapped out by tests.
func surprisePick(entries []Entry, previous int, intn func(int) int) (Entry, bool) {
	if len(entries) == 0 {
		return Entry{}, false
	}
	candidates := entries
	if len(entries) > 1 && previous != 0 {
		candidates = make([]Entry, 0, len(entries)-1)
		for _, entry := range entries {
			if entry.Media.ID != previous {
				candidates = append(candidates, entry)
			}
		}
	}
	return candidates[intn(len(candidates))], true
}

// surpriseLabel names the pick with what helps decide on it: its length and
// kind, since a two-cour series and a film are different evenings.
func surpriseLabel(entry Entry, config *Config) string {
	details := []string{}
	if format := strings.TrimSpace(entry.Media.Format); format != "" {
		details = append(details, strings.ReplaceAll(format, "_", " "))
	}
	if episodes := entry.Media.Episodes; episodes == 1 {
		details = append(details, "1 episode")
	} else if episodes > 1 {
		details = append(details, fmt.Sprintf("%d episodes", episodes))
	}
	label := "▶ Start " + mediaDisplayTitle(entry.Media, config)
	if len(details) > 0 {
		label += " (" + strings.Join(details, ", ") + ")"
	}
	return label
}

// SurpriseMe offers random shows from Plan to Watch until one is started or
// the viewer goes back. It returns the AniList id of the show to start.
func SurpriseMe(config *Config, list AnimeList) (int, bool) {
	planned := list.Planning
	if len(planned) == 0 {
		Out("Plan to Watch is empty, so there is nothing to pick from.")
		return 0, false
	}

	previous := 0
	for {
		pick, _ := surprisePick(planned, previous, rand.IntN)
		previous = pick.Media.ID

		options := []SelectionOption{{Key: "start", Label: surpriseLabel(pick, config)}}
		if len(planned) > 1 {
			options = append(options, SelectionOption{Key: "reroll", Label: "Reroll"})
		}

		// Order kept: the pick is the point of the menu and leads it.
		selected, err := DynamicSelectPreserveOrder(options)
		if err != nil {
			Log(fmt.Sprintf("Surprise me: %v", err))
			return 0, false
		}
		selected = NormalizeSelectionKey(selected)
		switch {
		case selected.Key == "start":
			return pick.Media.ID, true
		case selected.Key == "reroll":
			ClearScreen()
			continue
		case SelectionMeansQuit(selected):
			Exit(nil)
		}
		return 0, false
	}
}

package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The two menus disagreed about order: the list applied no sort at all and took
// whatever AniList returned, while the poster grid sorted alphabetically by
// title. Neither surfaces the thing you were last watching, which is the whole
// point of a continue-watching list.
//
// Both now order by when the entry last changed, most recent first, so the show
// you are part-way through is at the top of either menu.

// sortEntriesByRecency orders entries most-recently-updated first, falling back
// to the title so the order is stable when timestamps tie or are missing.
func sortEntriesByRecency(entries []Entry, config *CurdConfig) []Entry {
	sorted := append([]Entry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool {
		left, right := sorted[i].UpdatedAt, sorted[j].UpdatedAt
		if !left.Equal(right) {
			return left.After(right)
		}
		return strings.ToLower(mediaDisplayTitle(sorted[i].Media, config)) <
			strings.ToLower(mediaDisplayTitle(sorted[j].Media, config))
	})
	return sorted
}

// formatTimeUntilAiring renders a countdown to the next episode.
//
// AniList sends timeUntilAiring with every list fetch and Curd stored it without
// ever reading it: only the episode number was used, for the "new episode" flag.
// It costs nothing extra to say when the next one lands.
func formatTimeUntilAiring(seconds int) string {
	delay := formatAiringDelay(seconds)
	if delay == "" {
		return ""
	}
	return "next in " + delay
}

// formatAiringDelay renders just the wait -- "4d", "22h", "35m" -- so callers
// can phrase it themselves. A row says "next in 4d"; a prompt explaining why an
// episode cannot be played says "airs in 4d".
func formatAiringDelay(seconds int) string {
	if seconds <= 0 {
		return ""
	}

	d := time.Duration(seconds) * time.Second
	switch {
	case d < time.Hour:
		minutes := int(d.Minutes())
		if minutes < 1 {
			return "<1m"
		}
		return fmt.Sprintf("%dm", minutes)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

const (
	// resumeMinSeconds is how far in you must be before a resume point is worth
	// showing. Two seconds into an episode is noise, not information.
	resumeMinSeconds = 60
	// resumeMaxFraction is how far through an episode a resume point stays
	// interesting; past this you have effectively finished it.
	resumeMaxFraction = 0.95
)

// formatResumePosition renders a resume point, or "" when there is nothing
// useful to say. durationMinutes is the episode length as stored locally.
func formatResumePosition(playbackSeconds, durationMinutes int) string {
	if playbackSeconds < resumeMinSeconds {
		return ""
	}
	if durationMinutes > 0 {
		total := durationMinutes * 60
		if float64(playbackSeconds) > float64(total)*resumeMaxFraction {
			return ""
		}
	}

	hours := playbackSeconds / 3600
	minutes := (playbackSeconds % 3600) / 60
	seconds := playbackSeconds % 60
	if hours > 0 {
		return fmt.Sprintf("resume %d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("resume %d:%02d", minutes, seconds)
}

// entryStatusNote returns the one extra note worth appending to a list row.
//
// Resume wins over the airing countdown: a part-watched episode is something to
// act on now, whereas a countdown is something to come back for, and showing
// both would crowd the row.
//
// withCountdown is false for the poster grid. A cover's label is clipped at the
// column width, and "next in 22h" costs about fourteen characters of a
// thirty-seven character line -- it buys back more than a third of the title.
// The text menu has the width to spare, so it keeps the countdown.
func entryStatusNote(entry Entry, resume string, withCountdown bool) string {
	if resume != "" {
		return resume
	}
	if !withCountdown {
		return ""
	}
	if next := entry.Media.NextAiringEpisode; next != nil {
		return formatTimeUntilAiring(next.TimeUntilAiring)
	}
	return ""
}

// resumePointsByAnilistID reads the local watch history into a lookup of resume
// notes. It is read once per menu build rather than per row: the history is a
// flat file and re-reading it for every entry would be wasteful on a long list.
func resumePointsByAnilistID(config *CurdConfig) map[int]string {
	points := make(map[int]string)
	if config == nil {
		return points
	}

	databaseFile := filepath.Join(os.ExpandEnv(config.StoragePath), "curd_history.txt")
	for _, anime := range LocalGetAllAnime(databaseFile) {
		if anime.AnilistId == 0 {
			continue
		}
		if note := formatResumePosition(anime.Ep.Player.PlaybackTime, anime.Ep.Duration); note != "" {
			points[anime.AnilistId] = note
		}
	}
	return points
}

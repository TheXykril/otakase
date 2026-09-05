package internal

import (
	"fmt"
	"strings"
)

// The watching list showed titles alone, so answering "how far through am I?" or
// "is there anything new?" meant opening the show. Every number needed is
// already on the AniList entry.
//
// The denominator is always the season total, so a fraction means the same thing
// on every row. How many episodes have actually aired is added only while a show
// is still releasing and differs from the total, because that is the only time
// the distinction matters.

// EpisodeProgress is what is known about one entry's episode counts.
type EpisodeProgress struct {
	// Watched is how many episodes the tracker records as seen.
	Watched int
	// Total is the season's episode count, or 0 when AniList does not know it
	// yet — common for shows announced before a count is fixed.
	Total int
	// Aired is how many episodes have been broadcast, or 0 when unknown.
	Aired int
	// Releasing reports whether the show is still airing.
	Releasing bool
}

// episodeProgressFor reads the counts off a list entry.
func episodeProgressFor(entry Entry) EpisodeProgress {
	progress := EpisodeProgress{
		Watched:   entry.Progress,
		Total:     entry.Media.Episodes,
		Releasing: strings.EqualFold(strings.TrimSpace(entry.Media.Status), "RELEASING"),
	}

	// nextAiringEpisode names the next episode to broadcast, so everything
	// before it has aired.
	if next := entry.Media.NextAiringEpisode; next != nil && next.Episode > 0 {
		progress.Aired = next.Episode - 1
	} else if !progress.Releasing {
		// A finished show has aired its whole run.
		progress.Aired = progress.Total
	}

	if progress.Watched < 0 {
		progress.Watched = 0
	}
	return progress
}

// Summary renders the counts for a list row, or "" when nothing is known.
func (p EpisodeProgress) Summary() string {
	switch {
	case p.Total > 0:
		summary := fmt.Sprintf("%d/%d", p.Watched, p.Total)
		// Only worth saying while airing, and only when it tells the user
		// something the total does not. Parenthesised rather than joined with a
		// second "·": the rofi row splits on the first "·" to dim metadata, so an
		// interior one would leave half the counts at full strength.
		if p.Releasing && p.Aired > 0 && p.Aired < p.Total {
			summary += fmt.Sprintf(" (%d aired)", p.Aired)
		}
		return summary

	case p.Aired > 0:
		// No season total, but we know what has aired.
		return fmt.Sprintf("%d/%d aired", p.Watched, p.Aired)

	case p.Watched > 0:
		// Nothing but the user's own progress, e.g. a long-running show with no
		// announced episode count.
		return fmt.Sprintf("%d watched", p.Watched)

	default:
		return ""
	}
}

// WithEpisodeCounts appends the counts to a list row's title.
func WithEpisodeCounts(title string, entry Entry) string {
	summary := episodeProgressFor(entry).Summary()
	if summary == "" {
		return title
	}
	// " · " is the separator the rofi rows already treat as the start of
	// metadata, so the whole count block dims with it.
	return fmt.Sprintf("%s · %s", title, summary)
}

package internal

import (
	"math"
	"strings"
	"sync"

	"github.com/thexykril/otakase/internal/aniskip"
)

var (
	aniSkipOnce   sync.Once
	aniSkipShared *aniskip.Client
)

// aniSkipClient is created on first use so it shares the HTTP client set up in
// http_client.go.
func aniSkipClient() *aniskip.Client {
	aniSkipOnce.Do(func() { aniSkipShared = aniskip.New(sharedHTTPClient) })
	return aniSkipShared
}

// skipTimesFromEntries files entries under opening, ending and recap by the
// kind each one names, keeping the first usable entry of each kind. Entries
// are expected best first, as aniskip.Fit returns them.
func skipTimesFromEntries(entries []aniskip.Entry) (SkipTimes, SkipIDs) {
	var times SkipTimes
	var ids SkipIDs
	for _, e := range entries {
		span := Skip{Start: int(math.Round(e.Start)), End: int(math.Round(e.End))}
		switch strings.ToLower(strings.TrimSpace(e.Kind)) {
		case "op", "mixed-op":
			if !usableSpan(times.Op) {
				times.Op, ids.Op = span, e.ID
			}
		case "ed", "mixed-ed":
			if !usableSpan(times.Ed) {
				times.Ed, ids.Ed = span, e.ID
			}
		case "recap":
			if !usableSpan(times.Recap) {
				times.Recap = span
			}
		}
	}
	return times, ids
}

// SendSkipTimesToMPV marks the opening and ending as chapters in mpv, so they
// show on the seek bar and can be jumped with the chapter keys.
func SendSkipTimesToMPV(anime *Anime) error {
	if anime == nil || anime.Ep.Player.SocketPath == "" {
		// Casting has no player socket.
		return nil
	}
	op, ed := anime.Ep.SkipTimes.Op, anime.Ep.SkipTimes.Ed
	chapter := func(title string, start, end int) map[string]interface{} {
		c := map[string]interface{}{"title": title, "time": float64(start)}
		if end >= 0 {
			c["end"] = float64(end)
		}
		return c
	}
	chapters := []map[string]interface{}{
		chapter("Pre-Opening", 0, op.Start),
		chapter("Opening", op.Start, op.End),
		chapter("Main", op.End, ed.Start),
		chapter("Ending", ed.Start, ed.End),
		chapter("Post-Credits", ed.End, -1),
	}
	_, err := MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"set_property", "chapter-list", chapters})
	return err
}

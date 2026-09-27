package internal

import (
	"path/filepath"
	"testing"
)

// Review Focus 3. The last episode must not ask StartNextEpisode for an
// episode that does not exist, and must still reach the completion handling a
// finished series gets.
func TestAdvanceAfterEpisodeAtTheEndOfASeries(t *testing.T) {
	anime := &Anime{AnilistId: 1, TotalEpisodes: 12}
	anime.Ep.Number = 12

	decided := advanceDecision(anime, false)

	if decided.Continue {
		t.Error("the last episode of a series offered a next episode")
	}
	if !decided.SeriesFinished {
		t.Error("finishing the last episode was not recognised as finishing the series")
	}
}

// A mid-season episode continues, and is not treated as the end of anything.
func TestAdvanceAfterEpisodeMidSeason(t *testing.T) {
	anime := &Anime{AnilistId: 1, TotalEpisodes: 12}
	anime.Ep.Number = 5

	decided := advanceDecision(anime, true)

	if !decided.Continue {
		t.Error("a mid-season episode did not continue")
	}
	if decided.SeriesFinished {
		t.Error("a mid-season episode was treated as finishing the series")
	}
}

// Review Focus 4. A rewatch must not push progress: the entry is already
// complete, and writing to it rewrites a finished record.
func TestAdvanceAfterEpisodeSkipsProgressOnARewatch(t *testing.T) {
	anime := &Anime{AnilistId: 1, TotalEpisodes: 12, Rewatching: true}
	anime.Ep.Number = 12

	if advanceDecision(anime, false).PushProgress {
		t.Error("a rewatch pushed remote progress, overwriting a completed entry")
	}
}

// A show with no known episode count cannot be at its end, so it continues.
func TestAdvanceAfterEpisodeWithUnknownTotal(t *testing.T) {
	anime := &Anime{AnilistId: 1, TotalEpisodes: 0}
	anime.Ep.Number = 3

	if advanceDecision(anime, true).SeriesFinished {
		t.Error("a show with no known total was treated as finished")
	}
}

// The two callers ask "start the next episode?" differently -- a menu for
// local playback, a countdown for a cast -- and AdvanceAfterEpisode must act
// on whatever wantsNext answers rather than asking again itself. Rewatching
// is set so PushProgress is false and no remote call is attempted; the
// episode is mid-season so HandleLastEpisodeCompletion is a no-op either way.
func TestAdvanceAfterEpisodeDoesNotAdvanceWhenWantsNextSaysNo(t *testing.T) {
	config := &Config{}
	anime := &Anime{AnilistId: 1, TotalEpisodes: 12, Rewatching: true}
	anime.Ep.Number = 5
	user := &User{}
	databaseFile := filepath.Join(t.TempDir(), "curd_history.txt")

	asked := false
	result := AdvanceAfterEpisode(config, anime, user, databaseFile, func() bool {
		asked = true
		return false
	})

	if !asked {
		t.Fatal("wantsNext was never called")
	}
	if result {
		t.Error("AdvanceAfterEpisode reported an episode to continue to when wantsNext said no")
	}
	if anime.Ep.Number != 5 {
		t.Errorf("StartNextEpisode ran even though wantsNext said no: episode is now %d", anime.Ep.Number)
	}
}

// A nil wantsNext must not silently advance: a caller that forgot to supply an
// answer is a bug, not permission to continue.
func TestAdvanceAfterEpisodeTreatsNilWantsNextAsNo(t *testing.T) {
	config := &Config{}
	anime := &Anime{AnilistId: 1, TotalEpisodes: 12, Rewatching: true}
	anime.Ep.Number = 5
	user := &User{}
	databaseFile := filepath.Join(t.TempDir(), "curd_history.txt")

	result := AdvanceAfterEpisode(config, anime, user, databaseFile, nil)

	if result {
		t.Error("AdvanceAfterEpisode reported an episode to continue to with a nil wantsNext")
	}
	if anime.Ep.Number != 5 {
		t.Errorf("StartNextEpisode ran with a nil wantsNext: episode is now %d", anime.Ep.Number)
	}
}

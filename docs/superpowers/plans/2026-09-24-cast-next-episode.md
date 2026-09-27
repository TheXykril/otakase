# Casting Continues To The Next Episode — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A cast that reaches the end of an episode continues to the next one, doing everything local playback already does, without the viewer walking back to the keyboard.

**Architecture:** Casting stops reporting completion as failure, so a terminal launch rejoins the loop main already runs. The block that loop runs after an episode is extracted into one function, which the spawned rofi process calls in a small loop of its own — one implementation, two callers. The only genuinely new UI is a countdown in the cast panel.

**Tech Stack:** Go 1.26, vendored. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-09-24-cast-next-episode-design.md`

## Global Constraints

- `internal/cast` must NOT import `internal` — that is an import cycle.
- Never re-encode on the copy path. Subtitle burning is the one exception and is unchanged by this work.
- No new dependency. No new required config.
- Go 1.26, vendored builds: `CGO_ENABLED=0 go build -mod=vendor`. `go mod tidy` needs `GOFLAGS=-mod=mod`.
- `./Build/ci-local --quick` is the gate and must pass before every commit.
- **No attribution trailers in commit messages** — no `Co-authored-by:`, no `Claude-Session:`, no `Generated with`.
- Storage filenames keep their curd-era names: `curd_history.txt`, `curd_version`, `curd_id`.
- **The extraction must not change local playback.** It is a move, not a rewrite: the same calls in the same order.
- `q` stops, it does not complete. Stopping a cast never advances.
- A failure ends the loop. It never advances past an episode that did not play.

## Review Focus

Five things the spec implies that no obvious task test would reach, each pinned to a test in the task that owns the code:

1. **A provider link that has rotted mid-season.** Every episode fails in seconds; a loop that advances on failure marks a whole season watched in about a minute. — Task 1 and Task 4.
2. **`q` pressed during the countdown**, not during the episode. The episode completed, so the loop is entitled to advance — but the viewer just asked it not to. — Task 2.
3. **The last episode of a series.** `StartNextEpisode` must not be asked for an episode that does not exist, and `HandleLastEpisodeCompletion` must still run. — Task 3.
4. **A rewatch.** `UpdateAnimeProgress` is skipped when `anime.Rewatching`, and the extraction must preserve that — pushing progress on a rewatch rewrites a completed entry. — Task 3.
5. **A scratch directory per episode.** Each episode's teardown must remove its own before the next begins, or a twelve-episode session holds twelve episodes on disk. — Task 4.

---

## Task 1: Casting reports finished, stopped and failed apart

**Files:**
- Modify: `internal/cast_playback.go` (the `q` return in `watchCastWithControls`, and `CastEpisode`)
- Modify: `internal/otakase.go:1467-1497` (the `CastToDevice` branch in `StartPlayback`)
- Test: `internal/cast_outcome_test.go` (create)

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces: `var ErrCastStopped = errors.New("cast: stopped by the viewer")`, and `StartPlayback` returning the string `"cast"` when an episode finished.

Context an implementer needs: `watchCastWithControls` currently returns `nil` both when the episode ended and when the viewer pressed `q`, so the caller cannot tell them apart. `StartPlayback` returns `""` for every cast, and `cmd/otakase/main.go:483` reads `""` as "playback did not start" and exits. That is the whole reason casting never continues.

- [ ] **Step 1: Write the failing test**

Create `internal/cast_outcome_test.go`:

```go
package internal

import (
	"errors"
	"testing"
)

// Three outcomes, not two. A cast that reached the end of an episode is
// entitled to advance; one the viewer stopped is not; one that failed must not
// advance and must say why. Collapsing any pair of these either strands the
// viewer on one episode or marches a rotted provider link through a season.
func TestCastOutcomeForError(t *testing.T) {
	failure := errors.New("cast: ffmpeg failed")

	for _, tc := range []struct {
		name     string
		err      error
		want     string
		wantSaid bool
	}{
		{"episode finished", nil, "cast", false},
		{"viewer pressed q", ErrCastStopped, "", false},
		{"cast failed", failure, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			socket, reported := castOutcome(tc.err)
			if socket != tc.want {
				t.Errorf("socket = %q, want %q", socket, tc.want)
			}
			if reported != tc.wantSaid {
				t.Errorf("reported = %v, want %v", reported, tc.wantSaid)
			}
		})
	}
}

// A wrapped stop is still a stop: the error travels up through CastEpisode,
// and a comparison by value rather than errors.Is would miss it.
func TestCastOutcomeRecognisesAWrappedStop(t *testing.T) {
	socket, reported := castOutcome(errors.New("cast: " + ErrCastStopped.Error()))
	_ = socket
	_ = reported

	wrapped := errors.Join(ErrCastStopped, errors.New("and something else"))
	if socket, _ := castOutcome(wrapped); socket != "" {
		t.Errorf("a wrapped stop was treated as a finished episode (socket %q)", socket)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ -run TestCastOutcome -count=1`
Expected: FAIL, `undefined: ErrCastStopped` and `undefined: castOutcome`

- [ ] **Step 3: Write the implementation**

Add to `internal/cast_playback.go`, above `CastEpisode`:

```go
// ErrCastStopped reports that the viewer ended the cast themselves.
//
// It is not a failure and it is not a finished episode: nothing is wrong, and
// nothing should advance. Without it both look like the nil a finished episode
// returns, and pressing q would start the next episode.
var ErrCastStopped = errors.New("cast: stopped by the viewer")

// castSocketSentinel is what StartPlayback hands back for a cast that played
// an episode through.
//
// It follows the android-intent sentinel already in cmd/otakase/main.go: a
// player that ran somewhere else and is now finished, whose caller should carry
// on rather than poll a socket that will never exist.
const castSocketSentinel = "cast"

// castOutcome turns a cast's error into the socket path StartPlayback returns
// and whether the failure is worth reporting to the viewer.
func castOutcome(err error) (socket string, report bool) {
	switch {
	case err == nil:
		return castSocketSentinel, false
	case errors.Is(err, ErrCastStopped):
		return "", false
	default:
		return "", true
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/ -run TestCastOutcome -count=1 -v`
Expected: PASS, both tests and all sub-tests

- [ ] **Step 5: Return the stop sentinel from the q path**

In `internal/cast_playback.go`, in `watchCastWithControls`, the stop branch currently reads:

```go
			if stop {
				castOut(commands != nil, "Stopped.")
				savePartial(lastPosition)
				return nil
			}
```

Change its return:

```go
			if stop {
				castOut(commands != nil, "Stopped.")
				savePartial(lastPosition)
				// Not nil: a finished episode returns nil, and the caller
				// advances on that. The viewer asked for this to end.
				return ErrCastStopped
			}
```

- [ ] **Step 6: Use the outcome in StartPlayback**

In `internal/otakase.go`, the `CastToDevice` branch ends:

```go
		if err := CastEpisode(userConfig, anime); err != nil {
			Out("Casting failed: " + err.Error())
			Log(fmt.Sprintf("cast: %v", err))
		}
		return ""
	}
```

Replace those lines with:

```go
		castErr := CastEpisode(userConfig, anime)
		socket, report := castOutcome(castErr)
		if report {
			Out("Casting failed: " + castErr.Error())
			Log(fmt.Sprintf("cast: %v", castErr))
		}
		return socket
	}
```

- [ ] **Step 7: Run the gate**

Run: `./Build/ci-local --quick`
Expected: all checks pass

- [ ] **Step 8: Commit**

```bash
git add internal/cast_playback.go internal/cast_outcome_test.go internal/otakase.go
git commit -m "Tell a finished cast apart from a stopped one"
```

---

## Task 2: The countdown

**Files:**
- Create: `internal/cast_next_episode.go`
- Create: `internal/cast_next_episode_test.go`

**Interfaces:**
- Consumes: `castPanelWriter` and `castPanelState` from `internal/cast_panel.go`; `castCommand` and the command channel from `internal/cast_controls.go`.
- Produces: `castCountdownDecision` (an int enum with `castCountdownWaiting`, `castCountdownAdvance`, `castCountdownCancelled`), `castCountdownTick(elapsed time.Duration, keyPressed bool, config *Config) castCountdownDecision`, and `castCountdownDuration`.

Context an implementer needs: `NextEpisodePromptCLI` in `internal/otakase.go` blocks on a selection menu, which is wrong for a viewer across the room. This is the cast variant. The decision is a pure function so it tests without a terminal; the rendering reuses the panel that already redraws once a second.

- [ ] **Step 1: Write the failing test**

Create `internal/cast_next_episode_test.go`:

```go
package internal

import (
	"testing"
	"time"
)

// The countdown is the whole point of casting continuing: a viewer on a sofa
// should not walk to the keyboard between episodes, but should be able to stop
// it if they are at the desk.
func TestCastCountdownTick(t *testing.T) {
	prompting := &Config{NextEpisodePrompt: true}

	for _, tc := range []struct {
		name    string
		elapsed time.Duration
		key     bool
		config  *Config
		want    castCountdownDecision
	}{
		{"still counting", 3 * time.Second, false, prompting, castCountdownWaiting},
		{"time is up", castCountdownDuration, false, prompting, castCountdownAdvance},
		{"past the end", castCountdownDuration + time.Second, false, prompting, castCountdownAdvance},
		{"a key stops it", 3 * time.Second, true, prompting, castCountdownCancelled},
		{"a key at the last moment still stops it", castCountdownDuration, true, prompting, castCountdownCancelled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := castCountdownTick(tc.elapsed, tc.key, tc.config); got != tc.want {
				t.Errorf("castCountdownTick = %v, want %v", got, tc.want)
			}
		})
	}
}

// NextEpisodePrompt=false already means "do not ask me" for local playback, and
// it means the same here: no countdown, no wait.
func TestCastCountdownRespectsTheNoPromptSetting(t *testing.T) {
	config := &Config{NextEpisodePrompt: false}

	if got := castCountdownTick(0, false, config); got != castCountdownAdvance {
		t.Errorf("with prompting off, a zero-elapsed tick returned %v, want advance", got)
	}
}

// Review Focus 2. A key during the countdown is the viewer saying stop, even
// though the episode completed and the loop would otherwise be entitled to go
// on.
func TestCastCountdownCancelBeatsExpiry(t *testing.T) {
	config := &Config{NextEpisodePrompt: true}

	if got := castCountdownTick(castCountdownDuration*2, true, config); got != castCountdownCancelled {
		t.Errorf("a keypress past the deadline returned %v, want cancelled", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ -run TestCastCountdown -count=1`
Expected: FAIL, `undefined: castCountdownTick`

- [ ] **Step 3: Write the implementation**

Create `internal/cast_next_episode.go`:

```go
package internal

import (
	"fmt"
	"time"
)

// castCountdownDuration is how long the next episode waits before it starts.
//
// Long enough to reach a keyboard from a sofa, short enough that a viewer who
// wants the next episode is not made to sit through a timer.
const castCountdownDuration = 10 * time.Second

// castCountdownDecision is what one tick of the countdown concluded.
type castCountdownDecision int

const (
	castCountdownWaiting castCountdownDecision = iota
	castCountdownAdvance
	castCountdownCancelled
)

// castCountdownTick decides what a countdown should do, given how long it has
// been running and whether the viewer has touched the keyboard.
//
// Pure, so the decision is tested without a terminal: the rendering and the key
// reading are the caller's, and neither carries a decision of its own.
func castCountdownTick(elapsed time.Duration, keyPressed bool, config *Config) castCountdownDecision {
	// Checked before the deadline: a viewer reaching for the keyboard as the
	// timer runs out means stop, not "too late".
	if keyPressed {
		return castCountdownCancelled
	}
	// NextEpisodePrompt=false already means "do not ask" for local playback.
	if config != nil && !config.NextEpisodePrompt {
		return castCountdownAdvance
	}
	if elapsed >= castCountdownDuration {
		return castCountdownAdvance
	}
	return castCountdownWaiting
}

// castCountdownMessage is the line the panel shows while the countdown runs.
func castCountdownMessage(watched, next int, remaining time.Duration) string {
	seconds := int(remaining.Seconds() + 0.5)
	if seconds < 0 {
		seconds = 0
	}
	return fmt.Sprintf("Episode %d watched · Episode %d in %ds — any key to stop", watched, next, seconds)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/ -run TestCastCountdown -count=1 -v`
Expected: PASS, all three tests and every sub-test

- [ ] **Step 5: Write the message test**

Append to `internal/cast_next_episode_test.go`:

```go
// The panel line has to name both episodes and the remaining time: it is the
// only thing on screen, and a viewer glancing at it should not have to work out
// what is about to happen.
func TestCastCountdownMessage(t *testing.T) {
	line := castCountdownMessage(12, 13, 7*time.Second)

	for _, want := range []string{"12", "13", "7s", "any key"} {
		if !strings.Contains(line, want) {
			t.Errorf("the countdown line does not mention %q: %s", want, line)
		}
	}
}
```

Add `"strings"` to that file's imports.

- [ ] **Step 6: Run it**

Run: `go test ./internal/ -run TestCastCountdownMessage -count=1 -v`
Expected: PASS

- [ ] **Step 7: Run the gate**

Run: `./Build/ci-local --quick`
Expected: all checks pass

- [ ] **Step 8: Commit**

```bash
git add internal/cast_next_episode.go internal/cast_next_episode_test.go
git commit -m "Add the cast countdown decision"
```

---

## Task 3: Extract the post-episode block both callers need

**Files:**
- Create: `internal/advance_episode.go`
- Create: `internal/advance_episode_test.go`
- Modify: `cmd/otakase/main.go:507-530` (the android-intent branch's post-episode block)

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces: `func AdvanceAfterEpisode(config *Config, anime *Anime, user *User, databaseFile string) bool`.

Context an implementer needs: the block being extracted is at `cmd/otakase/main.go:507-530`. **This is a move, not a rewrite.** The same calls in the same order; the loop's `continue` becomes `return true`, and its `Exit(nil)` becomes `return false` so the caller decides how to end. Local playback must behave exactly as it does now.

The block as it stands:

```go
			// Mark as completed
			anime.Ep.IsCompleted = true

			// Update progress for the finished episode
			// Local update
			internal.LocalUpdateAnime(databaseFile, anime.AnilistId, anime.ProviderId, anime.Ep.Number, 0, 0, internal.GetAnimeName(anime), internal.CurrentAnimeProviderName(&anime))

			// Check if we should continue to next episode
			shouldContinue := internal.NextEpisodePromptCLI(&userConfig)

			if shouldContinue {
				internal.StartNextEpisode(&anime, &userConfig, databaseFile, user.Token)
				continue
			} else {
				if anime.Ep.Number == anime.TotalEpisodes {
					internal.HandleLastEpisodeCompletion(&userConfig, &anime, user.Token)
				}
				if !anime.Rewatching {
					internal.UpdateAnimeProgress(user.Token, anime.AnilistId, anime.Ep.Number)
				}
				internal.Exit(nil)
			}
```

- [ ] **Step 1: Write the failing test**

Create `internal/advance_episode_test.go`:

```go
package internal

import "testing"

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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ -run TestAdvanceAfterEpisode -count=1`
Expected: FAIL, `undefined: advanceDecision`

- [ ] **Step 3: Write the implementation**

Create `internal/advance_episode.go`:

```go
package internal

// advanceOutcome is what finishing an episode means for what happens next.
//
// Separated from the doing so the decisions are testable without a tracker, a
// database file or a network: everything below is a question about the episode
// just watched, and none of it needs those to answer.
type advanceOutcome struct {
	// Continue reports that there is another episode to play.
	Continue bool
	// SeriesFinished reports that the episode just watched was the last one.
	SeriesFinished bool
	// PushProgress reports that the tracker should be told about this episode.
	PushProgress bool
}

// advanceDecision works out what finishing this episode means.
//
// wantsNext is the viewer's answer, however it was asked -- a menu for local
// playback, a countdown for a cast.
func advanceDecision(anime *Anime, wantsNext bool) advanceOutcome {
	if anime == nil {
		return advanceOutcome{}
	}

	// A show with no known episode count cannot be at its end.
	atEnd := anime.TotalEpisodes > 0 && anime.Ep.Number >= anime.TotalEpisodes

	return advanceOutcome{
		Continue:       wantsNext && !atEnd,
		SeriesFinished: atEnd,
		// A rewatch already has a completed entry, and pushing progress to it
		// rewrites a finished record.
		PushProgress: !anime.Rewatching,
	}
}

// AdvanceAfterEpisode completes the episode just watched and prepares the next
// one on anime, reporting whether there is another to play.
//
// It is the block that used to sit inside main's playback loop, moved here so
// the process spawned for a rofi cast runs the same code rather than a second
// copy of it. Nothing about it is cast-specific.
func AdvanceAfterEpisode(config *Config, anime *Anime, user *User, databaseFile string) bool {
	if config == nil || anime == nil || user == nil {
		return false
	}

	anime.Ep.IsCompleted = true
	LocalUpdateAnime(databaseFile, anime.AnilistId, anime.ProviderId, anime.Ep.Number, 0, 0,
		GetAnimeName(*anime), CurrentAnimeProviderName(anime))

	decided := advanceDecision(anime, NextEpisodePromptCLI(config))

	if decided.Continue {
		StartNextEpisode(anime, config, databaseFile, user.Token)
		return true
	}

	if decided.SeriesFinished {
		HandleLastEpisodeCompletion(config, anime, user.Token)
	}
	if decided.PushProgress {
		UpdateAnimeProgress(user.Token, anime.AnilistId, anime.Ep.Number)
	}
	return false
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/ -run TestAdvanceAfterEpisode -count=1 -v`
Expected: PASS, all four

- [ ] **Step 5: Use it from main's loop**

In `cmd/otakase/main.go`, replace the block quoted in this task's context — from `// Mark as completed` through `internal.Exit(nil)` — with:

```go
			// The same call the spawned cast process makes, so a rofi cast and
			// a local playback advance through one implementation rather than
			// two that can drift.
			if internal.AdvanceAfterEpisode(&userConfig, &anime, &user, databaseFile) {
				continue
			}
			internal.Exit(nil)
```

- [ ] **Step 6: Run the gate**

Run: `./Build/ci-local --quick`
Expected: all checks pass — this is a move, so nothing about local playback should change

- [ ] **Step 7: Commit**

```bash
git add internal/advance_episode.go internal/advance_episode_test.go cmd/otakase/main.go
git commit -m "Extract what happens after an episode"
```

---

## Task 4: The cast loop, in both processes

**Files:**
- Modify: `internal/cast_playback.go` (`CastEpisode` runs the countdown before returning)
- Modify: `internal/cast_handoff.go` (`RunCastSession` loops)
- Modify: `cmd/otakase/main.go` (the `"cast"` sentinel branch)
- Modify: `internal/cast_next_episode_test.go` (the loop tests)
- Modify: `docs/casting-verification.md`, `README.md`

**Interfaces:**
- Consumes: `ErrCastStopped`, `castSocketSentinel`, `castOutcome` (Task 1); `castCountdownTick`, `castCountdownMessage`, `castCountdownDuration` (Task 2); `AdvanceAfterEpisode` (Task 3).
- Produces: nothing later tasks rely on.

Context an implementer needs: two callers finish a cast. A terminal launch goes through `StartPlayback`, whose `"cast"` return now lets main's loop carry on. A rofi launch runs in a spawned process that enters at `cmd/otakase/main.go`'s `-cast-session` branch and calls `RunCastSession`; that one needs a loop of its own, and it must contain no decisions — only calls to `CastEpisode` and `AdvanceAfterEpisode`.

- [ ] **Step 1: Write the failing test**

Append to `internal/cast_next_episode_test.go`:

```go
// Review Focus 1. A provider link that has rotted fails every episode in
// seconds. A loop that advances on failure marks a whole season watched in
// about a minute, and the viewer finds out from their tracker.
func TestCastLoopStopsOnFailure(t *testing.T) {
	episodes := 0
	cast := func() error {
		episodes++
		return errors.New("cast: no episode links")
	}
	advanced := 0
	advance := func() bool {
		advanced++
		return true
	}

	runCastLoop(cast, advance)

	if episodes != 1 {
		t.Errorf("a failing cast was attempted %d times, want 1", episodes)
	}
	if advanced != 0 {
		t.Errorf("the loop advanced %d times past an episode that never played", advanced)
	}
}

// Stopping is not completing: the viewer asked for this to end.
func TestCastLoopStopsWhenTheViewerStops(t *testing.T) {
	advanced := 0
	runCastLoop(
		func() error { return ErrCastStopped },
		func() bool { advanced++; return true },
	)

	if advanced != 0 {
		t.Errorf("pressing q advanced %d times", advanced)
	}
}

// The ordinary case: episodes play until there is no next one.
func TestCastLoopRunsUntilThereIsNoNextEpisode(t *testing.T) {
	episodes := 0
	remaining := 3
	runCastLoop(
		func() error { episodes++; return nil },
		func() bool { remaining--; return remaining > 0 },
	)

	if episodes != 3 {
		t.Errorf("played %d episodes, want 3", episodes)
	}
}
```

Add `"errors"` to the file's imports.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ -run TestCastLoop -count=1`
Expected: FAIL, `undefined: runCastLoop`

- [ ] **Step 3: Write the loop**

Append to `internal/cast_next_episode.go`:

```go
// runCastLoop casts episodes until there is no next one, the viewer stops, or
// something fails.
//
// It holds no decisions of its own: cast plays one episode and reports how it
// ended, advance completes it and prepares the next. Both are injected, which
// is what lets the loop be tested without a device -- and what keeps the
// advancing logic in one place rather than two.
func runCastLoop(cast func() error, advance func() bool) {
	for {
		err := cast()
		if err != nil {
			// A failure and a stop both end the loop. Advancing past an
			// episode that never played would mark a season watched in about a
			// minute on a provider link that has rotted.
			return
		}
		if !advance() {
			return
		}
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/ -run TestCastLoop -count=1 -v`
Expected: PASS, all three

- [ ] **Step 5: Run the countdown inside CastEpisode**

In `internal/cast_playback.go`, `CastEpisode` currently ends:

```go
	return watchCastWithControls(config, anime, s, srv, rx, device, commands)
```

Replace with:

```go
	if err := watchCastWithControls(config, anime, s, srv, rx, device, commands); err != nil {
		return err
	}

	// The countdown runs in this episode's panel, which still owns the screen:
	// the teardown below has not run yet, so there is a frame to draw in.
	if !castAwaitNextEpisode(config, anime, panel, commands) {
		return ErrCastStopped
	}
	return nil
```

Then append to `internal/cast_next_episode.go`:

```go
// castAwaitNextEpisode counts down to the next episode in the panel, reporting
// whether it should start.
//
// A nil panel means no terminal to draw in -- the countdown still runs, it is
// simply not shown, because a viewer who cannot see it can still be waiting for
// the next episode.
func castAwaitNextEpisode(config *Config, anime *Anime, panel *castPanelWriter, commands <-chan castCommand) bool {
	started := time.Now()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		var pressed bool
		select {
		case <-ticker.C:
		case <-commands:
			pressed = true
		}

		elapsed := time.Since(started)
		switch castCountdownTick(elapsed, pressed, config) {
		case castCountdownAdvance:
			return true
		case castCountdownCancelled:
			return false
		}

		if panel != nil {
			fmt.Print(panel.status(
				GetAnimeName(*anime), anime.Ep.Number, config.CastDevice,
				castCountdownMessage(anime.Ep.Number, anime.Ep.Number+1, castCountdownDuration-elapsed),
			))
		}
	}
}
```

`commands` is nil when controls are unavailable, and a receive on a nil channel blocks forever — which is correct here: the ticker still fires, and the countdown still expires.

- [ ] **Step 6: Loop in the spawned process**

In `internal/cast_handoff.go`, `RunCastSession` currently ends:

```go
	SetGlobalAnime(anime)

	return CastEpisode(config, anime)
```

Replace with:

```go
	SetGlobalAnime(anime)

	// The spawned process has no show to select, so it cannot enter main's
	// loop at the top. It runs the same advance instead, so a rofi cast and a
	// local playback continue through one implementation.
	databaseFile := filepath.Join(os.ExpandEnv(config.StoragePath), "curd_history.txt")
	var lastErr error
	runCastLoop(
		func() error {
			lastErr = CastEpisode(config, anime)
			return lastErr
		},
		func() bool {
			return AdvanceAfterEpisode(config, anime, GetGlobalUser(), databaseFile)
		},
	)

	if errors.Is(lastErr, ErrCastStopped) {
		return nil
	}
	return lastErr
```

Add `"errors"` to that file's imports if it is not already there.

- [ ] **Step 7: Let main's loop continue after a cast**

In `cmd/otakase/main.go`, the empty-socket check reads:

```go
		if anime.Ep.Player.SocketPath == "" {
			internal.Log("Playback did not start; no MPV socket")
			internal.Exit(nil)
			return
		}
```

Add the cast branch immediately before it:

```go
		// A cast played the episode somewhere else and is finished, the same
		// shape as the android-intent case below: carry on rather than poll a
		// socket that will never exist. The literal matches what
		// internal.castSocketSentinel returns; main is a different package and
		// cannot see it, which is how the android-intent sentinel is written
		// here too.
		if anime.Ep.Player.SocketPath == "cast" {
			if internal.AdvanceAfterEpisode(&userConfig, &anime, &user, databaseFile) {
				continue
			}
			internal.Exit(nil)
			return
		}

		if anime.Ep.Player.SocketPath == "" {
```

- [ ] **Step 8: Run the gate**

Run: `./Build/ci-local --quick`
Expected: all checks pass

- [ ] **Step 9: Confirm a scratch directory does not survive an episode**

Review Focus 5 is a property of code that already exists — `CastEpisode`'s teardown removes its own directory before returning — so this step confirms it rather than changing it.

Run: `grep -n "os.RemoveAll(streamDir)" internal/cast_playback.go`
Expected: one match, inside the once-guarded teardown

- [ ] **Step 10: Document it**

In `README.md`'s `## Casting` section, replace:

```markdown
`-cast` plays a single episode and does not continue to the next: otakase
exits once it ends rather than advancing the way local playback does.
```

with:

```markdown
Casting continues to the next episode the way local playback does. When one
ends, the panel counts down ten seconds and starts the next — any key stops it.
`NextEpisodePrompt=false` skips the countdown entirely, and filler episodes,
the end of a series and your tracker are all handled the same way they are
locally, because it is the same code.
```

In `docs/casting-verification.md`, add before `## Known limits`:

```markdown
## 6. Continuing to the next episode

- [ ] **Let an episode finish.** The panel shows a countdown and the next
      episode starts on its own.
- [ ] **Press a key during the countdown.** It stops, and otakase exits without
      starting the next episode.
- [ ] **Check the tracker advanced for both episodes**, not just the first.
- [ ] **Between episodes**, confirm `<StoragePath>/cast-scratch/` holds one
      episode at a time rather than accumulating.
- [ ] **Press `q` during an episode.** The next one must not start — stopping
      is not finishing.
```

Delete the now-untrue line in `## Known limits` that says `-cast` plays one episode and does not advance.

- [ ] **Step 11: Commit**

```bash
git add internal/cast_playback.go internal/cast_handoff.go internal/cast_next_episode.go internal/cast_next_episode_test.go cmd/otakase/main.go README.md docs/casting-verification.md
git commit -m "Continue to the next episode when casting"
```

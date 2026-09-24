# Casting continues to the next episode

**Status:** design, approved in conversation 2026-09-24
**Branch:** `chromecast-casting`
**Depends on:** casting, verified working on real hardware; skip-time
resolution on the cast path (commit `5705793`)

Casting plays one episode and the program exits. Local playback does not: it
runs a loop that marks the episode watched, updates the tracker, skips filler,
asks about the next one, plays it, and handles the end of a series. Casting has
never been inside that loop.

## Goal

A cast that reaches the end of an episode continues to the next one, doing
everything local playback already does, without a viewer walking back to the
keyboard.

## The decision this design rests on

**Casting rejoins the loop that already exists, rather than growing one of its
own.**

There is already a precedent for a player that runs elsewhere and reports back:
`android-intent`. `StartPlayback` returns that sentinel instead of an mpv
socket path, and `cmd/otakase/main.go:496` handles it — mark complete, update
local history, then fall into the same filler, prompt, advance and
end-of-series code every other episode uses.

Casting is the same shape. It plays an episode somewhere else and returns when
that episode is over. It exits today only because it returns `""`, which
`cmd/otakase/main.go:483` reads as "playback failed".

The alternative — a cast playlist controller inside `CastEpisode`, mirroring
`StartMPVPlaylistController` — was rejected. It would reimplement filler
handling, tracking, advancing and end-of-series in a second place, where a fix
to one can silently miss the other. Parity would become something to build and
maintain rather than a property of sharing a path.

## Non-goals

- **Holding the device session open across episodes.** Each episode gets a
  fresh remux, server and session. That costs a brief reconnect between
  episodes and keeps the teardown this branch spent five review rounds
  hardening working exactly as it does now. Reuse is a later optimisation.
- **Prefetching the next episode's stream** while the current one plays.
- **Any change to local playback.** The loop is shared; its behaviour is not
  altered for anyone.

---

## 1. Casting reports completion instead of failure

`CastEpisode` returns normally when an episode ends. `StartPlayback`'s cast
branch returns the sentinel `"cast"` in that case, and `""` only when casting
genuinely failed — a device that never started, a dead remux, a stream that
could not be resolved.

`cmd/otakase/main.go` gains one branch beside the `android-intent` case at
`:496`, reached when the socket path is `"cast"`:

- mark the episode complete
- write local history through `LocalUpdateAnime`, as the android branch does
- fall through into the existing `NextEpisodePromptCLI` / `StartNextEpisode` /
  `HandleLastEpisodeCompletion` path

The failure path is unchanged: `""` still means playback did not start, still
logs and exits, and still prints whatever `CastEpisode` reported.

**`q` is not completion.** A viewer who stops a cast has stopped watching, and
the loop must not advance. `CastEpisode` distinguishes the two: an episode that
reached its end returns `"cast"`, a stop returns `""` with no error, and main
exits quietly on the second without reporting a failure that did not happen.

## 2. The countdown prompt

`NextEpisodePromptCLI` blocks on a selection menu at the keyboard. Casting's
viewer is across the room, so a cast variant lives beside it in
`internal/cast_next_episode.go` and is used only on the cast path.

It makes the same decisions as the existing prompt, and reuses the same helpers
rather than restating them:

- `nextEpisodeAiring` guards an episode the tracker says has not aired, exactly
  as `NextEpisodePromptCLI` does; an unaired next episode ends the loop rather
  than counting down into a failure.
- The last episode of a series says so, and finishing it goes to
  `HandleLastEpisodeCompletion`.
- `NextEpisodePrompt=false` skips the countdown and advances immediately, which
  is what that setting already means for local playback.

The countdown renders in the panel, through the frame the controls already own:

```
╭ Rich Girl Caretaker: I'm Secretly the Ca… · Ep 12 ──────────────────── Office TV ╮
│ ✔ Episode 12 watched                                                             │
│ ▶ Episode 13 in 7s — any key to stop                                             │
╰ space · ←→ · ↑↓ · q stop ────────────────────────────────────────────────────────╯
```

Ten seconds, counted down once a second in the frame that is already being
redrawn. **Any key cancels**, not a specific one: a viewer reaching for the
keyboard to stop the next episode should not have to find the right key, and
every key otherwise available during playback is meaningless once the episode
has ended.

The decision itself is a pure function — elapsed time, whether a key arrived,
and the config — so it is tested without a terminal:

```go
// castCountdownDecision reports what a countdown tick concluded.
type castCountdownDecision int

const (
    castCountdownWaiting castCountdownDecision = iota
    castCountdownAdvance
    castCountdownCancelled
)

func castCountdownTick(elapsed time.Duration, keyPressed bool, config *Config) castCountdownDecision
```

## 3. Lifecycle across an episode boundary

The panel and the screen takeover belong to one episode. Between episodes:

1. The countdown renders in the outgoing episode's panel, which still owns the
   screen.
2. On advance, `CastEpisode` returns; its deferred teardown stops the device,
   closes the server, kills ffmpeg, removes the scratch directory and releases
   the screen — the path that already runs at the end of every cast.
3. Main's loop resolves the next episode and calls `StartPlayback` again, which
   takes the screen and builds a fresh panel.

Nothing in the teardown is made conditional or reused across episodes. The
reconnect is visible — a second or two of black between episodes — and is the
price of not touching code that took five review rounds to get right.

**Scratch directories do not accumulate.** Each episode's directory is removed
by its own teardown before the next begins, so a twelve-episode session holds
one episode on disk at a time rather than twelve.

## 4. Error handling

**A failure ends the loop.** An episode that cannot be resolved, cannot start on
the device, or whose remux dies reports and stops. It does not advance.

This matters more than it sounds: a provider link that has rotted would
otherwise march through a whole season, each episode failing in seconds and
each one marked watched by a loop that never checked whether anything played.
Advancing past a failure is how a viewer loses a season's tracking in a minute.

**Interrupts behave as they do now.** Ctrl+C, SIGHUP and closing the spawned
terminal run the same teardown at whatever point they land, including during a
countdown. The countdown is not a special state for signals.

## 5. Testing

- `castCountdownTick` is a table test over elapsed time, keypress and config,
  including `NextEpisodePrompt=false` advancing with no wait.
- The sentinel is tested at the boundary: a cast that reached the end of an
  episode does not read as a failure, and a cast stopped with `q` does not read
  as completion.
- The loop itself is the existing local-playback path and is already covered;
  this design adds no second copy of it to test.
- The hardware checklist gains a section: let an episode finish, confirm the
  countdown appears and advances; press a key during it and confirm it stops;
  confirm the tracker advanced for both episodes; and confirm no scratch
  directory or ffmpeg process survives the boundary.

## 6. Verification

`docs/casting-verification.md` gains a "continuing to the next episode"
section. The existing checklist still applies in full — this changes what
happens after an episode, not what happens during one.

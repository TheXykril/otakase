# Casting UX: subtitles, controls, and a terminal for rofi launches

**Status:** design, approved in conversation 2026-09-23
**Branch:** `chromecast-casting`
**Depends on:** casting itself, verified working on real hardware 2026-09-23

Casting works, but three things are missing that a viewer notices in the first
minute: soft subtitles silently do not appear, nothing can be paused, and a
launch from the rofi keybind has no interface at all. This design covers all
three. They land in the order below, and the branch merges when all three are
done.

## Goal

A cast launched from a terminal, or from the Hyprland keybind, is watchable and
controllable, and shows subtitles where the provider can supply them burned in.

## Non-goals

- **Reattaching to a running cast.** Closing the controls terminal ends the
  cast, deliberately (see "Lifecycle").
- **Controls inside the rofi process.** Rofi is a picker, not a player UI.
- **The resume seek.** Casting still starts at the beginning; deferred earlier
  and still deferred, because a correct seek must wait for the event playlist
  to cover the target position.
- **Transcoding.** Unchanged: the remux is `-c copy` and never re-encodes.

---

## 1. Hardsub when casting

The Default Media Receiver renders WebVTT only, and no provider here supplies
WebVTT, so a soft-subbed cast plays with no subtitles at all. Casting should
therefore ask for the burned-in variant.

**Change.** When `-cast` is set, `userConfig.SubStyle = "hard"` for the run,
applied where the cast flag is already read in `cmd/otakase/main.go`, before any
episode is resolved.

**It must not persist.** `providerhost.CurrentSubStyle` reads the in-memory
config, so the override reaches providers with no file write. `substyle.Choose`
only calls `rememberChoice` on its `ask` branches and on `promptSoftFallback`;
a forced `hard` with a hard variant available takes neither, so the user's
stored preference is untouched.

**Existing fallbacks stand.** A provider with only soft subs still reaches
`promptSoftFallback` and asks. A provider with no substyle support at all —
`anikoto`, the most-used one — is unaffected, and keeps today's note that
subtitles cannot be cast.

**Scope reality:** only `anineko` and `anipub` implement substyle. This helps
there and nowhere else. That is worth doing and worth not overselling.

**Tests.** A test that `-cast` sets `SubStyle` to `hard` on the run config and
that the config file on disk is unchanged afterwards.

---

## 2. Controls

### Keys

| Key | Action |
|---|---|
| `space` | pause / resume |
| `←` / `→` | seek 10s back / forward |
| `↑` / `↓` | device volume ±5% |
| `s` | skip the opening or ending span the position is in, or the next one |
| `q` | stop the cast and exit |
| `Ctrl+C` (`0x03`) | same as `q` |

### Status line

Redrawn in place on every poll, carriage-return rewritten rather than appended:

```
  12:04 / 24:11  ████████░░░░░░░░  PLAYING  vol 60%
```

### Structure

`internal/cast_controls.go` is new and owns: putting stdin in raw mode, decoding
key bytes into commands, and rendering the status line. It exposes a channel of
commands and nothing else; it knows nothing about cast sessions.

`watchCast` replaces `time.Sleep(castPollInterval)` with a `select` over a
ticker and that command channel, and applies each command against the session
it already holds. This is the only change to its control flow.

`internal/cast/session.go` gains thin wrappers over the vendored library, which
already provides every one: `Pause`, `Unpause`, `SetVolume(float32)`,
`Volume() *cast.Volume`, and `StopMedia`.

The `castSession` interface in `internal/cast_playback.go` grows to match, so
the existing fakes drive every key with no device present.

### When controls are unavailable

Enabled only when stdin is a terminal (`term.IsTerminal`) and `RofiSelection`
is off. Otherwise `watchCast` runs exactly as it does today and says once that
controls need a terminal. `golang.org/x/term` is already vendored and already
used by `internal/update.go`; no new dependency.

### Three hazards, and their answers

1. **Pause versus the stall bound.** A paused device stops advancing its
   position, and the two-minute stall bound exists precisely to catch a
   position that stops advancing. It would kill a paused episode. The stall
   clock is therefore suspended while paused and reset on resume.
2. **Raw mode must always be undone.** It is registered through
   `RegisterExitCleanup`, which already exists for exactly this and already
   runs before `RestoreScreen`. In raw mode Ctrl+C raises no signal, so `0x03`
   is decoded explicitly and routed to the same teardown as `q`.
3. **The status line and `Out()` share a cursor.** Any real message clears the
   status line before printing and the status is redrawn after.

### Tests

Key decoding is a table test over byte sequences, including the three-byte
arrow escapes and a split escape sequence arriving across two reads. Command
handling is a table test driving `watchCast` through the existing fakes: pause
suspends the stall bound, `q` returns without marking the episode watched,
seek and volume reach the session. Raw-mode entry and exit are not unit
tested — they need a tty — and are covered by the hardware checklist.

---

## 3. A terminal for rofi launches

A cast started from the Hyprland keybind has no terminal, so `Out` becomes a
desktop notification and there is no way to pause. Rofi should hand the cast to
a terminal that can show the controls from section 2.

### Lifecycle

**The terminal owns the cast.** The rofi process resolves the episode, writes a
session file, spawns a terminal, and exits. The spawned process owns ffmpeg,
the HTTP server, the cast session, and all tracking. Closing the window ends the
cast and saves the position, exactly as Ctrl+C does today.

This is chosen over a detached background cast because **only one process may
ever track**. The parent exits before any tracking code runs, so two processes
cannot both append to `curd_history.txt` or push progress to AniList. A
detached model would need a rule for that, and a wrong rule corrupts a
viewer's history.

### Handoff

Flow, when `RofiSelection` and `-cast` are both set:

1. Resolve the episode exactly as now.
2. Write a session file.
3. Resolve a terminal command.
4. Spawn it running `otakase -cast-session <path>`, not waiting on it.
5. Exit.

The child reads the file, deletes it immediately, forces `RofiSelection = false`
so `Out` prints to its terminal rather than notifying, and runs the ordinary
cast path with controls enabled.

### The session file

**It needs its own struct.** `Episode.StreamReferrer`, `SubtitleURL`,
`StreamHeaders` and `Mode` are all tagged `json:"-"`, so marshalling `Anime`
would silently drop the referrer — and without the referrer the remux cannot
fetch the stream from the provider. The struct carries, explicitly:

- anime identity: `AnilistId`, `MalId`, `ProviderId`, `ProviderName`, title
- episode: `Number`, `Duration`, `Links`, `StreamReferrer`, `StreamHeaders`,
  `SubtitleURL`, `Mode`, `SkipTimes`, `Resume`, `Player.PlaybackTime`
- the chosen device name, so the child does not prompt again
- a schema version integer, so a child from a different build refuses a file it
  does not understand rather than misreading it

Written `0600` under the storage path: it contains a stream URL with an
authentication token. The child deletes it on read. Files older than an hour
are swept when a cast starts, so a spawn that never happened leaves nothing
behind.

### Resolving a terminal

In order: the `CastTerminal` config value; `$TERMINAL`; then the first of
`ghostty`, `kitty`, `alacritty`, `foot`, `wezterm`, `xterm` found on `PATH`.
The command is run as `<terminal> -e <otakase> -cast-session <path>`, which all
six accept.

`CastTerminal` is a new optional config key defaulting to empty. No new
required configuration.

### Failure handling

- **No terminal found, or the spawn fails.** Fall back to casting in the rofi
  process with no controls — today's behaviour exactly — and notify the viewer
  why there is no window. A missing terminal must never mean a missing episode.
- **The session file cannot be read or has an unknown version.** The child
  reports it and exits. The file is already deleted, so nothing is left.
- **The child dies.** Its own teardown runs: device stopped, ffmpeg killed,
  scratch directory removed, position saved.

### Tests

Terminal resolution is a table test over a stubbed PATH lookup covering each
tier of the order and the none-found case. The session file gets a round-trip
test asserting specifically that the referrer and headers survive — the failure
this design exists to avoid — plus a version-mismatch test and a sweep test for
stale files. Spawning itself is not unit tested; it is in the hardware
checklist.

---

## Verification

`docs/casting-verification.md` gains a rofi section: launch from the keybind,
confirm a terminal opens with controls, confirm every key works, confirm
closing the window stops the TV and saves the position, and confirm the
fallback path by setting `CastTerminal` to a command that does not exist.

The existing checklist still applies in full; controls and the handoff change
how a cast is driven, not what it does.

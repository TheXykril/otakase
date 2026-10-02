# Prompts in a rofi-spawned cast window

A cast started from a rofi keybind runs in a terminal window the viewer is not
sitting at. Carrying `TotalEpisodes` through the handoff made
`HandleLastEpisodeCompletion` reachable there for the first time, and with it
four interactive prompts that can now open a full-screen menu in that window and
wait for an answer nobody is there to give.

This is a record of what those prompts should do instead, and why. It is the
companion to [Casting Verification](Developer-Casting-Verification), which is how any of it gets checked
against a real device.

## Why the window cannot simply prompt

`RunCastSession` sets `config.RofiSelection = false` before anything else, so
the spawned window is always the Bubble Tea path, whatever the config says. Two
places branch on that flag, and in this window both resolve the same way:

| Site | Resolves to |
| --- | --- |
| `cast_controls.go` `castControlsBeginTerminal` | controls start: raw mode, one reader goroutine |
| `selection_menu.go` `dynamicSelectInternal` | menus draw as a full-screen TUI |

So the window is guaranteed to have a goroutine parked in `os.Stdin.Read` at the
same moment a Bubble Tea program starts its own reader on the same descriptor.
The bytes are split non-deterministically. Nothing is *misinterpreted* — every
one of these prompts fires between episodes, when no episode holds a
subscription, so decoded commands are dropped — but the keystrokes are consumed
before that check, and menu navigation loses roughly half of them.

The damage that matters more is not cosmetic. A prompt that blocks ends the
season: the window waits forever, the device sits on the finale's last frame,
and nothing explains why. A viewer who comes back to a cast that stopped for no
visible reason has been told less than a viewer who was told "skipped".

A suspend flag does not fix this. The reader is parked in `os.Stdin.Read` and
takes the next byte whatever its state, and consumed bytes cannot be pushed back
for a TUI to re-read. Suppressing the prompts is what removes the race, rather
than working around it.

## What is actually reachable

Enumerated from the cast session's two entry points, `castSessionEpisode` and
the advance it runs. Two entries that look reachable are not:

- **Audio sub/dub fallback** already does the right thing.
  `ResolveEpisodeURLAlternateModeWithPrompt` passes `ask = !AutoAudioFallback`,
  and `AutoAudioFallback` defaults to `true`, so the default path prints
  `No <mode> for episode N — playing <other>.` and continues without asking.
- **`promptAnimepaheEpisodeFallbackConsent` is dead code** — one reference in
  the tree, its own definition. Worth deleting, but it decides nothing.
- **`confirmUnairedEpisode`** is guarded inside `StartPlayback`, which the
  spawned session never calls.

That leaves four:

| Prompt | Site | When |
| --- | --- | --- |
| Provider match confirm | `provider_mapping.go` `confirmProviderMatch` | episode 2+, stale mapping |
| Playback recovery | `provider_mapping.go` `promptEpisodeLinkFailureRecovery` | episode 2+, resolve dead end |
| Rate this anime | `otakase.go` `HandleLastEpisodeCompletion` | season end |
| Sequel select / action | `otakase.go` `handleSequelCheck` | season end |

The sequel prompt is only ever reached at series end. `advanceDecision` makes
`Continue` and `SeriesFinished` mutually exclusive, so the next episode is
`StartNextEpisode`'s business with `castAwaitNextEpisode`'s countdown, and that
countdown already returns immediately when `castSeasonFinished` rather than
counting into a finale. "Sequel" here means a further instalment in the
franchise — AniList's `SEQUEL` relation, which for a show split across cours is
a separate media entry, so it often reads as "season 2" without being the season
machinery.

## The answers

Set on a per-run config flag, `CastNonInteractive`, tagged `config:"-"` the way
`CastToDevice` is so it stays out of the config file. `RunCastSession` sets it.

- **Provider match** — accept. A stale mapping is the common cause and refusing
  it ends an episode that would otherwise play. The alternative, "select
  manually", needs a menu that cannot be shown.
- **Playback recovery** — one automatic remap, then decline. This needs care:
  `resolveEpisodeLinksWithRecovery` is a bare `for {}` in which `"remap"`
  continues, so today only a human backing out ends it. An unconditional
  auto-remap would loop forever, which is a worse hang than the prompt. One
  attempt, mirroring the `attemptedAutoAudio` flag already in that loop, then
  back out with the diagnosis printed.
- **Rate this anime** — asked in the panel, with arrows and Enter. It started as
  a countdown and declined, which was wrong twice over: accepting it led to
  `RateAnime`, which opens *another* prompt for the score, and that prompt is
  exactly what a cast window cannot show. So the accept key produced no rating —
  and, because a cancelled prompt returned nil, a summary reading "rating
  saved". The countdown is now the score picker itself: ↑↓ move between 1 and 10,
  Enter saves, and only the window running out declines. Nothing else declines,
  so a stray keypress cannot rate or un-rate a show on the viewer's behalf.
  The ten seconds is for absence, not for answering. It exists because a window
  nobody is at has to resolve by itself rather than sit on a prompt forever, and
  it is not a budget for the question: a viewer shown a clock is being rushed, and
  a viewer told "9s to answer" and left alone reasonably concludes a countdown to
  being rated 8 rather than to losing the rating. So the first ↑↓ takes the clock
  off the panel and ends the deadline with it. From then on Enter saves, q declines
  — both named on the panel, so neither is a guess, and the footer changes to say
  so — and there is no way out but one of them.
  This needs the panel to be held for the whole cast, which is also what removed
  the alt-buffer blink between episodes.
- **Sequel** — decline. Declining is reported. A countdown here would be
  answering a question about a different show, which is not a thing to decide on
  the viewer's behalf.

Reporting matters as much as the answer. `HandleLastEpisodeCompletion` already
accumulates a `summary` and prints `Completion summary: …`, and
`handleSequelCheck` already returns summary strings. Every suppressed prompt
says what it assumed, because a silent default is the same failure shape as the
AniList repeat-count bug — a decision made on the viewer's behalf that they
never see.

## The safety net

The four above are handled at their call sites. Separately,
`dynamicSelectInternal` and `promptCancelable` refuse to draw when
`CastNonInteractive` is set, and return an error instead.

This is belt and braces on purpose. The inventory above was built by reading
call sites, and reading call sites is how `promptAnimepaheEpisodeFallbackConsent`
looked like a live 500 MB consent prompt in an earlier draft of this document
when it has never run. A prompt added later, on any path, degrades to declining
and a log line rather than hanging a window across the room.

## Known limits

- The rate countdown is a sibling of `castAwaitNextEpisode`, not a call into it.
  That helper is coupled to the cast panel, and `CastEpisode` releases the panel
  in a `defer` — by season end the alternate buffer is gone and the subscription
  released. The countdown has to take its own subscription to be cancellable and
  draw as ordinary terminal output.
- `provider_mapping.go` prints the recovery diagnosis with a bare `fmt.Println`.
  In raw mode `\n` does not return the cursor, which `Out` already guards
  against; the diagnosis prints diagonally. Fixed here, on the episode 2+ path
  where it is otherwise only reachable.
- None of this is verified against hardware. See [Casting Verification](Developer-Casting-Verification).

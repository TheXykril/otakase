# Changelog

## Unreleased

### Fixed

- **Watch history filed provider ids under the wrong provider.** Six history
  writes paired `anime.ProviderId` -- the id of whichever host actually served
  the episode -- with `GetProvider().Name()`, which is the *first configured*
  provider, not that one. Playing something anipub could not serve but anineko
  could stored anineko's slug under the name "anipub", and every later run then
  handed that slug straight back to anipub:

  ```
  anipub episode: invalid anipub show id "rich-girl-caretaker-im-secretly-..."
  ```

  which removed anipub from consideration for that show permanently. The name
  written now always belongs to the id beside it.

  Those corrupted pairings already exist in users' history files, so a stored id
  that fails is no longer believed: it is re-derived by searching once, and the
  working id replaces it. A stored id that works is still used directly, with no
  extra search.

- **A show could be reported as uncarried on the one provider that had it.**
  Mapping an anime onto a provider during playback searched the full AniList
  title once and gave up. Hosts shorten long titles: anineko carries *Rich Girl
  Caretaker: I'm Secretly the Caregiver of the Most Popular Girl in This Rich
  Kid School* as plain *Rich Girl Caretaker*, and answers nothing for the full
  name. So episode 10 was declared missing while anineko had it, and the only
  host consulted was anipub, whose catalogue for that show stops at episode 5.

  The same ordered title variants the initial search already used are now tried
  here too — exact titles first, simplified forms last, so a provider that
  answers the real title is never sent the broader guesses.

- **A failed episode lookup left curd spinning instead of stopping.** When no
  stream could be resolved, `StartCurd` returned an empty socket path — having
  already reported why — but the caller ran on and started the playback
  watchers for a session that did not exist. Those then polled a socket nothing
  would ever answer, once a second, indefinitely:

  ```
  Error getting playback time: no MPV IPC socket   (x60, and counting)
  ```

  The empty socket path is now treated as the failure it is. The polling loop
  also gained its own guard: its "MPV is gone" bail-out sat behind a
  CLI-only condition, so with `RofiSelection=true` it was never reached.

- **Choosing an anime from the poster grid failed with "error selecting
  anime".** The grid clips a label to the column width, so a long title came
  back from rofi shortened:

  ```
  written:  That Time I Got Reincarnated as a Slime Season 4 · 0/24 (21 aired)
  returned: That Time I Got Reincarn… · 0/24 (21 aired)
  ```

  The returned text was then matched against the full label to find the chosen
  show, which could not succeed — so the menu died on exactly the entries with
  the longest names. Rows are now addressed by index (`rofi -format i`) instead
  of by their text, which cannot drift from what was displayed.

  The row table is built as the menu is written rather than reused from the
  option list, because a row whose cover fails to download is skipped; indexing
  into the unfiltered list would have resolved every row after such a gap to its
  neighbour. Dismissing the picker now means "back" rather than an error.

- **Dual tracking (`TrackingRemote=anilist+myanimelist`) failed to launch.**
  Three defects compounded:

  The AniList write loop slept 350ms between updates; the MyAnimeList loop had
  no delay at all. A first dual sync is large — 180 entries on a real library —
  and firing those back to back gets the account rate limited. Both loops are
  now paced.

  MyAnimeList answers a rate-limited write with a redirect to `/error.json`,
  which never responds. Go followed it and re-issued the `PUT`, so throttling
  became a minute-long hang rather than an error. API requests no longer follow
  redirects: a REST API returning one is reporting a failure, not a new address.

  A single failed entry aborted the entire sync, and with it the launch. One
  anime that cannot be written is now logged and skipped, and the count of
  skipped updates is reported.

  The error was also undiagnosable: it carried no status code and, for a
  redirect, an empty body. It now names the status and the redirect target.

### Added

- **The list leads with what you were last watching.** The text menu applied no
  sort at all and took whatever AniList returned, while the poster grid sorted
  alphabetically — so neither surfaced the show you were part-way through, and
  the two menus disagreed with each other. Both now order by when the entry last
  changed, most recent first.
- **How long until the next episode.** AniList sends `timeUntilAiring` with every
  list fetch and curd stored it without ever reading it; only the episode number
  was used, for the "new episode" flag. Releasing shows now say when the next one
  lands, at no extra cost:

  ```
  Rich Girl Caretaker … · 9/12 (9 aired) · next in 22h
  That Time I Got Rein… · 0/24 (21 aired) · next in 6d
  ```

  The countdown appears in the text menu only. A poster label is clipped at the
  column width, and "next in 22h" costs about fourteen of its thirty-seven
  characters — more than a third of the title — which is a poor trade when the
  cover already identifies the show. Resume points still show in both: they are
  shorter, and they are something to act on now.
- **Resume points.** Where the local history shows you stopped part-way through
  an episode, the row says so: `· resume 11:40`. Shown only when you are
  meaningfully in — past a minute and not yet 95% through — so it stays signal
  rather than appearing on every row. A resume point takes precedence over the
  airing countdown: one is something to act on now, the other is something to
  come back for, and showing both would crowd the row.

## 2.3.0 — 2026-09-05

### Added

- **The menus follow Omarchy's own design language.** Curd's rofi themes are now
  built from the tokens in the active theme's `shell.toml` — the same contract
  Omarchy's bar, dropdowns and menus use — so curd looks like part of the desktop
  rather than a separate application:

  - an opaque card on a light (0.5) scrim, hairline border on every side, small
    radius, matching `[menu]`;
  - a selected row marked by an 8% foreground fill with its own 25% outline and
    accent-coloured text, rather than a heavy accent band;
  - no prompt chip: a bare field with placeholder text and a hairline beneath;
  - the system monospace family, via `fc-match`, which follows `omarchy font set`
    and still works with no Omarchy installed.

  The poster grid is now a card too, not a fullscreen surface. Omarchy's scrim is
  deliberately light because its menus are opaque cards on top of it; rendering
  covers straight onto that scrim let the wallpaper compete with the artwork.

- **Poster labels keep their episode counts.** The grid clips a label at the
  column width, and a long anime title consumed the whole line, so the counts —
  the one thing a cover cannot tell you — were what disappeared. The title now
  absorbs the truncation and the counts stay pinned at the end:

  ```
  That Time I Got Re…            · 0/24 (21 aired)
  From Old Country Bumpkin to M… · 4/12
  Uzaki-chan Wants to Hang Out!  · 3/12
  ```

  The budget is measured against the layout by rendering a character ruler, not
  derived from column arithmetic — a first estimate that way was off by a third.
  A test pins the layout to the measured value so the two cannot drift apart.

### Fixed

- **Poster grid rows were not pango-escaped.** Only rows flagged as having new
  episodes went through the markup builder, so an ordinary title containing `&`
  broke its own row, and the episode counts were never dimmed.

### Added

- **Episode counts in the anime list.** Rows showed only a title, so knowing how
  far through a show you were, or whether anything new had aired, meant opening
  it. Every number was already on the AniList entry.

  The denominator is always the season total, so a fraction means the same thing
  on every row, and the aired count appears only while a show is still releasing
  and differs from the total:

  ```
  That Time I Got Reincarnated as a Slime Season 4 · 0/24 (21 aired)
  Rich Girl Caretaker: I'm Secretly the Caregiver … · 9/12 (9 aired)
  Uzaki-chan Wants to Hang Out!                     · 3/12
  ```

  Shows AniList has no episode count for fall back to `6 watched`, or
  `1100/1177 aired` when the aired figure is known. Counts render dimmed after
  the title, so they never compete with it.

### Fixed

- **A rate-limited provider was reported as though the show did not exist.**
  anipub answers HTTP 200 with `{"error":"Too many requests, ..."}` when it
  throttles, which fell through to a generic parse failure. Nothing recognised
  it as temporary, so it was not retried, did not trip the provider cooldown,
  and told the user their anime was not carried. Throttling is now detected,
  reported as the host being unavailable, and retried.
- **The config file is written atomically.** It holds tracker credentials and is
  rewritten on most runs, but was truncated in place with no lock, so a crash or
  two concurrent runs could mangle it. A real config in the wild ended up
  containing `nimeListClientID` — `MyAnimeListClientID` with its first three
  characters gone. Writes now go to a temporary file and are renamed into place.
- **Unrecognised config options are reported instead of silently ignored.** A
  typo such as `SkipOP=true` did nothing at all, with no indication why. Unknown
  keys are now named on startup with the closest valid option suggested, and are
  never deleted, since they may belong to another curd the user runs.

### Added

- **A scheduled provider canary** (`.github/workflows/provider-canary.yml`). The
  repo already had live tests covering real providers, but they are gated behind
  environment variables and CI ran `go test -short`, which skips every one — so
  provider rot was only ever discovered by users. The canary runs them weekly,
  probes every provider, and opens or updates an issue when one breaks. It
  retries once after a pause, because a canary that cries wolf over a brief
  throttle gets ignored.

  It also reports when upstream (`Wraient/curd`) has commits this fork does not,
  so upstream fixes surface on their own rather than depending on someone
  remembering to look. That is filed separately from provider health and never
  fails the run: commits existing upstream is information, not a fault.

## 2.2.0 — 2026-09-04

### Added

- **Episode downloading** (`curd -download`), the most-requested missing feature
  upstream (Wraient/curd#104, #55). It reuses the whole existing selection flow
  and saves instead of playing: `-episodes 1-12` for a range, `-download-dir` or
  the `DownloadDir` config for the destination. Streams are remuxed to MP4 rather
  than re-encoded, so a download costs bandwidth and almost no CPU with no quality
  loss, and soft subtitles are muxed in where the provider supplies them. An
  episode already on disk is skipped without a provider round trip, and a failed
  download is deleted rather than left looking complete.

  Providers routinely disguise HLS segments as images (`…/seg-1-f1-v1-a1.jpg`),
  which ffmpeg's demuxer rejects by default; the extension check is relaxed so
  those streams download correctly.
- **Desktop theming.** On [Omarchy](https://omarchy.org/) the active theme's
  `colors.toml` is read from `~/.local/state/omarchy/current/theme/` and drives
  both the terminal menus and the rofi menus, so Curd matches the rest of the
  desktop. Configurable with `Theme=auto|omarchy|builtin`. Colours are chosen by
  measured contrast rather than assumed: the highlighted row falls back to the
  accent when a theme's selection colour is too close to its background to see,
  and text on a filled accent is picked light or dark to stay readable.
- **Redesigned rofi menus**, now generated from the palette and embedded in the
  binary rather than downloaded from GitHub on first run. The menus work offline,
  a theme change is picked up automatically, and a hand-edited `.rasi` is copied
  to `<name>.rasi.user-backup` before being replaced.

  The design is a flat plane anchored by a single accent rail, rather than a
  floating rounded card: radius is applied only at the outermost level so it
  reads as hierarchy, rows run full width with a filled selection band, and one
  hairline separates what you type from what you are choosing between.

  Sizing follows a modular type scale (11 / 13 / 16, roughly 1.2x) and a 4/8
  spacing rhythm, replacing ad-hoc values. The list menus are wider so a long
  anime title is not clipped, and the poster grid declares the four columns rofi
  actually renders rather than a number it silently ignores.

  Rows also carry typographic hierarchy now. `One Piece · 1171 eps [anipub]` was
  rendered at one weight, so a list was an undifferentiated wall of text; the
  title is now full strength and the episode count and provider are dimmed, so
  the eye scans titles down the left edge.

### Fixed

- **`curd -e` failed for anyone whose `$EDITOR` carries arguments.** The whole
  environment variable was passed to `exec.Command` as the executable name, so
  `EDITOR="omarchy-launch-editor --inline"` (Omarchy's default) produced
  `executable file not found in $PATH`. The same broke `code --wait`, `subl -w`
  and any other editor invoked with a flag. The value is now split the way a
  shell would, honouring quotes so a path containing spaces survives, and
  `$VISUAL` takes precedence over `$EDITOR` per convention.

  The failure was also invisible: `CurdOut` sends to a desktop notification when
  `RofiSelection` is on, so the terminal the user was looking at stayed silent.
  `-e` now reports to the terminal it runs in, as `-u` already did, and an editor
  that is not on `PATH` gets a message saying so instead of a raw exec error.
- **Playback polling loops now stop when MPV exits** (upstream Wraient/curd#58,
  "Pipe Status Spam"). Three loops polled the IPC socket and treated a dead
  connection as a transient error: the Discord presence loop retried every 5s
  forever, the CLI next-episode branch every 1s, and the playback-status check
  logged and re-polled because "MPV has exited" was handled as an inconclusive
  error rather than as "nothing is playing". Each now recognises a gone
  connection and stops. This is the other half of the empty-socket fix in 2.1.0,
  and the cause of the multi-megabyte logs and the Windows terminal flood.
- **A stacked search no longer waits on the slowest provider.** It waited on all
  of them, so animepahe's ~17s browser challenge set the pace even when anipub
  had answered in 0.2s. A straggler now gets a 2.5s grace window once another
  provider has produced results, with a 20s overall deadline while nothing has
  succeeded. Healthy providers are unaffected.
- **Failures are grouped by cause.** Every provider error used to be
  concatenated onto one line, burying whether the hosts were down or simply did
  not carry the show. Clean misses now collapse onto a single line and the
  message says which situation it is.
- **Providers that just failed are no longer re-probed every search.** Two
  consecutive unreachable failures put a provider on a 5 minute cooldown. A
  provider answering "no results" is working correctly and is never cooled down.

- **Anime titles containing `&`, `<` or `>` no longer break their rofi row.**
  Curd passes `-markup-rows`, so labels are parsed as pango markup, but they were
  emitted unescaped. Titles are escaped now, and selections are unescaped on the
  way back so they still match the option they came from.

### Performance

- `Log()` kept the log file open instead of reopening it for every line, which
  cost an open/write/close syscall triple per entry. Writes stay unbuffered.

## 2.1.0 — provider resolution fixes

Fork of [Wraient/curd](https://github.com/Wraient/curd) at v2.0.7. Drop-in
replacement: same binary name, config path, and stored tokens.

### The reported symptom

Curd could not find any anime. A real `debug.log` showed three independent causes
firing at once for a single search:

```
Provider senshi search failed:  Post "https://senshi.live/anime/filter": EOF
Provider anipub search failed:  no results for "Saijo no Osewa: Takane no Hanadarake na ..."
Provider anineko search failed: context deadline exceeded (Client.Timeout exceeded while awaiting headers)
Provider search failed: all provider searches failed
```

### Fixed

**Search used the romaji title only.** `curd.go` seeded every provider search with
`anime.Title.Romaji`, ignoring the configured `AnimeNameLanguage` and never trying
another title. AniList supplies *"Saijo no Osewa: Takane no Hanadarake na Meimonkou
de, Gakuin Ichi no Ojou-sama (Seikatsu Nouryoku Kaimu) wo Kagenagara Osewa suru
Koto ni Narimashita"*; anipub indexes the same show as *"Rich Girl Caretaker"* and
matches it instantly. Searches now walk an ordered list of title variants —
preferred language first, then the other titles, then simplified forms with
parentheticals, season suffixes, and subtitle clauses removed — stopping at the
first that returns results. A query the user types is never overridden.

**senshi.live is a parked domain.** The domain lapsed and now serves a
domain-broker page over HTTP while aborting the TLS handshake entirely. It was
first in `preferredProviderOrder` *and* the hardcoded return value of
`firstEnabledProviderName()`, so every search began by waiting on a host that can
no longer answer. Retired, with the fallback now derived from what is actually
registered and enabled.

**One slow provider sank the whole search.** Providers were searched sequentially
against a shared 15s client timeout with no retries, so a single stall consumed the
budget and the run reported "all provider searches failed". Providers are now
searched concurrently and transient failures (timeouts, resets, truncated
responses) are retried once. A definitive "no such show" answer is never retried.
Results are still merged in configured stack order.

**anipub's newer episode links did not resolve.** anipub serves two link shapes:
the legacy `/video/{embedId}/{mode}` and a newer `/play/{malId}/{ep}/{mode}` that
fronts a different megaplay route. Only the first was handled, so newer shows
failed with `unsupported video link` after searching and listing correctly. Both
shapes are resolved now.

**AllAnime listed shows that could never play.** Its episode endpoint now rejects
unsigned requests with a `AA_CRYPTO_MISSING` GraphQL error (HTTP 200, null
episode), while catalogue search still answers. The failure surfaced as the
unhelpful "no encoded Allanime provider sources found". It is now detected and
reported explicitly, and AllAnime is disabled by default so it cannot offer
unplayable results. `allanime.day` is additionally serving a 301 redirect loop, so
there is no live frontend to derive the signing scheme from.

**A failed MPV launch spun a hot loop.** When MPV never came up, an empty IPC
socket path was still polled, and each poll retried three times against
`dial unix: missing address` — producing multi-megabyte logs. Missing socket paths
now fail immediately and count as a closed session, so pollers stop.

**Background prefetch could race and leak.** `prefetchAfterPlaylistSwitch` ran as
an untracked goroutine writing `anime.Ep.NextEpisode` without holding the
controller lock, and kept doing network work after its caller had moved on. It is
now tracked, awaitable via `WaitForPrefetch`, publishes under the lock, and skips
entirely when there is no MPV socket to feed.

### Added

- `curd -provider-status` probes every registered provider concurrently and reports
  which work, which are disabled and why, and how long each took. Exits non-zero
  when nothing answers. Use `-provider-status-query` to probe with a specific title.
- **anidb** provider (`anidb.app`), the host ani-cli moved to for its v5 provider.
  anidb.app was serving a site-wide maintenance page throughout development, so
  this provider is written against ani-cli's documented endpoint shapes and **has
  not been verified against live traffic**. It is disabled by default and guarded
  by explicit maintenance and Cloudflare detection; add `anidb` to `Provider` to
  try it.

### Changed

- Default provider order is now `anipub, anineko, anidb, senshi, allanime,
  animepahe`, ranked by what currently resolves streams end to end.
- Shared HTTP client: 25s timeout (was 15s), with explicit TLS handshake and
  response-header timeouts and a larger connection pool for concurrent search.
- The built-in updater and rofi theme fetch point at this fork, so a future
  upstream release cannot replace this build with the unfixed one.

### Verified

- `go test -short -race ./...` passes clean.
- Live search regression (`CURD_LIVE_SEARCH_TEST=1`) confirms the reported show
  resolves, and that anipub — which indexes only the English title — is rescued by
  the variant fallback.
- Live stream resolution confirmed for anipub and anineko on both link shapes.

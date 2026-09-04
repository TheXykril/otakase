# Changelog

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

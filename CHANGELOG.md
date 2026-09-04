# Changelog

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

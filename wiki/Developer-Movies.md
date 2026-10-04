# Movies (experimental)

Movies are an experimental feature, off unless `ExperimentalMovies=true`. They
live apart from the anime code on purpose: AniList and MyAnimeList do not list
non-anime movies, so nothing in the anime search, provider stack or tracking
knows about them.

| Piece | Where |
|---|---|
| Site client, search, movie pages | `internal/movies/site.go` |
| Video hosts (Streamtape, Doodstream) | `internal/movies/hosts.go` |
| History file (`movies.json`) | `internal/movies/store.go` |
| Menus and the mpv loop | `internal/movies_menu.go` |
| Main menu entry (`MOVIES`, `^x`) | `menuActions` in `internal/menu_layout.go`, added by `mainMenuLayout` when the option is on |

## The site

The source is a Lithuanian WordPress site on the DooPlay theme. It has no fixed
address: it moves between bare IP addresses. `MovieSite` (default
`https://8filmai.eu`) answers every request with a 301 to the current address,
keeping the path. The client asks it once, stores the address it was sent to
in `movies.json`, and goes there directly until a request fails; then it asks
`MovieSite` again and retries once.

- Search: `/?s=<query>`. Results are `<article>` blocks; only links under
  `/filmas/` are kept, so series (`/serialai/`) are left out.
- Movie page: `h1.h1e` is the title, the line under it the original title,
  `span.date` the year, and `var imid='tt…'` the IMDb id.
- Streams: a plain `var data='s0|s1|s2|s3|subs'` line sits in front of the
  page's obfuscated player script. Series pages use `1=…;2=…;` instead and are
  rejected as not a movie.

## Servers

| Field | Player link | Host |
|---|---|---|
| s3 | `/p2.php?str=<id>` | Streamtape (`streamtape.com/e/<id>`) |
| s2 | `/p2.php?d=<id>` | Doodstream (`dood.pm/e/<id>`, domain changes) |
| s1 | `/p2.php?n=<id>` | `player.eltitbus.xyz/f/<id>`, a Netu/HQQ clone behind a captcha; not supported |
| s0 | `https://imgsto.re/files/<id>` | a player page; the `.mp4` link is a base64 string (of the URL-escaped link) in its script |

`p2.php` is a page holding one iframe to the host. Servers are tried in the
order above, each started in mpv and given `MpvPlaybackStartTimeout` to begin
before the next is tried.

- **Streamtape**: the embed page writes the file link into `#robotlink` from a
  literal plus a string cut by `.substring(n)` calls (other lines are decoys).
  The link is IP-bound, so it must be resolved on the machine that plays it.
- **Doodstream**: the embed page has a `/pass_md5/…` path. Its response is the
  start of the file link; the player adds 10 random characters,
  `?token=<last path part>&expiry=<ms>`. The file needs the embed host as
  referrer.

Both hosts change their pages every few months; when a server stops working,
the extractor in `hosts.go` is the first place to look. The log
(`otakase-debug.log`) has a `movies:` line for every server tried.

## Tracking

`movies.json` in the storage directory, keyed by the page path. It keeps the
position, duration, watched and watchlist flags. A movie counts as watched at
`PercentageToMarkComplete`. The position is saved every 15 seconds and when
the player closes. Nothing is sent anywhere.

## Testing

`go test ./internal/movies/` runs offline against markup cut from the site.
`OTAKASE_LIVE_MOVIES=1 go test ./internal/movies/ -run Live` searches the real
site and resolves a stream. The cloud CI cannot reach the site, so run it
locally.

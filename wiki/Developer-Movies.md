# Movies (experimental)

Movies are an experimental feature, off unless `ExperimentalMovies=true`. They
live apart from the anime code on purpose: AniList and MyAnimeList do not list
non-anime movies, so nothing in the anime search, provider stack or tracking
knows about them. Series are handled here too: a `movies.Movie` with `Series`
set, played an episode at a time.

| Piece | Where |
|---|---|
| Provider interface, 8Filmai adapter | `internal/movies/provider.go` |
| vidsrc (English) and OpenSubtitles | `internal/movies/vidsrc.go` |
| Filmukas (Lithuanian, family) | `internal/movies/filmukas.go` |
| 8Filmai site client, search, movie pages | `internal/movies/site.go` |
| Video hosts (Streamtape, Doodstream, imgsto.re) | `internal/movies/hosts.go` |
| History file (`movies.json`) | `internal/movies/store.go` |
| Menus | `internal/movies_menu.go`, series in `internal/movies_series.go` |
| Playing (mpv, cast), next episode, download | `internal/movies_play.go` |
| Provider choice (`MovieProvider`) | `internal/movies_providers.go` |
| Trakt client (device sign-in, sync) | `internal/movies/trakt.go` |
| Trakt menu and sync hooks | `internal/movies_trakt.go` |
| Main menu entry (`MOVIES`, `^x`) | `menuActions` in `internal/menu_layout.go`, added by `mainMenuLayout` when the option is on |

## Providers

`MovieProvider` picks where searches go; `^o` in the Movies menu changes it
and saves it. The default, `all`, asks every provider at once
(`movieLibrary.search`) and lists the results grouped by provider, each row
tagged with it; a provider that fails is logged and left out. A movie in the
history is always opened on the provider it came from. A provider implements
`movies.Provider`: `Search` returns films and series, and `Open` fills in a
film's details and lists `Source`s, each resolved only when tried. A provider
with series also implements `movies.SeriesProvider`: `Episodes` lists them
and `OpenEpisode` lists one episode's sources. vidsrc and 8Filmai have
series; Filmukas lists each episode as a page of its own with no way to group
them, so its series are left out.

### vidsrc (English)

- Search: IMDb's keyless suggestion list,
  `https://v3.sg.media-imdb.com/suggestion/x/<query>.json`; `movie` and
  `tvMovie` items are films, `tvSeries` and `tvMiniSeries` series. The IMDb id
  is the path.
- Episodes: vidsrc has no list, so Trakt's is used
  (`/shows/<imdb>/seasons?extended=full,episodes`, no sign-in), aired ones
  only, specials left out.
- Streams: `https://data.vidsrc.sh/api.php?type=movie&imdb=<tt…>&stream_urls`
  (the API behind vidsrc.to's player); an episode is
  `type=tv&imdb=<show>&season=<n>&episode=<n>`. `data.stream_urls` is base64 of
  nonce + ciphertext; `vs.wasm_url` is a WebAssembly module with the key, which
  changes every five minutes. The module imports nothing and is run with
  wazero: `alloc(len)`, write the bytes, `decrypt(ptr, len)` returns the plain
  length, and the text starts 12 bytes in, one HLS link per line.
- Each link needs `?token=` from `<link origin>/generate.php`, a JWT tied to
  the caller's IP, so resolve on the machine that plays.
- Subtitles: OpenSubtitles' keyless REST search
  (`rest.opensubtitles.org/search/imdbid-<7 digits>/sublanguageid-<code>`, an
  episode `episode-<n>/imdbid-<show>/season-<n>/…`, header
  `X-User-Agent: trailers.to-UA`), preferring the file named like the API's
  `file_name`. The language is the one picked in the movie's menu
  (`Entry.SubtitleLanguage`), else `SubsLanguage`, as its ISO 639-2 code
  (`providers.ThreeLetterCode`); up to three in it, then two English ones when
  it is another language. Files come gzipped; they are unpacked to the temp
  folder with advert cues (OpenSubtitles' own, and cues that are only a web
  address) taken out.

### Filmukas (Lithuanian, family films)

- Search: `https://www.filmukas.com/visi?q=<query>`, matching Lithuanian
  titles. Results are `<li id="li_N">` items; `/serialai/` links are left out.
- A film page has `loadScript('/@videojs-v5.js?id=…&z=…')`. That script,
  fetched with the page as referrer, holds `videoSrc = '…/master.m3u8?d=<token>'`
  on the site's own CDN. The token is short-lived, so it is fetched only when
  the source is tried. The playlist needs the site as referrer; the dubs are
  audio tracks, and `--alang=lt` picks the Lithuanian one. `videoSrc` can also
  be a YouTube link, which mpv plays through yt-dlp.

### 8Filmai (Lithuanian)

The source is a Lithuanian WordPress site on the DooPlay theme. It has no fixed
address: it moves between bare IP addresses. `MovieSite` (default
`https://8filmai.eu`) answers every request with a 301 to the current address,
keeping the path. The client asks it once, stores the address it was sent to
in `movies.json`, and goes there directly until a request fails; then it asks
`MovieSite` again and retries once.

- Search: `/?s=<query>`. Results are `<article>` blocks linking under
  `/filmas/` (films) or `/serialai/` (series). A series has a page per season,
  `/serialai/<name>-<n>-sezonas-online/`, so each season is found and kept
  as its own entry.
- Movie page: `h1.h1e` is the title, the line under it the original title,
  `span.date` the year, and `var imid='tt…'` the IMDb id.
- Streams: a plain `var data='s0|s1|s2|s3|subs'` line sits in front of the
  page's obfuscated player script. A season page has
  `var data='1=s0|s1|s2|s3|lang;2=…;'` instead: each episode's ids, read by
  `parseSeriesData`.

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

- **imgsto.re** (server 0): `/files/<id>` is a player page; the `.mp4` link is
  a base64 string of the URL-escaped link inside its script.
- Server 1 (`player.eltitbus.xyz`) is a Netu/HQQ clone behind a captcha and is
  not supported.

The hosts change their pages every few months; when a server stops working,
the extractor in `hosts.go` is the first place to look. The log
(`otakase-debug.log`) has a `movies:` line for every server tried.

## Tracking

`movies.json` in the storage directory, keyed by `provider:path` (entries
from before providers existed are 8Filmai's and are rekeyed when read). It
keeps the position, duration, watched and watchlist flags, a rating out
of 10, and the server it last played from, which is tried first next time
(`movies.PreferServer`). List rows show the provider and server.
A series' entry also keeps the episode last played (`Season`, `Episode`) and
whether it was finished (`EpisodeDone`); the position is that episode's. It
stays in Continue until its last episode is finished. Playing a series goes
on to the next episode when one finishes, after asking when
`NextEpisodePrompt` is on.
After a play, `afterMoviePlay` puts a film or series stopped part way on the
watchlist, and asks for a rating when one was just finished and
`ScoreOnCompletion` is on. Finishing a film from one provider takes the same
film from the others off Continue and the watchlist (`movies.SameMovie`: the
same IMDb id, else a shared title and year).

The movie menu shows Trakt's details (`/movies/<imdb>?extended=full` or
`/shows/…`, no sign-in): runtime, genres, age rating, Trakt rating and the
plot, in rofi's message bar or above the terminal menu.

### Trakt

otakase has its own Trakt app (`movies.TraktClientID`, a PKCE app with no
secret; redirect URI `https://thexykril.github.io/otakase/`, never visited).
`TraktClientID`/`TraktClientSecret` in the config swap in another app. After a
sign-in from the Movies menu (`^t`, Trakt's device flow: a code entered at
trakt.tv/activate, with the Client ID alone), the
store's `OnChange` hook sends Trakt each change to watched, watchlist and
rating (`/sync/history`, `/sync/watchlist`, `/sync/ratings` and their
`/remove`); a series goes under `shows`, and its history is sent an episode
at a time, never the whole series. When playback starts, `/scrobble/start`
shows it as watching now; stopping part way sends the position to
`/scrobble/pause`, and finishing sends `/scrobble/stop` at 100%, which adds the
play to the history (so `/sync/history` is not sent for it as well). Movies are matched by IMDb id. One without an id is identified first and the
id kept: an 8Filmai page has it (`var imid`); otherwise (Filmukas, which
gives the English title and year in its description and JSON-LD) Trakt's
`/search/movie` is asked with `fields=title,translations,aliases`, so a
Lithuanian title matches too. Trakt ignores its `years` filter there, so the
year is checked in otakase (same year, or one off); no match is skipped and
logged rather than syncing a namesake. The
token is `trakt_token.json` in the storage directory, renewed with its
refresh token when it runs out. Local history stays the source of truth;
Trakt failures are only logged. A movie counts as watched at
`PercentageToMarkComplete`. The position is saved every 15 seconds and when
the player closes. Nothing is sent anywhere.

## Testing

`go test ./internal/movies/` runs offline against markup cut from the site.
`OTAKASE_LIVE_MOVIES=1 go test ./internal/movies/ -run Live` searches the real
site and resolves a stream, and does the same on vidsrc and Filmukas. Streams are tied to
the address that resolved them, so run it on the machine that plays.

## Cast and download

Casting goes through `CastEpisode` with the movie dressed as an untracked
one-episode show; `Anime.Movie` carries the start position and a callback that
saves progress to `movies.json`. Under rofi it casts in-process rather than
handing off to a terminal, which would lose that callback. Downloads use the
anime download's ffmpeg job with `File` set for hosts whose links have no
extension, and are named `Title (Year).mkv`, or `Title - S01E03.mkv` for an
episode, in `DownloadDir`.

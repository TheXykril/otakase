# Configuration

Edit with `otakase -e`. The file lives at `~/.config/otakase/otakase.conf`.

| Option | Type | Values | Description |
|---|---|---|---|
| `Player` | String | any mpv-compatible binary | Playback binary. Falls back to `mpv` if missing. |
| `MpvArgs` | List | e.g. `["--fullscreen=yes"]` | Extra arguments passed to mpv. |
| `MpvPlaybackStartTimeout` | Integer | `1`–`600` seconds | How long to wait for playback before trying elsewhere. Default `20`. |
| `MpvEpisodePlaylist` | Boolean | `true`, `false` | Fill mpv's playlist with the other episodes once playback is stable. Reach it with `Ctrl+P`. Default `true`. |
| `SaveMpvSpeed` | Boolean | `true`, `false` | Carry playback speed to the next episode. |
| `StoragePath` | String | any path, `$VARS` expanded | Where Otakase keeps its data. |
| `DownloadDir` | String | any path | Where `-download` saves episodes. |
| `DownloadFormat` | Enum | `mkv`, `mp4` | Container for downloads. `mkv` (the default) keeps every audio track, ASS subtitles and their fonts; `mp4` suits devices that play nothing else. Each episode also gets a small `.otakase.json` naming the show, episode and skip times. |
| `CastDevice` | String | a device name | Cast to this device without asking, when `-cast` is given and the device is found. Empty asks each time. |
| `CastTerminal` | String | a terminal emulator | Terminal opened for a cast started from rofi. Empty uses `$TERMINAL`, then whatever is installed. |
| `CastPort` | Integer | `0`–`65535` | Port the cast stream server listens on. `0` (the default) picks a free one; fix it to allow a single port through a firewall. |
| `CastDiscoveryPort` | Integer | `0`–`65535` | UDP port device discovery listens for TV answers on (default `8011`), so one firewall rule lets them in. `0` picks a random one. |
| `KodiHost` | String | `host` or `host:port`, comma-separated | Kodi instances to offer for casting when discovery does not find them. Port defaults to `8080`. |
| `KodiUser` | String | any | User name of Kodi's web server, when it asks for one. |
| `KodiPassword` | String | any | Password of Kodi's web server, when it asks for one. |
| `CastBurnSubtitles` | Boolean | `true`, `false` | Burn subtitles into the picture when casting. Costs a re-encode. Default `true`. |
| `CastEncoder` | Enum | empty, `vaapi`, `software` | Encoder for burned subtitles. Empty detects what this machine can do. |
| `CastNextEpisode` | Enum | `countdown`, `stop` | When a cast episode ends: `countdown` (the default) counts down 10 seconds on the panel and plays the next one, any key stops it; `stop` ends the cast with the video. |
| `SubOrDub` | Enum | `sub`, `dub` | Preferred audio. Switching a show to the other audio (the player's `(DUB)`/`(SUB)` playlist row, or `a` while casting) is remembered for that show; `-sub` and `-dub` still win for a run. |
| `SubStyle` | Enum | `ask`, `soft`, `hard` | External or burned-in subtitles, where both exist. `ask` prompts once and remembers. |
| `SubsLanguage` | String | `english` | Preferred subtitle language, as a name or code (`english`, `pt`, `spa`). Switching language in mpv (the `j` key) is remembered for that show in `show_prefs.json` and wins over this. |
| `Quality` | String | `best`, `1080`, `720`, `480`, or any number of lines | Picture height for HLS streams, in mpv, casts and downloads. `best` (the default) plays the provider's stream untouched; a number picks the nearest variant at or below it, else the smallest above. Set a different one per show from the update menu (*Quality for this show*); it is kept in `show_prefs.json`. |
| `AutoAudioFallback` | Boolean | `true`, `false` | Play the other language when a show is carried in only one, instead of asking. Default `true`. |
| `AnimeNameLanguage` | Enum | `english`, `romaji` | Preferred title language. |
| `PercentageToMarkComplete` | Integer | `0`–`100` | Watched percentage that counts as complete. |
| `NextEpisodePrompt` | Boolean | `true`, `false` | In mpv, ask before playing the next episode; `false` plays it straight away. Casting has `CastNextEpisode` instead. |
| `ScoreOnCompletion` | Boolean | `true`, `false` | Prompt to rate a show when you finish it. |
| `SkipOp` / `SkipEd` | Boolean | `true`, `false` | Skip openings and endings where timings exist. |
| `Theme` | Enum | `auto`, `omarchy`, `builtin` | Which colour palette to use. `auto` follows the desktop on Omarchy. |
| `ThemeOverrides` | String | `name:#hex` pairs | Replaces named colours on top of the palette in use. See [Theming](#theming) below. |
| `ContributeSkipTimes` | Boolean | `true`, `false` | Bind the player keys for marking, submitting and voting on skip times — see [Fixing the times yourself](Skip-Times#fixing-the-times-yourself). |
| `IntroDBSkipTimes` | Boolean | `true`, `false` | Ask theintrodb for openings and endings the other sources do not know. Downloads a public id mapping in the background the first time it is needed. |
| `AnimeSkipClientID` | String | `auto`, or an Anime-Skip client id | Adds Anime-Skip as a source of skip timings — see [Adding Anime-Skip](Skip-Times#adding-anime-skip). `auto` uses the public shared id. Empty by default. |
| `SkipFiller` / `SkipRecap` | Boolean | `true`, `false` | Skip filler episodes and recap sections. `SkipRecap` also skips a "previously on" stretch inside an episode when AniSkip has one on file. |
| `DiscordPresence` | Boolean | `true`, `false` | Discord Rich Presence. |
| `DiscordClientId` | String | Discord application id | Application Rich Presence reports as. |
| `ShowNewEpisodes` | Boolean | `true`, `false` | Mark shows with an unwatched aired episode in the list. Default `true`. |
| `RofiSelection` | Boolean | `true`, `false` | Use rofi for selection menus. |
| `ImagePreview` | Boolean | `true`, `false` | Poster previews in rofi. |
| `VimKeys` | Boolean | `true`, `false` | `j`/`k`/`h`/`l` to move and `/` to search in menus, instead of type-to-filter. |
| `AlternateScreen` | Boolean | `true`, `false` | Use an alternate screen buffer for a cleaner terminal. |
| `CurrentCategory` | Boolean | `true`, `false` | Open straight into your watching list, skipping the menu; the tabs and bottom bar still reach everything. Escape then quits. Terminal only. `-current` does the same for one run. |
| `MenuOrder` | String | comma-separated | Which menu entries appear, and in what order. Choose from `CURRENT`, `ALL`, `UNTRACKED`, `UPDATE`, `REMAP_PROVIDER`, `CONTINUE_LAST`, `SURPRISE`, `PLANNING`, `COMPLETED`, `PAUSED`, `DROPPED`, `REWATCHING`, `TRACKER`, `PROVIDER`, `CAST`, `STATS`. `TRACKER` is always added if left out. Entries added in a new release are added to a saved `MenuOrder` once, on upgrade. |
| `ContinueWatchingRows` | Integer | `0` or more | How many recently played shows open the menu. Default `5`; `0` hides them. |
| `Provider` | List | `stacked`, or a single-entry list | Which sources to search and in what order. `stacked` (the default) uses the preferred order with fallback; naming one restricts the search to it. |
| `DisabledProviders` | List | provider names, e.g. `["nyaa"]` | Sources never searched, even under `stacked`. |
| `ManualProviderSearch` | Boolean | `true`, `false` | Always choose the match yourself instead of matching automatically. |
| `TrackingRemote` | Enum | `none`, `anilist`, `myanimelist`, `anilist+myanimelist` | Which tracker to sync with. |
| `MyAnimeListClientID` | String | MAL OAuth client ID | Used for MyAnimeList sign-in. |
| `MyAnimeListClientSecret` | String | MAL OAuth secret | Optional; used for sign-in and token refresh. |
| `CheckUpdates` | Boolean | `true`, `false` | Check for a newer release in the background at launch, and offer it before the first menu. Default `true`. |
| `DevBuilds` | Boolean | `true`, `false` | Also offer dev builds (the `dev` pre-release on GitHub, built from unreleased work on `main`) when one is newer than the latest release, in the update check and in `-u`. They can be broken. Default `false`. |
| `AdultContent` | Boolean | `true`, `false` | Show 18+ titles (AniList "adult", MyAnimeList rated explicit) in your lists, Surprise Me and AniList search (continue-watching rows only with `ContinueWatchingAdult`). When `false` they are hidden but stay on your tracker and keep syncing. Provider search when watching untracked is not filtered. Default `false`. |
| `ContinueWatchingAdult` | Boolean | `true`, `false` | Show 18+ titles in the continue-watching rows that open the menu. Needs `AdultContent=true`. Default `false`, so they stay off the first screen. |
| `AddMissingOptions` | Boolean | `true`, `false` | On a version upgrade, append newly added options to your config file. Default `true`. |

## Theming

The menus — both rofi and terminal — follow a colour palette. On [Omarchy](https://omarchy.org) that palette follows the current
desktop theme, so changing your theme changes the menus with no extra step.
Elsewhere Otakase uses its own palette. `Theme` picks between them: `auto` (the
default), `omarchy`, or `builtin`.

### Changing individual colours

`ThemeOverrides` replaces named colours with your own, on top of whichever
palette is in use. Following your desktop theme and disliking one colour in it
are not exclusive:

```
ThemeOverrides=accent:#ff6188, green:#a9dc76
```

Comma-separated `name:#hex` pairs. `#rgb` and `#rrggbb` both work, names are
case-insensitive, and anything you do not name keeps the value it had. The
names are:

```
accent      selection   muted
background  background-dark  background-light
foreground  foreground-dark  foreground-bright
red  green  yellow  blue  magenta  cyan
```

A mistyped colour is skipped and noted in `otakase-debug.log`; the rest still
apply.

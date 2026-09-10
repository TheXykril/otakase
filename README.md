# Curd

A cli application to stream anime with [Anilist](https://anilist.co/) integration and Discord RPC written in golang.
Works on Linux, MacOS and Windows.

> ### About this fork
>
> This is a fork of [Wraient/curd](https://github.com/Wraient/curd) that fixes provider
> resolution, which had stopped finding anime at all. It is a drop-in replacement:
> same `curd` binary, same `~/.config/curd/curd.conf`, same AniList/MAL tokens.
>
> **What was broken and what changed** — see [CHANGELOG.md](CHANGELOG.md) for detail:
>
> | Problem | Fix |
> |---|---|
> | Providers were searched with the AniList **romaji** title only, so shows indexed under their English title returned "no results" | Search now falls through a list of title variants (English, romaji, native, then simplified forms) |
> | `senshi.live` lapsed and is now a **parked domain for sale**, yet was first in the default provider stack and the hardcoded fallback | Retired; the stack now leads with the providers that actually resolve streams |
> | A single slow provider could sink an entire search, because providers were tried one at a time against a shared client timeout | Providers are searched **concurrently**, with retries for transient network failures |
> | anipub's newer `/play/{id}/{ep}/{mode}` episode links failed with `unsupported video link` | Both anipub link shapes are now resolved |
> | AllAnime returns `AA_CRYPTO_MISSING` for episode sources, so it listed shows that could never play | Detected and reported explicitly; AllAnime is disabled by default |
> | A failed MPV launch left an empty IPC socket path being polled in a hot loop, writing millions of log lines | Missing sockets now fail immediately and are treated as a closed session |
> | One slow provider set the pace for the whole search — animepahe costs ~17s | Stragglers are abandoned once a faster provider has answered |
> | A failed provider was re-probed on every search, paying its timeout each time | Repeated failures put a provider on a short cooldown |
> | Failures were reported as one concatenated wall of provider errors | Grouped by cause: "hosts are down" reads differently from "nobody carries it" |
> | Choosing from the poster grid failed with "error selecting anime" — the grid clips long labels, and the clipped text was matched against the full one | Rows are addressed by index, which cannot drift from what was displayed |
> | Watch history filed a provider's show id under the *first configured* provider's name, so one host's id was handed to another forever after | The name written now belongs to the id beside it, and a stored id that fails is re-derived instead of believed |
> | Dual tracking (`anilist+myanimelist`) failed to launch: unthrottled writes hit a rate limit, and MyAnimeList's redirect to a dead endpoint was followed | Both loops are paced, API requests no longer follow redirects, and one unwritable entry is skipped rather than aborting the sync |
> | A stream whose CDN wanted an `Origin` header played nothing — the manifest opened, then every segment returned 403 | Providers can require arbitrary HTTP headers, which are passed through to MPV |
> | Finishing an episode could lose it: MPV ran past the end onto a playlist placeholder, curd chased that as a request for episode 1, and the failed switch had already cleared the progress it needed to record | Running off the end ends playback instead, the playlist follows the audio actually playing, and a switch that fails restores everything it cleared |
> | Dual tracking (`anilist+myanimelist`) silently lost every show's broadcast schedule — only AniList reports one, and the merge dropped it whenever the MyAnimeList entry was fresher | Carried across the merge like every other field only one side knows, so countdowns and "caught up" work on both trackers |
> | Menus never displayed the sentence explaining them — the theme omitted rofi's `message` widget, so `-mesg` was silently discarded | Drawn, dimmed, above a hairline |
>
> **Also new:**
>
> *Providers*
> - **[KickassAnime](https://kaa.lt) (`kickassanime`)** leads the stack — whole
>   seasons rather than only what is recent, plain HLS, English subtitles, and
>   the one host here that indexes dubs separately.
> - **[Nyaa](https://nyaa.si) (`nyaa`)** streams from torrents without waiting for
>   a download, for the episodes that aired this week. Streaming hosts lag there;
>   fansub releases are indexed within hours. Nothing is kept — the cache is
>   temporary and removed on exit.
> - `curd -provider-status` probes every provider and reports which ones work.
>
> *Playback*
> - **A show carried in only one language just plays.** Reaching the other audio
>   took two menus for a question with one useful answer; curd now switches and
>   says so (`No dub for episode 7 — playing sub`). See `AutoAudioFallback`.
> - **Being caught up says so**, instead of offering an episode that has not
>   aired and then failing to find it — `Episode 11 airs in 2d`, with the option
>   to look anyway in case the cached schedule is behind.
> - `curd -download` saves episodes instead of streaming them — the most-requested
>   missing feature upstream ([#104](https://github.com/Wraient/curd/issues/104),
>   [#55](https://github.com/Wraient/curd/issues/55)).
>
> *The list*
> - Episode counts: `Show · 9/12 (9 aired)` — watched, season total, and how many
>   have actually aired while a show is still releasing.
> - The list leads with what you were **last watching**, in both menus, instead of
>   whatever order the tracker returned.
> - **Resume points** (`· resume 11:40`) where you stopped part-way, and **airing
>   countdowns** (`· next in 22h`) for releasing shows.
>
> *Interface*
> - **Curd says when it is starting**, if starting takes more than about a second
>   — launched from a keybind there was previously no sign of life until the first
>   menu appeared.
> - The menus follow your desktop colours on [Omarchy](https://omarchy.org/), and
>   the rofi themes were redesigned and are now generated locally rather than
>   downloaded — see [Theming](#theming). Rows set the title at full strength and
>   dim the episode count and provider, so a long list stays scannable.
>
> **Provider status** at the time of writing — hosts break constantly, so run
> `curd -provider-status` rather than trusting this table:
>
> | Provider | State |
> |---|---|
> | `kickassanime` | Working — full seasons, sub and dub |
> | `anipub`, `anineko` | Working |
> | `nyaa` | Working — recent episodes, torrent-backed |
> | `allanime` | Disabled — episode sources now demand a signed request or a CAPTCHA |
> | `animepahe` | Disabled — Cloudflare Turnstile, which no automated browser clears |
> | `senshi` | Retired — the domain lapsed and is parked for sale |

## Join the discord server

https://discord.gg/rrpBfu2gHq

## Join the Matrix server

https://matrix.to/#/#curd:matrix.org

## Demo Video
Normal mode:


https://github.com/user-attachments/assets/376e7580-b1af-40ee-82c3-154191f75b79

Rofi with Image preview


https://github.com/user-attachments/assets/cbf799bc-9fdd-4402-ab61-b4e31f1e264d


## Features
- Multiple content providers (KickassAnime, AniPub, AniNeko, Nyaa, and optionally AniDB) searched concurrently with ordered fallback and up to 1080p support
- Torrent-backed playback for episodes that aired this week, streamed rather than downloaded, with nothing kept afterwards
- Falls back to the other audio automatically when a show exists in only one language, instead of asking
- Stream anime online, or download episodes with `-download`
- Track anime locally, on AniList, or on MyAnimeList
- Browser-based AniList and MyAnimeList login flows
- Skip anime Intro and Outro
- Skip Filler and Recap episodes
- Discord RPC about the anime
- Rofi support, with menus themed from your desktop colours on Omarchy
- Image preview in rofi
- Local anime history to continue from where you left off last time
- Save mpv speed for next episode
- Configurable through config file


## Installing and Setup

> **Note**: `Curd` requires `mpv`. `rofi` and `ueberzugpp` are optional, for the
> graphical menus and image previews.

> **Installing this fork?** It is a drop-in replacement for the official `curd`
> package: same binary name, same `~/.config/curd/curd.conf`, same AniList and
> MyAnimeList tokens. Installing it **replaces** the official package, and your
> existing config, watch history and logins carry over untouched.
>
> This fork is **not on the AUR**, so `yay -S curd` installs the *original*
> (unfixed) package. Use one of the methods below instead.

### Linux

<details open>
<summary><b>Arch Linux / Manjaro</b></summary>

**Build from source with the bundled PKGBUILD** (recommended — pacman then
tracks it like any other package):

```bash
sudo pacman -S --needed go git mpv
git clone https://github.com/TheXykril/curd.git
cd curd
makepkg -si
```

`makepkg` runs the test suite as part of the build, so a broken build fails
before it is installed.

**Or install the prebuilt binary** (no Go toolchain needed):

```bash
sudo pacman -S --needed mpv
curl -Lo curd https://github.com/TheXykril/curd/releases/latest/download/curd-linux-x86_64
chmod +x curd
sudo install -Dm755 curd /usr/bin/curd
```

Note this bypasses pacman, so it will be overwritten if you later install the
official `curd` package.

**Optional extras** for the rofi menus and image previews:

```bash
sudo pacman -S rofi ueberzugpp
```

**Switching back to upstream** at any point:

```bash
sudo pacman -R curd && yay -S curd
```
</details>

<details>
<summary>Debian / Ubuntu (and derivatives)</summary>

```bash
sudo apt update
sudo apt install mpv curl rofi ueberzugpp

# For x86_64 systems:
curl -Lo curd https://github.com/TheXykril/curd/releases/latest/download/curd-linux-x86_64

# For ARM64 systems:
curl -Lo curd https://github.com/TheXykril/curd/releases/latest/download/curd-linux-arm64

chmod +x curd
sudo mv curd /usr/bin/
curd
```
</details>

<details>
<summary>Fedora Installation</summary>

```bash
sudo dnf update
sudo dnf install mpv curl rofi ueberzugpp

# For x86_64 systems:
curl -Lo curd https://github.com/TheXykril/curd/releases/latest/download/curd-linux-x86_64

# For ARM64 systems:
curl -Lo curd https://github.com/TheXykril/curd/releases/latest/download/curd-linux-arm64

chmod +x curd
sudo mv curd /usr/bin/
curd
```
</details>

<details>
<summary>openSUSE Installation</summary>

```bash
sudo zypper refresh
sudo zypper install mpv curl rofi ueberzugpp

# For x86_64 systems:
curl -Lo curd https://github.com/TheXykril/curd/releases/latest/download/curd-linux-x86_64

# For ARM64 systems:
curl -Lo curd https://github.com/TheXykril/curd/releases/latest/download/curd-linux-arm64

chmod +x curd
sudo mv curd /usr/bin/
curd
```
</details>

<details>
<summary>NixOS Installation</summary>

1. Add curd as a flake input, for example:
```nix
{
    inputs = {
        nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
        curd = {
            url = "github:TheXykril/curd";
            inputs.nixpkgs.follows = "nixpkgs";
        };
    }
}
```
2. Install the package, for example:
```nix
{inputs, pkgs, ...}: {
  environment.systemPackages = [
    inputs.curd.packages.${pkgs.system}.default
  ];
}
```

</details>

<details>
<summary>Generic Installation</summary>

Choose the appropriate binary for your system:
```bash
# For Linux x86_64:
curl -Lo curd https://github.com/TheXykril/curd/releases/latest/download/curd-linux-x86_64

# For Linux ARM64:
curl -Lo curd https://github.com/TheXykril/curd/releases/latest/download/curd-linux-arm64

chmod +x curd
sudo mv curd /usr/bin/
curd
```
</details>

<details>
<summary>Uninstallation</summary>

```bash
sudo rm /usr/bin/curd
```

For AUR-based distributions:

```bash
yay -R curd
```
</details>

### MacOS

<details>
<summary>MacOS Installation</summary>

Install required dependencies
```bash
brew install mpv curl
```

Download the appropriate binary for your system:

- For Apple Silicon (M1/M2) Macs:
```bash
curl -Lo curd https://github.com/TheXykril/curd/releases/latest/download/curd-macos-arm64
```

- For Intel Macs:
```bash
curl -Lo curd https://github.com/TheXykril/curd/releases/latest/download/curd-macos-x86_64
```

- For Universal Binary (works on both architectures):
```bash
curl -Lo curd https://github.com/TheXykril/curd/releases/latest/download/curd-macos-universal
```

Then complete the installation:

```bash
chmod +x curd
sudo mv curd /usr/local/bin/
curd
```

</details>

<details>
<summary>Uninstallation</summary>

```bash
sudo rm /usr/local/bin/curd
```

</details>

### Windows

<details>
<summary>Windows Installation</summary>

Option 1: Using the installer
- Download and run the [Windows Installer](https://github.com/TheXykril/curd/releases/latest/download/curd-windows-installer.exe)

Option 2: Standalone executable
- Download [curd-windows-x86_64.exe](https://github.com/TheXykril/curd/releases/latest/download/curd-windows-x86_64.exe)
</details>

## Data Storage

<details>
<summary>Windows</summary>
Stroage: (Token, Timestamps, debug.log, etc) 

```bash
C:\.local\share\curd
```

Config : 

```bash
C:\Users\USERNAME\AppData\Roaming\Curd
```

</details>

<details>
<summary>Linux/Unix</summary>
Stroage: (Token, Timestamps, debug.log, etc)

```bash
$USER/.local/share/curd
```

Config : 

```bash
$USER/.config/curd
```

</details>

## Usage

Run `curd` with the following options:

```bash
curd [options]
```

### Arguments would always take precedence over configuration

> **Note**:
> - To use rofi you need rofi and ueberzug installed.
> - Rofi `.rasi` themes live in `~/.local/share/curd/`.
> - They are **generated from your colour theme on every run** (see
>   [Theming](#theming)), so they follow your desktop and work offline. A theme
>   you have hand-edited is copied to `<name>.rasi.user-backup` before being
>   replaced, so your work is never lost.
> - To keep your own themes permanently, set `Theme=builtin` — or edit the
>   backup and copy it back over the generated file.


### Options

| Flag                      | Description                                                             | Default       |
|---------------------------|-------------------------------------------------------------------------|---------------|
| `-c`                      | Continue the last episode                                              | -             |
| `-change-token`           | Change your authentication token                                       | -             |
| `-dub`                    | Watch the dubbed version of the anime                                  | -             |
| `-sub`                    | Watch the subbed version of the anime                                  | -             |
| `-softsub`                | Prefer soft subtitles when available (AniNeko)                         | -             |
| `-hardsub`                | Prefer hard subtitles when available (AniNeko)                         | -             |
| `-new`                    | Add a new anime to your list                                           | -             |
| `-e`                      | Edit the configuration file                                            | -             |
| `-skip-op`                | Automatically skip the opening section of each episode                 | `true`        |
| `-skip-ed`                | Automatically skip the ending section of each episode                  | `true`        |
| `-skip-filler`            | Automatically skip filler episodes                                     | `true`        |
| `-skip-recap`             | Automatically skip recap sections                                      | `true`        |
| `-discord-presence`       | Enable or disable Discord presence                                     | `true`        |
| `-image-preview`          | Show an image preview of the anime                                     | -             |
| `-no-image-preview`       | Disable image preview                                                  | -             |
| `-next-episode-prompt`    | Prompt for the next episode after completing one                       | -             |
| `-rofi`                   | Open anime selection in the rofi interface                             | -             |
| `-no-rofi`                | Disable rofi interface                                                 | -             |
| `-percentage-to-mark-complete` | Set the percentage watched to mark an episode as complete       | `85`          |
| `-player`                 | Specify the player to use for playback                                 | `"mpv"`       |
| `-save-mpv-speed`         | Save the current MPV speed setting for future sessions                 | `true`        |
| `-score-on-completion`    | Prompt to score the episode on completion                              | `true`        |
| `-storage-path`           | Path to the storage directory                                          | `"$HOME/.local/share/curd"` |
| `-subs-lang`              | Set the language for subtitles                                         | `"english"`   |
| `-u`                      | Update the script                                                      | -             |
| `-v`                      | Show curd version                                                      | -             |
| `-download`               | Download episodes instead of playing them (needs `ffmpeg`)             | -             |
| `-episodes`               | Episodes to download, e.g. `5` or `1-12`                               | selected ep   |
| `-download-dir`           | Where to save downloads                                                | `$HOME/Downloads/curd` |
| `-provider-status`        | Probe every provider and report which ones work                        | -             |
| `-provider-status-query`  | Search term used by `-provider-status`                                 | `one piece`   |

### Examples

- **Continue the Last Episode**:
  ```bash
  curd -c
  ```

- **Add a New Anime**:
  ```bash
  curd -percentage-to-mark-complete=90
  ```

- **Play with Rofi and Image Preview**:
  ```bash
  curd -rofi -image-preview
  ```

## Downloading

Curd can save episodes instead of streaming them. This needs `ffmpeg`.

```bash
# Download the episode you are up to
curd -download

# Download a range
curd -download -episodes 1-12

# Download somewhere specific
curd -download -episodes 5 -download-dir ~/Videos/anime
```

You pick the anime the same way as for playback — the whole selection flow is
shared — and then Curd saves rather than plays. Files land in
`$HOME/Downloads/curd` by default (set `DownloadDir` in the config), named
`<Title> - Episode NN (sub|dub).mp4`.

Streams are **remuxed, not re-encoded**, so a download costs bandwidth and almost
no CPU, and there is no quality loss. Soft subtitles are muxed into the MP4 where
the provider supplies them. An episode already on disk is skipped, and a download
that fails partway is deleted rather than left to look complete.

## Theming

Curd colours its menus from your desktop theme where it can.

On [Omarchy](https://omarchy.org/), the active theme's `colors.toml` is read
from `~/.local/state/omarchy/current/theme/`, and both the terminal menus and
the rofi menus follow it. Switch themes with `omarchy theme set <name>` and Curd
picks it up on its next run — nothing to configure.

Anywhere else, Curd uses its own palette.

Set `Theme` in `~/.config/curd/curd.conf`:

| Value     | Behaviour                                                        |
|-----------|------------------------------------------------------------------|
| `auto`    | Follow the desktop theme when one is detected (default)           |
| `omarchy` | Always use the Omarchy theme; fall back to builtin if unreadable  |
| `builtin` | Always use Curd's own palette, and stop rewriting the rofi themes |

Colours are chosen for contrast rather than assumed: the highlighted row falls
back to the theme's accent when its selection colour sits too close to the
background to be seen, and text drawn on a filled accent is picked light or dark
by measured contrast. That keeps the menus readable on both light and dark
themes.

## Configuration

All configurations are stored in a file you can edit with the `-e` option.

```bash
curd -e
```

Script is made in a way that you use it for one session of watching.

You can quit it anytime and the resume time would be saved in the history file

more settings can be found at config file.
config file is located at ```~/.config/curd/curd.conf```

On first start, curd asks which tracking mode you want to use:

- local
- anilist
- myanimelist
- anilist + myanimelist

Local history stays enabled in all modes. `anilist` means local history + AniList sync, `myanimelist` means local history + MyAnimeList sync, and `anilist + myanimelist` updates both platforms. In dual-sync mode, curd compares the latest remote update time for each anime and pushes the newest status/progress/category back to the older tracker so both sides converge automatically. Legacy installs are migrated to `anilist` automatically. If you switch trackers later, curd exposes a **Change Tracker** menu entry and can either merge both lists, replace MyAnimeList with AniList, or replace AniList with MyAnimeList.

MyAnimeList OAuth requires your own MAL application credentials. You can set them in the config file or with environment variables:

```bash
CURD_MAL_CLIENT_ID=your_client_id
CURD_MAL_CLIENT_SECRET=your_client_secret
```

If the browser reaches the localhost callback page but curd does not continue automatically, rerun the command and paste the full callback URL when prompted. Curd now keeps the pending MyAnimeList PKCE state so the login can be completed manually.

| **Option**               | **Type**   | **Valid Values**                           | **Description**                                                                                   |
|---------------------------|------------|-------------------------------------------|---------------------------------------------------------------------------------------------------|
| `DiscordPresence`         | Boolean    | `true`, `false`                           | Enables or disables Discord Rich Presence integration.                                            |
| `AnimeNameLanguage`       | Enum       | `english`, `romaji`                       | Sets the preferred language for anime names.                                                      |
| `MpvArgs`                 | List       | all mpv args eg ["--fullscreen=yes", "--mute=yes"]     | Add args to mpv player                                                               | 
| `MpvPlaybackStartTimeout` | Integer    | `1` to `600` (seconds)                    | How long to wait for MPV playback to start before trying another provider. Default: `20`          |
| `AddMissingOptions`       | Boolean    | `true`, `false`                           | When `true`, on a **version upgrade** only options registered as new for that release are **appended** to `curd.conf` (not every historical default). Set `false` to opt out. Defaults still apply in memory always. |
| `VimKeys`                 | Boolean    | `true`, `false`                           | When `true`, selection menus use vim motions: `j`/`k`/`h`/`l` (and arrows) to move, `/` or `?` to search. Default: `false` (type-to-filter immediately). Add `VimKeys=true` to `~/.config/curd/curd.conf` to enable. |
| `CheckUpdates`            | Boolean    | `true`, `false`                           | When `true` (default), checks GitHub for a newer release **while idle** (does not delay startup). If an update is found, the next launch shows notes and options: update now / skip / remind later / turn off. |
| `MpvEpisodePlaylist`    | Boolean    | `true`, `false`                           | When `true` (default), after playback is stable curd fills the MPV playlist with episodes (and dub/sub when available) so you can pick from MPV's playlist UI. Access: `MPV` → right-click the prev/next OSC button (or `Ctrl+P`). Selecting a row loads that stream and keeps tracking in sync. |
| `AlternateScreen`         | Boolean    | `true`, `false`                           | Toggles the use of an alternate screen buffer for cleaner UI.                                     |
| `RofiSelection`           | Boolean    | `true`, `false`                           | Enables or disables anime selection via Rofi.                                                     |
| `PercentageToMarkComplete`| Integer    | `0` to `100`                              | Sets the percentage of an episode watched to consider it as completed.                            |
| `StoragePath`             | String     | Any valid path (Environment variables accepted)  | Specifies the directory where Curd stores its data.                                        |
| `SubOrDub`                | Enum       | `sub`, `dub`                              | Sets the preferred format for anime audio.                                                        |
| `SubStyle`                | Enum       | `ask`, `soft`, `hard`                     | For AniNeko sub streams: `ask` prompts once when both soft-sub and hard-sub servers exist, then saves your choice here; `soft` uses external `.vtt` subtitles via mpv; `hard` uses burned-in subs. Default: `ask` |
| `NextEpisodePrompt`       | Boolean    | `true`, `false`                           | Prompts the user before automatically playing the next episode.                                   |
| `SubsLanguage`            | String     | `english` (redundant rn)                  | Sets the preferred subtitle language.                                                             |
| `ScoreOnCompletion`       | Boolean    | `true`, `false`                           | Automatically prompts the user to rate the anime upon completion.                                 |
| `SkipOp`                  | Boolean    | `true`, `false`                           | Automatically skips the opening of episodes when supported.                                       |
| `SkipEd`                  | Boolean    | `true`, `false`                           | Automatically skips the ending of episodes when supported.                                        |
| `SkipRecap`               | Boolean    | `true`, `false`                           | Skips recap sections in episodes when supported.                                                  |
| `ImagePreview`            | Boolean    | `true`, `false`                           | Enables or disables image previews during anime selection (only for rofi).                        |
| `Player`                  | String     | any mpv-compatible binary (e.g. `mpv`, `iina`) | Player binary used for playback. If not found, Curd falls back to `mpv`.                          |
| `SaveMpvSpeed`            | Boolean    | `true`, `false`                           | Retains the playback speed set in MPV for next episode.                                           |
| `SkipFiller`              | Boolean    | `true`, `false`                           | Skips filler episodes when supported.                                                             |
| `MenuOrder`               | String     | Comma-separated list                      | Controls which menu items appear and their order. Available options: `CURRENT`, `ALL`, `UNTRACKED`, `UPDATE`, `REMAP_PROVIDER`, `CONTINUE_LAST`, `PLANNING`, `COMPLETED`, `PAUSED`, `DROPPED`, `REWATCHING`, `TRACKER`, `PROVIDER`. Only listed items will be shown. Default: `CURRENT,ALL,UNTRACKED,UPDATE,REMAP_PROVIDER,CONTINUE_LAST,TRACKER,PROVIDER` |
| `Provider`                | List       | `stacked`, `["kickassanime"]`, `["anipub"]`, `["anineko"]`, `["nyaa"]`, `["allanime"]`, `["animepahe"]` | Sets the content-provider fallback list. `stacked` (default) uses the preferred order: kickassanime → anipub → anineko → nyaa. `nyaa` sits last of the working four because finding peers costs a few seconds, but it carries episodes the streaming hosts have not indexed yet. A single-provider list uses only that site. AllAnime and Animepahe are registered but disabled — both now refuse automated clients — and are only used if named explicitly. Default: `stacked` |
| `AutoAudioFallback`       | Boolean    | `true`, `false`                           | When `true` (default), a show carried in only one language plays in the other rather than stopping to ask — the preferred language is still tried first every episode, and the switch is announced (`No dub for episode 7 — playing sub`). Set `false` to be prompted instead. |
| `ManualProviderSearch`    | Boolean    | `true`, `false`                           | Skip automatic provider matching and always show provider search results for manual selection. Displays a hint with the tracker title, format (TV/Movie/etc.), episode count, and sub/dub mode. Default: `false` |
| `TrackingLocal`           | Boolean    | `true`                                    | Legacy compatibility flag. Local playback history is always enabled.                              |
| `TrackingRemote`          | Enum       | `none`, `anilist`, `myanimelist`, `anilist+myanimelist` | Selects which remote tracker curd syncs with.                                           |
| `TrackingConfigured`      | Boolean    | `true`, `false`                           | Internal flag used to remember that the startup tracking prompt has already been completed.       |
| `MyAnimeListClientID`     | String     | MAL OAuth client ID                       | Client ID used for MyAnimeList browser login.                                                     |
| `MyAnimeListClientSecret` | String     | MAL OAuth client secret                   | Optional secret used for MyAnimeList browser login and token refresh.                             |
| `MyAnimeListImported`     | Boolean    | `true`, `false`                           | Tracks whether the one-time AniList-to-MyAnimeList import prompt has already been handled.        |

## Todo (fix)
- Use Powershell for windows token input instead of notepad or cmd
- Add a better way to do commands in windows (Convinience for users)

## Troubleshooting

**"No provider had ..." / nothing plays.** Streaming hosts break often. Check
which ones are actually working:

```bash
curd -provider-status
```

Each provider is reported as working, disabled (with the reason), or unreachable.
If everything is unreachable, it is your connection or the hosts are down; if
everything answered but nothing matched, the show is genuinely not carried under
that name — try searching for it manually from the menu.

**A provider says it is disabled.** Some are off by default because they cannot
currently play anything: `allanime`'s episode endpoint demands a signed request
or a CAPTCHA, `animepahe` sits behind Cloudflare Turnstile that no automated
browser clears, and `senshi`'s domain has lapsed. `anidb` is off as unverified.
To enable one anyway, list it in `Provider`:

```
Provider=["kickassanime","anipub","anineko","animepahe"]
```

**Curd is slow to search.** A provider that fails repeatedly is skipped for five
minutes, and slow providers are abandoned once a faster one has answered, so
this usually resolves itself. `curd -provider-status` shows the per-provider
timings.

**A recent episode is missing.** Streaming hosts often carry a show's back
catalogue but lag on the episode that aired this week. That is what `nyaa` is
for — it is in the default stack, and releases are indexed within hours of
broadcast. To use it alone while testing, pick it from the **PROVIDER** menu or
set `Provider=nyaa`.

**Torrent playback: what to expect.** The first frame takes a few seconds while
peers are found, then it plays and seeks normally. Curd seeds while you watch,
which is ordinary swarm behaviour and is what keeps peers willing to serve you —
it does mean outbound traffic. Nothing is kept: the cache is a temporary
directory removed when curd exits. Nyaa's feed returns roughly the most recent
matches, so the early episodes of a long-running show may not appear there.

**It plays nothing, or dies after a second.** If the log shows repeated `403`
against a CDN, the stream needs an HTTP header the player is not sending. That
is a provider bug rather than a broken host — please open an issue with the
provider name and the failing URL.

**Logs.** `~/.local/share/curd/debug.log`, truncated on each run.

## Dependencies
- mpv - Video player (required fallback)
- iina - Optional mpv-based player on macOS
- rofi - Selection menu
- ueberzug - Display images in rofi
- ffmpeg - Required for `-download`, which remuxes the stream
- chromium - Only for Animepahe, which is disabled by default (auto-downloaded; Termux users must install manually via `pkg install chromium`)

## API Used
- [Anilist API](https://anilist.gitbook.io/anilist-apiv2-docs) - Update user data and download user data
- [MyAnimeList API](https://myanimelist.net/apiconfig/references/api/v2) - MyAnimeList OAuth and tracking sync
- [AniSkip API](https://api.aniskip.com/api-docs) - Get anime intro and outro timings
- [KickassAnime](https://kaa.lt/) - Default provider: JSON catalog, HLS streams and external subtitle tracks, sub and dub
- [AniPub](https://anipub.xyz/) - Fast JSON catalog APIs with MegaPlay HLS streams
- [AniNeko Content](https://anineko.to/) - Alternative provider with soft/hard sub stream selection
- [Nyaa](https://nyaa.si/) - Torrent index, streamed as playback rather than downloaded
- [AllAnime Content](https://allanime.to/) - Registered but disabled; episode sources now require a signed request or a CAPTCHA
- [Animepahe Content](https://animepahe.pw/) - Alternative provider for 1080p streams
- [Jikan](https://jikan.moe/) - Get filler episode number

## Credits
- [ani-cli](https://github.com/pystardust/ani-cli) - Code for fetching anime url
- [jerry](https://github.com/justchokingaround/jerry) - For the inspiration

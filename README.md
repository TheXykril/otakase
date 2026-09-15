# Otakase

Watch anime from the command line, with your list kept in sync.

Otakase finds the episode, plays it in mpv, skips the opening, ending, filler
and recaps, and updates AniList or MyAnimeList when you are done. It works on
Linux, macOS and Windows, with optional rofi menus that follow your desktop
theme.

> **Otakase** is a maintained continuation of
> [curd by Wraient](https://github.com/Wraient/curd), rebuilt around source
> resolution that had stopped finding anime at all, with reworked tracking and
> theming that follows the desktop. Original work © its contributors; modified
> since 2026 and released under the same GPL-3.0 licence.
>
> The name is *omakase* — 「お任せ」, "I'll leave it to you", the meal where the
> chef chooses — with the otaku syllable swapped in. You pick the show; it picks
> the rest.

---

## Features

- Search runs concurrently with ordered fallback, up to 1080p
- Plays this week's episodes without waiting for a download, keeping nothing afterwards
- Falls back to the other audio automatically when a show exists in only one language
- Stream, or save episodes with `-download`
- Track locally, on AniList, on MyAnimeList, or on both at once
- Browser-based AniList and MyAnimeList sign-in
- Skips openings, endings, filler episodes and recaps
- Discord Rich Presence
- rofi menus with image previews, themed from your desktop colours on Omarchy
- Resumes where you left off, and remembers your mpv speed
- Configurable through a plain-text config file

## Install

> Otakase needs **mpv**. Everything else is optional: `rofi` for the graphical
> menus (it draws its own poster thumbnails, so `ueberzugpp` is only for
> previews in the *terminal*), `libnotify` so a launch with no terminal — from
> the keybinding, say — can still report progress and errors, `xdg-utils` to
> open the browser for AniList/MyAnimeList sign-in, and `ffmpeg` for
> `-download`.

### Arch Linux / Manjaro

Build from source with the bundled PKGBUILD — pacman then tracks it like any
other package, and the test suite runs as part of the build, so a broken build
fails before it is installed:

```bash
sudo pacman -S --needed go git mpv
git clone https://github.com/TheXykril/otakase.git
cd otakase
makepkg -si
```

Or install the prebuilt binary, with no Go toolchain needed:

```bash
sudo pacman -S --needed mpv
curl -Lo otakase https://github.com/TheXykril/otakase/releases/latest/download/otakase-linux-x86_64
chmod +x otakase
sudo install -Dm755 otakase /usr/bin/otakase
sudo ln -sf otakase /usr/bin/otk
```

Optional extras:

```bash
sudo pacman -S rofi ueberzugpp ffmpeg
```

### Debian / Ubuntu and other Linux

```bash
sudo apt update
sudo apt install mpv curl rofi ueberzugpp

# x86_64
curl -Lo otakase https://github.com/TheXykril/otakase/releases/latest/download/otakase-linux-x86_64
# ARM64
curl -Lo otakase https://github.com/TheXykril/otakase/releases/latest/download/otakase-linux-arm64

chmod +x otakase
sudo install -Dm755 otakase /usr/bin/otakase
sudo ln -sf otakase /usr/bin/otk
```

### macOS

```bash
brew install mpv curl

# Apple Silicon
curl -Lo otakase https://github.com/TheXykril/otakase/releases/latest/download/otakase-macos-arm64
# Intel
curl -Lo otakase https://github.com/TheXykril/otakase/releases/latest/download/otakase-macos-x86_64
# Either (universal)
curl -Lo otakase https://github.com/TheXykril/otakase/releases/latest/download/otakase-macos-universal

chmod +x otakase
sudo mv otakase /usr/local/bin/
sudo ln -sf otakase /usr/local/bin/otk
```

Uninstall with `sudo rm /usr/local/bin/otakase /usr/local/bin/otk`.

### Windows

Either run the
[installer](https://github.com/TheXykril/otakase/releases/latest/download/otakase-windows-installer.exe),
or download the standalone
[otakase-windows-x86_64.exe](https://github.com/TheXykril/otakase/releases/latest/download/otakase-windows-x86_64.exe).
The rofi interface is Linux-only; Windows uses the terminal menus.

## Coming from curd

Nothing to do. On first launch Otakase copies your config, your AniList and
MyAnimeList tokens and your watch history from `~/.config/curd` and
`~/.local/share/curd` into its own locations, and repoints `StoragePath` if it
was still on the old default. A storage path you chose yourself is left alone.

The originals are **copied, not moved**, so they are still there if you want
them; delete them once Otakase looks right.

The `curd` command itself is gone — installing Otakase replaces the old package
rather than sitting beside it. Use `otakase`, or `otk`.

If you had set `CURD_MAL_CLIENT_ID` or `CURD_MAL_CLIENT_SECRET` in a shell
profile, those names still work. `OTAKASE_MAL_*` takes precedence.

## Usage

Run `otakase` with no arguments to pick from your list. **`otk` is a shorter
alias for the same program** — every command below works with either.
Arguments always take precedence over the config file.

| Flag | Description | Default |
|---|---|---|
| `-c` | Continue the last episode | |
| `-new` | Add a new anime to your list | |
| `-dub` / `-sub` | Choose the audio track | |
| `-softsub` / `-hardsub` | Prefer external or burned-in subtitles | |
| `-rofi` / `-no-rofi` | Use the rofi interface, or force the terminal | |
| `-image-preview` / `-no-image-preview` | Poster previews in rofi | |
| `-skip-op` | Skip openings | `true` |
| `-skip-ed` | Skip endings | `true` |
| `-skip-filler` | Skip filler episodes | `true` |
| `-skip-recap` | Skip recap sections | `true` |
| `-next-episode-prompt` | Ask before playing the next episode | |
| `-percentage-to-mark-complete` | Watched percentage that counts as complete | `85` |
| `-player` | Playback binary | `mpv` |
| `-save-mpv-speed` | Carry mpv speed to the next episode | `true` |
| `-score-on-completion` | Prompt to rate a finished show | `true` |
| `-discord-presence` | Discord Rich Presence | `true` |
| `-subs-lang` | Subtitle language | `english` |
| `-storage-path` | Data directory | `$HOME/.local/share/otakase` |
| `-download` | Save episodes instead of playing them (needs `ffmpeg`) | |
| `-episodes` | Episodes to save, e.g. `5` or `1-12` | selected |
| `-download-dir` | Where to save them | `$HOME/Downloads/otakase` |
| `-current` | Jump straight to what you are currently watching | |
| `-show-new-episodes` | Mark shows with an unwatched episode in the list | `true` |
| `-vim-keys` | `j`/`k`/`h`/`l` to move and `/` to search in menus | |
| `-check-updates` | Look for a newer release while idle | `true` |
| `-discord-client-id` | Discord application id for Rich Presence | |
| `-install-keybind` | Bind Super+Shift+A to the rofi menu in your Hyprland config | |
| `-remove-keybind` | Undo `-install-keybind` | |
| `-force-keybind` | Let `-install-keybind` take a key something else uses | |
| `-provider-status` | Probe each configured source and report which respond | |
| `-provider-status-query` | Search term `-provider-status` probes with | `one piece` |
| `-change-token` | Change your authentication token | |
| `-e` | Edit the config file | |
| `-u` | Update to the latest release | |
| `-v` | Show the version | |

```bash
otk                                 # the short form, identical in every way
otakase -c                          # continue where you left off
otakase -rofi -image-preview        # graphical menu with posters
otakase -dub -next-episode-prompt   # dub, asking before each next episode
```

### Saving episodes

```bash
otakase -download                   # the episode you are up to
otakase -download -episodes 1-12    # a range
otakase -download -download-dir ~/Videos/anime
```

Needs `ffmpeg`. Episodes are saved as `.mp4`.

## Hyprland keybinding

One command binds **Super+Shift+A** to open the rofi menu with poster previews —
no terminal, pick a show, watch it:

```bash
otakase -install-keybind
```

Launched this way there is no terminal, so install `rofi` (the menus),
`libnotify` (progress and errors have nowhere else to go) and `xdg-utils` (the
sign-in browser, if you have not signed in yet).

It writes to `~/.config/hypr/bindings.lua` on Omarchy, or `hyprland.conf` on a
stock Hyprland, backs the file up first, and is safe to run twice. If something
already owns that key it tells you what and changes nothing; `-force-keybind`
takes it anyway and comments out the old line rather than deleting it.
`-remove-keybind` undoes the whole thing.

Installing the package does **not** do this for you. A package is installed as
root with no user context, so it cannot write to your config — and one that
tried would be reaching somewhere it does not belong.

## Tracking

On first start Otakase asks how you want to track: **local**, **anilist**,
**myanimelist**, or **anilist + myanimelist**. Local history stays on in every
mode, so you can always resume.

In dual mode, Otakase compares the last update time for each show and pushes
the newer status, progress and category to whichever tracker is behind, so the
two converge on their own. You can change your mind later from the **Change
Tracker** menu, which can merge both lists or let one replace the other.

MyAnimeList sign-in needs your own MAL application credentials, set in the
config file or the environment:

```bash
OTAKASE_MAL_CLIENT_ID=your_client_id
OTAKASE_MAL_CLIENT_SECRET=your_client_secret
```

If the browser reaches the localhost callback page but Otakase does not
continue, run the command again and paste the full callback URL when prompted.

## Theming

The rofi menus are rendered from a colour palette at every launch. On
[Omarchy](https://omarchy.org) that palette follows the current desktop theme,
so changing your theme changes the menus with no extra step. Elsewhere Otakase
uses its own palette.

## Configuration

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
| `SubOrDub` | Enum | `sub`, `dub` | Preferred audio. |
| `SubStyle` | Enum | `ask`, `soft`, `hard` | External or burned-in subtitles, where both exist. `ask` prompts once and remembers. |
| `SubsLanguage` | String | `english` | Preferred subtitle language. |
| `AutoAudioFallback` | Boolean | `true`, `false` | Play the other language when a show is carried in only one, instead of asking. Default `true`. |
| `AnimeNameLanguage` | Enum | `english`, `romaji` | Preferred title language. |
| `PercentageToMarkComplete` | Integer | `0`–`100` | Watched percentage that counts as complete. |
| `NextEpisodePrompt` | Boolean | `true`, `false` | Ask before playing the next episode. |
| `ScoreOnCompletion` | Boolean | `true`, `false` | Prompt to rate a show when you finish it. |
| `SkipOp` / `SkipEd` | Boolean | `true`, `false` | Skip openings and endings where timings exist. |
| `SkipFiller` / `SkipRecap` | Boolean | `true`, `false` | Skip filler episodes and recap sections. |
| `DiscordPresence` | Boolean | `true`, `false` | Discord Rich Presence. |
| `RofiSelection` | Boolean | `true`, `false` | Use rofi for selection menus. |
| `ImagePreview` | Boolean | `true`, `false` | Poster previews in rofi. |
| `VimKeys` | Boolean | `true`, `false` | `j`/`k`/`h`/`l` to move and `/` to search in menus, instead of type-to-filter. |
| `AlternateScreen` | Boolean | `true`, `false` | Use an alternate screen buffer for a cleaner terminal. |
| `MenuOrder` | String | comma-separated | Which menu entries appear, and in what order. Choose from `CURRENT`, `ALL`, `UNTRACKED`, `UPDATE`, `REMAP_PROVIDER`, `CONTINUE_LAST`, `PLANNING`, `COMPLETED`, `PAUSED`, `DROPPED`, `REWATCHING`, `TRACKER`, `PROVIDER`. |
| `Provider` | List | `stacked`, or a single-entry list | Which sources to search and in what order. `stacked` (the default) uses the preferred order with fallback; naming one restricts the search to it. |
| `ManualProviderSearch` | Boolean | `true`, `false` | Always choose the match yourself instead of matching automatically. |
| `TrackingRemote` | Enum | `none`, `anilist`, `myanimelist`, `anilist+myanimelist` | Which tracker to sync with. |
| `MyAnimeListClientID` | String | MAL OAuth client ID | Used for MyAnimeList sign-in. |
| `MyAnimeListClientSecret` | String | MAL OAuth secret | Optional; used for sign-in and token refresh. |
| `CheckUpdates` | Boolean | `true`, `false` | Check for a newer release while idle, and offer it at the next launch. Default `true`. |
| `AddMissingOptions` | Boolean | `true`, `false` | On a version upgrade, append newly added options to your config file. Default `true`. |

## Where your data lives

| Path | Contents |
|---|---|
| `~/.config/otakase/otakase.conf` | Your settings |
| `~/.local/share/otakase/` | Watch history, tokens, logs, cached artwork |
| `~/.local/share/otakase/debug.log` | The log to check first when something misbehaves |

## Troubleshooting

**Nothing is found for a show.** Titles differ between your tracker and the
sources. Set `ManualProviderSearch=true` to pick the match yourself, or use the
**Remap** menu entry to correct a bad match permanently.

**Playback never starts.** Run `otakase -provider-status` to see which
sources are responding. Check `debug.log`, and raise
`MpvPlaybackStartTimeout` if your connection is slow.

**The rofi menus look wrong.** Themes are rewritten at every launch, so a
stale theme fixes itself on the next run. Confirm `rofi` is installed and
`RofiSelection=true`.

**Progress did not update.** Tracker writes are paced to stay inside API rate
limits and happen in the background. Check `debug.log` for the write, and that
`TrackingRemote` is what you expect.

## Built with

[AniList](https://anilist.co) and [MyAnimeList](https://myanimelist.net) for
lists and metadata, [AniSkip](https://api.aniskip.com/api-docs) for opening and
ending timings, and [Jikan](https://jikan.moe/) for filler episode numbers.

## Credits

Otakase continues [curd](https://github.com/Wraient/curd) by Wraient and its
contributors. [ani-cli](https://github.com/pystardust/ani-cli) and
[jerry](https://github.com/justchokingaround/jerry) came first, and this owes
both.

## Licence

GPL-3.0. See [LICENSE](LICENSE).

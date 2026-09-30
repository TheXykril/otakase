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

- Searches several sources at once and falls back when one fails, up to 1080p
- Plays this week's episodes without waiting for a download, keeping nothing afterwards
- Falls back to the other audio automatically when a show exists in only one language
- Stream, save episodes with `-download`, or cast them to a Chromecast or DLNA TV
- Track locally, on AniList, on MyAnimeList, or on both at once
- Browser-based AniList and MyAnimeList sign-in
- Skips openings, endings, filler episodes and recaps
- Discord Rich Presence
- rofi menus with image previews, themed from your desktop colours on Omarchy
- Resumes where you left off, even on another device: the position is kept in your AniList note or MyAnimeList comment as `[otakase:5@754]` (episode 5, 754 s in), next to anything else you wrote there
- Remembers your mpv speed
- Configurable through a plain-text config file

## Install

> Otakase needs **mpv**. Everything else is optional: `rofi` for the graphical
> menus (it draws its own poster thumbnails), `libnotify` so a launch with no terminal — from
> the keybinding, say — can still report progress and errors, `xdg-utils` to
> open the browser for AniList/MyAnimeList sign-in, and `ffmpeg` for
> `-download`.

### Arch Linux / Manjaro

Build from source with the bundled PKGBUILD, so pacman tracks it like any
other package:

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
sudo pacman -S rofi libnotify xdg-utils ffmpeg
```

### Debian / Ubuntu and other Linux

```bash
sudo apt update
sudo apt install mpv curl rofi libnotify-bin xdg-utils

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

In the terminal list, **← and → move between your categories** — watching,
planning, completed and the rest — without going back to the menu. Each tab
shows how many entries it holds. Tab and Shift+Tab do the same, and are the
way to do it when vim keys are on, where the arrows move the cursor instead.
Which categories appear, and in what order, is `MenuOrder`. The highlighted show's
full title and notes appear beside the list, where the row itself is clipped.

Along the bottom are the actions, each on a key you can press from anywhere in
the list — `^` means Ctrl, so `^u` is Ctrl+U. `MenuOrder` decides which of them
appear and in what order too. Under rofi they stay menu entries.

| Key | Menu entry | What it does |
|---|---|---|
| `^u` | Untracked Watching | Search any show and watch it without touching your list, history or tracker |
| `^e` | Update (Episode, Status, Score) | Change a show's progress, status or score by hand |
| `^r` | Remap Provider | Fix a show that plays the wrong anime, by picking the right match |
| `^l` | Continue Last Session | Resume the show you watched last |
| `^t` | Change Tracker | Switch between local, AniList, MyAnimeList or both |
| `^o` | Change Provider | Pick which sources are searched, and in what order |
| `^k` | Cast | Toggle casting for this run — shows `[ ] Cast: Off` or `[x] Cast: <device>` |

The category entries — Currently Watching, Show All, Plan to Watch, Completed,
Paused, Dropped, Rewatching — are the tabs in the terminal list and plain
entries under rofi.

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
| `-cast` | Play on a Chromecast or DLNA TV on this network instead of locally | |
| `-current` | Jump straight to what you are currently watching | |
| `-show-new-episodes` | Mark shows with an unwatched episode in the list | `true` |
| `-vim-keys` | `j`/`k`/`h`/`l` to move and `/` to search in menus | |
| `-check-updates` | Look for a newer release while idle | `true` |
| `-discord-client-id` | Discord application id for Rich Presence | |
| `-install-keybind` | Bind Super+Shift+A to the rofi menu in your Hyprland config | |
| `-remove-keybind` | Undo `-install-keybind` | |
| `-force-keybind` | Let `-install-keybind` take a key something else uses | |
| `-refresh-keybind` | Update an existing binding, adding nothing if absent | |
| `-provider-status` | Probe each configured source and report which respond | |
| `-provider-status-query` | Search term `-provider-status` probes with | `one piece` |
| `-change-token` | Change your authentication token | |
| `-setup-anime-skip` | Get a personal Anime-Skip client id and save it | |
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

## Casting

`otakase -cast` plays the episode on a Chromecast or a DLNA TV on the same
network. Most smart TVs are DLNA renderers (LG, Samsung, Sony, Hisense,
Philips); they are listed next to Chromecasts, marked `· DLNA`. A TV that is
both can show up twice, and the Chromecast entry is the one with the better
controls. A DLNA TV is sent one plain MPEG-TS stream, so it needs nothing
installed; if it does not appear, check that the TV's "media renderer" or
"DLNA" setting is on and that your firewall lets UDP answers back in. The menu
also carries a **Cast** toggle (`[ ] Cast: Off` / `[x] Cast: Office TV`, `^k`
in the terminal list) that does the same thing per-run; `CastDevice` skips
being asked which device each time. Casting works from **Untracked Watching**
too, and like local untracked playback it records nothing.

Casting needs `ffmpeg`, same as `-download`. The stream is served from this
computer, so keep otakase running while you watch.

**Subtitles** are burned into the picture, using your GPU when it can. A dub
plays its English audio track and gets no subtitles. `CastBurnSubtitles=false`
turns burning off; `CastEncoder` forces `vaapi` or
`software` if detection picks wrong.

**Seeking** takes a couple of seconds to restart the stream, so a press moves
30 seconds and a held key waits until you let go. A seek or a resume
re-encodes the stream even when there are no subtitles to burn, so the sound
and the picture start together.

While casting, the terminal shows a control panel: position, device, and
keys — space pauses, arrows seek/adjust volume, `a` switches between sub and dub
from where you are, `q` stops. Anything else
otakase needs to tell you arrives as a desktop notification instead of
interrupting the panel. Next episode, filler skip, and tracker updates all
work the same as local playback.

From the rofi keybind, casting opens its own terminal (`CastTerminal` if the
wrong one opens); closing that window stops the episode and saves position.

**If the episode never starts**, it's almost always a firewall dropping the
connection. otakase detects this, prints the exact fix command, and copies it
to your clipboard — paste it in a terminal and cast again. The stream server
uses a random port by default; set `CastPort` to a fixed one so a single
firewall rule covers every cast. If that doesn't fix
it (a second firewall service or Docker can override it silently), see the
[Casting Problems wiki page](https://github.com/TheXykril/otakase/wiki/Casting-Problems)
for the full diagnosis and manual commands.

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

**Installing the package does this for you**, and uninstalling removes the
binding again. Set `OTAKASE_NO_KEYBIND=1` before installing to skip it. An
upgrade only refreshes a binding that is already there; it will not re-add one
you removed.

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

## Skip timings

Openings and endings are skipped using whichever source knows them, asked in
this order: the provider in use, [AniSkip](https://api.aniskip.com/api-docs),
[theintrodb](https://theintrodb.org), and optionally Anime-Skip. The first
three need no setup.

The first time theintrodb is needed, otakase downloads a small lookup table in
the background; playback is never held up for it. Set `IntroDBSkipTimes=false`
to leave theintrodb out.

### Fixing the times yourself

When a skip is wrong, or missing, you can say so from the player:

| Key | What it does |
|---|---|
| `Alt+o` | Mark the opening — once at its start, again at its end |
| `Alt+e` | The same for the ending |
| `Alt+s` | Send what you marked to AniSkip — press once to see it, again to send |
| `Alt+r` | Forget the marks |
| `Alt+u` | Agree with the skip you were given |
| `Alt+d` | Report the skip you were given as wrong |

Everything appears on the player's own screen, so you never leave the episode.
Nothing is sent until you press `Alt+s` a second time, with the exact times on
screen in between — these entries are public, and every player that reads
AniSkip inherits them. Submissions are filed under an id this program invents
and keeps; AniSkip has no accounts and asks for nothing about you. Set
`ContributeSkipTimes=false` to leave the keys unbound.

### Adding Anime-Skip

[Anime-Skip](https://anime-skip.com) is the fourth source, and the only one
that needs a client id. The simplest way to turn it on:

```
AnimeSkipClientID=auto
```

Otakase then uses Anime-Skip's public shared id and replaces it on its own if
it changes. Being shared, it may be rate-limited.

For an id of your own, run:

```
otakase -setup-anime-skip
```

This opens Anime-Skip's account page in your browser — sign up first if you
have not already — and asks you to paste back the client id you create there.

You can also write an id you already have straight into the config, in place
of `auto`:

```
AnimeSkipClientID=your_client_id_here
```

Leaving the setting empty is fine — Anime-Skip is simply not asked, and the
other sources carry on.

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
| `CastDevice` | String | a device name | Cast to this device without asking, when `-cast` is given and the device is found. Empty asks each time. |
| `CastTerminal` | String | a terminal emulator | Terminal opened for a cast started from rofi. Empty uses `$TERMINAL`, then whatever is installed. |
| `CastPort` | Integer | `0`–`65535` | Port the cast stream server listens on. `0` (the default) picks a free one; fix it to allow a single port through a firewall. |
| `CastBurnSubtitles` | Boolean | `true`, `false` | Burn subtitles into the picture when casting. Costs a re-encode. Default `true`. |
| `CastEncoder` | Enum | empty, `vaapi`, `software` | Encoder for burned subtitles. Empty detects what this machine can do. |
| `SubOrDub` | Enum | `sub`, `dub` | Preferred audio. Switching a show to the other audio (the player's `(DUB)`/`(SUB)` playlist row, or `a` while casting) is remembered for that show; `-sub` and `-dub` still win for a run. |
| `SubStyle` | Enum | `ask`, `soft`, `hard` | External or burned-in subtitles, where both exist. `ask` prompts once and remembers. |
| `SubsLanguage` | String | `english` | Preferred subtitle language, as a name or code (`english`, `pt`, `spa`). Switching language in mpv (the `j` key) is remembered for that show in `show_prefs.json` and wins over this. |
| `Quality` | String | `best`, `1080`, `720`, `480`, or any number of lines | Picture height for HLS streams, in mpv, casts and downloads. `best` (the default) plays the provider's stream untouched; a number picks the nearest variant at or below it, else the smallest above. Set a different one per show from the update menu (*Quality for this show*); it is kept in `show_prefs.json`. |
| `AutoAudioFallback` | Boolean | `true`, `false` | Play the other language when a show is carried in only one, instead of asking. Default `true`. |
| `AnimeNameLanguage` | Enum | `english`, `romaji` | Preferred title language. |
| `PercentageToMarkComplete` | Integer | `0`–`100` | Watched percentage that counts as complete. |
| `NextEpisodePrompt` | Boolean | `true`, `false` | Ask before playing the next episode. |
| `ScoreOnCompletion` | Boolean | `true`, `false` | Prompt to rate a show when you finish it. |
| `SkipOp` / `SkipEd` | Boolean | `true`, `false` | Skip openings and endings where timings exist. |
| `Theme` | Enum | `auto`, `omarchy`, `builtin` | Which colour palette to use. `auto` follows the desktop on Omarchy. |
| `ThemeOverrides` | String | `name:#hex` pairs | Replaces named colours on top of the palette in use. See [Theming](#theming). |
| `ContributeSkipTimes` | Boolean | `true`, `false` | Bind the player keys for marking, submitting and voting on skip times — see [Fixing the times yourself](#fixing-the-times-yourself). |
| `IntroDBSkipTimes` | Boolean | `true`, `false` | Ask theintrodb for openings and endings the other sources do not know. Downloads a public id mapping in the background the first time it is needed. |
| `AnimeSkipClientID` | String | `auto`, or an Anime-Skip client id | Adds Anime-Skip as a source of skip timings — see [Adding Anime-Skip](#adding-anime-skip). `auto` uses the public shared id. Empty by default. |
| `SkipFiller` / `SkipRecap` | Boolean | `true`, `false` | Skip filler episodes and recap sections. `SkipRecap` also skips a "previously on" stretch inside an episode when AniSkip has one on file. |
| `DiscordPresence` | Boolean | `true`, `false` | Discord Rich Presence. |
| `DiscordClientId` | String | Discord application id | Application Rich Presence reports as. |
| `ShowNewEpisodes` | Boolean | `true`, `false` | Mark shows with an unwatched aired episode in the list. Default `true`. |
| `RofiSelection` | Boolean | `true`, `false` | Use rofi for selection menus. |
| `ImagePreview` | Boolean | `true`, `false` | Poster previews in rofi. |
| `VimKeys` | Boolean | `true`, `false` | `j`/`k`/`h`/`l` to move and `/` to search in menus, instead of type-to-filter. |
| `AlternateScreen` | Boolean | `true`, `false` | Use an alternate screen buffer for a cleaner terminal. |
| `CurrentCategory` | Boolean | `true`, `false` | Open straight into your watching list, skipping the menu; the tabs and bottom bar still reach everything. Escape then quits. Terminal only. `-current` does the same for one run. |
| `MenuOrder` | String | comma-separated | Which menu entries appear, and in what order. Choose from `CURRENT`, `ALL`, `UNTRACKED`, `UPDATE`, `REMAP_PROVIDER`, `CONTINUE_LAST`, `PLANNING`, `COMPLETED`, `PAUSED`, `DROPPED`, `REWATCHING`, `TRACKER`, `PROVIDER`, `CAST`. `TRACKER` is always added if left out. |
| `Provider` | List | `stacked`, or a single-entry list | Which sources to search and in what order. `stacked` (the default) uses the preferred order with fallback; naming one restricts the search to it. |
| `DisabledProviders` | List | provider names, e.g. `["nyaa"]` | Sources never searched, even under `stacked`. |
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
| `~/.local/share/otakase/otakase-debug.log` | The log to check first when something misbehaves |

## Troubleshooting

Longer answers, and anything about casting, live on the
[wiki](https://github.com/TheXykril/otakase/wiki) instead of growing this file
forever.

**Nothing is found for a show.** Titles differ between your tracker and the
sources. Set `ManualProviderSearch=true` to pick the match yourself, or use the
**Remap** action — `^r` in the terminal list, a menu entry under rofi — to
correct a bad match permanently.

**Playback never starts.** Run `otakase -provider-status` to see which
sources are responding. Check `otakase-debug.log`, and raise
`MpvPlaybackStartTimeout` if your connection is slow.

**The rofi menus look wrong.** Themes are rewritten at every launch, so a
stale theme fixes itself on the next run. Confirm `rofi` is installed and
`RofiSelection=true`.

**Progress did not update.** Tracker writes are paced to stay inside API rate
limits and happen in the background. Check `otakase-debug.log` for the write,
and that `TrackingRemote` is what you expect.

## Versions

Releases are numbered by year, the way Minecraft numbers its drops:
`YY.DROP.HOTFIX`. `26.1.0` is the first feature release of 2026, `26.1.1` a
fix on top of it, `26.2.0` the next feature release, and `27.1.0` the first
of 2027. Before 26.1.0 releases used semver (the last was 2.2.2); updating
from those works as usual, since 26 counts as newer than 2.

The running version is at the right of the menu header in the terminal and
of the search bar in rofi, and `otakase -v` prints it. Breaking changes are
called out in [CHANGELOG.md](CHANGELOG.md) rather than in the number. See
[CONTRIBUTING.md](CONTRIBUTING.md) for how a release is cut.

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

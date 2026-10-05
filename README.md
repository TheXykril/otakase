<p align="center">
  <img src="site/img/icon.svg" width="128" height="128" alt="">
</p>

<h1 align="center">Otakase</h1>

<p align="center">Watch anime from the command line, with your list kept in sync.</p>

<p align="center">
  <a href="https://github.com/TheXykril/otakase/releases/latest"><img src="https://img.shields.io/github/v/release/TheXykril/otakase?label=release&color=d2492f" alt="Latest release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/licence-GPL--3.0-8a8276" alt="GPL-3.0"></a>
</p>

<p align="center">
  <a href="https://thexykril.github.io/otakase/">Website</a> ·
  <a href="#install">Install</a> ·
  <a href="https://github.com/TheXykril/otakase/wiki">Wiki</a>
</p>

Otakase finds the episode, plays it in mpv, skips the opening, ending, filler
and recaps, and updates AniList or MyAnimeList when you are done. It works on
Linux, macOS and Windows, with optional rofi menus that follow your desktop
theme.

> **Otakase** continues [curd by Wraient](https://github.com/Wraient/curd),
> released under the same GPL-3.0 licence. The name is *omakase* — 「お任せ」,
> "I'll leave it to you" — with the otaku syllable swapped in. You pick the
> show; it picks the rest.

---

## Features

- Searches several sources at once and falls back when one fails, up to 1080p
- Picks the other audio by itself when a show has only sub or only dub
- Stream, save episodes with `-download`, or cast them to a Chromecast, DLNA TV or Kodi
- Track locally, on AniList, on MyAnimeList, or on both at once
- Skips openings, endings, filler episodes and recaps
- Resumes where you left off, even on another device
- Discord Rich Presence
- rofi menus with poster previews, themed from your desktop colours (Omarchy, pywal, base16, KDE, GNOME, macOS, Windows)
- Its own look in mpv, in the same colours, with a skip button and the next episode a click away

## Install

> Otakase needs **mpv**. Everything else is optional: `rofi` for the graphical
> menus, `libnotify` for desktop notifications, `xdg-utils` to open the
> browser for sign-in, and `ffmpeg` for saving and casting episodes.

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
The installer comes with mpv and adds `otakase` and `otk` to your PATH, so you
can type either in any new terminal. The standalone file needs mpv installed
separately. The rofi interface is Linux-only; Windows uses the terminal menus.

## Coming from curd

Nothing to do. On first launch Otakase copies your curd config, sign-ins and
watch history into its own folders. The originals are left where they were,
so delete them once Otakase looks right. The `curd` command is gone: use
`otakase`, or `otk`. Details are on the [Tracking](https://github.com/TheXykril/otakase/wiki/Tracking) wiki page.

## Usage

Run `otakase` (or the shorter `otk`) to pick from your list. The first time,
it asks how you want to track: **local**, **AniList**, **MyAnimeList**, or
both. AniList and MyAnimeList sign in through your browser.

Otakase opens on your Watching list. The shows you played last are at the
top; pick one to resume where you stopped. In the terminal, **← and →** (or Tab
and Shift+Tab) move between your lists: watching, planning, all and the rest.
Under rofi, Tab does the same and the **Lists** button picks one. **Add new
anime**, at the end of a list, searches AniList and adds a show.

The actions sit beside the list, each on a key you can press from anywhere in
it (`^u` means Ctrl+U): along the bottom in the terminal, as buttons above the
list under rofi. Escape quits.

| Key | Action | What it does |
|---|---|---|
| `^u` | Untracked | Search any show and watch it without touching your list, history or tracker |
| `^e` | Update | Change a show's progress, status or score by hand |
| `^k` | Cast | Turn casting on or off for this run |
| `^g` | Surprise | A random show from Plan to Watch, with Reroll; starting it offers to move it to Watching |
| `^s` | Stats | Your watch totals and recent history ([more](https://github.com/TheXykril/otakase/wiki/Stats)) |
| `^t` | Tracker | Switch between local, AniList, MyAnimeList or both |
| `^o` | Provider | Pick which sources are searched, and in what order |
| `^r` | Remap | Fix a show that plays the wrong anime, by picking the right match (add `REMAP_PROVIDER` to `MenuActions`) |
| `^l` | Continue | Pick up a show you played recently (terminal) |

`MenuOrder` picks your lists and `MenuActions` your actions, both in the
config (`otakase -e`).

Common commands:

```bash
otakase -c                          # continue where you left off
otakase -rofi -image-preview        # graphical menu with posters
otakase -dub                        # dub instead of sub, for this run
otakase -download -episodes 1-12    # save episodes instead of playing (needs ffmpeg)
otakase -cast                       # play on a TV on your network (needs ffmpeg)
otakase -e                          # edit the config file
otakase -u                          # update to the latest release
```

Every flag is listed on the [Command-line Flags](https://github.com/TheXykril/otakase/wiki/Command-Line-Flags)
wiki page.

## Casting

`otakase -cast` plays on a Chromecast, a smart TV (DLNA) or Kodi on the same
network. Keep otakase running while you watch; the terminal shows the
controls. If your TV does not show up, run `otakase -cast-setup` once to let
casting through your firewall.

More in [Casting](https://github.com/TheXykril/otakase/wiki/Casting) and
[Casting Problems](https://github.com/TheXykril/otakase/wiki/Casting-Problems).

## Hyprland keybinding

Installing the package binds **Super+Shift+A** to open the rofi menu, with no
terminal needed. Run `otakase -install-keybind` to add it yourself, or
`otakase -remove-keybind` to take it off. Set `OTAKASE_NO_KEYBIND=1` before
installing to skip it. More on the
[Hyprland Keybinding](https://github.com/TheXykril/otakase/wiki/Hyprland-Keybinding) wiki page.

## MyAnimeList sign-in

MyAnimeList needs your own MAL app credentials, set in the config file or the
environment:

```bash
OTAKASE_MAL_CLIENT_ID=your_client_id
OTAKASE_MAL_CLIENT_SECRET=your_client_secret
```

If the browser reaches the callback page but otakase does not continue, run
the command again and paste the full callback URL when asked.

## Settings

Run `otakase -e` to edit `~/.config/otakase/otakase.conf`. The ones most
people change:

| Setting | What it does |
|---|---|
| `SubOrDub` | `sub` or `dub` |
| `SubsLanguage` | Subtitle language, e.g. `english` or `pt` |
| `Quality` | `best`, `1080`, `720` or `480` |
| `SkipOp` / `SkipEd` | Skip openings and endings (`true` by default) |
| `SkipFiller` / `SkipRecap` | Skip filler episodes and recaps (`true` by default) |
| `NextEpisodePrompt` | Ask before playing the next episode |
| `RofiSelection` | Use rofi menus instead of the terminal |
| `TrackingRemote` | `none`, `anilist`, `myanimelist` or `anilist+myanimelist` |
| `CastDevice` | Cast to this device without asking |
| `Theme` | `auto` follows your desktop colours; `builtin` uses otakase's own |
| `ThemeFile` | Path to a colour palette file to use instead (pywal, base16 or `colors.toml`) |
| `Icons` | Icons in the menus: `auto` (default), `true` or `false` for plain text |
| `ExperimentalMovies` | `true` adds Movies to the menu, for movies and series that aren't anime. Experimental, off by default |
| `MpvSkin` | otakase's look in mpv: `true` (default), `auto` (only if you don't use your own mpv skin), or `false` for mpv's usual look |

Every setting is explained on the
[Configuration](https://github.com/TheXykril/otakase/wiki/Configuration) wiki
page.

## Where your data lives

| Path | Contents |
|---|---|
| `~/.config/otakase/otakase.conf` | Your settings |
| `~/.local/share/otakase/` | Watch history, sign-ins, logs, cached artwork |
| `~/.local/share/otakase/otakase-debug.log` | The log to check first when something misbehaves |

## Help

See [Troubleshooting](https://github.com/TheXykril/otakase/wiki/Troubleshooting)
on the wiki. Still stuck? [Open an issue](https://github.com/TheXykril/otakase/issues)
with the relevant lines from `otakase-debug.log`.

## Credits

Otakase continues [curd](https://github.com/Wraient/curd) by Wraient and its
contributors. [ani-cli](https://github.com/pystardust/ani-cli) and
[jerry](https://github.com/justchokingaround/jerry) came first, and this owes
both. Lists and metadata come from [AniList](https://anilist.co) and
[MyAnimeList](https://myanimelist.net), skip times from
[AniSkip](https://api.aniskip.com/api-docs) and others, and filler episode
numbers from [Jikan](https://jikan.moe/). The player's icons are Google's
[Material Icons](https://github.com/google/material-design-icons).

## Contributing

Building, testing and how releases work are in the
[Developer section of the wiki](https://github.com/TheXykril/otakase/wiki/Developer).

## Licence

GPL-3.0. See [LICENSE](LICENSE).

# Desktop

The desktop app runs on Windows, macOS and Linux. Install links are in the
[README](https://github.com/TheXykril/otakase#install).

## Opening it

Once installed, Otakase is in your app menu (the Start menu, Launchpad or
your Linux launcher) and `otakase` opens it from a terminal; `otakase
--launcher` opens the launcher.

- **Windows:** the installer does both, and adds **Otakase Launcher** to the
  Start menu.
- **macOS:** with Otakase in Applications, choose **Settings > Add to apps and
  terminal** for the command. macOS asks for your password when
  `/usr/local/bin` needs it.
- **Linux:** run the AppImage with `--install`, or accept the offer the app
  makes on its first start (also under **Settings > Add to apps and
  terminal**). It moves to `~/.local/share/otakase-app`, links
  `~/.local/bin/otakase` and adds the app menu entry and icon. If your
  terminal cannot find `otakase`, add `~/.local/bin` to your `PATH`.
  `otakase --uninstall` takes it all out again and keeps your profiles and
  progress.

## The window

The pages sit in a bar at the top, with the window buttons in the style of
your system. **Esc** goes back. Moving the mouse over the
player shows its controls.

## Player keys

The player takes mpv's keys. **?** shows this list in the player.

| Key | Does |
| --- | --- |
| Space / P | Play / pause |
| ← / → | Seek 5 seconds (Shift: 1 second) |
| ↑ / ↓ | Volume (9 / 0 for small steps) |
| M | Mute |
| F | Fullscreen (Esc leaves it) |
| Enter | Skip the opening or ending |
| < / > | Previous / next episode |
| PgUp / PgDn | Previous / next chapter |
| J / Shift+J | Next / previous subtitles |
| V | Subtitles on / off |
| Z / X | Subtitles earlier / later |
| # | Next audio track |
| [ / ] | Slower / faster ({ / } halve or double, Backspace back to normal) |
| , / . | Frame back / forward |
| S | Screenshot, saved in Pictures/Otakase |
| Q | Leave the player |
| Alt+O / Alt+E | Mark the opening / ending; press again where it ends |
| Alt+S | Send your marks to AniSkip |
| Alt+R | Forget your marks |
| Alt+U / Alt+D | The skip you got is right / wrong |
| Alt+K / Alt+N | In a watch party: vote to skip / for the next episode |

## The launcher

Starting the app with `--launcher` opens a fixed-size popup. The menu fades
in unless your system asks for reduced motion, and closes when it loses
focus. Type to filter, use the arrows or PageUp/PageDown to move, and press
**Enter** to pick. **Esc** clears your filter, goes back, then closes the
menu. Starting playback or opening the full app restores the normal window
controls.

With **Movies & shows** on for your profile, the launcher lists the films
and series you are watching and your Trakt watchlist, searches them along
with anime, and has a **Trending movies & shows** list.

The popup opens above other windows on Windows, macOS and X11. On Hyprland,
sway and i3, Otakase centers the floating popup and brings it forward.
On Wayland, the compositor controls stacking and its own window shortcuts.

Bind it to a keyboard shortcut in your desktop's settings to open Otakase
from anywhere. On Hyprland, for example:

```
bind = SUPER SHIFT, A, exec, ~/.local/bin/otakase --launcher
```

## Discord status

With the Discord app open on the same computer, your Discord status shows
what is playing: the show, the episode, its cover and the time left, with
links to AniList and MyAnimeList (or a *Watch with me* button while Watch with
me is on). Films and series show too. Turn it off under **Settings →
Playback → Show what I watch in Discord**.

## Updates

The app checks for new releases and installs them:

- **Windows:** runs the new installer silently.
- **macOS:** swaps in the new app. It is not signed by Apple yet, so the
  first start needs right-click > Open.
- **Linux:** replaces the AppImage it was started from (the installed one
  after `--install`, so the menu entry and `otakase` stay the same). A copy it cannot
  replace (installed by a package manager, for example) gets a link to the
  download page instead.

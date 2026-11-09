# Desktop

The desktop app runs on Windows, macOS and Linux. Download it from the
[website](https://otakase.xyverion.com/#download).

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

## Episode browsing

**Go to episode** accepts number keys or its keypad and selects the episode in
the appropriate range without playing it. The title line and **Episode details**
show the focused or hovered episode; the Menu or Info key on a focused card
also opens its details. A pointer long press opens details on phone or desktop.

Focus outlines appear when you navigate with arrow keys or Tab. Moving, clicking
or scrolling the mouse hides the outline while keeping the logical focus.

**Settings > Playback > Hide episode spoilers** defaults off. When enabled,
unwatched titles are hidden and artwork is blurred behind a crossed-out eye.
Reveal an episode from its details for the current visit without marking it watched.

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
in on open and fades out when dismissed or when the popup loses focus.
Reduced motion skips both fades. Type to filter, use the arrows or
PageUp/PageDown to move, and press **Enter** to pick. **Esc** clears your
filter, goes back, then closes the menu. Ctrl+J and Ctrl+K move down and up.
Going back restores the previous filter and highlighted row. Type parts
of a title such as “solo lev” to filter, or choose **Search for…** to search
the catalog. The shortcut closes the popup when pressed again.

Playing from the popup fades out the menu and opens a normal player window.
On Hyprland with Wayland layer shell, playback and the full app open in
separate windows. Locked profiles ask for the profile code in the new
window. Other desktops keep the unlocked session when reusing the popup
window. Closing playback returns control to the desktop.

When your profile shows movies & shows (**More > What you watch**), the launcher lists the films
and series you are watching and your Trakt watchlist, searches them along
with anime, and has a **Trending movies & shows** list.

On Hyprland with Wayland layer shell version 4 or later, the launcher
opens as a centered overlay. Hyprland’s window drag and resize shortcuts
leave the overlay in place. On older Hyprland sessions, the popup falls
back to a floating window and returns to center after a compositor drag.
On sway and i3, the floating popup opens on the focused workspace. Windows,
macOS and X11 open the popup above other windows on the display under the
pointer.

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

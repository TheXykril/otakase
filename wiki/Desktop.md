# Desktop

The desktop app runs on Windows, macOS and Linux. Install links are in the
[README](https://github.com/TheXykril/otakase#install).

## The window

The pages sit in a bar at the top, with the window buttons in the style of
your system. **Esc** goes back. Moving the mouse over the
player shows its controls.

## The launcher

Starting the app with `--launcher` opens a small window that stays on top, for when you
only want to pick something and watch: type to filter, arrows or
PageUp/PageDown to move, **Enter** to pick, **Esc** to clear the text, go back,
then close. Playing an episode grows it to fit the player.

Bind it to a keyboard shortcut in your desktop's settings to open Otakase
from anywhere. On Hyprland, for example:

```
bind = SUPER SHIFT, A, exec, ~/Applications/otakase-app-linux-x86_64.AppImage --launcher
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
- **Linux:** replaces the AppImage it was started from. A copy it cannot
  replace (installed by a package manager, for example) gets a link to the
  download page instead.

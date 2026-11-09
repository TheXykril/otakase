<p align="center">
  <img src="site/img/icon.svg" width="128" height="128" alt="">
</p>

<h1 align="center">Otakase</h1>

<p align="center"><b>Leave it to Otakase.</b><br>Anime, movies and shows, served on every screen you own. One app that plays whatever you want, remembers every episode and picks up wherever you are. You pick, Otakase handles the rest.</p>

<p align="center">
  <a href="https://github.com/TheXykril/otakase/releases/latest"><img src="https://img.shields.io/github/v/release/TheXykril/otakase?label=release&color=d2492f" alt="Latest release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/licence-GPL--3.0-c99a2e" alt="GPL-3.0"></a>
  <a href="https://otakase.xyverion.com/discord/"><img src="https://img.shields.io/badge/discord-join-d2492f?logo=discord&logoColor=white" alt="Discord"></a>
  <a href="https://github.com/TheXykril/otakase/releases"><img src="https://img.shields.io/github/downloads/TheXykril/otakase/total?label=downloads&color=c99a2e" alt="Downloads"></a>
  <a href="#install"><img src="https://img.shields.io/badge/platform-android%20%C2%B7%20android%20tv%20%C2%B7%20windows%20%C2%B7%20macOS%20%C2%B7%20linux-c99a2e" alt="Android, Android TV, Windows, macOS, Linux"></a>
  <a href="#features"><img src="https://img.shields.io/badge/sync-AniList%20%C2%B7%20MAL%20%C2%B7%20Trakt-c99a2e" alt="Syncs with AniList, MyAnimeList and Trakt"></a>
</p>

<p align="center">
  <a href="https://otakase.xyverion.com/">Website</a> ·
  <a href="#install">Install</a> ·
  <a href="https://github.com/TheXykril/otakase/wiki">Wiki</a> ·
  <a href="https://otakase.xyverion.com/discord/">Discord</a>
</p>

Otakase finds the episode, plays it, skips the opening and ending, and
updates AniList or MyAnimeList when you are done. One app for Android phones,
Android TV, Windows, macOS and Linux.

> *Omakase* (「お任せ」) means "I'll leave it to you". Add a little *otaku*
> and you get *otakase*: pick a show and leave the rest to it.

---

## Features

- Pick a show and press play: no links to hunt for and nothing to set up
- Syncs progress, status, scores and resume position with AniList, MyAnimeList, or both
- Skips openings, endings and filler episodes
- Resumes where you left off, on any of your devices
- Continue Watching, Up next with countdowns for airing shows, and a weekly Schedule
- Up to six profiles, each with its own accounts, history and settings
- Android TV with full remote control, sign-in by QR code, and your phone as a remote
- On the computer: a full window, or a small keyboard launcher you can open from a shortcut
- Works with the Otakase Discord: watch rooms, queued shows, and problem reports from the app
- Updates itself on every platform

## Install

The app has its own player, so there is nothing else to install.

### Android phone and Android TV

Install [otakase-android.apk](https://github.com/TheXykril/otakase/releases/latest/download/otakase-android.apk)
(Android 7.0 or newer). Android asks once to allow installing apps from your
browser or file manager.

### Windows

Run [otakase-app-windows-setup.exe](https://github.com/TheXykril/otakase/releases/latest/download/otakase-app-windows-setup.exe).
It installs for your user only, so it needs no administrator rights, and adds
Otakase to the Start menu and the `otakase` command to your terminal.

### macOS

Download [otakase-app-macos.zip](https://github.com/TheXykril/otakase/releases/latest/download/otakase-app-macos.zip),
unzip it and move **Otakase** to Applications. It works on Apple Silicon and
Intel. The app is not signed by Apple yet, so the first time, right-click it
and choose **Open**. For the `otakase` terminal command, choose **Settings >
Add to apps and terminal** in the app.

### Linux

Download [otakase-app-linux-x86_64.AppImage](https://github.com/TheXykril/otakase/releases/latest/download/otakase-app-linux-x86_64.AppImage)
and install it:

```bash
chmod +x otakase-app-linux-x86_64.AppImage
./otakase-app-linux-x86_64.AppImage --install
```

That puts Otakase in your app menu and the `otakase` command in
`~/.local/bin`, for your user only; afterwards you can delete the download.
Running the AppImage without `--install` also works, and the app offers to
install itself the first time. `otakase --uninstall` removes it again and
keeps your profiles and progress.

### Updates

The app checks for new releases itself. On Android open **More > About &
updates**, choose **Download update**, then **Install update**. On the
computer it offers the update and installs it for you. Your profiles and
progress stay in place.

## Getting started

Sign in under **Accounts** to sync with AniList or MyAnimeList; you can sign in
to both. Without an account, Home shows trending and seasonal anime and keeps
your progress on the device. On a TV, scan the QR code with your phone to sign
in, with both on the same home network.

Pick a show, pick an episode, and it plays. **Sub/Dub** and the **Tracks**
button in the player (the subtitles icon on phones and TVs; hold an icon to
see its name) choose the audio and subtitles. Playback settings (skips
and even out volume) are under **Settings**.

On the computer, starting the app with `--launcher` opens the keyboard launcher instead of the
window: type to filter, Enter to pick, Esc to go back. Bind it to a desktop
shortcut to open Otakase from anywhere.

More on the [wiki](https://github.com/TheXykril/otakase/wiki).

## Help

See [Troubleshooting](https://github.com/TheXykril/otakase/wiki/Troubleshooting)
on the wiki. **Diagnostics** in the app shows what went wrong. Still stuck?
Ask on [Discord](https://otakase.xyverion.com/discord/) or [open an issue](https://github.com/TheXykril/otakase/issues).

## Licence

GPL-3.0. See [LICENSE](LICENSE).

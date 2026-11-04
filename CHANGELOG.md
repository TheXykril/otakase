# Changelog

## Unreleased

### Fixed

- The desktop launcher opens as a fixed-size popup, fades in and closes when
  it loses focus. Playback and the full app use normal window controls.

## 26.1.7 - 2026-11-04

### Fixed

- **Anime episodes save your place when you leave early.** Otakase marks
  episodes watched based on playback and ending credits instead of the old
  85% threshold. Automatic opening and ending skips look for missing markers
  across enabled sources.
- **AniList and MyAnimeList keep progress in episode order.** Otakase remembers
  episodes watched out of order, advances tracker progress once you fill the
  gap, and retries failed completion updates.

## 26.1.6 - 2026-11-03

Phones find TVs and computers for the remote more often, and you can pair by
typing the code. Watch rooms play in the app again.

### Fixed

- **Watch rooms play in the app.** Rooms no longer stop with "Retry or leave
  the room", and one account can watch in the Discord Activity and the app
  at the same time.
- **Phone remote finds TVs and computers more often.** The phone now also
  checks every device on the Wi-Fi directly when broadcasts get lost, and a
  computer no longer shows a Docker or VM address in its QR code.
- **Pair by typing the code.** Control TV has an Enter code button. Type the
  six digits and, if the TV is not found, the address shown under the code.

## 26.1.5 - 2026-11-03

18+ titles now stay off Home, even after you watch them.

### Changed

- **AdultContent: 18+ titles no longer appear on Home**, even after you
  watch them. They stay in your lists and search when your account allows
  them.

## 26.1.4 - 2026-11-03

Movies and shows now pick up where you left them, on every device signed in
to Trakt.

### Fixed

- **Movies and shows resume where you stopped.** A spot saved on this
  device is no longer replaced by an older one from Trakt, and spots that
  never reached Trakt are sent on the next sync. Leaving the app pauses
  playback and saves your spot, and the player checks Trakt for a newer
  one before it starts, so you carry on from another phone, TV or
  computer.
- **Watching a film again** resumes from where you are and keeps it in
  Continue. Downloaded files remember where you left them too.
- **The newest movie stays first in Continue** when two saves land at the
  same moment.

### Removed

- **Wrong show?** and **Find it myself** are gone, along with their own
  search. Shows are always matched automatically, and picks made with it
  are no longer used.

## 26.1.3 - 2026-11-02

Sign-ins now stay saved on Linux, and Trakt signs in through your browser
on a computer. If you were signed out, sign in once more after updating.

### Changed

- **Trakt signs in through your browser on a computer**, like AniList and
  MyAnimeList, instead of with a code. Phones and TVs keep the code.

### Fixed

- **Sign-ins stay saved on Linux.** AniList, MyAnimeList and Trakt no
  longer sign out after a restart or an update. If you were signed out,
  sign in once more. Desktops without a keyring keep sign-ins in a private
  file instead.
- **Open Trakt works on phones.** The button opens Trakt's code page
  again, and phones no longer show a QR code for it; TVs still do.

## 26.1.2 - 2026-11-02

The desktop player takes mpv's keys again, the launcher carries your movies
and shows, and desktop and TV now look and feel the same.

### Added

- **mpv's keys in the desktop player.** Seek, volume, subtitles, audio
  tracks, speed, frame steps, screenshots, previous and next episode, and
  the Alt keys for marking skip times. Press **?** in the player for the
  list.
- **Movies and shows in the launcher.** With Movies & shows on for your
  profile, the launcher carries on your films and series, shows your Trakt
  watchlist and trending titles, and searches them with anime.
- **Quality list for movies and shows.** The quality button opens a list
  of the sizes on offer.

### Changed

- **Same look on desktop and TV.** Trakt sits in Accounts next to AniList
  and MyAnimeList. The TV uses the desktop's top bar and episode cards, and
  More on phone and TV follows the desktop sidebar order.
- **Desktop tabs open on click** instead of on hover, and covers lift when
  you point at them.

### Fixed

- **The launcher is a popup again on Hyprland, sway and i3.** It floats in
  the middle of the screen instead of opening as a tile, and closes when
  you click away.

## 26.1.1 - 2026-11-02

The desktop app now installs like any other app, so it opens from your app
menu and as `otakase` in a terminal.

### Added

- **Opens like any other app.** From the Start menu, Launchpad or your
  Linux app menu, and as `otakase` in a terminal. On Linux,
  `--install` sets the AppImage up for you (the app offers it on first
  start).

## 26.1.0 - 2026-11-02

The first release of the Otakase app. It replaces the command-line version
and starts the version numbers over.

### Added

- **One app for every screen.** Android phones, Android TV, Windows, macOS
  and Linux, with its own player built in, so there is nothing else to
  install.
- **Pick a show and press play.** Otakase finds the episode, skips openings,
  endings and filler, and resumes where you left off on any of your devices.
- **AniList and MyAnimeList sync.** Progress, status, scores and resume
  position, with one or both accounts. Without an account, progress stays on
  the device.
- **Movies and shows.** Each profile can show anime, movies and series, or
  both, with Trakt sync for movies and shows.
- **Made for the TV.** Full remote control, sign-in by QR code from your
  phone, and your phone as a remote. On the computer, a full window or a
  small keyboard launcher (`--launcher`).
- **Continue Watching, Up next and a weekly Schedule**, up to six profiles,
  casting to your TV, and the Otakase Discord in the app: watch rooms,
  queued shows and problem reports.
- **Updates itself on every platform.** Android downloads and installs the
  new version from **More > About & updates**; on the computer Otakase
  installs it and restarts.

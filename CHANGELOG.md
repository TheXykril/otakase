# Changelog

## Unreleased

## 26.2.3 - 2026-11-10

Everything plays in one player, casting gets past firewalls, closing the app mid-show no longer crashes it, and movie search gets filters.

### Added

- **Movie and show search has filters,** like anime: type (movies or
  series), year, genre, series status and sort. Filters alone browse
  popular or trending titles, no search text needed.
- **Search can be sorted** by Popular, Trending, Top rated or Newest, for
  anime too.
- **More genres for movies and shows,** such as Crime, Documentary, Family,
  War and Western.

### Changed

- **Anime, films, shows and downloads now play in one player.** Every title
  gets the same controls, keys, casting, stall recovery and quality list
  (with Try another copy). Buttons for things a title has no data for, such
  as skip times for films, stay hidden.
- **The website is now otakase.com.** Links in the app, help pages and
  Discord point there. The old address forwards to it, and updates keep
  working on older versions.

### Fixed

- **Closing the app while something plays no longer crashes it.** Super+W,
  Alt+F4, Cmd+Q, the task bar or logging out now save your spot, stop the
  player, casting and any external mpv window, then quit cleanly.
- **Leaving a film right as it starts keeps your spot.** Backing out while
  it was still loading reset where you were to the start, here and on Trakt.
- **Skipping to the end no longer marks a film or episode watched.** Only
  watching it through counts, like anime.
- **Films finished during the credits stay finished on Trakt** and no longer
  come back to Continue watching.
- **Try another copy and quality changes keep your place,** audio and
  subtitles in films.
- **The phone remote and Discord controls work for films and shows,** and
  so do the Play, Pause, Stop, Rewind, Fast forward and Next buttons on TV
  remotes.
- **Series wait 10 seconds before the next episode** with Play next and
  Cancel, or ask first when the next episode prompt is on.
- **Downloads count as watched.** Episodes and films played from Downloads
  now update AniList, MyAnimeList, Trakt and Continue watching (new downloads).
- **A short placeholder clip no longer marks an anime episode watched;** the
  next copy is tried instead.
- **Anime keeps playing on a computer when the window loses focus,** like
  films.
- **Casting works behind a firewall.** The computer now sends casts from a
  fixed port instead of a random one a firewall blocks, so the TV no longer
  sits on loading. If the TV still cannot reach the computer, the stream goes
  straight to the TV when it can, or the app says so and offers **Allow
  casting**, which opens the firewall after your password (Linux) or admin
  approval (Windows).
- **Phones and Android TVs find Chromecasts again.** Android 11 and later
  hid the network from the search, so only the browser showed up.
- **A TV that gives up on a stream says so** instead of loading forever.
- **macOS asks for local network access** so it can find TVs.

## 26.2.2 - 2026-11-10

Films that drop no longer end as watched, Trakt stays signed in, and voices stay clear with Even out volume.

### Fixed

- **Films no longer stop early as watched.** When a stream dropped partway
  through a film or episode, the player closed and marked it watched (also on
  Trakt). Now it reconnects at the same spot, then tries another copy from
  there, and only the real end counts as watched.
- **Trakt stays signed in.** Opening the app after a while (for example after
  an update) could sign you out of Trakt. Otakase now refreshes the sign-in
  once instead of several times at the same moment, and keeps it when Trakt or
  the network is briefly down.
- **Voices stay clear with Even out volume.** Loud effects or music no longer
  push dialogue down to a whisper. Surround soundtracks now fold down with the
  dialogue channel up front, and loud bangs are tamed before the volume is
  levelled, so voices near them keep their level.

### Improved

- **No more typing the Trakt code.** On phones, **Open Trakt** opens the
  activation page with the code already filled in (and copies it, just in
  case). On TVs the QR code carries the code too. Tap the code to copy it.

## 26.2.1 - 2026-11-09

Fixes for phone screens, downloads that stop when the phone sleeps, settings backups and problem reports, plus tidier settings.

### Improved

- **Tidier settings.** What you watch is gone. Appearance is now **Display**
  and starts with the choice of anime, movies & shows, or both. Movie & show
  languages sit in **Subtitles & languages** next to the anime ones. Movie
  sources no longer have on/off switches; all of them are tried in your
  language order.
- **Downloads keep going when the phone sleeps.** On Android phones and TVs
  the screen stays on while an app update or episode downloads, and the
  download carries on in the background (with a notification) if the screen
  turns off or you switch apps. New **Keep screen on** setting in Display
  keeps the screen on the whole time Otakase is open.

### Fixed

- **Settings backup shows the right device.** A backup made on a computer no
  longer says it came from Android; it names Windows, macOS, Linux, Android or
  Android TV.
- **Players fill the whole phone screen.** The side with the camera cutout is
  no longer left empty; controls stay clear of it, and pages keep their content
  beside the cutout in landscape.
- **Problem reports share less.** Report a problem leaves out web addresses
  and other details that aren't needed, and the film player's title line shows
  just the year and language.

## 26.2.0 - 2026-11-09

Movies and shows join Home and Search and can be cast to your TV, one player look everywhere, and an Android TV update fix.

### New

- **Cast movies and shows.** The cast button now works in the film and series
  player too, on phones and computers. Play, pause and seeking control the
  TV, the next episode follows onto it, and progress and Trakt keep updating.
  Stop casting picks up on this screen where the TV was. Downloads still play
  on this screen only.

### Improved

- **Clearer settings.** The Movies & shows section is now **What you watch**,
  since it is where a profile picks anime only, movies & shows only, or both.
  Movie languages and sources show there once movies are on. Subtitle
  colours, background, style, sub or dub and quality show plain names instead
  of codes, and many descriptions say what the setting does in plain words.
- **Anime, movies and shows share Home and Search.** With both turned on in
  What you watch, there is no separate Movies tab. Films and series you are
  watching show in the big resume card, the desktop banner and Continue
  watching; trending movies and shows join the desktop banner and the home
  you see before signing in. Signed in to Trakt, your watchlist gets its own
  row. Search lists anime first, then movies and shows, and the filters apply
  to both. Turn one off and only its parts disappear.
- **The phone and TV home banner takes turns like the desktop one.** It moves
  between what you are watching and trending picks every few seconds, pausing
  after you touch it; swipe it or tap the dots on a phone.
- **Schedule moved to More on desktop too.** The tab bar is just Home, Search
  and More everywhere.
- **Films, series and anime share one player look on phones and TVs.** Back,
  title and cast sit on top, −10 / play / +10 in the middle, and the timeline
  with times at the bottom.
- **Player buttons are plain icons.** Next, Audio & subs, Quality and Skip
  times sit on the time row; hold an icon to see its name, or on TV the focused
  icon shows its name. Skip opening is a small chip instead of a round button.

### Fixed

- **Android TV updates no longer report a wrong signing key on some firmware.**
  When Android leaves out certificate details, the updater asks again the
  older way. Different keys are still rejected, and unreadable certificates
  show their own message.

## 26.1.8 - 2026-11-07

Subtitle fixes, easier episode browsing on TV, and clearer menus and playback feedback.

### Improved

- **More and desktop Settings are easier to browse.** Watching, profile,
  device, community and help options are grouped together.
- **Accounts shows tracker sync status.** See updates being sent, updates
  that need attention, and the last update sent this session.
- **Playback problems are easier to report.** Review a prefilled report with
  optional playback details before sending, or copy it without linking Discord.
- **TV episodes use a desktop-style thumbnail grid.** Browse four columns
  directly below the anime details, jump by episode number without autoplay,
  and read full titles and episode details with the remote.
- **Optional spoiler protection hides unwatched episode titles and artwork.**
  It is off by default. Desktop focus outlines appear for keyboard navigation
  and hide again when using the mouse.

### Fixed

- **Automatic subtitles prefer full dialogue over forced or signs-only tracks.**
  The same selection order applies to anime, movies and casting, with closed
  captions available before forced tracks when full dialogue is unavailable.
- **Malformed subtitle files no longer hide later timed dialogue.** Untimed
  WebVTT text blocks are removed before local playback or cast preparation
  so the player can continue reading the remaining cues.
- **Failed searches and episode lists offer clearer retries.** Retry keeps
  filters and loaded pages. Movie search and episode loading offer Retry,
  and partial movie-list failures keep the titles that loaded successfully.
- **Unavailable playback and downloads explain why they cannot be used yet.**
  Browse errors use plain wording without internal service names or URLs.
- **The desktop launcher opens as a fixed popup without a large-window flash.**
  It fades in and out, closes on a second shortcut press, restores filters,
  matches partial title words and opens playback in a regular window. The
  Hyprland overlay stays fixed during window drags; Esc works after the
  search field loses focus.

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

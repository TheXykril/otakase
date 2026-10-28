# Changelog

## Unreleased

- Android: Kage Home, Search and More for phones and TV, automatic playback choices, AniList/MAL login, tracker updates and compatible resume sync.
- Android: 18+ visibility follows the signed-in AniList preference, with no local switch.

## 26.7.0 - 2026-10-04

### Added

- **otakase's own look in mpv.** The player gets a top bar with a back
  button, the show's title, the episode's name and tracker and source chips,
  a thin seek bar with openings and endings marked, a round play button,
  volume, episode, audio and subtitle menus, quality and fullscreen. A Skip
  Opening/Ending button (or Undo after an automatic skip) and an Up next
  card during the ending. Colours follow your palette. Your own mpv.conf,
  input.conf and scripts still load.
- **`MpvSkin` setting.** `true` (the default) always uses the new look;
  `auto` uses it only when you don't run an mpv skin of your own or set
  `osc=no`; `false` keeps mpv's usual look. It is left out for mpv older than
  0.37, IINA and mpv-android.
- **Menus follow your desktop theme.** Besides Omarchy, otakase now picks up
  pywal, wallust and matugen, tinty or stylix base16 schemes, the KDE Plasma
  colour scheme, GNOME's accent and dark style, and the macOS or Windows
  accent and light/dark mode. `Theme` can force any one of them.
- **`ThemeFile` setting.** Points at a palette file (pywal `colors.json`, a
  base16 scheme or a `colors.toml`) for any other theme. A light palette
  backs light desktops.
- **winget.** Each release is sent to winget, so once the package is
  accepted there `winget install TheXykril.Otakase` installs otakase on
  Windows and `winget upgrade` keeps it current.

### Changed

- **Shu colours by default.** The builtin palette moves from sky blue on
  black to warm near-black, cream text and a vermilion accent, matching the
  new website and icon.
- **The Windows installer adds otakase to PATH.** `otakase` and `otk` run
  from any new terminal, and uninstalling removes it again. It installs as
  64-bit.

## 26.6.0 - 2026-10-03

### Added

- **A toolbar in rofi.** The list and the poster grid have a row of buttons
  above them: Lists, then your actions (Untracked, Update, Cast, Surprise,
  Stats, Tracker, Provider) and Quit. Each button shows its key, `Update ^e`,
  and is pressed by a click or that key. Tab moves to the next list, Lists
  picks one with its count, and escape quits.
- **`MenuActions` setting.** `MenuOrder` now names the lists
  (`CURRENT,PLANNING,ALL` by default) and `MenuActions` the things to do, in
  the order the bottom bar and the toolbar show them. `CONTINUE_LAST` in it
  puts the shows to continue at the top of Watching.
- **Launch and close notifications.** Started from rofi, otakase says it is
  starting right away and says when it closes. A cast in its own window
  sends the closing notice when that window ends.

### Changed

- **otakase opens on Watching.** The menu in front of the lists is gone; the
  shows you were watching lead the list, with the other lists a Tab away.
  Your settings carry over: an unchanged menu gets the new default, and one
  you set up keeps every entry and its order, its actions moved to
  `MenuActions`.
- **Colours with a meaning in rofi.** Shows to continue take the theme's
  accent, where a show stopped takes its yellow, titles stay plain, and the
  rest is dimmed but kept readable. Quit, counts and the search hint no
  longer fade into the background on darker themes. The selected row
  brightens instead of turning accent.
- **Cast shows its state.** The cast entry drops its checkbox: the icon is
  crossed out while casting is off and the words say `Cast: Off`,
  `Cast: On` or `Cast: On | device`. The terminal's list tabs get icons.
- **`CurrentCategory` and `-current` are gone.** otakase always opens on
  Watching (or the first list `MenuOrder` names), so they had nothing left
  to do.

### Fixed

- **One play icon on continue rows.** Continue Watching and Surprise Me rows
  showed two.

## 26.5.0 - 2026-10-03

### Added

- **Icons in the menus.** Menus in rofi and the terminal show small line
  icons in front of their entries: the main menu, list categories, the
  episode, cast, quality and update menus. otakase carries the few glyphs it
  needs in a tiny font and installs it for you on Linux. The new `Icons`
  setting picks `auto` (the default: on where the font can be shown), `true`
  or `false`. Rows that are values, like titles or qualities, stay plain.
- **Read the changelog before updating.** The update prompt has a "Read the
  changelog" page with every release between yours and the new one, grouped
  into Added, Changed and Fixed, and an entry to open the release page in
  your browser.

### Changed

- **Updates are offered in the same run.** The update check starts at launch,
  alongside the tracker sign-in, and a new release is offered before the
  first menu instead of on the next launch.
- **A cleaner update prompt.** It shows a short summary of what is new, read
  from this changelog, instead of the raw release text, and long notes no
  longer scroll off the screen in the terminal.
- **No emoji in the episode jump menu.** Its entries use the same icons as
  the other menus, or plain text with icons off.

## 26.4.0 - 2026-10-02

### Added

- **otakase now shows when it is working.** Slow steps (searching AniList or
  the providers, finding an episode's stream, looking for cast devices,
  refreshing your list, checking where you left off, and a slow start) show a
  spinner with the step and the seconds spent in a terminal, or a desktop
  notification when launched from rofi. Quick steps stay silent, and the
  indicator hides while a menu is open.

### Changed

- **Notifications update in place.** A message that repeats, like a step
  counting its seconds, now changes one notification instead of stacking new
  ones, on GNOME, KDE, mako and dunst alike. Progress notifications go away
  shortly after the step finishes.

## 26.3.1 - 2026-10-02

### Fixed

- **DLNA casts no longer stop after a couple of minutes** on TVs that keep
  reporting the same playback position (seen on Hisense). otakase now counts
  the time itself when the TV's position stops moving, so the cast keeps
  going and progress is still saved.

## 26.3.0 - 2026-10-02

### Added

- **Rescan for devices.** The cast device list (rofi and terminal) ends with
  a Rescan entry, for a TV that was still waking up.
- **A menu when no devices are found**, instead of an error: Rescan, Fix the
  firewall now (when ufw or firewalld is on), or Cancel.

### Changed

- **The device is always confirmed.** otakase asks which device to cast to
  even when only one is found. `CastDevice` still skips the question, and a
  device picked earlier in the run is reused for the next episode.

### Fixed

- **TVs that ask to allow this device.** LG webOS, Hisense and other DLNA TVs
  show an "allow this device?" prompt the first time. otakase now waits up
  to two minutes for you to accept it on the TV, instead of failing.

## 26.2.0 - 2026-10-02

### Added

- **`otakase -cast-setup`** lets casting through your firewall in one go.
  It fixes `CastPort` and `CastDiscoveryPort` if they are random, shows the
  rules for ufw or firewalld (local network only, tagged `otakase cast`),
  runs them with sudo after you say yes, then lists the devices it finds.
- **Fix the firewall now.** When a cast search finds no devices and ufw or
  firewalld is on, the menu offers to fix it there and searches again. The
  password prompt is the same one `otakase -u` uses and lists every command
  it will run.
- **`CastDiscoveryPort`** (UDP, default `8011`, `0` for random). DLNA TVs
  answer the search on this port, so one firewall rule lets them in instead
  of opening all UDP. Existing configs get it on upgrade.

### Changed

- **"No devices found" says why.** With a firewall on, the message carries
  the exact rules for your network and copies them to the clipboard.

## 26.1.2 - 2026-10-01

### Added

- **`ContinueWatchingAdult`** (default `false`). With `AdultContent=true`,
  it also lets titles marked 18+ into the continue-watching rows.

### Changed

- **Continue-watching rows leave out titles marked 18+** unless
  `ContinueWatchingAdult` is on. Before, `AdultContent=true` was enough, and
  with local-only tracking they were never left out.
- The update prompt no longer fetches a separate extra-notes file; release
  notes are all in one place.

## 26.1.1 - 2026-10-01

### Fixed

- **New settings reach existing configs.** Options added in 26.1.0 and
  earlier (`ContinueWatchingRows`, `Quality`, `DownloadFormat`, the Kodi and
  cast options and others) were missing from configs created before them.
  They are now added on upgrade.
- **Titles marked 18+ keep to their own source.** With `AdultContent=true`
  they are looked up only on the provider meant for them, and other shows
  no longer search it. A saved match that pointed one elsewhere is searched
  again, and asked for sub, dub-only copies are picked last.
- **Streams from that source start reliably.** They get up to 120 seconds to
  begin instead of 20, only the episode being watched downloads, and a slow
  start no longer ends in a false "no dub, playing sub".
- Search results no longer list one show as several rows such as
  "Title - 06 - English Softsubs".

## 26.1.0 - 2026-10-01

Releases are now numbered by year: `YY.DROP.HOTFIX`. 26.1.0 is the first
feature release of 2026 and updates from 2.2.2 like any other version. The
version is shown in the menu header and the rofi search bar.

### Added

- **Cast to DLNA TVs and Kodi.** `-cast` lists DLNA/UPnP TVs (marked
  `· DLNA`) and Kodi (marked `· Kodi`) next to Chromecasts, with subtitles
  burned in. Pause, seek, volume and stop work from the terminal as before.
  Kodi needs its web server on; set `KodiUser`/`KodiPassword`, and `KodiHost`
  if it is not found on its own.
- **Next episode after a cast.** `CastNextEpisode=countdown` (the default)
  counts down 10 seconds on the panel and plays the next episode, any key
  stops it; `stop` ends with the video. `NextEpisodePrompt` is now mpv only.
- **Switch sub/dub while casting** with `a`: the episode restarts in the
  other audio at the same spot.
- **Sub or dub is remembered per show.** Switching audio in mpv or on a cast
  sets that show's default; other shows keep `SubOrDub`. `-sub`/`-dub` still
  win for one run.
- **Subtitle language is remembered per show.** Switching with `j` in mpv
  sets it for that show; others use `SubsLanguage`.
- **Quality per show.** `Quality` (`best`, `1080`, `720`, `480`) applies to
  mpv, casts and downloads, and Update → *Quality for this show* overrides
  it for one show.
- **Downloads save as MKV** by default, keeping every audio track and styled
  subtitles, with a `.otakase.json` file of episode info next to each one.
  `-download-format mp4` and `DownloadFormat` choose MP4. Episodes already
  downloaded are skipped.
- **Continue watching.** The menu opens with up to 5 rows like
  `▶ Frieren · ep 13 at 12:34` that resume at that spot.
- **Surprise Me** (`ctrl+g`) picks a random Plan to Watch show, with reroll,
  and offers to move it to Watching when you start it.
- **Stats** (`ctrl+s`): episodes, time watched, mean score and recent
  history, read from your tracker.
- **Already watched some elsewhere.** Adding a show can record the episodes
  you have seen and start at the next one, or mark it completed.
- **Watch position syncs between devices** through a tag in your AniList
  notes / MyAnimeList comments, so another machine resumes at the same spot.
- **Dev builds.** `DevBuilds=true` also offers the `dev` pre-release, built
  from unreleased work, in the update check.
- **`AdultContent`** (default `false`) hides titles marked 18+ from your
  lists, Surprise Me and AniList search; they stay on your tracker and keep
  syncing. Set it to `true` to show them and to enable a provider for them.

### Changed

- A saved custom `MenuOrder` gains the new Surprise Me and Stats rows once.
- When the tracker is ahead of your local history (another device, Update),
  the tracker's episode plays instead of the stale local one.

### Fixed

- **Shows private on AniList were copied to MyAnimeList**, where anyone can
  see them. They are no longer added to or updated on MyAnimeList. Ones
  already copied stay there until you delete them.
- **anizone passed its Japanese stream off as a dub.**
- **`AutoAudioFallback` still asked sub or dub** on some paths.
- **Torrent (nyaa) downloads failed**, and ffmpeg before 7.1 rejected the
  `-extension_picky` option.
- **The rofi Stats page showed two Back rows.**

## 2.2.2 - 2026-09-30

### Added

- **Recap scenes are skipped.** A "previously on" stretch inside an episode
  is skipped under `SkipRecap` when AniSkip has it on file, in mpv and on a
  cast.
- **Untracked watching skips openings and endings in mpv.** It used to look
  up skip times only when casting. The show is matched to AniList by exact
  title only, so a near miss gets no skips rather than another season's.
- **The cast panel marks the opening and ending** on its progress bar.

### Fixed

- **The next episode showed the previous episode's subtitles** in a running
  mpv, and could play with none of its own.
- **Skips landed in the wrong place.** An AniSkip entry timed against another
  cut of the episode (one without the cold open) skipped 0:00–1:00 of an
  episode whose opening starts at 2:45. Entries are now matched to the length
  of the file playing. A lookup that finished after moving to the next
  episode no longer becomes that episode's skip times.
- **A provider's own skip times were never used.** They are now, from the
  provider the episode is actually streaming from.
- **Later episodes in a running mpv were requested with the first episode's
  HTTP headers**, which could fail on a provider whose CDN wants others.
- **An episode could fail to count as watched.** The episode length from
  Jikan could replace the length mpv measured, or set it to zero.
- **The update notes in rofi were light text on a white box.** They now sit
  on the menu's own background, in your theme's colours.
- **Long update notes in the terminal were not cut.** The prompt said
  "truncated" but printed every note, which could push it off screen.

## 2.2.1 - 2026-09-29

### Fixed

- **The voice ran seconds behind the picture on a cast** after a resume or
  a seek. The stream restarts at the target, and on hosts that space their
  keyframes far apart (anizone: up to 10 seconds) the picture could only
  start at the next keyframe while the sound started at the target; the
  device plays both from their first sample. The stream is now read from
  a little before the target and both tracks are cut at it exactly. Casts
  without burned subtitles are re-encoded after a seek for the same
  reason, and now start where the seek asked instead of seconds earlier.
- **Burned ASS subtitles were late after a seek**, or missing until the
  first late line. Only WebVTT timings were moved with the restart point.
- **A dub cast played the Japanese track** on hosts that serve every dub in
  one stream (anizone, kickassanime). The cast now picks the audio in the
  requested language, and burns subtitles only when that audio is not
  English. Local playback in mpv prefers the dub's track too.
- **kickassanime casts could not fetch a segment**: its CDN needs the
  Origin header, which ffmpeg was never sent.
- **A device blocked by the firewall** now gets a hint in the cast panel
  naming the port after 20 seconds without a fetch, with the fix copied to
  the clipboard. A failed cast in a spawned window waits for q or enter
  instead of closing on its error.

## 2.2.0 - 2026-09-29

### Added

- **Casting from Untracked Watching.** It used to ignore casting and open
  the episode in mpv. It now casts with the same panel, skips and
  next-episode countdown as every other list, and still writes nothing:
  no history, no tracker progress, no resume position. Skip times are
  found by matching the show's exact title on AniList; a show whose title
  does not match exactly is cast without them rather than with a sequel's.

### Fixed

- **The cast panel froze at the end of an episode** ("23:41 / 23:41,
  PLAYING") for two minutes and then reported the cast as failed. Some
  receivers never say an episode has ended; the episode now finishes on
  the last update before the end, and the countdown starts right away.
- **Skipping an ending that runs almost to the end of the episode** failed
  and was retried every second, leaving the ending playing. The skip now
  lands a couple of seconds earlier when it has to, so whatever follows
  the ending still plays; if even that fails, the device is paused and the
  episode finishes.
- **Seeking past the watched threshold marked the episode as watched**
  while it was still playing. A cast is now judged by where playback was
  when it ended, as local playback is.
- **Esc in the cast device menu under rofi** had to be pressed twice: the
  first one fell back to casting in place, which showed the menu again.

## 2.1.3 - 2026-09-29

### Fixed

- **Audio ran up to a few seconds behind the picture after a seek**, on a
  cast with subtitles burned in -- including the automatic opening skip at
  the start of an episode. The video was re-encoded and cut at the seek
  target, but the copied audio could only start at the keyframe before it,
  and the device plays both from their first sample. The audio is now
  re-encoded too, so both tracks start at the same point.

## 2.1.2 - 2026-09-28

### Fixed

- **Casting could hang for 36-41 seconds and error out** when the device
  stopped answering status polls -- the vendored Chromecast library sets no
  read/write deadline on its connection. Status checks now give up after
  10 seconds.
- **Casting could error out instead of advancing** when a device went
  unresponsive right at the true end of an already fully-delivered episode
  (confirmed on hardware). That case now advances to the next episode like
  any other clean finish, while a genuine mid-episode drop still fails as
  before.
- **The cast device could be re-prompted for on every episode** when no
  device was set in the config and more than one was on the network. The
  picked device is now remembered for the rest of the run.
- **Audio could drift out of sync with video after seeking**, on a cast with
  subtitles burned in. The re-encode had no explicit frame-rate handling, so
  ffmpeg could duplicate or drop video frames to hold a constant rate while
  the copied audio track kept its original timestamps.
- **The cast panel could show a position past the total** (e.g. "24:02 /
  24:00") when real content ran a couple seconds past the probed duration.
  The displayed position is now clamped to the total.
- **A build made without the release `-ldflags` (any plain `go build`, not
  the `Build/` scripts or the AUR package) reported itself as v2.0.7**
  instead of a development build, which could also trigger a false "update
  available" prompt against every real release. Falls back to `dev` now.
- **The rofi update dialog's changelog could render as an unstyled white
  box** when a lot of release notes were concatenated (skipping several
  updates at once) and the truncation cut the generated markup mid-tag.
  Notes are now truncated before conversion, so the markup stays well-formed.

### Security

- Pinned `softprops/action-gh-release` to a commit hash instead of a mutable
  tag in the release workflow.
- The CI build job -- which can run an untrusted pull request's own code --
  now runs with no permissions at all instead of `contents: read`, and does
  not persist a checkout credential.

## 2.1.1 - 2026-09-28

### Fixed

- **The AniList OAuth callback reflected an error parameter into its response
  page unescaped.** The local callback server binds `:8000` on all
  interfaces, not just localhost, so a crafted `error` value reaching it
  during authentication could execute script in that page. The value is now
  HTML-escaped before being written out.

## 2.1.0 - 2026-09-27

### Added

- **`otakase -setup-anime-skip` gets you a personal Anime-Skip client id.** It
  opens Anime-Skip's account page in your browser and asks you to paste back
  the id you create there. That is the most this program can automate:
  Anime-Skip's own docs say accounts "must be created at
  anime-skip.com/sign-up" and cannot be created through their API, and
  creating a client id is a page in account settings with no API at all.
  Unlike AniList, there is no redirect this program can catch the end of. A
  personal id is rate-limited on its own, rather than sharing the playground
  id `auto` reads.

- **`otakase -cast` plays on a Chromecast.** A Chromecast cannot send the
  `Referer` these hosts require, so it can never fetch a provider URL itself.
  Otakase remuxes the stream locally with ffmpeg (a container change, not a
  re-encode, so it costs bandwidth and almost no CPU), serves it from this
  machine, and points the device at that. No Google developer registration: it
  uses the Default Media Receiver, the same one every other casting tool uses.

  Subtitles are burned into the picture when a provider has no WebVTT track
  (which is every provider here), using your GPU when it can. Openings and
  endings are skipped, filler and recaps are handled, and progress is tracked
  the same as local playback, because it runs through the same code once an
  episode starts.

  The receiver cannot seek a stream of unknown length on its own (it accepts
  the request and keeps playing where it was), so seeking and resuming both
  work by rebuilding the stream from that position and handing the device a
  new one, subtitles re-synced to match. This is also how a part-watched
  episode now resumes where you left it, rather than starting over.

  A firewall dropping the connection is the most common reason a cast never
  starts. Otakase detects the firewall, prints the exact command to fix it,
  and copies that command to your clipboard, since the window the message
  appears in is usually gone before there is time to copy it by hand.

  Casting continues to the next episode the same way local playback does,
  rather than stopping after one.

### Fixed

- **A rating could be silently lost.** On a config tracking only AniList or
  only MyAnimeList, declining the score prompt was reported as a save. The
  prompt now always runs, and a decline says so rather than nothing at all.
- **A show with no dub could play one anyway, with no subtitles.** anikoto
  answered a dub request with whatever stream it had rather than refusing one
  the show does not carry, so the automatic fallback to sub never had a
  reason to run. It now refuses correctly, and the fallback plays sub with
  subtitles instead.
- **Rofi could say goodbye far too often.** Otakase launched from rofi runs a
  fresh process for every action (opening the menu, handing off a cast,
  adding a show), and each one used to send a desktop notification saying
  "Have a great day!" on exit, however small the action. That notification is
  gone in rofi mode; a real terminal session still gets the one-line goodbye.

## 2.0.1 - 2026-09-19

### Changed

- **The debug log is now `otakase-debug.log`.** It was a bare `debug.log`, which
  is fine while it sits in the storage directory and unhelpful the moment it
  leaves, and leaving is what it is for: it is the first thing a bug report
  asks for, and a `debug.log` in someone's downloads folder says nothing about
  what produced it. A log left by an older version is removed on launch rather
  than kept as a stale file that looks current.

## 2.0.0 - 2026-09-18

### Removed

- **allanime, animepahe and senshi are gone.** All three had been disabled for
  months and none of them could play an episode:

  - senshi's domain lapsed and is parked for sale.
  - allanime requires a signed request its episode endpoint refuses without
    (`AA_CRYPTO_MISSING`). Reviving it means solving their signing scheme, not
    refreshing a hash, and the scheme rotates, so it would need re-solving.
  - animepahe sits behind a Cloudflare challenge. The bypass here launched a
    headless Chromium (a ~500MB download), and the cookies it came back with
    were rejected anyway.

  Keeping a provider that cannot play anything is not free. animepahe alone
  brought in `go-rod` and a browser automation stack, ~8MB of vendored
  dependencies now removed, and its quirks had spread through the codebase: a
  consent prompt for the Chromium download, a `no-animepahe` config token to
  decline it permanently, per-session id normalisation in the history file, a
  bespoke episode-count rule, and a stale-mapping reselect pass. All of that is
  gone with it.

  Two smaller leftovers went too: every provider that registered no Referer
  inherited allanime's, sending an unrelated (and now unresolvable) domain to
  hosts with nothing to do with it; and a CDN special case pointed at
  `allanime.to`, which no longer exists.

### Fixed

- **A provider name this build does not have no longer reads as enabled.**
  `ProviderEnabled` asked only whether a provider was switched off, so a name
  nothing registers came back as enabled. A config naming a removed provider
  therefore survived migration untouched, leaving the user pointed at nothing.
  Such a config is now replaced with the default stack, as a whole, rather than
  by quietly dropping the dead name and leaving a shorter stack nobody chose.

## 1.3.0 - 2026-09-16

### Added

- **You can fix a wrong skip from the player.** `Alt+o` at the start of the
  opening and again at its end marks it, `Alt+e` does the ending, and `Alt+s`
  offers what you marked to AniSkip: once to see exactly what will be sent,
  again to send it. `Alt+u` and `Alt+d` agree or disagree with the skip you were
  given, so an entry that throws you into the credits can be voted down by the
  person who noticed rather than surviving because nobody could say anything.

  Everything is drawn on the player's own screen, because that is what you are
  looking at; a message printed to the terminal behind mpv is a message nobody
  reads. Nothing leaves the machine on one keypress, and a marked span is
  checked before it is offered: the right episode, inside its length, and long
  enough to be an opening rather than a mis-press. These entries are public and
  every player reading AniSkip inherits them, so a wrong one is worse for
  everyone than a missing one.

  AniSkip has no accounts. Submissions are filed under an id this program
  invents and keeps in your storage directory, which says nothing about you or
  the machine. `ContributeSkipTimes=false` leaves the keys unbound.

- **theintrodb is a fourth source of skip timings.** It knows a great deal,
  including anime, but files everything under TMDB ids, a third naming of the
  same shows, which nothing here had. otakase now builds a table mapping AniList
  ids onto TMDB ones from a public list, keeps it for a month, and consults
  theintrodb only for episodes the earlier sources could not answer.

  The table is 7MB to build, so it is built in the background and a lookup that
  arrives first says it does not know rather than waiting: starting an episode
  is never held up for it. A show whose mapping has no TMDB *season* is left
  alone: AniList files each cour separately while TMDB groups them, and a
  mapping without the season sends episode 1 of a second cour to episode 1 of
  the first. `IntroDBSkipTimes=false` leaves it out.

### Fixed

- **The first episode of a session now asks every skip source.** It asked
  AniSkip alone while every later episode in the playlist asked the whole chain,
  so a show AniSkip does not cover started unskipped and then began skipping an
  episode later. The provider's own timings and Anime-Skip were reachable only
  after the first episode ended.

- **An episode with only an ending on file no longer skips the opening to it.**
  AniSkip's results say which each one is, and this took the first as the
  opening and the last as the ending regardless, so an episode that had only an
  ending recorded jumped to the credits about a minute in.

## 1.2.0 - 2026-09-16

### Added

- **`AnimeSkipClientID=auto` sets Anime-Skip up for you.** Anime-Skip needs a
  client id identifying the application, which until now meant finding one and
  writing it into the config, and finding it again whenever it changed. With
  `auto`, otakase reads the id Anime-Skip publishes for its own GraphQL
  playground, keeps it in the storage directory so a restart costs nothing, and
  goes looking for a new one the moment the old one stops being accepted. A
  rotation now fixes itself before the next opening.

  It is a setting rather than the default because that id is Anime-Skip's, and
  shared with everyone who opens their playground. Pointing every install at it
  uninvited is their bandwidth and their rate limit; asking for it is your
  choice to make. An id of your own still goes in the same setting, and empty
  still means Anime-Skip is not asked at all.

  Only the API saying the id itself was refused starts the search for another.
  "The header must be passed" means nothing was sent, which no amount of
  fetching fixes, and a fetch that does not help is not repeated for ten
  minutes: a page that is down must not cost a request per episode.

## 1.1.0 - 2026-09-16

### Added

- **anikoto, and it leads the provider stack.** Its identifiers are AniList
  media ids, the same ids the tracker already stores for every entry. Every
  other provider has to work out which of its shows is the one on your list,
  and getting that wrong is the failure this project has spent most of its
  history repairing. Searching "rich girl caretaker", the title that broke the
  older providers, returns one exact result whose id is the one already in the
  watch history.

  It answers with a plain HLS manifest, a subtitle track, the HTTP headers its
  CDN requires, and per-episode intro and outro ranges, in two requests. Sub
  and dub are indexed separately, so a dub request gets a dub or a straight
  answer that there is none.

  Verified against the real host end to end, including fetching the manifest
  with the stated headers. Resolving a URL is not the same as it playing, and
  this project has been caught by that difference before.

- **Skip timings now come from every source that knows them.** The opening is
  taken from the first source that has one and the ending from the first that
  has one, and they need not be the same source: AniSkip frequently knows an
  opening and not an ending, and a provider that ships timings with the stream
  often knows both, so a half-answer is completed rather than discarded. Asking
  stops as soon as both halves are known.

  The chain is the active provider first, because a provider that ships timings
  with the stream has already paid for that request; then AniSkip, which is
  keyless; then Anime-Skip, which needs two requests and a client id.

  This also fixes a quiet gap: skipping was keyed on a MyAnimeList id, so a show
  tracked on AniList alone never skipped anything at all.

  Anime-Skip requires an `X-Client-ID` identifying the application. None is
  bundled: the ones in circulation belong to other projects, and borrowing one
  makes this program's traffic look like theirs. Set `AnimeSkipClientID` to use
  it; the other sources work without it.

### Changed

- **The terminal menu is rebuilt.** Your list statuses are a tab bar across the
  top (watching, rewatching, completed and the rest) with a count beside each
  one, and ← and → move between them without going back to a menu. Switching is
  instant: the whole list is already in memory, so a category is a filter
  rather than a fetch. Beside the list, a pane describes the highlighted entry,
  which is where a title too long for its row can still be read.

  The frame around it is measured from the terminal, so the rule spans the
  window, the pane keeps a fixed share of it, and the key hints rest on the
  bottom edge; resizing relays the lot. Rows are cut to their column rather
  than wrapped, because a row that becomes two lines breaks the count of what
  fits on screen. A terminal smaller than 36 × 10 is told to resize instead of
  being shown something unreadable.

  Along the bottom, the actions that used to be menu entries are keys you can
  press from anywhere in the list: `^u` untracked, `^e` update, `^r` remap,
  `^l` continue, `^t` tracker, `^o` provider. They are ctrl combinations
  because a plain letter types into the filter, and binding `u` to update would
  make any title containing a u unsearchable.

  The keys offered are the keys that work: no category key where there are no
  categories, quit on the home menu and back inside one, and `j/k` and `tab`
  under vim keys, where the arrows move the cursor instead. When the bar is
  wider than the window the least useful hints drop off rather than wrapping.

  `MenuOrder` is unchanged and still decides all of it: its list entries
  become the tabs, its action entries become the bottom bar, each in the order
  you wrote them. The default now offers every list: watching, all, planning,
  on hold, dropped and rewatching.

  The shape is borrowed from [kari](https://github.com/Dhairya3391/kari); the
  colours are still yours, from the palette.

- **The questions the program asks can be backed out of, and look like the
  rest of it.** Every way out of the untracked search used to close otakase:
  the prompt reported an empty answer as an error, and each caller turned that
  into quitting, so pressing enter at a question you had opened by accident
  shut everything. It is reached by one keypress from a list, and the cost of
  pressing that by accident should be one more keypress. A search that finds
  nothing, or results you do not want, now asks again instead of leaving.

  Escape backs out of it, and so does enter on an empty line. Escape needs the
  prompt to read keys rather than lines: a terminal in its normal mode hands
  over nothing until enter, so an escape typed at a line-based prompt arrives
  buried in the answer, if at all.

  The prompt is drawn in the same frame as the menus, with the way out written
  on it, rather than appearing as a bare line of text.

  Every other question the program asks now works the same way, and the
  line-based prompt they shared is gone:

  - **Adding an anime to your list**, and **searching the providers under a
    different name**, return to the list they were opened from. So does a
    failed search: a dropped connection while adding an anime used to close
    the program. The provider question starts from the name tried so far, so
    correcting one word of it costs one word, and leaving it unchanged offers
    the other ways out rather than repeating a search that has already failed.

  - **The two that ask for an episode number.** Backing out of "change the
    episode number" returns to the menu it was chosen from. Backing out of the
    question asked when the total number of episodes cannot be worked out
    starts from your tracker progress, which is what happens anyway when the
    total is known, and which used to close the program instead. A number that
    is not a number is asked for again rather than costing you the menu.

  - **Setting your progress, and rating an anime.** Both read a bare line and
    closed the program when it could not be read as a number, and an empty
    line cannot, so pressing enter at either was enough. Backing out of the
    progress question leaves your progress alone and returns to the list;
    backing out of the rating simply does not rate it, which is the ordinary
    answer to a question that arrives unasked when an episode ends. A score
    outside nought to ten, or a typo in either, is asked about again.

  - **"Start this anime from the beginning?"** now offers escape, which means
    the same as no. Under rofi, a failed read of the answer used to close the
    program rather than take the default.

  - **Connecting a tracker.** The MyAnimeList client id, secret and pasted
    callback URL, and a manually pasted AniList token, are asked for in the
    same frame as everything else. Each was read by a reader built for that one
    read, which threw away anything typed past it (so pasting the id and the
    secret together lost the secret), and the length cap on the field would
    have quietly eaten the end of a pasted callback URL. The questions where an
    empty answer already meant something ("optional", "start over") take escape
    as meaning it.

- **A closed stdin no longer spins the external-player loop.** On the Android
  intent path the program waits for you to press enter after each episode, and
  it could not tell enter from stdin having ended, so with nothing attached it
  read an instant answer every time round the loop and opened a player per
  turn. It now stops.

- **`CurrentCategory` can skip the menu entirely**, opening straight into your
  watching list. It existed only as a command-line flag and was never written
  into a config, so nothing suggested it was possible. With the tabs reaching
  every other list and the bottom bar every action, the menu has little left to
  do; with it on, escape quits rather than promising a screen behind the list
  that is not there.

  It is a terminal setting. What makes skipping the menu reasonable is the tabs
  and the bottom bar, and rofi has neither: there the menu is still the only
  way to the other lists and to the actions, so it keeps appearing. `-current`
  asks for one run specifically and is honoured whichever is drawing the list.

- **Rewatching is offered as a category.** It was always a list the program
  could produce, and was simply missing from the default menu order.

- **Individual colours can be replaced with your own.** `ThemeOverrides` takes
  `name:#hex` pairs and layers them onto whichever palette is in use, so
  following the desktop theme and disliking one colour in it are no longer
  exclusive. A mistyped colour costs that colour alone: it is reported and
  skipped while the rest apply.

- **The Bubble Tea stack moved up**: bubbletea 1.3.10, lipgloss 1.1.0,
  termenv 0.16.0.

### Removed

- **ueberzugpp is no longer a dependency.** The function that used it had no
  callers, and every image preview in the program is drawn by rofi itself
  through its own icon protocol. It had never done anything here.

- **The Nix packaging is gone.** It no longer built. Arch packaging and the
  release binaries are unaffected.

## 1.0.0 - 2026-09-15

First release as Otakase.

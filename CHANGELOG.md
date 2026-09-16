# Changelog

## Unreleased

### Added

- **anikoto, and it leads the provider stack.** Its identifiers are AniList
  media ids — the same ids the tracker already stores for every entry. Every
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
  with the stated headers — resolving a URL is not the same as it playing, and
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
  bundled — the ones in circulation belong to other projects, and borrowing one
  makes this program's traffic look like theirs. Set `AnimeSkipClientID` to use
  it; the other sources work without it.

### Changed

- **The terminal menu is rebuilt.** Your list statuses are a tab bar across the
  top — watching, rewatching, completed and the rest — with a count beside each
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

  `MenuOrder` is unchanged and still decides all of it — its list entries
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
  prompt to read keys rather than lines — a terminal in its normal mode hands
  over nothing until enter, so an escape typed at a line-based prompt arrives
  buried in the answer, if at all.

  The prompt is drawn in the same frame as the menus, with the way out written
  on it, rather than appearing as a bare line of text.

  Every other question the program asks now works the same way, and the
  line-based prompt they shared is gone:

  - **Adding an anime to your list**, and **searching the providers under a
    different name**, return to the list they were opened from. So does a
    failed search — a dropped connection while adding an anime used to close
    the program. The provider question starts from the name tried so far, so
    correcting one word of it costs one word, and leaving it unchanged offers
    the other ways out rather than repeating a search that has already failed.

  - **The two that ask for an episode number.** Backing out of "change the
    episode number" returns to the menu it was chosen from. Backing out of the
    question asked when the total number of episodes cannot be worked out
    starts from your tracker progress — which is what happens anyway when the
    total is known, and which used to close the program instead. A number that
    is not a number is asked for again rather than costing you the menu.

  - **Setting your progress, and rating an anime.** Both read a bare line and
    closed the program when it could not be read as a number — and an empty
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
    read, which threw away anything typed past it — so pasting the id and the
    secret together lost the secret — and the length cap on the field would
    have quietly eaten the end of a pasted callback URL. The questions where an
    empty answer already meant something ("optional", "start over") take escape
    as meaning it.

- **A closed stdin no longer spins the external-player loop.** On the Android
  intent path the program waits for you to press enter after each episode, and
  it could not tell enter from stdin having ended — so with nothing attached it
  read an instant answer every time round the loop and opened a player per
  turn. It now stops.

- **`CurrentCategory` can skip the menu entirely**, opening straight into your
  watching list. It existed only as a command-line flag and was never written
  into a config, so nothing suggested it was possible. With the tabs reaching
  every other list and the bottom bar every action, the menu has little left to
  do; with it on, escape quits rather than promising a screen behind the list
  that is not there.

  It is a terminal setting. What makes skipping the menu reasonable is the tabs
  and the bottom bar, and rofi has neither — there the menu is still the only
  way to the other lists and to the actions, so it keeps appearing. `-current`
  asks for one run specifically and is honoured whichever is drawing the list.

- **Rewatching is offered as a category.** It was always a list the program
  could produce, and was simply missing from the default menu order.

- **Individual colours can be replaced with your own.** `ThemeOverrides` takes
  `name:#hex` pairs and layers them onto whichever palette is in use, so
  following the desktop theme and disliking one colour in it are no longer
  exclusive. A mistyped colour costs that colour alone: it is reported and
  skipped while the rest apply.

- **The Bubble Tea stack moved up** — bubbletea 1.3.10, lipgloss 1.1.0,
  termenv 0.16.0.

- **The leftovers still calling themselves curd are gone from everything you
  can see.** "Please restart curd" after an update, the comment at the top of
  the generated rofi themes, the image cache at `~/.cache/curd/images`, the
  name of the built-in palette, the debug-log path in the bug report template,
  and the module paths and config names throughout the provider documentation.
  Temporary files — the mpv socket, the update downloads, the playlist and
  torrent scratch directories — are named after this program too.

  A rofi theme written before this still counts as one otakase wrote, so
  upgrading does not decide every theme was hand-edited and back up the lot.

  What keeps the old name does so deliberately: the files inside your storage
  directory (`curd_history.txt`, `curd_version`, `curd_id`), because renaming
  them orphans the history migrated from curd; `CURD_MAL_CLIENT_ID` and
  `CURD_MAL_CLIENT_SECRET`, which still work; the packaging's `replaces=curd`;
  and the references to Wraient's own AUR package, which is not ours to touch.
  Internally the Go code still says `CurdConfig` and `curdhost` — invisible
  from outside, and a rename of that size is not worth folding into a release.

### Removed

- **ueberzugpp is no longer a dependency.** The function that used it had no
  callers, and every image preview in the program is drawn by rofi itself
  through its own icon protocol. It had never done anything here.

- **The Nix packaging is gone.** `flake.nix`, `package.nix` and `flake.lock`
  came from upstream and the rename never reached them, so they no longer
  built at all: the derivation wraps `$out/bin/curd`, and the binary is
  `otakase`. The name, homepage, exposed attribute and dependency list were
  stale alongside it. Arch packaging and the release binaries are unaffected.

## 1.0.0 — 2026-09-15

Curd is now **Otakase**, and the version resets to 1.0.0. Everything below this
section is the curd-era history, kept because it is the record of what changed
and why.

### Changed

- **Renamed.** The binary, the Arch package, the config directory and the
  release assets are all `otakase`. The Go module is
  `github.com/thexykril/otakase`.

- **Version reset to 1.0.0.** The old numbering belonged to a different
  project's line. Note that this reads as a downgrade to anything comparing
  versions, so an existing curd install will not be offered this as an update —
  moving over is a deliberate install, not an automatic one.

- **An existing curd install is carried over on first launch.** The config,
  the AniList/MyAnimeList tokens and the local watch history are copied from
  `~/.config/curd` and `~/.local/share/curd` into the otakase locations, and a
  `StoragePath` still set to curd's default is repointed. A path you chose
  yourself is left alone. The originals are **copied, not moved**, so a curd
  binary that is still installed keeps working; they can be deleted once
  otakase looks right.

  Without this, the first launch after the rename would look like a factory
  reset — signed out of both trackers, no history — with the data still on disk
  under the old name.

- **The Arch package `replaces`/`conflicts` with `curd`**, so installing it
  removes the old package rather than sitting beside it. The `curd` command is
  not carried over: the rename is a rename, and leaving the old name working
  indefinitely only blurs which program is which.

- **The provider canary no longer reports upstream commits.** Watching a parent
  repository for commits to pick up made sense for a fork; this is a
  continuation, so those weekly reports were noise. The canary keeps doing the
  job it was written for: probing every source on a schedule and opening an
  issue when one stops working, so rot is found on a Monday rather than by
  whoever next tries to watch something.

### Added

- **A Hyprland keybinding, on request.** `otakase -install-keybind` binds
  Super+Shift+A to the rofi menu with poster previews — the launcher-style way
  to use this: no terminal, pick a show, watch it. It writes Lua on Omarchy and
  `.conf` syntax on a stock Hyprland, since putting one dialect in the other's
  file is a parse error rather than a keybinding. It backs the file up, is safe
  to run twice, and refuses a key something else already owns unless forced —
  and when forced it comments the old line out rather than leaving two bindings
  on one combination. `-remove-keybind` reverses it.

  Installing with `sudo` sets it up for you. The obstacle was never permission
  but identity: an install script runs as root, so `$HOME` is `/root`, and a
  keybinding written there helps nobody. The script resolves the invoking user
  through `SUDO_USER` and runs the command as them, so the file lands in the
  right config owned by the right account, and skips itself when there is no
  such user — a chroot, an image build, pacman as root. `OTAKASE_NO_KEYBIND=1`
  opts out.

  Uninstalling removes the binding first, while the binary it points at still
  exists, so `pacman -R` does not leave a key bound to something that is gone.
  An upgrade refreshes a binding already present and adds nothing otherwise.

- **`otk`, a short alias.** Same program, same flags — `otakase` is a mouthful
  for something typed several times a day. Installed as a symlink beside
  `otakase` on Linux and macOS, and as a forwarding shim on Windows, where a
  symlink is not worth the trouble.

  The updater resolves symlinks before replacing the binary, so `otk -u`
  updates the real executable rather than overwriting the link with it.

### Fixed

- **`curd -u` updated from the upstream repository instead of this fork.**
  Every other update path — the background check, the next-launch prompt — already
  read `TheXykril/curd`, but the flag passed `wraient/curd` explicitly, so asking
  curd to update itself replaced a forked build with an upstream one that has
  none of its providers or fixes. The repository now lives in a single exported
  constant that all three paths share, and a test fails if any of them names a
  repository of its own again.

- **Generated release notes listed the whole project history under stale names.**
  The notes step called `git tag --sort=… "v*"`, which tries to *create* a tag
  rather than list one; it failed, no previous tag was found, and the commit
  range fell back to the root commit — 563 commits, every upstream contributor's
  work reprinted on each release. Separately, the author lookup authenticated
  with a secret this repository does not define, so every request was rejected
  and the notes fell back to the commit's git author name, reviving an account
  name that was renamed long ago. Both are fixed, and a failed lookup now warns
  instead of quietly publishing the wrong name.

## 2.4.0 — 2026-09-14

### Added

- **Launching no longer waits for both trackers to be reconciled.** With
  `TrackingRemote=anilist+myanimelist`, every launch pushed the differences
  between the two services before the first menu could appear — one paced write
  per entry. That difference is not small and does not shrink on its own: 78
  entries present on AniList but not on MyAnimeList were re-sent every time,
  around half a minute of waiting to push a planning list nobody was about to
  watch.

  Measured rather than guessed, after the obvious suspects turned out innocent:

  ```
  Reading your AniList list          5ms
  Reading your MyAnimeList list      5ms
  Reconciling both trackers      53.11s      <- all of it
  ```

  The merge itself is local and instant, and nothing on screen depends on the
  writes having finished — the merged list is already what curd shows. So the
  writes now run behind the menu instead of in front of it. Ordering stays safe:
  these are catch-up writes for entries that differed at launch, and anything
  done during the session is written later by definition.

  Startup also reports where its time went, so a slow launch names the step that
  spent it instead of being a minute of silence.

- **Being caught up now says so, instead of failing.** After the last aired
  episode, the prompt offered the next one — which does not exist yet, so
  choosing it searched every provider and returned a failure that read as though
  something were broken:

  ```
  before:  Yes, start episode 11   →   failed to get episode links
  after:   Caught up
           Episode 11 airs in 4d
  ```

  The airing schedule is the same tracker data that already drives the list
  countdowns, so this costs no extra request — it is read from memory at the end
  of an episode, where a prompt that paused on a lookup would be worse than one
  that occasionally cannot say.

  Both routes into that situation answer the same way — finishing an episode, or
  coming back a day later and picking the show from the list, which used to
  search every provider before failing.

  There is still a way through: the schedule comes from a cached list, so an
  episode that aired an hour ago can read as upcoming, and a flat refusal would
  have curd overruling the user on something it only half knows. Choosing to look
  anyway lands exactly where this used to land by itself.

  When the schedule is unknown the episode is offered as before: refusing one the
  user could have watched is the worse mistake, and a failed lookup is exactly
  what happened previously anyway. A countdown is shown only for the immediately
  next episode, since a later one airs later still and reusing the wait would
  understate it.

- **Curd says when it is starting, if starting takes a while.** Launched from a
  keybind rather than a terminal, it gave no sign of life until its first menu
  appeared — and refreshing a tracker token and pulling a large list can take
  several seconds. The honest reading of a silent desktop is "did that even
  start?", so people press the key again and end up with two copies racing.

  A notification now fills that gap, with the seconds ticking so a slow launch
  reads as working rather than hung:

  ```
  Signing in to your tracker (3s)
  Loading your anime list (6s)
  ```

  It only appears when there is a gap worth filling: a launch that reaches the
  menu within about a second stays silent, since a message nobody has time to
  read is worse than none. The menu opening ends it, because a visible menu is
  its own proof, and quitting mid-launch ends it too rather than leaving a
  notification describing work that stopped. In a terminal nothing is shown, as
  the output is already visible there.

- **Providers can require HTTP headers beyond a referrer.** MPV's `--referrer`
  cannot set `Origin`, and one CDN needs exactly that, so a stream hint may now
  carry arbitrary headers. They are appended to whatever `MpvArgs` already
  configures rather than replacing it.

- **KickassAnime (`kickassanime`, kaa.lt) — now the first provider tried.** It
  is the most conventional source Curd talks to and the best behaved: a plain
  JSON API with no anti-bot gate, no persisted-query handshake, and stream URLs
  that sit in the player page rather than behind an obfuscated endpoint. Two
  requests get an HLS manifest and an English subtitle track.

  It also carries whole seasons rather than only what is recent, and indexes
  dubs separately — so a dub request either gets a dub or a straight answer that
  the show has none, which is what `AutoAudioFallback` needs to act on:

  ```
  no dub release for this show (available: ja-JP)
  ```

  The player is an Astro island whose props carry the manifest and every
  subtitle track already. Two URL shapes it emits need repairing before use: the
  manifest is protocol-relative, and subtitle links carry an empty authority
  (`https:///host/...`), which no HTTP client resolves.

- **A torrent-backed provider, `nyaa`.** Every other provider Curd ships scrapes
  a streaming host, and those share a failure mode: they carry a show's back
  catalogue but lag or omit the episodes that aired this week — exactly the ones
  a continue-watching list asks for. On one currently-airing show, anipub
  stopped at episode 5 and anineko's streams stopped resolving after 6, so
  episodes 7-10 were unreachable while the index carried all of them.

  Releases are indexed within hours of broadcast, the RSS feed is a documented
  interface rather than markup that changes shape, and there is no anti-bot gate
  in front of it. Playback does not wait for a download: pieces are fetched in
  the order the player asks for them and served over a local HTTP endpoint that
  honours range requests, so MPV starts within seconds and can still seek.
  Nothing is kept — the cache is temporary and removed on exit.

  Releases are ranked by resolution and then by swarm health, since a "trusted"
  release with two peers starts slower than an untrusted one with four hundred.
  Batch torrents are skipped: they cannot answer "play episode 10" without
  fetching a whole season. A dub request with no dubbed release fails cleanly
  rather than quietly returning subtitles, which lets `AutoAudioFallback` do its
  job.

  It sits third in the default stack — behind the streaming hosts, because
  finding peers costs a few seconds, and ahead of the ones that no longer work.

- **`AutoAudioFallback` (default on): a show carried in only one language just
  plays.** Reaching the other audio took two menus — a recovery menu, then a
  confirmation — for a question with a single useful answer. Curd now switches
  on its own and says so:

  ```
  No dub for episode 7 — playing sub.
  ```

  The switch is announced, never silent, and the preferred language is still
  tried first every time. Set `AutoAudioFallback=false` to be asked as before.

  The setting is honoured in one place, so every route into the other audio —
  the preferred-first resolve, the recovery menu, the playlist controller —
  behaves the same way.

### Fixed

- **Dual tracking ran the whole session on cached data.** Both trackers answer
  from cache and refresh behind the launch, so the merged list dual tracking
  shows was built from two caches. The refreshes did arrive — into the two
  sub-lists, which nothing reads once the merged list replaces them. So an
  episode that aired an hour ago still read as unreleased, and progress made on
  another device did not appear until the next launch at the earliest.

  The merge now happens again when both refreshes land, and the result is
  published to any menu already on screen. The launch still shows the cached
  merge immediately, so nothing got slower; a tracker that never answers is
  given up on rather than leaving the list marked as still refreshing, which
  playback waits on.

- **Dual tracking silently lost every show's broadcast schedule.** Only AniList
  reports when the next episode airs; MyAnimeList has no equivalent. Merging the
  two lists backfilled titles, episode counts, status and duration from whichever
  side had them — but not the schedule. So the moment curd pushed progress to
  MyAnimeList, making that entry the fresher of the two, it won the merge and
  the airing dates were dropped.

  Everything downstream then behaved as though the dates were simply unknown:
  no `next in 22h` countdown in the list, and no way to tell "you are caught up"
  apart from "that episode could not be found" — so a `anilist+myanimelist` user
  still got a failed provider search at the end of a season, where a single
  tracker user was told when the next episode airs.

  The schedule is now carried across the merge like every other field only one
  side knows, as is `format`, which had the same gap.

- **Menus never showed the sentence explaining them.** The redesigned rofi theme
  left `message` out of its `mainbox` children, and rofi draws `-mesg` only if
  the theme asks for it. Nothing looked broken — the menu appeared, the choices
  worked — but the line giving them meaning was silently dropped, so "why
  playback failed" and "when the next episode airs" arrived as a bare **Done**
  with nothing above it.

- **Finishing an episode could lose it.** On a show with no dub, watched with
  `SubOrDub=dub`, the episode played correctly as sub — and then, as MPV reached
  the end, curd tried to load episode 1 and the tracker was never updated.

  Three faults compounded. The MPV playlist controller built its playlist for the
  *configured* audio rather than the audio actually playing, so on a sub-only
  show every entry asked providers for a dub that does not exist. When playback
  ended, MPV landed on the first placeholder row and the controller read that as
  the user choosing episode 1, rather than as end-of-file. And the switch it then
  attempted had already zeroed the playback state it needed in order to put
  things back, so after it failed the finished episode looked unwatched:

  ```
  MPV playlist: resolving stream for episode 1 (dub) [was 10]
  MPV playlist: failed to play ep 1: no dub episode links found ...
  Episode is not completed, exiting
  ```

  The completion check divides by a duration that was now zero, decided the
  episode had been abandoned, and left AniList showing 9/12 after episode 10 had
  been watched in full.

  Playback now records which audio it actually resolved, and the playlist follows
  that. Every failing path of a switch restores what it cleared — position,
  duration, links, headers and completion — so a switch that cannot happen leaves
  the episode exactly as it found it.

  Running off the end is now ended rather than merely declined. The episode's
  stream is one row in a playlist of placeholders, so when it finishes MPV moves
  to the next row: a black clip that runs for a day. Declining to follow it left
  MPV sitting on that blackness with "Loading episode 1…" on screen, and since
  playback never ended the episode was still never marked watched. Curd now
  closes MPV instead, which is what happens without a playlist — and the episode
  gets recorded. A row the user genuinely picked is told apart by MPV having
  media loaded; running off the end leaves it idle with no path.

- **Watch history filed provider ids under the wrong provider.** Six history
  writes paired `anime.ProviderId` -- the id of whichever host actually served
  the episode -- with `GetProvider().Name()`, which is the *first configured*
  provider, not that one. Playing something anipub could not serve but anineko
  could stored anineko's slug under the name "anipub", and every later run then
  handed that slug straight back to anipub:

  ```
  anipub episode: invalid anipub show id "rich-girl-caretaker-im-secretly-..."
  ```

  which removed anipub from consideration for that show permanently. The name
  written now always belongs to the id beside it.

  Those corrupted pairings already exist in users' history files, so a stored id
  that fails is no longer believed: it is re-derived by searching once, and the
  working id replaces it. A stored id that works is still used directly, with no
  extra search.

- **A show could be reported as uncarried on the one provider that had it.**
  Mapping an anime onto a provider during playback searched the full AniList
  title once and gave up. Hosts shorten long titles: anineko carries *Rich Girl
  Caretaker: I'm Secretly the Caregiver of the Most Popular Girl in This Rich
  Kid School* as plain *Rich Girl Caretaker*, and answers nothing for the full
  name. So episode 10 was declared missing while anineko had it, and the only
  host consulted was anipub, whose catalogue for that show stops at episode 5.

  The same ordered title variants the initial search already used are now tried
  here too — exact titles first, simplified forms last, so a provider that
  answers the real title is never sent the broader guesses.

- **A failed episode lookup left curd spinning instead of stopping.** When no
  stream could be resolved, `StartCurd` returned an empty socket path — having
  already reported why — but the caller ran on and started the playback
  watchers for a session that did not exist. Those then polled a socket nothing
  would ever answer, once a second, indefinitely:

  ```
  Error getting playback time: no MPV IPC socket   (x60, and counting)
  ```

  The empty socket path is now treated as the failure it is. The polling loop
  also gained its own guard: its "MPV is gone" bail-out sat behind a
  CLI-only condition, so with `RofiSelection=true` it was never reached.

- **Choosing an anime from the poster grid failed with "error selecting
  anime".** The grid clips a label to the column width, so a long title came
  back from rofi shortened:

  ```
  written:  That Time I Got Reincarnated as a Slime Season 4 · 0/24 (21 aired)
  returned: That Time I Got Reincarn… · 0/24 (21 aired)
  ```

  The returned text was then matched against the full label to find the chosen
  show, which could not succeed — so the menu died on exactly the entries with
  the longest names. Rows are now addressed by index (`rofi -format i`) instead
  of by their text, which cannot drift from what was displayed.

  The row table is built as the menu is written rather than reused from the
  option list, because a row whose cover fails to download is skipped; indexing
  into the unfiltered list would have resolved every row after such a gap to its
  neighbour. Dismissing the picker now means "back" rather than an error.

- **Dual tracking (`TrackingRemote=anilist+myanimelist`) failed to launch.**
  Three defects compounded:

  The AniList write loop slept 350ms between updates; the MyAnimeList loop had
  no delay at all. A first dual sync is large — 180 entries on a real library —
  and firing those back to back gets the account rate limited. Both loops are
  now paced.

  MyAnimeList answers a rate-limited write with a redirect to `/error.json`,
  which never responds. Go followed it and re-issued the `PUT`, so throttling
  became a minute-long hang rather than an error. API requests no longer follow
  redirects: a REST API returning one is reporting a failure, not a new address.

  A single failed entry aborted the entire sync, and with it the launch. One
  anime that cannot be written is now logged and skipped, and the count of
  skipped updates is reported.

  The error was also undiagnosable: it carried no status code and, for a
  redirect, an empty body. It now names the status and the redirect target.

### Added

- **The list leads with what you were last watching.** The text menu applied no
  sort at all and took whatever AniList returned, while the poster grid sorted
  alphabetically — so neither surfaced the show you were part-way through, and
  the two menus disagreed with each other. Both now order by when the entry last
  changed, most recent first.
- **How long until the next episode.** AniList sends `timeUntilAiring` with every
  list fetch and curd stored it without ever reading it; only the episode number
  was used, for the "new episode" flag. Releasing shows now say when the next one
  lands, at no extra cost:

  ```
  Rich Girl Caretaker … · 9/12 (9 aired) · next in 22h
  That Time I Got Rein… · 0/24 (21 aired) · next in 6d
  ```

  The countdown appears in the text menu only. A poster label is clipped at the
  column width, and "next in 22h" costs about fourteen of its thirty-seven
  characters — more than a third of the title — which is a poor trade when the
  cover already identifies the show. Resume points still show in both: they are
  shorter, and they are something to act on now.
- **Resume points.** Where the local history shows you stopped part-way through
  an episode, the row says so: `· resume 11:40`. Shown only when you are
  meaningfully in — past a minute and not yet 95% through — so it stays signal
  rather than appearing on every row. A resume point takes precedence over the
  airing countdown: one is something to act on now, the other is something to
  come back for, and showing both would crowd the row.

## 2.3.0 — 2026-09-05

### Added

- **The menus follow Omarchy's own design language.** Curd's rofi themes are now
  built from the tokens in the active theme's `shell.toml` — the same contract
  Omarchy's bar, dropdowns and menus use — so curd looks like part of the desktop
  rather than a separate application:

  - an opaque card on a light (0.5) scrim, hairline border on every side, small
    radius, matching `[menu]`;
  - a selected row marked by an 8% foreground fill with its own 25% outline and
    accent-coloured text, rather than a heavy accent band;
  - no prompt chip: a bare field with placeholder text and a hairline beneath;
  - the system monospace family, via `fc-match`, which follows `omarchy font set`
    and still works with no Omarchy installed.

  The poster grid is now a card too, not a fullscreen surface. Omarchy's scrim is
  deliberately light because its menus are opaque cards on top of it; rendering
  covers straight onto that scrim let the wallpaper compete with the artwork.

- **Poster labels keep their episode counts.** The grid clips a label at the
  column width, and a long anime title consumed the whole line, so the counts —
  the one thing a cover cannot tell you — were what disappeared. The title now
  absorbs the truncation and the counts stay pinned at the end:

  ```
  That Time I Got Re…            · 0/24 (21 aired)
  From Old Country Bumpkin to M… · 4/12
  Uzaki-chan Wants to Hang Out!  · 3/12
  ```

  The budget is measured against the layout by rendering a character ruler, not
  derived from column arithmetic — a first estimate that way was off by a third.
  A test pins the layout to the measured value so the two cannot drift apart.

### Fixed

- **Poster grid rows were not pango-escaped.** Only rows flagged as having new
  episodes went through the markup builder, so an ordinary title containing `&`
  broke its own row, and the episode counts were never dimmed.

### Added

- **Episode counts in the anime list.** Rows showed only a title, so knowing how
  far through a show you were, or whether anything new had aired, meant opening
  it. Every number was already on the AniList entry.

  The denominator is always the season total, so a fraction means the same thing
  on every row, and the aired count appears only while a show is still releasing
  and differs from the total:

  ```
  That Time I Got Reincarnated as a Slime Season 4 · 0/24 (21 aired)
  Rich Girl Caretaker: I'm Secretly the Caregiver … · 9/12 (9 aired)
  Uzaki-chan Wants to Hang Out!                     · 3/12
  ```

  Shows AniList has no episode count for fall back to `6 watched`, or
  `1100/1177 aired` when the aired figure is known. Counts render dimmed after
  the title, so they never compete with it.

### Fixed

- **A rate-limited provider was reported as though the show did not exist.**
  anipub answers HTTP 200 with `{"error":"Too many requests, ..."}` when it
  throttles, which fell through to a generic parse failure. Nothing recognised
  it as temporary, so it was not retried, did not trip the provider cooldown,
  and told the user their anime was not carried. Throttling is now detected,
  reported as the host being unavailable, and retried.
- **The config file is written atomically.** It holds tracker credentials and is
  rewritten on most runs, but was truncated in place with no lock, so a crash or
  two concurrent runs could mangle it. A real config in the wild ended up
  containing `nimeListClientID` — `MyAnimeListClientID` with its first three
  characters gone. Writes now go to a temporary file and are renamed into place.
- **Unrecognised config options are reported instead of silently ignored.** A
  typo such as `SkipOP=true` did nothing at all, with no indication why. Unknown
  keys are now named on startup with the closest valid option suggested, and are
  never deleted, since they may belong to another curd the user runs.

### Added

- **A scheduled provider canary** (`.github/workflows/provider-canary.yml`). The
  repo already had live tests covering real providers, but they are gated behind
  environment variables and CI ran `go test -short`, which skips every one — so
  provider rot was only ever discovered by users. The canary runs them weekly,
  probes every provider, and opens or updates an issue when one breaks. It
  retries once after a pause, because a canary that cries wolf over a brief
  throttle gets ignored.

  It also reports when upstream (`Wraient/curd`) has commits this fork does not,
  so upstream fixes surface on their own rather than depending on someone
  remembering to look. That is filed separately from provider health and never
  fails the run: commits existing upstream is information, not a fault.

## 2.2.0 — 2026-09-04

### Added

- **Episode downloading** (`curd -download`), the most-requested missing feature
  upstream (Wraient/curd#104, #55). It reuses the whole existing selection flow
  and saves instead of playing: `-episodes 1-12` for a range, `-download-dir` or
  the `DownloadDir` config for the destination. Streams are remuxed to MP4 rather
  than re-encoded, so a download costs bandwidth and almost no CPU with no quality
  loss, and soft subtitles are muxed in where the provider supplies them. An
  episode already on disk is skipped without a provider round trip, and a failed
  download is deleted rather than left looking complete.

  Providers routinely disguise HLS segments as images (`…/seg-1-f1-v1-a1.jpg`),
  which ffmpeg's demuxer rejects by default; the extension check is relaxed so
  those streams download correctly.
- **Desktop theming.** On [Omarchy](https://omarchy.org/) the active theme's
  `colors.toml` is read from `~/.local/state/omarchy/current/theme/` and drives
  both the terminal menus and the rofi menus, so Curd matches the rest of the
  desktop. Configurable with `Theme=auto|omarchy|builtin`. Colours are chosen by
  measured contrast rather than assumed: the highlighted row falls back to the
  accent when a theme's selection colour is too close to its background to see,
  and text on a filled accent is picked light or dark to stay readable.
- **Redesigned rofi menus**, now generated from the palette and embedded in the
  binary rather than downloaded from GitHub on first run. The menus work offline,
  a theme change is picked up automatically, and a hand-edited `.rasi` is copied
  to `<name>.rasi.user-backup` before being replaced.

  The design is a flat plane anchored by a single accent rail, rather than a
  floating rounded card: radius is applied only at the outermost level so it
  reads as hierarchy, rows run full width with a filled selection band, and one
  hairline separates what you type from what you are choosing between.

  Sizing follows a modular type scale (11 / 13 / 16, roughly 1.2x) and a 4/8
  spacing rhythm, replacing ad-hoc values. The list menus are wider so a long
  anime title is not clipped, and the poster grid declares the four columns rofi
  actually renders rather than a number it silently ignores.

  Rows also carry typographic hierarchy now. `One Piece · 1171 eps [anipub]` was
  rendered at one weight, so a list was an undifferentiated wall of text; the
  title is now full strength and the episode count and provider are dimmed, so
  the eye scans titles down the left edge.

### Fixed

- **`curd -e` failed for anyone whose `$EDITOR` carries arguments.** The whole
  environment variable was passed to `exec.Command` as the executable name, so
  `EDITOR="omarchy-launch-editor --inline"` (Omarchy's default) produced
  `executable file not found in $PATH`. The same broke `code --wait`, `subl -w`
  and any other editor invoked with a flag. The value is now split the way a
  shell would, honouring quotes so a path containing spaces survives, and
  `$VISUAL` takes precedence over `$EDITOR` per convention.

  The failure was also invisible: `CurdOut` sends to a desktop notification when
  `RofiSelection` is on, so the terminal the user was looking at stayed silent.
  `-e` now reports to the terminal it runs in, as `-u` already did, and an editor
  that is not on `PATH` gets a message saying so instead of a raw exec error.
- **Playback polling loops now stop when MPV exits** (upstream Wraient/curd#58,
  "Pipe Status Spam"). Three loops polled the IPC socket and treated a dead
  connection as a transient error: the Discord presence loop retried every 5s
  forever, the CLI next-episode branch every 1s, and the playback-status check
  logged and re-polled because "MPV has exited" was handled as an inconclusive
  error rather than as "nothing is playing". Each now recognises a gone
  connection and stops. This is the other half of the empty-socket fix in 2.1.0,
  and the cause of the multi-megabyte logs and the Windows terminal flood.
- **A stacked search no longer waits on the slowest provider.** It waited on all
  of them, so animepahe's ~17s browser challenge set the pace even when anipub
  had answered in 0.2s. A straggler now gets a 2.5s grace window once another
  provider has produced results, with a 20s overall deadline while nothing has
  succeeded. Healthy providers are unaffected.
- **Failures are grouped by cause.** Every provider error used to be
  concatenated onto one line, burying whether the hosts were down or simply did
  not carry the show. Clean misses now collapse onto a single line and the
  message says which situation it is.
- **Providers that just failed are no longer re-probed every search.** Two
  consecutive unreachable failures put a provider on a 5 minute cooldown. A
  provider answering "no results" is working correctly and is never cooled down.

- **Anime titles containing `&`, `<` or `>` no longer break their rofi row.**
  Curd passes `-markup-rows`, so labels are parsed as pango markup, but they were
  emitted unescaped. Titles are escaped now, and selections are unescaped on the
  way back so they still match the option they came from.

### Performance

- `Log()` kept the log file open instead of reopening it for every line, which
  cost an open/write/close syscall triple per entry. Writes stay unbuffered.

## 2.1.0 — provider resolution fixes

Fork of [Wraient/curd](https://github.com/Wraient/curd) at v2.0.7. Drop-in
replacement: same binary name, config path, and stored tokens.

### The reported symptom

Curd could not find any anime. A real `debug.log` showed three independent causes
firing at once for a single search:

```
Provider senshi search failed:  Post "https://senshi.live/anime/filter": EOF
Provider anipub search failed:  no results for "Saijo no Osewa: Takane no Hanadarake na ..."
Provider anineko search failed: context deadline exceeded (Client.Timeout exceeded while awaiting headers)
Provider search failed: all provider searches failed
```

### Fixed

**Search used the romaji title only.** `curd.go` seeded every provider search with
`anime.Title.Romaji`, ignoring the configured `AnimeNameLanguage` and never trying
another title. AniList supplies *"Saijo no Osewa: Takane no Hanadarake na Meimonkou
de, Gakuin Ichi no Ojou-sama (Seikatsu Nouryoku Kaimu) wo Kagenagara Osewa suru
Koto ni Narimashita"*; anipub indexes the same show as *"Rich Girl Caretaker"* and
matches it instantly. Searches now walk an ordered list of title variants —
preferred language first, then the other titles, then simplified forms with
parentheticals, season suffixes, and subtitle clauses removed — stopping at the
first that returns results. A query the user types is never overridden.

**senshi.live is a parked domain.** The domain lapsed and now serves a
domain-broker page over HTTP while aborting the TLS handshake entirely. It was
first in `preferredProviderOrder` *and* the hardcoded return value of
`firstEnabledProviderName()`, so every search began by waiting on a host that can
no longer answer. Retired, with the fallback now derived from what is actually
registered and enabled.

**One slow provider sank the whole search.** Providers were searched sequentially
against a shared 15s client timeout with no retries, so a single stall consumed the
budget and the run reported "all provider searches failed". Providers are now
searched concurrently and transient failures (timeouts, resets, truncated
responses) are retried once. A definitive "no such show" answer is never retried.
Results are still merged in configured stack order.

**anipub's newer episode links did not resolve.** anipub serves two link shapes:
the legacy `/video/{embedId}/{mode}` and a newer `/play/{malId}/{ep}/{mode}` that
fronts a different megaplay route. Only the first was handled, so newer shows
failed with `unsupported video link` after searching and listing correctly. Both
shapes are resolved now.

**AllAnime listed shows that could never play.** Its episode endpoint now rejects
unsigned requests with a `AA_CRYPTO_MISSING` GraphQL error (HTTP 200, null
episode), while catalogue search still answers. The failure surfaced as the
unhelpful "no encoded Allanime provider sources found". It is now detected and
reported explicitly, and AllAnime is disabled by default so it cannot offer
unplayable results. `allanime.day` is additionally serving a 301 redirect loop, so
there is no live frontend to derive the signing scheme from.

**A failed MPV launch spun a hot loop.** When MPV never came up, an empty IPC
socket path was still polled, and each poll retried three times against
`dial unix: missing address` — producing multi-megabyte logs. Missing socket paths
now fail immediately and count as a closed session, so pollers stop.

**Background prefetch could race and leak.** `prefetchAfterPlaylistSwitch` ran as
an untracked goroutine writing `anime.Ep.NextEpisode` without holding the
controller lock, and kept doing network work after its caller had moved on. It is
now tracked, awaitable via `WaitForPrefetch`, publishes under the lock, and skips
entirely when there is no MPV socket to feed.

### Added

- `curd -provider-status` probes every registered provider concurrently and reports
  which work, which are disabled and why, and how long each took. Exits non-zero
  when nothing answers. Use `-provider-status-query` to probe with a specific title.
- **anidb** provider (`anidb.app`), the host ani-cli moved to for its v5 provider.
  anidb.app was serving a site-wide maintenance page throughout development, so
  this provider is written against ani-cli's documented endpoint shapes and **has
  not been verified against live traffic**. It is disabled by default and guarded
  by explicit maintenance and Cloudflare detection; add `anidb` to `Provider` to
  try it.

### Changed

- Default provider order is now `anipub, anineko, anidb, senshi, allanime,
  animepahe`, ranked by what currently resolves streams end to end.
- Shared HTTP client: 25s timeout (was 15s), with explicit TLS handshake and
  response-header timeouts and a larger connection pool for concurrent search.
- The built-in updater and rofi theme fetch point at this fork, so a future
  upstream release cannot replace this build with the unfixed one.

### Verified

- `go test -short -race ./...` passes clean.
- Live search regression (`CURD_LIVE_SEARCH_TEST=1`) confirms the reported show
  resolves, and that anipub — which indexes only the English title — is rescued by
  the variant fallback.
- Live stream resolution confirmed for anipub and anineko on both link shapes.

# Discord Bot

`bot/` holds the Otakase Discord bot, a small Go program (its own module,
built with [discordgo](https://github.com/bwmarrin/discordgo)) that runs on a
server and stays connected to Discord.

## What it does

- **Slash commands:** `/install` (steps per system, mirroring the README),
  `/faq` (the same answers as the server's FAQ channel), `/latest` (the newest
  GitHub release), `/anime` (an AniList lookup, never adult titles),
  `/link-github`, `/link-anilist`, `/profile` and `/help`.
- **Support forum:** each new post gets a checklist of what to include and a
  **Mark solved** button. The poster or a moderator pressing it adds the
  Solved tag and closes the post.
- **Wiki answers:** the same greeting links up to three wiki sections that
  match the post's title and question (`wiki.go`). The bot reads the pages
  listed under "Using otakase" in `_Sidebar.md` from the repo once a day and
  splits them at their `##` and `###` headings, so a link opens the right
  section. With Ollama the sections are matched by meaning (similarity at
  least 0.5); without it, by the words they share with the question. Nothing
  is added when no section is close enough. To make a page findable, give its
  sections headings that say the problem in a user's words.
- **Log reader:** a message in a support post with a pasted log or error
  (a code block, `[LOG]` lines, `otakase-debug.log`) or an attached `.log` /
  `.txt` file is scanned for errors the CLI prints (mpv or rofi or ffmpeg
  missing, sources blocked or down, network errors, AniList sign-in expired,
  casting finding nothing, …). The post gets up to three matching fixes, each
  at most once per post, and the FAQ auto-answer skips that message. The
  patterns use the CLI's own wording and are `logHints` in `logreader.go`.
- **Auto-answers:** a message in chat that looks like a common question (mpv
  missing, icons shown as boxes, nothing found, progress not syncing, casting
  not finding a TV, how to install or update) gets the matching FAQ answer as
  a reply, at most once per topic per channel every 30 minutes. The patterns
  are `autoRules` in `chat.go`.
- **Spam:** the same message from one member in three channels within a
  minute is deleted everywhere, the member is timed out for an hour and the
  moderator channel is told. Discord's AutoMod handles the rest.
- **Welcome DM** to each new member, unless `OTAKASE_WELCOME_DM=false`.
- **Contributor role:** `/link-github` uses GitHub's device flow (the member
  enters a code on github.com, so the bot needs no web address) and gives the
  Contributor role when that account has a merged pull request here.
- **AniList profiles:** `/link-anilist` links a member's AniList account and
  `/profile [member]` posts a landscape picture card the bot draws, inside
  a components v2 container (Shu accent, no mention, last watched from
  AniList activity, Profile / Anime list buttons; picture in an
  embed if Discord refuses that; `profilecard.go`,
  fonts in `assets/fonts`): avatar, mean score gauge, anime count, episodes,
  days watched, what they are watching now with cover art and progress, and
  favourite genres (adult titles and genres left out). The top is their
  AniList banner, or a blurred cover of a show they watch when they have none.
  If drawing fails it falls back to a plain embed. `/unlink-anilist` removes it. Linked members get the
  "AniList linked" role when the server has a role with that name
  (`OTAKASE_ANILIST_ROLE` sets another). See [AniList linking](#anilist-linking).
- **Issue links:** `#123` in a message gets a card for that GitHub issue or
  pull request (title, open/merged/closed, labels), at most once per number
  per channel every 10 minutes. Code spans and channel mentions are skipped.
- **/provider-status:** runs `otakase -provider-status` with the latest
  release (bundled in the add-on, fetched by `install.sh`) and lists which
  sources answer, cached for five minutes. 18+ sources are never listed.
  The same check is posted as a reply when someone asks in chat whether
  otakase or a source is down (`downRe` in `providers.go`), at most once per
  channel every ten minutes.
- **Matching by meaning (optional):** with a local Ollama
  (`OTAKASE_OLLAMA_URL`; the add-on defaults to the Ollama add-on's address),
  questions the word patterns miss are compared with example phrasings
  (`intentExamples` in `semantic.go`) using the small `all-minilm` embedding
  model, and the closest FAQ answer or the source check is sent when the
  similarity is at least `OTAKASE_AI_THRESHOLD` (0.6). Only messages that
  look like questions are checked, and the bot still sends only its own fixed
  answers. It downloads the model itself on start; without Ollama nothing
  changes. To catch a new phrasing, add it to `intentExamples`.
- **Airing today:** each day at `OTAKASE_AIRING_HOUR` (UTC, default 6) the
  anime channel gets a live board of the Japanese episodes airing in the next
  24 hours from AniList, never adult titles (`airing.go`, picture in
  `airingcard.go`). A drawn picture (date, episode count, the most popular
  show as the spotlight, six more covers) sits above the 20 most popular
  shows grouped by hour, all in one Shu card (components v2), times as
  Discord timestamps so each reader sees their own zone. Every 5 minutes
  the bot checks whether an hour started or ended and then rebuilds the
  card around the posted picture: aired hours small and crossed out, the
  current one marked airing now, the next up next with a countdown. The board (message id, the day's shows) is kept in the
  data file; yesterday's board, and posts from before the live board, are
  deleted when the new one goes up. Deleting today's board makes the bot
  post it again on its next pass (5 minutes). Under it, a menu opens a show's card for
  only the member who picked it (`airingmenu.go`: score, genres, synopsis,
  DM me when it airs via the follow list, a watch party in 15 minutes,
  AniList and MyAnimeList links) and **My shows today** lists today's
  episodes from their linked AniList (watching, planning, paused,
  rewatching). `OTAKASE_DAILY_AIRING=false` turns it off.
- **Episode DMs:** `/follow` (AniList title suggestions) adds a show to a
  member's list, up to 50; `/following` lists them and `/unfollow` removes
  one. Every 5 minutes the bot asks AniList which followed shows aired since
  the last check and DMs each follower. Follows and the last check time are
  in the data file, so a restart neither repeats nor skips an episode (gaps
  over a day are dropped). Members with DMs from server members off get none.
- **Report on GitHub:** the support checklist has a button, and `/bug` does the
  same inside a post: it opens a new GitHub issue pre-filled with the post's
  title and question.
- **Version check:** when a support post mentions an otakase version (like the
  `otakase -v` line) older than the latest GitHub release, the bot replies once
  per post with the update command `otakase -u` and a changelog link.
- **Stale posts:** a support post quiet for a week gets a nudge to its poster;
  with no reply three days later it is closed. Solved posts are skipped.
- **/watchparty:** makes a voice channel for the party and a server event in
  it with the show's AniList banner, starting in the given minutes. While
  typing the anime, AniList's matching titles (with format and year) are
  suggested so the host picks the exact show; typed text that isn't picked
  uses AniList's best match. Options
  set a member limit and who can talk and screen share: everyone talks and
  only the host shares (default), everyone talks and shares, or only the host
  does both. Party channels go in a **WatchParty** category, made at the top with the
  first party and deleted when the last party channel goes. The party's
  channel stays until the start. After that the party ends a minute after
  the last person leaves (the minute lets someone whose connection dropped
  rejoin), or 15 minutes after the start if nobody came: the channel is
  deleted and the event is ended.
  Any member can start one, one at a time; the post's **Cancel** button
  deletes the event and the channel at once and works for the host and for
  anyone who can manage events.
- **Voice rooms:** joining the voice channel named **Create room** (or
  "join to create") makes a voice channel for that member, in the same
  category, and moves them in. `/room` does the same with a choice of public
  or private, a name and a member limit, and changes the room when the member
  already has one. A private room is hidden from everyone but its owner, the
  people they add with `/room-invite`, the staff roles and the bot. The owner
  can rename it and move people. A room is deleted after it has been empty
  for a minute (a new room waits two minutes for its owner). `OTAKASE_CREATE_ROOM` sets the
  channel when the name doesn't match.
- **Suggestions:** `/suggest` opens a form (idea and optional details) and
  posts it in the suggestions channel (the text channel named
  "suggestions", else "ideas"; `OTAKASE_SUGGESTIONS_CHANNEL` sets it) with
  👍 and 👎 buttons showing the counts and a thread to talk about it. Each
  member has one vote: pressing the same button again takes it back,
  pressing the other moves it. One suggestion per member every five
  minutes. Members who can manage messages run `/suggestion-status` inside a
  suggestion's thread to mark it Planned, In progress, Done or Declined (or
  Open again) with an optional note; Done and Declined close voting and
  archive the thread. Votes are kept in `otakase-bot.json`.
- **Report to mods:** right-click a message → **Apps → Report to mods**
  posts it in mod-chat with **Delete message**, **Timeout 1h** and
  **Dismiss** buttons. The reporter gets a private thanks.
- **Warnings:** `/warn` (members who can time out) records a warning, DMs the
  member and logs it in mod-chat; every third warning is a one hour timeout.
  `/warnings` lists a member's warnings, or clears them with `clear`.
- **Raid guard:** ten or more joins within a minute pings Moderator and
  Maintainer in mod-chat and pauses invites for 30 minutes (needs **Manage
  Server**; without it the alert says to pause them by hand).
- **New accounts:** members whose account is under a week old are noted in
  mod-chat when they join, and their links are removed during their first
  day, with a short note that deletes itself.
- **Scam filter:** free Nitro and Steam gift lures, lookalike Discord and
  Steam domains (`dlscord`, `stearncommunity`) and `@everyone` with an
  outside link are deleted, the author is timed out for an hour and mod-chat
  is told. Members who can manage messages are skipped. The patterns are in
  `moderation.go`.
- **/purge** (manage messages) deletes up to 100 recent messages, optionally
  one member's, skipping ones older than 14 days. **/slowmode** (manage
  channels) sets a channel's slowmode.
- **/post-welcome** (manage server) deletes the bot's posts in the welcome
  channel and posts the welcome message again, linking channels found by
  name, so renamed or new channels are right. The text is in `welcome.go`,
  the header image in `assets/`.
- **Status:** Do Not Disturb, cycling every two minutes through the custom
  statuses "Watching anime · /help", "Otakase <latest release> is out", "Get
  it: otakase.xyverion.com", "Anime in your terminal" and "/suggest an idea"
  (`presence.go`). Custom statuses
  because the member list hides a bot's "Playing ..." text. Bots can't show
  rich presence images or buttons.
- **Health alerts:** when a background job fails (the daily airing post,
  episode DMs, the stale support check, Ollama staying unreachable) or panics, the bot posts
  a short note in the text channel named "bot-log", else mod-chat
  (`OTAKASE_LOG_CHANNEL` sets it). Each job alerts at most once every six
  hours and posts once more when it works again; a panicking job restarts
  after a minute. After a crash it posts "Bot restarted after a crash" with
  the start of the panic output. A marker file (`otakase-bot.running`) in the
  data directory is removed on a clean stop, and Go writes fatal panics to
  `otakase-bot.crash` there (`health.go`).

- **Support extras:** Mark solved locks the post and asks who helped
  (helper points, `/thank`, `/helpers`, Helper role at `helper_points`).
  Automatic answers carry Helped / Didn't help buttons; misses go to the bot
  log and `/answer-stats`. `/support` builds a post from a form, `/diagnose`
  explains `otakase -diag`. A pinned Known issues post lists open issues
  labelled `known-issue` (`knownissues.go`); new posts get links to similar
  earlier posts (`similarposts.go`, Ollama similarity 0.75 or shared
  keywords); `/bug` posts hear when the fix ships (`bugfollow.go`, matched by
  the post link in the issue body). `/support-queue` for staff.
- **Releases:** `/changelog [version] [since]`, `/suggestion-issue` (needs a
  `github_token` that can write issues; closing the issue settles the
  suggestion), `/github` for server admins to start a repository workflow
  (build, release, dev build, site, wiki, provider check; every build tests
  the Windows installer and a release also runs winget and the Discord post;
  `githubworkflows.go`), and a "Bot updated" note in the bot log from the embedded
  `CHANGELOG.md` (`botupdate.go`).
- **AniList social:** season preview on the first day of each season
  (`season_preview`, `/season-preview`), `/server-top`, `/compare`,
  `/recommend`, Plan to watch (needs a proven AniList link, see below).
- **Play in Otakase:** buttons lead to `otakase.xyverion.com/open/?anilist=ID&ep=N`
  (`site/open/`), which opens `otakase://play/anilist/ID?ep=N` in the app.
- **Watch parties:** RSVP buttons, reminder and cancel DMs, a Watch party
  pings role ping (`parties.go`); `sync:True` makes a synced party
  (`partysync.go`, see [App link](Developer-App-Link)). Voice rooms get a
  controls panel in their chat (`roompanel.go`).
- **Community:** spoiler guard (`/spoiler-guard off|nudge|hide`),
  a self-roles card (`/post-roles`), quiet levels (`/rank`,
  `/levels`, optional roles Level 5/10/20/30). Spoiler guard and XP sort
  channels by name: chat channels contain general, anime, chat, off-topic,
  lounge, seasonal, discussion, manga or memes; staff channels mod, staff,
  admin or logs.
- **Games:** played only in the [Games Activity](#games-activity); in chat
  `/activity` and the button under `/leaderboard` open it, `/leaderboard` shows
  one points board for every game (`games.go`) and `/riddle-add` adds an
  emoji riddle (`chatgames.go`). The games: the daily puzzle (one show a day
  for everyone, 8 tries, each try shows how close its year, format,
  episodes, score, studio, source, genres and popularity are; `daily.go`),
  guess the anime (`guess.go`), higher or lower (`higherlower.go`), emoji
  riddles and title scramble (`chatgames.go`), the puzzles in
  `activitypuzzles.go` (zoomed cover, timeline, connections, redacted,
  anime grid, hangman), wordle, quiz and two truths, one lie
  (`activityquiz.go`), the party games in `activitysocial.go` (quiz show,
  imposter, draw it, hot potato, guess who), Koi-Koi (`activitykoikoi.go`)
  and watch bingo, a monthly 3×3 card filled from the
  member's linked AniList (`bingo.go`). Points: 1 per Activity win (10 a
  day at most), 5 to 2 for the daily puzzle, 1 per 3 in a row in higher or
  lower (5 a day at most), 2 per bingo line and 5 for a full card. Every
  Monday last week's winner gets the **Game Champion** role (made by the
  bot when missing) and a post in the games channel (else general, chat or
  lounge). Every game except bingo and riddles draws from one pool of the
  500 most popular shows, refreshed weekly.
- **Hanko (判子):** the server's currency (`hanko.go`), no earning limit:
  1 per game point (`addGamePoints`), 1 per chat message that earns XP, and
  5 per 20 minutes watched in a linked app (heartbeats). `/hanko` shows a
  member's balance and the richest; staff change it with `/hanko-give`.
- **Shinigami Notebook giveaway** (`giveaway.go`): staff `/giveaway start
  [prize] [join_minutes] [page_minutes]` posts it with a thread for the
  pages, `/giveaway next` closes joining or writes a page early,
  `/giveaway end` calls it off. Every page writes out a third of the
  survivors at random, sometimes with a twist (filler, double page, revival
  arc, plot armor, free Shinigami Eyes); the last name wins. The Shop sells
  six items for hanko, two per member per giveaway, each costing more than
  the last. Without a prize the winner gets a minted code from Claim prize.
- **Staff codes:** `/codes mint|list|revoke` (`codes.go`) make one-use
  `/redeem` codes for giveaways and events. Every redeemed code adds a
  watch ticket (`WatchTickets` in the data file) for the Activity watch
  room.
- **Moderation:** deleted and edited messages go to `mod-log` (else
  mod-chat); `/verification off|on|auto` (an Unverified role and a verify
  channel, see the comment at the top of `verification.go`); warnings expire
  after `warning_days`; appeals from warn and timeout DMs (`appeals.go`).
- **Staff tools:** `/bot-status`, `/settings` (runtime overrides kept in the
  data file; add one with a line in the `settings` table in `settings.go`),
  `/preview`.
- **App link:** `/link-app`, `/linked-apps`, `/unlink-app`, `/whos-watching`,
  `/watch-time`, the Tester role for development builds, app reports and
  news, served by the public API. See [App link](Developer-App-Link).
- **App link, both ways:** the bot also sends to linked apps through their
  inbox: Queue in Otakase buttons on show cards, `/share-anime`, `/remote`,
  new episodes of followed shows, replies to support posts, fixed bugs,
  update notices, party reminders and `/beta` (DevBuilds follows the Tester
  role). Watch with me parties, Ask to party under `/whos-watching`, party
  chat, reactions and skip votes in mpv. From heartbeats: `/badges`,
  `/wrap`, `/club`, `/server-watch-top`, `/featured`; apps send source
  reports (in `/provider-status`), settings backups (`/settings-backup`),
  scores (reviews channel) and crash reports. `/linked-role` for the
  Otakase linked role (`linkedrole.go`). New features register their
  commands, buttons, jobs and heartbeat listeners from `init()` through
  `registry.go`.
- **Dashboard:** a page in the Home Assistant sidebar through ingress
  (`dashboard.go`; add a section with `addDashSection` and place it in a
  menu group in `dashPages` in `dashlayout.go`, which also holds the page
  look and menus). Pages in
  `dashpages.go`, `dashextra.go` and `dashmore.go`; controls in
  `dashcontrols.go` post a form with a per-run token and log each change to
  the staff channels. Texts staff edit there are listed in `texts.go`.

The text it sends lives in `content.go`; keep it in step with the README and
the FAQ channel.

## Server setup for the newer features

Optional, everything missing is skipped or created by the bot:

- **Roles:** Helper, Tester, Watch party pings (created by `/post-roles` if
  missing), Level 5 / 10 / 20 / 30, Linux / Windows / macOS / Release pings
  for the roles card. Keep the bot's role above them. Unverified is created
  by `/verification`.
- **Channels:** a staff-only `mod-log`; the bot creates `証・verify` when
  verification first turns on. The bot reads the announcements channel for
  the app's news.
- **Support forum tags:** Linux, Windows, macOS (for `/support` and app
  reports). Unpin any other post so Known issues can be pinned.
- **GitHub:** a `known-issue` label; `github_token` with Issues read and
  write for `/suggestion-issue`, and Actions read and write for `/github`.
- **App link roles (exact names):** Tester (for `/beta`), and for watch
  badges 3-Day Streak, 7-Day Streak, 30-Day Streak, Binge Watcher, Season
  Finisher, Season Collector, and Touch Grass (the hidden one). Missing
  ones are skipped, never created.
- **App link channels (by name, else the anime channel):** `club` (or
  `anime-club`, `watch-club`) for the club board, `top` (or `watch-top`) for
  the Monday most-watched post, and one ending in `reviews` (or `review`,
  `ratings`) for scores shared from the app; without it, sharing fails.
- **Time capsule:** the birthday post goes in `general` (or `chat`,
  `lounge`, `off-topic`, else announcements) at 09:00 UTC on April 12. Each
  sealed message gets a line in `mod-log` with a Remove button;
  `/capsule-remove` does the same by id.
- **Linked role:** Developer Portal → General Information → **Linked Roles
  Verification URL** `https://bot.xyverion.com/v1/linked-role`; OAuth2 →
  Redirects: add `https://bot.xyverion.com/v1/linked-role/callback`;
  `discord_client_secret` set (as for the Activity); restart the bot once so
  it registers the fields. Then Server Settings → Roles → a role → Links →
  Add requirement → Otakase (Hours watched, Otakase app linked).
  `OTAKASE_PUBLIC_URL` changes the public base URL if it ever moves.
- **Add-on options:** `helper_points` (5), `warning_days` (90, 0 = never),
  `season_preview` (true), `public_port` (8100).

## Discord application settings

In the developer portal, under **Bot**, turn on **Server Members Intent** and
**Message Content Intent**; the bot exits at login without them. It also asks
for message reactions (party reactions), which needs no switch. It needs the
Bot role in the server, with permission to send messages, manage messages
(spam removal), manage threads (closing solved posts), moderate members
(timeouts), manage roles (Contributor, placed above that role), create
events (`/watchparty`), manage channels (`/slowmode`, voice rooms) and move
members (voice rooms). Manage Server is optional: it lets the raid guard pause
invites.

Warnings, suggestions, linked AniList accounts, followed shows and the list of voice rooms are kept in `otakase-bot.json` in
`OTAKASE_DATA_DIR` (the add-on's `/data`, `/var/lib/otakase-bot` with the
systemd unit, else the working directory).

## GitHub linking

`/link-github` appears only when `OTAKASE_GITHUB_CLIENT_ID` is set: the client
ID of a GitHub OAuth app (github.com → Settings → Developer settings → OAuth
Apps → New) with **Enable Device Flow** ticked. The homepage and callback URL
can be the website; neither is used.

## AniList linking

Without `OTAKASE_ANILIST_CLIENT_ID` and `OTAKASE_ANILIST_SECRET` (add-on
options `anilist_client_id` and `anilist_secret`), `/link-anilist` asks
for an AniList username. That needs no setup but isn't checked, so anyone can link any public profile; the card then says "username
not verified".

With them, linking is proven: create an app at anilist.co → Settings →
Developer → Create New Client, with the redirect URL
`https://anilist.co/api/v2/oauth/pin`, and set its client ID and secret.
The member opens AniList, presses Approve, copies the code AniList shows and
pastes it into the bot's form. The bot trades the code for a token (AniList
only offers the authorization code grant, which needs the secret) and reads
which account it belongs to. It keeps the AniList user id, the name and the
token (in the data file, readable only by the bot) so Plan to watch can add
shows to the member's list; `/unlink-anilist` deletes it, and a token AniList
refuses is dropped. Profiles are read from AniList's public API, so a list set to private shows the stats without
the watching list.

## MyAnimeList linking

`/link-mal` lets members link MyAnimeList so the watch room moves their MAL
list on, like a proven AniList link. It needs a MAL API app: myanimelist.net
→ Account Settings → API → Create ID, App Type **web**, App Redirect URL
`https://bot.xyverion.com/v1/mal/callback` (`OTAKASE_PUBLIC_URL` +
`/v1/mal/callback` when the API lives elsewhere). Set its Client ID and
Client Secret as the add-on options `mal_client_id` and `mal_client_secret`
(`OTAKASE_MAL_CLIENT_ID`, `OTAKASE_MAL_CLIENT_SECRET`). The command gives a
one-use button to `/v1/mal`, which sends the member to MAL (PKCE "plain",
the only kind MAL takes) and back to the callback; the bot keeps the token
and refresh token in the data file and refreshes them as they run out.
`/unlink-mal` deletes them. Without the options the command says it isn't
set up.

## Hosting

### Home Assistant

The repo is also a Home Assistant add-on repository (`repository.yaml`, with
the add-on in `bot/`).

1. In Home Assistant open **Settings → Add-ons → Add-on Store**, then
   **⋮ → Repositories**, and add `https://github.com/TheXykril/otakase`.
2. Install **Otakase Bot**. Home Assistant builds it on the device, which
   takes a few minutes.
3. On the **Configuration** tab paste the token, save, then **Start** it on
   the **Info** tab with **Start on boot** and **Watchdog** on (Watchdog
   restarts it after a crash). The **Log** tab shows its output.

Updates show up in Home Assistant when `version` in `bot/config.yaml` goes up.

### Ollama on Home Assistant (optional)

For matching questions by meaning, add the repository
`https://github.com/SirUli/homeassistant-ollama-addon` in the Add-on Store,
install **Ollama** and start it. The bot's default `ollama_url`
(`http://76e18fb5-ollama:11434`) points at it, and the bot pulls
`all-minilm` (about 45 MB) on its own; the log says "semantic matching on"
once it is ready.

### Public API and Cloudflare Tunnel

The app talks to the bot through its public API on port 8100
(`public_port`; `OTAKASE_PUBLIC_ADDR` outside Home Assistant), reached from
the internet through a Cloudflare Tunnel, so no router port is opened:

1. Put the domain's DNS on Cloudflare.
2. Add the Cloudflared add-on repository
   `https://github.com/brenner-tobias/ha-addons` in the Add-on Store and
   install **Cloudflared**.
3. In its configuration set `external_hostname` to the Home Assistant name
   you want (or leave it empty) and add an additional host:
   `bot.xyverion.com` → `http://<bot add-on hostname>:8100`. The bot's
   hostname is shown on its add-on page (for a local repository add-on it
   looks like `xxxxxxxx-otakase-bot`).
4. Start Cloudflared and open `https://bot.xyverion.com/v1/ping`; it answers
   with the bot's version.

Don't publish port 8100 on the router: rate limits trust Cloudflare's
`CF-Connecting-IP` header.

### Games Activity

The games run as a Discord Activity: a page inside Discord with a menu of
the daily puzzle, guess the anime, higher or lower, emoji riddles, title
scramble, the puzzles (zoomed cover, timeline, connections, redacted, anime
grid, hangman, wordle, quiz, two truths and a lie), the bingo card and the leaderboard, played alone or
together. It is served by the public API under `/activity/` (`activity.go`
for sign-in and the daily puzzle, `activitygames.go` for guess, riddle,
scramble, higher or lower, bingo and the board, page in
`assets/activity`), and opens from the App Launcher, `/activity` or the button
under `/leaderboard`. Answers stay on the bot until a round ends; Activity
wins give at most 10 points a day.

The puzzles (`activitypuzzles.go`) are boards the bot keeps: each game
builds one from the show pool and takes moves (`pz/{game}/start` and
`pz/{game}/move`, `activitypuzzleapi.go`); the page only gets what it may
see. Zoomed cover's picture comes from `pz/pic`, never past the board's
zoom step. Connections and the grid build their groups and rules from
studio, year or era, source, format, genre and theme tags (adult themes left
out) so every tile has one group and every square at least three answers.
Quiz questions and two truths' facts are made from the same pool
(`activityquiz.go`: studio, year, episodes, genres, source, score, main
characters); wordle words are 4 to 7 letter title words or character names.
Sound effects and the music (a composed 16-bar dark lo-fi loop) are made in
`app.js` with Web Audio, no files;
the mode (off, effects, effects + music) is kept in `localStorage`.

Play together (`activityparty.go`, routes under `/activity/api/party/`)
puts everyone in the same Activity instance (the SDK's `instanceId`) in one
room kept in memory. The member who joined first is the host and picks the
game: race rounds (guess, riddle, scramble; 5 rounds, a clue every 12
seconds, first right answer wins the round), higher or lower party
(everyone votes, wrong votes are out) or a same-puzzle game (3 rounds,
everyone plays their own copy of one puzzle board via `party/move`, the
first to solve wins the round; the round ends when every board is done or
time runs out). The party games (`activitysocial.go`, moves through
`party/act`): quiz show (everyone answers the same question at once, 500 to
1000 points by speed), imposter (3+ players; one gets a similar show with
two genres in common, talk then vote, 3 rounds), draw it (each player draws
a title once or twice; strokes go up through `party/ink` and come down by
index, hint letters open from 35 seconds), hot potato (name a pool show in
the category before a hidden 20 to 45 second fuse runs out; last one in
wins) and guess who (1v1 on 24 main characters, pictures through
`face/{show}/{k}`; questions are asked in the call). Koi-Koi
(`activitykoikoi.go`, also through `party/act`) is hanafuda with 12 pool
covers standing in for the 12 months, so matching cards share a cover. The
host picks tables of 2, 3 or 4; everyone is shuffled onto as few tables as
that takes, all playing at once, and bots fill the empty seats (one person
alone plays a bot). A player who leaves is taken over by a bot. Deals are
8 cards each and 8 on the field for 2, 7 and 6 for 3, 5 and 8 for 4. The
combos follow the usual rules (brights, boar-deer-butterfly, poem and blue
ribbons, flower and moon viewing, 5 animals, 5 ribbons, 10 plains with the
sake cup counting as a plain); 7 or more doubles, and stopping while
someone else at the table had called koi-koi doubles again. Turns time out
after 45 seconds and decisions after 20 (then the bot logic plays for
you). After 3 hands the best total takes the game point; a bot on top pays
nobody. The winners get the usual game point. Pages long poll `party/state` with
the room's version, so every change reaches every screen at once; members
who stop polling for 45 seconds drop out. `party/daily` shows the room's
daily tries as colours only. Without the Activity set up there are no
games to play; `/activity` then shows today's board. To turn it on:

1. Developer Portal → the bot's app → **OAuth2**: copy the **Client
   Secret** (Reset Secret if none is shown) into the add-on's
   `discord_client_secret` option (`OTAKASE_DISCORD_CLIENT_SECRET`). Under
   **Redirects** add `https://127.0.0.1` and save: Discord refuses the
   sign-in ("Missing redirect_uri") when the app has no redirect, though
   Activities never use it.
2. **Activities → URL Mappings**: root mapping `/` → target
   `bot.xyverion.com/activity`.
3. **Activities → Settings**: turn on **Enable Activities**, then restart
   the add-on.

The page signs in with the Embedded App SDK (`identify` only), the bot trades
the code for the member's id with the client secret and gives the page a
12 hour session. Covers come through `/activity/api/cover/{id}`, since an
Activity can only load from its own address. `sdk.js` is the
`@discord/embedded-app-sdk` bundle; to update it:

```sh
npm i @discord/embedded-app-sdk esbuild
echo 'export { DiscordSDK } from "@discord/embedded-app-sdk";' > entry.js
npx esbuild entry.js --bundle --minify --format=iife --global-name=Discord --legal-comments=none --outfile=sdk.js
```

#### Watch room

The first tile, **Watch room** (`watchroom.go`, `watchrelay.go`,
`watchsource.go`, page in `assets/activity/watch.js`), has the bot play an
anime for everyone in the call, in step. Opening one takes a watch ticket
(from `/redeem` codes); the ticket is spent once the first episode plays,
so a room that never plays costs nothing. Staff need a ticket too. Only one
room runs at a time across the server; other calls see where it is. Members'
own `/watchparty` parties are separate.

- The member who opens it hosts: they search AniList, pick an episode and
  sub or dub, and control playback. Everyone else follows the host's
  position (late joiners jump to it) and can ask the host to pause, play,
  skip the opening (85 seconds) or go to the next episode; the host gets
  Yes and No. Staff can take control; when the host leaves, whoever has
  been in the room longest takes over. The host or staff can end it.
- A room lasts 4 hours from its first episode, as many episodes as fit.
  `/settings watch_room_max` sets how many fit in it (default 10; the
  opener and staff always get in).
- 18+ shows only in age-restricted voice channels.
- Episodes are found by the otakase CLI in the image:
  `otakase -stream-info <anilist id>:<episode>` (with `-dub`, and
  `-stream-adult` for 18+) prints the episode's links, headers and
  subtitles as JSON without asking anything. The next episode is looked
  up while the current one plays. The CLI must be new enough to have the
  flag; the Dockerfile takes the latest release, else main.
- The relay serves `/activity/api/watch/media/{key}/{id}`: the key is
  random per episode, ids are handed out for each address the playlists
  name, so it fetches nothing else. The master playlist is cut to one
  variant (the best at or under 720p) and one audio track, so everyone
  asks for the same segments; a 192 MB memory cache fetches each one once
  and the next three ahead. Segments served as pictures are unmasked.
  Subtitles become WebVTT (SRT and ASS are converted), English on by
  default, picked per viewer.
- Upload is about 2.5 Mbps per viewer at 720p, download about one stream.
  Each finished room is logged with its traffic (`watch room over: …` in
  the log, `WatchRooms` in the data file, last 20).
- Members with a proven AniList link or a MAL link get their progress set
  to the episode once they've been in the room for 80% of it (marked
  completed at the last episode, rewatches left as rewatches). Each member
  can switch it off in the room; staff can switch it off for everyone with
  `/settings watch_room_progress`.

The host's page reports its position every 4 seconds (`watch/beat`); the
room only wakes the other pages when it's more than 2 seconds off where
they expect, or when playback state changes. Pages long poll
`watch/state` like the party rooms.

#### Admin tools in the Activity

Staff (the owner, a staff role, or a role with Administrator, Manage Server
or Moderate Members; checked through Discord and cached for 2 minutes,
`activityadmin.go`) get a shield button in the Activity. Its sheet works on
the room they are in through `party/admin`, whoever hosts it, and staff can
also pick and end games like the host. Moderation: pause and resume (every
deadline moves on by the pause), skip a round (nobody wins it), end the
game (back to the lobby, no points), take back a race or puzzle round win
(the round and its point), clear a Draw it drawing, hide one answer in a
feed (× on the line), hide a member's wrong answers (they show as dots),
sit someone out until the next game, ±1 game point, and a break from games
for 1 hour or 1 day. Moderation steps go to the bot log channel.

A break (`GameBans` in the data file, also `/game-ban member hours reason`
and the dashboard) blocks every Activity request with the reason and when
it ends, takes the member out of every room, and stops all game points.
`/game-points member points` and the dashboard give or take points
(-100 to 100, never below 0); they count under the `admin` key so the
points-by-game board stays about games.

Pranks are harmless and never logged: upside down or mirrored screen, a
fake 残念 stamp that turns into "jk" (the answer still counts), a fake hint
toast (typed or a silly built-in one), runaway buttons for 30 seconds, a
fake +1000 points popup, confetti, an air horn and an earthquake (one
member or everyone), swapped profile pictures, a rigged imposter (always
them) or bomb (the fuse burns on, up to 40 seconds more, until they hold
it), and Koi-Koi bots that talk back. They live on the room: they end when
the room goes back to the lobby or a new game starts after one finished;
set-up made in the lobby carries into the next game. One-off effects go out
in the room view as `fx` with an id for 15 seconds; the page plays each id
once and skips the ones it finds on joining.

More moderation (`activitypranks.go`): lock the room (members and staff
stay, newcomers can only watch), make someone host, make someone a
spectator, put someone out of the room for 10 minutes, slow someone to one
answer every 3 seconds, warn someone (a popup they must close, and a real
warning that counts toward the `/warn` timeout), hide someone's Discord
name in every room (`HiddenNames`, shown as "Player N" until shown again)
and turn a game off for the whole server (`GamesOff`; greyed out in the
menu and lobby, and every start request for it is refused). The Live
games page lists the last 50 moderation steps (kept in memory, pranks
left out) and turns games off and on.

More pranks: screen effects for one member or everyone (fake disconnect,
shrink or grow for 30 seconds, fake kick, fake ban, fake win, fake update,
an anime quote, a soft jumpscare, their face raining down, the room booing
or cheering them) and switches that last until the game ends (ghost typing:
their wrong answers show under someone else's name; a 2× timer; rainbow
Comic Sans; upside-down text; a fake first place with 9999; a fish cursor;
typing shown backwards; a pink or random pen in Draw it; quacking taps; a
fake admin button that only says no; taps a second late; a keyboard that
sometimes types the key next door; invisible typing; answer buttons in
reverse; drunk mode; a spotlight round the pointer; vote counts passed
round one place). A prank name (typed or silly), a puppet line in the feed
under someone's name (text only, never a guess) and name roulette
(everyone shows someone else's name, reshuffled every 8 seconds) end with
the game too.

### Dashboard

The add-on shows an **Otakase Bot** panel in the Home Assistant sidebar
(turn on **Show in sidebar** on the add-on page if it isn't there). It is
served through ingress, so only Home Assistant users can open it. Outside
Home Assistant set `OTAKASE_DASHBOARD_ADDR=:8099` and open
`http://127.0.0.1:8099`; there is no login then, so keep it on 127.0.0.1.

Pages: overview (every background job with its last error, Ollama, memory),
settings, post as the bot, safety (join verification, spoiler guard, pause
invites), airing board, support, follows, suggestions, watch parties, voice
rooms, moderation, games (podium, points boards, levels), emoji riddles, AniList, linked apps and
the bot's recent log. Controls change the `/settings` values, verification
and spoiler guard modes, pause or resume invites, redraw the airing board,
run the quiet support post check, set a suggestion's status, clear a
member's warnings, post a message, end a guess round and reset the week's
guess points, edit the welcome DM, support greeting and `/faq` answers, add
notes to the known issues post, set the bot's statuses, re-sync slash
commands, download or restore the data file, close or rename voice rooms,
cancel watch parties, pick level roles and start a guess round. The Live
games page lists every Activity room with its players and can pause,
resume, skip or end a game, sit someone out, give or lift breaks from games
and fix game points. Every change is posted to the bot log or mod channel with the
Home Assistant user's name.

Pages sit in five groups: Status, Posts, Members, Events and Safety. Wide
screens show a side menu with every page; narrow ones (phones, small
dashboard cards) show the groups as a bar at the bottom and the current
group's pages as tabs at the top.

### Dashboard card

The add-on also adds an **Otakase Bot** card to the Home Assistant card
picker, showing the dashboard inside any dashboard. On start it writes
`otakase-bot-card.js` to the config folder's `www` folder (the add-on maps
the config folder) and adds `/local/otakase-bot-card.js?v=<version>` as a
JavaScript module resource through the Home Assistant API (`hacard.go`,
script in `assets/ha-card.js`). If the `www` folder is new, restart Home
Assistant once. Dashboards in YAML mode need that resource added by hand.
The card asks the supervisor for an ingress session, like the add-on page
does, so it works for admin users. Options: `page` (the dashboard tab to
open), `height` in pixels, `title`.

The same script also defines native cards that draw with Home Assistant's
own styles: `otakase-bot-status-card` (option `details`),
`otakase-bot-controls-card`, `otakase-bot-today-card` (option `shows`, how
many airing rows), `otakase-bot-leaderboard-card` (option `board`: `levels`,
`week`, `all` or `helpers` to fix one list) and `otakase-bot-support-card`.
They read `GET api/summary` on the dashboard port through the ingress URL
(one request shared by every card, every 30 seconds) and run controls with
`POST api/do`, a JSON body `{"do", "name", "v", "text"}` like the page's
forms, and the `token` from the summary in the `X-Otakase-Token` header
(`dashapi.go`).

### Linux server

Any Linux machine works:

```bash
curl -fsSL https://raw.githubusercontent.com/TheXykril/otakase/main/bot/deploy/install.sh | sudo bash
sudo nano /etc/otakase-bot.env   # paste the token
sudo systemctl restart otakase-bot
journalctl -u otakase-bot -f     # logs
```

`install.sh` installs Go, builds the bot from `main` into
`/usr/local/bin/otakase-bot` and runs it as the `otakase-bot` systemd service.
Run the same command again to update. `BRANCH=name` before `bash` builds
another branch. Settings are in `/etc/otakase-bot.env`
(`bot/deploy/otakase-bot.env` lists them); empty channel and role ids are
found by name.

### Panel hosts

On hosts that only take files, upload a Linux build
(`CGO_ENABLED=0 GOOS=linux go build -o otakase-bot ./bot`) together with an
`otakase-bot.env` (or `.env`) file next to it, and start `./otakase-bot`.

## Developing

```bash
cd bot
go test ./...
DISCORD_BOT_TOKEN=... OTAKASE_GUILD_ID=... go run .
```

Slash commands are registered to the one server at startup, so changes show
up at once.

### Watch-room torrent playback

The bot launches the CLI from the same checkout with `-torrent-serve HASH sub|dub`.
Install ffmpeg, ffprobe and a font package on non-container hosts. Container builds
accept `OTAKASE_CLI_REF` to pin the helper for branch validation; the add-on defaults
to main. The build fails if the helper cannot be built.

The helper buffers the selected video before announcing a complete VOD playlist.
Torrents larger than 1 GiB or containing more than 256 files are rejected before
opening storage. The download cache plus the completed source use at most 2 GiB
of payload space, excluding filesystem metadata. Downloading stops after ten
minutes. Downloads are limited to 8 MiB/s and 16 established peers; uploads are
disabled. One helper, including startup, runs at a time per bot process.

Segments are encoded on demand, so forward and backward seeks do not depend on
a growing playlist. One encoder runs at a time with one decoding, filtering and
encoding thread, at most 720p/24 fps, and a 30-second segment deadline. The helper
retains at most 16 segments, each capped at 4 MiB, in memory. The existing relay
cache is separately bounded. These are application limits, not an OS CPU quota.

Sub mode requires labelled Japanese audio and English dialogue subtitles; dub
mode requires labelled English audio and uses English signs/forced subtitles
when available. Commentary tracks are excluded. Embedded ASS/SSA, SRT and WebVTT
subtitles are burned into the video. Bitmap subtitles and unlabelled audio are
rejected when needed, rather than silently selecting the wrong language. Burned
subtitles cannot be toggled off.

Closing a room or changing episodes cancels pending startup and terminates the
helper. All temporary files live under its private directory. The bot kills
leftover encoder processes on helper exit and removes the directory before
releasing the helper slot. Inherited directory locks protect active helpers;
the next startup reclaims unlocked directories left by abrupt termination.

Local verification: run `go test ./...` in both the repository root and `bot/`,
and `go test -race ./...` in `bot/`. The CLI integration test generates a local
dual-audio MKV and checks subtitle rendering after out-of-order segment requests.
It requires ffmpeg and ffprobe; otherwise that test is skipped.

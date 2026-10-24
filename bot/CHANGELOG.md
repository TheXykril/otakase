# Changelog

## Unreleased

## 1.30.24

- Activity: return moderation and sound controls to a dedicated non-floating row.
- Watch Together: make player controls more compact and clarify when separate dubbed/subbed streams are available.

## 1.30.23

- Activity: restore floating moderation and sound controls with mobile safe-area positioning.
- Watch Together: remove the episode provider credit line from the picker.

## 1.30.22

- Watch Together: remove TVmaze episode artwork provider and its credit.
- Watch Together: fetch remaining episode metadata sources concurrently, limit request latency, and cache episode previews for 30 minutes.

## 1.30.21

- Watch Together: remove TMDB episode artwork integration and API key setting; other sources remain available without credentials.

## 1.30.20

- Watch Together: add Kitsu, TVmaze and optional TMDB episode artwork via AniMap ID matching.
- Watch Together: use compact text cards when episode artwork cannot load, with source credits.

## 1.30.19

- Watch Together: use Jikan for missing episode titles and display compact cards when episode artwork is unavailable.

## 1.30.18

- Activity: place moderation and sound controls in a dedicated toolbar instead of floating over game cards.

## 1.30.17

- Watch Together: separate episode numbers and titles, and omit duplicate fallback titles.
- Activity: fix mobile floating moderation and sound controls positioning beneath Discord overlays.

## 1.30.16

- Watch Together: load episode titles and supported thumbnails from Tenrai when opening the episode picker, with AniList fallback.

## 1.30.15

- Activity: reposition floating moderation and sound controls in portrait and landscape to avoid Discord overlays.

## 1.30.14

- Activity: keep menus and playback controls clear of Discord's landscape overlays and safe areas.
- Watch Together: show episode thumbnail and title cards when preview metadata is available, with numbered fallbacks otherwise.

## 1.30.13

- Activity: add `/games` for games only and `/watch` to open Watch Together directly; `/activity` keeps the full menu.
- Watch Together: improve theater playback controls with tap-to-reveal overlays.
- Activity: add mobile spacing for Discord controls across games, watch rooms, and moderation screens.
- Refresh the Activity menu and cover layout.

## 1.30.12

- Watch room: stop defaulting all episodes to Dub when no preference is selected.
- Look for separate Sub and Dub playback editions when a stream contains only one audio track, and show newly discovered languages in the player.
- Choose the playback format from the selected rendition when switching languages.

## 1.30.11

- Watch room: remember personal audio and subtitle language preferences and select them in the player.
- Keep multiple audio renditions in supported HLS playlists and simplify episode selection to a single step.
- Torrent playback: offer separate audio variants from one download and selectable subtitles when conversion is supported.

## 1.30.10

- Watch room: improve player controls with a clearer toolbar, subtitles and volume labels, and responsive sizing.
- Fullscreen button now uses a reliable theater layout inside the Activity, with native fullscreen when allowed by the host.

## 1.30.9

- Watch room: Sub and Dub preferences now use large, clear selection cards.
- Episode selection uses a responsive numbered card grid, with pagination for longer shows, instead of manual number entry.

## 1.30.8

- Watch room: choosing an anime opens a Sub or Dub preference menu before picking an episode, replacing the repeated dub checkbox.
- When Dub is unavailable, playback automatically falls back to Sub.

## 1.30.7

- Watch room: hosts can cancel an episode while it is being found or prepared, returning to episode selection without ending the room.
- Canceling stops the active lookup process and pending Relay preparation rather than letting the abandoned request continue.

## 1.30.6

- Watch room: public playback failures now show a clear, discreet explanation and a report code instead of technical helper errors.
- Home Assistant: new Watch room diagnostics card displays recent full error details, stages, episode names, and matching report codes for private troubleshooting.
- Activity: use Catalog and Relay terminology in preparation messages.

## 1.30.5

- Watch room: report the torrent helper's actual startup failure (such as unavailable peers, download timeout, or media probe error) instead of an invalid-address message.

## 1.30.4

- Watch room: display source lookup and torrent preparation stages with elapsed time in the Activity.
- Give slower torrent source searches up to eight minutes before timing out, and show a clear timeout error when they fail.

## 1.30.3

- Watch room: torrent episodes now resolve directly from indexed releases without waiting for peers during lookup. Source errors display their cause instead of always claiming the episode is missing.

## 1.30.2

- Watch room: increase the default torrent episode limit to 4 GiB, with an 8 GiB temporary storage budget. Both limits are configurable in the add-on options.
- Watch rooms can play torrent-only episodes after buffering, with bounded disk usage and encoding load.
- Torrent playback selects the requested audio language, renders embedded English text subtitles, and supports seeking throughout the episode.
- Room changes cancel pending downloads; helper crashes release temporary files. Playback synchronization now follows host buffering.

## 1.30.1

- Watch room: the Activity's music stops while you're in the room.
- Watch room: picking dub now plays the dub, and shows with several audio
  tracks play the English one for dub and the Japanese one otherwise.
- Watch room: fullscreen fills the Activity window.

## 1.30.0

- **Watch room** in the games Activity: the bot plays an anime for
  everyone in the voice call, in step. The member who opens it picks the
  show and episode and controls playback; everyone else follows along and
  can ask to pause, skip the opening or go to the next episode. Staff can
  take control. Up to 10 people (`/settings watch_room_max`), 720p, soft
  subtitles, 4 hours from the first episode. One room at a time across the
  server. 18+ shows only in age-restricted voice channels.
- Opening a room takes a **watch ticket**, spent once the first episode
  plays. Staff need one too.
- Watching an episode in the room moves your **AniList** (proven link) and
  **MyAnimeList** progress on. Switch it off for yourself in the room, or
  for everyone with `/settings watch_room_progress`.
- **`/link-mal`** and `/unlink-mal`: link MyAnimeList (needs the new
  `mal_client_id` and `mal_client_secret` options).
- Every `/redeem` code now gives a watch ticket and nothing else.

## 1.29.0

- **Hanko (判子)**, the server's own currency. Earned with no limit from
  game points in the Activity (one per point), chatting and watching in
  Otakase with a linked app. `/hanko` shows yours and the richest members;
  staff can give or take with `/hanko-give`.
- **Shinigami Notebook giveaway.** Staff start one with `/giveaway start`.
  Members press Join, then every few minutes a page of the notebook is
  written and random names on it are out, with the odd twist (filler
  episode, double page, revival arc…). Last name standing wins.
- The giveaway **Shop** sells ways to survive for hanko: Plot Armor, Fake
  Name, Shinigami Eyes, Apple for Ryuk, Revival Ticket and Decoy. Two per
  member each giveaway, every one costs more than the last.
- **`/codes`** lets staff mint one-use `/redeem` codes for giveaways and
  events, list the unused ones and revoke them. Redeemed codes now give a
  watch ticket for the upcoming watch room.

## 1.28.0

- **Time capsule** for Otakase's birthday, April 12. `/capsule message:`
  seals a message for the next birthday (one per member, replaced when you
  seal another; `/capsule` alone shows yours or takes it out). Linked apps
  can seal one too. On April 12 the server gets a post with one message
  picked at random, with Open another and Report buttons, and every app
  shows one as it starts.
- Messages are checked automatically, nobody has to approve them: no
  links, emails, phone or long numbers, mentions, invites or addresses, a
  swear filter that sees through l33t and spaced-out spellings, and with
  Ollama a meaning check against insults and creepy messages
  (`/settings capsule_threshold`). 3 reports hide a message. Staff can
  still take any out with the Remove button on its line in mod chat, or
  `/capsule-remove`. Members need a week in the server first.

## 1.27.1

- Fixed the add-on failing to build while Otakase has no release to download.
  The image now builds the Otakase CLI from source for `/provider-status`
  when there's no release, and still builds if that fails too (the command
  then says the check isn't available).

## 1.27.0

- **`/github`** (server admins only) starts a GitHub workflow from Discord:
  a build, a release (`release:true`, which also submits to winget and posts
  the release in Discord), a dev build, the site, the wiki sync or the
  provider check. Every build also tests the Windows installer. The bot's `github_token` needs Actions: Read and write.

## 1.26.0

### Otakase app link

- **Queue in Otakase**: show cards (`/anime`, the airing board, plans and
  episode DMs) get a button that sends the show to your linked apps, under
  **Community → From Discord**, ready to play.
- **The bot can now reach your app**: new episodes of shows you `/follow`
  pop up on your desktop with Play; replies to your support post and bugs
  you reported getting fixed show up in the app too.
- **`/share-anime`** recommends a show to a friend: a DM with Play and Queue
  buttons, and it lands in their app's queue.
- **`/remote`** pauses, plays, skips 85 s, goes to the next or previous
  episode or stops your own Otakase from Discord.
- **Update notices**: an app older than the latest release gets one notice
  and you get one DM per release.
- **`/beta`** takes the Tester role, which turns on development builds in
  your linked apps (and off again without it).
- **Watch with me**: turn it on in the app and your Discord status gets a
  button friends press to join your player, synced. `/whos-watching` now
  has Open episode and Ask to party buttons.
- **Parties in the app**: planned watch parties show in Community with I'm
  in, Join and a reminder in the app before they start.
- **In mpv during a synced party**: the voice channel's chat and emoji float
  on screen (Alt+c hides the chat), and everyone can vote to skip the
  opening or ending (Alt+k) or go to the next episode (Alt+n).
- **Watch badges**: day streaks, binge nights and finished seasons, with
  game points and matching roles. `/badges`.
- **Server club**: staff pick a show and an episode with `/club set`; a
  board shows who caught up, and the app shows the club in Community.
- **`/server-watch-top`** and a Monday post of the most watched shows on the
  server last week.
- **`/featured`**: a daily featured anime; 10 minutes of it gives game
  points once a day.
- **`/wrap`**: your year in Otakase, mentioned on `/profile` in December.
- **Otakase linked role**: `/linked-role` shows your hours watched as a
  Discord linked role that staff can require.
- **From the app**: source failures reported by apps show in
  `/provider-status` and "is it down?", settings backups you can restore on
  a new computer (`/settings-backup`), scores shared to the reviews channel,
  crash reports and "where to watch this?" posts. `/settings score_sharing
  off` turns score sharing off for the whole server; apps then stop asking.

### Fixed

- The "fixed in" note on a bug report now mentions the member for posts made
  from the app or with `/support`, not the bot.

## 1.25.0

### Games

- **`/daily` is now `/games`**: it opens the games Activity with every game,
  not just the daily puzzle.
- **Admin tools in the Activity**: staff get a shield button in any room,
  whoever hosts it. Pause and resume, skip a round, end the game, take back
  a round win, clear a drawing, hide an answer or all of one member's wrong
  answers, sit someone out until the next game, fix points and give a break
  from games. Staff can also pick and restart games like the host.
- **Pranks** for staff to play on friends, all harmless and gone when the
  game ends: upside down or mirrored screen, a fake wrong stamp that turns
  into "jk", fake hints, runaway buttons, fake +1000 points, confetti, air
  horn and earthquake, swapped profile pictures, always the imposter, the
  hot potato that waits for one player, and Koi-Koi bots that talk back.
- **Breaks from games**: `/game-ban` (or the dashboard) keeps a member out of
  the games and their points for up to 30 days, with a reason they see.
  `/game-points` gives or takes points.
- **More moderation**: lock a room, make someone host, spectators, out of
  the room for 10 minutes, one answer every 3 seconds, warnings from inside
  the Activity (they count toward `/warn`), "Player N" instead of a
  member's Discord name, and games turned off for the whole server.
- **More pranks**: fake disconnect, kick, ban, win and update; shrink or
  grow the screen; anime quotes; a soft jumpscare; face rain; boos and
  applause; ghost typing; a 2× timer; rainbow Comic Sans; upside-down text;
  a fake first place; a fish cursor; backwards, cursed or invisible typing;
  a pink pen; quacking taps; a fake admin button; fake lag; swapped answer
  buttons; drunk mode; a spotlight; lying vote counts; silly names; puppet
  lines; and name roulette.

### Dashboard

- **Live games** page: every Activity room with its players; pause, resume,
  skip or end a game, sit someone out, give or lift breaks from games and
  fix game points.
- Live games also turns games off and on and lists the last 50 moderation
  steps.

## 1.24.0

### Games

- **Koi-Koi** in the Activity: hanafuda, the Japanese flower card game,
  with anime covers on the cards (each month is one show, so matching cards
  share a cover). Play your card onto one of the same month to take both,
  then flip one from the deck. Make a combo and choose: stop and score, or
  call koi-koi and play on for more. Any number of players: the host picks
  tables of 2, 3 or 4, the call is split over as many tables as it takes,
  all playing at once, and bots fill the empty seats or take over for
  anyone who leaves. Alone, you play a bot. 3 hands, best total wins. On the
  menu and in Play together.
- Koi-Koi has a How to play guide (cards, turns, every combo and its
  points, koi-koi or stop). It opens on your first game and from the ? button
  or the lobby link.
- Phone layout: the sound button moved to the top corner so it no longer
  covers cards, inputs and buttons; party game picks sit two to a row; the
  anime grid's row labels and long Connections titles fit better; the
  Hangman answer box gets its own line.
- The red line at the top of the page stays attached to the top on phones
  instead of drifting over text.

## 1.23.1

### Games

- Redacted: the guess box takes typing again (every key but Enter was
  swallowed).
- New Activity music: a composed dark lo-fi loop instead of random notes.
  Slow, warbly electric piano chords in D Phrygian over a low drone, a deep
  bass, soft swung drums with brushes and a rim click, vinyl crackle and a
  koto melody, muffled and in a large room. Still off by default; the speaker
  button turns it on.

## 1.23.0

### Games

- Six new games in the Activity:
  - **Zoomed cover:** a tiny piece of a show's cover. Each miss zooms out
    and shows how close the guess was: year (with an arrow toward the
    answer), studio, shared genres, format and an overall closeness score.
    6 tries.
  - **Timeline:** put five shows in the order they aired. Shows in the
    right place turn green and show their year. 3 tries.
  - **Connections:** sixteen shows hide four groups (same studio, year,
    source, theme…). Pick four at a time; 4 mistakes allowed, with a "one
    away" hint.
  - **Redacted:** a show's synopsis with the words blacked out. Guess words
    to open them; open every word of the title, or type it, to win.
  - **Anime grid:** a 3×3 grid of rules (theme or genre × studio, era,
    source or format). Fill each square with a show that fits both; 12
    guesses.
  - **Hangman:** an anime title or a main character's name, letter by
    letter, with a hint. 6 misses.
- All six can be played together too: in **Play together**, pick one under
  **Same puzzle** and everyone gets their own copy of the same puzzle for 3
  rounds; the first to solve takes the round. Wins give game points under
  the usual 10 a day.
- Games are now played only in the Activity. `/guess`, `/higher-lower`,
  `/riddle`, `/scramble`, `/bingo` and typing guesses into `/daily` are
  gone; `/daily` and the button under `/leaderboard` open the Activity.
  `/leaderboard` and `/riddle-add` stay. The dashboard's Games page shows
  the calls playing together instead of chat rounds.
- Sound in the Activity: soft effects for taps, right and wrong answers,
  the stamp and the last seconds of a party round, plus optional calm
  music (koto-like plucks on a Japanese scale), all made in the page. The
  speaker button in the corner switches between off, effects (the default)
  and effects with music.
- Three more games to play alone:
  - **Wordle:** a word from an anime title or a main character's name, 4 to
    7 letters, 6 tries. Green is the right place, yellow is somewhere else;
    after 3 tries you get a hint. Also in **Same puzzle**.
  - **Quiz:** ten questions about studios, years, episodes, genres and main
    characters. 7 right wins.
  - **Two truths, one lie:** five shows, three facts each; spot the fake
    one. 4 right wins.
- New **Party games** in **Play together**, made for a call:
  - **Quiz show:** everyone answers the same question at once; faster right
    answers score more. Also with two truths, one lie.
  - **Imposter** (3+ players): everyone gets the same show except one, who
    gets a similar one and doesn't know it. Describe it in the call, then
    vote. 3 rounds.
  - **Draw it:** one player draws an anime title, the rest type guesses;
    letters open as hints over time.
  - **Hot potato:** the bomb goes round; name a show that fits the category
    (a studio, a year, a genre…) before it goes off. Last one in wins.
  - **Guess who** (1v1): 24 characters with their pictures. Ask yes or no
    questions in the call, flip cards down, then guess the other's
    character. A wrong guess loses.
- **Play together** shows members' Discord profile pictures in the lobby,
  standings and puzzle rounds, instead of only initials.

## 1.22.0

### Dashboard

- New look to match the games Activity: flat dark cards, the Inter font,
  thin line icons and faint お任せ lines in the background. The scrollbar is
  gone; a line along the top fills as you scroll.
- Easier to find your way around: pages sit in five groups (Status, Posts,
  Members, Events, Safety). Wide screens get a side menu with every page;
  phones get the groups as a bar at the bottom and the group's pages in a
  page picker at the top.
- The Games page shows stats better: a podium for this week, ranked boards
  with bars, points by game, what is running now with its controls, and
  chat levels with progress to the next level. Emoji riddles moved to their
  own Riddles page.

## 1.21.0

### Games

- Play together in the Activity: everyone who opens it from the same call
  shares a lobby that shows who is there, and the one who came first (the
  host) picks a game. Race rounds give everyone the same guess the anime,
  emoji riddle or title scramble for 5 rounds; clues come every 12 seconds
  and the first right answer takes the round, with the answers and points
  shown live. Higher or lower party has everyone vote on the same pair at
  once; wrong votes are out until one player is left. Round wins and the
  party win give game points under the usual 10 a day.
- The daily puzzle in the Activity shows how everyone in the call is doing
  today: their tries as colours only, never the titles.
- Guess the anime in the Activity shows its picture again (it was loaded
  without the sign-in, so Discord showed a broken image).
- The title suggestions under the answer box sit on top of the buttons
  again instead of behind them.
- Title scramble shows each word in its own box and says how many words
  the title has, so a wrapped line on a phone no longer looks like an extra
  word.
- The Activity has no scrollbar: on longer pages the Shu line at the top
  dims and a full-strength line fills it as you scroll.
- The Activity menu drops the Game Champion line under the cards.
- Higher or lower in the Activity shows both numbers after each pick: the
  higher show's card grows, the other shrinks, bars and a "how far apart"
  badge show the gap. A right pick moves to the next pair after a few
  seconds; a wrong one asks to play again and goes back to the menu after
  10 seconds.
- The Activity looks calmer and more its own: flat dark cards with a Shu
  accent, line icons instead of emojis, the Inter font served by the bot, no
  glows or gradient text. Leaderboard ranks are numbers, the bingo centre
  says Free.
- Each game card in the Activity menu has a small drawing of its game, a
  number and a katakana tab, and results get a hanko stamp: 正解 when right,
  残念 when missed.
- Title hints like "S _ _ _   C _ _" show as boxes, one group per word.
- The Activity background has faint diagonal lines of お任せ OTAKASE
  drifting in turns one way and the other (still for reduced motion).
- The higher or lower start screen is centred, with a short how-to and two
  big cards to pick score or popularity.

## 1.20.0

### Games

- `/games` is now `/leaderboard`: the points board first, every game listed
  under it.
- The Activity got a new look: coloured game cards, moving background,
  flip-in daily tiles, falling emojis and letter tiles, a podium on the
  leaderboard, confetti on wins and a shake on wrong answers. Turns calm for
  people who set reduced motion.

### Dashboard

- Members show by name on the dashboard, the Home Assistant cards and the
  Activity leaderboard, not as "member …" plus an id. Names the bot hadn't
  seen yet are asked from Discord; people who left show their username.

## 1.19.0

### Games

- The Activity is now a games menu: the daily puzzle, guess the anime,
  higher or lower, emoji riddles, title scramble, your watch bingo card and
  the leaderboard, all played on your own inside Discord. Guess, riddle and
  scramble wins give a game point each, up to 10 a day from the Activity.
  Open it from the App Launcher, `/daily` or the button under `/games`.
- `/guess-top` is gone: `/games` shows the points of every game, guess the
  anime included.

## 1.18.0

### Games

- **`/daily`**: one anime a day, the same for everyone, 8 tries. Each try
  shows how close its year, format, episodes, score, studio, source, genres
  and popularity are (green right, yellow close, arrows for higher or
  lower). Finishing posts your grid in the channel without spoilers, with
  your streak. Plays in chat with `/daily guess:`, or as an Activity inside
  Discord once the app's client secret is set (`discord_client_secret`, see
  the wiki).
- **`/higher-lower`**: is the next show's AniList score (or popularity)
  higher or lower? Keep the streak going; only you can press your buttons.
- **`/riddle`**: guess the anime from emojis, with a hint after 30 seconds.
  Add your own with `/riddle-add` (you can't win your own).
- **`/scramble`**: unscramble a popular show's title.
- **`/bingo`**: a 3×3 watch card each month (a movie, something from
  before 2000, a sports anime…) that fills itself from your linked AniList
  as you finish shows.
- **`/games`**: every game and one points board for all of them, this week
  and all time. Each Monday last week's winner gets the **Game Champion**
  role and a shout-out.

### Dashboard

- The games page shows game points, today's daily puzzle (players and
  solves) and the emoji riddles, with a button to remove one.
- The Home Assistant leaderboard card has game points this week and all
  time; the Today card shows the daily puzzle.

## 1.17.0

### Dashboard

- New **Otakase Bot** card for Home Assistant dashboards: add it like any
  card (search "Otakase Bot") to see the bot dashboard right there. Pick
  which page it opens on and how tall it is. The first time, restart Home
  Assistant once if the card doesn't show up in the list.
- Smaller cards that fit in with the rest of Home Assistant, all under
  "Otakase Bot" in the card list:
  - **Status**: members, latency, uptime, version and any failing jobs.
  - **Controls**: join verification, spoiler guard, daily airing board and
    welcome DM switches, pause or resume invites, redraw the airing board,
    check quiet support posts, re-sync commands.
  - **Today**: today's airing episodes, watch parties (cancel them), voice
    rooms (close them) and running guess the anime rounds.
  - **Leaderboard**: chat levels, guess the anime this week or all time,
    and top helpers; reset the week.
  - **Support**: open posts, bugs, suggestions, warnings and known issue
    notes (remove them, update the post).

## 1.16.0

### Dashboard

- More to see: every background job with its last error, Ollama, memory and
  data file size on the overview, plus new pages for settings, safety,
  the airing board, support (open posts, bug reports, top helpers, how
  automatic answers do), voice rooms, games (chat levels and guess the anime
  points) and the bot's recent log.
- Controls, no Discord needed: change the `/settings` values, set join
  verification and the spoiler guard, pause or resume invites, redraw the
  airing board, check quiet support posts now, set a suggestion's status,
  clear a member's warnings, post a message as the bot, end a guess round and
  reset this week's guess points. Each change shows up in the staff log with
  your Home Assistant name.
- Texts page: change the welcome DM, the support post greeting and the
  `/faq` answers.
- Known issues post: add or remove your own notes above the GitHub list.
- Bot page: your own rotating statuses, re-sync slash commands, download a
  backup of the bot's data or restore one.
- Close or rename voice rooms, cancel watch parties.
- Pick which role each chat level gives, and start a guess round in any chat
  channel.

## 1.15.1

### Moderation

- `/purge` now leaves one entry in the message log instead of two: the
  "messages deleted" summary says who purged (and whose messages, if a
  member was picked) and lists what was removed. Purging a single message
  is logged too.

## 1.15.0

### Guess the anime

- Better hints: every 30 seconds the round's message is edited with a new
  clue (added under the earlier ones) and a sharper cover, no extra
  messages. Every round the bot picks three clues at random from a pool (what
  it's based on, tags, AniList score, when it aired, format and episodes,
  genres, popularity rank, studio, a main character), hardest first. The
  last clue is the title with letters blanked: either each word's first
  letter (`F _ _ _ _ _ _`) or only the vowels. Rounds now last 2 minutes.

## 1.14.1

### Airing board

- The picture on top no longer disappears when the bot updates the board
  through the day.
- AIRING NOW and UP NEXT are written in Otakase red, as letter emojis the
  bot uploads itself (Discord can't colour text).
- Aired hours are no longer crossed out: they move to a small "Aired today"
  list at the bottom of the board.

## 1.14.0

### Support

- Mark solved also locks the post and asks who helped. The people you pick
  are thanked in the post and get a helper point; `/thank` gives one inside
  your post. `/helpers` shows the top helpers, and at 5 points (add-on option
  `helper_points`) members get the Helper role.
- The bot's automatic answers (FAQ replies, wiki links, log fixes and the
  update reminder) have Helped and Didn't help buttons. Didn't help tells
  staff in the bot log, and `/answer-stats` shows which answers miss most.
- `/support` opens a short form and makes a support post from it, tagged with
  your system. `/diagnose` shows how to share `otakase -diag`, and the bot
  reads that block like a log.
- A pinned Known issues post in the support forum lists the problems already
  known (GitHub label `known-issue`) and what the latest release fixed.
- New support posts get links to earlier posts that look like the same
  problem, solved ones first.
- When a post reported with `/bug` gets fixed, the bot tells the poster which
  version has the fix and how to update.
- `/support-queue` (staff) shows posts with no reply yet, ones waiting on
  staff for over a day, and what was solved this week.
- The Stop DMs for this show button on new episode DMs works again.

### Releases

- `/changelog` shows what changed in an Otakase release, and `since:` your
  version shows everything newer in one card.
- Staff can turn a suggestion into a GitHub issue with `/suggestion-issue`;
  when the issue closes, the suggestion is marked done or declined.
- After an update the bot posts what changed in the bot log.

### Anime

- On the first day of each anime season the anime channel gets a season
  preview with Plan to watch and DM me when it airs (add-on option
  `season_preview`; staff can post it with `/season-preview`).
- `/server-top` shows what members here watch now and finished this week,
  `/compare` puts your AniList next to someone else's, and `/recommend` picks
  five shows for you with the reason for each.
- Plan to watch adds a show to your AniList. It needs `/link-anilist` with
  the AniList approval, so members linked before this update link again.
- Play in Otakase buttons on airing board show cards, `/anime` results and
  new episode DMs open the show (or that episode) in the app.

### Watch parties and rooms

- Watch party posts have I'm in and Can't make it buttons and show who's
  going. Everyone going gets a DM 5 minutes before the start, and one if the
  party is cancelled. New parties ping the Watch party pings role.
- `/watchparty sync:True`: everyone watches in their own Otakase, following
  the host's play, pause, seeks and episode changes (Join in Otakase button).
- Voice rooms get a controls panel in their chat: rename, lock, limit, hide,
  kick or hand the room over. When the owner leaves, the room goes to whoever
  has been there longest.

### Community

- Spoiler guard: naming an airing show with a spoiler outside ||spoiler
  tags|| gets a short reminder; `/spoiler-guard` turns it off or hides such
  messages instead.
- `/guess` shows a pixelated anime cover; the first right guess wins a point
  (`/guess-top`).
- A roles card (`/post-roles`) lets members pick their system, release pings,
  Tester and Watch party pings.
- Quiet levels: XP for chatting, `/rank` and `/levels`, no level-up messages.

### The Otakase app

- Link the app with `/link-app` (`/linked-apps`, `/unlink-app`). Linked apps
  count watch time (`/watch-time`, shown on `/profile`), can share what you
  watch (`/whos-watching`), give the Tester role on development builds,
  report problems straight to the support forum, and show the latest
  announcement and online count.

### Moderation

- Deleted and edited messages are logged in mod-log (else mod-chat).
- `/verification`: new members only see a verify channel until they pass a
  short check. Auto (the default) turns it on during a raid.
- Warnings expire after 90 days (add-on option `warning_days`).
- Members can appeal a warning or timeout once from the DM.

### Staff tools

- `/bot-status`, `/settings` (change the airing board, its hour, the welcome
  DM and question matching without a restart) and `/preview`.
- A dashboard in the Home Assistant sidebar (Otakase Bot panel).

## 1.13.0

- The welcome DM new members get has the welcome picture on top and a card
  with the first steps (install, rules and roles, help, `/follow`), plus
  Website, Download and Wiki buttons.
- New episode DMs from `/follow` are cards with the show's cover, the
  episode (marked first episode or finale), when the next one airs, AniList
  and MyAnimeList buttons and a Stop DMs for this show button that works
  right in the DM.

## 1.12.3

- More space on the airing board between the picture and the hours, and
  between the hours and the menu.

## 1.12.2

- Delete the airing board in Discord and the bot posts today's board again
  within 5 minutes (also when it was deleted while the bot was offline).

## 1.12.1

- The airing board is one card again: the picture on top, then the hours in
  one column with a line between them, the menu and My shows today at the
  bottom. Aired hours are small and crossed out. Smaller gaps, and long
  titles no longer break onto a second line.

## 1.12.0

- The daily airing post is now a live board: a picture with the day's most
  popular show and six more covers, then the 20 most popular episodes
  grouped by hour in two columns. Through the day the bot updates it: aired
  hours are crossed out, the current hour says airing now and the next one
  up next with a countdown.
- Yesterday's board is deleted when today's goes up, so the channel only
  shows today.
- Pick a show in the menu under the board for its card (only you see it):
  score, genres, synopsis, and buttons for a DM when it airs, a watch party,
  AniList and MyAnimeList.
- **My shows today** lists today's episodes from your linked AniList.

## 1.11.1

- `/profile` card is landscape and larger, and easier to read on phones.
- It now comes in one card with a Shu side bar: the picture, a small line
  with what they last watched and when, and the Profile / Anime list buttons.
  No member mention or other text above it.

## 1.11.0

- `/profile` is now a picture card: avatar, score gauge, stats, what the
  member is watching with cover art and progress, and favourite genres, with
  Profile and Anime list buttons. Their AniList banner sits at the top (a
  blurred cover of a show they watch when they have none).

## 1.10.2

- `/link-anilist` works again with an AniList app: AniList shows a code after
  Approve, members paste it and are linked. Needs the new `anilist_secret`
  option (the app's client secret) next to `anilist_client_id`.
- `/link-github` says when linking isn't set up (Device Flow off or an unknown
  client ID) instead of "GitHub didn't answer", and logs GitHub's exact error.

## 1.10.1

- `/provider-status` replies only to the member who ran it. The automatic
  "is it down?" answer stays public.

## 1.10.0

- Version check: support posts on an old otakase version get the update
  command and a changelog link.
- Log reader: pasted or attached logs in support posts get the matching fixes.
- `/follow`, `/following`, `/unfollow`: DMs when a followed show airs a new
  episode.
- Health alerts in bot-log when a background job fails or the bot restarts
  after a crash.
- `/link-anilist`, `/unlink-anilist` and `/profile`: what a member watches
  and their stats, with an optional "AniList linked" role.
- Support posts link up to three matching wiki sections.

## 1.9.1

- The bot's status text shows in the member list (rotating custom statuses).

## 1.9.0

- `/post-welcome` reposts the welcome message with the current channels.

## 1.8.0

- `/watchparty` suggests anime titles while typing.
- Cancelling a watch party no longer times out.

## 1.7.0

- Watch parties get their own category, a member limit and voice options.

## 1.6.0

- `/suggest` posts suggestions with vote buttons and a thread;
  `/suggestion-status` for staff.

## 1.5.3

- Recognises more casual problem reports.

## 1.5.2

- "My list didn't update" gets the sync answer.

## 1.5.1

- Checks more problem reports by meaning.

## 1.5.0

- Finds channels with kanji names again.
- Empty voice rooms are cleaned up; watch parties make their own channel.

## 1.4.0

- Moderation tools: report to mods, `/warn` and `/warnings`, raid guard,
  new-account link block, scam filter, `/purge`, `/slowmode`.
- Voice rooms: join "Create room" or use `/room`.

## 1.3.0

- Answers "is it down?" with a live source check.
- Optional question matching by meaning through Ollama.
- Watch parties can be cancelled.

## 1.2.0

- Issue and pull request links, source check, daily airing post, bug reports,
  stale support post nudges, watch parties.

## 1.1.0

- Do Not Disturb with a rotating status.

## 1.0.0

- First version of the Otakase community bot.

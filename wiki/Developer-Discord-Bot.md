# Discord Bot

`bot/` holds the Otakase Discord bot, a small Go program (its own module,
built with [discordgo](https://github.com/bwmarrin/discordgo)) that runs on a
server and stays connected to Discord.

## What it does

- **Slash commands:** `/install` (steps per system, mirroring the README),
  `/faq` (the same answers as the server's FAQ channel), `/latest` (the newest
  GitHub release), `/anime` (an AniList lookup, never adult titles),
  `/link-github` and `/help`.
- **Support forum:** each new post gets a checklist of what to include and a
  **Mark solved** button. The poster or a moderator pressing it adds the
  Solved tag and closes the post.
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
  anime channel gets the 20 most popular Japanese episodes airing in the next
  24 hours from AniList, never adult titles, with times shown in each
  reader's own time zone. `OTAKASE_DAILY_AIRING=false` turns it off.
- **Report on GitHub:** the support checklist has a button, and `/bug` does the
  same inside a post: it opens a new GitHub issue pre-filled with the post's
  title and question.
- **Stale posts:** a support post quiet for a week gets a nudge to its poster;
  with no reply three days later it is closed. Solved posts are skipped.
- **/watchparty:** makes a voice channel for the party and a server event in
  it with the show's AniList banner, starting in the given minutes. Options
  set a member limit and who can talk and screen share: everyone talks and
  only the host shares (default), everyone talks and shares, or only the host
  does both. Party channels go in a **WatchParty** category, made with the
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
- **Status:** Do Not Disturb, cycling every two minutes through "Watching
  anime · /help", "Playing Otakase <latest release>", the website and
  "Listening to your terminal" (`presence.go`). Bots can't show rich presence
  images or buttons.

The text it sends lives in `content.go`; keep it in step with the README and
the FAQ channel.

## Discord application settings

In the developer portal, under **Bot**, turn on **Server Members Intent** and
**Message Content Intent**; the bot exits at login without them. It needs the
Bot role in the server, with permission to send messages, manage messages
(spam removal), manage threads (closing solved posts), moderate members
(timeouts), manage roles (Contributor, placed above that role), create
events (`/watchparty`), manage channels (`/slowmode`, voice rooms) and move
members (voice rooms). Manage Server is optional: it lets the raid guard pause
invites.

Warnings, suggestions and the list of voice rooms are kept in `otakase-bot.json` in
`OTAKASE_DATA_DIR` (the add-on's `/data`, `/var/lib/otakase-bot` with the
systemd unit, else the working directory).

## GitHub linking

`/link-github` appears only when `OTAKASE_GITHUB_CLIENT_ID` is set: the client
ID of a GitHub OAuth app (github.com → Settings → Developer settings → OAuth
Apps → New) with **Enable Device Flow** ticked. The homepage and callback URL
can be the website; neither is used.

## Hosting

### Home Assistant

The repo is also a Home Assistant add-on repository (`repository.yaml`, with
the add-on in `bot/`).

1. In Home Assistant open **Settings → Add-ons → Add-on Store**, then
   **⋮ → Repositories**, and add `https://github.com/TheXykril/otakase`.
2. Install **Otakase Bot**. Home Assistant builds it on the device, which
   takes a few minutes.
3. On the **Configuration** tab paste the token, save, then **Start** it on
   the **Info** tab with **Start on boot** on. The **Log** tab shows its output.

Updates show up in Home Assistant when `version` in `bot/config.yaml` goes up.

### Ollama on Home Assistant (optional)

For matching questions by meaning, add the repository
`https://github.com/SirUli/homeassistant-ollama-addon` in the Add-on Store,
install **Ollama** and start it. The bot's default `ollama_url`
(`http://76e18fb5-ollama:11434`) points at it, and the bot pulls
`all-minilm` (about 45 MB) on its own; the log says "semantic matching on"
once it is ready.

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

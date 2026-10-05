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
- **/watchparty:** creates a server event in a voice channel (the lounge by
  default) with the show's AniList banner, starting in the given minutes.
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
(timeouts), manage roles (Contributor, placed above that role) and create
events (`/watchparty`).

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

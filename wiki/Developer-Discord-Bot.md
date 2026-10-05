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

The text it sends lives in `content.go`; keep it in step with the README and
the FAQ channel.

## Discord application settings

In the developer portal, under **Bot**, turn on **Server Members Intent** and
**Message Content Intent**; the bot exits at login without them. It needs the
Bot role in the server, with permission to send messages, manage messages
(spam removal), manage threads (closing solved posts), moderate members
(timeouts) and manage roles (Contributor, placed above that role).

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

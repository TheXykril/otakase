# App Link

How the Otakase app talks to the Discord bot: linking, heartbeats, news,
problem reports and synced watch parties. The bot side is in `bot/`
(`publicapi.go`, `applink.go`, `appheartbeat.go`, `appinbox.go`, `appnews.go`,
`appreport.go`, `partysync.go` and the `app*.go`, `party*.go` and `watch*.go`
files beside them); the app side in `internal/discord_*.go` and
`internal/partysync*.go`.

## Reaching the bot

The bot runs at home, so the app reaches it through a Cloudflare Tunnel to
the bot's public API (port 8100, see [Discord Bot](Developer-Discord-Bot)).
The app's `DiscordBotURL` option is the base URL, default
`https://bot.xyverion.com`; empty turns every Discord feature off.
`GET /v1/ping` answers `{"ok":true,"version":"…"}`.

Bodies are JSON. Requests that need a linked app carry
`Authorization: Bearer <token>`. Errors are a non-2xx status with
`{"error":"…"}`; a 401 means the token was revoked, and the app forgets it.
Every network call runs in the background with a 5 s timeout (20 s for a
report upload) and never blocks playback.

## Linking

1. `POST /v1/link/start` → `{"code":"ABCD-2345","poll":"<secret>","expires_in":600}`
   (10 per hour per IP; codes skip 0/O/1/I).
2. The app shows "In the Otakase Discord, run /link-app code:ABCD-2345" and
   polls `POST /v1/link/poll {"poll":"…"}` every 3 s: `202` while waiting,
   `200 {"token","discord_name"}` once, `410` when expired.
3. `/link-app code:` in Discord binds the code to the member.

The bot stores only the token's SHA-256. `POST /v1/unlink` revokes the
calling token, `/unlink-app` all of a member's, `/linked-apps` lists them
with per-app buttons. A member keeps at most 10 apps. The app keeps the
token in `discord_link.json` (mode 0600) in its storage directory.

## Heartbeat

`POST /v1/heartbeat`, 4 per minute per token:

```json
{"version":"26.8.0","dev_build":false,"os":"linux","share_watching":true,
 "watching":{"anilist_id":154587,"title":"Frieren","episode":12,"episodes":28},
 "watched_seconds":57}
```

Sent at launch, every 60 s while an episode plays (mpv or cast), on episode
change and when playback stops (`watching: null`). `watching` is sent only
with `DiscordShareWatching` on. `watched_seconds` is playback time since the
last heartbeat, clamped to 120 by the bot.

The bot updates the app's last seen, keeps a now-watching entry for 3 min
(`/whos-watching`), adds watch time (`/watch-time`, the `/profile` card) and
gives the Tester role once for a development build.

The answer is `{"ok":true,"discord_name":"…","inbox":2}`, `inbox` being the
number of items waiting for this app.

Features listen to heartbeats with `onHeartbeat` (`appheartbeat.go`). What
plays reaches them only with sharing on; watch time always does. From them:
watch badges and streaks (`/badges`, roles by name, game points), the year
wrap (`/wrap`), the server club's caught-up list, the weekly most watched
shows (`/server-watch-top`), today's featured anime bonus (`/featured`), the
update notice for an app older than the latest release (once per release:
one DM, one inbox notice) and the linked role's hours.

## Inbox

The way back from the bot to the app (`appinbox.go`, `internal/discord_inbox.go`).
Each linked app of a member gets its own copy of an item, kept until it acks
it (at most 50, 30 days unless the item says sooner).

- `GET /v1/inbox?wait=25` (auth, 30 per minute) → `{"items":[…]}`. While
  empty the bot holds the request up to `wait` seconds (max 25) and answers
  as soon as something arrives, so the app keeps one request open for as long
  as it runs.
- `POST /v1/inbox/ack {"ids":["…"]}` (auth) → `{}`.

An item: `{"id","kind","at","expires","title","text","url","anilist_id",
"episode","from","key","value"}`, fields by kind. The app acks every item,
also kinds it doesn't know (an older app), since the bot would otherwise send
them again at once.

| Kind | From | What the app does |
|---|---|---|
| `queue` | Queue in Otakase buttons, `/share-anime` (`from`, note in `text`), club Join | Kept in Community → From Discord; picking it plays the show |
| `airing` | Followed shows (`follow.go`), expires in 3 days | Notification (with a Play action where notify-send has them), kept as a playable entry at that episode |
| `notice` | Replies to the member's support post, a reported bug fixed in a release, update notices, party reminders, Ask to party | Notification, kept in From Discord; picking it opens `url` |
| `remote` | `/remote`, expires in 30 s; `key` is `pause`, `play`, `skip`, `next`, `previous` or `stop` | Applied to the mpv this app plays in, over IPC |
| `setting` | The Tester role given or taken (`/beta`) | Only `DevBuilds`, with `value` `true`/`false` |

## More from the app

All need the token unless said.

- `POST /v1/provider-report {"provider","ok","kind":"search|lookup|episode|playback","error"}`
  → `{}`, 60 per hour per member. A failure once per source per run (again
  only after a success, up to 3), a success at most hourly; the error has
  quotes and URLs removed. Kept in memory for 3 hours; `/provider-status`
  and the automatic "is it down?" answer count members per source, no names.
- `POST /v1/backup {"config":"Key=value\n…"}` → `{"at"}` (≤64 KB, 10 per
  hour), `GET /v1/backup` → `{"config","at","from_os"}` or 404, `DELETE
  /v1/backup`. Per member, so a new computer can restore after linking.
  Secret-looking keys (token, secret, password, apikey, clientid) and
  `StoragePath` are left out on both sides. Community → Back up / Restore
  settings; `/settings-backup` shows or deletes it.
- `POST /v1/rating {"anilist_id","score","text"}` → `{"url"}`. Score out of
  10 (above 10 is read as out of 100). Posts a card with Play and Queue
  buttons in the reviews channel; 403 adult, 409 already shared today, 503 no
  reviews channel. The app offers it after scoring the last episode
  (`DiscordShareRatings`).
- Crash reports and "where to watch X?" posts go through `/v1/report`. The
  app notes a run that didn't end cleanly (a marker per process in
  `crash/`, fatal panics written there by `debug.SetCrashOutput`) and asks
  once on the next launch.
- `GET /v1/club` (no token, 30 per minute per IP) →
  `{"club":{"anilist_id","title","episode","deadline","caught_up","joined","url"}}`
  or `{"club":null}`; cached by the app for 3 h for its Community row.
- `GET /v1/linked-role` and `/v1/linked-role/callback` are browser pages for
  the linked role sign-in (`linkedrole.go`).

## News

`GET /v1/news` (no token, cached 5 min):
`{"announcement":{"title","text","url","at"}|null,"online":12,"members":85,"invite":"…","score_sharing":true}`.
`score_sharing` is `/settings score_sharing`; off, apps don't offer to share
scores and `/v1/rating` answers 503.
The app fetches it at most every 6 h and shows it in the Community menu and
the update prompt.

## Reports

`POST /v1/report`, multipart: `title` (≤100), `description` (≤4000), `diag`
(≤8000, the `otakase -diag` block) and a `log` file (≤1 MB of text); 3 per
hour per member. The bot makes a support post that mentions the member,
with the log attached, and runs the usual greeting, log reader and version
check on it. Without a link the app copies the report and opens the support
forum instead.

## Synced watch parties

`/watchparty sync:True` adds a **Join in Otakase** button to
`otakase.xyverion.com/open/?party=<event id>`, which opens
`otakase://party/<event id>` (or `otakase -party <id|link>`, or Community →
Join a watch party).

WebSocket `GET /v1/party/{id}/ws` (token in the header, or `?token=`). The
party's creator is the host. One JSON message per frame, at most 4 KB and
10 per second; up to 25 members.

- bot → app: `{"type":"welcome","host":bool,"show":"…","anilist_id":N,"server_at":ms}` first.
- host → bot: `{"type":"state","anilist_id":N,"episode":12,"playing":true,"position":312.4,"sent_at":ms}`
  on any change and every 5 s. Only the host's state is relayed; late
  joiners get the last one at once.
- member → bot: `{"type":"hello","version":"…"}`, `{"type":"ready","episode":12}`.
- either → bot: `{"type":"ping","t":ms}` → `{"type":"pong","t":…,"server_at":ms}`.
  `sent_at` is in the bot's clock, estimated from the shortest ping round trip.
- bot → members: `{"type":"members","list":[{"name","ready"}]}`, `{"type":"ended"}`.
- app → bot: `{"type":"vote","what":"skip"|"next"}` (Alt+k, Alt+n in mpv).
  bot → all: `{"type":"votes","what","votes","needed"}`, and
  `{"type":"voted","what"}` once more than half of the connected members
  (host included) agree; the host's app skips the opening or ending (else 85
  s) or goes to the next episode. Votes last 90 s and reset with the episode.
- bot → all: `{"type":"react","emoji","name"}` and `{"type":"chat","name","text"}`
  from the party's voice channel chat (emoji-only messages and reactions are
  reactions), at most 5 per second. The app draws them with `osd-overlay`;
  Alt+c hides the chat.
- host → bot: `{"type":"end"}` ends a Watch with me party.

Members open the same show and episode through their own source and follow
the host through mpv's IPC: seek when more than 2 s off, pause and play with
the host, switch episodes with them. The hub is memory only; after a bot
restart the apps reconnect with backoff.

Watch with me (`DiscordWatchWithMe`): `POST /v1/party/start
{"anilist_id","episode","title"}` → `{"id","join_url"}` makes an ad-hoc
party, in memory only, one per member, with the app as host; the join link
becomes a Watch with me button in the Discord status. It ends 2 minutes after
the host's app leaves. `/whos-watching` has Open episode and Ask to party
buttons per member; asking gives the join link at once when they run Watch
with me, else asks them by DM and inbox.

`GET /v1/parties` lists planned parties, soonest first:
`{"parties":[{"id","title","start","anilist_id","sync","host","going","rsvp","live","join_url","event_url"}]}`;
`POST /v1/party/{id}/rsvp {"going":true}` → `{"going","count"}`. Going
members' apps get a notice when the party's reminder goes out.

## otakase:// links

`otakase://play/anilist/<id>?ep=<n>` opens a show (or that episode);
`otakase://party/<event id>` joins a party. Linux registers the scheme via
its own hidden desktop entry, `otakase-link.desktop` (`otakase -rofi %u`,
`Terminal=false`), so a link opens in the rofi menus without a terminal
window; the app sets it as the default with `xdg-mime` at launch when it is
not already. Beside a package install it removes user copies under
`~/.local/share/applications` that shadow the package's, among them an older
`otakase.desktop` still claiming the scheme, which made links offer two apps. With `LinkMenus=terminal`, or without rofi installed, such a
launch reopens itself in a terminal with `-no-rofi` (`PrepareLinkLaunch`). Windows registers via the
installer and a self-check at launch; macOS has no app bundle, so no handler.

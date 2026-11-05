# Troubleshooting

## Empty results and Retry

An empty result means the catalog answered but found no matches. Try another
spelling or clear the search filters. A failed request shows an error and
**Retry** instead. Anime search keeps the query and filters; if **Load more**
fails, Retry keeps the cards already loaded and asks for that same page again.
Schedule retries also keep the selected day and the airing times already loaded.

Movies and shows offer Retry for search, catalog and episode-list failures.
Available movie lists stay visible when another list fails. A successfully
loaded but empty catalog or episode list is shown as empty, rather than as a
connection failure.

When Play or Download is unavailable, the nearby explanation says whether the
anime has not aired, episode information is loading, the request failed, or no
aired episodes are available. Disabled actions also have an accessible hint and
a mouse tooltip.

## Nothing is found for a show, or playback never starts

Otakase keeps trying until the episode plays and remembers what worked for
each show. When nothing works, the problem is usually on the other end and
passes on its own, so try again a little later. **Diagnostics** (in More or
Settings, under **Help & app**) shows what went wrong.

On a playback error, choose **Report playback problem**. The report fills in
what you were watching and shows optional app details, source, position and
the error for review. **Attach app details** controls whether those details
are included. **Send report** posts to support through your linked Discord
profile; **Copy report** also works without Discord. Nothing is sent until
you choose Send report.

## Progress didn't update on AniList / MyAnimeList

Writes to the trackers are paced to stay inside their limits and happen in the
background, so they can take a moment. Check that the account is still signed
in under Accounts: a tracker that rejects the sign-in shows it there, and
signing in again can restore access.

The Accounts card shows when sync needs attention. Inside Accounts, AniList
and MAL show updates being sent, failures, and the last update sent this
session. Trakt also shows local playback progress awaiting upload. Signing
in alone does not mean an update was sent. Completion updates remain queued
when a tracker is unavailable and retry automatically.

## The TV can't finish signing in

Phone and TV need to be on the same home network, and guest Wi-Fi often keeps
devices apart. Check that the code on the phone matches the one on the TV.

## The desktop app doesn't open on macOS

It is not signed by Apple yet. Right-click Otakase in Applications and choose
**Open** the first time.

---

Still stuck? Ask on [Discord](https://otakase.xyverion.com/discord/) or
[open an issue](https://github.com/TheXykril/otakase/issues).

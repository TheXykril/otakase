# Troubleshooting

## Nothing is found for a show, or playback never starts

Otakase tries the sources one after another and remembers which one worked
for each show. Streaming sites go down or change all the time, so when nothing
works it is usually them. **Diagnostics** (in More or Settings) shows which
sources answer right now; the Otakase Discord bot's `/provider-status` shows
the same from its side.

When no site has an episode, Otakase can play it from a torrent while it
downloads. That needs **Torrent sources** on under Settings > Playback, and
**Torrents on mobile data** if you are not on Wi-Fi.

## Progress didn't update on AniList / MyAnimeList

Writes to the trackers are paced to stay inside their limits and happen in the
background, so they can take a moment. Check that the account is still signed
in under Accounts: a tracker that rejects the sign-in shows it there, and
signing in again fixes it.

## The TV can't finish signing in

Phone and TV need to be on the same home network, and guest Wi-Fi often keeps
devices apart. Check that the code on the phone matches the one on the TV.

## The desktop app doesn't open on macOS

It is not signed by Apple yet. Right-click Otakase in Applications and choose
**Open** the first time.

---

Still stuck? Ask on [Discord](https://otakase.xyverion.com/discord/) or
[open an issue](https://github.com/TheXykril/otakase/issues).

# Otakase for Android

One app for Android phones and Android TV, running Android 7.0 or newer.

Home holds your saved episodes and personal lists. Search browses anime. More holds Accounts,
Schedule, playback settings, subtitles and diagnostics. Touch and remote controls share the
same app. Back closes menus before leaving a page.

On TV, Up from the top of a page returns to the Home, Search and More tabs.
Left and Right move between tabs, OK opens a tab, and Down returns to its page.
Back from a root page first focuses its tab.
On Home's tab, Back opens the exit prompt with Exit selected. Press OK to leave,
or choose Stay or press Back to cancel. TV has no second farewell prompt.

More > Profiles manages up to six profiles. Each keeps its own accounts, history, lists and
playback settings. Switch profiles there without restarting. TV asks who is watching when
more than one profile exists. Change Ask on start in Profiles. A direction code uses 4–8
arrows, entered twice when set. Use the D-pad, arrow buttons or phone swipes to unlock.
Five wrong codes pause attempts for 30 seconds. Existing data moves into Profile 1.

Sign in through More > Accounts to sync AniList or MyAnimeList progress, status, scores and
resume positions. With both signed in, Home combines your lists and updates both trackers.
Shared titles use the most recently updated entry and one category picker. Background list
sync runs at most every 30 minutes, or when you refresh Home. Manage the switch and status
through More > Accounts. AniList-private entries stay off MAL. On TV, scan the QR with your phone
and keep both devices on the same home network. Log out of each account separately in Accounts.

Watched episodes and resume positions stay on this device until you sign in. The 18+
preference follows your AniList setting.

Caught-up airing shows move from Continue to Up next, with the next episode’s countdown.
More > Discord links each profile with the bot’s `/link-app` command. It receives queued
shows and remote commands, reports source health, and sends watch time. Sharing title
details is off by default. Finished scores ask before sharing. More > Time capsule keeps
a letter and stats for April 12, with optional community messages through Discord. On TV,
Type on phone opens a temporary page on your local network. Episode milestones and rare
exit greetings use the same thresholds and badge state as the desktop app.

More > Watch rooms lists rooms your linked Discord account can join. The player follows
the host, handles ready checks, and lets viewers ask for a pause, jump or next episode.
The host accepts requests in the player. Room tracking uses the bot's linked trackers.
This requires the bot from this branch, which accepts linked apps on its existing watch
routes. Private rooms still require presence in their Discord voice channel and the
profile's AniList setting must allow them.

On TV, More > Phone remote shows a QR and a ten-minute pairing code. On your phone,
More > Control TV finds TVs on the same local network. Pair once, then control play,
pause, ten-second seeks, next episode, episode selection and volume. Pairings belong
to each profile. Forget a TV on the phone, or forget all paired phones on the TV.

Playback chooses a working copy automatically. Change Sub/Dub before playing and choose
actual audio or subtitle tracks through Tracks. The red episode thread follows your saved
position and continues through the played part of the timeline. Even out volume (on by
default, in Playback settings) raises quiet dialogue and softens loud effects.

[Developer build and testing notes](../wiki/Developer-Android-App.md)

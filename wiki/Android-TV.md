# Android TV

The same APK runs on phones and on Android TV (Android 7.0 or newer). On a TV
it switches to a layout made for the remote.

## Moving around

- **Left/Right** on the top bar switch between Home, Search and More; **Down**
  or **OK** enters the page, and **Up** from the top of a page goes back to the
  bar.
- Left and Right stay inside each cover row and stop at either end.
  Anime episodes appear directly on the details page in a four-column grid,
  with the same thumbnail cards as desktop. Left/Right moves across a row;
  Up/Down moves between rows and scrolls further episodes into view.
  **Back** from a show returns to the same cover.
- Search keeps the keyboard closed while you move around. Press **OK** on the
  search field to type, then submit to reach the filters and results.
- **Back** on Home asks whether to leave, with Exit already selected.
- Play, pause, seek and next buttons on the remote work in the player.

Menus scale to your screen by themselves, and Android's larger text setting
still applies.

## Finding an episode

Anime details include **Go to episode**, with current/latest shortcuts and a
keypad. While this dialog is open, the remote's number buttons also enter an
episode number. Confirming selects that card; it does not start playback.
Otakase consumes the number presses it receives in this dialog. Some TVs reserve
these buttons before apps receive them, so this depends on the TV and remote.

The selected card's title appears in a reserved line above the grid.
**Episode details** (or the remote's Info button on a card) opens its full title,
airdate, duration, saved resume point and a synopsis when the source supplies one.
Back returns to the same card. Pressing OK on a card still starts playback.

**More > Playback > Hide episode spoilers** is off by default. When enabled,
unwatched episode titles are hidden, and covers are blurred with a crossed-out
eye. Use **Reveal this episode** in its details to show that episode for this
visit; revealing it does not mark it watched. Watched episodes stay visible.
The setting belongs to the current profile and also applies on phone and desktop.

## Signing in

Typing a password with a remote is no fun, so the TV shows a QR code and a
short check code. Scan it with your phone, check that the code matches, and
sign in there. Phone and TV must be on the same home network.

**Type on phone** opens a temporary page on your network for typing longer
text.

## Your phone as a remote

On the phone, More > Control TV finds TVs and computers running Otakase on the
same network. Tap one and the TV asks **Allow** over whatever is on screen,
showing two numbers that the phone shows too, so you can tell your phone from
someone else's. Pick Allow and the phone is paired. Not now refuses; after
three refusals in a row phones wait 30 seconds before asking again.

If the phone finds nothing, open More > Phone remote on the TV. It shows a QR
code and a pairing code that lasts ten minutes, with the TV's address under
it: scan the QR code with the phone camera, or tap Enter code on the phone and
type the code (and the address if needed). A computer works the same way from
More > Phone remote. If its firewall is on (Linux or Windows), that page
shows **Allow phone remote**: tap it and enter your password once. Otherwise
allow port 8654 (TCP and UDP) yourself.
Pair once and the phone opens the remote for that TV: cover art, a seek bar
with the opening and ending marked, Skip Intro when it shows on the TV,
ten-second jumps, previous and next episode, volume, and buttons for
episodes, subtitles, audio, quality and stop. While the TV plays, a bar above
the phone's tabs shows what is on and opens the remote again. The remote's
menu disconnects or forgets the TV.

You can also start from the phone. On a show's page, **Play on…** lists your
paired TVs and computers, this phone, and cast devices (Chromecast, DLNA,
Kodi, any browser) in one place. Pick a TV and the episode starts there while
the phone becomes its remote. While watching on the phone, the player's cast
button offers the same list: pick a paired TV and the episode moves there at
the same spot. Films and series still go to TVs through casting. The phone stays connected and shows what the TV is
doing as it happens; if the connection drops it reconnects on its own and
finds the TV again when its address changed, for example after a router
restart. Pairings belong to each profile; forget a TV on the
phone, or every paired phone on the TV.

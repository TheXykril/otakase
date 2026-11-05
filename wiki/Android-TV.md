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

On the TV, More > Phone remote shows a QR code and a pairing code that lasts
ten minutes, with the TV's address under it. On the phone, More > Control TV
finds TVs on the same network; scan the QR code with the phone camera, pick
the TV, or tap Enter code and type the code. If the phone finds nothing, also
type the address shown under the code. A computer works the same way from
More > Phone remote; if its firewall is on, allow port 8654 (TCP and UDP).
Pair once, then control play, pause, ten-second seeks, the next episode,
episode choice and volume. Pairings belong to each profile; forget a TV on the
phone, or every paired phone on the TV.

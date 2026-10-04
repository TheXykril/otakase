# Website

The project site lives in `site/` and is published to GitHub Pages by
`.github/workflows/site.yml` whenever `site/` changes on `main`. It is plain
HTML, CSS and JavaScript with no build step: open `site/index.html` through any
local web server (`python3 -m http.server -d site`) to preview it.

## Look

- Colours are the Shu palette: ink `#0e0d0c`, cream
  `#ece4d6`, vermilion `#d2492f`, grey `#8a8276`.
- The hero shows the kanji 任 (from お任せ, "leave it to us") bleeding off the
  left edge, with anime cover art showing through the letters.
- Fonts come from Google Fonts. The Japanese font is requested with `text=`
  so only the few kanji the page uses are downloaded; add any new kanji to
  that URL in `index.html`.

## Cover art

`app.js` asks AniList for the 15 trending anime (`isAdult: false`) and fades
from cover to cover every 5 seconds. The list is cached in `localStorage` for
6 hours. If AniList can't be reached, the letters show `img/scene-*.jpg`, the
same drawn sunset the player screenshots use.

## Screenshots

The `player-*.jpg` and `menu-*.jpg` images are real otakase in the default
Shu colours: mpv with otakase's skin playing a still drawing (no show's
footage), the terminal menu in kitty and the rofi poster grid with real AniList
covers, plus `os-*.jpg`, the terminal menu framed as macOS Terminal and Windows
Terminal. Retake them when the player or menus change look, and
keep source names out of the frame. Take every image at twice the size it is
shown (mpv in a 2560x1440 window, terminals at double font size, rofi at 192
dpi with its px sizes doubled) so it stays sharp on high-DPI screens.

## Icon

`site/img/icon.svg` is the app icon (任 with a dark red shadow over お任せ and
OTAKASE) and `site/img/favicon.svg` the small version with 任 alone. The text
is outlined to paths, so neither needs a font. The PNGs beside them are
renders of those two files.

## Rules

The site never names a stream source. The
footer must keep saying that otakase does not host, upload or store any video.
Screenshots of the player are cropped so the top bar, which names the source,
is not shown.

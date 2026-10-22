# Casting

`otakase -cast` plays the episode on a Chromecast, a DLNA TV or Kodi on the
same network. Most smart TVs are DLNA renderers (LG, Samsung, Sony, Hisense,
Philips); they are listed next to Chromecasts, marked `· DLNA`. A TV that is
both can show up twice, and the Chromecast entry is the one with the better
controls. A DLNA TV is sent one plain MPEG-TS stream, so it needs nothing
installed; if it does not appear, check that the TV's "media renderer" or
"DLNA" setting is on and that your firewall lets its answers back in. The
first time, most TVs ask on screen whether to allow this device: otakase
waits up to two minutes for you to accept it there.

**Behind a firewall** (ufw, firewalld) the TV looks absent, because its
answers are dropped. Run `otakase -cast-setup` once: it fixes the cast ports,
shows the firewall rules for your network, runs them with `sudo` if you say
yes, then lists the devices it finds. When a search finds nothing with a
firewall on, otakase offers **Fix the firewall now** right there too. The
rules to type by hand are on the
[Casting Problems](Casting-Problems) page.

**Kodi** (on its own, or on an Android TV box, Fire TV or Raspberry Pi) is
listed marked `· Kodi` once *Settings > Services > Control > Allow remote
control via HTTP* is on. If it does not show up, set `KodiHost` to its address
(`192.168.1.20` or `192.168.1.20:8080`); if its web server has a password, set
`KodiUser` and `KodiPassword` to match. The menu
also carries a **Cast** toggle (`[ ] Cast: Off` / `[x] Cast: Office TV`, `^k`
in the terminal list) that does the same thing per-run. otakase asks which
device to use once per run, even when only one is found; `CastDevice` skips
the question. The device list ends with **Rescan for devices**, for a TV that
was still waking up, and the same entry is offered when nothing was found. Casting works from **Untracked Watching**
too, and like local untracked playback it records nothing.

**Browser** is always last in the device list. Pick it and otakase shows a
link and a QR code: open the link (or scan the code) on anything on the same
network with a web browser, such as a phone, tablet, smart TV, games console
or another computer. The page plays the episode and has its own buttons (back
30s, pause, forward 30s, subtitles, full screen). The first tap or key on the
page goes full screen (browsers allow it no sooner), and a double tap on the
video also goes full screen, and a phone turns sideways there. The page also shows
what the terminal panel says (preparing the stream, seeking, the next-episode
countdown). A TV remote's arrows, OK and
play/pause keys work too. The terminal keys still work, and the page follows
them. The link stays the same between episodes and runs, so a TV can keep the
page open or bookmark it, and the next episode starts there by itself. More
than one device can open the link at once. The link has a short secret code
in it so others on the network can't find the page; the terminal panel's
header keeps showing it during the episode. The page is served on
`CastBrowserPort` (default `8012`); `otakase -cast-setup` allows it through
the firewall along with the other cast ports.
`CastDevice=Browser` skips the device list. `CastBrowser=false` removes the
entry. The browser needs to play H.264 video (Chrome, Edge, Safari, Firefox
and most TV browsers do).

Casting needs `ffmpeg`, same as `-download`. The stream is served from this
computer, so keep otakase running while you watch.

**Subtitles** are burned into the picture, using your GPU when it can. A dub
plays its English audio track and gets no subtitles. A browser gets the
subtitle file instead, as plain text it can turn on and off (`CC`) with no
re-encode, but without the release's fonts and styling;
`CastBrowserSubtitles=burn` burns them in for the browser too. A torrent's own
subtitles are always burned in. `CastBurnSubtitles=false`
turns burning off; `CastEncoder` forces `vaapi` or
`software` if detection picks wrong.

A torrent release keeps its subtitles inside the file, spread through it, so
they are read as the file downloads, starting from where the episode starts
playing (a resume, or a seek). The cast starts once about a minute of them is
in (30 seconds at most), with the release's own fonts, and the stream
restarts where you are a few times early on to add more, until the download
is done. `CastWaitForSubtitles=true` waits for the whole download instead, so
there are no restarts.

**Seeking** moves 30 seconds per press, and a held key waits until you let
go. A seek to a part otakase has already prepared (usually anything behind
you, and a little ahead) is quick, but lands a few seconds either side of the
exact spot. A seek further ahead takes a couple of seconds to restart the
stream; that restart re-encodes the stream even when there are no subtitles to
burn, so the sound and the picture start together. If a restart takes more
than 30 seconds, otakase gives up on it and keeps playing where you were; on a
torrent it waits longer and shows how much has downloaded.

While casting, the terminal shows a control panel: position, device, and
keys (space pauses, arrows seek/adjust volume, `a` switches between sub and dub
from where you are, `q` stops). Anything else
otakase needs to tell you arrives as a desktop notification instead of
interrupting the panel. Next episode, filler skip, and tracker updates all
work the same as local playback.

From the rofi keybind, casting opens its own terminal (`CastTerminal` if the
wrong one opens); closing that window stops the episode and saves position.

## If it does not work

**If the episode never starts**, it's almost always a firewall. otakase
prints the fix and copies it to your clipboard: paste it in a terminal and
cast again. Set `CastPort` to a fixed port so one rule covers every cast.
Still stuck? See [Casting Problems](Casting-Problems).

Every casting setting (`CastDevice`, `CastPort`, `KodiHost` and the rest) is described on [Configuration](Configuration).

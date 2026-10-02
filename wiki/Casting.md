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

Casting needs `ffmpeg`, same as `-download`. The stream is served from this
computer, so keep otakase running while you watch.

**Subtitles** are burned into the picture, using your GPU when it can. A dub
plays its English audio track and gets no subtitles. `CastBurnSubtitles=false`
turns burning off; `CastEncoder` forces `vaapi` or
`software` if detection picks wrong.

**Seeking** takes a couple of seconds to restart the stream, so a press moves
30 seconds and a held key waits until you let go. A seek or a resume
re-encodes the stream even when there are no subtitles to burn, so the sound
and the picture start together.

While casting, the terminal shows a control panel: position, device, and
keys — space pauses, arrows seek/adjust volume, `a` switches between sub and dub
from where you are, `q` stops. Anything else
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

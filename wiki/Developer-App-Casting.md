# App Casting

The phone and desktop apps cast to the same devices as the old command-line
player: Chromecast, DLNA TVs, Kodi, and any browser on the network (the
browser player page with a link and QR code). The work happens in the Go
core; the app only picks a device and shows controls.

## Core operations

Cast operations go through the normal core request (`mobilecore.Core.Start`
on Android, the `otakase-core` helper on desktop) with a `cast` object:

| Operation | `cast` fields | Answer |
| --- | --- | --- |
| `cast_devices` | `timeout` (seconds, default 3) | `devices`: `id`, `name`, `kind`, `address`. The browser entry is always last. |
| `cast_start` | `device`, `url`, `headers`, `hls`, `subtitle`, `start`, `duration`, `title` | Returns at once. Follow the load with `cast_status`. |
| `cast_status` | none | `active`, `device`, `kind`, `state`, `position`, `duration`, `paused`, `volume`, `load`, `mode`, and `note`, `error`, `link`, `viewers` when they apply. |
| `cast_control` | `action` (`pause`, `play`, `seek`, `volume`, `stop`), `value` | `ok` |
| `cast_log` | none | The last 200 lines the cast code logged. |

`state` is one of `connecting`, `loading`, `buffering`, `playing`, `paused`,
`ended`, `error`, `stopped`. `load` goes up with every `cast_start` and every
rebuilt seek, so the app can tell an old `ended` from a new one. Starting on
the device that is already casting reuses the connection (next episode);
starting on another device ends the old cast first. Closing the core ends any
cast.

## How the stream reaches the device

The device cannot send the headers a source needs, so it never fetches from
the source directly. There are three paths (`mode` in `cast_status`):

- `relay` (default, no ffmpeg needed): `internal/cast/relay.go` serves the
  source on the LAN address and rewrites HLS playlists so every playlist,
  segment and key goes through it. Segments dressed up as images are cut back
  to the transport stream. The device plays the source as it is, so it can
  seek on its own. Whole files (torrents, mp4) pass through with Range
  support. Used for Chromecast, Kodi and the browser, and for DLNA with whole
  files.
- `progressive`: for a DLNA TV with an HLS source and no ffmpeg. The relay
  joins the best variant's segments into one transport stream, decrypting
  AES-128 segments itself. A seek rebuilds the joined stream from the target
  segment. A source that keeps its sound in a separate playlist plays without
  sound this way.
- `ffmpeg`: when ffmpeg is on the PATH (desktop) and subtitles must be burned
  in (Chromecast, DLNA), for every DLNA HLS cast, and for a Chromecast with a
  non-mp4 file. Same remux and burn arguments as the command-line player;
  a seek restarts ffmpeg from the target.

Subtitles: the browser page and Kodi get a WebVTT file from the relay
(converted from SRT or ASS in `mobilecore/castsubs.go`, styling dropped).
Chromecast and DLNA need ffmpeg for subtitles; without it the cast plays
without them and `note` says so.

## Security

The relay binds only the LAN address, every path carries a random 128-bit
token, and every address a rewritten playlist names is signed with HMAC-SHA256
under a per-run key. The relay fetches only what a playlist it rewrote
pointed at, so nobody on the network can use it as a proxy. Starting a new
source makes the old source's addresses stop working.

## Android

Discovery needs a Wi-Fi multicast lock; the app holds it only while the device
picker searches. ffmpeg does not exist there, so Android casts always use the
relay. Chromecast discovery uses mDNS, which needs the network interface list;
some Android versions refuse it to apps, in which case only DLNA and the
browser show up. Untested on a real device so far.

## Tests

- `internal/cast/relay_test.go`: rewriting, signatures, image-dressed
  segments, Range, AES-128 joined stream.
- `mobilecore/cast_session_test.go`: DLNA joined stream with resume, Kodi
  resume seek and end, ffmpeg burn with seek (skipped without ffmpeg).
- `app/test/cast_test.dart`: request shape, device picker, cast panel.

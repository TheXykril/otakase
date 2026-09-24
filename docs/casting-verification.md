# Verifying casting against real hardware

Casting is the one feature in otakase that no automated test can fully
verify. The unit tests cover the pure logic — skip spans, the completion
threshold, device parsing, the server, and `watchCast`'s failure handling
through fakes — but three things need a real device on a real network:
whether the Default Media Receiver accepts what ffmpeg produced, whether it
reports the states the poll loop expects, and whether it can reach this
machine at all.

Run this checklist after any change to `internal/cast/` or
`internal/cast_playback.go`.

**Setup:** a Chromecast (or anything running the Default Media Receiver — a
Google/Nest Hub, an Android TV with casting on) powered on and on the same
subnet as otakase. `ffmpeg` on `PATH`. Then pick an episode as usual and run
`otakase -cast`.

## 1. It works at all

Expected sequence: `Looking for cast devices...` → a device menu (skipped if
only one answers, or if `CastDevice` names one that is found) → `Preparing the
stream for <device>...` → the episode starts within ~30s → `Playing on
<device>.`

- [ ] **Listen to the audio for a full minute.** Not a glance — listen. A
      bitstream filter wrong for this container produced perfectly fine video
      with undecodable audio, while ffmpeg exited 0 and otakase reported a
      healthy cast. `TestRemuxedStreamDecodesCleanly` now guards it, but only
      the device's own decoder settles it.
- [ ] Video plays without stutter or artefacts.
- [ ] **Subtitles appear**, on a stream that has them. They are drawn into the
      picture, so they cannot be turned off from the TV — that is expected.
      `.ass` styling and positioning should look as it does in mpv.
- [ ] **Check which encoder was used**: `grep 'burning subtitles' ` the debug
      log. If it fell back to `libx264`, the GPU probe failed — worth knowing,
      since the CPU cost is much higher on a long film.

- [ ] **The panel is the only thing on the terminal.** No text scrolls past it
      while the episode plays, and anything otakase has to say arrives as a
      desktop notification instead.
- [ ] **Your scrollback survives.** After `q`, the terminal is as it was before
      the cast, with the panel gone rather than left behind.
- [ ] **Resize the window mid-episode.** The panel follows within a second,
      stays centred, and leaves no trail of the old frame.
- [ ] **Every control key works**: space pauses and resumes, left/right seek,
      up/down change the TV's volume, `q` stops and saves the position.
- [ ] **Press right five times quickly.** The episode must move about fifty
      seconds, not ten — each press acts on where the last one left it.
- [ ] **Pause, then seek.** The device resumes on any seek, so the status line
      must go back to PLAYING rather than staying PAUSED.
- [ ] **Pause for three minutes.** The episode must still be there — the stall
      bound is two minutes and must be suspended while paused.

## 2. It tracks correctly

- [ ] Seeking from the TV remote or the Google Home app is picked up within
      about a second.
- [ ] With `SkipOp`/`SkipEd` on, **the opening is jumped automatically** —
      casting resolves its own skip times, because it never reaches the place
      local playback gets them from. If nothing skips, check the debug log for
      a skip lookup line before assuming the show has no times.
- [ ] At `PercentageToMarkComplete` (85% by default), otakase prints
      `Episode N marked as watched.`, `curd_history.txt` gains an entry, and
      the tracker updates if remote tracking is on.
- [ ] **Not marked early.** Check `curd_history.txt` directly, not just the
      terminal line — ideally on a slow provider, where the device can catch up
      to the live edge of a playlist that is still growing.

## 3. It fails honestly

This is where every bug in this feature has lived. Each of these once
reported `Playback finished.` instead of the truth.

- [ ] **Kill ffmpeg mid-episode** (`pkill ffmpeg`). Expect the real reason,
      not a clean finish — and check `curd_history.txt` and your tracker
      afterwards: a truncated episode must **not** be marked watched.
- [ ] **Cast to a device that cannot reach this machine** (guest VLAN, or AP
      client isolation). After 30s expect
      `Casting failed: cast: <device> never started playing -- it may not be
      able to reach this machine on the network`, printed to a real terminal
      you can actually read. Not `Playing on X.` followed by
      `Playback finished.` on a black screen.
- [ ] **Stop the cast from the Google Home app**, and separately cast
      something else to the same device. Expect otakase to notice and say so
      within the two-minute stall bound, rather than polling a frozen status
      forever.
- [ ] **Ctrl+C mid-episode.** The terminal must return immediately, the device
      must stop, `ps aux | grep ffmpeg` must show nothing left, and
      `<StoragePath>/cast-scratch/` must be empty.

## 4. It cleans up

- [ ] **Watch the scratch directory's peak size** during a long episode or a
      film: `du -sh <StoragePath>/cast-scratch/`. `-c copy` downloads far
      faster than playback, so the whole episode lands on disk within minutes.
      Confirm that is acceptable on your storage before a 4 GB film surprises
      you.
- [ ] After a normal finish, the terminal still shows the session's output —
      including `Episode N marked as watched.` — rather than a bare prompt.

## When it does not work

`otakase-debug.log` under the storage path records what the device did. Three
lines answer almost everything:

- `cast: <ip> requested /playlist.m3u8` — the device reached this machine. If
  **no request appears at all**, it could not. The usual cause is a host
  firewall dropping inbound connections; set `CastPort` and allow that one port
  rather than the whole ephemeral range. This failure is indistinguishable from
  a device that never got the load, which is why the request log exists.
- `cast: <ip> requested /seg00000.ts` — it accepted the manifest. Playlist
  requests with no segment requests mean it fetched the manifest and refused
  it.
- `cast: device status: ... player=... idleReason=...` — what the receiver says
  about itself. `media=<nil>` means the receiver is running with no media
  session at all, which is what a stream it cannot decode looks like.

`OTAKASE_CAST_KEEP=1 otakase -cast` keeps the stream directory instead of
deleting it, so the segments can be probed after a failure:

```bash
ffprobe -v error -show_entries stream=codec_name,profile,level,width,height \
    -of default=noprint_wrappers=1 <dir>/seg00000.ts
```

That directory is the whole episode, so delete it when finished -- half a
gigabyte is normal.

Three real failures were found this way, and all three looked identical from
the outside: missing CORS headers (the device fetched the manifest and could
not read it), `.ts` served as a text type from the system mime database, and a
stream declaring H.264 Level 5.0 to a decoder specified for 4.1.

## 5. The rofi handoff

- [ ] **Launch from the Hyprland keybind with casting on.** A terminal opens,
      showing the status line and the control keys.
- [ ] **Every key works there**, the same as in a terminal launch.
- [ ] **Close the window mid-episode**, then check each of these separately —
      a partial pass is not a pass:
  - [ ] the TV stops playing
  - [ ] the position was saved (check `curd_history.txt`)
  - [ ] no ffmpeg process survives
  - [ ] `<StoragePath>/cast-scratch/` is empty
- [ ] **Let an episode complete in the spawned window, then check AniList or
      MyAnimeList actually advanced** — not just `curd_history.txt`. The child
      process signs in separately from the one that launched it, and local
      history is written either way, so the terminal line alone cannot tell you
      remote tracking worked.
- [ ] **Set `CastTerminal` to a command that does not exist.** The episode
      still casts, in the launching process, with a message saying why there is
      no window — a missing terminal must never mean a missing episode.
- [ ] **Check `<StoragePath>/cast-session/` is empty afterwards.** Those files
      hold a stream URL with an authentication token.

## 6. Continuing to the next episode

- [ ] **Let an episode finish.** The panel shows a countdown and the next
      episode starts on its own.
- [ ] **Press a key during the countdown.** It stops, and otakase exits without
      starting the next episode.
- [ ] **Check the tracker advanced for both episodes**, not just the first.
- [ ] **Between episodes**, confirm `<StoragePath>/cast-scratch/` holds one
      episode at a time rather than accumulating.
- [ ] **Press `q` during an episode.** The next one must not start — stopping
      is not finishing.

## Known limits

Not bugs; these are deliberate, and documented in the README.

- Casting always starts from the beginning. It tells you when you had a resume
  position, and it writes your position back on exit so local playback can
  resume from it later.
- Soft subtitles cannot be cast; the Default Media Receiver renders WebVTT
  only.

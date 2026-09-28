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
      desktop notification instead. Between episodes too: the panel is held for
      the whole cast, so there is no gap where plain text gets through.
- [ ] **Your scrollback survives.** After `q`, the terminal is as it was before
      the cast, with the panel gone rather than left behind.
- [ ] **The panel does not blink between episodes.** It is taken once for the
      cast and held, so the frame is continuous from the first episode to the
      last. Any flicker at an episode boundary means the screen is still being
      taken per episode.
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

## Before blaming the code: is the device even reaching this machine?

The single most expensive failure in this feature's history is a cast that works
on one day and silently does nothing the next, and the cause is almost always
the host firewall rather than the code.

**The trap: `ufw status` lies.** It reads the *saved config*, not the live
kernel ruleset, so a rule can be listed and absent at the same time. The symptom
is a device that accepts `LAUNCH` and `LOAD`, then never fetches anything -- no
media session is ever created, which reads like a refused load but is usually a
dropped packet. The server's request log settles it: no `requested` line means
nothing arrived.

Two mechanisms produce it, and they look identical from here:

1. **`iptables.service` restoring a stale snapshot.** It runs `iptables-restore`
   from `/etc/iptables/iptables.rules` at boot, which ufw knows nothing about.
   Whichever service writes last wins, so ufw's rules can be wiped at every
   boot. Confirmed on this machine in September 2026: the snapshot was dated
   May, carried `:INPUT DROP`, held allow-rules for a NordVPN install that had
   since been removed, and mentioned port 8010 nowhere. ufw reported the cast
   rule present throughout.

   On a ufw system this service is redundant -- ufw persists its own rules --
   and running both means two owners for one table:

   ```bash
   systemctl is-enabled iptables ip6tables   # expect disabled on a ufw host
   sudo systemctl disable --now iptables
   ```

2. **Docker rewriting the filter table** when its daemon or any container
   starts, also without going through ufw. Check `systemctl is-active docker`
   before reaching for this one; it is not the cause when Docker is inactive.

Check which firewalls actually own the table before diagnosing anything:

```bash
for s in ufw iptables ip6tables nftables firewalld docker; do
  printf '%s: enabled=%s active=%s\n' "$s" \
    "$(systemctl is-enabled $s 2>/dev/null || echo n/a)" \
    "$(systemctl is-active $s 2>/dev/null || echo n/a)"
done
```

**Prove inbound works without spending a cast on it.** Bind a plain HTTP server
to the LAN address and fetch it from a phone on the same Wi-Fi. This takes the
program out of the question entirely -- if this fails, the bug is not in this
repo:

```bash
python3 -m http.server 8010 --bind <this-machine-lan-ip>
# then open http://<this-machine-lan-ip>:8010/ on a phone
```

A `200` in that server's output is inbound confirmed. Silence is a dropped
packet.

Check the live chain, not the status:

```bash
sudo iptables -L ufw-user-input -n -v | grep -E "8010|48010"
```

An empty result means the rule is in the config and not in the kernel. Re-apply
it with `ufw allow` rather than editing the rules file, because `ufw allow`
writes the config *and* applies it:

```bash
sudo ufw allow from 192.168.0.0/24 to any port 8010 proto tcp comment 'otakase cast'
```

Expect to do this again after a Docker restart. `sudo iptables -L DOCKER-USER -n -v`
shows whether Docker is intercepting ahead of ufw's chain.

**Also check the service, not just the file:** `/etc/nftables.conf` on some
systems carries a `policy drop` input chain that looks entirely guilty, while
`nftables.service` is disabled and the file is not loaded at all. Confirm with
`systemctl is-enabled nftables` before blaming it.

**And watch the log, not the symptom.** A device that plays a public stream but
not yours is a network problem. A device that plays neither is a receiver
problem. A browser on a phone that shows an error page looks identical whether
the packet was dropped or the URL was wrong -- the server's access log is what
distinguishes them.

One more measurement trap: a device that still has a previous stream loaded will
report it as playing. Stop the media and confirm the receiver is idle before
concluding anything from a status reading.

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
- [ ] **Between episodes, the TV shows the idle card** — cover art, the
      anime's title, and "Episode N watched — Episode N+1 up next" — instead
      of the Default Media Receiver's own idle screen, for the whole
      countdown.
- [ ] **At the end of a season, the idle card does not flash on screen right
      before the device disconnects.** The countdown is skipped entirely at
      season end (see `castSeasonFinished`), and so is this card.

## 7. The spawned window answers for itself

A cast launched from the rofi keybind runs in a terminal the viewer is not at.
Nothing in that window may wait for a keyboard — see
`cast-window-prompts.md` for what each prompt decides instead. These are the
cases only a real device and a real room can settle.

- [ ] **Finish a whole season from a rofi launch without touching the
      keyboard.** The window must never show a menu, a text prompt, or sit
      waiting. The last thing printed should be a completion summary, and the
      window should close or return to a prompt on its own.
- [ ] **At the end of a season, a score picker appears in the panel**, in the
      same frame as the episode countdown -- header, message row, keys. It opens
      at 8/10 and shows `Ns to answer`, counting down from 10.
- [ ] **Press ↑ once, then take 30 seconds deciding.** The clock must vanish and
      the picker must still be there. The footer should now read `q to skip`.
- [ ] **Press ↑, pause 30s, press ↓, pause 30s, then Enter.** It must still be
      waiting. Every press puts the two minutes back, so a deadline set by the
      first keypress cannot expire mid-thought.
- [ ] **Press ↑ and then walk away for more than two minutes.** It declines on
      its own. That deadline is deliberately not on the panel, so this is the one
      thing worth confirming by hand: the season should end normally with
      `rating skipped (cast window)`, not sit on the prompt.
- [ ] **↑ and ↓ move the score, Enter saves it.** Then the panel reads
      `Rated N.` and the summary reads `rating saved (N)`. Press nothing and it
      reads `No answer -- rating skipped.` with `rating skipped (cast window)`.
- [ ] **Press ↑ three times quickly — it must move three points, not one.**
      Reading one key per tick is how the first version of this ate every
      keypress.
- [ ] **Press space or an arrow left/right during the picker.** Nothing should
      happen. Those are playback controls, and a viewer pressing one by accident
      must not have their rating written or discarded.
- [ ] **Check the rating on the tracker, not just the panel.** `Rated 8.` with no
      score on the entry means the write failed while the panel claimed success,
      which is the bug this replaced.
- [ ] **Confirm the summary names what it assumed.** It should read
      `rating skipped (cast window)` and `sequel skipped (cast window)` rather
      than the plain `rating skipped`, so a suppressed decision is visible
      rather than silent.
- [ ] **The completion summary reaches the terminal, not a notification.** It is
      held while the panel owns the screen and printed once the screen is
      released, so it is the last line in the scrollback. If it arrives as a
      desktop notification instead, the deferral is broken.
- [ ] **Your AniList list is untouched by the suppressed prompts.** A cast
      finale must not have added a sequel to Watching or Plan to Watch, and
      `SkipRemoteSync` must still hold — check the entry by hand.
- [ ] **Force a dead end mid-season.** The reliable way is to point
      `Provider` at a hostname that does not resolve *while casting a season
      that already has episode 1 cached*, then let the countdown carry into
      episode 2. Expect one automatic re-search and then
      `Could not find a stream for episode 2; stopping the cast.` It must
      **stop** — this path used to be an unbounded `for {}` that only a viewer
      backing out could end, and an auto-answer without a bound turns it into an
      infinite loop.
- [ ] **The dead-end diagnosis reaches you somehow** — the panel holds the
      terminal for the whole cast, so it arrives as a desktop notification
      rather than as text on screen. If nothing arrives at all, check
      `otakase-debug.log`; the panel is only allowed to swallow it into a
      notification, never discard it.
- [ ] **Confirm the window is not left raw** after any of the above. If
      keystrokes echo oddly afterwards, `reset` and report it — a prompt ending
      while the cast reader is still parked is exactly the sequence that would
      do that.
- [ ] **At season end, the TV reconnects and shows a rating card** — cover
      art, the anime's title, "Rate this anime — check your terminal" —
      while the terminal panel runs the actual score picker. Confirm the
      device returns to its own idle screen once a score is given or the
      window times out (not left showing the card forever).
- [ ] **Power the device off (or block it at the firewall) before the season
      ends.** The score picker in the terminal must still work exactly as
      before this feature existed -- no delay, no error shown to the
      viewer, just a log line about the reconnect failing.

## Known limits

Not bugs; these are deliberate, and documented in the README.

- Seeking and resuming both rebuild the stream from the target, because the
  receiver ignores SEEK on a playlist with no EXT-X-ENDLIST. Each one costs a
  few seconds of rebuffering. Worth checking on hardware: that burned subtitles
  are still in sync after a seek, and that the panel reports episode time rather
  than the rebuilt stream's own clock.
- Soft subtitles cannot be cast; the Default Media Receiver renders WebVTT
  only.

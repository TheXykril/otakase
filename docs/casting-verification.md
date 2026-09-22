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
- [ ] If the provider offered soft subtitles, otakase says they cannot be cast
      and none appear. That is expected — set `SubStyle=hard` for a hardsubbed
      stream instead.

## 2. It tracks correctly

- [ ] Seeking from the TV remote or the Google Home app is picked up within
      about a second.
- [ ] With `SkipOp`/`SkipEd` on and known skip times, the opening and ending
      are jumped automatically.
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

## Known limits

Not bugs; these are deliberate, and documented in the README.

- `-cast` plays **one episode** and does not advance to the next, so it never
  reaches the next-episode prompt or end-of-series scoring.
- Casting always starts from the beginning. It tells you when you had a resume
  position, and it writes your position back on exit so local playback can
  resume from it later.
- Soft subtitles cannot be cast; the Default Media Receiver renders WebVTT
  only.

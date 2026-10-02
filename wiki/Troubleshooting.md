# Troubleshooting

For casting-specific issues, see [Casting Problems](Casting-Problems) instead.

## Nothing is found for a show

Titles differ between your tracker and the sources otakase searches. Two
fixes:

- Set `ManualProviderSearch=true` (`otakase -e`) to pick the match yourself
  next time.
- Use the **Remap** action — `^r` in the terminal list, or a menu entry under
  rofi — to correct a bad match permanently for that show.

## Playback never starts (local, not casting)

Run:

```bash
otakase -provider-status
```

This checks every provider and reports which ones are actually responding —
streaming hosts go down or change constantly, and it's usually not otakase's
fault. Also check `otakase-debug.log`, and raise `MpvPlaybackStartTimeout` if
your connection is slow to open a stream.

## The rofi menus look wrong

Themes are rewritten at every launch, so a stale or broken theme fixes itself
on the next run. If it's still wrong, confirm `rofi` is installed and
`RofiSelection=true` in your config.

## Progress didn't update on AniList / MyAnimeList

Tracker writes are paced to stay inside API rate limits and happen in the
background — they may take a moment. Check `otakase-debug.log` for the write
attempt, and confirm `TrackingRemote` is set to what you expect (`anilist`,
`myanimelist`, or `both`).

## Where to find things

| What | Where |
|---|---|
| Debug log | `<StoragePath>/otakase-debug.log` |
| Watch history | `<StoragePath>/curd_history.txt` |
| Config file | edit with `otakase -e` |

`StoragePath` is set in your config; the default is documented in the
[README](https://github.com/TheXykril/otakase#readme).

---

Still stuck? [Open an issue](https://github.com/TheXykril/otakase/issues) with
the relevant lines from `otakase-debug.log`.

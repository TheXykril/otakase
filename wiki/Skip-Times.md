# Skip times

Openings, endings and recaps are skipped using whichever service knows them.
All are asked at once: the source playing the episode,
[AniSkip](https://api.aniskip.com/api-docs), [TheIntroDB](https://theintrodb.org)
and [Anime-Skip](https://anime-skip.com). For each kind, the first one in that
order with a usable answer wins. None of them needs setup.

TheIntroDB files episodes under TMDB show numbers, so the first time it is
needed Otakase downloads a small lookup table (about 6 MB, refreshed monthly).
Playback is never held up for it.

Turn services off under **More → Settings → Playback**: *TheIntroDB skip
times* and *Anime-Skip skip times*. AniSkip is always asked. Anime-Skip uses
the public client id it publishes for trying its API; being shared, it may be
rate-limited now and then.

## See where a skip came from, and vote

In the player, press **Skip times** (the flag button on desktop). The panel
lists every span of the episode with the service it came from. Pick one to
watch it. For AniSkip spans, answer **Right times?** with Yes or No: the vote
goes to AniSkip and helps everyone get better times.

## Mark missing or wrong times

From the same panel, pick **Mark opening**, **Mark ending** or **Mark recap**
(it says **Fix** when one is already there). A card stays on the video while
it plays:

- **Now** sets the start or the end to the moment playing.
- **−1s** and **+1s** nudge either by a second.
- **Watch start** and **Watch end** jump just before each point, so you can
  check them.
- **Send** sends the span to AniSkip; the player uses it straight away.

A new mark starts where you are, with a typical length (1:30 for openings and
endings), so often only the end needs a nudge. Back closes the card.

Marked times are public: every player that reads AniSkip gets them. They are
filed under a random id the app makes for your profile; AniSkip has no
accounts and asks for nothing about you. A show without a MyAnimeList id
can't take times, because AniSkip files them under that id.

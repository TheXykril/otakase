# Skip times

Openings, endings and recaps skip automatically when enabled. Otakase
checks the playing source first. If an opening or ending is missing, it
checks [AniSkip](https://api.aniskip.com/api-docs), [TheIntroDB](https://theintrodb.org)
and [Anime-Skip](https://anime-skip.com), as enabled. If the results still
lack an opening or ending, the source with the most usable markers wins.
An episode can have either marker, both, or neither.

An ending skip jumps to the end of the marked credits. Any footage after
the marker continues playing.

TheIntroDB files episodes under TMDB show numbers, so the first time it is
needed Otakase downloads a small lookup table (about 6 MB, refreshed monthly).
Playback is never held up for it.

Turn services off under **More → Settings → Playback**: *TheIntroDB skip
times* and *Anime-Skip skip times*. AniSkip is always enabled. Anime-Skip uses
the public client id it publishes for trying its API; being shared, it may be
rate-limited now and then.

## TV series

Episodes of TV series (movies & shows) get their intro, recap and credits
times from TheIntroDB, looked up by the show's TMDB id, so no table is
needed. They skip the same way, following the same Playback settings.
Films have no skip times. Marking and voting are for anime only.

## See where a skip came from, and vote

In the player, press **Skip times** (the » icon on phones and TVs, the flag
button on desktop). The panel
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

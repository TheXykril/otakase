# Tracking

On first start Otakase asks how you want to track: **local**, **anilist**,
**myanimelist**, or **anilist + myanimelist**. Local history stays on in every
mode, so you can always resume.

In dual mode, Otakase compares the last update time for each show and pushes
the newer status, progress and category to whichever tracker is behind, so the
two converge on their own. You can change your mind later from the **Change
Tracker** menu, which can merge both lists or let one replace the other.

MyAnimeList sign-in needs your own MAL application credentials, set in the
config file or the environment:

```bash
OTAKASE_MAL_CLIENT_ID=your_client_id
OTAKASE_MAL_CLIENT_SECRET=your_client_secret
```

If the browser reaches the localhost callback page but Otakase does not
continue, run the command again and paste the full callback URL when prompted.

## Resuming on another device

With AniList or MyAnimeList on, your place in an episode is kept in the show's
AniList note or MyAnimeList comment as a short tag like `[otakase:5@754]`
(episode 5, 754 seconds in), next to anything else you wrote there. Another
computer running otakase picks it up and resumes from there.

## Adding shows and fixing progress

**Add new anime**, at the end of a list, searches AniList and asks which list
to put the show on. *Already watched some elsewhere* asks how many episodes
you have seen, records them, and starts at the next one; all of them marks the
show completed. Progress set this way, with Update, or on another device is
followed next time you play the show, rather than an older local position.

The menu opens with the shows you played last, newest first. `ContinueWatchingRows` sets how many are shown; `0` hides them.

## Coming from curd

Nothing to do. On first launch Otakase copies your config, your AniList and
MyAnimeList tokens and your watch history from `~/.config/curd` and
`~/.local/share/curd` into its own locations, and repoints `StoragePath` if it
was still on the old default. A storage path you chose yourself is left alone.

The originals are **copied, not moved**, so they are still there if you want
them; delete them once Otakase looks right.

The `curd` command itself is gone — installing Otakase replaces the old package
rather than sitting beside it. Use `otakase`, or `otk`.

If you had set `CURD_MAL_CLIENT_ID` or `CURD_MAL_CLIENT_SECRET` in a shell
profile, those names still work. `OTAKASE_MAL_*` takes precedence.

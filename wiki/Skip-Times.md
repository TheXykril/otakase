# Skip times

Openings and endings are skipped using whichever source knows them, asked in
this order: the provider in use, [AniSkip](https://api.aniskip.com/api-docs),
[theintrodb](https://theintrodb.org), and optionally Anime-Skip. The first
three need no setup.

The first time theintrodb is needed, otakase downloads a small lookup table in
the background; playback is never held up for it. Set `IntroDBSkipTimes=false`
to leave theintrodb out.

## Fixing the times yourself

When a skip is wrong, or missing, you can say so from the player:

| Key | What it does |
|---|---|
| `Alt+o` | Mark the opening — once at its start, again at its end |
| `Alt+e` | The same for the ending |
| `Alt+s` | Send what you marked to AniSkip — press once to see it, again to send |
| `Alt+r` | Forget the marks |
| `Alt+u` | Agree with the skip you were given |
| `Alt+d` | Report the skip you were given as wrong |

With otakase's player look (`MpvSkin`), the same actions are in the skip
times menu (the fast-forward button in the control row), and the keyboard
button or `?` lists every key.

Everything appears on the player's own screen, so you never leave the episode.
Nothing is sent until you press `Alt+s` a second time, with the exact times on
screen in between — these entries are public, and every player that reads
AniSkip inherits them. Submissions are filed under an id this program invents
and keeps; AniSkip has no accounts and asks for nothing about you. Set
`ContributeSkipTimes=false` to leave the keys unbound.

## Adding Anime-Skip

[Anime-Skip](https://anime-skip.com) is the fourth source, and the only one
that needs a client id. The simplest way to turn it on:

```
AnimeSkipClientID=auto
```

Otakase then uses Anime-Skip's public shared id and replaces it on its own if
it changes. Being shared, it may be rate-limited.

For an id of your own, run:

```
otakase -setup-anime-skip
```

This opens Anime-Skip's account page in your browser — sign up first if you
have not already — and asks you to paste back the client id you create there.

You can also write an id you already have straight into the config, in place
of `auto`:

```
AnimeSkipClientID=your_client_id_here
```

Leaving the setting empty is fine — Anime-Skip is simply not asked, and the
other sources carry on.

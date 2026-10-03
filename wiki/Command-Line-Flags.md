# Command-line flags

Every flag works with `otakase` or `otk`. A flag always wins over the same setting in the config file, for that run only. Settings that last are on [Configuration](Configuration).

| Flag | Description | Default |
|---|---|---|
| `-c` | Continue the last episode | |
| `-new` | Add a new anime to your list | |
| `-dub` / `-sub` | Choose the audio track | |
| `-softsub` / `-hardsub` | Prefer external or burned-in subtitles | |
| `-rofi` / `-no-rofi` | Use the rofi interface, or force the terminal | |
| `-image-preview` / `-no-image-preview` | Poster previews in rofi | |
| `-skip-op` | Skip openings | `true` |
| `-skip-ed` | Skip endings | `true` |
| `-skip-filler` | Skip filler episodes | `true` |
| `-skip-recap` | Skip recap sections | `true` |
| `-next-episode-prompt` | Ask before playing the next episode | |
| `-percentage-to-mark-complete` | Watched percentage that counts as complete | `85` |
| `-player` | Playback binary | `mpv` |
| `-save-mpv-speed` | Carry mpv speed to the next episode | `true` |
| `-score-on-completion` | Prompt to rate a finished show | `true` |
| `-discord-presence` | Discord Rich Presence | `true` |
| `-subs-lang` | Subtitle language | `english` |
| `-storage-path` | Data directory | `$HOME/.local/share/otakase` |
| `-download` | Save episodes instead of playing them (needs `ffmpeg`) | |
| `-episodes` | Episodes to save, e.g. `5` or `1-12` | selected |
| `-download-dir` | Where to save them | `$HOME/Downloads/otakase` |
| `-download-format` | Container for downloads, `mkv` or `mp4` | `mkv` |
| `-cast` | Play on a Chromecast, DLNA TV or Kodi on this network instead of locally | |
| `-cast-setup` | Let casting through your firewall: fixes the cast ports, shows the rules and runs them with sudo if you say yes, then lists the devices it finds | |
| `-show-new-episodes` | Mark shows with an unwatched episode in the list | `true` |
| `-vim-keys` | `j`/`k`/`h`/`l` to move and `/` to search in menus | |
| `-check-updates` | Look for a newer release at launch | `true` |
| `-discord-client-id` | Discord application id for Rich Presence | |
| `-install-keybind` | Bind Super+Shift+A to the rofi menu in your Hyprland config | |
| `-remove-keybind` | Undo `-install-keybind` | |
| `-force-keybind` | Let `-install-keybind` take a key something else uses | |
| `-refresh-keybind` | Update an existing binding, adding nothing if absent | |
| `-provider-status` | Probe each configured source and report which respond | |
| `-provider-status-query` | Search term `-provider-status` probes with | `one piece` |
| `-change-token` | Change your authentication token | |
| `-setup-anime-skip` | Get a personal Anime-Skip client id and save it | |
| `-e` | Edit the config file | |
| `-u` | Update to the latest release | |
| `-v` | Show the version | |

```bash
otk                                 # the short form, identical in every way
otakase -c                          # continue where you left off
otakase -rofi -image-preview        # graphical menu with posters
otakase -dub -next-episode-prompt   # dub, asking before each next episode
```

## Saving episodes

```bash
otakase -download                   # the episode you are up to
otakase -download -episodes 1-12    # a range
otakase -download -download-dir ~/Videos/anime
```

Needs `ffmpeg`. Episodes are saved as `.mkv`, keeping every audio track and styled subtitles; `-download-format mp4` saves MP4 instead. A finished episode is never fetched twice, whichever format it was saved in.

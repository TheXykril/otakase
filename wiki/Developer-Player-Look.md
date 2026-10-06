# Player look

otakase gives the mpv it starts its own controls (`MpvSkin`). This page is how
that works; what users see is on [Configuration](Configuration).

## Pieces

- **otakase_skin.lua** (`internal/mpvskin/assets/scripts`) is the whole
  on-screen controller, drawn with ASS into one `osd-overlay`: the top bar
  (back button, title, tracker/source chips), the seek bar with openings and
  endings marked, the control row (previous, play, next, time, volume,
  episodes, audio, subtitles, quality, fullscreen) and its menus, the
  *Skip Opening/Ending* button, the *Skipped Opening · Undo* notice and the
  *Up next* card. mpv's own OSC is turned off with `--osc=no`, and its
  seek/volume bar with `--osd-bar=no --osd-on-seek=no` (the skin's controls
  flash up on seeks and volume changes instead).
- **Keys**: Up/Down are bound to `no-osd add volume ±5` with
  `mp.add_key_binding` (mpv's default is a one-minute seek). Being plain
  bindings, the user's `input.conf` still overrides them; Left/Right keep
  mpv's 5-second seek.
- **Keys panel and skip times menu**: two more menus in the control row. The
  keyboard button (or `?`, also a plain binding) lists the keys from the
  script's `KEYS` table under *Player*, then the skip-time keys under
  *Skip times* when the `contribute` script-opt is on. The fast-forward button, shown only with `contribute`
  (`ContributeSkipTimes`), sends `script-message otakase-skip <action>`,
  exactly what otakase's own Alt bindings send (`skipMarkerBindings` in
  `internal/skip_marker.go`), so both paths stay one feature.
- **Films and series** (the Movies section) have no skip times:
  `mpvSkinArgs` gets `skipTimes=false` (`anime.Movie != nil`), passing
  `contribute=no` and `skips=no`, so the skip times button, the *Skip times*
  keys and the Enter-to-skip line are left out. `StartSkipMarker` never binds
  the Alt keys for them either.
- **MaterialIconsRound.otf** (`assets/fonts`, Apache-2.0, licence in
  `assets/LICENSE.MaterialIcons`) draws the icons by ligature name
  (`play_arrow`, `skip_next`, `subtitles`, …). It is loaded with
  `--osd-fonts-dir`, so nothing is installed and Windows works the same.
  Keep only fonts in that folder: libass tries to open every file in it.
- **logo/otakase.ass** is the app icon as ASS drawings, one shape per line
  with the palette role it takes. The script reads it from the `logo`
  script-opt and draws it in the middle of the window while there is no
  picture (`video-params` unset), so a loading episode shows the logo rather
  than black. It is generated; see [App Icon](Developer-App-Icon).

## Launch

`mpvskin.Install` writes the embedded files to
`<StoragePath>/mpv-skin/<content hash>/` once per build (written to a
temporary folder and renamed, so two otakase processes never race), and
removes copies from older builds.

`mpvSkinArgs` (`internal/mpv_skin.go`) decides per launch, and adds nothing
when:

- `MpvSkin=false`, or the player is not `mpv`/`mpv.exe` (IINA, mpv-android,
  others);
- `MpvArgs` already decide the controls (`--osc`, `--script=`, `--no-config`);
- `MpvSkin=auto` and the user's mpv config has a skin script (`uosc`,
  `modernx`, `modernz`, `osc*`, … in `scripts/`) or `osc=no` in `mpv.conf`
  (`mpvskin.ConfigDirs` lists where mpv looks, `$MPV_HOME` and
  `portable_config` included);
- `mpv --version` is older than 0.37 or unreadable: `--osd-fonts-dir` is
  0.37+, and mpv refuses to start on a flag it does not know.

The flags go first on the command line, so the user's `MpvArgs` after them
still win: `--osc=no`, `--script=`, `--osd-fonts-dir`, `--osd-font` (the
monospace font from `theme.MonospaceFont`, unless `mpv.conf` or `MpvArgs`
set one), and one `--script-opts-append=otakase_skin-<key>=<value>` per
option. Command-line script options beat the user's own
`script-opts/otakase_skin.conf`, for this process only.

A skin of the user's own (only reachable with `MpvSkin=true`, or one loaded
from a folder otakase does not look in) still loads, so it is always asked to
stay hidden for this process:
`uosc-disable_elements=timeline,controls,volume,top_bar,…`,
`modernz-visibility=never`, `modernx-visibility=never`.

## Colours

`mpvSkinColors` maps the active palette: `background` (cards; the bar fades are always pure black `000000`, so a light theme shades the picture rather than greying it, and they blend into letterbox bars),
`surface` (menus, chips, hovered buttons), `foreground`, `bright` (title,
current time), `dim` (`MetaText`), `accent` (play button, seek fill),
`accent_text` (readable on the accent) and `highlight` (`ResumeText`, the
opening/ending marks). They are passed as `rrggbb`; ASS wants `bbggrr`,
which the script converts.

## Drawing notes

- Text is measured by libass itself: a hidden overlay with
  `compute_bounds` returns the box a string was drawn in (cached per
  string and size).
- ASS has no gradients; the bar fades are bands 3 px tall that meet
  exactly. Overlapping bands double their alpha into visible stripes.
- Sizes scale with the window height (`h / 720`, clamped).
- While the controls are up, `sub-pos` is raised so subtitles sit above
  them, and put back when they hide. `sub-margin-y` would not move them
  while paused.

## Openings, endings, undo

The skin reads openings and endings from the chapter list otakase already
writes (`SendSkipTimesToMPV`: chapters titled `Opening` and `Ending`).

otakase skips by seeking from the first two seconds of a span to its end
(`SkipSeekTarget`, polled once a second). The script, told by
`otakase_skin-skip_op`/`-skip_ed` that otakase skips these:

- hides the *Skip* button for the first 2.5 s of the span, so it does not
  flash before the automatic skip;
- treats a seek from there to the span's end as that skip and shows *Undo*,
  which seeks back to 2.5 s into the span, past the window otakase skips
  from, so it is not skipped again.

## State from otakase

`SendMPVSkinState` sends `script-message-to otakase_skin otakase-state
{"chips":[{"icon":"sync","text":"AniList"},...],"episode_title":"..."}` when
an episode starts and when the playlist moves on (the source can change after
a fallback). `episode_title` is the episode's name from Jikan (English, else
romaji), shown after the title in the top bar; it is sent only once Jikan has
been asked for that episode (`finalizePlaylistEpisodeChange`), left out
otherwise so a stale one never shows, and the script clears it whenever the
playlist position changes. It retries for a few seconds, because right after launch the script may not
have registered yet. Only sockets started with the skin are sent to.

## Clicks and keys

The script binds `MBTN_LEFT` and the wheel only while the pointer is over
one of its buttons or bars, so every other click still reaches the user's
`input.conf`. `ENTER` is bound only while the corner offers something
(skip, undo, play next), `ESC` only while a menu is open.

## Trying it without a show

Under X11 (or Xvfb with `--vo=x11`), start mpv with the output of
`mpvskin.Args` and two local files, then set chapters and chips over IPC:

```json
{"command":["set_property","chapter-list",[{"title":"Opening","time":90},{"title":"Main","time":180},{"title":"Ending","time":1330},{"title":"Post-Credits","time":1420}]]}
{"command":["script-message-to","otakase_skin","otakase-state","{\"chips\":[{\"icon\":\"sync\",\"text\":\"AniList\"}]}"]}
```

IPC `mouse x y` moves the pointer and `mouse x y 0 single` clicks.

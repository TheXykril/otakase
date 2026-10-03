# Player look

otakase gives the mpv it starts its own controls (`MpvSkin`). This page is how
that works; what users see is on [Configuration](Configuration).

## Pieces

- **uosc** (`internal/mpvskin/assets/scripts/uosc`, LGPL-2.1, version in
  `mpvskin.UOSCVersion`) draws the timeline, control bar, top bar and menus.
  It is a release copy without its `bin/` helper (18 MB), which only the
  updater and clipboard copy/paste use. The updater is turned
  off with `disable_elements=updater`.
- **otakase_skin.lua** (same folder) draws what is otakase's own: the
  *Skip Opening/Ending* button, the *Skipped Opening · Undo* notice, the
  *Up next* card and the tracker/source chips under the top bar. It uses
  uosc's icon font (Material Icons Round, by ligature name: `skip_next`,
  `undo`, `play_arrow`), so both look alike.
- **Fonts** (`assets/fonts`) are uosc's icon and texture fonts, loaded with
  `--osd-fonts-dir` so nothing is installed and Windows works the same.

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
still win: `--osc=no`, two `--script=`, `--osd-fonts-dir`, `--osd-font` (the
monospace font from `theme.MonospaceFont`, unless `mpv.conf` or `MpvArgs`
set one), and one `--script-opts-append` per option. The `-append` form takes
the whole value as one option; `--script-opts=` would split uosc's
comma-separated lists. Command-line script options also beat the user's own
`script-opts/uosc.conf`, for this process only.

## Colours

`mpvSkinColors` maps the active palette: accent → uosc `foreground` (timeline
fill, active buttons), background → bar background, foreground → text,
`ResumeText` (yellow) → the opening/ending ranges, `MetaText` → dim text in
otakase_skin.lua. uosc takes colours as `rrggbb` without `#`; ASS wants
`bbggrr`, which the Lua script converts.

## Openings, endings, undo

The skin reads openings and endings from the chapter list otakase already
writes (`SendSkipTimesToMPV`: chapters titled `Opening` and `Ending`). uosc
shades them on the timeline by matching those titles.

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
{"chips":[{"icon":"sync","text":"AniList"},...]}` when an episode starts and
when the playlist moves on (the source can change after a fallback). It
retries for a few seconds, because right after launch the script may not
have registered yet. Only sockets started with the skin are sent to.

## Clicks and keys

The script binds `MBTN_LEFT` only while the pointer is over one of its
buttons, so uosc gets every other click, and `ENTER` only while the corner
offers something (skip, undo, play next).

## Updating uosc

1. Download `uosc.zip` from the uosc releases page.
2. Replace `internal/mpvskin/assets/scripts/uosc` with its `scripts/uosc`,
   without `bin/`; keep `LICENSE`. Replace `assets/fonts` with its `fonts`.
3. Bump `UOSCVersion`, then check the option names `mpvskin.Args` passes
   still exist in uosc's `main.lua` (`defaults`).
4. `go test ./internal/mpvskin/` and try it in mpv.

## Trying it without a show

Under X11 (or Xvfb with `--vo=x11 --override-display-fps=30`, since uosc
waits on display refresh), start mpv with the output of `mpvskin.Args` and
two local files, then set chapters over IPC:

```json
{"command":["set_property","chapter-list",[{"title":"Opening","time":90},{"title":"Main","time":180},{"title":"Ending","time":1330},{"title":"Post-Credits","time":1420}]]}
```
